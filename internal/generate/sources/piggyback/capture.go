package piggyback

import "sort"

// The archived capture, as the crawler wrote it: the map's settings, the
// category tree, every pin, and the tile windows the crawler observed. Piggyback
// publishes no bounds of its own, so the crawler's survey is the only account of
// where the pyramid is drawn. Fields marshal in declaration order and every list
// is sorted by normalize, so unchanged data always hashes to the capture already
// archived.

type capture struct {
	Source     string     `json:"source"`
	GameSlug   string     `json:"gameSlug"`
	MapSlug    string     `json:"mapSlug"`
	GameTitle  string     `json:"gameTitle"`
	MapTitle   string     `json:"mapTitle"`
	Map        sheet      `json:"map"`
	Labels     labels     `json:"labels"`
	Categories []category `json:"categories"`
	Pins       []pin      `json:"pins"`
	Levels     []level    `json:"levels"`
}

// sheet keeps the settings the page ships and the transformation the map's own
// scripts project through. MaxZoom is the deepest level actually captured; the
// premium ceiling rides along as a matter of record.
type sheet struct {
	TileServer     string    `json:"tileServer"`
	MinZoom        int       `json:"minZoom"`
	MaxZoom        int       `json:"maxZoom"`
	PremiumMaxZoom int       `json:"premiumMaxZoom,omitempty"`
	Transform      Transform `json:"transform"`
}

// Transform is Leaflet's affine pair: a point (x, y) in game coordinates draws
// at (a*x + b, c*y + d) on the unit tile at zoom zero. It is exported because
// the verified-transform gate is a table of these and a reader of that table
// needs to see what it is comparing.
type Transform struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
	C float64 `json:"c"`
	D float64 `json:"d"`
}

// labels carries the display names the page's language bundle gives the category
// and type keys, which the data API never repeats.
type labels struct {
	Categories []label `json:"categories"`
	Types      []label `json:"types"`
}

type label struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type category struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Position float64 `json:"position"`
	Types    []kind  `json:"types"`
}

type kind struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Position float64 `json:"position"`
}

// pin is one pin as Piggyback serves it, coordinates included as the strings
// they arrive in.
type pin struct {
	ID          string `json:"id"`
	X           string `json:"x"`
	Y           string `json:"y"`
	CategoryKey string `json:"categoryKey"`
	TypeKey     string `json:"typeKey"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// level is one observed tile window: at Zoom, tiles exist from MinX,MinY through
// MaxX,MaxY inclusive.
type level struct {
	Zoom int `json:"zoom"`
	MinX int `json:"minX"`
	MinY int `json:"minY"`
	MaxX int `json:"maxX"`
	MaxY int `json:"maxY"`
}

func (c *capture) normalize() {
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
