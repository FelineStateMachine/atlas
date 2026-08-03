package island

import (
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The state island, held to its own contract.
//
// internal/app/island.go promises twelve keys -- the ones the reference
// implementation wrote to localStorage -- and promises what each one is: the
// volume and world the page stands in, the lens as an index, the camera as
// the server's echo of what the seam last reported, the hide set as the
// payload's own ids, the fold and label ledgers, and the three booleans of
// the furniture. The recorded baselines these used to be compared against are
// gone; what replaced the recording is the contract itself, asked over the
// two corpus volumes: every key present, no key invented, and every value in
// agreement with the page it rides in and the corpus payload the page was
// rendered from.

// islandKeys is the whole of what the entry may say. A key missing is a hole
// in the contract; a key beyond these is an invention no client asked for.
var islandKeys = []string{
	"volume", "world", "lens", "center", "zoom", "hidden", "collapsed",
	"expanded", "labels", "overviewDocked", "dockFolded", "dockDismissed",
}

// holdKeys holds an entry to exactly the twelve.
func holdKeys(t *testing.T, entry map[string]any) {
	t.Helper()
	for _, key := range islandKeys {
		if _, held := entry[key]; !held {
			t.Errorf("the island has no %q, which the contract promises", key)
		}
	}
	for key := range entry {
		found := false
		for _, want := range islandKeys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the island carries %q, which the contract does not name", key)
		}
	}
}

// idStrings flattens a JSON id list -- numbers on the wire, float64 here --
// back to the strings they ride the DOM as, sorted the way the island sorts.
func idStrings(t *testing.T, value any) []string {
	t.Helper()
	held, ok := value.([]any)
	if !ok {
		t.Fatalf("not a list: %v", value)
	}
	out := make([]string, 0, len(held))
	for _, member := range held {
		switch v := member.(type) {
		case float64:
			out = append(out, strconv.FormatFloat(v, 'f', -1, 64))
		case string:
			out = append(out, v)
		default:
			t.Fatalf("an id that is neither number nor string: %v", member)
		}
	}
	return out
}

// openExplorer opens one corpus volume's first world and hands back the page.
func openExplorer(t *testing.T, handler http.Handler, slug, world string) string {
	t.Helper()
	page := get(t, handler, "/v/"+slug+"/"+world)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	return page.Body.String()
}

// arrange runs one session interaction and requires it answered.
func arrange(t *testing.T, handler http.Handler, slug, concern string, pairs ...string) string {
	t.Helper()
	form := url.Values{"volume": {slug}}
	for i := 0; i+1 < len(pairs); i += 2 {
		form.Set(pairs[i], pairs[i+1])
	}
	answer := post(t, handler, "/session/"+concern, form)
	if answer.Code != http.StatusOK {
		t.Fatalf("/session/%s answered %d: %s", concern, answer.Code, answer.Body)
	}
	return answer.Body.String()
}

// TestTheIslandOpensWithTheWorldsOwnArrangement is the "initial" step of every
// recorded tour, re-derived: a world nobody has arranged publishes exactly the
// twelve keys, and every value is the one the world itself supplies -- nothing
// hidden or folded, the ungrouped shape collections unfolded, the first lens,
// a camera nobody has reported, and the furniture in its opening positions.
func TestTheIslandOpensWithTheWorldsOwnArrangement(t *testing.T) {
	for _, slug := range []string{"bend-or", "mars"} {
		t.Run(slug, func(t *testing.T) {
			volume := corpusVolume(t, slug)
			world := firstWorld(t, volume)
			payload := readCorpusWorld(t, slug, world)
			handler := newApp(t, volume)

			page := openExplorer(t, handler, slug, world)
			island := readIsland(t, page)
			if island["last"] != slug {
				t.Errorf("island last = %v, want %s", island["last"], slug)
			}
			entry := entryOf(t, page)
			holdKeys(t, entry)

			if entry["volume"] != slug {
				t.Errorf("volume = %v", entry["volume"])
			}
			if entry["world"] != world {
				t.Errorf("world = %v", entry["world"])
			}
			if lens, _ := entry["lens"].(float64); lens != 0 {
				t.Errorf("lens = %v, want the first lens's index", entry["lens"])
			}
			// The camera is the seam's to originate, and nobody has spoken.
			if entry["center"] != nil || entry["zoom"] != nil {
				t.Errorf("a world nobody has looked at carries a camera: center %v, zoom %v",
					entry["center"], entry["zoom"])
			}
			// Neither corpus payload curates anything invisible, so the hide
			// set opens empty -- and empty, not null: the golden key names came
			// with golden shapes.
			if got := idStrings(t, entry["hidden"]); len(got) != 0 {
				t.Errorf("hidden opens as %v", got)
			}
			if got := idStrings(t, entry["collapsed"]); len(got) != 0 {
				t.Errorf("collapsed opens as %v", got)
			}
			if got := idStrings(t, entry["labels"]); len(got) != 0 {
				t.Errorf("the label ledger opens holding %v", got)
			}
			// The ungrouped shape collections open unfolded, so their feature
			// indexes are there the moment the section is opened.
			want := []string{}
			for _, collection := range payload.Collections {
				if collection.Kind != "point" && collection.Group == "" {
					want = append(want, collection.ID.String())
				}
			}
			sort.Strings(want)
			if got := idStrings(t, entry["expanded"]); !reflect.DeepEqual(got, want) {
				t.Errorf("expanded = %v, want the ungrouped shape collections %v", got, want)
			}
			if entry["overviewDocked"] != false {
				t.Errorf("overviewDocked opens %v", entry["overviewDocked"])
			}
			if entry["dockFolded"] != true {
				t.Errorf("dockFolded opens %v; a map opens on the map", entry["dockFolded"])
			}
			if entry["dockDismissed"] != false {
				t.Errorf("dockDismissed opens %v", entry["dockDismissed"])
			}
		})
	}
}

// TestTheHideSetIsThePayloadsOwnIDs walks the hide set through one row, the
// whole world, and back, and holds the island to the page at every step: the
// ids recorded are the payload's own, and the count the footer draws is the
// corpus's own arithmetic.
func TestTheHideSetIsThePayloadsOwnIDs(t *testing.T) {
	const slug = "bend-or"
	volume := corpusVolume(t, slug)
	world := firstWorld(t, volume)
	payload := readCorpusWorld(t, slug, world)
	handler := newApp(t, volume)
	openExplorer(t, handler, slug, world)

	everything := pointTotal(t, slug, world) + shapeTotal(payload)
	countOf := func(n int) string {
		if n == 1 {
			return ">1 feature<"
		}
		return ">" + strconv.Itoa(n) + " features<"
	}

	// One row put away: the id lands in the set, and the features it carried
	// leave the count.
	mpo := collectionNamed(t, payload, "MPO Boundary")
	answer := arrange(t, handler, slug, "collections",
		"collection", mpo.ID.String(), "visible", "0")
	if !strings.Contains(answer, countOf(everything-len(mpo.Features))) {
		t.Errorf("hiding %s did not take its %d features off the count:\n%s",
			mpo.Title, len(mpo.Features), answer)
	}
	page := openExplorer(t, handler, slug, world)
	if got := idStrings(t, entryOf(t, page)["hidden"]); !reflect.DeepEqual(got, []string{mpo.ID.String()}) {
		t.Errorf("hidden = %v, want the one row put away", got)
	}

	// Everything put away: the set is every collection the payload declares,
	// sorted as the strings they ride the DOM as, and the map draws nothing.
	answer = arrange(t, handler, slug, "collections", "all", "hide")
	if !strings.Contains(answer, countOf(0)) {
		t.Errorf("hiding everything left something on the count:\n%s", answer)
	}
	want := make([]string, 0, len(payload.Collections))
	for _, collection := range payload.Collections {
		want = append(want, collection.ID.String())
	}
	sort.Strings(want)
	page = openExplorer(t, handler, slug, world)
	if got := idStrings(t, entryOf(t, page)["hidden"]); !reflect.DeepEqual(got, want) {
		t.Errorf("hidden = %v, want every collection the payload declares %v", got, want)
	}

	// And everything back: an empty set that means "asked for", not "untouched".
	answer = arrange(t, handler, slug, "collections", "all", "show")
	if !strings.Contains(answer, countOf(everything)) {
		t.Errorf("showing everything did not give the %d features back:\n%s", everything, answer)
	}
	page = openExplorer(t, handler, slug, world)
	if got := idStrings(t, entryOf(t, page)["hidden"]); len(got) != 0 {
		t.Errorf("hidden = %v after showing everything", got)
	}
}

// TestTheIslandRecordsFoldsAndUnfolds is the section ledger on both volumes:
// the key folded is the section's own key -- the viewer's zones section on the
// city, the producer's group on the planet -- and unfolding everything empties
// the ledger rather than writing an "open" beside every name.
func TestTheIslandRecordsFoldsAndUnfolds(t *testing.T) {
	for _, tt := range []struct {
		slug    string
		section string
	}{
		{"bend-or", "zones"},
		{"mars", "group-Nomenclature"},
	} {
		t.Run(tt.slug, func(t *testing.T) {
			volume := corpusVolume(t, tt.slug)
			world := firstWorld(t, volume)
			handler := newApp(t, volume)
			openExplorer(t, handler, tt.slug, world)

			arrange(t, handler, tt.slug, "sections", "section", tt.section, "open", "0")
			page := openExplorer(t, handler, tt.slug, world)
			if got := idStrings(t, entryOf(t, page)["collapsed"]); !reflect.DeepEqual(got, []string{tt.section}) {
				t.Errorf("collapsed = %v, want [%s]", got, tt.section)
			}
			// The page agrees: the folded section wears its fold.
			if !strings.Contains(page, `data-layer-section="`+tt.section+`"`) {
				t.Fatalf("the page has no section %s to have folded", tt.section)
			}

			arrange(t, handler, tt.slug, "sections", "all", "unfold")
			page = openExplorer(t, handler, tt.slug, world)
			if got := idStrings(t, entryOf(t, page)["collapsed"]); len(got) != 0 {
				t.Errorf("collapsed = %v after unfolding everything", got)
			}
		})
	}
}

// TestTheLabelLedgerKeepsOnlyDisagreements is the label ladder's record: a
// flip that disagrees with the curation is stored as "<collection>=<policy>",
// a flip back to the curated word drops the override rather than storing it,
// and turning every toggle over and back leaves the ledger empty -- which is
// what the recorded label-override-set, label-override-dropped and
// label-ladder-restored steps were each pinning.
func TestTheLabelLedgerKeepsOnlyDisagreements(t *testing.T) {
	const slug = "bend-or"
	volume := corpusVolume(t, slug)
	world := firstWorld(t, volume)
	payload := readCorpusWorld(t, slug, world)
	handler := newApp(t, volume)
	openExplorer(t, handler, slug, world)

	ledger := func() []string {
		return idStrings(t, entryOf(t, openExplorer(t, handler, slug, world))["labels"])
	}
	flip := func(id string) {
		arrange(t, handler, slug, "labels", "collection", id, "flip", "1")
	}

	// A speaking area silenced disagrees with its curation and is recorded.
	speaking := collectionNamed(t, payload, "Zoning")
	flip(speaking.ID.String())
	if got := ledger(); !reflect.DeepEqual(got, []string{speaking.ID.String() + "=quiet"}) {
		t.Errorf("labels = %v, want the one disagreement recorded", got)
	}
	// Flipped back, the override has nothing left to say and is dropped.
	flip(speaking.ID.String())
	if got := ledger(); len(got) != 0 {
		t.Errorf("labels = %v; a flip back to the curated word should drop the override", got)
	}

	// A quiet collection asked to speak is the disagreement spelled the other
	// way round.
	quiet := collectionNamed(t, payload, "Watersheds")
	if quiet.Attrs["atlas.label.policy"] != "quiet" {
		t.Fatalf("the corpus no longer curates %s quiet; this test needs a quiet collection", quiet.Title)
	}
	flip(quiet.ID.String())
	if got := ledger(); !reflect.DeepEqual(got, []string{quiet.ID.String() + "=always"}) {
		t.Errorf("labels = %v, want the quiet collection's override", got)
	}

	// Every toggle over and back restores the ladder exactly and leaves no
	// overrides behind.
	flip(quiet.ID.String())
	for _, collection := range payload.Collections {
		if collection.Kind == "point" {
			continue
		}
		if collection.Kind == "area" {
			flip(collection.ID.String())
		}
	}
	for _, collection := range payload.Collections {
		if collection.Kind == "area" {
			flip(collection.ID.String())
		}
	}
	if got := ledger(); len(got) != 0 {
		t.Errorf("labels = %v after turning every toggle over and back", got)
	}
}

// TestTheCameraIsTheSeamsEchoRoundedForTheRecord is the two keys the server
// cannot originate, checked as the round trip they actually are: the seam
// reports a settled camera, and the island already carries it back in the
// same answer -- rounded the way the harness that used to read it rounded,
// whole world units for the centre with a half going up, three decimals for
// the zoom.
func TestTheCameraIsTheSeamsEchoRoundedForTheRecord(t *testing.T) {
	const slug = "bend-or"
	volume := corpusVolume(t, slug)
	world := firstWorld(t, volume)
	handler := newApp(t, volume)
	openExplorer(t, handler, slug, world)

	// The report answers with the island alone, which is how the camera it
	// just wrote becomes readable without a second request.
	answer := post(t, handler, "/session/view", url.Values{
		"volume": {slug}, "world": {world},
		"x": {"120.5"}, "y": {"-40.5"}, "zoom": {"6.2504"},
	})
	if answer.Code != http.StatusOK {
		t.Fatalf("the camera report answered %d", answer.Code)
	}
	if !strings.Contains(answer.Body.String(), `id="atlas-session-island"`) {
		t.Fatalf("the camera report answered without the island:\n%s", answer.Body)
	}

	entry := entryOf(t, answer.Body.String())
	holdKeys(t, entry)
	// Math.round's convention: a half goes toward positive infinity on both
	// axes, so 120.5 climbs to 121 and -40.5 climbs to -40.
	center, _ := entry["center"].([]any)
	if len(center) != 2 || center[0] != 121.0 || center[1] != -40.0 {
		t.Errorf("center = %v, want [121 -40]: the echo rounds half up", entry["center"])
	}
	if entry["zoom"] != 6.25 {
		t.Errorf("zoom = %v, want 6.25: the echo keeps three decimals", entry["zoom"])
	}
}

// TestALensIsRecordedAsItsIndex is the planet's variant-second step: a lens is
// an arrangement of one world, not a different ground, so the island carries
// the index of the lens the reader chose -- and the overview's docking rides
// in the same record.
func TestALensIsRecordedAsItsIndex(t *testing.T) {
	const slug = "mars"
	volume := corpusVolume(t, slug)
	world := firstWorld(t, volume)
	handler := newApp(t, volume)
	openExplorer(t, handler, slug, world)

	arrange(t, handler, slug, "lens", "lens", "MOLA Elevation")
	page := openExplorer(t, handler, slug, world)
	entry := entryOf(t, page)
	if lens, _ := entry["lens"].(float64); lens != 1 {
		t.Errorf("lens = %v, want 1: MOLA Elevation is the payload's second lens", entry["lens"])
	}
	// The page agrees with the record: the chosen lens is the one the picker
	// marks current.
	if !strings.Contains(page, "MOLA Elevation") {
		t.Errorf("the page no longer names the lens the island recorded:\n%s", page)
	}

	arrange(t, handler, slug, "overview", "docked", "1")
	entry = entryOf(t, openExplorer(t, handler, slug, world))
	if entry["overviewDocked"] != true {
		t.Errorf("overviewDocked = %v after docking the overview", entry["overviewDocked"])
	}
	holdKeys(t, entry)
}

// TestWhatIsNotTheIslandsLeavesItUntouched is the other half of the contract:
// the island speaks for the arrangement, and the interactions that are not
// arrangement -- a search, a highlight set raised and cleared, the sidebar,
// the grid -- must neither move its values nor teach it new keys. It is what
// the recorded search-a, and-cleared, sidebar-collapsed and grid-ascended
// steps were really saying: the entry those steps recorded was the entry the
// step before them recorded.
func TestWhatIsNotTheIslandsLeavesItUntouched(t *testing.T) {
	const slug = "bend-or"
	volume := corpusVolume(t, slug)
	world := firstWorld(t, volume)
	payload := readCorpusWorld(t, slug, world)
	handler := newApp(t, volume)
	openExplorer(t, handler, slug, world)

	before := entryOf(t, openExplorer(t, handler, slug, world))

	arrange(t, handler, slug, "search", "q", "a")
	arrange(t, handler, slug, "search", "q", "")
	arrange(t, handler, slug, "highlight", "feature", featureNamed(t, payload, "MPO Boundary"))
	arrange(t, handler, slug, "highlight", "feature", featureNamed(t, payload, "Baker Lake"))
	arrange(t, handler, slug, "highlight", "all", "clear")
	arrange(t, handler, slug, "sidebar", "open", "0")
	arrange(t, handler, slug, "grid", "system", "geohash")
	arrange(t, handler, slug, "grid", "cell", "")
	arrange(t, handler, slug, "grid", "system", "toggle")

	after := entryOf(t, openExplorer(t, handler, slug, world))
	holdKeys(t, after)
	if !reflect.DeepEqual(after, before) {
		t.Errorf("interactions that are not the arrangement moved the island:\nbefore %v\nafter  %v",
			before, after)
	}
}
