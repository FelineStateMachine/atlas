// Package trekmap reads a captured planetary map and speaks MapGenie for it.
//
// The capture marries two public NASA/USGS publications: a global equirectangular
// mosaic served by NASA's Trek tile services, and the IAU Gazetteer of Planetary
// Nomenclature's feature list for the same body. Neither knows the other exists;
// this package is where a body's named places land on its picture. Like ignmap
// and pbmap, translation happens when a capture is read, not when it is taken --
// the archive keeps what NASA and the IAU published, and every keep-or-leave
// decision lives here, where changing it re-applies to captures already on disk.
//
// The coordinate design is the whole trick. Trek mosaics are equirectangular,
// two tiles wide and one tall at their zoom zero, so a Trek level sits in the
// pipeline's square pyramid one zoom up: pipeline zoom z holds Trek zoom z-1,
// 2^z tiles across and 2^(z-1) down, the planet filling the top half of the
// world square the way a wikimap narrower than tall fills part of its own.
// A feature's planetary coordinates become a pixel on that image -- x from
// longitude across the full width, y from latitude down the top half -- and
// the pixel becomes synthetic latitude and longitude through the same inverse
// Mercator every other source uses. Mercator's distortion cancels exactly,
// because the raster and the pins ride the same mapping; the real planetary
// coordinates stay in the capture, and in the pin's own text, so nothing is
// flattened away.
package trekmap

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// Kind names Trek captures in a map's snapshot index.
const Kind = "trek-map"

// Source is the value a well-formed capture carries in its source field.
const Source = "nasa-trek"

// Capture is the canonical document the crawler stores: which body and mosaic
// the raster is, every sibling mosaic captured beside it, and every named
// feature the Gazetteer publishes for the body. Fields marshal in declaration
// order and Normalize sorts the lists, so unchanged data always hashes to the
// capture already archived. Variants is newer than the field's first captures;
// a capture without one is simply a body pictured a single way.
type Capture struct {
	Source   string    `json:"source"`
	Body     string    `json:"body"`
	Layer    string    `json:"layer"`
	MapSlug  string    `json:"mapSlug"`
	MapTitle string    `json:"mapTitle"`
	Map      MapConfig `json:"map"`
	Variants []Variant `json:"variants,omitempty"`
	Features []Feature `json:"features"`
}

// MapConfig is the pyramid as captured. MaxZoom counts in the pipeline's
// square world -- the deepest level actually taken, one above the Trek level
// it came from. LayerTitle names the mosaic the way a person knows it, which
// is what the viewer's layer picker shows.
type MapConfig struct {
	MaxZoom    int    `json:"maxZoom"`
	Extension  string `json:"extension"`
	LayerTitle string `json:"layerTitle"`
}

// Variant is a sibling mosaic of the same ground: another way of seeing the
// body -- elevation beside photograph -- captured into the same window, so
// the viewer offers it the way it offers a game's satellite layer beside its
// atlas. Its pyramid is its own; siblings need not agree on depth.
type Variant struct {
	Layer     string `json:"layer"`
	Title     string `json:"title"`
	MaxZoom   int    `json:"maxZoom"`
	Extension string `json:"extension"`
}

// Feature is one Gazetteer entry, as the feature list publishes it: the IAU's
// own feature identifier, planetocentric latitude, east-positive longitude in
// the Gazetteer's 0..360 spelling, diameter in kilometers, and the origin text
// explaining the name -- the one piece of prose the data carries.
type Feature struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Code       string  `json:"code"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	DiameterKM float64 `json:"diameterKm"`
	Origin     string  `json:"origin,omitempty"`
}

// Normalize puts the capture into its canonical order. The crawler calls this
// before marshaling, so the Gazetteer listing its features differently does
// not masquerade as a new version.
func (c *Capture) Normalize() {
	sort.Slice(c.Variants, func(a, b int) bool { return c.Variants[a].Layer < c.Variants[b].Layer })
	sort.Slice(c.Features, func(a, b int) bool { return c.Features[a].ID < c.Features[b].ID })
}

// TileSetPath is the layer path tile records group under, matching what
// tools/tiles recovers from a canonical tile URL: the segments between
// trek.nasa.gov's /tiles/ marker and the zoom.
func TileSetPath(body, layer string) string {
	return body + "/EQ/" + layer
}

// LevelExtent reports the last tile column and row of one pipeline level: the
// full width of the world square, and the top half of its height. The crawler
// asks for exactly these tiles, and Translate declares exactly these bounds,
// because they are the same call.
func LevelExtent(zoom int) (maxX, maxY int) {
	if zoom == 0 {
		return 0, 0
	}
	return 1<<zoom - 1, 1<<(zoom-1) - 1
}

// MaybeTranslate hands other sources' snapshots through untouched and
// translates Trek captures.
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
		return nil, fmt.Errorf("decode trek capture: %w", err)
	}
	if capture.Source != Source {
		return nil, fmt.Errorf("capture source is %q, not %q", capture.Source, Source)
	}
	if capture.Body == "" || capture.Layer == "" || capture.MapSlug == "" {
		return nil, fmt.Errorf("capture names no map")
	}
	if capture.Map.MaxZoom < 1 {
		return nil, fmt.Errorf("capture declares no pyramid")
	}
	if len(capture.Features) == 0 {
		return nil, fmt.Errorf("capture carries no features")
	}
	taken := map[string]bool{capture.Layer: true}
	for _, variant := range capture.Variants {
		if variant.Layer == "" || variant.MaxZoom < 1 {
			return nil, fmt.Errorf("variant %q declares no pyramid", variant.Layer)
		}
		if taken[variant.Layer] {
			return nil, fmt.Errorf("layer %q is captured twice", variant.Layer)
		}
		taken[variant.Layer] = true
	}
	capture.Normalize()

	ids := mgdoc.NewIDSpace()
	scope := capture.Body + "/" + capture.MapSlug
	mapID, err := ids.Claim("trek:map:" + scope)
	if err != nil {
		return nil, err
	}
	gameID, err := ids.Claim("trek:game:" + capture.Body)
	if err != nil {
		return nil, err
	}

	groups, err := buildGroups(&capture, ids, scope)
	if err != nil {
		return nil, err
	}

	sets := []mgdoc.TileSet{tileSet(capture.Body, capture.Layer,
		title(capture.Map.LayerTitle, "default"), capture.Map.Extension, capture.Map.MaxZoom)}
	for _, variant := range capture.Variants {
		sets = append(sets, tileSet(capture.Body, variant.Layer,
			title(variant.Title, "default"), variant.Extension, variant.MaxZoom))
	}

	out := mgdoc.Map{
		ID:    mapID,
		Title: title(capture.MapTitle, capture.MapSlug),
		Slug:  capture.MapSlug,
		// The map opens on the middle of the planet's picture: halfway across
		// the world, a quarter down it, which is where a 2:1 image's centre
		// sits in the square.
		InitialLatitude:  mgdoc.SyntheticLatitude(mgdoc.WorldSize / 4),
		InitialLongitude: mgdoc.SyntheticLongitude(mgdoc.WorldSize / 2),
		Config:           mgdoc.Config{TileSets: sets},
		Game: mgdoc.Game{
			ID: gameID,
			// A planet is a "game" to the pipeline, which is the point being
			// proven: the slug is the body's own lowercase name, free for
			// another source's capture of the same body to answer to.
			Title: mgdoc.SpellOut(capture.Body),
			Slug:  capture.Body,
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

// tileSet declares one layer's pyramid in the shape tools/tiles expects.
// Bounds are declared for every level down to zero: a level without them
// would be measured against the MapGenie window this map does not sit in,
// and the half-height windows are what tell the frame derivation where the
// planet actually is. Every layer of a body shares that window whatever its
// depth, which is what lets siblings ride one map as its variants.
func tileSet(body, layer, name, ext string, maxZoom int) mgdoc.TileSet {
	set := mgdoc.TileSet{
		Name:      name,
		Path:      TileSetPath(body, layer),
		MinZoom:   0,
		MaxZoom:   maxZoom,
		Extension: extension(ext),
		Bounds:    make(map[string]mgdoc.Bound, maxZoom+1),
	}
	for zoom := 0; zoom <= maxZoom; zoom++ {
		maxX, maxY := LevelExtent(zoom)
		set.Bounds[strconv.Itoa(zoom)] = mgdoc.Bound{
			X: mgdoc.Range{Min: 0, Max: maxX},
			Y: mgdoc.Range{Min: 0, Max: maxY},
		}
	}
	return set
}

func extension(given string) string {
	if given != "" {
		return given
	}
	return "jpg"
}

// worldPixel lands a feature's planetary coordinates on the picture. The
// mosaic spans longitude -180..180 west to east and latitude 90..-90 top to
// bottom; the Gazetteer speaks east-positive 0..360, so longitudes wrap into
// the mosaic's half-open window first. X crosses the full world square, y
// only its top half, because that is where a 2:1 image sits in it.
func worldPixel(longitude, latitude float64) (x, y float64) {
	wrapped := math.Mod(longitude+180, 360) - 180
	x = (wrapped + 180) / 360 * mgdoc.WorldSize
	y = (90 - latitude) / 180 * (mgdoc.WorldSize / 2)
	return x, y
}

// standardIcons names a library glyph for each Gazetteer feature-type code,
// in the set/name vocabulary of atlas.icon.std. The codes are the IAU's own
// and hold across bodies, so the Moon's craters will wear the same rim the
// day a capture arrives. The reading leans semantic -- a mons is a mountain,
// a patera a volcano, a palus literally a marsh -- and falls back to shape
// language where no glyph says the thing: rimmed circles for depressions,
// squares for plains, triangles for relief. A code missing here is not an
// error; its category keeps the viewer's initials until someone curates it,
// and re-curating is one table edit and a re-translation of captures already
// on disk.
var standardIcons = map[string]string{
	"AA": "maki/circle-stroked",   // crater: a rim
	"CA": "maki/circle-stroked",   // catena: a chain of them
	"CB": "maki/circle-stroked",   // cavus: a hollow
	"AL": "maki/circle",           // albedo feature: a patch of tone
	"MA": "maki/circle",           // macula: a dark spot
	"MO": "maki/mountain",         // mons
	"TH": "maki/mountain",         // tholus: a small domical mountain
	"CO": "maki/triangle-stroked", // collis: small hills
	"DO": "maki/triangle-stroked", // dorsum: a ridge
	"RU": "maki/triangle",         // rupes: a scarp
	"SC": "maki/triangle",         // scopulus: a lobate scarp
	"PA": "maki/volcano",          // patera: an irregular volcanic crater
	"CH": "maki/caution",          // chaos: broken terrain
	"LA": "maki/caution",          // labes: a landslide
	"CM": "maki/wetland",          // chasma: parallel-walled canyon, read as lines
	"FO": "maki/wetland",          // fossa: a long narrow trench
	"LB": "maki/wetland",          // labyrinthus: intersecting valleys
	"SU": "maki/wetland",          // sulcus: subparallel furrows
	"PE": "maki/wetland",          // palus: literally a marsh
	"VA": "maki/water",            // vallis: a channel something carved
	"SE": "maki/water",            // serpens: a sinuous feature
	"FL": "maki/waterfall",        // fluctus: flow terrain
	"PL": "maki/square-stroked",   // planitia: a low plain
	"VS": "maki/square-stroked",   // vastitas: a vast one
	"PM": "maki/square",           // planum: a plateau
	"MN": "maki/square",           // mensa: a mesa
	"TA": "maki/landuse",          // terra: extensive land
	"LN": "maki/landuse",          // lingula: a tongue of plateau
	"UN": "maki/beach",            // unda: dunes
}

// buildGroups arranges the Gazetteer's flat feature list the way the pipeline
// expects: one category per feature type, all under one group. The type's
// descriptor -- "Crater, craters" names the singular and the plural -- keeps
// its singular half as the category title and lends its slug as the icon key,
// so artwork dropped into the archive later attaches without a policy change;
// until then the viewer's fallback glyph stands in.
func buildGroups(capture *Capture, ids *mgdoc.IDSpace, scope string) ([]mgdoc.Group, error) {
	byType := make(map[string][]Feature)
	var order []string
	for _, feature := range capture.Features {
		if err := check(feature); err != nil {
			return nil, err
		}
		if _, seen := byType[feature.Type]; !seen {
			order = append(order, feature.Type)
		}
		byType[feature.Type] = append(byType[feature.Type], feature)
	}
	sort.Strings(order)

	categories := make([]mgdoc.Category, 0, len(order))
	for _, featureType := range order {
		name := typeName(featureType)
		categoryID, err := ids.Claim("trek:type:" + scope + ":" + name)
		if err != nil {
			return nil, err
		}
		attrs := map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin}
		if standard := standardIcons[byType[featureType][0].Code]; standard != "" {
			attrs[semconv.KeyIconStd] = standard
		}
		category := mgdoc.Category{
			ID:          categoryID,
			Title:       name,
			Icon:        iconKey(name),
			DisplayType: "markers",
			Visible:     true,
			Locations:   make([]mgdoc.Location, 0, len(byType[featureType])),
			Attrs:       attrs,
		}
		for _, feature := range byType[featureType] {
			locationID, err := ids.Claim("trek:feature:" + strconv.FormatInt(feature.ID, 10))
			if err != nil {
				return nil, err
			}
			x, y := worldPixel(feature.Longitude, feature.Latitude)
			category.Locations = append(category.Locations, mgdoc.Location{
				ID:          locationID,
				Title:       feature.Name,
				Description: describe(feature),
				Latitude:    mgdoc.SyntheticLatitude(y),
				Longitude:   mgdoc.SyntheticLongitude(x),
			})
		}
		categories = append(categories, category)
	}

	groupID, err := ids.Claim("trek:group:" + scope)
	if err != nil {
		return nil, err
	}
	return []mgdoc.Group{{ID: groupID, Title: "Nomenclature", Categories: categories}}, nil
}

func check(feature Feature) error {
	switch {
	case feature.ID <= 0:
		return fmt.Errorf("feature %q has no identifier", feature.Name)
	case feature.Name == "":
		return fmt.Errorf("feature %d has no name", feature.ID)
	case feature.Type == "":
		return fmt.Errorf("feature %q has no type", feature.Name)
	case feature.Latitude < -90 || feature.Latitude > 90:
		return fmt.Errorf("feature %q sits at latitude %v", feature.Name, feature.Latitude)
	case feature.Longitude < 0 || feature.Longitude > 360:
		return fmt.Errorf("feature %q sits at longitude %v", feature.Name, feature.Longitude)
	}
	return nil
}

// iconKey spells a category title the way icon files are named, so artwork
// for "Albedo Feature" lands at albedo-feature like every other source's.
func iconKey(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+'a'-'A')
		default:
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// typeName keeps the singular half of the Gazetteer's "Crater, craters"
// descriptor; a descriptor without the plural half is already the name.
func typeName(descriptor string) string {
	for at, r := range descriptor {
		if r == ',' {
			return descriptor[:at]
		}
	}
	return descriptor
}

// describe writes the pin's card: the feature's real place on the planet, its
// size when the Gazetteer gives one, and the origin of its name. The card is
// where the true coordinates survive for a reader, since the pin itself
// stores only synthetic ones.
func describe(feature Feature) string {
	text := place(feature.Latitude, feature.Longitude)
	if feature.DiameterKM > 0 {
		text += fmt.Sprintf(" · %s km across", trimmed(feature.DiameterKM))
	}
	if feature.Origin != "" {
		text += " — " + feature.Origin
	}
	return text
}

func place(latitude, longitude float64) string {
	ns := "N"
	if latitude < 0 {
		ns, latitude = "S", -latitude
	}
	return fmt.Sprintf("%s°%s %s°E", trimmed(latitude), ns, trimmed(longitude))
}

// trimmed spells a value to two decimals and no further, dropping the
// trailing zeroes a card does not need.
func trimmed(value float64) string {
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64)
}
