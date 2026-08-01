// Package mgdoc is the MapGenie-shaped document the pipeline reads, spelled
// as Go structs for the translators that emit it. tools/tiles and
// tools/generate decode this shape from every snapshot; a capture from
// another source becomes one of these on the way in, and this package is
// where the translators agree on the fields, the world they project into,
// and the way stable names become the numeric identifiers the pipeline
// expects.
package mgdoc

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// The world every translated map lives in: a 32-tile square, 8192 pixels
// across, whose tile pyramid starts at a single tile. Pins are stored as
// latitude and longitude and projected by the viewer, so a marker's linear
// position on a source's image becomes the synthetic coordinate that projects
// back onto that exact pixel.
const (
	SourceZoom = 5
	TileSize   = 256
	WorldSize  = 8192
)

// Only the fields tools/tiles and tools/generate actually read, spelled with
// the snake_case tags their own decoders name. Attrs carries the semantic
// conventions a translator declares -- registered atlas.* keys -- under a
// field no real MapGenie capture spells, so a snapshot passing through
// untranslated simply says nothing and the generator speaks for it.
type Map struct {
	ID               int64             `json:"id"`
	Title            string            `json:"title"`
	Slug             string            `json:"slug"`
	InitialLatitude  float64           `json:"initial_latitude"`
	InitialLongitude float64           `json:"initial_longitude"`
	Config           Config            `json:"config"`
	Game             Game              `json:"game"`
	Groups           []Group           `json:"groups"`
	Regions          []struct{}        `json:"regions"`
	Attrs            map[string]string `json:"atlas_attrs,omitempty"`
}

type Game struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type Config struct {
	TileSets []TileSet `json:"tile_sets"`
}

type TileSet struct {
	Name      string           `json:"name"`
	Path      string           `json:"path"`
	MinZoom   int              `json:"min_zoom"`
	MaxZoom   int              `json:"max_zoom"`
	Extension string           `json:"extension"`
	Bounds    map[string]Bound `json:"bounds"`
}

type Bound struct {
	X Range `json:"x"`
	Y Range `json:"y"`
}

type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Group struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	Categories []Category `json:"categories"`
}

type Category struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Icon        string            `json:"icon"`
	DisplayType string            `json:"display_type"`
	Visible     bool              `json:"visible"`
	Locations   []Location        `json:"locations"`
	Attrs       map[string]string `json:"atlas_attrs,omitempty"`
}

type Location struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Latitude    float64           `json:"latitude"`
	Longitude   float64           `json:"longitude"`
	Attrs       map[string]string `json:"atlas_attrs,omitempty"`
}

// Synthetic coordinates invert the viewer's projection, so a pin's linear
// position on a source's image, run through the same spherical Mercator every
// other map uses, lands on the same pixel it was captured at. The world here
// is the map's own window -- a 32-tile square starting at tile zero -- which
// the bundle carries as this map's grid.
func SyntheticLongitude(x float64) float64 {
	worldTiles := math.Pow(2, SourceZoom)
	xTile := x / TileSize
	return (xTile/worldTiles)*360 - 180
}

func SyntheticLatitude(y float64) float64 {
	worldTiles := math.Pow(2, SourceZoom)
	yTile := y / TileSize
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}

// ProjectX and ProjectY are the viewer's projection, parameterised by the
// window a map is cut from: the inverse of the synthetic coordinates above
// for the shared window, and of MapGenie's own for every other. They exist
// here so anything comparing two sources' pins can land both in pixels
// without carrying its own copy of the formula.
func ProjectX(longitude float64, sourceZoom, firstTile int) float64 {
	worldTiles := math.Pow(2, float64(sourceZoom))
	xTile := ((longitude + 180) / 360) * worldTiles
	return (xTile - float64(firstTile)) * TileSize
}

func ProjectY(latitude float64, sourceZoom, firstTile int) float64 {
	worldTiles := math.Pow(2, float64(sourceZoom))
	yTile := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * worldTiles
	return (yTile - float64(firstTile)) * TileSize
}

// IDSpace hands out the numeric identifiers the pipeline expects, derived
// from the stable names a source publishes, so the same capture always
// numbers itself the same way. Identifiers stay within a positive int31 --
// the packed location format and the viewer both read them as signed 32-bit
// -- and zero is avoided because the viewer reads zero as absence. A
// collision between two names is vanishingly unlikely at this scale and
// loudly fatal rather than silently merging two pins.
type IDSpace struct {
	seen map[int64]string
}

func NewIDSpace() *IDSpace {
	return &IDSpace{seen: make(map[int64]string)}
}

func (s *IDSpace) Claim(seed string) (int64, error) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	id := int64(hash.Sum32() & 0x7fffffff)
	if id == 0 {
		id = 1
	}
	if holder, taken := s.seen[id]; taken {
		return 0, fmt.Errorf("id collision: %q and %q both hash to %d", holder, seed, id)
	}
	s.seen[id] = seed
	return id, nil
}

// SpellOut turns a slug into a title, which is all the naming some sources
// give a thing.
func SpellOut(slug string) string {
	words := strings.Split(slug, "-")
	for index, word := range words {
		if word != "" {
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}
