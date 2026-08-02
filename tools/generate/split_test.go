package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// The wire emission mirrors the model: point collections first with their
// features packed under their collection's index, shape collections after
// with their features inline, prose deferred to the text payload either way.
func TestBuildPayloadEmitsCollections(t *testing.T) {
	collections, err := normalizeWorld(declaredFixture())
	if err != nil {
		t.Fatal(err)
	}
	detail, packed, text := buildPayload(catalogWorld{Collections: collections})
	if len(detail.Collections) != 3 {
		t.Fatalf("wire lists %d collections, want 3", len(detail.Collections))
	}
	districts, creeks, implicit := detail.Collections[0], detail.Collections[1], detail.Collections[2]
	if districts.Kind != kindArea || len(districts.Features) != 1 || districts.Features[0].Title != "R-5" {
		t.Fatalf("districts emit as %+v", districts)
	}
	if districts.Attrs[semconv.KeyLabelPolicy] != "quiet" {
		t.Fatalf("districts lost their declared attrs on the wire: %v", districts.Attrs)
	}
	if creeks.Kind != kindPath || creeks.Attrs[semconv.KeyStrokeWidthPx] != "10" {
		t.Fatalf("creeks emit as %+v", creeks)
	}
	if implicit.Kind != kindArea || implicit.Features[0].Title != "Old Town" {
		t.Fatalf("implicit collection emits as %+v", implicit)
	}
	seen := map[int64]bool{districts.ID: true, creeks.ID: true, implicit.ID: true}
	if len(seen) != 3 || seen[0] {
		t.Fatalf("collection ids are not distinct and nonzero: %d %d %d",
			districts.ID, creeks.ID, implicit.ID)
	}
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 || len(text) != 0 {
		t.Fatalf("a shapes-only world packed %d locations and %d texts", len(locations), len(text))
	}
}

// A world with both kinds packs its points under the collection indexes the
// wire lists, and defers every kind's prose behind the hasText marker.
func TestBuildPayloadPacksOwnersAndDefersProse(t *testing.T) {
	world := catalogWorld{Collections: []worldCollection{
		{ID: 41, Kind: kindArea, Key: "districts", Title: "Districts", Visible: true,
			Features: []feature{{ID: 9001, Title: "Old Town", Description: "The old quarter."}}},
		{ID: 10, Kind: kindPoint, Title: "Shops", Visible: true,
			Features: []feature{{ID: 100, Title: "Bakery", Description: "Fresh bread."}}},
		{ID: 11, Kind: kindPoint, Title: "Signs", Visible: true,
			Features: []feature{{ID: 101, Title: "Welcome"}}},
	}}
	detail, packed, text := buildPayload(world)
	// Points lead the wire whatever order the model held them in.
	if detail.Collections[0].ID != 10 || detail.Collections[1].ID != 11 || detail.Collections[2].ID != 41 {
		t.Fatalf("wire order = %d %d %d, want points then shapes",
			detail.Collections[0].ID, detail.Collections[1].ID, detail.Collections[2].ID)
	}
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 2 || locations[0].Owner != 0 || locations[1].Owner != 1 {
		t.Fatalf("locations own %+v, want collection indexes 0 and 1", locations)
	}
	if entry, told := text["100"]; !told || entry.Description != "Fresh bread." {
		t.Fatalf("pin text = %+v, %v", entry, told)
	}
	if entry, told := text["9001"]; !told || entry.Description != "The old quarter." {
		t.Fatalf("zone text = %+v, %v", entry, told)
	}
	if !detail.Collections[2].Features[0].HasText {
		t.Fatal("the zone's deferred prose left no hasText marker")
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "quarter") || strings.Contains(string(raw), "bread") {
		t.Fatal("prose rode the detail payload")
	}
}
