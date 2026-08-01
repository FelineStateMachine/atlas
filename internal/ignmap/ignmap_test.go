package ignmap

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"math"
	"strings"
	"testing"
)

// fixture is a small capture exercising everything Translate decides: a type
// with markers, a type without, a parented type, and coordinates at the
// corners and centre of the image.
func fixture() Capture {
	return Capture{
		Source:     Source,
		ObjectSlug: "cyberpunk-2077",
		MapSlug:    "night-city",
		GameTitle:  "Cyberpunk 2077",
		MapTitle:   "Night City",
		Map: MapConfig{
			Width: 0.8822243, Height: 1,
			MinZoom: 2, MaxZoom: 5,
			InitialLat: -0.5, InitialLng: 0.44111216,
			BackgroundColor: "#110b0b",
			Tileset:         "https://oyster.ignimgs.com/ignmedia/wikimaps/cyberpunk-2077/night-city/{z}/{x}-{y}.jpg",
		},
		Types: []Type{
			{TypeSlug: "fast-travel", TypeName: "Fast Travel"},
			{TypeSlug: "elevator", TypeName: "Elevator"},
			{TypeSlug: "tarot-card", TypeName: "Tarot Card", ParentTypeSlug: "collectibles"},
		},
		Markers: []Marker{
			{ID: "cp:nc:b", Lat: -1, Lng: 0.8822243, MarkerName: "Corner", TypeSlug: "fast-travel"},
			{ID: "cp:nc:a", Lat: -0.5, Lng: 0.4411, MarkerName: "Centre", TypeSlug: "fast-travel", WikiPage: "Centre_Page"},
			{ID: "cp:nc:c", Lat: 0, Lng: 0, MarkerName: "Origin", TypeSlug: "tarot-card"},
		},
	}
}

func translate(t *testing.T, capture Capture) mgMap {
	t.Helper()
	doc, err := json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Translate(doc)
	if err != nil {
		t.Fatal(err)
	}
	var m mgMap
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTranslateIsDeterministic(t *testing.T) {
	capture := fixture()
	doc, _ := json.Marshal(capture)
	first, err := Translate(doc)
	if err != nil {
		t.Fatal(err)
	}

	// The same markers arriving in a different order are the same capture.
	shuffled := fixture()
	shuffled.Markers[0], shuffled.Markers[2] = shuffled.Markers[2], shuffled.Markers[0]
	shuffled.Types[0], shuffled.Types[1] = shuffled.Types[1], shuffled.Types[0]
	reordered, _ := json.Marshal(shuffled)
	second, err := Translate(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the same capture translated to different bytes")
	}
}

func TestTranslateShape(t *testing.T) {
	m := translate(t, fixture())

	if m.Slug != "night-city" || m.Title != "Night City" {
		t.Fatalf("map identity: %q %q", m.Slug, m.Title)
	}
	if m.Game.Slug != "cyberpunk-2077" || m.Game.Title != "Cyberpunk 2077" {
		t.Fatalf("game identity: %q %q", m.Game.Slug, m.Game.Title)
	}

	if len(m.Config.TileSets) != 1 {
		t.Fatalf("tile sets: %d", len(m.Config.TileSets))
	}
	set := m.Config.TileSets[0]
	if set.Path != "cyberpunk-2077/night-city" || set.Extension != "jpg" {
		t.Fatalf("tile set: %+v", set)
	}
	if set.MinZoom != 0 || set.MaxZoom != 5 {
		t.Fatalf("zoom range: %d..%d", set.MinZoom, set.MaxZoom)
	}
	for zoom := 0; zoom <= 5; zoom++ {
		if _, ok := set.Bounds[itoa(zoom)]; !ok {
			t.Fatalf("no bounds declared for zoom %d", zoom)
		}
	}
	// The image is narrower than tall: at zoom 5 the last column holding any
	// content is tile 28 of 32, while rows fill the square.
	deepest := set.Bounds["5"]
	if deepest.X.Max != 28 || deepest.Y.Max != 31 {
		t.Fatalf("deepest bounds: x..%d y..%d", deepest.X.Max, deepest.Y.Max)
	}
	top := set.Bounds["0"]
	if top.X.Max != 0 || top.Y.Max != 0 {
		t.Fatalf("top of the pyramid is not a single tile: %+v", top)
	}

	// Two groups: the unparented types, and the collectibles parent. The
	// elevator type has no markers and no category.
	if len(m.Groups) != 2 {
		t.Fatalf("groups: %d", len(m.Groups))
	}
	if len(m.Groups[0].Categories) != 1 || m.Groups[0].Categories[0].Icon != "fast-travel" {
		t.Fatalf("first group: %+v", m.Groups[0])
	}
	if m.Groups[1].Title != "Collectibles" {
		t.Fatalf("parented group title: %q", m.Groups[1].Title)
	}
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			if category.Icon == "elevator" {
				t.Fatal("a markerless type kept its category")
			}
		}
	}
}

// TestSyntheticCoordinatesRoundTrip runs the synthetic latitude and longitude
// through the viewer's projection and expects the marker's pixel back.
func TestSyntheticCoordinatesRoundTrip(t *testing.T) {
	m := translate(t, fixture())

	project := func(latitude, longitude float64) (float64, float64) {
		worldTiles := math.Pow(2, sourceZoom)
		x := ((longitude + 180) / 360) * worldTiles * tileSize
		y := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * worldTiles * tileSize
		return x, y
	}

	byTitle := map[string][2]float64{
		"Origin": {0, 0},
		"Centre": {0.4411 * worldSize, 0.5 * worldSize},
		"Corner": {0.8822243 * worldSize, worldSize},
	}
	seen := 0
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				want, ok := byTitle[location.Title]
				if !ok {
					continue
				}
				seen++
				x, y := project(location.Latitude, location.Longitude)
				if math.Abs(x-want[0]) > 1e-6 || math.Abs(y-want[1]) > 1e-6 {
					t.Fatalf("%s projects to %.8f,%.8f, want %.1f,%.1f",
						location.Title, x, y, want[0], want[1])
				}
			}
		}
	}
	if seen != len(byTitle) {
		t.Fatalf("saw %d of %d markers", seen, len(byTitle))
	}
}

func TestIdentifiersAreStable(t *testing.T) {
	first := translate(t, fixture())
	second := translate(t, fixture())
	if first.ID != second.ID || first.ID <= 0 || first.ID > math.MaxInt32 {
		t.Fatalf("map id: %d then %d", first.ID, second.ID)
	}
	firstLocations := locationIDs(first)
	secondLocations := locationIDs(second)
	for title, id := range firstLocations {
		if secondLocations[title] != id {
			t.Fatalf("%s renumbered: %d then %d", title, id, secondLocations[title])
		}
		if id <= 0 || id > math.MaxInt32 {
			t.Fatalf("%s id %d leaves the positive int31 range", title, id)
		}
	}
}

func locationIDs(m mgMap) map[string]int64 {
	out := make(map[string]int64)
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				out[location.Title] = location.ID
			}
		}
	}
	return out
}

func TestIDCollisionIsFatal(t *testing.T) {
	space := newIDSpace()
	hash := fnv.New32a()
	_, _ = hash.Write([]byte("some-seed"))
	occupied := int64(hash.Sum32() & 0x7fffffff)
	space.seen[occupied] = "already-here"
	if _, err := space.claim("some-seed"); err == nil {
		t.Fatal("claiming an occupied id did not fail")
	}
}

func TestTranslateRejectsBadCaptures(t *testing.T) {
	duplicate := fixture()
	duplicate.Types = append(duplicate.Types, Type{TypeSlug: "fast-travel", TypeName: "Again"})
	if _, err := translateErr(duplicate); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate type: %v", err)
	}

	stray := fixture()
	stray.Markers[0].TypeSlug = "never-declared"
	if _, err := translateErr(stray); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("undeclared type: %v", err)
	}

	foreign := fixture()
	foreign.Source = "someone-else"
	if _, err := translateErr(foreign); err == nil {
		t.Fatal("foreign source accepted")
	}
}

func translateErr(capture Capture) ([]byte, error) {
	doc, err := json.Marshal(capture)
	if err != nil {
		return nil, err
	}
	return Translate(doc)
}

func TestMaybeTranslatePassesMapGenieThrough(t *testing.T) {
	doc := []byte(`{"id": 5, "title": "As Captured"}`)
	out, err := MaybeTranslate("map", doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, doc) {
		t.Fatal("a MapGenie snapshot was rewritten")
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}
