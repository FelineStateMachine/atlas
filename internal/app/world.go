package app

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// The world as the display logic reads it.
//
// A world payload is JSON on the data plane and the seam reads it there
// directly; the application reads the same bytes for a different reason. Every
// display decision the reference implementation made in the browser -- which
// legend sections exist, what each row counts, which names speak, which
// features survive a filter -- is made here instead, once, in Go, before a
// template is handed anything (issue #5 §4.5).
//
// The decoding below is deliberately lenient in the reader's direction: a
// field this build has never heard of is ignored, and a collection missing
// half its optional fields still renders. Conventions are read only through
// format/semconv; nothing here compares an "atlas." string literal of its own.
//
// The payload's Go shape lives here rather than in format/bundle because the
// format package serves these bytes without opening them: nothing in it needs
// a struct for the payload, and inventing one there would export a shape the
// format lane has not committed to. When the format lane wants one, this
// moves and the app imports it.

// worldPayload is worlds/<slug>.json.
type worldPayload struct {
	Attrs       map[string]string   `json:"attrs"`
	Grid        *payloadGrid        `json:"grid"`
	Lenses      []payloadLens       `json:"lenses"`
	Collections []payloadCollection `json:"collections"`
	Merged      []payloadMerge      `json:"merged"`
}

// payloadGrid is a world's override of the volume's tile window. Absent
// fields keep the volume's.
type payloadGrid struct {
	SourceZoom *int `json:"sourceZoom"`
	FirstTile  *int `json:"firstTile"`
	TileSize   *int `json:"tileSize"`
	Size       *int `json:"size"`
}

// payloadLens is one raster pyramid picturing this world.
type payloadLens struct {
	Name    string `json:"name"`
	Tiles   string `json:"tiles"`
	MinZoom int    `json:"minZoom"`
	MaxZoom int    `json:"maxZoom"`
	Shard   int    `json:"shard"`
	// Bounds is the raster window the pyramid fills; Surface is the ground
	// that window pictures. They differ on a split sheet, where the window
	// was grown to take in a title drawn beside the map, and anything that
	// divides the world measures the ground (docs/render-seam.md §6.1).
	Bounds  *cells.Rect `json:"bounds"`
	Surface *cells.Rect `json:"surface"`
}

// payloadCollection is one ordered group of features.
type payloadCollection struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Group     string `json:"group"`
	Icon      string `json:"icon"`
	IconAsset string `json:"iconAsset"`
	// Color is the collection's own accent and IconColor the older spelling
	// of the same fact. Both are read because the seam reads both, and a
	// legend that fell back to the palette where the seam honoured a
	// declared colour would draw a different world than the map.
	Color     string            `json:"color"`
	IconColor string            `json:"iconColor"`
	Visible   *bool             `json:"visible"`
	Attrs     map[string]string `json:"attrs"`
	Features  []payloadFeature  `json:"features"`
}

// payloadFeature is one path or area. Points are not here: they live packed
// in the .bin, and owner indexes this array.
type payloadFeature struct {
	ID       int64              `json:"id"`
	Title    string             `json:"title"`
	Subtitle string             `json:"subtitle"`
	Parent   *int64             `json:"parent"`
	Center   *bundle.Coordinate `json:"center"`
	HasText  bool               `json:"hasText"`
	Shard    int64              `json:"shard"`
	Attrs    map[string]string  `json:"attrs"`
	Geometry []payloadGeometry  `json:"geometry"`
}

// payloadGeometry is one GeoJSON-shaped ring set in the volume's own degrees.
type payloadGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// payloadMerge is one source's account of what it contributed.
type payloadMerge struct {
	Slug   string `json:"slug"`
	Source string `json:"source"`
	Origin bool   `json:"origin"`
}

// ---------------------------------------------------------------------------
// The model
// ---------------------------------------------------------------------------

// worldModel is one world, stood up: its collections in payload order, its
// point features unpacked, its shape features projected onto the world square.
// It is immutable once built and shared by every request reading that build of
// that world.
type worldModel struct {
	Slug      string
	Lenses    []payloadLens
	Attrs     map[string]string
	Origin    string
	Grid      tileGrid
	Members   []*collectionModel
	ByID      map[string]*collectionModel
	Points    []*pointModel
	PointByID map[string]*pointModel
	Shapes    []*shapeModel
	ShapeByID map[string]*shapeModel
}

// collectionModel is one collection with everything a row needs already
// decided: its kind, its curated label policy, whether it starts hidden.
type collectionModel struct {
	ID    string
	Title string
	Kind  string
	Group string
	Icon  string

	// IconAsset is the artwork the collection wears, as a path under the
	// build's `icons/`. The map composes a marker from it and the legend
	// draws it as the row's mark; both name it the same way, which is what
	// keeps a row and the pins it stands for looking like each other.
	IconAsset string

	// Color and IconColor are the collection's declared accent, in the
	// order the seam consults them. Neither is a decision -- the one colour
	// a collection wears is `collectionColor`.
	Color     string
	IconColor string

	Attrs    map[string]string
	Curated  string // the producer's label policy, through semconv
	RenderAs string
	Hidden   bool // the payload's own "visible": false
	Count    int
	Index    int
	Shapes   []*shapeModel
}

// Domain is the ground an isolate may touch: point collections isolate
// against point collections, shape collections against shape collections, so
// highlighting a region and then asking for only one resource leaves the
// region standing with the resource inside it.
func (c *collectionModel) Domain() string {
	if c.Kind == semconv.GeometryPoint {
		return "features"
	}
	return "zones"
}

// pointModel is one packed location, already on the world square.
type pointModel struct {
	ID         string
	Title      string
	Lat, Lng   float64
	X, Y       float64
	Shard      int64
	Collection *collectionModel
}

// shapeModel is one path or area feature: its rings in world pixels, its
// extent, and the collection it belongs to.
type shapeModel struct {
	ID         string
	Title      string
	Subtitle   string
	Parent     string
	Depth      int
	HasText    bool
	Attrs      map[string]string
	Collection *collectionModel
	// Shard is the layer of a split world this ground belongs to. A shape on
	// another lens's shard is elsewhere in the world rather than filtered
	// out, exactly as a point is.
	Shard      int64
	Polygons   [][][]point // rings per polygon; ring 0 is the outline
	Lines      [][]point
	MinX, MinY float64
	MaxX, MaxY float64
	Drawn      bool // carries geometry the chart can draw
}

type point struct{ X, Y float64 }

// tileGrid is the window a world's degrees are projected through: the volume's
// grid, overridden by whatever the world declares for itself.
type tileGrid struct {
	SourceZoom int
	FirstTile  int
	TileSize   int
	Size       int
}

func (g tileGrid) project(lat, lng float64) (x, y float64) {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	xTile := (lng + 180) / 360 * worldTiles
	yTile := (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2 * worldTiles
	return (xTile - float64(g.FirstTile)) * float64(g.TileSize),
		-(yTile - float64(g.FirstTile)) * float64(g.TileSize)
}

// ---------------------------------------------------------------------------
// Building
// ---------------------------------------------------------------------------

// worlds is the model cache. Standing a world up decodes a payload and
// unpacks every location, which is work worth doing once per build rather than
// once per keystroke of a search. The key carries the build's stamp, so a new
// build is a new entry and nothing is ever stale; a handful of entries is kept
// because a reader wanders between two or three worlds and back.
type worldCache struct {
	mu    sync.Mutex
	held  map[string]*worldModel
	order []string
}

const worldsHeld = 6

func newWorldCache() *worldCache { return &worldCache{held: map[string]*worldModel{}} }

func (c *worldCache) get(key string) (*worldModel, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	model, ok := c.held[key]
	return model, ok
}

func (c *worldCache) put(key string, model *worldModel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.held[key]; !held {
		c.order = append(c.order, key)
		for len(c.order) > worldsHeld {
			delete(c.held, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.held[key] = model
}

// world stands one world of one volume up, or answers nil when the payload
// cannot be read. A world that will not decode is not a reason to refuse a
// page: the chrome still renders, the legend is simply empty, and the failure
// is in the log where somebody can act on it.
func (a *App) world(volume hostenv.Volume, slug string) *worldModel {
	manifest := volume.Manifest()
	key := manifest.Volume.Slug + "@" + bundle.ShortStamp(manifest.Version.Stamp) + "/" + slug
	if held, ok := a.worlds.get(key); ok {
		return held
	}
	model, err := buildWorld(volume, manifest, slug)
	if err != nil {
		return nil
	}
	a.worlds.put(key, model)
	return model
}

func buildWorld(volume hostenv.Volume, manifest bundle.Manifest, slug string) (*worldModel, error) {
	payload, err := readEntry(volume, bundle.WorldEntryName(slug, bundle.WorldSuffix))
	if err != nil {
		return nil, err
	}
	var decoded worldPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("world %s payload: %w", slug, err)
	}

	grid := tileGrid{
		SourceZoom: manifest.TileGrid.SourceZoom,
		FirstTile:  manifest.TileGrid.FirstTile,
		TileSize:   manifest.TileGrid.TileSize,
		Size:       manifest.TileGrid.Size,
	}
	if own := decoded.Grid; own != nil {
		if own.SourceZoom != nil {
			grid.SourceZoom = *own.SourceZoom
		}
		if own.FirstTile != nil {
			grid.FirstTile = *own.FirstTile
		}
		if own.TileSize != nil {
			grid.TileSize = *own.TileSize
		}
		if own.Size != nil {
			grid.Size = *own.Size
		}
	}

	model := &worldModel{
		Slug:      slug,
		Lenses:    decoded.Lenses,
		Attrs:     decoded.Attrs,
		Grid:      grid,
		ByID:      map[string]*collectionModel{},
		PointByID: map[string]*pointModel{},
		ShapeByID: map[string]*shapeModel{},
	}
	for _, account := range decoded.Merged {
		if account.Origin {
			model.Origin = account.Source
			break
		}
	}

	for at, held := range decoded.Collections {
		kind := held.Kind
		if kind == "" {
			kind = semconv.GeometryPoint
		}
		collection := &collectionModel{
			ID:        strconv.FormatInt(held.ID, 10),
			Title:     held.Title,
			Kind:      kind,
			Group:     held.Group,
			Icon:      held.Icon,
			IconAsset: held.IconAsset,
			Color:     held.Color,
			IconColor: held.IconColor,

			Attrs:    held.Attrs,
			Curated:  semconv.LabelPolicy(kind, held.Attrs),
			RenderAs: semconv.RenderAs(held.Attrs, ""),
			Hidden:   held.Visible != nil && !*held.Visible,
			Index:    at,
		}
		model.Members = append(model.Members, collection)
		model.ByID[collection.ID] = collection

		for _, feature := range held.Features {
			shape := buildShape(feature, collection, grid)
			collection.Shapes = append(collection.Shapes, shape)
			model.Shapes = append(model.Shapes, shape)
			model.ShapeByID[shape.ID] = shape
		}
		collection.Count = len(held.Features)
	}

	// Points arrive packed. The owner column indexes the collections array,
	// which is why the array's order is significant and why nothing here
	// sorts it.
	if packed, err := readEntry(volume, bundle.WorldEntryName(slug, bundle.PackedSuffix)); err == nil {
		if locations, err := bundle.UnpackLocations(packed); err == nil {
			for _, location := range locations {
				owner := int(location.Owner)
				if owner < 0 || owner >= len(model.Members) {
					continue
				}
				collection := model.Members[owner]
				x, y := grid.project(location.Lat, location.Lng)
				pin := &pointModel{
					ID:         strconv.FormatInt(location.ID, 10),
					Title:      location.Title,
					Lat:        location.Lat,
					Lng:        location.Lng,
					X:          x,
					Y:          y,
					Shard:      location.Shard,
					Collection: collection,
				}
				model.Points = append(model.Points, pin)
				model.PointByID[pin.ID] = pin
				collection.Count++
			}
		}
	}

	// Depth is the parent chain, so the feature index can indent a
	// sub-watershed under its watershed without the template counting.
	for _, shape := range model.Shapes {
		shape.Depth = shapeDepth(model, shape, 0)
	}
	return model, nil
}

func shapeDepth(model *worldModel, shape *shapeModel, guard int) int {
	if shape.Parent == "" || guard > 32 {
		return 0
	}
	parent, held := model.ShapeByID[shape.Parent]
	if !held {
		return 0
	}
	return 1 + shapeDepth(model, parent, guard+1)
}

func buildShape(feature payloadFeature, collection *collectionModel, grid tileGrid) *shapeModel {
	shape := &shapeModel{
		ID:         strconv.FormatInt(feature.ID, 10),
		Title:      feature.Title,
		Subtitle:   feature.Subtitle,
		HasText:    feature.HasText,
		Shard:      feature.Shard,
		Attrs:      feature.Attrs,
		Collection: collection,
		MinX:       math.Inf(1), MinY: math.Inf(1),
		MaxX: math.Inf(-1), MaxY: math.Inf(-1),
	}
	if feature.Parent != nil {
		shape.Parent = strconv.FormatInt(*feature.Parent, 10)
	}
	for _, geometry := range feature.Geometry {
		switch geometry.Type {
		case "Polygon":
			var rings [][][2]float64
			if json.Unmarshal(geometry.Coordinates, &rings) != nil {
				continue
			}
			shape.addPolygon(rings, grid)
		case "MultiPolygon":
			var polygons [][][][2]float64
			if json.Unmarshal(geometry.Coordinates, &polygons) != nil {
				continue
			}
			for _, rings := range polygons {
				shape.addPolygon(rings, grid)
			}
		case "LineString":
			var line [][2]float64
			if json.Unmarshal(geometry.Coordinates, &line) != nil {
				continue
			}
			shape.addLine(line, grid)
		case "MultiLineString":
			var lines [][][2]float64
			if json.Unmarshal(geometry.Coordinates, &lines) != nil {
				continue
			}
			for _, line := range lines {
				shape.addLine(line, grid)
			}
		}
	}
	shape.Drawn = len(shape.Polygons) > 0 || len(shape.Lines) > 0
	return shape
}

// Coordinates arrive GeoJSON-ordered -- longitude first -- and land on the
// world square through the volume's own tile window.
func (s *shapeModel) addPolygon(rings [][][2]float64, grid tileGrid) {
	converted := make([][]point, 0, len(rings))
	for _, ring := range rings {
		converted = append(converted, s.convert(ring, grid))
	}
	if len(converted) > 0 {
		s.Polygons = append(s.Polygons, converted)
	}
}

func (s *shapeModel) addLine(line [][2]float64, grid tileGrid) {
	if converted := s.convert(line, grid); len(converted) > 1 {
		s.Lines = append(s.Lines, converted)
	}
}

func (s *shapeModel) convert(ring [][2]float64, grid tileGrid) []point {
	out := make([]point, 0, len(ring))
	for _, pair := range ring {
		x, y := grid.project(pair[1], pair[0])
		out = append(out, point{X: x, Y: y})
		s.MinX, s.MaxX = math.Min(s.MinX, x), math.Max(s.MaxX, x)
		s.MinY, s.MaxY = math.Min(s.MinY, y), math.Max(s.MaxY, y)
	}
	return out
}

// ---------------------------------------------------------------------------
// Containment
// ---------------------------------------------------------------------------

// grace is the pixel of slack containment allows. A pin dropped on a zone's
// border was put there to mean the zone, and exact point-in-polygon arithmetic
// would flip it out over the width of the line it stands on.
const grace = 1.0

// contains answers whether a coordinate stands inside this feature: inside its
// outline and outside its holes, or within a pixel of anything it draws.
func (s *shapeModel) contains(x, y float64) bool {
	if x < s.MinX-grace || x > s.MaxX+grace || y < s.MinY-grace || y > s.MaxY+grace {
		return false
	}
	for _, polygon := range s.Polygons {
		if len(polygon) == 0 || !inRing(polygon[0], x, y) {
			continue
		}
		inHole := false
		for _, hole := range polygon[1:] {
			if inRing(hole, x, y) {
				inHole = true
				break
			}
		}
		if !inHole {
			return true
		}
	}
	for _, polygon := range s.Polygons {
		for _, ring := range polygon {
			if nearRing(ring, x, y) {
				return true
			}
		}
	}
	for _, line := range s.Lines {
		if nearRing(line, x, y) {
			return true
		}
	}
	return false
}

// inRing is the crossing-number test, boundary-inclusive on the way in.
func inRing(ring []point, x, y float64) bool {
	inside := false
	for at, prior := 0, len(ring)-1; at < len(ring); prior, at = at, at+1 {
		a, b := ring[prior], ring[at]
		if (a.Y > y) == (b.Y > y) {
			continue
		}
		if x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
	}
	return inside
}

// nearRing is the pixel of grace: within one world pixel of a drawn edge.
func nearRing(ring []point, x, y float64) bool {
	for at := 1; at < len(ring); at++ {
		if segmentDistanceSquared(ring[at-1], ring[at], x, y) <= grace*grace {
			return true
		}
	}
	return false
}

func segmentDistanceSquared(a, b point, x, y float64) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	span := dx*dx + dy*dy
	t := 0.0
	if span > 0 {
		t = ((x-a.X)*dx + (y-a.Y)*dy) / span
		t = math.Max(0, math.Min(1, t))
	}
	cx, cy := a.X+t*dx, a.Y+t*dy
	return (cx-x)*(cx-x) + (cy-y)*(cy-y)
}

// ---------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------

// featureText is one feature's entry in worlds/<slug>.text: prose, links, and
// the attributes that only matter once a card is open.
type featureText struct {
	Description string            `json:"d"`
	Links       []int64           `json:"l"`
	Attrs       map[string]string `json:"a"`
}

// text reads one world's prose file. The whole file is read rather than one
// entry because it is a single JSON object and there is nothing to seek to;
// it is small beside the tiles and it is read only when a card opens.
func (a *App) text(volume hostenv.Volume, world string) map[string]featureText {
	manifest := volume.Manifest()
	key := manifest.Volume.Slug + "@" + bundle.ShortStamp(manifest.Version.Stamp) + "/" + world + ".text"
	if held, ok := a.texts.get(key); ok {
		return held
	}
	out := map[string]featureText{}
	if data, err := readEntry(volume, bundle.WorldEntryName(world, bundle.TextSuffix)); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	a.texts.put(key, out)
	return out
}

// textCache is the same shape of cache as worldCache, for the prose file.
type textCache struct {
	mu    sync.Mutex
	held  map[string]map[string]featureText
	order []string
}

func newTextCache() *textCache {
	return &textCache{held: map[string]map[string]featureText{}}
}

func (c *textCache) get(key string) (map[string]featureText, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[key]
	return held, ok
}

func (c *textCache) put(key string, held map[string]featureText) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, seen := c.held[key]; !seen {
		c.order = append(c.order, key)
		for len(c.order) > worldsHeld {
			delete(c.held, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.held[key] = held
}

// readEntry reads one archive entry whole.
func readEntry(volume hostenv.Volume, name string) ([]byte, error) {
	entry, size, err := volume.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer entry.Close()
	_ = size
	out, err := io.ReadAll(entry)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return out, nil
}

// sortedIDs is the order a set of collection ids is written in everywhere it
// is written: as strings, which is how the session record and the state
// island both spell it (docs/app.md §6).
func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
