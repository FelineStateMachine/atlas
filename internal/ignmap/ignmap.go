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
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
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

// MaybeTranslate hands other sources' snapshots through untouched and
// translates IGN captures, so the tools that read snapshots stay one call
// away from either.
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

	ids := mgdoc.NewIDSpace()
	scope := capture.ObjectSlug + "/" + capture.MapSlug
	mapID, err := ids.Claim("ign:map:" + scope)
	if err != nil {
		return nil, err
	}
	gameID, err := ids.Claim("ign:game:" + capture.ObjectSlug)
	if err != nil {
		return nil, err
	}

	groups, err := buildGroups(&capture, ids, scope)
	if err != nil {
		return nil, err
	}

	out := mgdoc.Map{
		ID:               mapID,
		Title:            mapTitle(&capture),
		Slug:             capture.MapSlug,
		InitialLatitude:  mgdoc.SyntheticLatitude(-capture.Map.InitialLat * mgdoc.WorldSize),
		InitialLongitude: mgdoc.SyntheticLongitude(capture.Map.InitialLng * mgdoc.WorldSize),
		Config:           mgdoc.Config{TileSets: []mgdoc.TileSet{tileSet(&capture, scope)}},
		Game: mgdoc.Game{
			ID: gameID,
			// The game keeps its plain slug, so captures of the same game from
			// different sources build bundles that answer for one library entry
			// and the newest capture is the one the reader sees.
			Title: capture.GameTitle,
			Slug:  capture.ObjectSlug,
		},
		Groups:  groups,
		Regions: []mgdoc.Region{},
	}
	return json.Marshal(out)
}

// mapTitle prefers the name IGN gives the map and falls back to its slug,
// spelled out, when the page offers none.
func mapTitle(capture *Capture) string {
	if capture.MapTitle != "" {
		return capture.MapTitle
	}
	return mgdoc.SpellOut(capture.MapSlug)
}

// tileSet describes the pyramid the crawler captured in the shape tools/tiles
// expects. Bounds are declared for every level: a level without them would be
// measured against the MapGenie window this map does not sit in.
func tileSet(capture *Capture, scope string) mgdoc.TileSet {
	set := mgdoc.TileSet{
		// The name rides into the legend's layer picker -- and, when this
		// raster is resampled into a finer source's map, it names the
		// variant there too, so it says where the picture came from.
		Name:      "IGN Wiki",
		Path:      scope,
		MinZoom:   0,
		MaxZoom:   capture.Map.MaxZoom,
		Extension: TileExtension(capture.Map.Tileset),
		Bounds:    make(map[string]mgdoc.Bound, capture.Map.MaxZoom+1),
	}
	for zoom := 0; zoom <= capture.Map.MaxZoom; zoom++ {
		maxX, maxY := LevelExtent(capture.Map.Width, capture.Map.Height, zoom)
		set.Bounds[strconv.Itoa(zoom)] = mgdoc.Bound{
			X: mgdoc.Range{Min: 0, Max: maxX},
			Y: mgdoc.Range{Min: 0, Max: maxY},
		}
	}
	return set
}

// LevelExtent reports the last tile column and row holding any of the image at
// a zoom. The crawler asks for exactly these tiles, and Translate declares
// exactly these bounds, because they are the same call.
func LevelExtent(width, height float64, zoom int) (maxX, maxY int) {
	across := float64(int(1) << zoom)
	return lastTile(width, across), lastTile(height, across)
}

func lastTile(extent, across float64) int {
	last := int(math.Ceil(extent*across)) - 1
	if last < 0 {
		return 0
	}
	return last
}

// TileExtension reads the image format off IGN's tile URL template.
func TileExtension(template string) string {
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
func buildGroups(capture *Capture, ids *mgdoc.IDSpace, scope string) ([]mgdoc.Group, error) {
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

	grouped := make(map[string][]mgdoc.Category)
	var order []string
	for _, t := range capture.Types {
		markers := markersByType[t.TypeSlug]
		if len(markers) == 0 {
			continue
		}
		categoryID, err := ids.Claim("ign:type:" + scope + ":" + t.TypeSlug)
		if err != nil {
			return nil, err
		}
		category := mgdoc.Category{
			ID:          categoryID,
			Title:       t.TypeName,
			Icon:        t.TypeSlug,
			DisplayType: "markers",
			Visible:     true,
			Locations:   make([]mgdoc.Location, 0, len(markers)),
			Attrs:       map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
		}
		for _, marker := range markers {
			locationID, err := ids.Claim("ign:marker:" + marker.ID)
			if err != nil {
				return nil, err
			}
			category.Locations = append(category.Locations, mgdoc.Location{
				ID:        locationID,
				Title:     marker.MarkerName,
				Latitude:  mgdoc.SyntheticLatitude(-marker.Lat * mgdoc.WorldSize),
				Longitude: mgdoc.SyntheticLongitude(marker.Lng * mgdoc.WorldSize),
			})
		}
		key := t.ParentTypeSlug
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], category)
	}

	groups := make([]mgdoc.Group, 0, len(order))
	for _, key := range order {
		title := "Markers"
		seed := "ign:group:" + scope
		if key != "" {
			title = mgdoc.SpellOut(key)
			seed += ":" + key
		}
		groupID, err := ids.Claim(seed)
		if err != nil {
			return nil, err
		}
		groups = append(groups, mgdoc.Group{ID: groupID, Title: title, Categories: grouped[key]})
	}
	return groups, nil
}
