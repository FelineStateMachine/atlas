package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The data plane. Its shape is not a choice this rewrite gets to make: the
// seam and the golden transcript both read it, and golden/http replays the
// recorded answers against this file byte for byte (issue #5 §4.2).

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
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// catalogVolume is one volume as the composed catalog lists it. The base URL
// carries the version stamp, so everything beneath it may be cached as
// immutable and a new build of the volume arrives at new URLs.
//
// The field order and the tags are the wire, and the wire is pinned by
// golden/fixtures/http/catalog.json.
type catalogVolume struct {
	Slug     string              `json:"slug"`
	Title    string              `json:"title"`
	Stamp    string              `json:"stamp"`
	Base     string              `json:"base"`
	TileGrid bundle.TileGrid     `json:"tileGrid"`
	Worlds   []bundle.WorldEntry `json:"worlds"`
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
		manifest := volume.Manifest()
		listed = append(listed, catalogVolume{
			Slug:     manifest.Volume.Slug,
			Title:    manifest.Volume.Title,
			Stamp:    manifest.Version.Stamp,
			Base:     volumeBase(manifest),
			TileGrid: manifest.TileGrid,
			Worlds:   manifest.Worlds,
		})
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Title < listed[j].Title })
	return json.Marshal(catalogDoc{Volumes: listed, BundlesDir: location})
}

// volumeBase is where one build's content is served from.
func volumeBase(m bundle.Manifest) string {
	return BasePath + "/" + m.Volume.Slug + "/" + bundle.ShortStamp(m.Version.Stamp)
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
// but the reference implementation sets a length and copies the entry: a Range
// request is answered 200 with the whole body and no Accept-Ranges, and
// golden/fixtures/http/transcript.json records exactly that exchange. Answering
// 206 here would be a better data plane and a red golden; it is a change to
// make deliberately, with a waiver, once something wants it. See
// golden/http/NOTES.md.
func (a *App) handleContent(w http.ResponseWriter, r *http.Request) {
	held, ok := a.library().bySlug[r.PathValue("slug")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	manifest := held.Manifest()
	if r.PathValue("stamp") != bundle.ShortStamp(manifest.Version.Stamp) {
		http.NotFound(w, r)
		return
	}

	rest := r.PathValue("rest")
	if !strings.HasPrefix(rest, bundle.WorldsPrefix) &&
		!strings.HasPrefix(rest, bundle.TilesPrefix) &&
		!strings.HasPrefix(rest, bundle.IconsPrefix) {
		http.NotFound(w, r)
		return
	}
	dot := strings.LastIndexByte(rest, '.')
	if dot < 0 {
		http.NotFound(w, r)
		return
	}
	kind, ok := contentTypes[rest[dot:]]
	if !ok {
		http.NotFound(w, r)
		return
	}

	entry, size, err := held.Open(rest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer entry.Close()
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := io.Copy(w, entry); err != nil {
		// The response is already on the wire; there is nothing to say to the
		// client, and a reader who navigated away is the usual cause.
		slog.Debug("content cut short", logging.Op("serve"),
			logging.Volume(manifest.Volume.Slug), logging.Path(rest), slog.Any("error", err))
	}
}
