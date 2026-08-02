package arcgishub

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The curation table, which is this source's whole authority.
//
// A city's hub publishes dozens of layers under machine names, and what a
// bundle should make of each one -- a pin or a zone, titled from which field,
// described from which others, bucketed how -- is an editorial judgement about
// that particular city. It lives here, in reviewed code, and it is applied when
// a capture is read rather than when it is taken: the archive keeps what the
// city published, and re-curating replays over every day already on disk.
//
// An uncurated city is refused at the door. An unverified bounding box would
// hang every pin on the wrong pixel and unverified field names would title every
// zone with nothing, so the table is a gate rather than a default.
//
// What is *not* here is how to fetch any of it -- item ids, layer numbers, the
// field allowlist, the query filters. Those are the crawler's, and the crawler
// is the only thing in this rewrite permitted to reach the network. A source is
// a pure function of bytes already on disk (issue #5 §5.1), so its table says
// only what a capture means.

// dataset is one curated layer as the reader understands it.
type dataset struct {
	Slug  string
	Title string
	// Geometry is the shape the capture normalized this layer to:
	// "point", "line" or "polygon".
	Geometry string

	// Pins. A dataset with Group set becomes a point collection filed under
	// that heading; Icon names a standard glyph for it to be resolved to.
	Group    string
	Icon     string
	TitleOf  func(fields) string
	Describe func(fields) string

	// Zones. ZoneOf folds a layer's rows into buckets, each bucket one shape
	// feature of the dataset's collection. StrokeWidth is the ground width a
	// line dataset's features are drawn and clicked at, in world pixels; Label
	// is the label policy the collection declares, and empty means the
	// format's own default for the kind.
	ZoneOf      func(fields) zoneKey
	StrokeWidth float64
	Label       string

	// National says this layer came from a federal service by bounding box
	// rather than from the city's own hub. It is not a fetch detail here: the
	// membership join claims a subwatershed for the city's own zones and never
	// for the nation's, so the reader has to be able to tell them apart.
	National bool

	// Basemap. Role is what the renderer draws this layer as, and Emphasis
	// scales a stroke per row -- an arterial wider than a lane. Neither is read
	// by translation; they are the curation the offline renderer answers to,
	// and they live beside the rest of a layer's judgement rather than in a
	// second table that could drift from it.
	Role     string
	Emphasis func(fields) float64
}

// zoneKey says which bucket a row folds into: the bucket's stable key and the
// title and subtitle the resulting shape wears. An empty key is a row that makes
// no zone.
type zoneKey struct {
	Key      string
	Title    string
	Subtitle string
}

// city is one curated hub.
type city struct {
	Slug     string
	Title    string
	BBox     [4]float64 // west, south, east, north, degrees
	Datasets []dataset
}

// datasets is the city's own curation followed by the national enrichment every
// US city receives by bounding box, in that order, so a city's zones keep their
// places and the nation's sort after them. The order is the legend order.
func (c city) datasets() []dataset {
	out := make([]dataset, 0, len(c.Datasets)+len(national))
	out = append(out, c.Datasets...)
	return append(out, national...)
}

// cities is every city this source can read. A city that should stay out of the
// repository is not registered here at all; the reference tree's habit of a
// gitignored companion file is not carried over, because a table that can be
// extended by a file nobody reviews is not a gate.
var cities = map[string]city{
	"bend-or":          bend,
	"redondo-beach-ca": redondoBeach,
}

// bend is the City of Bend, Oregon -- the proof city, chosen for a hub surface
// with every shape the format wants to prove: polygon ground, named line work,
// and titled points. The water utility layers stay out deliberately: their
// attribute tables carry staff account names, and an open capture keeps people
// out of it.
var bend = city{
	Slug:  "bend-or",
	Title: "Bend, Oregon",
	BBox:  [4]float64{-121.4180, 43.9500, -121.2170, 44.1650},
	Datasets: []dataset{
		{
			Slug: "mpo-boundary", Title: "MPO Boundary",
			Geometry: "polygon",
			ZoneOf: func(fields) zoneKey {
				return zoneKey{Key: "mpo-boundary", Title: "MPO Boundary",
					Subtitle: "Metropolitan planning area"}
			},
			Role: "boundary",
		},
		{
			Slug: "zoning", Title: "Zoning",
			Geometry: "polygon",
			ZoneOf: func(f fields) zoneKey {
				code := f["ZONE"]
				if code == "" {
					return zoneKey{}
				}
				return zoneKey{Key: doc.Slugify(code), Title: code}
			},
		},
		{
			Slug: "annexations", Title: "Annexations",
			Geometry: "polygon",
			// Annexations fold into the decades they took effect in: the story
			// a version history wants to tell is how the city grew, not which
			// ordinance grew it.
			ZoneOf: func(f fields) zoneKey {
				ms, err := strconv.ParseInt(f["EFF_DATE"], 10, 64)
				if err != nil || ms == 0 {
					return zoneKey{Key: "undated", Title: "Annexed, date unknown"}
				}
				decade := time.UnixMilli(ms).UTC().Year() / 10 * 10
				return zoneKey{
					Key:   strconv.Itoa(decade) + "s",
					Title: fmt.Sprintf("Annexed %d–%d", decade, decade+9),
				}
			},
		},
		{
			Slug: "wetlands", Title: "Wetlands",
			Geometry: "polygon",
			ZoneOf: func(f fields) zoneKey {
				kind := f["TYPE"]
				if kind == "" {
					return zoneKey{}
				}
				return zoneKey{Key: doc.Slugify(kind), Title: kind,
					Subtitle: "Local Wetlands Inventory"}
			},
			Role: "water",
		},
		{
			Slug: "trails", Title: "Paths & Trails",
			Geometry: "line",
			// Every existing segment draws into the basemap; the named trails
			// earn zones, each one clickable end to end.
			ZoneOf: func(f fields) zoneKey {
				if f["Status"] != "Existing" || f["Trail_Name"] == "" {
					return zoneKey{}
				}
				return zoneKey{Key: doc.Slugify(f["Trail_Name"]), Title: f["Trail_Name"],
					Subtitle: named(f["Park"], "Trail")}
			},
			StrokeWidth: 12,
			Role:        "trail",
		},
		{
			Slug: "road-centerlines", Title: "Road Centerlines",
			Geometry: "line",
			Role:     "street",
			Emphasis: func(f fields) float64 {
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
			Geometry: "point",
			Group:    "Heritage",
			Icon:     "maki/monument",
			TitleOf:  func(f fields) string { return f["NAME"] },
			Describe: func(f fields) string {
				return join(" · ", f["TAB_NAME"], f["SHORT_DESC"],
					f["DESC1"], f["DESC2"], f["DESC3"], f["DESC4"], f["DESC5"])
			},
		},
	},
}

// redondoBeach is the City of Redondo Beach, California -- the second proof
// city. Its land-use layer is parcel-grained and carries the zoning code on
// every polygon, so the zoning zones bucket by code and double as the basemap's
// parcel texture.
var redondoBeach = city{
	Slug:  "redondo-beach-ca",
	Title: "Redondo Beach, California",
	BBox:  [4]float64{-118.4060, 33.8110, -118.3490, 33.8985},
	Datasets: []dataset{
		{
			Slug: "land-use", Title: "Zoning",
			Geometry: "polygon",
			ZoneOf: func(f fields) zoneKey {
				code := strings.TrimSpace(f["ZONINGCODE"])
				if code == "" {
					return zoneKey{}
				}
				return zoneKey{Key: doc.Slugify(code), Title: code}
			},
			Role: "parcel",
		},
		{
			Slug: "general-plan", Title: "General Plan",
			Geometry: "polygon",
			ZoneOf: func(f fields) zoneKey {
				code := strings.TrimSpace(f["GENERALPLAN"])
				if code == "" {
					return zoneKey{}
				}
				return zoneKey{Key: doc.Slugify(code), Title: code, Subtitle: "General Plan"}
			},
		},
		{
			Slug: "council-districts", Title: "Council Districts",
			Geometry: "polygon",
			ZoneOf: func(f fields) zoneKey {
				number := strings.TrimSpace(f["District"])
				if number == "" {
					return zoneKey{}
				}
				return zoneKey{Key: "district-" + doc.Slugify(number),
					Title:    "Council District " + number,
					Subtitle: strings.TrimSpace(f["CouncilMember"])}
			},
		},
		{
			Slug: "district-lines", Title: "Council District Boundaries",
			Geometry: "line",
			Role:     "boundary",
		},
		{
			Slug: "parks", Title: "Parks & Recreation",
			Geometry: "point",
			Group:    "Recreation",
			Icon:     "maki/park",
			TitleOf:  func(f fields) string { return f["name"] },
			Describe: func(f fields) string {
				return join(" · ", f["address"], f["facility_size"], f["web_desc"])
			},
		},
	},
}

// join glues the non-empty pieces with the separator, which is most of what
// composing a card from optional fields is.
func join(sep string, pieces ...string) string {
	kept := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if trimmed := strings.TrimSpace(piece); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, sep)
}

// named prefers what was given and falls back when it is empty.
func named(given, fallback string) string {
	if given != "" {
		return given
	}
	return fallback
}
