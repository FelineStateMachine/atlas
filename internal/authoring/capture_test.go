package authoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureEnforcesRequestBudgetWithoutResidue(t *testing.T) {
	body := strings.Repeat("x", 9)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		flusher, _ := rw.(http.Flusher)
		for _, value := range body {
			_, _ = rw.Write([]byte{byte(value)})
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cache := OpenCache(root)
	request := Request{
		Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson", Locator: server.URL,
		IdentityLocator: "https://example.invalid/sample-region.geojson", MediaType: "application/geo+json", MaxBytes: 8,
	}
	finalizeRequest(&request)
	if _, _, err := cache.Acquire(context.Background(), request, false); err == nil || !strings.Contains(err.Error(), "request budget") {
		t.Fatalf("oversized capture = %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized capture left cache residue: %v", entries)
	}
	request.MaxBytes = int64(len(body))
	if _, _, err := cache.Acquire(context.Background(), request, false); err != nil {
		t.Fatalf("exact-limit capture: %v", err)
	}
}

func TestOfflineCaptureNeverContactsTheSource(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		rw.Header().Set("Content-Type", "application/geo+json")
		_, _ = rw.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer server.Close()
	cache := OpenCache(t.TempDir())
	request := Request{
		Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson", Locator: server.URL,
		IdentityLocator: "https://example.invalid/sample-region.geojson", MediaType: "application/geo+json", MaxBytes: 1024,
	}
	finalizeRequest(&request)
	if _, _, err := cache.Acquire(context.Background(), request, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.Acquire(context.Background(), request, true); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("source calls = %d, want exactly one online call", got)
	}
}

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

func TestCaptureReusesFirstSeenEvidenceWhenBytesReappear(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	cache := OpenCache(filepath.Join(root, "cache"))
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return clock }
	request := Request{
		Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson",
		Locator: source, IdentityLocator: "sample-region.geojson",
	}
	request.ID = requestID(request)

	writeSource := func(body string) {
		t.Helper()
		if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSource(`{"v":1}`)
	first, _, err := cache.Acquire(context.Background(), request, false)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	writeSource(`{"v":2}`)
	if _, _, err := cache.Acquire(context.Background(), request, false); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	writeSource(`{"v":1}`)
	reappeared, cached, err := cache.Acquire(context.Background(), request, false)
	if err != nil || !cached {
		t.Fatalf("reappeared acquire = cached %v, err %v", cached, err)
	}
	if reappeared != first {
		t.Fatalf("reappeared capture moved: first %+v reappeared %+v", first, reappeared)
	}
	selected, err := cache.Select(request.ID, first.Body.SHA256)
	if err != nil || selected != first {
		t.Fatalf("select first capture = %+v, %v", selected, err)
	}
}

func TestConcurrentCaptureReturnsOnePersistedFirstSeenEnvelope(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	var ticks atomic.Int64
	cache.now = func() time.Time {
		return time.Date(2026, 8, 8, 12, 0, 0, int(ticks.Add(1)), time.UTC)
	}
	request := Request{
		Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson",
		Locator: source, IdentityLocator: "sample-region.geojson",
	}
	request.ID = requestID(request)

	type result struct {
		capture Capture
		cached  bool
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			capture, cached, err := cache.Acquire(context.Background(), request, false)
			results <- result{capture: capture, cached: cached, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	var captures []Capture
	cachedCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		captures = append(captures, result.capture)
		if result.cached {
			cachedCount++
		}
	}
	if captures[0] != captures[1] || cachedCount != 1 {
		t.Fatalf("concurrent captures = %+v, cached count %d", captures, cachedCount)
	}
	selected, err := cache.Select(request.ID, captures[0].Body.SHA256)
	if err != nil || selected != captures[0] {
		t.Fatalf("persisted capture = %+v, %v", selected, err)
	}
}

func TestCacheRejectsInvalidHashesWithoutPanicking(t *testing.T) {
	cache := OpenCache(t.TempDir())
	if cache.Has("short") {
		t.Fatal("short request hash reported cached")
	}
	if _, err := cache.Latest("short"); err == nil {
		t.Fatal("Latest accepted a short request hash")
	}
	if _, err := cache.Select(strings.Repeat("a", 64), "../escape"); err == nil {
		t.Fatal("Select accepted a path-like body hash")
	}
	_ = cache.BlobPath(BlobRef{SHA256: "short"})
}

func TestCacheRejectsCorruptCaptureEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, cache *Cache, request Request, capture Capture)
	}{
		{
			name: "malformed envelope",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				t.Helper()
				writeCaptureFile(t, cache, request.ID, capture.Body.SHA256, []byte("{"))
			},
		},
		{
			name: "wrong format",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				capture.Format = "atlas-capture/unknown"
				writeCapture(t, cache, request.ID, capture.Body.SHA256, capture)
			},
		},
		{
			name: "wrong request hash",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				capture.RequestHash = strings.Repeat("b", 64)
				writeCapture(t, cache, request.ID, capture.Body.SHA256, capture)
			},
		},
		{
			name: "wrong capture path",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				wrong := strings.Repeat("b", 64)
				writeCapture(t, cache, request.ID, wrong, capture)
				if err := os.Remove(captureFilePath(cache, request.ID, capture.Body.SHA256)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid captured time",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				capture.CapturedAt = "eventually"
				writeCapture(t, cache, request.ID, capture.Body.SHA256, capture)
			},
		},
		{
			name: "missing media type",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				capture.Body.MediaType = ""
				writeCapture(t, cache, request.ID, capture.Body.SHA256, capture)
			},
		},
		{
			name: "wrong declared length",
			mutate: func(t *testing.T, cache *Cache, request Request, capture Capture) {
				capture.Body.Length++
				writeCapture(t, cache, request.ID, capture.Body.SHA256, capture)
			},
		},
		{
			name: "missing body",
			mutate: func(t *testing.T, cache *Cache, _ Request, capture Capture) {
				if err := os.Remove(cache.BlobPath(capture.Body)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "body hash mismatch",
			mutate: func(t *testing.T, cache *Cache, _ Request, capture Capture) {
				path := cache.BlobPath(capture.Body)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data[0] ^= 1
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, request, capture := capturedFixture(t)
			tt.mutate(t, cache, request, capture)
			if cache.Has(request.ID) {
				t.Fatal("corrupt evidence reported cached")
			}
			if _, err := cache.Latest(request.ID); err == nil {
				t.Fatal("Latest accepted corrupt evidence")
			}
			if _, err := cache.Select(request.ID, capture.Body.SHA256); err == nil && tt.name != "wrong capture path" {
				t.Fatal("Select accepted corrupt evidence")
			}
		})
	}
}

func capturedFixture(t *testing.T) (*Cache, Request, Capture) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"type":"FeatureCollection","features":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := OpenCache(filepath.Join(root, "cache"))
	cache.now = func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) }
	request := Request{
		Kind: RequestFeatures, Source: "sample-features", Adapter: "geojson",
		Locator: source, IdentityLocator: "sample-region.geojson", MediaType: "application/geo+json",
	}
	request.ID = requestID(request)
	capture, _, err := cache.Acquire(context.Background(), request, false)
	if err != nil {
		t.Fatal(err)
	}
	return cache, request, capture
}

func writeCapture(t *testing.T, cache *Cache, requestID, bodySHA string, capture Capture) {
	t.Helper()
	data, err := json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	writeCaptureFile(t, cache, requestID, bodySHA, append(data, '\n'))
}

func writeCaptureFile(t *testing.T, cache *Cache, requestID, bodySHA string, data []byte) {
	t.Helper()
	if err := os.WriteFile(captureFilePath(cache, requestID, bodySHA), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureFilePath(cache *Cache, requestID, bodySHA string) string {
	return filepath.Join(cache.Root(), "captures", requestID[:2], requestID, bodySHA+".json")
}
