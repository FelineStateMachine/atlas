package arcgishub

import (
	"strings"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The national enrichment. A state GIS viewer is usually a branded window onto
// federal datasets, so the generic move is to read the federal layers directly,
// by any city's own bounding box, and every curated city gains the same
// hydrography without a line of per-city curation.
//
// Every national layer labels quiet: the hydrology is the city's context, not
// its headline, and a map flooded with creek names is a map about the wrong
// thing.

// slugSubwatersheds names the captured dataset the membership join reads, so
// the join and the table cannot drift apart on a spelling.
const slugSubwatersheds = "subwatersheds"

// national is the enrichment every curated city receives: the watersheds its
// ground drains through, in two grains of the USGS hydrologic unit hierarchy,
// and the named streams and waterbodies that do the draining.
var national = []dataset{
	{
		Slug: "watersheds", Title: "Watersheds",
		Geometry: "polygon",
		ZoneOf:   hucZone("huc10", "Watershed"),
		Label:    "quiet",
		National: true,
	},
	{
		Slug: slugSubwatersheds, Title: "Subwatersheds",
		Geometry: "polygon",
		ZoneOf:   hucZone("huc12", "Subwatershed"),
		Label:    "quiet",
		National: true,
	},
	{
		Slug: "streams", Title: "Streams",
		Geometry: "line",
		ZoneOf: func(f fields) zoneKey {
			name := strings.TrimSpace(f["gnis_name"])
			if name == "" {
				return zoneKey{}
			}
			return zoneKey{Key: doc.Slugify(name), Title: name, Subtitle: "Stream"}
		},
		StrokeWidth: 10,
		Label:       "quiet",
		National:    true,
		Role:        "water",
	},
	{
		Slug: "waterbodies", Title: "Waterbodies",
		Geometry: "polygon",
		// Every pond draws into the basemap; only the named ones earn a zone
		// anyone could search for.
		ZoneOf: func(f fields) zoneKey {
			name := strings.TrimSpace(f["GNIS_NAME"])
			if name == "" {
				return zoneKey{}
			}
			return zoneKey{Key: doc.Slugify(name), Title: name, Subtitle: "Waterbody"}
		},
		Label:    "quiet",
		National: true,
		Role:     "water",
	},
}

// hucZone folds a watershed-boundary layer into one zone per hydrologic unit,
// titled by the unit's own name with the code kept legible beside the grain.
// The code is the bucket key: it is nationally unique and survives an upstream
// refresh, where the layer's object ids are load artifacts that churn.
func hucZone(field, grain string) func(fields) zoneKey {
	return func(f fields) zoneKey {
		code, name := strings.TrimSpace(f[field]), strings.TrimSpace(f["name"])
		if code == "" || name == "" {
			return zoneKey{}
		}
		return zoneKey{Key: code, Title: name, Subtitle: grain + " · HUC " + code}
	}
}
