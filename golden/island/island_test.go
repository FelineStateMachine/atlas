package islandgolden_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// The state island, held to the golden baselines.
//
// issue #5 §6 asks the rewritten application to publish "server session state
// as a JSON island ... matching golden key names". The baselines in
// golden/parity/<volume>/tour.json each record, per step, the arrangement the
// reference implementation had saved at that moment
// (golden/parity/SCHEMA.md §3.2). This test drives the same arrangements
// through the new application's own routes and holds the island it renders to
// those recordings, key for key.
//
// What is being checked is not that two JSON documents look alike. It is that
// the arrangement a reader can reach through the hypermedia surface is the
// same arrangement the reference implementation reached through its own -- the
// defaults a world opens with, what a filter does to the hide set, what a
// label flip records and what it declines to record. The island is where that
// is visible from outside.
//
// # What cannot be produced here, and why
//
// Two of the twelve keys are the chart's camera. `center` and `zoom` are
// arrived at by fitting a raster to a window: they depend on the size of the
// viewport, the lens's own depth, and the fit the seam computes. No amount of
// session state produces them, and the server's only honest relationship with
// them is the one issue #5 §4.1 describes -- the seam reports a settled camera
// upward, debounced, and the server stores it and hands it back.
//
// So the sequences below post the baseline's camera at POST /session/view
// before reading the island, and what is proved about those two keys is the
// round trip: the server echoes what the seam said, rounded the way the
// harness rounds it. The values' *origin* is seam-side and is documented as
// such in docs/app.md §6.
//
// Everything else is produced from nothing but the volume and the posts.

// islandCase is one baseline step, and the interactions that reach it from a
// fresh session.
type islandCase struct {
	// step is the baseline step whose session object is the expectation.
	step string

	// world is the world the run opens on; empty means the first.
	world string

	// posts are the session interactions, in order.
	posts []islandPost
}

type islandPost struct {
	concern string
	form    url.Values
}

func at(concern string, pairs ...string) islandPost {
	form := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		form.Set(pairs[i], pairs[i+1])
	}
	return islandPost{concern: concern, form: form}
}

// islandCases is the table: several steps per volume, spread across the
// concerns that move the arrangement -- the legend's hide set, its folds and
// unfolds, isolating, search, the label ladder, the lens, the grid, the
// corner locator.
//
// The sequences are derived from the baselines rather than copied from the
// tour script: the tour walks one long path and each step inherits everything
// before it, so what is reproduced here is the *state* a step records, by the
// shortest route the hypermedia surface offers to it.
var islandCases = map[string][]islandCase{
	"bend-or": {
		// The city is the label-policy and shape-collection test bed: nine
		// shape collections, three of them curated quiet.
		{step: "initial"},
		{step: "search-a", posts: []islandPost{
			at("search", "q", "a"),
		}},
		{step: "category-hidden", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("collections", "collection", "39191589", "visible", "0"),
		}},
		{step: "section-folded", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "section", "zones", "open", "1"),
		}},
		{step: "all-hidden", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("collections", "all", "hide"),
		}},
		{step: "all-shown", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("collections", "all", "hide"),
			at("collections", "all", "show"),
		}},
		// A flip that disagrees with the curation is recorded; a flip back
		// to the curated word drops the override rather than storing it.
		{step: "label-override-set", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("labels", "collection", "39191589", "flip", "1"),
		}},
		{step: "label-override-dropped", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("labels", "collection", "39191589", "flip", "1"),
			at("labels", "collection", "39191589", "flip", "1"),
		}},
		// Turning every toggle over and back restores the ladder exactly
		// and leaves no overrides behind.
		{step: "label-ladder-restored", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("labels", "collection", "39191589", "flip", "1"),
			at("labels", "collection", "80332795", "flip", "1"),
			at("labels", "collection", "1951802496", "flip", "1"),
			at("labels", "collection", "39191589", "flip", "1"),
			at("labels", "collection", "80332795", "flip", "1"),
			at("labels", "collection", "1951802496", "flip", "1"),
		}},
		// Highlighting is not in the record, so what this pins is that a
		// filter which culls two thirds of the map leaves the arrangement
		// exactly where it was.
		{step: "and-cleared", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("highlight", "feature", "39191589"),
			at("highlight", "feature", "277390785"),
			at("highlight", "all", "clear"),
		}},
		{step: "sidebar-collapsed", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("sidebar", "open", "0"),
		}},
	},
	"mars": {
		{step: "initial"},
		{step: "category-hidden", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("collections", "collection", "1481981621", "visible", "0"),
		}},
		{step: "section-folded", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "section", "group-Nomenclature", "open", "0"),
		}},
		{step: "sections-unfolded", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
		}},
		{step: "all-hidden", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("collections", "all", "hide"),
		}},
		// A lens is an arrangement of one world, not a different ground, so
		// it is a session write and the record carries its index.
		{step: "variant-second", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("lens", "lens", "MOLA Elevation"),
		}},
		{step: "overview-docked", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("overview", "docked", "1"),
		}},
		{step: "grid-ascended", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
			at("grid", "system", "geohash"),
			at("grid", "cell", ""),
		}},
	},
	"zelda-tears-of-the-kingdom": {
		// Seventy collections start put away by the payload's own curation,
		// and one shape row starts unfolded. Both are defaults the world
		// supplies rather than anything a reader did.
		{step: "initial"},
		{step: "category-hidden", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("collections", "collection", "1834502030", "visible", "0"),
		}},
	},
	"cyberpunk-2077": {
		{step: "initial"},
		{step: "sections-unfolded", posts: []islandPost{
			at("search", "q", "a"),
			at("search", "q", ""),
			at("sections", "all", "unfold"),
		}},
	},
	"tunic": {
		{step: "initial"},
		{step: "search-a", posts: []islandPost{
			at("search", "q", "a"),
		}},
	},
	"fallout-new-vegas": {
		{step: "initial"},
		{step: "search-a", posts: []islandPost{
			at("search", "q", "a"),
		}},
	},
}

func TestIslandMatchesTheBaselines(t *testing.T) {
	for slug, cases := range islandCases {
		volume := fixtureVolume(t, slug)
		baseline := readBaseline(t, slug)
		for _, held := range cases {
			t.Run(slug+"/"+held.step, func(t *testing.T) {
				want, ok := baseline[held.step]
				if !ok {
					t.Fatalf("the baseline has no step %q", held.step)
				}
				handler, _ := newApp(t, volume)

				world := held.world
				if world == "" {
					world = volume.Manifest().Worlds[0].Slug
				}
				if got := get(t, handler, "/v/"+slug+"/"+world, nil); got.Code != http.StatusOK {
					t.Fatalf("the explorer answered %d", got.Code)
				}
				for _, step := range held.posts {
					form := url.Values{"volume": {slug}}
					for name, values := range step.form {
						form[name] = values
					}
					answer := post(t, handler, "/session/"+step.concern, form)
					if answer.Code != http.StatusOK {
						t.Fatalf("/session/%s answered %d: %s",
							step.concern, answer.Code, answer.Body)
					}
				}
				// The camera is the seam's to originate and the server's to
				// echo. Report the one the baseline recorded, then hold the
				// island to giving it back.
				reportCamera(t, handler, slug, world, want)

				page := get(t, handler, "/v/"+slug+"/"+world, nil)
				if page.Code != http.StatusOK {
					t.Fatalf("the explorer answered %d", page.Code)
				}
				got := readIsland(t, page.Body.String())
				if got["last"] != slug {
					t.Errorf("island last = %v, want %s", got["last"], slug)
				}
				entry, _ := got["entry"].(map[string]any)
				if entry == nil {
					t.Fatalf("the island carries no entry:\n%v", got)
				}
				compareEntry(t, entry, want)
			})
		}
	}
}

// compareEntry holds the island's entry to the baseline's, key for key in
// both directions: a key the baseline has and the island does not is a hole,
// and a key the island has and the baseline does not is an invention.
func compareEntry(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, expected := range want {
		actual, held := got[key]
		if !held {
			t.Errorf("the island has no %q; the baseline says %v", key, expected)
			continue
		}
		if !reflect.DeepEqual(normalize(actual), normalize(expected)) {
			t.Errorf("%s:\n  island   %v\n  baseline %v", key, actual, expected)
		}
	}
	for key := range got {
		if _, held := want[key]; !held {
			t.Errorf("the island carries %q, which no baseline records", key)
		}
	}
}

// normalize flattens the two shapes JSON gives the same value. A collection id
// is a number in both documents but arrives as float64 here, and an absent
// list is null in one and empty in the other.
func normalize(value any) any {
	switch held := value.(type) {
	case []any:
		out := make([]string, 0, len(held))
		for _, member := range held {
			out = append(out, describe(member))
		}
		return out
	case nil:
		return nil
	}
	return describe(value)
}

func describe(value any) string {
	switch held := value.(type) {
	case float64:
		return strconv.FormatFloat(held, 'f', -1, 64)
	case string:
		return held
	case bool:
		return strconv.FormatBool(held)
	case nil:
		return "null"
	}
	return "?"
}

// reportCamera posts the baseline's own camera, so the two keys the server
// cannot originate are compared as the round trip they actually are.
func reportCamera(t *testing.T, handler http.Handler, slug, world string, want map[string]any) {
	t.Helper()
	center, ok := want["center"].([]any)
	if !ok || len(center) != 2 {
		return
	}
	zoom, ok := want["zoom"].(float64)
	if !ok {
		return
	}
	form := url.Values{
		"volume": {slug}, "world": {world},
		"x":    {strconv.FormatFloat(center[0].(float64), 'f', -1, 64)},
		"y":    {strconv.FormatFloat(center[1].(float64), 'f', -1, 64)},
		"zoom": {strconv.FormatFloat(zoom, 'f', -1, 64)},
	}
	if got := post(t, handler, "/session/view", form); got.Code != http.StatusNoContent {
		t.Fatalf("the camera report answered %d", got.Code)
	}
}

// readIsland pulls the island's JSON back out of a rendered page. It is
// deliberately read the way the parity harness reads it -- by id, off the
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

// readBaseline reads one volume's tour and indexes its steps' session objects
// by step name.
func readBaseline(t *testing.T, slug string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "parity", slug, "tour.json"))
	if err != nil {
		t.Skipf("no baseline for %s: %v", slug, err)
	}
	var tour struct {
		Steps []struct {
			Name     string `json:"name"`
			Snapshot struct {
				Session struct {
					Entry map[string]any `json:"entry"`
				} `json:"session"`
			} `json:"snapshot"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &tour); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, step := range tour.Steps {
		if step.Snapshot.Session.Entry != nil {
			out[step.Name] = step.Snapshot.Session.Entry
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Fixture volumes
// ---------------------------------------------------------------------------

// fixtureVolume stands one golden fixture up as a servable volume.
//
// The fixtures are canonicalized extractions rather than .atlas files -- the
// archives are not in the repository -- so the packed locations are repacked
// from their JSON extraction through the format's own codec. What the handler
// sees is therefore exactly what it would see out of an archive: a manifest,
// a world payload, and ATLASLOC bytes.
func fixtureVolume(t *testing.T, slug string) hostenv.Volume {
	t.Helper()
	dir := filepath.Join("..", "fixtures", "bundles", slug)
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Skipf("no fixture for %s: %v", slug, err)
	}
	var manifest bundle.Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}

	entries := map[string][]byte{}
	for _, world := range manifest.Worlds {
		payload, err := os.ReadFile(filepath.Join(dir, "worlds", world.Slug+".payload.json"))
		if err != nil {
			t.Fatalf("fixture %s world %s: %v", slug, world.Slug, err)
		}
		entries[bundle.WorldEntryName(world.Slug, bundle.WorldSuffix)] = payload

		entries[bundle.WorldEntryName(world.Slug, bundle.PackedSuffix)] =
			repack(t, filepath.Join(dir, "worlds", world.Slug+".locations.json"))

		if text, err := os.ReadFile(filepath.Join(dir, "worlds", world.Slug+".text.json")); err == nil {
			entries[bundle.WorldEntryName(world.Slug, bundle.TextSuffix)] = text
		}
	}
	return &fixtureBundle{manifest: manifest, entries: entries}
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

type fixtureBundle struct {
	manifest bundle.Manifest
	entries  map[string][]byte
}

func (v *fixtureBundle) Manifest() bundle.Manifest { return v.manifest }

func (v *fixtureBundle) Open(name string) (io.ReadCloser, int64, error) {
	held, ok := v.entries[name]
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(held)), int64(len(held)), nil
}
