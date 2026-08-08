package authoring

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureReusesUnchangedEvidence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"type":"FeatureCollection","features":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return clock }
	request := Request{ID: requestID(Request{Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson", IdentityLocator: "sample-region.geojson"}), Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson", Locator: source, IdentityLocator: "sample-region.geojson", MediaType: "application/geo+json"}
	first, cached, err := cache.Acquire(context.Background(), request, false)
	if err != nil || cached {
		t.Fatalf("first acquire = cached %v, err %v", cached, err)
	}
	clock = clock.Add(time.Hour)
	second, cached, err := cache.Acquire(context.Background(), request, false)
	if err != nil || !cached {
		t.Fatalf("second acquire = cached %v, err %v", cached, err)
	}
	if second != first {
		t.Fatalf("unchanged capture moved: first %+v second %+v", first, second)
	}
}

func TestCaptureRecordsChangedEvidence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return clock }
	request := Request{Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson", Locator: source, IdentityLocator: "sample-region.geojson"}
	request.ID = requestID(request)
	first, _, err := cache.Acquire(context.Background(), request, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	second, cached, err := cache.Acquire(context.Background(), request, false)
	if err != nil || cached {
		t.Fatalf("changed acquire = cached %v, err %v", cached, err)
	}
	if first.Body.SHA256 == second.Body.SHA256 || first.CapturedAt == second.CapturedAt {
		t.Fatalf("change was not recorded: first %+v second %+v", first, second)
	}
}
