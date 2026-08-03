// Package island holds the clean-room application to its own account of the
// arrangement -- the state island and the display logic around it -- over the
// public corpus at testdata/corpus.
//
// It lives here rather than beside internal/app for one mechanical reason:
// these tests read the corpus off the disk, and the hostenv analyzer forbids
// internal/app -- test files included -- from importing os or path/filepath,
// which is exactly the rule that keeps the handler portable.
//
// What it holds is the island's own contract (internal/app/island.go,
// docs/app.md §6): the keys it promises, the values that must agree with the
// page it rides in, and counts that must agree with the corpus payloads the
// page was rendered from. The application is driven through its own HTTP
// surface over an in-memory host. Nothing here reaches inside it.
package island

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// corpusDir is where the committed corpus lives, relative to this package.
// The corpus is required: a missing bundle is a broken checkout and fails
// loudly, because a suite that skips is a suite that stopped judging.
const corpusDir = "../../testdata/corpus/bundles"

// ---------------------------------------------------------------------------
// The in-memory host
// ---------------------------------------------------------------------------

// memoryHost is a host with no machine under it: a fixed library, sessions in
// a map, and no picker. It is what hostenv is for.
type memoryHost struct {
	volumes  *memoryVolumes
	sessions hostenv.SessionStore
}

func (h *memoryHost) Volumes() hostenv.VolumeStore   { return h.volumes }
func (h *memoryHost) Sessions() hostenv.SessionStore { return h.sessions }

func (h *memoryHost) PickFile(context.Context) (io.ReadCloser, string, error) {
	return nil, "", hostenv.ErrNotAvailable
}

type memoryVolumes struct{ volumes []hostenv.Volume }

func (s *memoryVolumes) Volumes() []hostenv.Volume { return s.volumes }
func (s *memoryVolumes) Location() string          { return "/library" }
func (s *memoryVolumes) Rescan() ([]string, error) { return nil, nil }

func (s *memoryVolumes) Install(string, io.Reader) (hostenv.Installed, error) {
	return hostenv.Installed{}, os.ErrInvalid
}

// newApp mounts the application over a fresh in-memory host. No static tree
// is mounted, which is the shape the application is required to work in.
func newApp(t *testing.T, volumes ...hostenv.Volume) http.Handler {
	t.Helper()
	host := &memoryHost{
		volumes:  &memoryVolumes{volumes: volumes},
		sessions: hostenv.NewMemorySessions(),
	}
	return app.New(host, app.Options{})
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func post(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// ---------------------------------------------------------------------------
// Corpus volumes
// ---------------------------------------------------------------------------

// corpusVolume stands one corpus bundle up as a servable volume.
//
// The corpus holds canonicalized extractions rather than .atlas files -- the
// archives are not in the repository -- so the packed locations are repacked
// from their JSON extraction through the format's own codec. What the handler
// sees is therefore exactly what it would see out of an archive: a manifest,
// a world payload, and ATLASLOC bytes.
func corpusVolume(t *testing.T, slug string) hostenv.Volume {
	t.Helper()
	dir := filepath.Join(corpusDir, slug)
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("the corpus is missing %s: %v", slug, err)
	}
	var manifest bundle.Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}

	entries := map[string][]byte{}
	for _, world := range manifest.Worlds {
		payload, err := os.ReadFile(filepath.Join(dir, "worlds", world.Slug+".payload.json"))
		if err != nil {
			t.Fatalf("corpus %s world %s: %v", slug, world.Slug, err)
		}
		entries[bundle.WorldEntryName(world.Slug, bundle.WorldSuffix)] = payload

		entries[bundle.WorldEntryName(world.Slug, bundle.PackedSuffix)] =
			repack(t, filepath.Join(dir, "worlds", world.Slug+".locations.json"))

		if text, err := os.ReadFile(filepath.Join(dir, "worlds", world.Slug+".text.json")); err == nil {
			entries[bundle.WorldEntryName(world.Slug, bundle.TextSuffix)] = text
		}
	}
	return &corpusBundle{manifest: manifest, entries: entries}
}

func repack(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		return bundle.PackLocations(nil)
	}
	var held struct {
		Locations []bundle.Location `json:"locations"`
	}
	if err := json.Unmarshal(body, &held); err != nil {
		t.Fatalf("locations %s: %v", path, err)
	}
	return bundle.PackLocations(held.Locations)
}

// corpusBundleOf builds a servable volume out of nothing, for the one test
// whose fixture is not in the public corpus: a payload written in the test
// itself, empty locations, empty prose.
func corpusBundleOf(slug, title, world string) *corpusBundle {
	const stamp = "13d5657ed9038808e5fe12ef44e769b40297e9d720e53376f430f224128f2dfc"
	return &corpusBundle{
		manifest: bundle.Manifest{
			Format:        bundle.Format,
			FormatVersion: bundle.FormatVersion,
			Conventions:   2,
			Volume:        bundle.Volume{Slug: slug, Title: title},
			Version:       bundle.Version{Stamp: stamp, CreatedAt: "2026-01-01T00:00:00Z"},
			TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
			Worlds:        []bundle.WorldEntry{{Slug: world, Title: title, UpdatedAt: "2026-01-01T00:00:00Z"}},
		},
		entries: map[string][]byte{
			bundle.WorldEntryName(world, bundle.WorldSuffix):  []byte(`{"lenses":[],"collections":[]}`),
			bundle.WorldEntryName(world, bundle.PackedSuffix): bundle.PackLocations(nil),
			bundle.WorldEntryName(world, bundle.TextSuffix):   []byte(`{}`),
		},
	}
}

type corpusBundle struct {
	manifest bundle.Manifest
	entries  map[string][]byte
}

func (v *corpusBundle) Manifest() bundle.Manifest { return v.manifest }

func (v *corpusBundle) Open(name string) (io.ReadCloser, int64, error) {
	held, ok := v.entries[name]
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(held)), int64(len(held)), nil
}

// firstWorld is the world a volume opens on when nobody has been anywhere.
func firstWorld(t *testing.T, volume hostenv.Volume) string {
	t.Helper()
	worlds := volume.Manifest().Worlds
	if len(worlds) == 0 {
		t.Fatal("the corpus volume holds no worlds")
	}
	return worlds[0].Slug
}

// ---------------------------------------------------------------------------
// What the corpus itself says
// ---------------------------------------------------------------------------

// The expectations below are derived from the corpus rather than copied into
// the tests, so a re-extraction that renumbers a feature moves the question
// and the answer together. What is pinned is the relationship: the page and
// the island must agree with the payload they were rendered from.

// corpusWorld is the slice of a payload the tests read expectations out of.
type corpusWorld struct {
	Collections []corpusCollection `json:"collections"`
}

type corpusCollection struct {
	ID       json.Number       `json:"id"`
	Title    string            `json:"title"`
	Kind     string            `json:"kind"`
	Group    string            `json:"group"`
	Attrs    map[string]string `json:"attrs"`
	Features []corpusFeature   `json:"features"`
}

type corpusFeature struct {
	ID    json.Number `json:"id"`
	Title string      `json:"title"`
}

func readCorpusWorld(t *testing.T, slug, world string) corpusWorld {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(corpusDir, slug, "worlds", world+".payload.json"))
	if err != nil {
		t.Fatalf("the corpus is missing %s/%s: %v", slug, world, err)
	}
	var out corpusWorld
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// pointTotal is how many locations the corpus packs beside a world, which is
// how many points the map draws when nothing is hidden.
func pointTotal(t *testing.T, slug, world string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(corpusDir, slug, "worlds", world+".locations.json"))
	if err != nil {
		t.Fatalf("the corpus packs no locations for %s/%s: %v", slug, world, err)
	}
	var held struct {
		Locations []json.RawMessage `json:"locations"`
	}
	if err := json.Unmarshal(body, &held); err != nil {
		t.Fatal(err)
	}
	return len(held.Locations)
}

// shapeTotal is how many features the payload's path and area collections
// carry, which is how many shapes the map draws when nothing is hidden.
func shapeTotal(world corpusWorld) int {
	total := 0
	for _, collection := range world.Collections {
		if collection.Kind != "point" {
			total += len(collection.Features)
		}
	}
	return total
}

// featureNamed finds one shape feature by title, so a test can point at "the
// MPO Boundary" rather than at a number a re-extraction would move.
func featureNamed(t *testing.T, world corpusWorld, title string) string {
	t.Helper()
	for _, collection := range world.Collections {
		for _, feature := range collection.Features {
			if feature.Title == title {
				return feature.ID.String()
			}
		}
	}
	t.Fatalf("the corpus payload holds no feature titled %q", title)
	return ""
}

// firstPoint is one point the corpus packs -- the first location of a world,
// its id and its title -- for the tests that need a card about somewhere.
func firstPoint(t *testing.T, slug, world string) (id, title string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(corpusDir, slug, "worlds", world+".locations.json"))
	if err != nil {
		t.Fatalf("the corpus packs no locations for %s/%s: %v", slug, world, err)
	}
	var held struct {
		Locations []struct {
			ID    json.Number `json:"id"`
			Title string      `json:"title"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(body, &held); err != nil {
		t.Fatal(err)
	}
	if len(held.Locations) == 0 {
		t.Fatalf("the corpus packs an empty location list for %s/%s", slug, world)
	}
	return held.Locations[0].ID.String(), held.Locations[0].Title
}

// collectionNamed finds one collection by title, for the same reason.
func collectionNamed(t *testing.T, world corpusWorld, title string) corpusCollection {
	t.Helper()
	for _, collection := range world.Collections {
		if collection.Title == title {
			return collection
		}
	}
	t.Fatalf("the corpus payload holds no collection titled %q", title)
	return corpusCollection{}
}

// ---------------------------------------------------------------------------
// Reading the island
// ---------------------------------------------------------------------------

// readIsland pulls the island's JSON back out of a rendered answer. It is
// deliberately read the way every consumer reads it -- by id, off the
// document -- rather than by asking the application for it a second way.
func readIsland(t *testing.T, page string) map[string]any {
	t.Helper()
	const open = `<script type="application/json" id="atlas-session-island">`
	start := strings.Index(page, open)
	if start < 0 {
		t.Fatal("the page carries no state island")
	}
	body := page[start+len(open):]
	end := strings.Index(body, "</script>")
	if end < 0 {
		t.Fatal("the state island is not closed")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body[:end]), &out); err != nil {
		t.Fatalf("the state island is not JSON: %v\n%s", err, body[:end])
	}
	return out
}

// entryOf is the island's entry, which every test below has questions for.
func entryOf(t *testing.T, page string) map[string]any {
	t.Helper()
	island := readIsland(t, page)
	entry, _ := island["entry"].(map[string]any)
	if entry == nil {
		t.Fatalf("the island carries no entry:\n%v", island)
	}
	return entry
}
