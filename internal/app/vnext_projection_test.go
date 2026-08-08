package app

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestVNextEarthProjectsToTheEstablishedRendererContract(t *testing.T) {
	t.Parallel()

	legacy, err := bundle.Open(includedEarthPath(t))
	if err != nil {
		t.Fatalf("open v3 Earth: %v", err)
	}
	defer legacy.Close()
	semantic, err := vnext.ImportV3Volume(legacy.Manifest, legacy.ReadEntry)
	if err != nil {
		t.Fatalf("import v3 Earth: %v", err)
	}

	payload, locations, texts, err := presentVNextWorld(semantic.Worlds[0])
	if err != nil {
		t.Fatalf("present vNext Earth: %v", err)
	}
	if len(payload.Collections) != 7 || len(payload.Lenses) != 1 {
		t.Fatalf("projection has %d collections and %d lenses", len(payload.Collections), len(payload.Lenses))
	}
	if len(locations) != 202 {
		t.Fatalf("projection has %d point locations", len(locations))
	}
	if len(texts) != 379 {
		t.Fatalf("projection has %d text records", len(texts))
	}
	countries := projectedCollection(payload, "Countries")
	if countries == nil || len(countries.Features) != 177 {
		t.Fatalf("Countries projection = %#v", countries)
	}
	fiji := projectedFeature(*countries, "Fiji")
	if fiji == nil || len(fiji.Geometry) != 1 || fiji.Geometry[0].Type != "MultiPolygon" {
		t.Fatalf("Fiji projection = %#v", fiji)
	}

	model, err := buildVNextWorld(semantic.Worlds[0])
	if err != nil {
		t.Fatalf("build vNext world: %v", err)
	}
	if len(model.Points) != 202 || len(model.Shapes) != 177 || len(model.Members) != 7 {
		t.Fatalf("runtime model has %d points, %d shapes, %d collections", len(model.Points), len(model.Shapes), len(model.Members))
	}
}

func TestVNextEarthServesTheThreeRendererPayloads(t *testing.T) {
	t.Parallel()

	legacy, err := bundle.Open(includedEarthPath(t))
	if err != nil {
		t.Fatalf("open v3 Earth: %v", err)
	}
	defer legacy.Close()
	semantic, err := vnext.ImportV3Volume(legacy.Manifest, legacy.ReadEntry)
	if err != nil {
		t.Fatalf("import v3 Earth: %v", err)
	}
	world := semantic.Worlds[0]

	payloadBytes, kind, held, err := projectedVNextEntry(world, bundle.WorldEntryName(world.ID, bundle.WorldSuffix))
	if err != nil || !held || kind != "application/json" {
		t.Fatalf("project world entry = %q, %t, %v", kind, held, err)
	}
	var payload worldPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode projected world: %v", err)
	}
	if len(payload.Collections) != 7 || len(payload.Lenses[0].Formats) != 7 {
		t.Fatalf("projected payload = %#v", payload)
	}

	locationBytes, kind, held, err := projectedVNextEntry(world, bundle.WorldEntryName(world.ID, bundle.PackedSuffix))
	if err != nil || !held || kind != "application/octet-stream" {
		t.Fatalf("project locations entry = %q, %t, %v", kind, held, err)
	}
	locations, err := bundle.UnpackLocations(locationBytes)
	if err != nil || len(locations) != 202 {
		t.Fatalf("projected locations = %d, %v", len(locations), err)
	}

	textBytes, kind, held, err := projectedVNextEntry(world, bundle.WorldEntryName(world.ID, bundle.TextSuffix))
	if err != nil || !held || kind != "application/json" {
		t.Fatalf("project text entry = %q, %t, %v", kind, held, err)
	}
	var texts map[string]featureText
	if err := json.Unmarshal(textBytes, &texts); err != nil || len(texts) != 379 {
		t.Fatalf("projected text = %d, %v", len(texts), err)
	}
}

func includedEarthPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate projection test")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "included", "earth-20260803-76633cb323ec.atlas")
}

func projectedCollection(payload worldPayload, title string) *payloadCollection {
	for index := range payload.Collections {
		if payload.Collections[index].Title == title {
			return &payload.Collections[index]
		}
	}
	return nil
}

func projectedFeature(collection payloadCollection, title string) *payloadFeature {
	for index := range collection.Features {
		if collection.Features[index].Title == title {
			return &collection.Features[index]
		}
	}
	return nil
}
