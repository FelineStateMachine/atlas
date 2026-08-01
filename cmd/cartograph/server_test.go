package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

func testServer(t *testing.T) (*server, string) {
	t.Helper()
	dir := t.TempDir()
	install(t, dir, "hollowmere-jan.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-01-01T00:00:00Z",
	})
	install(t, dir, "hollowmere-feb.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-02-01T00:00:00Z",
		Worlds: []bundletest.WorldSpec{{
			Slug:   "overworld",
			Pins:   []bundletest.Pin{{Title: "Gate"}, {Title: "Well"}},
			Merged: fixtureMerged(),
		}},
	})
	return newServer(&library{dir: dir}, &workshop{bundles: dir}), dir
}

func newTestSite(t *testing.T, s *server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	response, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func TestPages(t *testing.T) {
	server, _ := testServer(t)
	ts := newTestSite(t, server)

	status, body := get(t, ts, "/")
	if status != http.StatusOK {
		t.Fatalf("collection answered %d", status)
	}
	for _, want := range []string{"Hollowmere", "hollowmere", "ign-wiki"} {
		if !strings.Contains(body, want) {
			t.Errorf("collection page misses %q", want)
		}
	}

	status, body = get(t, ts, "/volume/hollowmere")
	if status != http.StatusOK {
		t.Fatalf("volume page answered %d", status)
	}
	for _, want := range []string{
		"hollowmere-feb.atlas", "hollowmere-jan.atlas", "serving",
		"annotation", "cartography", "structure", "Old Well", "name 200px away",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("volume page misses %q", want)
		}
	}
	// The serving mark belongs to the newer build alone.
	if strings.Index(body, "hollowmere-feb.atlas") > strings.Index(body, "hollowmere-jan.atlas") {
		t.Error("builds are not newest first")
	}

	if status, _ := get(t, ts, "/volume/nowhere"); status != http.StatusNotFound {
		t.Errorf("unknown game answered %d", status)
	}
	if status, _ := get(t, ts, "/static/style.css"); status != http.StatusOK {
		t.Errorf("stylesheet answered %d", status)
	}

	status, body = get(t, ts, "/sources")
	if status != http.StatusOK {
		t.Fatalf("sources page answered %d", status)
	}
	for _, want := range []string{
		"MapGenie", "IGN Wiki", "Piggyback", "Referer",
		"raster", "icons", "locations", "metadata",
		"Rebuild pyramids", "Recompose bundles",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sources page misses %q", want)
		}
	}
}
