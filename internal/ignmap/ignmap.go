// Package ignmap reads a captured IGN wikimap and speaks MapGenie for it.
//
// The crawler stores what IGN publishes -- a map's configuration, its marker
// types, and every marker -- as one canonical document, and nothing downstream
// ever sees that document directly. Translate turns it into the MapGenie-shaped
// JSON that tools/tiles and tools/generate already decode, so a second source
// flows through the pipeline without the pipeline learning a second schema.
//
// Translation happens when a capture is read, not when it is taken. The archive
// keeps IGN's data as IGN published it, and every decision about what a bundle
// keeps or leaves -- dropping empty categories, holding back wiki links the
// offline app cannot follow -- lives here, where changing it re-applies to
// every capture already on disk.
package ignmap

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind names IGN captures in a map's snapshot index, alongside the "map"
// records MapGenie crawls write.
const Kind = "ign-map"

// Source is the value a well-formed capture carries in its source field.
const Source = "ign-wikimaps"

// Capture is the canonical document the crawler stores. Its fields are structs
// and slices only, marshaled in declaration order, so the same data always
// produces the same bytes and an unchanged re-crawl hashes to the capture
// already archived. Anything volatile on the page -- build ids, trace headers,
// timestamps -- stays out; when the capture was taken is the snapshot index's
// business.
type Capture struct {
	Source     string    `json:"source"`
	ObjectSlug string    `json:"objectSlug"`
	MapSlug    string    `json:"mapSlug"`
	GameTitle  string    `json:"gameTitle"`
	MapTitle   string    `json:"mapTitle"`
	Map        MapConfig `json:"map"`
	Types      []Type    `json:"types"`
	Markers    []Marker  `json:"markers"`
}

// MapConfig is the slice of IGN's page config a bundle needs. Width and height
// are normalized to the map image: the taller dimension spans 1, so a marker's
// coordinates and the tile pyramid describe the same square world.
type MapConfig struct {
	Width           float64 `json:"width"`
	Height          float64 `json:"height"`
	MinZoom         int     `json:"minZoom"`
	MaxZoom         int     `json:"maxZoom"`
	InitialLat      float64 `json:"initialLat"`
	InitialLng      float64 `json:"initialLng"`
	BackgroundColor string  `json:"backgroundColor"`
	Tileset         string  `json:"tileset"`
}

type Type struct {
	TypeSlug       string `json:"typeSlug"`
	TypeName       string `json:"typeName"`
	ParentTypeSlug string `json:"parentTypeSlug,omitempty"`
	IconURL        string `json:"iconUrl,omitempty"`
	IconWidth      int    `json:"iconWidth,omitempty"`
	IconHeight     int    `json:"iconHeight,omitempty"`
}

// Marker keeps every field IGN serves for a pin, including the ones a bundle
// holds back today. WikiPage names an article on IGN's wiki -- the offline app
// cannot follow it, so it stays archived here until something can be made of
// it; the raw messages pass through whatever shape IGN used, which their
// endpoint has not settled.
type Marker struct {
	ID              string          `json:"id"`
	Lat             float64         `json:"lat"`
	Lng             float64         `json:"lng"`
	MarkerName      string          `json:"markerName"`
	MarkerSlug      string          `json:"markerSlug"`
	TypeSlug        string          `json:"typeSlug"`
	WikiPage        string          `json:"wikiPage,omitempty"`
	IconSlug        json.RawMessage `json:"iconSlug,omitempty"`
	RegionID        json.RawMessage `json:"regionId,omitempty"`
	ChecklistTaskID json.RawMessage `json:"checklistTaskId,omitempty"`
}

// Normalize puts the capture into its canonical order. The crawler calls this
// before marshaling so that IGN returning the same markers in a different
// order does not masquerade as a new version.
func (c *Capture) Normalize() {
	sort.Slice(c.Types, func(a, b int) bool { return c.Types[a].TypeSlug < c.Types[b].TypeSlug })
	sort.Slice(c.Markers, func(a, b int) bool { return c.Markers[a].ID < c.Markers[b].ID })
}

// The world every IGN map is translated into: a 32-tile square, 8192 pixels
// across, whose tile pyramid starts at a single tile. Pins are stored as
// latitude and longitude and projected by the viewer, so a marker's linear
// position on the image becomes the synthetic coordinate that projects back
// onto that exact pixel.
const (
	sourceZoom = 5
	tileSize   = 256
	worldSize  = 8192
)

// MaybeTranslate hands MapGenie snapshots through untouched and translates IGN
// captures, so the tools that read snapshots stay one call away from either.
func MaybeTranslate(kind string, doc []byte) ([]byte, error) {
	if kind != Kind {
		return doc, nil
	}
	return Translate(doc)
}

// Translate turns a capture into the MapGenie-shaped document the pipeline
// reads. The output is deterministic for a given capture, so re-generating an
// unchanged archive reproduces byte-identical payloads and the bundle stamp
// stands still.
func Translate(doc []byte) ([]byte, error) {
	var capture Capture
	if err := json.Unmarshal(doc, &capture); err != nil {
		return nil, fmt.Errorf("decode ign capture: %w", err)
	}
	if capture.Source != Source {
		return nil, fmt.Errorf("capture source is %q, not %q", capture.Source, Source)
	}
	if capture.ObjectSlug == "" || capture.MapSlug == "" {
		return nil, fmt.Errorf("capture names no map")
	}
	if capture.Map.Width <= 0 || capture.Map.Height <= 0 {
		return nil, fmt.Errorf("capture map has no size")
	}
	capture.Normalize()

	ids := newIDSpace()
	scope := capture.ObjectSlug + "/" + capture.MapSlug
	mapID, err := ids.claim("ign:map:" + scope)
	if err != nil {
		return nil, err
	}
	gameID, err := ids.claim("ign:game:" + capture.ObjectSlug)
	if err != nil {
		return nil, err
	}

	groups, err := buildGroups(&capture, ids, scope)
	if err != nil {
		return nil, err
	}

	out := mgMap{
		ID:               mapID,
		Title:            mapTitle(&capture),
		Slug:             capture.MapSlug,
		InitialLatitude:  syntheticLatitude(-capture.Map.InitialLat * worldSize),
		InitialLongitude: syntheticLongitude(capture.Map.InitialLng * worldSize),
		Config:           mgConfig{TileSets: []mgTileSet{tileSet(&capture, scope)}},
		Game: mgGame{
			ID: gameID,
			// The game keeps its plain slug, so captures of the same game from
			// different sources build bundles that answer for one library entry
			// and the newest capture is the one the reader sees.
			Title: capture.GameTitle,
			Slug:  capture.ObjectSlug,
		},
		Groups:  groups,
		Regions: []struct{}{},
	}
	return json.Marshal(out)
}

// mapTitle prefers the name IGN gives the map and falls back to its slug,
// spelled out, when the page offers none.
func mapTitle(capture *Capture) string {
	if capture.MapTitle != "" {
		return capture.MapTitle
	}
	return spellOut(capture.MapSlug)
}

// tileSet describes the pyramid the crawler captured in the shape tools/tiles
// expects. Bounds are declared for every level: a level without them would be
// measured against the MapGenie window this map does not sit in.
func tileSet(capture *Capture, scope string) mgTileSet {
	set := mgTileSet{
		Name:      "Default",
		Path:      scope,
		MinZoom:   0,
		MaxZoom:   capture.Map.MaxZoom,
		Extension: tileExtension(capture.Map.Tileset),
		Bounds:    make(map[string]mgBound, capture.Map.MaxZoom+1),
	}
	for zoom := 0; zoom <= capture.Map.MaxZoom; zoom++ {
		across := float64(int(1) << zoom)
		set.Bounds[strconv.Itoa(zoom)] = mgBound{
			X: mgRange{Min: 0, Max: lastTile(capture.Map.Width, across)},
			Y: mgRange{Min: 0, Max: lastTile(capture.Map.Height, across)},
		}
	}
	return set
}

func lastTile(extent, across float64) int {
	last := int(math.Ceil(extent*across)) - 1
	if last < 0 {
		return 0
	}
	return last
}

func tileExtension(template string) string {
	if dot := strings.LastIndex(template, "."); dot >= 0 && dot < len(template)-1 {
		return strings.ToLower(template[dot+1:])
	}
	return "jpg"
}

// buildGroups arranges IGN's flat marker types the way the pipeline expects
// them: categories inside groups. Types naming a parent gather under it, the
// rest share one group. A type no marker uses is left out -- an empty category
// would only dim the legend -- but it stays in the capture, so a marker
// appearing under it later revives the category without a policy change.
func buildGroups(capture *Capture, ids *idSpace, scope string) ([]mgGroup, error) {
	markersByType := make(map[string][]Marker, len(capture.Types))
	known := make(map[string]bool, len(capture.Types))
	for _, t := range capture.Types {
		if known[t.TypeSlug] {
			return nil, fmt.Errorf("type %q appears twice", t.TypeSlug)
		}
		known[t.TypeSlug] = true
	}
	for _, marker := range capture.Markers {
		if !known[marker.TypeSlug] {
			return nil, fmt.Errorf("marker %s uses undeclared type %q", marker.ID, marker.TypeSlug)
		}
		markersByType[marker.TypeSlug] = append(markersByType[marker.TypeSlug], marker)
	}

	grouped := make(map[string][]mgCategory)
	var order []string
	for _, t := range capture.Types {
		markers := markersByType[t.TypeSlug]
		if len(markers) == 0 {
			continue
		}
		categoryID, err := ids.claim("ign:type:" + scope + ":" + t.TypeSlug)
		if err != nil {
			return nil, err
		}
		category := mgCategory{
			ID:          categoryID,
			Title:       t.TypeName,
			Icon:        t.TypeSlug,
			DisplayType: "markers",
			Visible:     true,
			Locations:   make([]mgLocation, 0, len(markers)),
		}
		for _, marker := range markers {
			locationID, err := ids.claim("ign:marker:" + marker.ID)
			if err != nil {
				return nil, err
			}
			category.Locations = append(category.Locations, mgLocation{
				ID:        locationID,
				Title:     marker.MarkerName,
				Latitude:  syntheticLatitude(-marker.Lat * worldSize),
				Longitude: syntheticLongitude(marker.Lng * worldSize),
			})
		}
		key := t.ParentTypeSlug
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], category)
	}

	groups := make([]mgGroup, 0, len(order))
	for _, key := range order {
		title := "Markers"
		seed := "ign:group:" + scope
		if key != "" {
			title = spellOut(key)
			seed += ":" + key
		}
		groupID, err := ids.claim(seed)
		if err != nil {
			return nil, err
		}
		groups = append(groups, mgGroup{ID: groupID, Title: title, Categories: grouped[key]})
	}
	return groups, nil
}

// spellOut turns a slug into a title, which is all the naming a group or an
// unnamed map gets from IGN.
func spellOut(slug string) string {
	words := strings.Split(slug, "-")
	for index, word := range words {
		if word != "" {
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// Synthetic coordinates invert the viewer's projection, so a marker's linear
// position on IGN's image, run through the same spherical Mercator every other
// map uses, lands on the same pixel it was captured at. The world here is the
// map's own window -- a 32-tile square starting at tile zero -- which the
// bundle carries as this map's grid.
func syntheticLongitude(x float64) float64 {
	worldTiles := math.Pow(2, sourceZoom)
	xTile := x / tileSize
	return (xTile/worldTiles)*360 - 180
}

func syntheticLatitude(y float64) float64 {
	worldTiles := math.Pow(2, sourceZoom)
	yTile := y / tileSize
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}

// idSpace hands out the numeric identifiers the pipeline expects, derived from
// the stable names IGN publishes, so the same capture always numbers itself the
// same way. Identifiers stay within a positive int31 -- the packed location
// format and the viewer both read them as signed 32-bit -- and zero is avoided
// because the viewer reads zero as absence. A collision between two names is
// vanishingly unlikely at this scale and loudly fatal rather than silently
// merging two pins.
type idSpace struct {
	seen map[int64]string
}

func newIDSpace() *idSpace {
	return &idSpace{seen: make(map[int64]string)}
}

func (s *idSpace) claim(seed string) (int64, error) {
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

// The MapGenie shapes Translate emits: only the fields tools/tiles and
// tools/generate actually read, spelled with the snake_case tags their own
// decoders name.
type mgMap struct {
	ID               int64      `json:"id"`
	Title            string     `json:"title"`
	Slug             string     `json:"slug"`
	InitialLatitude  float64    `json:"initial_latitude"`
	InitialLongitude float64    `json:"initial_longitude"`
	Config           mgConfig   `json:"config"`
	Game             mgGame     `json:"game"`
	Groups           []mgGroup  `json:"groups"`
	Regions          []struct{} `json:"regions"`
}

type mgGame struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type mgConfig struct {
	TileSets []mgTileSet `json:"tile_sets"`
}

type mgTileSet struct {
	Name      string             `json:"name"`
	Path      string             `json:"path"`
	MinZoom   int                `json:"min_zoom"`
	MaxZoom   int                `json:"max_zoom"`
	Extension string             `json:"extension"`
	Bounds    map[string]mgBound `json:"bounds"`
}

type mgBound struct {
	X mgRange `json:"x"`
	Y mgRange `json:"y"`
}

type mgRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type mgGroup struct {
	ID         int64        `json:"id"`
	Title      string       `json:"title"`
	Categories []mgCategory `json:"categories"`
}

type mgCategory struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	Icon        string       `json:"icon"`
	DisplayType string       `json:"display_type"`
	Visible     bool         `json:"visible"`
	Locations   []mgLocation `json:"locations"`
}

type mgLocation struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
