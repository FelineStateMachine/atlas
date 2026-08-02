package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/semconv"
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

// declaredFixture is a capture that declares its collections: an area
// collection and a path collection, a region claiming each, and one region
// claiming nothing, which still folds into the implicit collection.
func declaredFixture() rawMap {
	ring := json.RawMessage(`[[[[0,0],[1,0],[1,1]]]]`)
	lines := json.RawMessage(`[[[0,0],[1,1]]]`)
	return rawMap{
		Collections: []rawCollectionDecl{
			{Key: "districts", Title: "Districts", Attrs: map[string]string{
				semconv.KeyGeometryKind: "area",
				semconv.KeyLabelPolicy:  "quiet",
			}},
			{Key: "creeks", Title: "Creeks", Attrs: map[string]string{
				semconv.KeyGeometryKind:  "path",
				semconv.KeyStrokeWidthPx: "10",
			}},
		},
		Regions: []rawRegion{
			{ID: 1, Title: "R-5", Collection: "districts",
				Features: []rawFeature{{Geometry: geometry{Type: "MultiPolygon", Coordinates: ring}}}},
			{ID: 2, Title: "Big Dry Creek", Collection: "creeks",
				Features: []rawFeature{{Geometry: geometry{Type: "MultiLineString", Coordinates: lines}}}},
			{ID: 3, Title: "Old Town",
				Features: []rawFeature{{Geometry: geometry{Type: "MultiPolygon", Coordinates: ring}}}},
		},
	}
}

// Declared collections bucket the regions that claim them, in declaration
// order after the point collections, the implicit collection last -- and the
// v2 zone emission walks that order, which for a producer that declares in
// its own region order is exactly the raw order the wire always said.
func TestNormalizeWorldBucketsDeclaredCollections(t *testing.T) {
	collections, err := normalizeWorld(declaredFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 3 {
		t.Fatalf("normalized into %d collections, want 3: %+v", len(collections), collections)
	}
	districts, creeks, implicit := collections[0], collections[1], collections[2]
	if districts.Key != "districts" || districts.Kind != kindArea || districts.Title != "Districts" {
		t.Fatalf("first collection is %+v", districts)
	}
	if districts.Attrs[semconv.KeyLabelPolicy] != "quiet" {
		t.Fatalf("districts lost its declared attrs: %v", districts.Attrs)
	}
	if creeks.Key != "creeks" || creeks.Kind != kindPath {
		t.Fatalf("second collection is %+v", creeks)
	}
	if implicit.Key != regionsCollectionKey || implicit.Kind != kindArea {
		t.Fatalf("last collection is %+v, want the implicit one", implicit)
	}
	for at, want := range []string{"R-5", "Big Dry Creek", "Old Town"} {
		if len(collections[at].Features) != 1 || collections[at].Features[0].Title != want {
			t.Fatalf("collection %d holds %+v, want one feature %q",
				at, collections[at].Features, want)
		}
	}
	m := catalogWorld{Collections: collections}
	ids := []int64{}
	for _, z := range m.v2Zones() {
		ids = append(ids, z.ID)
	}
	if !reflect.DeepEqual(ids, []int64{1, 2, 3}) {
		t.Fatalf("v2 zones emit as %v, want the raw region order 1,2,3", ids)
	}
}

func TestNormalizeWorldRefusals(t *testing.T) {
	refuse := func(name, wantWords string, mutate func(*rawMap)) {
		t.Helper()
		raw := declaredFixture()
		mutate(&raw)
		_, err := normalizeWorld(raw)
		if err == nil {
			t.Fatalf("%s: normalized anyway", name)
		}
		if !strings.Contains(err.Error(), wantWords) {
			t.Fatalf("%s: error %q says nothing of %q", name, err, wantWords)
		}
	}
	refuse("undeclared collection", "never declares", func(raw *rawMap) {
		raw.Regions[0].Collection = "ghost"
	})
	// The sniff is dead: an undeclared line is refused, not guessed at.
	refuse("implicit lines", "declared as a path collection", func(raw *rawMap) {
		raw.Regions[1].Collection = ""
	})
	refuse("path without stroke", "stroke", func(raw *rawMap) {
		delete(raw.Collections[1].Attrs, semconv.KeyStrokeWidthPx)
	})
	refuse("kind unspoken", "geometry kind", func(raw *rawMap) {
		delete(raw.Collections[0].Attrs, semconv.KeyGeometryKind)
	})
	refuse("kind foreign", "geometry kind", func(raw *rawMap) {
		raw.Collections[0].Attrs[semconv.KeyGeometryKind] = "point"
	})
	refuse("key doubled", "declared twice", func(raw *rawMap) {
		raw.Collections[1].Key = "districts"
		raw.Regions[1].Collection = "districts"
	})
	refuse("key reserved", "reserves", func(raw *rawMap) {
		raw.Collections[0].Key = regionsCollectionKey
		raw.Regions[0].Collection = regionsCollectionKey
	})
}
