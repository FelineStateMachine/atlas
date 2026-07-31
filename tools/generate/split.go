package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

// The catalog was one file holding every location of every game, fetched whole
// at startup and held for the session. Adding a game made the wait longer for
// anyone who never opened it. It is now an index naming what exists, and one
// payload per map, so opening Atlas costs the same however many games it holds.
//
// Each map splits three ways by when its parts are needed:
//
//   - catalog/<id>.json  its layers, categories and regions, read when the map opens
//   - catalog/<id>.bin   its locations, packed, read when the map opens
//   - catalog/<id>.text  descriptions and cross-references, read when a pin is opened
//
// Descriptions were half the catalog by weight and are read one pin at a time,
// so they are the part most worth not loading. Locations are overwhelmingly
// numbers, and numbers written as text cost several times what they measure.

const (
	locationMagic   = "ATLASLOC"
	locationVersion = 1
)

// mapIndex is a map as the index knows it: enough to list it and to open it,
// and nothing that only matters once it is open.
type mapIndex struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	Slug       string     `json:"slug"`
	Parent     string     `json:"parent,omitempty"`
	IconOutset string     `json:"iconOutset,omitempty"`
	Center     coordinate `json:"center"`
	PinCount   int        `json:"pinCount"`
	UpdatedAt  string     `json:"updatedAt"`
}

type gameIndex struct {
	ID    int64      `json:"id"`
	Title string     `json:"title"`
	Slug  string     `json:"slug"`
	Maps  []mapIndex `json:"maps"`
}

type catalogIndex struct {
	Source   string      `json:"source"`
	TileGrid tileGrid    `json:"tileGrid"`
	Games    []gameIndex `json:"games"`
}

// mapDetail is everything needed to draw a map except its locations, which
// travel packed alongside.
type mapDetail struct {
	ID       int64          `json:"id"`
	Variants []variant      `json:"variants"`
	Groups   []catalogGroup `json:"groups"`
	Zones    []zone         `json:"zones,omitempty"`
}

// locationText holds what only a selected pin needs.
type locationText struct {
	Description string        `json:"d,omitempty"`
	Links       []catalogLink `json:"l,omitempty"`
}

func writeCatalog(out catalog, output string) error {
	root := filepath.Dir(output)
	payloadDir := filepath.Join(root, "catalog")
	if err := os.RemoveAll(payloadDir); err != nil {
		return fmt.Errorf("clear map payloads: %w", err)
	}
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		return fmt.Errorf("create map payloads: %w", err)
	}

	index := catalogIndex{Source: out.Source, TileGrid: out.TileGrid}
	for _, game := range out.Games {
		listed := gameIndex{ID: game.ID, Title: game.Title, Slug: game.Slug}
		for _, m := range game.Maps {
			if err := writeMapPayload(payloadDir, m); err != nil {
				return fmt.Errorf("%s / %s: %w", game.Title, m.Title, err)
			}
			listed.Maps = append(listed.Maps, mapIndex{
				ID:         m.ID,
				Title:      m.Title,
				Slug:       m.Slug,
				Parent:     m.Parent,
				IconOutset: m.IconOutset,
				Center:     m.Center,
				PinCount:   m.PinCount,
				UpdatedAt:  m.UpdatedAt,
			})
		}
		index.Games = append(index.Games, listed)
	}

	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	return nil
}

func writeMapPayload(payloadDir string, m catalogMap) error {
	name := strconv.FormatInt(m.ID, 10)

	// Categories keep their identity; their locations travel packed, each
	// carrying the position of its category in this same flattened order.
	detail := mapDetail{ID: m.ID, Variants: m.Variants, Zones: m.Zones}
	var locations []catalogLocation
	var owners []uint16
	var ordinal uint16
	for _, group := range m.Groups {
		listed := catalogGroup{ID: group.ID, Title: group.Title}
		for _, category := range group.Categories {
			stripped := category
			stripped.Locations = nil
			listed.Categories = append(listed.Categories, stripped)
			for _, location := range category.Locations {
				locations = append(locations, location)
				owners = append(owners, ordinal)
			}
			ordinal++
		}
		detail.Groups = append(detail.Groups, listed)
	}

	if err := writeJSONFile(filepath.Join(payloadDir, name+".json"), detail); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(payloadDir, name+".bin"), packLocations(locations, owners), 0o644); err != nil {
		return fmt.Errorf("write locations: %w", err)
	}

	text := make(map[string]locationText, len(locations))
	for _, location := range locations {
		if location.Description == "" && len(location.Links) == 0 {
			continue
		}
		text[strconv.FormatInt(location.ID, 10)] = locationText{
			Description: location.Description,
			Links:       location.Links,
		}
	}
	return writeJSONFile(filepath.Join(payloadDir, name+".text"), text)
}

// packLocations lays the locations out as parallel arrays, four-byte fields
// first so a reader can view each one directly without copying or realigning.
// Coordinates are single precision, which resolves far finer than a game map
// is drawn.
func packLocations(locations []catalogLocation, owners []uint16) []byte {
	count := len(locations)

	titles := make([]byte, 0, count*12)
	offsets := make([]uint32, count+1)
	for index, location := range locations {
		offsets[index] = uint32(len(titles))
		titles = append(titles, location.Title...)
	}
	offsets[count] = uint32(len(titles))

	size := 16 + count*16 + (count+1)*4 + count*2 + len(titles)
	out := make([]byte, size)
	copy(out, locationMagic)
	binary.LittleEndian.PutUint16(out[8:], locationVersion)
	binary.LittleEndian.PutUint32(out[10:], uint32(count))
	// out[14:16] is reserved, and keeps the arrays four-byte aligned.

	at := 16
	put32 := func(value uint32) {
		binary.LittleEndian.PutUint32(out[at:], value)
		at += 4
	}
	for _, location := range locations {
		put32(uint32(int32(location.ID)))
	}
	for _, location := range locations {
		put32(math.Float32bits(float32(location.Latitude)))
	}
	for _, location := range locations {
		put32(math.Float32bits(float32(location.Longitude)))
	}
	for _, location := range locations {
		region := int32(0)
		if location.RegionID != nil {
			region = int32(*location.RegionID)
		}
		put32(uint32(region))
	}
	for _, offset := range offsets {
		put32(offset)
	}
	for _, owner := range owners {
		binary.LittleEndian.PutUint16(out[at:], owner)
		at += 2
	}
	copy(out[at:], titles)
	return out
}

func writeJSONFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
