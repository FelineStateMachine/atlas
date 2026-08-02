package islandgolden_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The display logic, held to what the baselines saw.
//
// The island (island_test.go) checks the arrangement a reader can reach. This
// checks what the application decides *from* that arrangement -- the counts,
// the ladder, the cull -- which is the half that used to live in the browser
// and now runs once, in Go (issue #5 §4.5). It is read off the rendered
// regions rather than off the Go values on purpose: a build whose model
// flipped and whose markup did not is wrong, and this is where that shows.

// TestTheFilterReadsAndAcrossOrWithin is the AND-across, OR-within rule on the
// volume that can express it. bend-or has nine shape collections; highlighting
// one area and one waterbody asks two questions of every point, and the
// baseline records the answer: 213 features drawn falls to 148, because none
// of the 65 points stands in both.
func TestTheFilterReadsAndAcrossOrWithin(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	const world = "2026-08-02"
	page := get(t, handler, "/v/bend-or/"+world, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	// Everything drawn, before any filter: 65 points and 148 shapes.
	if !strings.Contains(page.Body.String(), ">213 features<") {
		t.Errorf("the untouched world does not draw 213 features")
	}

	// One highlight of one collection: the question is "inside the MPO
	// boundary", which every point in the city answers, so nothing is culled.
	answer := post(t, handler, "/session/highlight", url.Values{
		"volume": {"bend-or"}, "feature": {"39191589"},
	})
	if answer.Code != http.StatusOK {
		t.Fatalf("the highlight answered %d: %s", answer.Code, answer.Body)
	}
	if !strings.Contains(answer.Body.String(), ">213 features<") {
		t.Errorf("one highlight over the whole city culled something:\n%s", answer.Body)
	}

	// A second highlight from a *different* collection narrows the question
	// to the ground the two share. Baker Lake holds no annotated place, so
	// every point goes and only the ground is left standing.
	answer = post(t, handler, "/session/highlight", url.Values{
		"volume": {"bend-or"}, "feature": {"277390785"},
	})
	if answer.Code != http.StatusOK {
		t.Fatalf("the second highlight answered %d", answer.Code)
	}
	if !strings.Contains(answer.Body.String(), ">148 features<") {
		t.Errorf("two highlights across two collections did not read as AND:\n%s", answer.Body)
	}
	if !strings.Contains(answer.Body.String(), ">filtered<") {
		t.Error("the panel does not say why its count dropped")
	}

	// Clearing gives them all back.
	answer = post(t, handler, "/session/highlight", url.Values{
		"volume": {"bend-or"}, "all": {"clear"},
	})
	if !strings.Contains(answer.Body.String(), ">213 features<") {
		t.Errorf("clearing the highlights did not give the features back:\n%s", answer.Body)
	}
}

// TestTheLabelLadderReadsTheCuration holds the legend's label toggles to the
// ladder the baseline read off the same buttons: four collections speaking and
// three silent on the city, because three of its nine shape collections carry
// atlas.label.policy=quiet and the paths carry no toggle at all.
func TestTheLabelLadderReadsTheCuration(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	page := get(t, handler, "/v/bend-or/2026-08-02", nil)
	speaking, silent := ladderOf(page.Body.String())

	wantSpeaking := []string{"1410210368", "2143902706", "39191589", "80332795"}
	wantSilent := []string{"1951802496", "253393030", "50985093"}
	if strings.Join(speaking, ",") != strings.Join(wantSpeaking, ",") {
		t.Errorf("speaking = %v, want %v", speaking, wantSpeaking)
	}
	if strings.Join(silent, ",") != strings.Join(wantSilent, ",") {
		t.Errorf("silent = %v, want %v", silent, wantSilent)
	}

	// A flip that disagrees with the curation moves the button; a flip back
	// to the curated word returns it. The record's side of this is checked
	// against the baseline in island_test.go; this is the button's side.
	answer := post(t, handler, "/session/labels", url.Values{
		"volume": {"bend-or"}, "collection": {"39191589"}, "flip": {"1"},
	})
	speaking, silent = ladderOf(answer.Body.String())
	if len(speaking) != 3 || len(silent) != 4 {
		t.Errorf("after one flip: speaking = %v, silent = %v", speaking, silent)
	}

	// Silencing is the reader's own choice and a path never wears a toggle,
	// so the two paths stay out of the ladder however it is turned over.
	if strings.Contains(answer.Body.String(), `data-label-toggle="480364651"`) {
		t.Error("a path collection was offered a label toggle")
	}
}

// ladderOf reads the label ladder off the rendered legend, the way the parity
// harness reads it: off the aria-pressed state of every toggle, in the order
// they appear, sorted.
func ladderOf(page string) (speaking, silent []string) {
	const marker = `data-label-toggle="`
	for at := 0; ; {
		found := strings.Index(page[at:], marker)
		if found < 0 {
			break
		}
		at += found + len(marker)
		end := strings.IndexByte(page[at:], '"')
		if end < 0 {
			break
		}
		id := page[at : at+end]
		// aria-pressed follows on this button and nowhere else, so the
		// window is the button's own tag: reading as far as the next toggle
		// let the last one in the legend take its answer from whatever
		// pressable chrome happened to be rendered after it.
		rest := page[at:]
		if next := strings.Index(rest, "</button>"); next >= 0 {
			rest = rest[:next]
		}
		if strings.Contains(rest, `aria-pressed="true"`) {
			speaking = append(speaking, id)
		} else {
			silent = append(silent, id)
		}
	}
	sortStrings(speaking)
	sortStrings(silent)
	return speaking, silent
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// TestTheLegendTreeIsOneTree checks the sections the tree makes: the viewer's
// own Zones section for ungrouped shape collections, unshifted to the front
// and folded, and the producer's groups in order of first appearance.
func TestTheLegendTreeIsOneTree(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	page := get(t, handler, "/v/bend-or/2026-08-02", nil).Body.String()

	zones := strings.Index(page, `data-layer-section="zones"`)
	heritage := strings.Index(page, `data-layer-section="group-Heritage"`)
	switch {
	case zones < 0:
		t.Fatal("the ungrouped shape collections have no section")
	case heritage < 0:
		t.Fatal("the producer's group is missing")
	case zones > heritage:
		t.Error("the viewer's own Zones section does not come first")
	}
	// Zones is folded on a world nobody has arranged, and its rows are
	// nevertheless rendered, so anything reaching for a feature by name
	// finds it without unfolding first.
	if !strings.Contains(page, `class="layer-section is-collapsed" data-layer-section="zones"`) {
		t.Error("the Zones section does not open folded")
	}
	if !strings.Contains(page, `data-feature-index="1951802496"`) {
		t.Error("a shape row carries no feature index")
	}
	// The city's one point collection is drawn as pins, so it has no
	// unfolding chevron and no label toggle: the affordance appears where
	// the capability does.
	if strings.Contains(page, `data-expand-collection="1496244488"`) {
		t.Error("a point collection was given a feature index")
	}
}

// TestTheDockListsWhatTheMapDraws is the sync invariant the parity tour checks
// on every step, checked here on one: the footer, the panel's count and the
// list under it all tell the same story.
func TestTheDockListsWhatTheMapDraws(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	page := get(t, handler, "/v/bend-or/2026-08-02", nil).Body.String()
	if !strings.Contains(page, `id="visible-count">213 features<`) {
		t.Error("the footer does not count what the map draws")
	}
	if !strings.Contains(page, `id="dock-count">213 features<`) {
		t.Error("the panel does not count what the map draws")
	}
	// The list is a shortlist and says when it is one.
	if !strings.Contains(page, "First 100 of") {
		t.Error("the capped list does not say it is capped")
	}
	if rows := strings.Count(page, `class="search-result`); rows != 100 {
		t.Errorf("the dock listed %d rows, want the 100 it caps at", rows)
	}
}

// TestSearchNarrowsPointsAndNeverTheGround pins the asymmetry the reference
// implementation settled on: a search takes a point off the map and out of the
// list, and never takes the ground out from under it.
func TestSearchNarrowsPointsAndNeverTheGround(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	get(t, handler, "/v/bend-or/2026-08-02", nil)
	answer := post(t, handler, "/session/search", url.Values{
		"volume": {"bend-or"}, "q": {"zzzzznothing"},
	})
	body := answer.Body.String()
	// Every point is gone and all 148 shapes are still drawn.
	if !strings.Contains(body, ">148 features<") {
		t.Errorf("a search that matches nothing did not leave the ground standing:\n%s", body)
	}
	if !strings.Contains(body, "No visible features match.") {
		t.Error("the empty list does not say why it is empty")
	}
	if !strings.Contains(body, "“zzzzznothing”") {
		t.Error("the panel does not say what it is searching for")
	}
}
