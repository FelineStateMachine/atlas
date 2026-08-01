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

// worldDetail is everything needed to draw a map except its locations, which
// travel packed alongside.
type worldDetail struct {
	// Grid travels with the layers it describes: a map cut from a window of its
	// own is the only one that carries it, and it is needed exactly when the
	// map is opened.
	Grid   *worldGrid     `json:"grid,omitempty"`
	Lenses []lens         `json:"lenses"`
	Groups []catalogGroup `json:"groups"`
	Zones  []zone         `json:"zones,omitempty"`
	// Attrs is the map speaking the shared conventions -- its geometry, its
	// marker outset -- for any reader that knows the vocabulary.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Merged is the provenance of any other sources folded into this map.
	// The viewer ignores it; it exists so the merged payload carries its own
	// account.
	Merged []mergedSource `json:"merged,omitempty"`
}

// locationText holds what only a selected pin needs.
type locationText struct {
	Description string        `json:"d,omitempty"`
	Links       []catalogLink `json:"l,omitempty"`
	// Attrs carries a pin's own conventions -- its true planetary
	// coordinates, when a source published them -- read when its card opens,
	// like everything else here.
	Attrs map[string]string `json:"a,omitempty"`
}

// buildPayload splits one map three ways: its layers, categories and regions
// as a detail structure, its locations packed, and its descriptions keyed by
// location -- and by zone, since a zone's prose defers the same way a pin's
// does. Zone and pin identifiers share one claimed space, so one map never
// keys two things alike.
func buildPayload(m catalogWorld) (worldDetail, []byte, map[string]locationText) {
	// Categories keep their identity; their locations travel packed, each
	// carrying the position of its category in this same flattened order.
	detail := worldDetail{Grid: m.Grid, Lenses: m.Lenses, Attrs: m.Attrs, Merged: m.Merged}
	var locations []bundle.Location
	text := make(map[string]locationText)
	for _, z := range m.Zones {
		if z.Description != "" {
			text[strconv.FormatInt(z.ID, 10)] = locationText{Description: z.Description}
			z.HasText = true
		}
		detail.Zones = append(detail.Zones, z)
	}
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
				if location.Description != "" || len(location.Links) > 0 || len(location.Attrs) > 0 {
					text[strconv.FormatInt(location.ID, 10)] = locationText{
						Description: location.Description,
						Links:       location.Links,
						Attrs:       location.Attrs,
					}
				}
			}
			ordinal++
		}
		detail.Groups = append(detail.Groups, listed)
	}
	return detail, bundle.PackLocations(locations), text
}
