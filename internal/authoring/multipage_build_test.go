package authoring

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMultiPageBuildIsByteIdenticalOnlineOfflineAndExactReplay(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		rw.Header().Set("Content-Type", "application/geo+json")
		if request.URL.Query().Get("page") == "2" {
			_, _ = rw.Write([]byte(samplePointPage("marker-2", "Sample Lookout", 7.5, 3.5, "", 2)))
			return
		}
		next := "http://" + request.Host + request.URL.Path + "?page=2"
		_, _ = rw.Write([]byte(samplePointPage("marker-1", "Sample Marker", 4, 6, next, 2)))
	}))

	projectDir := t.TempDir()
	copySampleProjectFiles(t, projectDir)
	projectPath := filepath.Join(projectDir, "sample-region"+ProjectExtension)
	manifest, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(manifest), "adapter: geojson", "adapter: ogc-api-features", 1)
	updated = strings.Replace(updated, "locator: sample-region.geojson", "locator: "+server.URL, 1)
	updated = strings.Replace(updated, "    mapping:\n      feature-set: places", "    query:\n      collection: sample-points\n      limit: 1\n    mapping:\n      feature-set: places", 1)
	updated = strings.Replace(updated, "presentation:\n", "budgets:\n  features: 4\n  geometry-positions: 11\n\npresentation:\n", 1)
	if err := os.WriteFile(projectPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")
	var onlineEvents []Event
	online, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "online"), Event: func(event Event) { onlineEvents = append(onlineEvents, event) }})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := loadEvidenceSelection(online.Path)
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := selectedSource(selection, "sample-places")
	if !ok || len(selected.Pages) != 2 || selected.Termination == nil || selected.Termination.Kind != TerminationNextExhausted {
		t.Fatalf("selected multi-page evidence = %+v", selected)
	}
	if !allCaptureEventsCached(onlineEvents, false) {
		t.Fatalf("first online build cache events = %+v", onlineEvents)
	}
	tampered := selection
	for index := range tampered.Captures {
		tampered.Captures[index].Pages = append([]SelectedPage(nil), tampered.Captures[index].Pages...)
		if tampered.Captures[index].Source == "sample-places" {
			tampered.Captures[index].Pages[1].Locator += "&changed=true"
		}
	}
	if _, err := replayPlanEvidence(online.Plan, tampered, OpenCache(cacheDir)); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("tampered ordered page selection = %v", err)
	}
	var repeatedEvents []Event
	repeated, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "repeated"), Event: func(event Event) { repeatedEvents = append(repeatedEvents, event) }})
	if err != nil {
		t.Fatal(err)
	}
	if !allCaptureEventsCached(repeatedEvents, true) {
		t.Fatalf("repeated online build cache events = %+v", repeatedEvents)
	}
	offline, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "offline"), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "replay"), ReplayPath: online.Path})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatalf("source calls = %d, want only the two pages from each online build", calls.Load())
	}
	want, err := os.ReadFile(online.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{repeated.Path, offline.Path, replayed.Path} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("build %s is not byte-identical to online build", path)
		}
	}
	tooSmall := strings.Replace(updated, "features: 4", "features: 3", 1)
	if err := os.WriteFile(projectPath, []byte(tooSmall), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, options := range []BuildOptions{
		{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "online-over")},
		{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "offline-over"), Offline: true},
		{ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: filepath.Join(t.TempDir(), "replay-over"), ReplayPath: online.Path},
	} {
		if _, err := Build(context.Background(), options); err == nil || !strings.Contains(err.Error(), "emits 4 features, budget is 3") {
			t.Fatalf("semantic budget mode error = %v", err)
		}
	}
	server.Close()
}

func allCaptureEventsCached(events []Event, want bool) bool {
	found := false
	for _, event := range events {
		if event.Stage != "capture" {
			continue
		}
		found = true
		if event.Cached != want {
			return false
		}
	}
	return found
}

func samplePointPage(id, title string, x, y float64, next string, matched int) string {
	links := ""
	if next != "" {
		links = fmt.Sprintf(`,"links":[{"rel":"next","href":%q}]`, next)
	}
	return fmt.Sprintf(`{"type":"FeatureCollection","numberMatched":%d,"numberReturned":1,"features":[{"type":"Feature","id":%q,"properties":{"name":%q,"kind":"landmark","featured":true,"rank":1,"score":9.5,"badge":"AQID","category":"sample.category.marker","area":"area-1"},"geometry":{"type":"Point","coordinates":[%g,%g]}}]%s}`, matched, id, title, x, y, links)
}

func copySampleProjectFiles(t *testing.T, destination string) {
	t.Helper()
	for _, name := range []string{
		"sample-region.atlas-project", "sample-region.geojson", "sample-region-routes.geojson",
		"sample-region-areas.geojson", "sample-region-raster.ppm", "sample-region-symbol.svg",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func selectedSource(selection EvidenceSelection, source string) (SelectedCapture, bool) {
	for _, selected := range selection.Captures {
		if selected.Source == source {
			return selected, true
		}
	}
	return SelectedCapture{}, false
}
