package trekmap

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
)

// fixture is a small capture exercising everything Translate decides: two
// feature types, real Gazetteer coordinates east and west of the meridian,
// both hemispheres, and a feature with no origin text.
func fixture() Capture {
	return Capture{
		Source:   Source,
		Body:     "mars",
		Layer:    "Mars_Viking_MDIM21_ClrMosaic_global_232m",
		MapSlug:  "global",
		MapTitle: "Global",
		Map: MapConfig{
			MaxZoom:    6,
			Extension:  "jpg",
			LayerTitle: "Viking MDIM 2.1",
		},
		Variants: []Variant{
			{Layer: "Mars_MGS_MOLA_ClrShade_merge_global_463m",
				Title: "MOLA Elevation", MaxZoom: 6, Extension: "jpg"},
		},
		Features: []Feature{
			{ID: 4453, Name: "Olympus Mons", Type: "Mons, montes", Code: "MO",
				Latitude: 18.6528, Longitude: 226.1975, DiameterKM: 610.13,
				Origin: "Classical albedo feature name."},
			{ID: 2071, Name: "Gale", Type: "Crater, craters", Code: "AA",
				Latitude: -5.3672, Longitude: 137.811, DiameterKM: 154.084,
				Origin: "Walter F.; Australian astronomer (1865-1945)."},
			{ID: 2432, Name: "Hellas Planitia", Type: "Planitia, planitiae", Code: "PL",
				Latitude: -42.4301, Longitude: 70.5025, DiameterKM: 2299.16},
		},
	}
}

func translate(t *testing.T, capture Capture) mgdoc.Map {
	t.Helper()
	doc, err := json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Translate(doc)
	if err != nil {
		t.Fatal(err)
	}
	var m mgdoc.Map
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTranslateIsDeterministic(t *testing.T) {
	doc, _ := json.Marshal(fixture())
	first, err := Translate(doc)
	if err != nil {
		t.Fatal(err)
	}

	// The same features arriving in a different order are the same capture.
	shuffled := fixture()
	shuffled.Features[0], shuffled.Features[2] = shuffled.Features[2], shuffled.Features[0]
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

	if m.Slug != "global" || m.Title != "Global" {
		t.Fatalf("map identity: %q %q", m.Slug, m.Title)
	}
	if m.Game.Slug != "mars" || m.Game.Title != "Mars" {
		t.Fatalf("game identity: %q %q", m.Game.Slug, m.Game.Title)
	}

	if len(m.Config.TileSets) != 2 {
		t.Fatalf("tile sets: %d", len(m.Config.TileSets))
	}
	set := m.Config.TileSets[0]
	if set.Path != "mars/EQ/Mars_Viking_MDIM21_ClrMosaic_global_232m" {
		t.Fatalf("tile set path: %q", set.Path)
	}
	if set.Name != "Viking MDIM 2.1" || set.Extension != "jpg" {
		t.Fatalf("tile set: %+v", set)
	}
	if set.MinZoom != 0 || set.MaxZoom != 6 {
		t.Fatalf("zoom range: %d..%d", set.MinZoom, set.MaxZoom)
	}
	// The sibling mosaic rides the same map as a second layer in the same
	// window, under its own name and path.
	sibling := m.Config.TileSets[1]
	if sibling.Name != "MOLA Elevation" ||
		sibling.Path != "mars/EQ/Mars_MGS_MOLA_ClrShade_merge_global_463m" {
		t.Fatalf("variant tile set: %+v", sibling)
	}
	// The planet fills the width of the world square and the top half of its
	// height, at every level of both pyramids.
	for _, layer := range m.Config.TileSets {
		for zoom := 0; zoom <= layer.MaxZoom; zoom++ {
			bounds, ok := layer.Bounds[strconv.Itoa(zoom)]
			if !ok {
				t.Fatalf("%s declares no bounds for zoom %d", layer.Name, zoom)
			}
			wantX, wantY := 1<<zoom-1, 1<<zoom/2-1
			if zoom == 0 {
				wantX, wantY = 0, 0
			}
			if bounds.X.Min != 0 || bounds.Y.Min != 0 || bounds.X.Max != wantX || bounds.Y.Max != wantY {
				t.Fatalf("%s zoom %d bounds: %+v", layer.Name, zoom, bounds)
			}
		}
	}

	if len(m.Groups) != 1 || m.Groups[0].Title != "Nomenclature" {
		t.Fatalf("groups: %+v", m.Groups)
	}
	titles := make([]string, 0, 3)
	for _, category := range m.Groups[0].Categories {
		titles = append(titles, category.Title+"/"+category.Icon)
	}
	want := "Crater/crater Mons/mons Planitia/planitia"
	if strings.Join(titles, " ") != want {
		t.Fatalf("categories: %v, want %s", titles, want)
	}
}

// TestSyntheticCoordinatesRoundTrip runs the synthetic latitude and longitude
// through the viewer's projection and expects each feature's equirectangular
// pixel back: longitude across the full 8192-pixel width, latitude down the
// 4096-pixel top half, with the Gazetteer's 0..360 longitudes wrapped into
// the mosaic's -180..180 window.
func TestSyntheticCoordinatesRoundTrip(t *testing.T) {
	m := translate(t, fixture())

	pixel := func(longitude, latitude float64) (float64, float64) {
		if longitude >= 180 {
			longitude -= 360
		}
		return (longitude + 180) / 360 * mgdoc.WorldSize,
			(90 - latitude) / 180 * mgdoc.WorldSize / 2
	}
	byTitle := map[string][2]float64{}
	for _, feature := range fixture().Features {
		x, y := pixel(feature.Longitude, feature.Latitude)
		byTitle[feature.Name] = [2]float64{x, y}
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
				x := mgdoc.ProjectX(location.Longitude, mgdoc.SourceZoom, 0)
				y := mgdoc.ProjectY(location.Latitude, mgdoc.SourceZoom, 0)
				if math.Abs(x-want[0]) > 1e-6 || math.Abs(y-want[1]) > 1e-6 {
					t.Fatalf("%s projects to %.6f,%.6f, want %.6f,%.6f",
						location.Title, x, y, want[0], want[1])
				}
			}
		}
	}
	if seen != len(byTitle) {
		t.Fatalf("saw %d of %d features", seen, len(byTitle))
	}
}

func TestDescriptionsCarryTheRealPlace(t *testing.T) {
	m := translate(t, fixture())
	descriptions := map[string]string{}
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				descriptions[location.Title] = location.Description
			}
		}
	}
	olympus := descriptions["Olympus Mons"]
	if olympus != "18.65°N 226.2°E · 610.13 km across — Classical albedo feature name." {
		t.Fatalf("Olympus Mons card: %q", olympus)
	}
	// A feature without origin text still says where and how big it is.
	hellas := descriptions["Hellas Planitia"]
	if hellas != "42.43°S 70.5°E · 2299.16 km across" {
		t.Fatalf("Hellas card: %q", hellas)
	}
	for title, description := range descriptions {
		if strings.Contains(description, "http") {
			t.Fatalf("%s card carries a link: %q", title, description)
		}
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

func locationIDs(m mgdoc.Map) map[string]int64 {
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

func TestTranslateRejectsBadCaptures(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Capture)
		mention string
	}{
		{"foreign source", func(c *Capture) { c.Source = "someone-else" }, "source"},
		{"no layer", func(c *Capture) { c.Layer = "" }, "no map"},
		{"no pyramid", func(c *Capture) { c.Map.MaxZoom = 0 }, "pyramid"},
		{"no features", func(c *Capture) { c.Features = nil }, "features"},
		{"unnamed feature", func(c *Capture) { c.Features[0].Name = "" }, "name"},
		{"feature without id", func(c *Capture) { c.Features[0].ID = 0 }, "identifier"},
		{"latitude off the planet", func(c *Capture) { c.Features[0].Latitude = 91 }, "latitude"},
		{"longitude off the planet", func(c *Capture) { c.Features[0].Longitude = -5 }, "longitude"},
		{"doubled feature", func(c *Capture) { c.Features[1].ID = c.Features[0].ID }, "collision"},
		{"doubled layer", func(c *Capture) { c.Variants[0].Layer = c.Layer }, "twice"},
		{"depthless variant", func(c *Capture) { c.Variants[0].MaxZoom = 0 }, "pyramid"},
	}
	for _, test := range cases {
		capture := fixture()
		test.mutate(&capture)
		doc, err := json.Marshal(capture)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Translate(doc); err == nil || !strings.Contains(err.Error(), test.mention) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestMaybeTranslatePassesOthersThrough(t *testing.T) {
	doc := []byte(`{"id": 5, "title": "As Captured"}`)
	out, err := MaybeTranslate("map", doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, doc) {
		t.Fatal("a MapGenie snapshot was rewritten")
	}
}
