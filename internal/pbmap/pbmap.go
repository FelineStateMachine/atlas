// Package pbmap reads a captured Piggyback map and speaks MapGenie for it.
//
// Piggyback is the official guide house, and its maps carry what IGN's do not:
// prose. Pins arrive with names and descriptions, and both survive into the
// bundle. Like ignmap, this package translates at read time -- the archive
// keeps the capture as Piggyback served it, and every keep-or-leave decision
// lives here where changing it re-applies to captures already on disk.
//
// Piggyback draws its world in game coordinates on a Leaflet CRS.Simple map:
// a linear transformation squeezes them onto the unit tile at zoom zero. The
// capture records that transformation, and pins pass through it, then through
// the shared inverse Mercator, to land on the same pixels Piggyback draws
// them at.
package pbmap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// Kind names Piggyback captures in a map's snapshot index.
const Kind = "piggyback-map"

// Source is the value a well-formed capture carries in its source field.
const Source = "piggyback"

// Capture is the canonical document the crawler stores: the map's settings,
// the category tree, every pin, and the tile windows the crawler observed --
// Piggyback publishes no bounds, so the crawler's survey is the only account
// of where the pyramid is drawn. Fields marshal in declaration order and every
// list is sorted by Normalize, so unchanged data always hashes to the capture
// already archived.
type Capture struct {
	Source     string     `json:"source"`
	GameSlug   string     `json:"gameSlug"`
	MapSlug    string     `json:"mapSlug"`
	GameTitle  string     `json:"gameTitle"`
	MapTitle   string     `json:"mapTitle"`
	Map        MapConfig  `json:"map"`
	Labels     Labels     `json:"labels"`
	Categories []Category `json:"categories"`
	Pins       []Pin      `json:"pins"`
	Levels     []Level    `json:"levels"`
}

// MapConfig keeps the settings the page ships and the transformation the
// map's own scripts project through. MaxZoom is the deepest level actually
// captured; the premium ceiling rides along as a matter of record.
type MapConfig struct {
	TileServer     string    `json:"tileServer"`
	MinZoom        int       `json:"minZoom"`
	MaxZoom        int       `json:"maxZoom"`
	PremiumMaxZoom int       `json:"premiumMaxZoom,omitempty"`
	Transform      Transform `json:"transform"`
}

// Transform is Leaflet's affine pair: a point (x, y) in game coordinates
// draws at (a*x + b, c*y + d) on the unit tile at zoom zero.
type Transform struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
	C float64 `json:"c"`
	D float64 `json:"d"`
}

// Labels carries the display names the page's language bundle gives the
// category and type keys, which the data API never repeats.
type Labels struct {
	Categories []Label `json:"categories"`
	Types      []Label `json:"types"`
}

type Label struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type Category struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Position float64 `json:"position"`
	Types    []Type  `json:"types"`
}

type Type struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Position float64 `json:"position"`
}

// Pin keeps a pin the way Piggyback serves it, coordinates included as the
// strings they arrive in.
type Pin struct {
	ID          string `json:"id"`
	X           string `json:"x"`
	Y           string `json:"y"`
	CategoryKey string `json:"categoryKey"`
	TypeKey     string `json:"typeKey"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Level is one observed tile window: at Zoom, tiles exist from MinX,MinY
// through MaxX,MaxY inclusive.
type Level struct {
	Zoom int `json:"zoom"`
	MinX int `json:"minX"`
	MinY int `json:"minY"`
	MaxX int `json:"maxX"`
	MaxY int `json:"maxY"`
}

// Normalize puts the capture into its canonical order.
func (c *Capture) Normalize() {
	sort.Slice(c.Labels.Categories, func(a, b int) bool {
		return c.Labels.Categories[a].Key < c.Labels.Categories[b].Key
	})
	sort.Slice(c.Labels.Types, func(a, b int) bool {
		return c.Labels.Types[a].Key < c.Labels.Types[b].Key
	})
	sort.SliceStable(c.Categories, func(a, b int) bool {
		if c.Categories[a].Position != c.Categories[b].Position {
			return c.Categories[a].Position < c.Categories[b].Position
		}
		return c.Categories[a].Key < c.Categories[b].Key
	})
	for index := range c.Categories {
		types := c.Categories[index].Types
		sort.SliceStable(types, func(a, b int) bool {
			if types[a].Position != types[b].Position {
				return types[a].Position < types[b].Position
			}
			return types[a].Key < types[b].Key
		})
	}
	sort.Slice(c.Pins, func(a, b int) bool { return c.Pins[a].ID < c.Pins[b].ID })
	sort.Slice(c.Levels, func(a, b int) bool { return c.Levels[a].Zoom < c.Levels[b].Zoom })
}

// districtTypes are Piggyback's region name markers. They arrive filed under
// the user-state "favorites" category, which is nothing to build a legend
// from; they become floating text labels, which is what they are on
// Piggyback's own map.
var districtTypes = map[string]bool{
	"province":   true,
	"sub-region": true,
}

// MaybeTranslate hands other sources' snapshots through untouched and
// translates Piggyback captures.
func MaybeTranslate(kind string, doc []byte) ([]byte, error) {
	if kind != Kind {
		return doc, nil
	}
	return Translate(doc)
}

// Translate turns a capture into the MapGenie-shaped document the pipeline
// reads, deterministically for a given capture.
func Translate(doc []byte) ([]byte, error) {
	var capture Capture
	if err := json.Unmarshal(doc, &capture); err != nil {
		return nil, fmt.Errorf("decode piggyback capture: %w", err)
	}
	if capture.Source != Source {
		return nil, fmt.Errorf("capture source is %q, not %q", capture.Source, Source)
	}
	if capture.GameSlug == "" || capture.MapSlug == "" {
		return nil, fmt.Errorf("capture names no map")
	}
	if len(capture.Levels) == 0 {
		return nil, fmt.Errorf("capture observed no tile levels")
	}
	if capture.Map.Transform.A == 0 || capture.Map.Transform.C == 0 {
		return nil, fmt.Errorf("capture carries no usable transformation")
	}
	capture.Normalize()

	ids := mgdoc.NewIDSpace()
	scope := capture.GameSlug + "/" + capture.MapSlug
	mapID, err := ids.Claim("pb:map:" + scope)
	if err != nil {
		return nil, err
	}
	gameID, err := ids.Claim("pb:game:" + capture.GameSlug)
	if err != nil {
		return nil, err
	}

	set, err := tileSet(&capture)
	if err != nil {
		return nil, err
	}
	groups, err := buildGroups(&capture, ids, scope)
	if err != nil {
		return nil, err
	}

	centerX, centerY := deepestWindowCenter(&capture)
	out := mgdoc.Map{
		ID:               mapID,
		Title:            title(capture.MapTitle, capture.MapSlug),
		Slug:             capture.MapSlug,
		InitialLatitude:  mgdoc.SyntheticLatitude(centerY),
		InitialLongitude: mgdoc.SyntheticLongitude(centerX),
		Config:           mgdoc.Config{TileSets: []mgdoc.TileSet{set}},
		Game: mgdoc.Game{
			ID: gameID,
			// The plain slug, deliberately shared with every other source's
			// capture of the same game: their bundles answer for one library
			// entry, and the newest capture is the one the reader sees.
			Title: title(capture.GameTitle, capture.GameSlug),
			Slug:  capture.GameSlug,
		},
		Groups:  groups,
		Regions: []struct{}{},
	}
	return json.Marshal(out)
}

func title(given, slug string) string {
	if given != "" {
		return given
	}
	return mgdoc.SpellOut(slug)
}

// TileSetPath cuts the layer path out of the tile URL template: everything
// between "/tiles/" and the zoom placeholder, which is how tile records group
// into layers downstream.
func TileSetPath(template string) (string, error) {
	_, rest, found := strings.Cut(template, "/tiles/")
	if !found {
		return "", fmt.Errorf("tile template %q has no /tiles/ segment", template)
	}
	path, _, found := strings.Cut(rest, "/{z}")
	if !found || path == "" {
		return "", fmt.Errorf("tile template %q does not name a layer before its zoom", template)
	}
	return path, nil
}

// TileExtension reads the image format off the tile URL template.
func TileExtension(template string) string {
	if dot := strings.LastIndex(template, "."); dot >= 0 && dot < len(template)-1 {
		return strings.ToLower(template[dot+1:])
	}
	return "webp"
}

// tileSet declares the pyramid from the crawler's survey. Bounds are declared
// for every observed level: a level without them would be measured against
// the MapGenie window this map does not sit in.
func tileSet(capture *Capture) (mgdoc.TileSet, error) {
	path, err := TileSetPath(capture.Map.TileServer)
	if err != nil {
		return mgdoc.TileSet{}, err
	}
	deepest := capture.Levels[len(capture.Levels)-1]
	set := mgdoc.TileSet{
		Name:      "Default",
		Path:      path,
		MinZoom:   capture.Levels[0].Zoom,
		MaxZoom:   deepest.Zoom,
		Extension: TileExtension(capture.Map.TileServer),
		Bounds:    make(map[string]mgdoc.Bound, len(capture.Levels)),
	}
	for _, level := range capture.Levels {
		set.Bounds[strconv.Itoa(level.Zoom)] = mgdoc.Bound{
			X: mgdoc.Range{Min: level.MinX, Max: level.MaxX},
			Y: mgdoc.Range{Min: level.MinY, Max: level.MaxY},
		}
	}
	return set, nil
}

// worldPixel runs a pin's game coordinates through the map's transformation
// onto the zoom-zero tile, then scales to the shared 8192-pixel world.
func worldPixel(transform Transform, x, y float64) (float64, float64) {
	const zoomZeroSpan = 256
	scale := float64(mgdoc.WorldSize) / zoomZeroSpan
	return (transform.A*x + transform.B) * scale, (transform.C*y + transform.D) * scale
}

func deepestWindowCenter(capture *Capture) (float64, float64) {
	deepest := capture.Levels[len(capture.Levels)-1]
	span := float64(mgdoc.WorldSize) / float64(int(1)<<deepest.Zoom)
	x := (float64(deepest.MinX) + float64(deepest.MaxX+1)) / 2 * span
	y := (float64(deepest.MinY) + float64(deepest.MaxY+1)) / 2 * span
	return x, y
}

// buildGroups turns Piggyback's category tree into the pipeline's groups. A
// declared category becomes a group, its types become categories, and a type
// no pin uses is left out. District name pins -- filed under the user-state
// favorites category -- gather instead into their own group as floating text
// labels. Anything else a pin references without a declaration is an error:
// better a loud crawl than a pin silently dropped.
func buildGroups(capture *Capture, ids *mgdoc.IDSpace, scope string) ([]mgdoc.Group, error) {
	categoryLabels := labelIndex(capture.Labels.Categories)
	typeLabels := labelIndex(capture.Labels.Types)

	pinsByType := make(map[string][]Pin)
	for _, pin := range capture.Pins {
		pinsByType[pin.TypeKey] = append(pinsByType[pin.TypeKey], pin)
	}

	declared := make(map[string]bool)
	var groups []mgdoc.Group
	for _, category := range capture.Categories {
		if category.Key == "favorites" {
			// The reader's own favorites, not the map's data. Its stowaway
			// district pins are picked up below.
			continue
		}
		var categories []mgdoc.Category
		for _, t := range category.Types {
			declared[t.Key] = true
			built, ok, err := buildCategory(capture, ids, scope, category.Key, t.Key,
				label(typeLabels, t.Key), "markers", pinsByType)
			if err != nil {
				return nil, err
			}
			if ok {
				categories = append(categories, built)
			}
		}
		if len(categories) == 0 {
			continue
		}
		groupID, err := ids.Claim("pb:category:" + scope + ":" + category.Key)
		if err != nil {
			return nil, err
		}
		groups = append(groups, mgdoc.Group{
			ID:         groupID,
			Title:      label(categoryLabels, category.Key),
			Categories: categories,
		})
	}

	// District names, and a loud failure for anything else undeclared.
	var districts []mgdoc.Category
	for _, key := range sortedKeys(pinsByType) {
		if declared[key] {
			continue
		}
		if !districtTypes[key] {
			return nil, fmt.Errorf("pins use type %q, which no category declares", key)
		}
		built, ok, err := buildCategory(capture, ids, scope, "districts", key,
			label(typeLabels, key), "text", pinsByType)
		if err != nil {
			return nil, err
		}
		if ok {
			districts = append(districts, built)
		}
	}
	if len(districts) > 0 {
		groupID, err := ids.Claim("pb:category:" + scope + ":districts")
		if err != nil {
			return nil, err
		}
		groups = append(groups, mgdoc.Group{ID: groupID, Title: "Districts", Categories: districts})
	}
	return groups, nil
}

func buildCategory(
	capture *Capture,
	ids *mgdoc.IDSpace,
	scope, categoryKey, typeKey, typeLabel, displayType string,
	pinsByType map[string][]Pin,
) (mgdoc.Category, bool, error) {
	pins := pinsByType[typeKey]
	if len(pins) == 0 {
		return mgdoc.Category{}, false, nil
	}
	categoryID, err := ids.Claim("pb:type:" + scope + ":" + categoryKey + ":" + typeKey)
	if err != nil {
		return mgdoc.Category{}, false, err
	}
	renderAs := semconv.RenderAsPin
	if displayType == "text" {
		renderAs = semconv.RenderAsText
	}
	category := mgdoc.Category{
		ID:    categoryID,
		Title: typeLabel,
		// The type key, so icons dropped into the archive later attach on the
		// next generation without a policy change; until then the viewer's
		// fallback glyph stands in.
		Icon:        typeKey,
		DisplayType: displayType,
		Visible:     true,
		Locations:   make([]mgdoc.Location, 0, len(pins)),
		Attrs:       map[string]string{semconv.KeyRenderAs: renderAs},
	}
	for _, pin := range pins {
		locationID, err := ids.Claim("pb:pin:" + pin.ID)
		if err != nil {
			return mgdoc.Category{}, false, err
		}
		x, err := strconv.ParseFloat(pin.X, 64)
		if err != nil {
			return mgdoc.Category{}, false, fmt.Errorf("pin %s x %q: %w", pin.ID, pin.X, err)
		}
		y, err := strconv.ParseFloat(pin.Y, 64)
		if err != nil {
			return mgdoc.Category{}, false, fmt.Errorf("pin %s y %q: %w", pin.ID, pin.Y, err)
		}
		pixelX, pixelY := worldPixel(capture.Map.Transform, x, y)
		name := pin.Name
		if name == "" {
			name = typeLabel
		}
		category.Locations = append(category.Locations, mgdoc.Location{
			ID:          locationID,
			Title:       name,
			Description: pin.Description,
			Latitude:    mgdoc.SyntheticLatitude(pixelY),
			Longitude:   mgdoc.SyntheticLongitude(pixelX),
		})
	}
	return category, true, nil
}

func labelIndex(labels []Label) map[string]string {
	out := make(map[string]string, len(labels))
	for _, l := range labels {
		out[l.Key] = l.Label
	}
	return out
}

func label(index map[string]string, key string) string {
	if l, ok := index[key]; ok && l != "" {
		return l
	}
	return mgdoc.SpellOut(key)
}

func sortedKeys(pins map[string][]Pin) []string {
	keys := make([]string, 0, len(pins))
	for key := range pins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
