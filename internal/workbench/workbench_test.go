package workbench

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
)

// The library every page test reads: two builds of one volume, the newer one
// richer and carrying a ledger, and a second volume with a single build.
func testLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bundleSpec{
		slug: "hollowmere", title: "Hollowmere",
		createdAt: "2026-01-01T00:00:00Z", stamp: strings.Repeat("a", 64),
		worlds: []worldSpec{{
			slug: "overworld", title: "Overworld",
			features: []featureSpec{{id: 1, title: "Gate"}},
		}},
	}.write(t, dir)
	bundleSpec{
		slug: "hollowmere", title: "Hollowmere",
		createdAt: "2026-02-01T00:00:00Z", stamp: strings.Repeat("b", 64),
		worlds: []worldSpec{{
			slug: "overworld", title: "Overworld", icon: "icons/marker.png",
			features: []featureSpec{
				{id: 1, title: "Gate", description: "The way in, and out."},
				{id: 2, title: "Well"},
				{id: 3, title: "Chapel"},
			},
			merged: []map[string]any{{
				"source":        "IGN Wiki",
				"slug":          "ign",
				"donorFeatures": map[string]any{"point": 4},
				"added":         1,
				"matched":       []map[string]any{{"d": 11, "w": 1, "px": 38, "e": true}},
				"held":          []map[string]any{{"d": 12, "t": "Old Well", "why": "name 200px away"}},
				"alignment":     "14 names, 3px residual",
			}},
		}},
	}.write(t, dir)
	bundleSpec{
		slug: "driftfen", title: "Driftfen",
		createdAt: "2026-03-01T00:00:00Z", stamp: strings.Repeat("c", 64),
		worlds: []worldSpec{{
			slug: "marsh", title: "Marsh",
			features: []featureSpec{{id: 7, title: "Reed Gate"}},
		}},
	}.write(t, dir)
	return dir
}

func testWorkbench(t *testing.T, dir string) *Workbench {
	t.Helper()
	held, err := New(Options{
		Targets: Targets{Registry: dir},
		Sources: testSources(),
		Runtime: []byte("/* the vendored runtime */"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return held
}

func testSources() []Source {
	return []Source{
		{
			Name: "mapgenie", Label: "MapGenie",
			Attribution: "Maps and pin data by MapGenie and its contributors.",
			IDSpace:     "native",
		},
		{
			Name: "ign", Label: "IGN Wiki",
			License: "Reproduced under IGN's terms.",
			IDSpace: "native", Crawlable: true, Pair: true,
			TargetHint: "an IGN wikimap as <objectSlug>/<mapSlug>, e.g. cyberpunk-2077/night-city",
		},
		{
			Name: "nasa-trek", Label: "NASA Trek",
			License:     "Public domain.",
			Attribution: "Global mosaics from NASA's Solar System Treks.",
			IDSpace:     "derived", Crawlable: true,
			TargetHint: "a body, e.g. mars",
		},
	}
}

func site(t *testing.T, held *Workbench) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(held)
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, server *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	response, err := server.Client().Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	// A page is read the way a person reads it: html/template escapes a plus
	// sign and an apostrophe, and a test that asserted on the escapes would be
	// asserting about the encoder rather than about the page.
	return response, html.UnescapeString(string(body))
}

func wants(t *testing.T, page, body string, wanted ...string) {
	t.Helper()
	for _, want := range wanted {
		if !strings.Contains(body, want) {
			t.Errorf("the %s page misses %q", page, want)
		}
	}
}

func TestTheLibraryPageHeadlinesEveryVolumeWithItsScore(t *testing.T) {
	dir := testLibrary(t)
	server := site(t, testWorkbench(t, dir))

	response, body := get(t, server, "/")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the library page answered %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("the page carries %q as its policy", got)
	}
	wants(t, "library", body, "Hollowmere", "hollowmere", "Driftfen", dir, "IGN Wiki")

	// The figures are the serving build's, and the serving build is the newer
	// one: the score on the page is the score the enrich lane would compute.
	table, err := maturity.Points()
	if err != nil {
		t.Fatal(err)
	}
	held := &library{dir: dir, table: table}
	volumes, skipped, err := held.volumes()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("bundles were skipped: %v", skipped)
	}
	for _, volume := range volumes {
		wants(t, "library", body, strconv.Itoa(volume.Serving().Total))
	}

	// Hollowmere's newer build is richer, and the movement says so.
	var hollowmere *volume
	for _, volume := range volumes {
		if volume.Slug == "hollowmere" {
			hollowmere = volume
		}
	}
	moved := hollowmere.Movement()
	if moved == nil || moved.Delta <= 0 {
		t.Fatalf("the richer build did not move the score: %+v", moved)
	}
	wants(t, "library", body, "+"+strconv.Itoa(moved.Delta))
}

func TestTheServingBuildIsTheOneTheRegistryWouldFoldTo(t *testing.T) {
	table, err := maturity.Points()
	if err != nil {
		t.Fatal(err)
	}
	held := &library{dir: testLibrary(t), table: table}
	volumes, _, err := held.volumes()
	if err != nil {
		t.Fatal(err)
	}
	for _, volume := range volumes {
		if want := maturity.Serving(volume.Builds); volume.Serving() != want {
			t.Errorf("%s serves %s; the registry's fold picks %s",
				volume.Slug, volume.Serving().File, want.File)
		}
	}
}

func TestTheMeasurementPageIsTheScoreThenTheDiagnosticsThenTheLedger(t *testing.T) {
	server := site(t, testWorkbench(t, testLibrary(t)))

	response, body := get(t, server, "/volume/hollowmere")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the measurement page answered %d", response.StatusCode)
	}
	wants(t, "measurement", body,
		// the headline
		"points", "against the build before it",
		// the per-world breakdown
		"overworld", "collections",
		// the five axes, carried as diagnostics
		"annotation", "cartography", "structure", "icons", "conventions",
		"nothing gates on them",
		// the whole ledger, not a count of it
		"IGN Wiki", "Old Well", "name 200px away", "14 names, 3px residual",
		// both builds, the newer marked
		"serving")

	newer := strings.Index(body, strings.Repeat("b", 12))
	older := strings.Index(body, strings.Repeat("a", 12))
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("the builds are not newest first (newer at %d, older at %d)", newer, older)
	}

	if response, _ := get(t, server, "/volume/nowhere"); response.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown volume answered %d", response.StatusCode)
	}
}

func TestTheDiffPageIsHeadlinedByTheScoreDelta(t *testing.T) {
	dir := testLibrary(t)
	server := site(t, testWorkbench(t, dir))

	table, err := maturity.Points()
	if err != nil {
		t.Fatal(err)
	}
	held := &library{dir: dir, table: table}
	volume, _, err := held.volumeBySlug("hollowmere")
	if err != nil {
		t.Fatal(err)
	}
	from, to := volume.Builds[1], volume.Builds[0]

	response, body := get(t, server, "/volume/hollowmere/diff?a="+from.File+"&b="+to.File)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the diff page answered %d: %s", response.StatusCode, body)
	}
	moved := maturity.Compare(from, to)
	wants(t, "diff", body,
		"+"+strconv.Itoa(moved.Delta), "richer",
		// what moved, under the headline
		"Score by world", "Axes", "point features", "described share",
		// the features themselves
		"Well", "Chapel",
		// and the ledgers' agreement
		"Matched-pair stability")

	// The reverse direction is the same comparison read the other way.
	_, reverse := get(t, server, "/volume/hollowmere/diff?a="+to.File+"&b="+from.File)
	wants(t, "reversed diff", reverse, strconv.Itoa(-moved.Delta), "poorer")

	if response, _ := get(t, server, "/volume/hollowmere/diff?a=nowhere.atlas&b="+to.File); response.StatusCode != http.StatusBadRequest {
		t.Errorf("a diff naming a build that is not there answered %d", response.StatusCode)
	}
}

func TestTheSourcePagePrintsWhatEachVolumeOwes(t *testing.T) {
	server := site(t, testWorkbench(t, testLibrary(t)))

	response, body := get(t, server, "/sources")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the sources page answered %d", response.StatusCode)
	}
	wants(t, "sources", body,
		"MapGenie", "IGN Wiki", "NASA Trek",
		"Maps and pin data by MapGenie and its contributors.",
		"Public domain.", "Reproduced under IGN's terms.",
		"native ids", "derived ids",
		"crawlable", "archived captures only")
}

func TestTheAssetsAreTheWorkbenchsOwnAndTheRuntimeIsOptional(t *testing.T) {
	server := site(t, testWorkbench(t, testLibrary(t)))

	response, body := get(t, server, "/assets/workbench.css")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the stylesheet answered %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("the stylesheet arrived as %q", got)
	}
	wants(t, "stylesheet", body, "--accent", ".op-row")

	if response, _ := get(t, server, "/assets/htmx.js"); response.StatusCode != http.StatusOK {
		t.Errorf("the runtime the wiring handed over answered %d", response.StatusCode)
	}
	if response, _ := get(t, server, "/assets/nothing.js"); response.StatusCode != http.StatusNotFound {
		t.Errorf("an asset nobody has answered %d", response.StatusCode)
	}

	// A workbench mounted without a runtime is a working workbench.
	bare, err := New(Options{Targets: Targets{Registry: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	bareSite := site(t, bare)
	if response, _ := get(t, bareSite, "/assets/htmx.js"); response.StatusCode != http.StatusNotFound {
		t.Errorf("a workbench with no runtime served one: %d", response.StatusCode)
	}
	if response, body := get(t, bareSite, "/"); response.StatusCode != http.StatusOK {
		t.Errorf("the library page of an empty registry answered %d: %s", response.StatusCode, body)
	}
}

// TestTheMeasurementPageCarriesAFixturesKnownScore anchors a page to a number
// that was captured rather than invented: the golden extraction of TUNIC, packed
// back into a bundle, scores 1323 under point table v1 -- the same figure the
// enrich lane's own suite reports for the same fixture. A page that stopped
// printing the score, or a table re-weighted without a version bump, fails here.
func TestTheMeasurementPageCarriesAFixturesKnownScore(t *testing.T) {
	const (
		fixture = "../../golden/fixtures/bundles/tunic"
		score   = 1323
		version = 1
	)
	dir := t.TempDir()
	path := packFixture(t, fixture, dir)

	table, err := maturity.Points()
	if err != nil {
		t.Fatal(err)
	}
	if table.Version != version {
		t.Skipf("the point table is v%d; the anchored score is a v%d figure", table.Version, version)
	}
	measured, err := maturity.Measure(path, table)
	if err != nil {
		t.Fatal(err)
	}
	if measured.Total != score {
		t.Fatalf("the fixture scores %d, and the anchor says %d: either the fixture moved "+
			"or the point table was re-weighted without a version bump", measured.Total, score)
	}

	server := site(t, testWorkbench(t, dir))
	response, body := get(t, server, "/volume/tunic")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the measurement page answered %d", response.StatusCode)
	}
	wants(t, "measurement", body, "TUNIC", strconv.Itoa(score), "point table v"+strconv.Itoa(version), "world")

	_, library := get(t, server, "/")
	wants(t, "library", library, "TUNIC", strconv.Itoa(score))
}
