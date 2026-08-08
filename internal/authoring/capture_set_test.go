package authoring

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureSetFollowsPagesAndReplaysOfflineAtomically(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		rw.Header().Set("Content-Type", "application/geo+json")
		switch request.URL.Query().Get("page") {
		case "2":
			_, _ = rw.Write([]byte(`{"type":"FeatureCollection","numberMatched":2,"numberReturned":1,"features":[{"type":"Feature","id":"second","properties":{},"geometry":null}]}`))
		default:
			_, _ = fmt.Fprintf(rw, `{"type":"FeatureCollection","numberMatched":2,"numberReturned":1,"features":[{"type":"Feature","id":"first","properties":{},"geometry":null}],"links":[{"rel":"next","href":%q}]}`, serverURL(request)+"?page=2")
		}
	}))
	defer server.Close()

	cache := OpenCache(t.TempDir())
	cache.now = func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) }
	request := featureSetRequest("sample-roads", "ogc-api-features", server.URL)
	registry := defaultAdapterRegistry()
	set, cached, err := cache.AcquireSet(context.Background(), []Request{request}, registry, CaptureSetOptions{MaxRequests: 8})
	if err != nil {
		t.Fatal(err)
	}
	if cached || len(set.Roots) != 1 || len(set.Roots[0].Pages) != 2 {
		t.Fatalf("online set = cached %v, %+v", cached, set)
	}
	if set.Roots[0].Termination.Kind != TerminationNextExhausted || set.Roots[0].Termination.Returned != 2 || set.Roots[0].Termination.Matched == nil || *set.Roots[0].Termination.Matched != 2 {
		t.Fatalf("termination = %+v", set.Roots[0].Termination)
	}
	if calls.Load() != 2 {
		t.Fatalf("online calls = %d, want 2", calls.Load())
	}

	offline, cached, err := cache.AcquireSet(context.Background(), []Request{request}, registry, CaptureSetOptions{Offline: true, MaxRequests: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !cached || offline.Digest != set.Digest || calls.Load() != 2 {
		t.Fatalf("offline set = cached %v digest %s calls %d", cached, offline.Digest, calls.Load())
	}
	selected, err := cache.SelectSet(set.ID, set.Digest)
	if err != nil || selected.Digest != set.Digest {
		t.Fatalf("select exact set = %+v, %v", selected, err)
	}
}

func TestCaptureSetRefusesIncompletePaginationWithoutPublishingASet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.Header().Set("Content-Type", "application/geo+json")
		if request.URL.Query().Get("page") == "2" {
			_, _ = rw.Write([]byte(`{"type":"FeatureCollection","numberMatched":3,"numberReturned":1,"features":[{"type":"Feature","id":"second","properties":{},"geometry":null}]}`))
			return
		}
		_, _ = fmt.Fprintf(rw, `{"type":"FeatureCollection","numberMatched":3,"numberReturned":1,"features":[{"type":"Feature","id":"first","properties":{},"geometry":null}],"links":[{"rel":"next","href":%q}]}`, serverURL(request)+"?page=2")
	}))
	defer server.Close()

	cache := OpenCache(t.TempDir())
	request := featureSetRequest("sample-roads", "ogc-api-features", server.URL)
	setID := captureSetID([]Request{request})
	_, _, err := cache.AcquireSet(context.Background(), []Request{request}, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete set = %v", err)
	}
	if _, err := cache.LatestSet(setID); !os.IsNotExist(err) {
		t.Fatalf("partial set became selectable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache.Root(), "capture-sets")); !os.IsNotExist(err) {
		t.Fatalf("partial set manifest residue = %v", err)
	}
}

func TestCaptureSetRefusesContinuationCycles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.Header().Set("Content-Type", "application/geo+json")
		_, _ = fmt.Fprintf(rw, `{"type":"FeatureCollection","numberReturned":0,"features":[],"links":[{"rel":"next","href":%q}]}`, serverURL(request))
	}))
	defer server.Close()

	request := featureSetRequest("sample-roads", "ogc-api-features", server.URL)
	_, _, err := OpenCache(t.TempDir()).AcquireSet(context.Background(), []Request{request}, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("continuation cycle = %v", err)
	}
}

func TestCaptureSetDoesNotPublishWhenAnyPlannedRootFails(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.bin")
	if err := os.WriteFile(firstPath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	requests := []Request{
		assetSetRequest("sample-assets", "first", firstPath),
		assetSetRequest("sample-assets", "missing", filepath.Join(root, "missing.bin")),
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	setID := captureSetID(requests)
	_, _, err := cache.AcquireSet(context.Background(), requests, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("failed multi-root set = %v", err)
	}
	if _, err := cache.LatestSet(setID); !os.IsNotExist(err) {
		t.Fatalf("partial multi-root set became selectable: %v", err)
	}
}

func TestPlanReportsCachedOnlyAfterTheWholeSetIsPublished(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"type":"FeatureCollection","features":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	request := featureSetRequest("sample-features", "geojson", source)
	plan := Plan{Budgets: defaultBuildBudgets, Requests: []PlannedRequest{{Request: request}}}
	if _, _, err := cache.Acquire(context.Background(), request, false); err != nil {
		t.Fatal(err)
	}
	markCachedCaptureSets(&plan, cache)
	if plan.Cached != 0 || plan.Requests[0].Cached {
		t.Fatal("an individually cached page was reported as a complete set")
	}
	if _, _, err := cache.AcquireSet(context.Background(), []Request{request}, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8}); err != nil {
		t.Fatal(err)
	}
	markCachedCaptureSets(&plan, cache)
	if plan.Cached != 1 || !plan.Requests[0].Cached {
		t.Fatal("published capture set was not reported as cached")
	}
}

func TestCaptureSetDigestIsIndependentOfPlannedRootOrder(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "first.bin"), filepath.Join(root, "second.bin")}
	for index, path := range paths {
		if err := os.WriteFile(path, []byte{byte(index + 1)}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	requests := []Request{
		assetSetRequest("sample-assets", "first", paths[0]),
		assetSetRequest("sample-assets", "second", paths[1]),
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	first, cached, err := cache.AcquireSet(context.Background(), []Request{requests[1], requests[0]}, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8})
	if err != nil || cached {
		t.Fatalf("first ordered set = cached %v, %v", cached, err)
	}
	second, cached, err := cache.AcquireSet(context.Background(), requests, defaultAdapterRegistry(), CaptureSetOptions{MaxRequests: 8})
	if err != nil || !cached {
		t.Fatalf("reordered set = cached %v, %v", cached, err)
	}
	if first.ID != second.ID || first.Digest != second.Digest || first.Roots[0].Acquisition >= first.Roots[1].Acquisition {
		t.Fatalf("capture set ordering differs: first %+v second %+v", first, second)
	}
}

func featureSetRequest(source, adapter, locator string) Request {
	request := Request{
		Kind: RequestFeatures, Source: source, Adapter: adapter, Locator: locator,
		IdentityLocator: locator, MediaType: "application/geo+json", MaxBytes: 1 << 20,
	}
	finalizeRequest(&request)
	return request
}

func assetSetRequest(source, asset, locator string) Request {
	request := Request{
		Kind: RequestAsset, Source: source, Adapter: "asset-file", Asset: asset,
		Locator: locator, IdentityLocator: locator, MediaType: "application/octet-stream", MaxBytes: 1 << 20,
	}
	finalizeRequest(&request)
	return request
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}
