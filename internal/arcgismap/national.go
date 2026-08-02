package arcgismap

import (
	"strconv"
	"strings"
)

// The national enrichment. A state GIS viewer is usually a branded window
// onto federal datasets -- Colorado's water viewer credits the USGS
// Watershed Boundary Dataset and National Hydrography Dataset by name -- so
// the generic move is to ask the federal services directly, by any city's
// own bounding box, and every curated city gains the same layers without a
// line of per-city curation.
//
// Both services are plain ArcGIS MapServer query endpoints on
// hydro.nationalmap.gov. Field spellings differ per layer -- the WBD layers
// speak lowercase, the NHD waterbody layer uppercase -- and were verified
// against the live services, the same discipline every city table keeps.

const (
	wbdServer = "https://hydro.nationalmap.gov/arcgis/rest/services/wbd/MapServer"
	nhdServer = "https://hydro.nationalmap.gov/arcgis/rest/services/nhd/MapServer"

	// SlugSubwatersheds names the captured dataset the membership join
	// reads, so the join and the table cannot drift apart on a spelling.
	SlugSubwatersheds = "subwatersheds"
)

// National is the enrichment every curated city receives: the watersheds
// its ground drains through, in two grains of the USGS hydrologic unit
// hierarchy, and the named streams and waterbodies that do the draining.
// A padded city window intersects a handful of hydrologic units and tens
// of named streams -- national extent, city-sized answers. Every national
// layer labels quiet: the hydrology is the city's context, not its
// headline, and a map flooded with creek names is a map about the wrong
// thing.
var National = []Dataset{
	{
		Slug: "watersheds", Title: "Watersheds",
		Server: wbdServer, Layer: 5,
		Geometry: "polygon",
		Keep:     []string{"objectid", "huc10", "name"},
		IDOf:     hucID("huc10"),
		ZoneOf:   hucZone("huc10", "Watershed"),
		Label:    "quiet",
	},
	{
		Slug: SlugSubwatersheds, Title: "Subwatersheds",
		Server: wbdServer, Layer: 6,
		Geometry: "polygon",
		Keep:     []string{"objectid", "huc12", "name"},
		IDOf:     hucID("huc12"),
		ZoneOf:   hucZone("huc12", "Subwatershed"),
		Label:    "quiet",
	},
	{
		Slug: "streams", Title: "Streams",
		Server: nhdServer, Layer: 6,
		Geometry: "line",
		Keep:     []string{"OBJECTID", "gnis_name"},
		// Named flowlines only: the layer also carries every unnamed ditch
		// and culvert, which is noise to a reader and no loss to the map.
		Where: "gnis_name IS NOT NULL",
		ZoneOf: func(f Fields) ZoneKey {
			name := strings.TrimSpace(f["gnis_name"])
			if name == "" {
				return ZoneKey{}
			}
			return ZoneKey{Key: slugify(name), Title: name, Subtitle: "Stream"}
		},
		StrokeWidth: 10,
		Label:       "quiet",
		Role:        "water",
	},
	{
		Slug: "waterbodies", Title: "Waterbodies",
		Server: nhdServer, Layer: 12,
		Geometry: "polygon",
		Keep:     []string{"OBJECTID", "GNIS_NAME"},
		// Every pond draws into the basemap; only the named ones earn a
		// zone anyone could search for.
		ZoneOf: func(f Fields) ZoneKey {
			name := strings.TrimSpace(f["GNIS_NAME"])
			if name == "" {
				return ZoneKey{}
			}
			return ZoneKey{Key: slugify(name), Title: name, Subtitle: "Waterbody"}
		},
		Label: "quiet",
		Role:  "water",
	},
}

// hucID reads the hydrologic unit code as the feature's identity: the code
// is nationally unique and survives upstream refreshes, where the layer's
// object ids are load artifacts that churn. A leading zero -- regions one
// through nine -- drops in the number without costing uniqueness.
func hucID(field string) func(Fields) (int64, bool) {
	return func(f Fields) (int64, bool) {
		code, err := strconv.ParseInt(f[field], 10, 64)
		return code, err == nil && code > 0
	}
}

// hucZone folds a WBD layer into one zone per hydrologic unit, titled by
// the unit's own name with the code kept legible beside the grain.
func hucZone(field, grain string) func(Fields) ZoneKey {
	return func(f Fields) ZoneKey {
		code, name := strings.TrimSpace(f[field]), strings.TrimSpace(f["name"])
		if code == "" || name == "" {
			return ZoneKey{}
		}
		return ZoneKey{Key: code, Title: name, Subtitle: grain + " · HUC " + code}
	}
}
