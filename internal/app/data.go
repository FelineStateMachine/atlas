package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The native vNext data plane. app_test.go holds schema, typed tables,
// content-addressed assets and the absence of the retired projections to it.

// BasePath is the URL prefix volume content is served under. It appears in the
// composed catalog so nothing has to assemble a content URL from parts it had
// to guess.
const BasePath = "/data/v"

// contentTypes names the content types the bundle layout can hold, and is the
// gate on what may be served at all: an entry whose extension is not named
// here is not served, whatever it is. Responses carry nosniff, so a response
// must say what it is or a browser drops it.
var contentTypes = map[string]string{
	".json": "application/json",
	".text": "application/json",
	".bin":  "application/octet-stream",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".pack": "application/vnd.atlas.table",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// catalogVolume is one volume as the composed catalog lists it. The base URL
// carries the version stamp, so everything beneath it may be cached as
// immutable and a new build of the volume arrives at new URLs.
//
// The field order and the tags are the wire, and the wire is pinned by
// TestCatalogComposition and TestCatalogCarriesTheBuildAndItsGrid.
type catalogVolume struct {
	Slug     string              `json:"slug"`
	Title    string              `json:"title"`
	Stamp    string              `json:"stamp"`
	Base     string              `json:"base"`
	TileGrid hostenv.TileGrid    `json:"tileGrid"`
	Worlds   []hostenv.WorldInfo `json:"worlds"`
}

// catalogDoc is the whole catalog. BundlesDir rides along so an empty library
// can tell the reader where a bundle goes, in the words of their own machine;
// it is whatever the store calls its location and the handler never reads it.
type catalogDoc struct {
	Volumes    []catalogVolume `json:"volumes"`
	BundlesDir string          `json:"bundlesDir,omitempty"`
}

// handleCatalog answers with the library as it stands. It is composed per
// request and never cached: it is the one response whose whole job is to be
// current.
func (a *App) handleCatalog(w http.ResponseWriter, r *http.Request) {
	body, err := composeCatalog(a.library().order, a.env.Volumes().Location())
	if err != nil {
		// Nothing in a validated manifest can fail to marshal. If something
		// does, an empty catalog is a saner face than a panic.
		slog.Error("composing the catalog", logging.Op("catalog"), slog.Any("error", err))
		body = []byte(`{"volumes":[]}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// composeCatalog folds the serving volumes into the catalog document.
// Volumes are listed by title, which is the order a reader meets them in.
func composeCatalog(volumes []hostenv.Volume, location string) ([]byte, error) {
	listed := make([]catalogVolume, 0, len(volumes))
	for _, volume := range volumes {
		info := volume.Info()
		listed = append(listed, catalogVolume{
			Slug: info.Slug, Title: info.Title, Stamp: info.Stamp,
			Base: volumeBase(info), TileGrid: info.TileGrid, Worlds: info.Worlds,
		})
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Title < listed[j].Title })
	return json.Marshal(catalogDoc{Volumes: listed, BundlesDir: location})
}

// volumeBase is where one build's content is served from.
func volumeBase(info hostenv.VolumeInfo) string {
	return BasePath + "/" + info.Slug + "/" + vnext.ShortStamp(info.Stamp)
}

// handleContent serves one entry out of one build.
//
// The stamp in the URL is the whole cache story: it names exactly one build of
// one volume, so the answer may be cached forever, and the moment a newer
// build takes the slug over the old URL is gone -- 404, which is the client's
// cue to refetch the catalog.
//
// Byte ranges are deliberately not served. Tiles are stored uncompressed
// precisely so that they could be, and issue #5 §2 describes them that way,
// but the data plane's published behavior is plainer: a Range request is
// answered 200 with the whole body and no Accept-Ranges, and
// TestContentDoesNotServeRanges asserts exactly that exchange. Answering 206
// would be a better data plane and a different one; it is a change to make
// deliberately, in the open, once something wants it.
func (a *App) handleContent(w http.ResponseWriter, r *http.Request) {
	held, ok := a.library().bySlug[r.PathValue("slug")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	info := held.Info()
	if r.PathValue("stamp") != vnext.ShortStamp(info.Stamp) {
		http.NotFound(w, r)
		return
	}

	rest := r.PathValue("rest")
	if rest == "schema.json" {
		data, err := held.Schema()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveContent(w, data, "application/schema+json")
		return
	}
	if strings.HasPrefix(rest, "data/") && strings.HasSuffix(rest, ".pack") {
		data, err := held.TableBlock(rest)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveContent(w, data, contentTypes[".pack"])
		return
	}
	if strings.HasPrefix(rest, "worlds/") || strings.HasPrefix(rest, "icons/") {
		http.NotFound(w, r)
		return
	}

	data, err := held.Blob(rest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	kind := ""
	if dot := strings.LastIndexByte(rest, '.'); dot >= 0 {
		kind = contentTypes[rest[dot:]]
	}
	if kind == "" {
		if semantic, err := a.semanticVolume(held); err == nil {
			for _, asset := range semantic.Assets {
				if asset.Path == rest {
					kind = asset.MediaType
					break
				}
			}
		}
	}
	if kind == "" {
		kind = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := w.Write(data); err != nil {
		// The response is already on the wire; there is nothing to say to the
		// client, and a reader who navigated away is the usual cause.
		slog.Debug("content cut short", logging.Op("serve"),
			logging.Volume(info.Slug), logging.Path(rest), slog.Any("error", err))
	}
}

func serveContent(w http.ResponseWriter, data []byte, kind string) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}
