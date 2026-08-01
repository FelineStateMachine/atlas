package main

import (
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// The catalog was once one file holding every location of every game, fetched
// whole at startup and held for the session. Adding a game made the wait
// longer for anyone who never opened it. A bundle instead carries one payload
// per map, so opening Atlas costs the same however many games are installed.
//
// Each map splits three ways by when its parts are needed:
//
//   - maps/<slug>.json  its layers, categories and regions, read when the map opens
//   - maps/<slug>.bin   its locations, packed, read when the map opens
//   - maps/<slug>.text  descriptions and cross-references, read when a pin is opened
//
// Descriptions were half the catalog by weight and are read one pin at a time,
// so they are the part most worth not loading. Locations are overwhelmingly
// numbers, and numbers written as text cost several times what they measure.

// mapDetail is everything needed to draw a map except its locations, which
// travel packed alongside.
type mapDetail struct {
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

// buildPayload splits one map three ways: its layers, categories and regions
// as a detail structure, its locations packed, and its descriptions keyed by
// location.
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
