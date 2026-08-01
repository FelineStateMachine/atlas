// Package arcgismap reads a captured city and speaks MapGenie for it.
//
// The capture comes from a city's ArcGIS Hub open-data site: the municipal
// datasets a city publishes about itself -- its limits, its zoning, its
// trails, its fire stations -- fetched as GeoJSON and kept verbatim under a
// curated allowlist of fields. Like ignmap, pbmap and trekmap, translation
// happens when a capture is read, not when it is taken: the archive keeps
// what the city published, and every keep-or-leave decision lives here,
// where changing it re-applies to captures already on disk.
//
// The semantics are the point being proven. A city is a volume to the
// pipeline; each crawl day registers its own world, so a bundle's picker
// becomes the city's version history -- four captured days are four worlds,
// and the differences between them are differences in the city. Polygon
// datasets ride the bundle's zones, point datasets its pins, and the one
// raster layer is a basemap rendered from the city's own vector data,
// because a bundle owes its reader ground to stand on and ships offline.
//
// The coordinate design follows trekmap, on a plane. A city sits off the
// Web-Mercator diagonal, and the viewer's window is a square upon it, so the
// city's bounding box -- padded, and squared in Mercator's own terms --
// becomes the 8192-pixel world. A feature's true coordinates land on that
// square through the Mercator forward, and the pixel becomes synthetic
// latitude and longitude through the same inverse every translated source
// uses. The window is declared on the map as the mercator.px/deg mapping,
// and every pin keeps its true coordinates in atlas.geo.*, so nothing is
// flattened away.
package arcgismap

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// Kind names ArcGIS Hub captures in a map's snapshot index.
const Kind = "arcgis-map"

// Source is the value a well-formed capture carries in its source field.
const Source = "arcgis-hub"

// Fields is one feature's kept attributes, every value spelled as text.
type Fields map[string]string

// ZoneKey says which zone a feature folds into: the bucket's stable key and
// the title and subtitle the zone wears. A zero key is a feature that makes
// no zone.
type ZoneKey struct {
	Key      string
	Title    string
	Subtitle string
}

// Dataset is one curated layer of a city's hub: where to fetch it, which
// fields to keep, and what it becomes in the bundle -- pins when Group is
// set, zones when ZoneOf is, basemap linework when Role is. The functions
// read a feature's kept fields, so re-curating presentation is a table edit
// and a re-translation of captures already on disk.
type Dataset struct {
	Slug     string
	Title    string
	ItemID   string
	Layer    int
	Geometry string // "point" | "line" | "polygon"
	Keep     []string

	// Pins.
	Group    string
	Icon     string
	TitleOf  func(Fields) string
	Describe func(Fields) string

	// Zones. StrokeWidth is the ground width a line dataset's zones are
	// drawn and clicked at, in world pixels of the 8192 square: the path
	// stays the line it is, and the viewer strokes it.
	ZoneOf      func(Fields) ZoneKey
	StrokeWidth float64

	// Zoneomics marks the dataset whose zone buckets are enriched with
	// district rules when a crawl is handed exported zone reports: each
	// report joins its bucket by zone code, and the note lands on the
	// bucket's zone as prose.
	Zoneomics bool

	// Basemap. Emphasis scales a stroke per feature -- an arterial wider
	// than a lane -- and absent means every feature draws alike.
	Role     string
	Emphasis func(Fields) float64
}

// City is one curated hub. The table is the whole authority: an uncurated
// city is refused at the door, because an unverified bounding box would hang
// every pin on the wrong pixel, and unverified field names would title every
// zone with nothing.
type City struct {
	Slug     string
	Title    string
	HubBase  string
	MaxZoom  int
	BBox     [4]float64 // west, south, east, north, degrees
	Datasets []Dataset
}

// Cities is every city the source knows how to capture. A city that should
// stay out of the repository -- your own, say -- can be registered from a
// cities_local.go beside this file: that name is ignored by git, and an
// init function there adding to this map curates privately what this table
// curates publicly.
var Cities = map[string]City{
	"bend-or": bend,
}

// bend is the City of Bend, Oregon -- the curated proof city, chosen for a
// hub surface with every shape the format wants to prove: polygon ground,
// named line work, and titled points, all behind the standard hub download
// API. Field names were verified against the live services. The water
// utility layers stay out deliberately: their attribute tables carry staff
// account names, and an open capture keeps people out of it.
var bend = City{
	Slug:    "bend-or",
	Title:   "Bend, Oregon",
	HubBase: "https://data.bendoregon.gov",
	MaxZoom: 6,
	BBox:    [4]float64{-121.4180, 43.9500, -121.2170, 44.1650},
	Datasets: []Dataset{
		{
			Slug: "mpo-boundary", Title: "MPO Boundary",
			ItemID: "ec95698d5b7f494eb12dba2c572ed6d1", Layer: 0,
			Geometry: "polygon",
			Keep:     []string{"OID", "LABEL", "Acres"},
			ZoneOf: func(f Fields) ZoneKey {
				return ZoneKey{Key: "mpo-boundary", Title: "MPO Boundary",
					Subtitle: "Metropolitan planning area"}
			},
			Role: "boundary",
		},
		{
			Slug: "zoning", Title: "Zoning",
			ItemID: "90f0e9717185404581b2c1c865c13f03", Layer: 0,
			Geometry: "polygon",
			Keep:     []string{"OBJECTID", "ZONE", "ORDINANCE"},
			ZoneOf: func(f Fields) ZoneKey {
				code := f["ZONE"]
				if code == "" {
					return ZoneKey{}
				}
				return ZoneKey{Key: slugify(code), Title: code}
			},
			Zoneomics: true,
		},
		{
			Slug: "annexations", Title: "Annexations",
			ItemID: "7aec052cc38645f69d2a74e8062abd95", Layer: 0,
			Geometry: "polygon",
			Keep:     []string{"OBJECTID", "DESCRIPT", "EFF_DATE", "ORDIN_NO", "ANNX_ACRES"},
			// Annexations fold into the decades they took effect in: the
			// story a version history wants to tell is how the city grew,
			// not which ordinance grew it.
			ZoneOf: func(f Fields) ZoneKey {
				ms, err := strconv.ParseInt(f["EFF_DATE"], 10, 64)
				if err != nil || ms == 0 {
					return ZoneKey{Key: "undated", Title: "Annexed, date unknown"}
				}
				decade := time.UnixMilli(ms).UTC().Year() / 10 * 10
				return ZoneKey{
					Key:   strconv.Itoa(decade) + "s",
					Title: fmt.Sprintf("Annexed %d–%d", decade, decade+9),
				}
			},
		},
		{
			Slug: "wetlands", Title: "Wetlands",
			ItemID: "8c9ded5d2a32423f8b380de1bec50cf4", Layer: 0,
			Geometry: "polygon",
			Keep:     []string{"OBJECTID", "TYPE", "MAP_CODE", "ACRES"},
			ZoneOf: func(f Fields) ZoneKey {
				kind := f["TYPE"]
				if kind == "" {
					return ZoneKey{}
				}
				return ZoneKey{Key: slugify(kind), Title: kind, Subtitle: "Local Wetlands Inventory"}
			},
			Role: "water",
		},
		{
			Slug: "trails", Title: "Paths & Trails",
			ItemID: "8d006fb5453c4dbfad476033e847eca8", Layer: 0,
			Geometry: "line",
			Keep:     []string{"OBJECTID", "Trail_Name", "Park", "Status", "Surface_Ma", "Classifica", "Maintained"},
			// Every existing segment draws into the basemap; the named
			// trails earn zones, each one clickable end to end.
			ZoneOf: func(f Fields) ZoneKey {
				if f["Status"] != "Existing" || f["Trail_Name"] == "" {
					return ZoneKey{}
				}
				return ZoneKey{Key: slugify(f["Trail_Name"]), Title: f["Trail_Name"],
					Subtitle: title(f["Park"], "Trail")}
			},
			StrokeWidth: 12,
			Role:        "trail",
		},
		{
			Slug: "road-centerlines", Title: "Road Centerlines",
			ItemID: "c303eb99c9a242149b95d5055589e251", Layer: 0,
			Geometry: "line",
			Keep:     []string{"OBJECTID", "fullname", "roadclass", "onewaydir", "speedlimit"},
			Role:     "street",
			Emphasis: func(f Fields) float64 {
				switch f["roadclass"] {
				case "Local":
					return 1
				case "Collector":
					return 1.4
				default: // arterials and everything grander
					return 1.8
				}
			},
		},
		{
			Slug: "historic-sites", Title: "Historic Resources",
			ItemID: "70b88002805f42dfa94d0c470d49eec1", Layer: 0,
			Geometry: "point",
			Keep:     []string{"OBJECTID", "NAME", "TAB_NAME", "SHORT_DESC", "DESC1", "DESC2", "DESC3", "DESC4", "DESC5"},
			Group:    "Heritage",
			Icon:     "maki/monument",
			TitleOf:  func(f Fields) string { return f["NAME"] },
			Describe: func(f Fields) string {
				return join(" · ", f["TAB_NAME"], f["SHORT_DESC"],
					f["DESC1"], f["DESC2"], f["DESC3"], f["DESC4"], f["DESC5"])
			},
		},
	},
}

// join glues the non-empty pieces with the separator, which is most of what
// composing a card from optional fields is.
func join(sep string, pieces ...string) string {
	kept := pieces[:0:0]
	for _, piece := range pieces {
		if trimmed := strings.TrimSpace(piece); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, sep)
}

// Window is the ground the city's world square pictures: the degrees at the
// padded square's edges. The square is square in Mercator's own terms -- u
// linear in longitude, v in projected latitude -- which is what lets one
// number serve as both axes' scale.
type Window struct {
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
	South float64 `json:"south"`
}

// mercator lands degrees on the global Web-Mercator unit square.
func mercator(lon, lat float64) (u, v float64) {
	u = (lon + 180) / 360
	v = (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2
	return u, v
}

// degrees is the way back.
func degrees(u, v float64) (lon, lat float64) {
	lon = u*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*v))) * 180 / math.Pi
	return lon, lat
}

// CityWindow pads a city's bounding box five percent per side and squares it
// in Mercator terms, growing the shorter axis around its own middle. The
// same box always makes the same window, which is what keeps every capture
// of a city in one world.
func CityWindow(bbox [4]float64) Window {
	u0, v0 := mercator(bbox[0], bbox[3]) // west, north: the top-left
	u1, v1 := mercator(bbox[2], bbox[1]) // east, south: the bottom-right
	padU, padV := (u1-u0)*0.05, (v1-v0)*0.05
	u0, u1 = u0-padU, u1+padU
	v0, v1 = v0-padV, v1+padV
	side := math.Max(u1-u0, v1-v0)
	uMid, vMid := (u0+u1)/2, (v0+v1)/2
	u0, u1 = uMid-side/2, uMid+side/2
	v0, v1 = vMid-side/2, vMid+side/2
	west, north := degrees(u0, v0)
	east, south := degrees(u1, v1)
	return Window{West: west, North: north, East: east, South: south}
}

// WorldPixel lands true coordinates on the world square.
func (w Window) WorldPixel(lon, lat float64) (x, y float64) {
	u0, v0 := mercator(w.West, w.North)
	u1, _ := mercator(w.East, w.South)
	side := u1 - u0
	u, v := mercator(lon, lat)
	return (u - u0) / side * mgdoc.WorldSize, (v - v0) / side * mgdoc.WorldSize
}

// PxDeg spells the window as the two map attributes that declare it.
func (w Window) PxDeg() (px, deg string) {
	px = fmt.Sprintf("0,0,%d,%d", mgdoc.WorldSize, mgdoc.WorldSize)
	parts := []string{
		strconv.FormatFloat(w.West, 'f', -1, 64),
		strconv.FormatFloat(w.North, 'f', -1, 64),
		strconv.FormatFloat(w.East, 'f', -1, 64),
		strconv.FormatFloat(w.South, 'f', -1, 64),
	}
	return px, strings.Join(parts, ",")
}

// TileSetPath is the layer path the basemap's tile records group under,
// matching what tools/tiles recovers from a record URL's /tiles/ marker.
// The capture day is part of the path: every dated map renders its own
// basemap, and the pyramid pipeline tells layers apart by this path alone.
func TileSetPath(city, day string) string {
	return city + "/" + day + "/basemap"
}

// TileTemplate is the record URL the rendered basemap answers to. The scheme
// is deliberately not a fetchable one: these tiles were never on a wire, and
// a spelling without http keeps them visibly clear of the offline rule.
func TileTemplate(city, day string) string {
	return "arcgis-basemap://tiles/" + TileSetPath(city, day) + "/{z}/{x}/{y}.png"
}

// Capture is the canonical document the crawler stores: the city, the day,
// the window, the basemap pyramid rendered beside it, and every curated
// dataset's features under their kept fields. Fields marshal in declaration
// order and Normalize sorts the lists, so unchanged data always hashes to
// the capture already archived.
type Capture struct {
	Source   string            `json:"source"`
	City     string            `json:"city"`
	Title    string            `json:"title"`
	MapSlug  string            `json:"mapSlug"`
	Window   Window            `json:"window"`
	Basemap  MapConfig         `json:"basemap"`
	Datasets []CapturedDataset `json:"datasets"`
	// Zoneomics carries the district rules fetched beside the datasets,
	// one note per zone bucket of the dataset curated for enrichment.
	// Riding the same capture means the enrichment shares the datasets'
	// content addressing: a rules change is a new version the same way a
	// boundary change is, and an unchanged day stays no day at all.
	Zoneomics []ZoneNote `json:"zoneomics,omitempty"`
}

// ZoneNote is what Zoneomics said about one zone bucket: the bucket's own
// key, and the answer's fields flattened to text under dotted names --
// kept as data rather than prose, so re-curating how a zone card reads is
// a translator change, not a re-fetch.
type ZoneNote struct {
	Code   string `json:"code"`
	Fields Fields `json:"fields,omitempty"`
}

// MapConfig is the basemap pyramid as rendered: the deepest level drawn, and
// the extension every tile wears.
type MapConfig struct {
	MaxZoom   int    `json:"maxZoom"`
	Extension string `json:"extension"`
}

// CapturedDataset is one curated layer's features, joined back to the table
// by slug when the capture is read.
type CapturedDataset struct {
	Slug     string    `json:"slug"`
	Features []Feature `json:"features"`
}

// Feature is one thing the city published: its object identifier, its kept
// fields spelled as text, and its ground in true degrees.
type Feature struct {
	ID       int64    `json:"id"`
	Fields   Fields   `json:"fields,omitempty"`
	Geometry Geometry `json:"geometry"`
}

// Geometry is a feature's ground, normalized at capture to one of three
// shapes: a point, lines in MultiLineString nesting, or rings in
// MultiPolygon nesting. Positions are [longitude, latitude] in WGS84,
// rounded to seven decimals -- a centimeter -- so float spelling cannot
// masquerade as change.
type Geometry struct {
	Type  string          `json:"type"`
	Point []float64       `json:"point,omitempty"`
	Lines [][][]float64   `json:"lines,omitempty"`
	Rings [][][][]float64 `json:"rings,omitempty"`
}

const (
	GeometryPoint = "point"
	GeometryLines = "lines"
	GeometryRings = "rings"
)

// Normalize puts the capture into its canonical order. The crawler calls
// this before marshaling, so the hub listing its features differently does
// not masquerade as a new version.
func (c *Capture) Normalize() {
	sort.Slice(c.Datasets, func(a, b int) bool { return c.Datasets[a].Slug < c.Datasets[b].Slug })
	for _, dataset := range c.Datasets {
		features := dataset.Features
		sort.Slice(features, func(a, b int) bool { return features[a].ID < features[b].ID })
	}
	sort.Slice(c.Zoneomics, func(a, b int) bool { return c.Zoneomics[a].Code < c.Zoneomics[b].Code })
}

// MaybeTranslate hands other sources' snapshots through untouched and
// translates ArcGIS Hub captures.
func MaybeTranslate(kind string, doc []byte) ([]byte, error) {
	if kind != Kind {
		return doc, nil
	}
	return Translate(doc)
}

// mapSlugShape holds map slugs to capture days, which is what makes a map
// picker read as a version history.
var mapSlugShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Translate turns a capture into the MapGenie-shaped document the pipeline
// reads, deterministically for a given capture.
func Translate(doc []byte) ([]byte, error) {
	var capture Capture
	if err := json.Unmarshal(doc, &capture); err != nil {
		return nil, fmt.Errorf("decode arcgis capture: %w", err)
	}
	if capture.Source != Source {
		return nil, fmt.Errorf("capture source is %q, not %q", capture.Source, Source)
	}
	city, curated := Cities[capture.City]
	if !curated {
		return nil, fmt.Errorf("city %q is not curated", capture.City)
	}
	if !mapSlugShape.MatchString(capture.MapSlug) {
		return nil, fmt.Errorf("map slug %q is not a capture day", capture.MapSlug)
	}
	if capture.Basemap.MaxZoom < 1 {
		return nil, fmt.Errorf("capture declares no basemap pyramid")
	}
	if capture.Window.East <= capture.Window.West || capture.Window.North <= capture.Window.South {
		return nil, fmt.Errorf("capture window has no ground")
	}
	if len(capture.Datasets) == 0 {
		return nil, fmt.Errorf("capture carries no datasets")
	}
	capture.Normalize()

	byOrder, err := curatedOrder(city, &capture)
	if err != nil {
		return nil, err
	}

	ids := mgdoc.NewIDSpace()
	scope := capture.City + "/" + capture.MapSlug
	mapID, err := ids.Claim("arcgis:map:" + scope)
	if err != nil {
		return nil, err
	}
	gameID, err := ids.Claim("arcgis:game:" + capture.City)
	if err != nil {
		return nil, err
	}

	groups, err := buildGroups(&capture, byOrder, ids)
	if err != nil {
		return nil, err
	}
	regions, err := buildRegions(&capture, byOrder, ids)
	if err != nil {
		return nil, err
	}

	px, deg := capture.Window.PxDeg()
	out := mgdoc.Map{
		ID: mapID,
		// The day is the map: its slug, and its title, because the picker
		// listing dates is the version history reading as itself.
		Title:            capture.MapSlug,
		Slug:             capture.MapSlug,
		InitialLatitude:  mgdoc.SyntheticLatitude(mgdoc.WorldSize / 2),
		InitialLongitude: mgdoc.SyntheticLongitude(mgdoc.WorldSize / 2),
		Config: mgdoc.Config{TileSets: []mgdoc.TileSet{
			tileSet(capture.City, capture.MapSlug, capture.Basemap),
		}},
		// The map says what it pictures: a plane -- no globe pretends a city
		// is a world -- cut from Earth by the declared Mercator window, which
		// is exactly the transform WorldPixel projects features through, so
		// the two can never disagree without a test noticing.
		Attrs: map[string]string{
			semconv.KeyGeometrySurface:     semconv.SurfacePlane,
			semconv.KeyGeometryBody:        "earth",
			semconv.KeyGeometryMercatorPx:  px,
			semconv.KeyGeometryMercatorDeg: deg,
		},
		Game: mgdoc.Game{
			ID: gameID,
			// A city is a volume to the bundle, spelled in the archive's
			// upstream game shape here, which is the point being
			// proven.
			Title: title(capture.Title, city.Title),
			Slug:  capture.City,
		},
		Groups:  groups,
		Regions: regions,
	}
	return json.Marshal(out)
}

func title(given, fallback string) string {
	if given != "" {
		return given
	}
	return fallback
}

// curatedOrder joins the capture's datasets back to the table, in the
// table's own order, refusing anything the table does not name -- the same
// posture the crawler takes at the door, held again at read time.
func curatedOrder(city City, capture *Capture) ([]pairing, error) {
	captured := make(map[string]*CapturedDataset, len(capture.Datasets))
	for at := range capture.Datasets {
		dataset := &capture.Datasets[at]
		if _, doubled := captured[dataset.Slug]; doubled {
			return nil, fmt.Errorf("dataset %q is captured twice", dataset.Slug)
		}
		captured[dataset.Slug] = dataset
	}
	var out []pairing
	for at := range city.Datasets {
		curated := &city.Datasets[at]
		if data, taken := captured[curated.Slug]; taken {
			out = append(out, pairing{curated, data})
			delete(captured, curated.Slug)
		}
	}
	for slug := range captured {
		return nil, fmt.Errorf("dataset %q is not curated for %s", slug, city.Slug)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("capture carries no curated datasets")
	}
	return out, nil
}

type pairing struct {
	curated *Dataset
	data    *CapturedDataset
}

// tileSet declares the basemap pyramid in the shape tools/tiles expects.
// Bounds are declared full-square for every level down to zero, which is
// what lands the frame derivation on the translated world's own grid --
// sourceZoom five, first tile zero -- instead of the MapGenie window this
// map does not sit in.
func tileSet(citySlug, day string, config MapConfig) mgdoc.TileSet {
	set := mgdoc.TileSet{
		Name:      "Basemap",
		Path:      TileSetPath(citySlug, day),
		MinZoom:   0,
		MaxZoom:   config.MaxZoom,
		Extension: title(config.Extension, "png"),
		Bounds:    make(map[string]mgdoc.Bound, config.MaxZoom+1),
	}
	for zoom := 0; zoom <= config.MaxZoom; zoom++ {
		last := 1<<zoom - 1
		set.Bounds[strconv.Itoa(zoom)] = mgdoc.Bound{
			X: mgdoc.Range{Min: 0, Max: last},
			Y: mgdoc.Range{Min: 0, Max: last},
		}
	}
	return set
}

// strayTolerance is how much of a point dataset may fall outside the window
// before the capture is refused rather than trimmed: a hub layer
// occasionally holds one stray feature, and one stray is not a wrong window.
const strayTolerance = 0.01

// buildGroups arranges the point datasets the way the pipeline expects: one
// category per dataset, gathered under the curated group titles in table
// order.
func buildGroups(capture *Capture, pairs []pairing, ids *mgdoc.IDSpace) ([]mgdoc.Group, error) {
	byGroup := make(map[string][]mgdoc.Category)
	var order []string
	for _, pair := range pairs {
		curated, data := pair.curated, pair.data
		if curated.Group == "" {
			continue
		}
		categoryID, err := ids.Claim("arcgis:cat:" + capture.City + "/" + curated.Slug)
		if err != nil {
			return nil, err
		}
		attrs := map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin}
		if curated.Icon != "" {
			attrs[semconv.KeyIconStd] = curated.Icon
		}
		category := mgdoc.Category{
			ID:          categoryID,
			Title:       curated.Title,
			Icon:        slugify(curated.Title),
			DisplayType: "markers",
			Visible:     true,
			Locations:   make([]mgdoc.Location, 0, len(data.Features)),
			Attrs:       attrs,
		}
		strays, untitled := 0, 0
		for _, feature := range data.Features {
			if feature.Geometry.Type != GeometryPoint || len(feature.Geometry.Point) < 2 {
				return nil, fmt.Errorf("%s feature %d has no point", curated.Slug, feature.ID)
			}
			lon, lat := feature.Geometry.Point[0], feature.Geometry.Point[1]
			x, y := capture.Window.WorldPixel(lon, lat)
			if x < 0 || x > mgdoc.WorldSize || y < 0 || y > mgdoc.WorldSize {
				strays++
				continue
			}
			name := scrub(curated.TitleOf(feature.Fields))
			// A nameless row is the source's hygiene, not the curation's: a
			// pin nobody could search for is left out. A dataset nameless
			// throughout is different -- that is a wrong field name, and it
			// is refused below rather than shipped blank.
			if name == "" {
				untitled++
				continue
			}
			locationID, err := ids.Claim("arcgis:loc:" + capture.City + "/" + curated.Slug + "/" + strconv.FormatInt(feature.ID, 10))
			if err != nil {
				return nil, err
			}
			category.Locations = append(category.Locations, mgdoc.Location{
				ID:          locationID,
				Title:       name,
				Description: scrub(curated.Describe(feature.Fields)),
				Latitude:    mgdoc.SyntheticLatitude(y),
				Longitude:   mgdoc.SyntheticLongitude(x),
				// The coordinates as the city published them, verbatim:
				// provenance beside the synthetic position, so no reader
				// ever has to invert the window to learn where a pin
				// truly stands.
				Attrs: map[string]string{
					semconv.KeyGeoLat: strconv.FormatFloat(lat, 'f', -1, 64),
					semconv.KeyGeoLon: strconv.FormatFloat(lon, 'f', -1, 64),
				},
			})
		}
		if total := len(data.Features); total > 0 && float64(strays) > strayTolerance*float64(total) {
			return nil, fmt.Errorf("%s: %d of %d features fall outside the window", curated.Slug, strays, total)
		}
		if total := len(data.Features); total > 0 && untitled == total {
			return nil, fmt.Errorf("%s: no feature carries a title; is the title field curated right?", curated.Slug)
		}
		if _, seen := byGroup[curated.Group]; !seen {
			order = append(order, curated.Group)
		}
		byGroup[curated.Group] = append(byGroup[curated.Group], category)
	}

	groups := make([]mgdoc.Group, 0, len(order))
	for _, name := range order {
		groupID, err := ids.Claim("arcgis:group:" + capture.City + "/" + name)
		if err != nil {
			return nil, err
		}
		groups = append(groups, mgdoc.Group{ID: groupID, Title: name, Categories: byGroup[name]})
	}
	return groups, nil
}

// zoneLimit is the most zones one dataset may make. The viewer's zone index
// renders every zone a button and a title; an uncurated explosion -- every
// parcel a zone -- is refused while the mistake is one table edit old.
const zoneLimit = 256

// buildRegions folds the polygon and line datasets into zones: every
// feature whose ZoneOf names a bucket lands its ground in that bucket's
// region, and line features widen into ribbon polygons first, because a
// zone is ground and a line has none.
func buildRegions(capture *Capture, pairs []pairing, ids *mgdoc.IDSpace) ([]mgdoc.Region, error) {
	// Notes join the zone buckets under the buckets' own spelling: the
	// slugified code, which is what ZoneOf keys zoning zones by.
	notes := make(map[string]ZoneNote, len(capture.Zoneomics))
	for _, note := range capture.Zoneomics {
		if note.Code == "" {
			return nil, fmt.Errorf("a zoneomics note names no zone")
		}
		key := slugify(note.Code)
		if _, doubled := notes[key]; doubled {
			return nil, fmt.Errorf("zoneomics notes name %q twice", note.Code)
		}
		notes[key] = note
	}

	regions := []mgdoc.Region{}
	for _, pair := range pairs {
		curated, data := pair.curated, pair.data
		if curated.ZoneOf == nil {
			continue
		}
		buckets := make(map[string]*mgdoc.Region)
		var order []string
		for _, feature := range data.Features {
			key := curated.ZoneOf(feature.Fields)
			if key.Key == "" {
				continue
			}
			zone, made := buckets[key.Key]
			if !made {
				if len(buckets) >= zoneLimit {
					return nil, fmt.Errorf("%s makes more than %d zones", curated.Slug, zoneLimit)
				}
				zoneID, err := ids.Claim("arcgis:zone:" + capture.City + "/" + curated.Slug + "/" + key.Key)
				if err != nil {
					return nil, err
				}
				zone = &mgdoc.Region{
					ID:       zoneID,
					Title:    scrub(key.Title),
					Subtitle: scrub(title(key.Subtitle, curated.Title)),
				}
				// A zone made of lines declares its ground width, so a
				// reader can draw the path as the one stroke it is.
				if curated.Geometry == "line" && curated.StrokeWidth > 0 {
					zone.Attrs = map[string]string{
						semconv.KeyStrokeWidthPx: strconv.FormatFloat(curated.StrokeWidth, 'f', -1, 64),
					}
				}
				// The enriched dataset's zones speak their rules: the
				// captured Zoneomics note composes into the zone's card.
				if curated.Zoneomics {
					if note, told := notes[key.Key]; told {
						zone.Description = composeZoneNote(note)
					}
				}
				buckets[key.Key] = zone
				order = append(order, key.Key)
			}
			geometry, err := zoneGeometry(capture.Window, curated, feature)
			if err != nil {
				return nil, err
			}
			if geometry != nil {
				zone.Features = append(zone.Features, mgdoc.RegionFeature{Geometry: *geometry})
			}
		}
		sort.Strings(order)
		for _, key := range order {
			if len(buckets[key].Features) > 0 {
				regions = append(regions, *buckets[key])
			}
		}
	}
	return regions, nil
}

// zoneGeometry lands one feature's ground in the world in synthetic
// coordinates: rings project position by position into a MultiPolygon, and
// lines stay the MultiLineString they are -- the zone's declared stroke
// width says how wide the viewer draws them.
func zoneGeometry(window Window, curated *Dataset, feature Feature) (*mgdoc.Geometry, error) {
	var polygons [][][][]float64
	switch feature.Geometry.Type {
	case GeometryRings:
		for _, polygon := range feature.Geometry.Rings {
			var rings [][][]float64
			for _, ring := range polygon {
				projected := make([][]float64, 0, len(ring))
				for _, position := range ring {
					if len(position) < 2 {
						return nil, fmt.Errorf("%s feature %d has a malformed ring", curated.Slug, feature.ID)
					}
					projected = append(projected, synthetic(window, position[0], position[1]))
				}
				if len(projected) >= 4 {
					rings = append(rings, projected)
				}
			}
			if len(rings) > 0 {
				polygons = append(polygons, rings)
			}
		}
	case GeometryLines:
		if curated.StrokeWidth <= 0 {
			return nil, fmt.Errorf("%s zones lines without a stroke width", curated.Slug)
		}
		var lines [][][]float64
		for _, line := range feature.Geometry.Lines {
			projected := make([][]float64, 0, len(line))
			for _, position := range line {
				if len(position) < 2 {
					continue
				}
				projected = append(projected, synthetic(window, position[0], position[1]))
			}
			if len(projected) >= 2 {
				lines = append(lines, projected)
			}
		}
		if len(lines) == 0 {
			return nil, nil
		}
		coordinates, err := json.Marshal(lines)
		if err != nil {
			return nil, err
		}
		return &mgdoc.Geometry{Type: "MultiLineString", Coordinates: coordinates}, nil
	default:
		return nil, fmt.Errorf("%s feature %d has no ground to zone", curated.Slug, feature.ID)
	}
	if len(polygons) == 0 {
		return nil, nil
	}
	coordinates, err := json.Marshal(polygons)
	if err != nil {
		return nil, err
	}
	return &mgdoc.Geometry{Type: "MultiPolygon", Coordinates: coordinates}, nil
}

// composeZoneNote writes a zone's card from the note's flattened fields.
// Field names arrive dotted by API section -- zoning.*, plu.*, controls.*
// -- and the exact vocabulary varies by plan, so composition is generic:
// sections in a fixed order, each field spelled out as its own line, every
// string scrubbed, and the source named at the end. Re-curating how a card
// reads is an edit here, re-applied to captures already on disk.
func composeZoneNote(note ZoneNote) string {
	sections := []struct{ prefix, title string }{
		{"zoning.", "Zoning"},
		{"plu.", "Permitted uses"},
		{"controls.", "Standards"},
	}
	keys := make([]string, 0, len(note.Fields))
	for key := range note.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string
	claimed := make(map[string]bool, len(keys))
	for _, section := range sections {
		var lines []string
		for _, key := range keys {
			if !strings.HasPrefix(key, section.prefix) {
				continue
			}
			claimed[key] = true
			if line := noteLine(strings.TrimPrefix(key, section.prefix), note.Fields[key]); line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			parts = append(parts, section.title+"\n"+strings.Join(lines, "\n"))
		}
	}
	// Fields outside the known sections still speak, unsectioned: a plan
	// answering in a vocabulary this table has not met loses nothing.
	var loose []string
	for _, key := range keys {
		if claimed[key] {
			continue
		}
		if line := noteLine(key, note.Fields[key]); line != "" {
			loose = append(loose, line)
		}
	}
	if len(loose) > 0 {
		parts = append(parts, strings.Join(loose, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n\nData: Zoneomics"
}

// noteLine spells one flattened field as card prose: the name unslugged,
// the value unpacked and scrubbed -- per line, so losing a smuggled URL
// never flattens the card's shape.
func noteLine(name, value string) string {
	value = scrub(unpackControls(strings.TrimSpace(value)))
	if value == "" {
		return ""
	}
	return mgdoc.SpellOut(strings.ReplaceAll(strings.ReplaceAll(name, "_", "-"), ".", "-")) + ": " + value
}

// unpackControls opens the exports' packed control spelling --
// "max_building_height_ft-25; min_lot_area_sq_ft-7000" -- into prose, and
// leaves any value not shaped that way exactly as it came.
func unpackControls(value string) string {
	segments := strings.Split(value, "; ")
	unpacked := make([]string, 0, len(segments))
	for _, segment := range segments {
		name, rest, found := strings.Cut(segment, "-")
		if !found || name == "" || !packedControlName(name) {
			return value
		}
		unpacked = append(unpacked, mgdoc.SpellOut(strings.ReplaceAll(name, "_", "-"))+" "+rest)
	}
	return strings.Join(unpacked, "; ")
}

// packedControlName admits only the snake_case names the packed spelling
// uses, so a prose value that merely contains a hyphen stays prose.
func packedControlName(name string) bool {
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// synthetic is one position's whole journey: true degrees to world pixel to
// the synthetic coordinates a zone ring speaks.
func synthetic(window Window, lon, lat float64) []float64 {
	x, y := window.WorldPixel(lon, lat)
	return []float64{mgdoc.SyntheticLongitude(x), mgdoc.SyntheticLatitude(y)}
}

// scrub keeps a live URL out of everything this package emits. Bundles ship
// offline and the validator refuses any http spelling anywhere in a payload;
// curated fields should never carry one, and any that slips through loses
// the token rather than failing the build a capture too late.
func scrub(text string) string {
	if !strings.Contains(strings.ToLower(text), "http") {
		return text
	}
	kept := []string{}
	for token := range strings.FieldsSeq(text) {
		if strings.Contains(strings.ToLower(token), "http") {
			continue
		}
		kept = append(kept, token)
	}
	return strings.Join(kept, " ")
}

// slugify spells a value the way slugs and icon keys are named.
func slugify(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
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

// FieldString spells one GeoJSON property value as capture text: numbers in
// their shortest exact form, text as itself, absence as emptiness. The
// crawler runs every kept field through this so a value's JSON spelling
// cannot wobble the capture's hash.
func FieldString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

// Round7 rounds a coordinate to seven decimals -- a centimeter on the
// ground -- which is where capture determinism and honest precision meet.
func Round7(value float64) float64 {
	return math.Round(value*1e7) / 1e7
}
