package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/bundle"
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
	// ID is the numeric identity the embedded tree keys its payload files by.
	// Bundles key everything by slug and leave it unset.
	ID int64 `json:"id,omitempty"`
	// Grid travels with the layers it describes: a map cut from a window of its
	// own is the only one that carries it, and it is needed exactly when the
	// map is opened.
	Grid     *mapGrid       `json:"grid,omitempty"`
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
	detail, packed, text := buildPayload(m)
	detail.ID = m.ID

	if err := writeJSONFile(filepath.Join(payloadDir, name+".json"), detail); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(payloadDir, name+".bin"), packed, 0o644); err != nil {
		return fmt.Errorf("write locations: %w", err)
	}
	return writeJSONFile(filepath.Join(payloadDir, name+".text"), text)
}

// buildPayload splits one map three ways: its layers, categories and regions
// as a detail structure, its locations packed, and its descriptions keyed by
// location. Both destinations -- the embedded tree and the game's bundle --
// are written from this one split.
func buildPayload(m catalogMap) (mapDetail, []byte, map[string]locationText) {
	// Categories keep their identity; their locations travel packed, each
	// carrying the position of its category in this same flattened order.
	detail := mapDetail{Grid: m.Grid, Variants: m.Variants, Zones: m.Zones}
	var locations []bundle.Location
	text := make(map[string]locationText)
	var ordinal uint16
	for _, group := range m.Groups {
		listed := catalogGroup{ID: group.ID, Title: group.Title}
		for _, category := range group.Categories {
			stripped := category
			stripped.Locations = nil
			listed.Categories = append(listed.Categories, stripped)
			for _, location := range category.Locations {
				var region int64
				if location.RegionID != nil {
					region = *location.RegionID
				}
				locations = append(locations, bundle.Location{
					ID:     location.ID,
					Title:  location.Title,
					Lat:    location.Latitude,
					Lng:    location.Longitude,
					Region: region,
					Shard:  location.Shard,
					Owner:  ordinal,
				})
				if location.Description != "" || len(location.Links) > 0 {
					text[strconv.FormatInt(location.ID, 10)] = locationText{
						Description: location.Description,
						Links:       location.Links,
					}
				}
			}
			ordinal++
		}
		detail.Groups = append(detail.Groups, listed)
	}
	return detail, bundle.PackLocations(locations), text
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
