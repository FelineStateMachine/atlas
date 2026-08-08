package app

import (
	"math"
	"sort"
	"sync"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// payloadLens is the presentation-ready view of one native raster pyramid.
type payloadLens struct {
	Name        string                     `json:"name"`
	Tiles       string                     `json:"tiles"`
	MinZoom     int                        `json:"minZoom"`
	MaxZoom     int                        `json:"maxZoom"`
	FullZoom    int                        `json:"fullZoom"`
	SourceZoom  int                        `json:"sourceZoom"`
	Formats     []string                   `json:"formats"`
	Interpolate bool                       `json:"interpolate"`
	Background  string                     `json:"background,omitempty"`
	Shard       int                        `json:"shard,omitempty"`
	Coverage    map[string]payloadCoverage `json:"coverage,omitempty"`
	// Bounds is the raster window the pyramid fills; Surface is the ground
	// that window pictures. They differ on a split sheet, where the window
	// was grown to take in a title drawn beside the map, and anything that
	// divides the world measures the ground (docs/render-seam.md §6.1).
	Bounds  *cells.Rect `json:"bounds"`
	Surface *cells.Rect `json:"surface"`
}

type payloadCoverage struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Bits string `json:"bits"`
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
	Feature    *vnext.Feature
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
	Feature    *vnext.Feature
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

func (g tileGrid) unproject(x, y float64) (lat, lng float64) {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	xTile := x/float64(g.TileSize) + float64(g.FirstTile)
	yTile := y/float64(g.TileSize) + float64(g.FirstTile)
	lng = xTile/worldTiles*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
	return lat, lng
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
	info := volume.Info()
	key := info.Slug + "@" + vnext.ShortStamp(info.Stamp) + "/" + slug
	if held, ok := a.worlds.get(key); ok {
		return held
	}
	semantic, err := a.semanticVolume(volume)
	if err != nil {
		return nil
	}
	world, held := semanticWorld(semantic, slug)
	if !held {
		return nil
	}
	model, err := buildVNextWorld(world, semantic.Assets)
	if err != nil {
		return nil
	}
	a.worlds.put(key, model)
	return model
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

// sortedIDs is the order a set of collection ids is written in everywhere it
// is written: as strings, which is how the session record and the state
// island both spell it (docs/app.md §6).
func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
