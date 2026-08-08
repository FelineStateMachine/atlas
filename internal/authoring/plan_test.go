package authoring

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquisitionIdentityIsSharedAcrossBindingsAndVersionsInputs(t *testing.T) {
	first := Request{
		Kind: RequestFeatures, Source: "sample-places", Adapter: "geojson",
		IdentityLocator: "https://example.invalid/sample-region.geojson", MediaType: "application/geo+json",
	}
	second := first
	second.Source = "alternate-places"
	finalizeRequest(&first)
	finalizeRequest(&second)
	if first.AcquisitionID != second.AcquisitionID {
		t.Fatalf("same acquisition has different identities: %s != %s", first.AcquisitionID, second.AcquisitionID)
	}
	if first.ID == second.ID {
		t.Fatal("distinct source bindings share a request identity")
	}
	changed := first
	changed.MediaType = "application/json"
	finalizeRequest(&changed)
	if changed.AcquisitionID == first.AcquisitionID {
		t.Fatal("Accept/media type did not change acquisition identity")
	}
}

func TestMappingAndPresentationChangesReuseAcquisitionEvidence(t *testing.T) {
	project, err := LoadProject(filepath.Join("..", "..", "examples", "sample-region"+ProjectExtension))
	if err != nil {
		t.Fatal(err)
	}
	first, err := PlanProject(project, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project.Sources[0].Mapping.FeatureTitle = "properties.alternate_name"
	project.Presentation.Styles[0].Fill = "#123456"
	second, err := PlanProject(project, nil, first.Output)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProjectDigest == second.ProjectDigest {
		t.Fatal("mapping/presentation policy did not change the project digest")
	}
	if len(first.Requests) != len(second.Requests) {
		t.Fatalf("request count changed: %d != %d", len(first.Requests), len(second.Requests))
	}
	for index := range first.Requests {
		if first.Requests[index].ID != second.Requests[index].ID || first.Requests[index].AcquisitionID != second.Requests[index].AcquisitionID {
			t.Fatalf("policy change invalidated evidence request %d", index)
		}
	}
}

func TestWMTSPlansAnExplicitPortableWindow(t *testing.T) {
	project := Project{baseDir: t.TempDir()}
	raster := Raster{
		ID: "background", Name: "Sample Background", Adapter: "wmts",
		Locator:   "https://example.invalid/tiles/{TileMatrix}/{TileRow}/{TileCol}.png",
		MediaType: "image/png", TileSize: 256,
		Levels: []RasterLevel{{Zoom: 0, MinX: 0, MinY: 0, MaxX: 0, MaxY: 0}, {Zoom: 1, MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}},
	}
	if err := validateRaster(raster); err != nil {
		t.Fatal(err)
	}
	requests, err := rasterRequests(project, raster)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 || requests[0].AdapterVersion != "wmts-rest/v1" {
		t.Fatalf("WMTS plan = %d requests, version %q", len(requests), requests[0].AdapterVersion)
	}
	for _, request := range requests {
		if strings.Contains(request.IdentityLocator, "{") || request.AcquisitionID == "" {
			t.Fatalf("unresolved WMTS request: %+v", request)
		}
	}
}
