package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The collections model only earns its place if the v2 wire can be rebuilt
// from it without a byte moving: a capture normalized into collections and
// re-emitted must say exactly what the old direct build said. This walks one
// fixture through normalizeWorld and the emission-time reconstruction and
// holds the result to the shapes the old code produced.
func TestNormalizeWorldRoundTripsTheV2Shapes(t *testing.T) {
	oldTown := int64(9001)
	ring := json.RawMessage(`[[[[0,0],[1,0],[1,1]]]]`)
	raw := rawMap{
		Groups: []rawGroup{
			{ID: 1, Title: "Districts", Color: "38344C", IconColor: "ffffff", Categories: []rawCategory{
				{
					ID: 10, Title: "Shops", Icon: "shop", Color: "aabbcc",
					DisplayType: "pin", Visible: true,
					Locations: []rawLocation{{
						ID:          100,
						Title:       "Bakery",
						Description: "Fresh bread.",
						Latitude:    json.RawMessage(`"1.5"`),
						Longitude:   json.RawMessage(`-2.25`),
						RegionID:    &oldTown,
						Attrs:       map[string]string{"atlas.geo.lat": "39.8"},
					}},
				},
				{ID: 11, Title: "Signs", Icon: "sign", DisplayType: "text", Visible: false},
			}},
			{ID: 2, Title: "Nature", Categories: []rawCategory{
				{ID: 12, Title: "Trees", Icon: "tree", Visible: true},
			}},
		},
		Regions: []rawRegion{
			{
				ID: 9001, Title: "Old Town", Subtitle: "OT", Description: "The old quarter.",
				CenterX: json.RawMessage(`"12.5"`),
				CenterY: json.RawMessage(`3.25`),
				Features: []rawFeature{
					{Geometry: geometry{Type: "MultiPolygon", Coordinates: ring}},
					// An empty part never survived the old build either.
					{Geometry: geometry{}},
				},
			},
			// A region whose geometry all came through empty has nothing to
			// draw, and the old build dropped it whole.
			{ID: 9002, Title: "Ghost", Features: []rawFeature{{Geometry: geometry{Type: "Polygon"}}}},
		},
	}

	collections, err := normalizeWorld(raw)
	if err != nil {
		t.Fatal(err)
	}
	m := catalogWorld{Collections: collections}

	wantGroups := []catalogGroup{
		{ID: 1, Title: "Districts", Categories: []catalogCategory{
			{
				ID: 10, Title: "Shops", Icon: "shop", Color: "#AABBCC", IconColor: "#FFFFFF",
				DisplayType: "pin", Visible: true,
				Locations: []catalogLocation{{
					ID: 100, Title: "Bakery", Description: "Fresh bread.",
					Latitude: 1.5, Longitude: -2.25, RegionID: &oldTown,
					Attrs: map[string]string{"atlas.geo.lat": "39.8"},
				}},
			},
			{
				ID: 11, Title: "Signs", Icon: "sign", Color: "#38344C", IconColor: "#FFFFFF",
				DisplayType: "text",
			},
		}},
		{ID: 2, Title: "Nature", Categories: []catalogCategory{
			{ID: 12, Title: "Trees", Icon: "tree", Visible: true},
		}},
	}
	if got := m.v2Groups(); !reflect.DeepEqual(got, wantGroups) {
		t.Fatalf("v2 groups = %+v\nwant %+v", got, wantGroups)
	}

	wantZones := []zone{{
		ID: 9001, Title: "Old Town", Subtitle: "OT", Description: "The old quarter.",
		Center:   &coordinate{Latitude: 3.25, Longitude: 12.5},
		Features: []geometry{{Type: "MultiPolygon", Coordinates: ring}},
	}}
	if got := m.v2Zones(); !reflect.DeepEqual(got, wantZones) {
		t.Fatalf("v2 zones = %+v\nwant %+v", got, wantZones)
	}

	if got := m.pinCount(); got != 1 {
		t.Fatalf("pin count = %d, want 1", got)
	}
	last := collections[len(collections)-1]
	if last.Key != regionsCollectionKey || last.Kind != kindArea {
		t.Fatalf("implicit collection = {key %q, kind %q}, want {%q, %q}",
			last.Key, last.Kind, regionsCollectionKey, kindArea)
	}
}
