package arcgishub

import "sort"

// The archived capture, as the crawler wrote it. A city's capture is one day of
// one open-data hub: the window the city's ground is pictured in, the basemap
// pyramid rendered beside it from that same vector data, and every curated
// dataset's features under their allowlisted fields. Its fields are declared in
// the order they marshal and normalize sorts its lists, so a hub listing its
// rows differently does not masquerade as a new day.

type capture struct {
	Source   string            `json:"source"`
	City     string            `json:"city"`
	Title    string            `json:"title"`
	MapSlug  string            `json:"mapSlug"`
	Window   window            `json:"window"`
	Basemap  mapConfig         `json:"basemap"`
	Datasets []capturedDataset `json:"datasets"`
}

// mapConfig is the basemap pyramid as it was rendered: the deepest level drawn,
// and the extension every tile wears. Nothing here is a promise about what a
// bundle serves -- the deriver decides that -- it is what the renderer was asked
// for, which is the only thing the capture can honestly say.
type mapConfig struct {
	MaxZoom   int    `json:"maxZoom"`
	Extension string `json:"extension"`
}

// capturedDataset is one curated layer's rows, joined back to the curation
// table by slug when the capture is read.
type capturedDataset struct {
	Slug     string    `json:"slug"`
	Features []feature `json:"features"`
}

// feature is one thing the city published: the row identity the layer numbers
// it by, its kept fields spelled as text, and its ground in true degrees.
type feature struct {
	ID       int64    `json:"id"`
	Fields   fields   `json:"fields,omitempty"`
	Geometry geometry `json:"geometry"`
}

// fields is one row's allowlisted attributes, every value spelled as text.
// Curation reads a row through this and nothing else, which is what keeps a
// field name a reviewed decision rather than a guess made at translate time.
type fields map[string]string

// geometry is a feature's ground, normalized at capture to one of three shapes:
// a point, lines in MultiLineString nesting, or rings in MultiPolygon nesting.
// Positions are [longitude, latitude] in WGS84.
type geometry struct {
	Type  string          `json:"type"`
	Point []float64       `json:"point,omitempty"`
	Lines [][][]float64   `json:"lines,omitempty"`
	Rings [][][][]float64 `json:"rings,omitempty"`
}

// The three shapes a captured geometry may declare.
const (
	geometryPoint = "point"
	geometryLines = "lines"
	geometryRings = "rings"
)

// normalize puts a capture into its canonical order. The crawler does this
// before archiving; the reader does it again because an archive holds captures
// older than the habit.
func (c *capture) normalize() {
	sort.Slice(c.Datasets, func(a, b int) bool { return c.Datasets[a].Slug < c.Datasets[b].Slug })
	for _, dataset := range c.Datasets {
		rows := dataset.Features
		sort.Slice(rows, func(a, b int) bool { return rows[a].ID < rows[b].ID })
	}
}
