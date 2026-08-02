package mapgenie

import "encoding/json"

// The archived capture, as MapGenie's map endpoint spells it. These are the
// only declarations in Atlas that carry MapGenie's field names, and they carry
// them because bytes on disk are not negotiable: the archive holds years of
// responses in exactly this shape, and reading them means naming their fields.
//
// Only what the interchange document needs is declared. A capture holds a great
// deal else -- marketing copy, asset URLs, tracking configuration, an embedded
// copy of the game record on every map -- and the way to keep it out of Atlas is
// to not have a field for it.

type rawMap struct {
	ID               int64       `json:"id"`
	Title            string      `json:"title"`
	Slug             string      `json:"slug"`
	InitialLatitude  float64     `json:"initial_latitude"`
	InitialLongitude float64     `json:"initial_longitude"`
	Config           rawConfig   `json:"config"`
	Game             rawGame     `json:"game"`
	Groups           []rawGroup  `json:"groups"`
	Regions          []rawRegion `json:"regions"`
}

type rawGame struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type rawConfig struct {
	TileSets []rawTileSet `json:"tile_sets"`
}

// rawTileSet names one raster of a map. Its zoom range, bounds and encoding are
// deliberately not read: what a bundle carries is the derived pyramid's account
// of itself, not the publisher's account of what it offered.
type rawTileSet struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// rawGroup is a legend section. It is a heading and a colour default, and
// nothing hangs off it that a category does not restate.
type rawGroup struct {
	ID         int64         `json:"id"`
	Title      string        `json:"title"`
	Color      string        `json:"color"`
	IconColor  string        `json:"icon_color"`
	Categories []rawCategory `json:"categories"`
}

// rawCategory is one legend row, one point layer and one icon slot at once.
type rawCategory struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Icon is a key into the archived artwork, not a file name.
	Icon      string `json:"icon"`
	Color     string `json:"color"`
	IconColor string `json:"icon_color"`
	// DisplayType is MapGenie's own render field. It reaches exactly one line of
	// this package, where it is spoken as atlas.render.as, and goes no further.
	DisplayType string        `json:"display_type"`
	Visible     bool          `json:"visible"`
	Locations   []rawLocation `json:"locations"`
}

type rawLocation struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Coordinates arrive as JSON numbers on some maps and as quoted strings on
	// others, so they are held raw and read by number.
	Latitude  json.RawMessage `json:"latitude"`
	Longitude json.RawMessage `json:"longitude"`
	// RegionID is the region whose ground this pin stands on.
	RegionID *int64 `json:"region_id"`
}

// rawRegion is a named piece of ground, standing outside the legend entirely.
type rawRegion struct {
	ID             int64           `json:"id"`
	Title          string          `json:"title"`
	Subtitle       string          `json:"subtitle"`
	Description    string          `json:"description"`
	ParentRegionID *int64          `json:"parent_region_id"`
	CenterX        json.RawMessage `json:"center_x"`
	CenterY        json.RawMessage `json:"center_y"`
	Features       []rawFeature    `json:"features"`
}

type rawFeature struct {
	Geometry rawGeometry `json:"geometry"`
}

type rawGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}
