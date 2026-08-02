package enrich

import (
	"encoding/json"
	"math"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// Volume is one reading of one volume, as the enrich lane sees it: the subject,
// the grounds within it, and what stands on each.
//
// It is the lane's own model, not the generate lane's document and not a
// bundle's payload, because the enrich lane may import neither the pipeline half
// that writes documents nor be imported by it. What travels between them is
// data: a caller that holds both -- the one binary's enrich subcommand -- adapts
// a document into this and this back into a document. See docs/enrich.md, "the
// seam".
type Volume struct {
	Slug  string
	Title string
	// Source is where this reading came from. It is the only place a source is
	// named, and it survives into a bundle only through a ledger.
	Source Source
	Worlds []World
	// Icons is the artwork the volume's collections name, by key.
	Icons []Icon
}

// Source is one capture source, in the two spellings a ledger and a card need.
type Source struct {
	Name  string
	Label string
}

// World is one ground within a volume.
type World struct {
	Slug  string
	Title string
	// ID is the world's numeric identity where its source had one, zero
	// otherwise. Nothing here derives from it; it is carried so a round trip
	// through this model gives back the document it came from.
	ID     int64
	Center Position
	// Capture and CapturedAt are the archived bytes this ground was read from.
	// They travel because a world contributed whole by one reading has to be
	// able to say where it came from when the volume is composed, exactly as a
	// world that was there all along does.
	Capture    Capture
	CapturedAt string
	// Grid is the tile window this world's coordinates are projected against.
	// An enricher that measures distance needs it; one that only reads
	// attributes does not, and a volume assembled for such an enricher may
	// leave it zero.
	Grid        Grid
	Lenses      []Lens
	Collections []Collection
	Attrs       map[string]string
	// Ledger is the world's provenance: the origin account it was composed
	// with, and one account per contribution folded in since.
	Ledger []Account
}

// Capture names the archived bytes a world was read from: provenance, and a
// determinism receipt.
type Capture struct {
	Kind        string
	ID          int64
	Locator     string
	ContentHash string
}

// Grid is the tile window a world is cut from, and the world square it lands
// in. It is what turns the volume's own degrees into the world pixels every
// radius in this lane is measured in.
type Grid struct {
	SourceZoom int
	FirstTile  int
	TileSize   int
	Size       int
}

// Ready reports whether a grid can project. A zero grid cannot, and an enricher
// that needs one says so rather than measuring against nothing.
func (g Grid) Ready() bool { return g.TileSize > 0 && g.Size > 0 }

// ProjectX and ProjectY mirror the reader's own projection: the volume's
// coordinates are spherical Mercator against its tile window, whether that
// window pictures a real planet or a game's artwork.
func (g Grid) ProjectX(lng float64) float64 {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	xTile := ((lng + 180) / 360) * worldTiles
	return (xTile - float64(g.FirstTile)) * float64(g.TileSize)
}

func (g Grid) ProjectY(lat float64) float64 {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	yTile := (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2 * worldTiles
	return (yTile - float64(g.FirstTile)) * float64(g.TileSize)
}

// UnprojectLng and UnprojectLat send world pixels back to the volume's own
// degrees, which is how a donor feature lands on the serving ground.
func (g Grid) UnprojectLng(x float64) float64 {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	xTile := x/float64(g.TileSize) + float64(g.FirstTile)
	return (xTile/worldTiles)*360 - 180
}

func (g Grid) UnprojectLat(y float64) float64 {
	worldTiles := math.Pow(2, float64(g.SourceZoom))
	yTile := y/float64(g.TileSize) + float64(g.FirstTile)
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}

// Position is a point in the volume's own projection.
type Position struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Lens is one picture of a world.
type Lens struct {
	Name string `json:"name"`
	// TileSet is the key the picture's pyramid is found under.
	TileSet string `json:"tileSet"`
	// Stamp is the pyramid's derivation stamp, where whatever derived it said
	// so. It is carried through a contribution so a lens attached by the enrich
	// lane says what it was made from, exactly as one composed from a source's
	// own tile set does.
	Stamp string `json:"stamp,omitempty"`
	// AlignedWith names the tile set this picture was resampled into, where the
	// picture is somebody else's raster warped onto this ground.
	AlignedWith string `json:"alignedWith,omitempty"`
}

// Collection is an ordered group of features of one kind.
type Collection struct {
	ID          int64             `json:"id"`
	Key         string            `json:"key,omitempty"`
	Title       string            `json:"title"`
	Group       string            `json:"group,omitempty"`
	Kind        string            `json:"kind"`
	Icon        string            `json:"icon,omitempty"`
	IconAsset   string            `json:"iconAsset,omitempty"`
	IconPicture bool              `json:"iconPicture,omitempty"`
	Color       string            `json:"color,omitempty"`
	IconColor   string            `json:"iconColor,omitempty"`
	Visible     bool              `json:"visible"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Features    []Feature         `json:"features,omitempty"`
}

// Feature is one thing on a world, whatever its dimensionality.
type Feature struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle,omitempty"`
	Description string            `json:"description,omitempty"`
	At          *Position         `json:"at,omitempty"`
	Center      *Position         `json:"center,omitempty"`
	Geometry    []Geometry        `json:"geometry,omitempty"`
	Member      int64             `json:"member,omitempty"`
	Parent      int64             `json:"parent,omitempty"`
	Links       []Link            `json:"links,omitempty"`
	Attrs       map[string]string `json:"attrs,omitempty"`
}

// Geometry is one GeoJSON part of a shape feature. Coordinates are carried
// opaquely: nothing in this lane rewrites them, so a source's numbers reach the
// payload unrounded.
type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// Link is a cross-reference to another feature of the same world.
type Link struct {
	Title   string `json:"title"`
	Feature int64  `json:"feature"`
}

// Icon is one piece of collection artwork, carried whole.
type Icon struct {
	Key  string `json:"key"`
	File string `json:"file"`
	Data []byte `json:"data"`
}

// The geometry kinds a collection may declare, spelled here so this lane does
// not reach into another's constants for them.
const (
	KindPoint = semconv.GeometryPoint
	KindPath  = semconv.GeometryPath
	KindArea  = semconv.GeometryArea
)

// Clone is a deep copy. An enricher that wants to try something folds it into
// a clone and hands back operations, so the volume a caller passed in is never
// half-changed by an enricher that then failed.
func (v *Volume) Clone() *Volume {
	out := &Volume{Slug: v.Slug, Title: v.Title, Source: v.Source}
	out.Worlds = make([]World, 0, len(v.Worlds))
	for index := range v.Worlds {
		out.Worlds = append(out.Worlds, v.Worlds[index].Clone())
	}
	out.Icons = make([]Icon, 0, len(v.Icons))
	for _, icon := range v.Icons {
		out.Icons = append(out.Icons, Icon{Key: icon.Key, File: icon.File, Data: cloneBytes(icon.Data)})
	}
	return out
}

// Clone is a deep copy of one world.
func (w *World) Clone() World {
	out := *w
	out.Lenses = append([]Lens(nil), w.Lenses...)
	out.Attrs = cloneAttrs(w.Attrs)
	out.Ledger = append([]Account(nil), w.Ledger...)
	out.Collections = make([]Collection, 0, len(w.Collections))
	for index := range w.Collections {
		out.Collections = append(out.Collections, w.Collections[index].Clone())
	}
	return out
}

// Clone is a deep copy of one collection.
func (c *Collection) Clone() Collection {
	out := *c
	out.Attrs = cloneAttrs(c.Attrs)
	out.Features = make([]Feature, 0, len(c.Features))
	for index := range c.Features {
		out.Features = append(out.Features, c.Features[index].Clone())
	}
	return out
}

// Clone is a deep copy of one feature.
func (f *Feature) Clone() Feature {
	out := *f
	out.Attrs = cloneAttrs(f.Attrs)
	if f.At != nil {
		at := *f.At
		out.At = &at
	}
	if f.Center != nil {
		center := *f.Center
		out.Center = &center
	}
	out.Links = append([]Link(nil), f.Links...)
	out.Geometry = make([]Geometry, 0, len(f.Geometry))
	for _, geometry := range f.Geometry {
		out.Geometry = append(out.Geometry, Geometry{
			Type:        geometry.Type,
			Coordinates: append(json.RawMessage(nil), geometry.Coordinates...),
		})
	}
	if len(out.Geometry) == 0 {
		out.Geometry = nil
	}
	return out
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return out
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}
	return append([]byte(nil), data...)
}

// NewestCapture is the newest capture time across a volume's worlds: what a
// build of it is versioned by, and what decides which of two readings serves.
func (v *Volume) NewestCapture() string {
	newest := ""
	for _, world := range v.Worlds {
		if world.CapturedAt > newest {
			newest = world.CapturedAt
		}
	}
	return newest
}

// World finds a world by slug.
func (v *Volume) World(slug string) *World {
	for index := range v.Worlds {
		if v.Worlds[index].Slug == slug {
			return &v.Worlds[index]
		}
	}
	return nil
}

// Collection finds a collection by its numeric identity.
func (w *World) Collection(id int64) *Collection {
	for index := range w.Collections {
		if w.Collections[index].ID == id {
			return &w.Collections[index]
		}
	}
	return nil
}

// Feature finds a feature by id, anywhere in the world. Identifiers share one
// space per world, so this is unambiguous.
func (w *World) Feature(id int64) (*Collection, *Feature) {
	for index := range w.Collections {
		collection := &w.Collections[index]
		for at := range collection.Features {
			if collection.Features[at].ID == id {
				return collection, &collection.Features[at]
			}
		}
	}
	return nil, nil
}

// MergeIdentity is the name a collection meets other sources under: the
// declared merge identity its payload carries, then the key its source gave it,
// then its artwork key. A collection with none of the three meets nothing,
// which is the honest answer rather than a guessed pairing.
func MergeIdentity(c Collection) string {
	if key := c.Attrs[semconv.KeyCollectionKey]; key != "" {
		return key
	}
	if c.Key != "" {
		return c.Key
	}
	return c.Icon
}

// Serving picks the reading a registry would serve: the newest capture wins,
// and among equal captures the earlier reading in the given order stands. It is
// pure and total, so two runs over the same readings always agree on which one
// the others are folded into.
func Serving(volumes []*Volume) int {
	winner := -1
	for index, volume := range volumes {
		if winner < 0 || volume.NewestCapture() > volumes[winner].NewestCapture() {
			winner = index
		}
	}
	return winner
}

// Positions walks a geometry's coordinates and yields every position in it, as
// longitude and latitude. It is the one place a geometry's shape is looked
// into, and it looks only for pairs of numbers, so a MultiPolygon and a
// LineString are read by the same code.
func (g Geometry) Positions() [][2]float64 {
	var value any
	if err := json.Unmarshal(g.Coordinates, &value); err != nil {
		return nil
	}
	var out [][2]float64
	var walk func(node any)
	walk = func(node any) {
		list, ok := node.([]any)
		if !ok {
			return
		}
		if len(list) >= 2 {
			lng, lngOK := list[0].(float64)
			lat, latOK := list[1].(float64)
			if lngOK && latOK {
				out = append(out, [2]float64{lng, lat})
				return
			}
		}
		for _, child := range list {
			walk(child)
		}
	}
	walk(value)
	return out
}

// Rings reads a geometry as polygon rings: a list of polygons, each a list of
// rings, each a list of positions. A geometry that is not made of rings reads
// as none, which is what a caller asking about ground wants to hear.
func (g Geometry) Rings() [][][][2]float64 {
	if g.Type != "Polygon" && g.Type != "MultiPolygon" {
		return nil
	}
	var value any
	if err := json.Unmarshal(g.Coordinates, &value); err != nil {
		return nil
	}
	if g.Type == "Polygon" {
		polygon := readRings(value)
		if polygon == nil {
			return nil
		}
		return [][][][2]float64{polygon}
	}
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out [][][][2]float64
	for _, polygon := range list {
		if rings := readRings(polygon); rings != nil {
			out = append(out, rings)
		}
	}
	return out
}

// Lines reads a geometry as lines. A geometry that is not made of lines reads
// as none.
func (g Geometry) Lines() [][][2]float64 {
	if g.Type != "LineString" && g.Type != "MultiLineString" {
		return nil
	}
	var value any
	if err := json.Unmarshal(g.Coordinates, &value); err != nil {
		return nil
	}
	if g.Type == "LineString" {
		line := readPositions(value)
		if line == nil {
			return nil
		}
		return [][][2]float64{line}
	}
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out [][][2]float64
	for _, line := range list {
		if positions := readPositions(line); positions != nil {
			out = append(out, positions)
		}
	}
	return out
}

func readRings(node any) [][][2]float64 {
	list, ok := node.([]any)
	if !ok {
		return nil
	}
	var out [][][2]float64
	for _, ring := range list {
		if positions := readPositions(ring); positions != nil {
			out = append(out, positions)
		}
	}
	return out
}

func readPositions(node any) [][2]float64 {
	list, ok := node.([]any)
	if !ok {
		return nil
	}
	out := make([][2]float64, 0, len(list))
	for _, position := range list {
		pair, ok := position.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		lng, lngOK := pair[0].(float64)
		lat, latOK := pair[1].(float64)
		if lngOK && latOK {
			out = append(out, [2]float64{lng, lat})
		}
	}
	return out
}
