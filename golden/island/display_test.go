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

// TestTheLabelToggleWearsTheMarkOfWhatItAsks holds the label button's glyph to
// the reference's two, because the clean-room template hardcoded one of them.
//
// The two rows that wear the control are not asking the same question. An
// area draws its names *on* the ground and the toggle is whether they speak
// unasked, which the reference drew as a bar over a stem. A point collection
// curated as text has no marker to fall back on -- the names are the whole of
// how it draws -- and the reference drew a luggage tag. Rendered with one
// glyph, the legend told the reader the two rows offered the same thing.
//
// Hyrule is the volume that can say all three words in one page: one area
// collection, two point collections curated as text, and 159 rows of plain
// pins that wear no button at all and keep the column as space, so the count
// beside them stays a column no row disagrees about.
func TestTheLabelToggleWearsTheMarkOfWhatItAsks(t *testing.T) {
	const slug = "zelda-tears-of-the-kingdom"
	volume := fixtureVolume(t, slug)
	handler, _ := newApp(t, volume)
	world := volume.Manifest().Worlds[0].Slug
	page := get(t, handler, "/v/"+slug+"/"+world, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	body := page.Body.String()

	// The first path of each mark, which is enough to tell them apart and
	// short enough to survive a formatter.
	const (
		bar = `d="M3.5 3.5h9M8 3.5v9"`
		tag = `d="M8.6 2.5H2.5v6.1`
	)
	for _, tt := range []struct {
		id    string
		what  string
		want  string
		wrong string
	}{
		{"1834502030", "Regions, an area", bar, tag},
		{"8988", "Area, points curated as text", tag, bar},
		{"8989", "Province, points curated as text", tag, bar},
	} {
		button := toggleMarkup(body, tt.id)
		switch {
		case button == "":
			t.Errorf("%s wears no label toggle at all", tt.what)
		case !strings.Contains(button, tt.want):
			t.Errorf("%s wears the wrong mark: %s", tt.what, button)
		case strings.Contains(button, tt.wrong):
			t.Errorf("%s wears both marks: %s", tt.what, button)
		}
	}

	// A plain pin row has no policy to flip, so it has no button -- and the
	// column is held open anyway, which is the half a missing element would
	// silently get right and a wrong element would silently get wrong.
	pins := rowMarkup(body, "8987")
	if pins == "" {
		t.Fatal("the Building row is not in the legend")
	}
	if strings.Contains(pins, `class="label-toggle"`) {
		t.Errorf("a pin row was offered a label toggle: %s", pins)
	}
	if !strings.Contains(pins, `class="label-toggle-spacer"`) {
		t.Errorf("a pin row does not hold its column open: %s", pins)
	}
}

// toggleMarkup is one label button, from its own attribute to its close. The
// window stops at `</button>` for the reason ladderOf's does: the last toggle
// in the legend would otherwise take its answer from whatever was rendered
// after it.
func toggleMarkup(page, collection string) string {
	at := strings.Index(page, `data-label-toggle="`+collection+`"`)
	if at < 0 {
		return ""
	}
	rest := page[at:]
	if end := strings.Index(rest, "</button>"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// rowMarkup is one collection's row, from the checkbox that names it to the
// end of the label that holds it. Nothing nests a second label inside a
// category row, so the first close is the row's own.
func rowMarkup(page, collection string) string {
	at := strings.Index(page, `data-collection="`+collection+`"`)
	if at < 0 {
		return ""
	}
	rest := page[at:]
	if end := strings.Index(rest, "</label>"); end >= 0 {
		return rest[:end]
	}
	return rest
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

// TestTheCardSaysWhetherItIsOpen is the defect that made a correct count read
// as a blank panel, held in the one place it can be held from Go.
//
// The carried assets/css/pin-detail.css hands the results list over to the
// card with `.dock-body:has(.pin-detail:not([hidden])) .dock-results`. That
// rule asks the card a question, and the only way the card can answer it is by
// wearing `hidden` when it is closed -- which the reference implementation's
// script set by hand and the server renders here. A card that never wears it
// answers "open" in every state, and the list is hidden under an empty card
// forever.
//
// Only the attribute can be checked from here; whether the browser then draws
// the list is the parity tour's `searchResultsVisible`. So this walks the four
// states the rule distinguishes and reads the attribute off both places the
// card is rendered -- the dock's own re-render and the card's own region --
// because two answers that disagree are the same bug wearing a hat.
func TestTheCardSaysWhetherItIsOpen(t *testing.T) {
	handler, _ := newApp(t, fixtureVolume(t, "bend-or"))
	const world = "2026-08-02"
	page := get(t, handler, "/v/bend-or/"+world, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}

	// The steps run in order against one handler: "back from a card" is only
	// a state if a card was open first.
	steps := []struct {
		name string
		// concern and form are the interaction that reaches the state. The
		// first step has none: it is the page as it is first painted.
		concern string
		form    url.Values
		// hidden is what the card must be wearing once the state is reached,
		// and rows is how many results are standing behind it.
		hidden bool
		rows   int
		// title is the card's heading when one is open.
		title string
	}{
		{
			name:   "a full list and no card",
			hidden: true,
			rows:   100,
		},
		{
			name:    "an empty list and no card",
			concern: "search",
			form:    url.Values{"q": {"zzzzznothing"}},
			hidden:  true,
			rows:    0,
		},
		{
			name:    "the list back, still no card",
			concern: "search",
			form:    url.Values{"q": {""}},
			hidden:  true,
			rows:    100,
		},
		{
			name:    "a card open over the list",
			concern: "select",
			form:    url.Values{"feature": {"890910106"}, "focus": {"1"}},
			hidden:  false,
			rows:    100,
			title:   "A.C. Lucas House",
		},
		{
			name:    "back from the card",
			concern: "select",
			form:    url.Values{"feature": {""}},
			hidden:  true,
			rows:    100,
		},
	}

	for _, tt := range steps {
		t.Run(tt.name, func(t *testing.T) {
			body := page.Body.String()
			if tt.concern != "" {
				form := url.Values{"volume": {"bend-or"}}
				for name, values := range tt.form {
					form[name] = values
				}
				answer := post(t, handler, "/session/"+tt.concern, form)
				if answer.Code != http.StatusOK {
					t.Fatalf("/session/%s answered %d: %s", tt.concern, answer.Code, answer.Body)
				}
				body = answer.Body.String()
			}

			tags := openingTags(body, `<article id="atlas-detail"`)
			if len(tags) == 0 {
				t.Fatalf("the answer carries no card at all:\n%s", body)
			}
			for _, tag := range tags {
				if wearing := strings.Contains(tag, " hidden"); wearing != tt.hidden {
					t.Errorf("the card is %q; hidden = %v, want %v", tag, wearing, tt.hidden)
				}
			}

			// The list is never the one hidden: the reference implementation
			// left `#dock-results` alone and let the card's rule cover it,
			// which is what the tour reads back as searchResultsVisible.
			for _, tag := range openingTags(body, `<div class="dock-results"`) {
				if strings.Contains(tag, " hidden") {
					t.Errorf("the results list hid itself: %q", tag)
				}
			}
			if rows := strings.Count(body, `class="search-result`); rows != tt.rows {
				t.Errorf("the list carries %d rows, want %d", rows, tt.rows)
			}

			// A card that is open has something in it, and a card that is
			// closed has nothing: the emptiness is still true, it is simply
			// no longer the thing the stylesheet is asked to read.
			switch {
			case tt.title != "":
				if !strings.Contains(body, `id="detail-title">`+tt.title+`<`) {
					t.Errorf("the open card does not name %q:\n%s", tt.title, body)
				}
				if !strings.Contains(body, `id="close-detail"`) {
					t.Error("the open card offers no way back to the list")
				}
			default:
				if strings.Contains(body, `id="detail-title"`) {
					t.Error("a closed card still holds a heading")
				}
			}
		})
	}
}

// openingTags collects every opening tag in a rendered answer that starts with
// the given prefix, as far as its own `>`. A region can be rendered more than
// once in one answer -- the card is inside the dock and is also its own swap --
// and an attribute worth checking is worth checking on all of them.
func openingTags(page, prefix string) []string {
	var out []string
	for at := 0; ; {
		found := strings.Index(page[at:], prefix)
		if found < 0 {
			return out
		}
		at += found
		end := strings.IndexByte(page[at:], '>')
		if end < 0 {
			return out
		}
		out = append(out, page[at:at+end+1])
		at += end + 1
	}
}
