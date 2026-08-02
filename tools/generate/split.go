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
//   - maps/<slug>.json  its layers and collections, read when the map opens
//   - maps/<slug>.bin   its point features, packed, read when the map opens
//   - maps/<slug>.text  descriptions and cross-references, read when a feature is opened
//
// Descriptions were half the catalog by weight and are read one feature at a
// time, so they are the part most worth not loading. Point features are
// overwhelmingly numbers, and numbers written as text cost several times what
// they measure; shape features are mostly geometry either way, so they ride
// the JSON inline under the collection that declares them.

// worldDetail is everything needed to draw a map except its point features,
// which travel packed alongside. Collections is the map's whole structure in
// one flat array, order significant: the packed payload's owner column
// indexes it.
type worldDetail struct {
	// Grid travels with the layers it describes: a map cut from a window of its
	// own is the only one that carries it, and it is needed exactly when the
	// map is opened.
	Grid        *worldGrid       `json:"grid,omitempty"`
	Lenses      []lens           `json:"lenses"`
	Collections []wireCollection `json:"collections"`
	// Attrs is the map speaking the shared conventions -- its geometry, its
	// marker outset -- for any reader that knows the vocabulary.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Merged is the provenance of any other sources folded into this map.
	// The viewer ignores it; it exists so the merged payload carries its own
	// account.
	Merged []mergedSource `json:"merged,omitempty"`
}

// wireCollection is one collection as the payload spells it. A point
// collection carries no inline features -- its members ride the packed
// payload, owned by this collection's index -- where a path or area
// collection inlines its features whole.
type wireCollection struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Group       string            `json:"group,omitempty"`
	Kind        string            `json:"kind"`
	Icon        string            `json:"icon,omitempty"`
	IconAsset   string            `json:"iconAsset,omitempty"`
	IconPicture bool              `json:"iconPicture,omitempty"`
	Color       string            `json:"color,omitempty"`
	IconColor   string            `json:"iconColor,omitempty"`
	Visible     bool              `json:"visible"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Features    []wireFeature     `json:"features,omitempty"`
}

// wireFeature is one shape feature as the payload inlines it. Its prose
// defers into the text payload behind the hasText marker, exactly the way a
// point feature's does; its attributes stay inline, because a card needs them
// the moment ground is asked about.
type wireFeature struct {
	ID       int64             `json:"id"`
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle,omitempty"`
	HasText  bool              `json:"hasText,omitempty"`
	Parent   *int64            `json:"parent,omitempty"`
	Center   *coordinate       `json:"center,omitempty"`
	Shard    int64             `json:"shard,omitempty"`
	Geometry []geometry        `json:"geometry"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

// locationText holds what only a selected feature needs.
type locationText struct {
	Description string        `json:"d,omitempty"`
	Links       []catalogLink `json:"l,omitempty"`
	// Attrs carries a point feature's own conventions -- its true planetary
	// coordinates, when a source published them -- read when its card opens,
	// like everything else here.
	Attrs map[string]string `json:"a,omitempty"`
}

// buildPayload splits one map three ways: its layers and collections as a
// detail structure, its point features packed, and its descriptions keyed by
// feature -- every kind defers its prose the same way. Feature identifiers
// share one claimed space, so one map never keys two things alike.
//
// Point collections are written first and shape collections after, so a
// packed location's owner is simply the ordinal of its collection among the
// points -- and the wire order is the legend order: categories as the source
// grouped them, then the curated shape collections, then the implicit one.
func buildPayload(m catalogWorld) (worldDetail, []byte, map[string]locationText) {
	detail := worldDetail{Grid: m.Grid, Lenses: m.Lenses, Attrs: m.Attrs, Merged: m.Merged}
	var locations []bundle.Location
	text := make(map[string]locationText)
	for _, collection := range m.Collections {
		if collection.Kind != kindPoint {
			continue
		}
		owner := uint16(len(detail.Collections))
		detail.Collections = append(detail.Collections, listedCollection(collection))
		for _, pin := range collection.Features {
			var member int64
			if pin.Member != nil {
				member = *pin.Member
			}
			locations = append(locations, bundle.Location{
				ID:     pin.ID,
				Title:  pin.Title,
				Lat:    pin.Lat,
				Lng:    pin.Lng,
				Member: member,
				Shard:  pin.Shard,
				Owner:  owner,
			})
			if pin.Description != "" || len(pin.Links) > 0 || len(pin.Attrs) > 0 {
				text[strconv.FormatInt(pin.ID, 10)] = locationText{
					Description: pin.Description,
					Links:       pin.Links,
					Attrs:       pin.Attrs,
				}
			}
		}
	}
	for _, collection := range m.Collections {
		if collection.Kind == kindPoint {
			continue
		}
		listed := listedCollection(collection)
		for _, shape := range collection.Features {
			feature := wireFeature{
				ID:       shape.ID,
				Title:    shape.Title,
				Subtitle: shape.Subtitle,
				Parent:   shape.Parent,
				Center:   shape.Center,
				Shard:    shape.Shard,
				Geometry: shape.Geometry,
				Attrs:    shape.Attrs,
			}
			if shape.Description != "" {
				text[strconv.FormatInt(shape.ID, 10)] = locationText{Description: shape.Description}
				feature.HasText = true
			}
			listed.Features = append(listed.Features, feature)
		}
		detail.Collections = append(detail.Collections, listed)
	}
	return detail, bundle.PackLocations(locations), text
}

// listedCollection is the collection as the wire lists it, features left for
// the caller to fill in or leave packed.
func listedCollection(collection worldCollection) wireCollection {
	return wireCollection{
		ID:          collection.ID,
		Title:       collection.Title,
		Group:       collection.Group,
		Kind:        collection.Kind,
		Icon:        collection.Icon,
		IconAsset:   collection.IconAsset,
		IconPicture: collection.IconPicture,
		Color:       collection.Color,
		IconColor:   collection.IconColor,
		Visible:     collection.Visible,
		Attrs:       collection.Attrs,
	}
}
