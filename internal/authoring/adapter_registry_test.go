package authoring

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdapterRegistryIsTheSingleVersionAuthority(t *testing.T) {
	registry := defaultAdapterRegistry()
	tests := map[string]string{
		"geojson": "geojson/v1", "arcgis-feature-service": "arcgis-feature-service/v2",
		"ogc-api-features": "ogc-api-features/v2", "raster-file": "raster-file/v1",
		"xyz": "xyz/v1", "wmts": "wmts-rest/v1", "asset-file": "asset-file/v1",
	}
	for name, version := range tests {
		adapter, err := registry.Lookup(name)
		if err != nil || adapter.Version() != version {
			t.Fatalf("adapter %s = %v, %v", name, adapter, err)
		}
	}
	if _, err := registry.Lookup("unknown"); err == nil || !strings.Contains(err.Error(), "unknown adapter") {
		t.Fatalf("unknown adapter = %v", err)
	}
}

func TestArcGISContinuationUsesStableOffsets(t *testing.T) {
	request := featureSetRequest("sample-roads", "arcgis-feature-service", "https://example.invalid/FeatureServer/0/query?f=geojson&resultRecordCount=2")
	adapter, err := defaultAdapterRegistry().Lookup(request.Adapter)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := adapter.Next(request, []byte(`{"type":"FeatureCollection","exceededTransferLimit":true,"features":[{"id":"a"},{"id":"b"}]}`), &PageProgress{})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Done || !strings.Contains(decision.Next.IdentityLocator, "resultOffset=2") || decision.Returned != 2 {
		t.Fatalf("ArcGIS decision = %+v", decision)
	}
}

func TestArcGISPaginationRefusesOverlapAndMissingContinuations(t *testing.T) {
	request := featureSetRequest("sample-roads", "arcgis-feature-service", "https://example.invalid/FeatureServer/0/query?f=geojson&resultRecordCount=2")
	adapter, err := defaultAdapterRegistry().Lookup(request.Adapter)
	if err != nil {
		t.Fatal(err)
	}
	progress := PageProgress{SeenIDs: make(map[string]bool)}
	first, err := adapter.Next(request, []byte(`{"type":"FeatureCollection","exceededTransferLimit":true,"features":[{"id":1},{"id":2}]}`), &progress)
	if err != nil {
		t.Fatal(err)
	}
	progress.Returned = first.Returned
	if _, err := adapter.Next(first.Next, []byte(`{"type":"FeatureCollection","features":[{"id":2},{"id":3}]}`), &progress); err == nil || !strings.Contains(err.Error(), "repeats object ID") {
		t.Fatalf("overlapping page = %v", err)
	}

	progress = PageProgress{Returned: first.Returned, SeenIDs: map[string]bool{"1": true, "2": true}, LastID: "2"}
	if _, err := adapter.Next(first.Next, []byte(`{"type":"FeatureCollection","features":[]}`), &progress); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing continuation = %v", err)
	}
}

func TestOGCPaginationRefusesDuplicateFeatureIDsAcrossPages(t *testing.T) {
	request := featureSetRequest("sample-roads", "ogc-api-features", "https://example.invalid/collections/sample/items")
	adapter, err := defaultAdapterRegistry().Lookup(request.Adapter)
	if err != nil {
		t.Fatal(err)
	}
	progress := PageProgress{RootLocator: request.IdentityLocator, SeenIDs: make(map[string]bool)}
	first, err := adapter.Next(request, []byte(`{"type":"FeatureCollection","numberReturned":1,"features":[{"id":"same"}],"links":[{"rel":"next","href":"?page=2"}]}`), &progress)
	if err != nil {
		t.Fatal(err)
	}
	progress.Returned = first.Returned
	if _, err := adapter.Next(first.Next, []byte(`{"type":"FeatureCollection","numberReturned":1,"features":[{"id":"same"}]}`), &progress); err == nil || !strings.Contains(err.Error(), "repeats object ID") {
		t.Fatalf("duplicate OGC page = %v", err)
	}
}

func TestArcGISPlanAlwaysCarriesTheConfiguredSpatialEnvelope(t *testing.T) {
	project, err := LoadProject(filepath.Join("..", "..", "examples", "sample-region"+ProjectExtension))
	if err != nil {
		t.Fatal(err)
	}
	source := project.Sources[0]
	source.Adapter = "arcgis-feature-service"
	source.Locator = "https://example.invalid/FeatureServer"
	source.Query.Layer = 2
	source.Query.Limit = 500
	source.Query.ObjectID = "sample_id"
	source.Query.SpatialReference = 4326
	request, err := featureRequest(project, source)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(request.IdentityLocator)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	extent, err := sourceExtent(source.Mapping.Geometry, project.Target.CoordinateSpace)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/FeatureServer/2/query" || query.Get("geometry") != extentString(extent) || query.Get("geometryType") != "esriGeometryEnvelope" || query.Get("spatialRel") != "esriSpatialRelIntersects" || query.Get("resultRecordCount") != "500" || query.Get("resultOffset") != "0" || query.Get("orderByFields") != "sample_id ASC" || query.Get("inSR") != "4326" || query.Get("outSR") != "4326" {
		t.Fatalf("ArcGIS request = %s", request.IdentityLocator)
	}
}

func TestArcGISPlanRefusesAnImplicitUnknownSpatialReference(t *testing.T) {
	project, err := LoadProject(filepath.Join("..", "..", "examples", "sample-region"+ProjectExtension))
	if err != nil {
		t.Fatal(err)
	}
	source := project.Sources[0]
	source.Adapter = "arcgis-feature-service"
	source.Locator = "https://example.invalid/FeatureServer/0"
	if _, err := featureRequest(project, source); err == nil || !strings.Contains(err.Error(), "spatial-reference") {
		t.Fatalf("implicit unknown ArcGIS spatial reference = %v", err)
	}

	source.Mapping.Geometry.SourceSpace = "EPSG:4326"
	request, err := featureRequest(project, source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.IdentityLocator, "inSR=4326") || !strings.Contains(request.IdentityLocator, "outSR=4326") {
		t.Fatalf("recognized spatial reference request = %s", request.IdentityLocator)
	}
}
