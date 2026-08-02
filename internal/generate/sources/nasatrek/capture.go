package nasatrek

import "sort"

// The archived capture, as the crawler wrote it. A Trek capture marries two
// public NASA/USGS publications that know nothing of each other: a global
// equirectangular mosaic served by NASA's Trek tile services, and the IAU
// Gazetteer of Planetary Nomenclature's feature list for the same body. Its
// fields are declared in the order they marshal, and Normalize sorts its lists,
// so the Gazetteer listing its features differently does not masquerade as a new
// capture.

type capture struct {
	Source   string    `json:"source"`
	Body     string    `json:"body"`
	Layer    string    `json:"layer"`
	MapSlug  string    `json:"mapSlug"`
	MapTitle string    `json:"mapTitle"`
	Map      mosaic    `json:"map"`
	Variants []variant `json:"variants,omitempty"`
	Features []feature `json:"features"`
}

// mosaic is the pyramid as captured. MaxZoom counts in the square world -- the
// deepest level actually taken, one above the Trek level it came from, because a
// Trek mosaic is two tiles wide and one tall where the square is one and one.
// LayerTitle names the mosaic the way a person knows it, which is what a lens
// picker shows.
type mosaic struct {
	MaxZoom    int    `json:"maxZoom"`
	Extension  string `json:"extension"`
	LayerTitle string `json:"layerTitle"`
}

// variant is a sibling mosaic of the same ground: another way of seeing the
// body, elevation beside photograph, captured into the same window. Its pyramid
// is its own; siblings need not agree on depth.
type variant struct {
	Layer     string `json:"layer"`
	Title     string `json:"title"`
	MaxZoom   int    `json:"maxZoom"`
	Extension string `json:"extension"`
}

// feature is one Gazetteer entry as the feature list publishes it: the IAU's own
// identifier, planetocentric latitude, east-positive longitude in the
// Gazetteer's 0..360 spelling, diameter in kilometres, and the origin text
// explaining the name -- the one piece of prose the data carries.
type feature struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Code       string  `json:"code"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	DiameterKM float64 `json:"diameterKm"`
	Origin     string  `json:"origin,omitempty"`
}

// normalize puts a capture into its canonical order. The crawler does this
// before archiving; the reader does it again because an archive holds captures
// older than the habit.
func (c *capture) normalize() {
	sort.Slice(c.Variants, func(a, b int) bool { return c.Variants[a].Layer < c.Variants[b].Layer })
	sort.Slice(c.Features, func(a, b int) bool { return c.Features[a].ID < c.Features[b].ID })
}
