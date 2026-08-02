package main

import (
	"encoding/json"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// document is a reading with one of everything the adaptation has to carry.
func document() doc.Document {
	return doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Volume:  doc.Volume{Slug: "tunic", Title: "TUNIC"},
		Source: doc.Provenance{
			Name: "mapgenie", Label: "MapGenie", IDSpace: doc.IDSpaceNative,
			License: "CC BY-SA", Attribution: "MapGenie",
		},
		Icons: []doc.Icon{{Key: "chest", File: "chest.svg", Data: []byte("<svg/>")}},
		Worlds: []doc.World{{
			ID:     11,
			Slug:   "overworld",
			Title:  "Overworld",
			Center: doc.Position{Lat: 1.25, Lng: -2.5},
			Capture: doc.Capture{
				Kind: "mapgenie-map", ID: 99, Locator: "https://example.invalid/x",
				ContentHash: "abc123", CapturedAt: "2026-07-30T03:57:41.529Z",
			},
			Lenses: []doc.Lens{{Name: "Default", TileSet: "tunic/overworld"}},
			Attrs:  map[string]string{semconv.KeyIconOutset: semconv.OutsetDark},
			Collections: []doc.Collection{
				{
					ID: 7, Key: "chests", Title: "Chests", Group: "Collectibles",
					Kind: doc.KindPoint, Icon: "chest", Color: "#AABBCC", IconColor: "#112233",
					Visible: true,
					Attrs:   map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
					Features: []doc.Feature{{
						ID: 1, Title: "A chest", Subtitle: "Sealed",
						Description: "Behind the waterfall.",
						At:          &doc.Position{Lat: 3, Lng: 4},
						Member:      42,
						Links:       []doc.Link{{Title: "The key", Feature: 2}},
						Attrs:       map[string]string{semconv.KeyGeoLat: "3"},
					}},
				},
				{
					ID: 8, Title: "Zones", Kind: doc.KindArea, Visible: true,
					Features: []doc.Feature{{
						ID: 2, Title: "The ruins", Parent: 3,
						Center:   &doc.Position{Lat: 5, Lng: 6},
						Geometry: []doc.Geometry{{Type: "MultiPolygon", Coordinates: json.RawMessage(`[[[[0,0],[1,0],[1,1],[0,0]]]]`)}},
						Attrs:    map[string]string{semconv.KeyHydroHUC12: "170703010801"},
					}},
				},
			},
		}},
	}
}

var grid = enrich.Grid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192}

// TestAdaptationRoundTrips is the seam's own gate: a document adapted into the
// enrich lane's model and back is the same document, byte for byte.
//
// It is what lets the ⊕ be a mechanical copy rather than a place where lane
// logic hides, and it is the mechanical form of the no-change-no-build law: an
// empty queue cannot produce a different build, because it cannot produce a
// different document.
func TestAdaptationRoundTrips(t *testing.T) {
	before := document()
	after := documentOf(volumeOf(before, grid), before.Source)

	first, err := before.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	second, err := after.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("the round trip changed the document:\n%s\n%s", first, second)
	}
	if err := after.Validate(); err != nil {
		t.Errorf("the round trip produced a document that does not validate: %v", err)
	}
}

func TestAdaptationCarriesWhatTheEnrichLaneNeeds(t *testing.T) {
	volume := volumeOf(document(), grid)
	if volume.Slug != "tunic" || volume.Source.Name != "mapgenie" || volume.Source.Label != "MapGenie" {
		t.Errorf("the volume reads %+v", volume.Source)
	}
	world := volume.World("overworld")
	if world == nil {
		t.Fatal("the world did not travel")
	}
	if world.CapturedAt != "2026-07-30T03:57:41.529Z" || world.Capture.ContentHash != "abc123" {
		t.Errorf("provenance reads %+v", world.Capture)
	}
	if world.Grid != grid {
		t.Errorf("the world is cut from %+v", world.Grid)
	}
	if got := enrich.MergeIdentity(world.Collections[0]); got != "chests" {
		t.Errorf("merge identity reads %q", got)
	}
	if _, feature := world.Feature(1); feature == nil || feature.At == nil || feature.At.Lat != 3 {
		t.Errorf("the point feature reads %+v", feature)
	}
	if _, feature := world.Feature(2); feature == nil || len(feature.Geometry) != 1 {
		t.Errorf("the shape feature reads %+v", feature)
	}
}

// TestEnrichedVolumeComposesBackIntoADocument holds the other half of the seam:
// what an enricher contributed reaches the document composition is handed, and
// the ledger reaches composition's ledger option rather than the document.
func TestEnrichedVolumeComposesBackIntoADocument(t *testing.T) {
	source := document().Source
	volume := volumeOf(document(), grid)
	enrich.OpenOrigin(volume)

	contribution := enrich.Contribution{Enricher: "test", Volume: "tunic", Ops: []enrich.Op{
		{Kind: enrich.OpAddFeature, World: "overworld", Collection: 7,
			NewFeature: &enrich.Feature{ID: 3, Title: "Another chest", At: &enrich.Position{Lat: 7, Lng: 8}}},
		{Kind: enrich.OpAddAsset, Asset: &enrich.Icon{
			Key: "std--maki-monument", File: "std--maki-monument.svg", Data: []byte("<svg/>")}},
		{Kind: enrich.OpLedger, World: "overworld", Account: &enrich.Account{
			Source: "IGN Wiki", Slug: "ign-wiki",
			DonorFeatures: enrich.Counts{Point: 1}, Added: 1,
		}},
	}}
	if err := enrich.Apply(volume, contribution); err != nil {
		t.Fatal(err)
	}
	if err := enrich.GateWorld(&volume.Worlds[0]); err != nil {
		t.Fatal(err)
	}

	document := documentOf(volume, source)
	if err := document.Validate(); err != nil {
		t.Fatalf("the enriched document does not validate: %v", err)
	}
	if got := len(document.Worlds[0].Collections[0].Features); got != 2 {
		t.Errorf("the contributed feature did not reach the document: %d features", got)
	}
	if got := len(document.Icons); got != 2 {
		t.Errorf("the contributed asset did not reach the document: %d icons", got)
	}

	ledger, err := ledgerOf(volume)
	if err != nil {
		t.Fatal(err)
	}
	accounts := ledger["overworld"]
	if len(accounts) != 2 {
		t.Fatalf("the world carries %d accounts", len(accounts))
	}
	var origin, donor enrich.Account
	if err := json.Unmarshal(accounts[0], &origin); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(accounts[1], &donor); err != nil {
		t.Fatal(err)
	}
	if !origin.Origin || origin.DonorFeatures.Point != 1 {
		t.Errorf("the origin account reads %+v; it must say what the world arrived with", origin)
	}
	if donor.Added != 1 || donor.Slug != "ign-wiki" {
		t.Errorf("the donor account reads %+v", donor)
	}
}
