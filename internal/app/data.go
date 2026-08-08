package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
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
	if rest == "outline.json" {
		w.Header().Set("X-Atlas-Feature-Partitions", "0")
		serveJSON(w, outlineResponse(held.Outline()))
		return
	}
	if strings.HasPrefix(rest, "features/") && strings.HasSuffix(rest, ".json") {
		a.handleFeaturePage(w, r, held, strings.TrimSuffix(strings.TrimPrefix(rest, "features/"), ".json"))
		return
	}
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
	if tile, err := held.RasterTile(rest); err == nil {
		w.Header().Set("X-Atlas-Raster-Ranges", "1")
		w.Header().Set("X-Atlas-Raster-Bytes", strconv.Itoa(len(tile.Data)))
		serveContent(w, tile.Data, tile.MediaType)
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
		for _, asset := range held.Outline().Assets {
			if asset.Path == rest {
				kind = asset.MediaType
				break
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

func (a *App) handleFeaturePage(w http.ResponseWriter, r *http.Request, volume hostenv.Volume, set string) {
	limit := 1_000
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "feature page limit is invalid", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	bounds, err := featureBounds(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := volume.FeaturePage(vnext.FeaturePageRequest{
		FeatureSet: set, Bounds: bounds, After: r.URL.Query().Get("after"), Limit: limit,
	})
	if err != nil {
		http.Error(w, "feature page could not be read", http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Atlas-Feature-Partitions", strconv.Itoa(page.PartitionsRead))
	w.Header().Set("X-Atlas-Feature-Bytes", strconv.FormatInt(page.BytesRead, 10))
	serveJSON(w, featurePageResponse(page))
}

type featurePageWire struct {
	Features []featureWire `json:"features"`
	Next     string        `json:"next,omitempty"`
}

type featureWire struct {
	ID            string             `json:"id"`
	Title         string             `json:"title"`
	Subtitle      string             `json:"subtitle,omitempty"`
	Description   string             `json:"description,omitempty"`
	Center        *vnext.Position    `json:"center,omitempty"`
	Shard         string             `json:"shard"`
	Geometry      geometryWire       `json:"geometry"`
	Properties    []propertyWire     `json:"properties,omitempty"`
	Relationships []relationshipWire `json:"relationships,omitempty"`
	Provenance    []provenanceWire   `json:"provenance,omitempty"`
}

type geometryWire struct {
	Kind  vnext.GeometryKind `json:"kind"`
	Parts []geometryPartWire `json:"parts"`
}

type geometryPartWire struct {
	Rings [][]vnext.Position `json:"rings"`
}

type propertyWire struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Kind  vnext.Kind  `json:"kind"`
	Value interface{} `json:"value"`
}

type relationshipWire struct {
	Predicate string `json:"predicate"`
	Target    string `json:"target"`
}

type provenanceWire struct {
	Source     string `json:"source"`
	NativeID   string `json:"nativeId"`
	CapturedAt string `json:"capturedAt"`
}

func featurePageResponse(page vnext.FeaturePageResult) featurePageWire {
	out := featurePageWire{Next: page.Next, Features: make([]featureWire, 0, len(page.Features))}
	for _, feature := range page.Features {
		out.Features = append(out.Features, featureResponse(feature))
	}
	return out
}

func featureResponse(feature vnext.Feature) featureWire {
	out := featureWire{
		ID: feature.ID, Title: feature.Title, Subtitle: feature.Subtitle, Description: feature.Description,
		Center: feature.Center, Shard: strconv.FormatInt(feature.Shard, 10),
		Geometry:      geometryWire{Kind: feature.Geometry.Kind, Parts: make([]geometryPartWire, 0, len(feature.Geometry.Parts))},
		Properties:    make([]propertyWire, 0, len(feature.Properties)),
		Relationships: make([]relationshipWire, 0, len(feature.Relationships)),
		Provenance:    make([]provenanceWire, 0, len(feature.Provenance)),
	}
	for _, part := range feature.Geometry.Parts {
		out.Geometry.Parts = append(out.Geometry.Parts, geometryPartWire{Rings: part.Rings})
	}
	for _, property := range feature.Properties {
		out.Properties = append(out.Properties, propertyResponse(property))
	}
	for _, edge := range feature.Relationships {
		out.Relationships = append(out.Relationships, relationshipWire{Predicate: edge.Predicate, Target: edge.Target})
	}
	for _, evidence := range feature.Provenance {
		out.Provenance = append(out.Provenance, provenanceWire{
			Source: evidence.Source, NativeID: evidence.NativeID, CapturedAt: evidence.CapturedAt,
		})
	}
	return out
}

func propertyResponse(property vnext.Property) propertyWire {
	id := property.FieldID
	if id == (vnext.ID{}) {
		id = property.Field.ID
	}
	out := propertyWire{ID: id.String(), Name: property.Field.Name, Kind: property.Value.Kind}
	switch property.Value.Kind {
	case vnext.KindBool:
		out.Value = property.Value.Bool
	case vnext.KindInt64:
		out.Value = strconv.FormatInt(property.Value.Int64, 10)
	case vnext.KindFloat64:
		out.Value = property.Value.Float64
	case vnext.KindString:
		out.Value = property.Value.String
	case vnext.KindBytes:
		out.Value = base64.StdEncoding.EncodeToString(property.Value.Bytes)
	case vnext.KindID:
		out.Value = property.Value.ID.String()
	default:
		out.Value = nil
	}
	return out
}

func featureBounds(r *http.Request) (*vnext.SpatialBounds, error) {
	names := []string{"minX", "minY", "maxX", "maxY"}
	values := make([]float64, len(names))
	present := 0
	for index, name := range names {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("feature bounds are invalid")
		}
		values[index] = value
		present++
	}
	if present == 0 {
		return nil, nil
	}
	if present != len(names) || values[0] > values[2] || values[1] > values[3] {
		return nil, fmt.Errorf("feature bounds are incomplete or inverted")
	}
	return &vnext.SpatialBounds{MinX: values[0], MinY: values[1], MaxX: values[2], MaxY: values[3]}, nil
}

func serveJSON(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "native data could not be encoded", http.StatusInternalServerError)
		return
	}
	serveContent(w, data, "application/json")
}

func serveContent(w http.ResponseWriter, data []byte, kind string) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}
