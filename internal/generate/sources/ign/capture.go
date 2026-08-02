package ign

import (
	"encoding/json"
	"sort"
)

// The archived capture, as the crawler wrote it: a wikimap's configuration, its
// marker types, and every marker. Its fields are structs and slices only,
// marshalled in declaration order, and normalize sorts its lists, so IGN
// returning the same markers in a different order does not masquerade as a new
// capture. Anything volatile on the page -- build ids, trace headers, timestamps
// -- was never captured; when a capture was taken is the archive's business.

type capture struct {
	Source     string   `json:"source"`
	ObjectSlug string   `json:"objectSlug"`
	MapSlug    string   `json:"mapSlug"`
	GameTitle  string   `json:"gameTitle"`
	MapTitle   string   `json:"mapTitle"`
	Map        sheet    `json:"map"`
	Types      []kind   `json:"types"`
	Markers    []marker `json:"markers"`
}

// sheet is the slice of IGN's page configuration a volume needs. Width and
// height are normalized to the map image: the taller dimension spans 1, so a
// marker's coordinates and the tile pyramid describe the same square world.
type sheet struct {
	Width           float64 `json:"width"`
	Height          float64 `json:"height"`
	MinZoom         int     `json:"minZoom"`
	MaxZoom         int     `json:"maxZoom"`
	InitialLat      float64 `json:"initialLat"`
	InitialLng      float64 `json:"initialLng"`
	BackgroundColor string  `json:"backgroundColor"`
	Tileset         string  `json:"tileset"`
}

// kind is one marker type. Types naming a parent gather under it in the legend;
// the rest share one heading.
type kind struct {
	TypeSlug       string `json:"typeSlug"`
	TypeName       string `json:"typeName"`
	ParentTypeSlug string `json:"parentTypeSlug,omitempty"`
	IconURL        string `json:"iconUrl,omitempty"`
	IconWidth      int    `json:"iconWidth,omitempty"`
	IconHeight     int    `json:"iconHeight,omitempty"`
}

// marker keeps every field IGN serves for a pin, including the ones a volume
// holds back. WikiPage names an article on IGN's own wiki, which an offline
// reader cannot follow, so it stays archived until something can be made of it.
// The raw messages pass through whatever shape IGN used, which their endpoint
// has not settled.
type marker struct {
	ID              string          `json:"id"`
	Lat             float64         `json:"lat"`
	Lng             float64         `json:"lng"`
	MarkerName      string          `json:"markerName"`
	MarkerSlug      string          `json:"markerSlug"`
	TypeSlug        string          `json:"typeSlug"`
	WikiPage        string          `json:"wikiPage,omitempty"`
	IconSlug        json.RawMessage `json:"iconSlug,omitempty"`
	RegionID        json.RawMessage `json:"regionId,omitempty"`
	ChecklistTaskID json.RawMessage `json:"checklistTaskId,omitempty"`
}

func (c *capture) normalize() {
	sort.Slice(c.Types, func(a, b int) bool { return c.Types[a].TypeSlug < c.Types[b].TypeSlug })
	sort.Slice(c.Markers, func(a, b int) bool { return c.Markers[a].ID < c.Markers[b].ID })
}
