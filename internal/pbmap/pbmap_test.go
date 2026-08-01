package pbmap

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
)

// fixture exercises everything Translate decides: a declared category with a
// used and an unused type, district pins under the favorites category, prose
// descriptions, and the night-city transformation.
func fixture() Capture {
	return Capture{
		Source:    Source,
		GameSlug:  "cyberpunk-2077",
		MapSlug:   "night-city",
		GameTitle: "Cyberpunk 2077",
		MapTitle:  "Night City",
		Map: MapConfig{
			TileServer:     "https://storage-cdn.piggyback.com/images/tiles/cbp/{z}/{x}/{y}.webp",
			MinZoom:        3,
			MaxZoom:        7,
			PremiumMaxZoom: 9,
			Transform:      Transform{A: 0.015625, B: 128, C: -0.015625, D: 128},
		},
		Labels: Labels{
			Categories: []Label{{Key: "gigs", Label: "Gigs"}},
			Types: []Label{
				{Key: "agent-saboteur", Label: "Agent Saboteur"},
				{Key: "province", Label: "Province"},
			},
		},
		Categories: []Category{
			{ID: "cat-fav", Key: "favorites", Position: 0},
			{ID: "cat-gigs", Key: "gigs", Position: 30, Types: []Type{
				{ID: "t-as", Key: "agent-saboteur", Position: 10},
				{ID: "t-empty", Key: "thievery", Position: 20},
			}},
		},
		Pins: []Pin{
			{ID: "pin-b", X: "-1992.51", Y: "4097.82", CategoryKey: "gigs", TypeKey: "agent-saboteur",
				Name: "WELCOME TO AMERICA, COMRADE", Description: "Unlock Condition – Street Cred Tier 1"},
			{ID: "pin-a", X: "0", Y: "0", CategoryKey: "gigs", TypeKey: "agent-saboteur", Name: "Centre Gig"},
			{ID: "pin-d", X: "-4000", Y: "5000", CategoryKey: "favorites", TypeKey: "province", Name: "Watson"},
		},
		Levels: []Level{
			{Zoom: 3, MinX: 0, MinY: 0, MaxX: 6, MaxY: 6},
			{Zoom: 7, MinX: 0, MinY: 0, MaxX: 111, MaxY: 111},
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

func TestTranslateShape(t *testing.T) {
	m := translate(t, fixture())

	if m.Slug != "night-city" || m.Game.Slug != "cyberpunk-2077" {
		t.Fatalf("identity: %q / %q", m.Game.Slug, m.Slug)
	}
	set := m.Config.TileSets[0]
	if set.Path != "cbp" || set.Extension != "webp" {
		t.Fatalf("tile set: %+v", set)
	}
	if set.MinZoom != 3 || set.MaxZoom != 7 {
		t.Fatalf("zoom range: %d..%d", set.MinZoom, set.MaxZoom)
	}
	if bound := set.Bounds["7"]; bound.X.Max != 111 || bound.Y.Max != 111 {
		t.Fatalf("deepest bounds: %+v", bound)
	}

	if len(m.Groups) != 2 {
		t.Fatalf("groups: %+v", m.Groups)
	}
	gigs := m.Groups[0]
	if gigs.Title != "Gigs" || len(gigs.Categories) != 1 {
		t.Fatalf("gigs group: %+v", gigs)
	}
	category := gigs.Categories[0]
	if category.Title != "Agent Saboteur" || category.Icon != "agent-saboteur" ||
		category.DisplayType != "markers" {
		t.Fatalf("gig category: %+v", category)
	}
	if len(category.Locations) != 2 {
		t.Fatalf("gig pins: %+v", category.Locations)
	}
	// Pins sort by id, so pin-a comes first.
	if category.Locations[0].Title != "Centre Gig" {
		t.Fatalf("pin order: %+v", category.Locations)
	}
	if category.Locations[1].Description != "Unlock Condition – Street Cred Tier 1" {
		t.Fatalf("description lost: %+v", category.Locations[1])
	}

	districts := m.Groups[1]
	if districts.Title != "Districts" || len(districts.Categories) != 1 {
		t.Fatalf("districts group: %+v", districts)
	}
	province := districts.Categories[0]
	if province.Title != "Province" || province.DisplayType != "text" {
		t.Fatalf("province category: %+v", province)
	}
	if province.Locations[0].Title != "Watson" {
		t.Fatalf("district pin: %+v", province.Locations[0])
	}

	// The empty thievery type keeps no category.
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			if category.Icon == "thievery" {
				t.Fatal("a pinless type kept its category")
			}
		}
	}
}

// TestCoordinatesRoundTrip runs a pin's synthetic coordinates through the
// viewer's projection and expects the pixel Piggyback draws it at: the game
// coordinate through the map's own transformation, scaled to the world.
func TestCoordinatesRoundTrip(t *testing.T) {
	m := translate(t, fixture())

	project := func(latitude, longitude float64) (float64, float64) {
		worldTiles := math.Pow(2, mgdoc.SourceZoom)
		x := ((longitude + 180) / 360) * worldTiles * mgdoc.TileSize
		y := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * worldTiles * mgdoc.TileSize
		return x, y
	}
	expected := map[string][2]float64{
		"Centre Gig":                  {4096, 4096},
		"WELCOME TO AMERICA, COMRADE": {-1992.51/2 + 4096, -4097.82/2 + 4096},
		"Watson":                      {-4000.0/2 + 4096, -5000.0/2 + 4096},
	}
	seen := 0
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				want, ok := expected[location.Title]
				if !ok {
					continue
				}
				seen++
				x, y := project(location.Latitude, location.Longitude)
				if math.Abs(x-want[0]) > 1e-6 || math.Abs(y-want[1]) > 1e-6 {
					t.Fatalf("%s projects to %.6f,%.6f, want %.3f,%.3f",
						location.Title, x, y, want[0], want[1])
				}
			}
		}
	}
	if seen != len(expected) {
		t.Fatalf("saw %d of %d pins", seen, len(expected))
	}
}

func TestTranslateIsDeterministic(t *testing.T) {
	doc, _ := json.Marshal(fixture())
	first, err := Translate(doc)
	if err != nil {
		t.Fatal(err)
	}
	shuffled := fixture()
	shuffled.Pins[0], shuffled.Pins[2] = shuffled.Pins[2], shuffled.Pins[0]
	shuffled.Categories[0], shuffled.Categories[1] = shuffled.Categories[1], shuffled.Categories[0]
	reordered, _ := json.Marshal(shuffled)
	second, err := Translate(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the same capture translated to different bytes")
	}
}

func TestTranslateRejectsBadCaptures(t *testing.T) {
	undeclared := fixture()
	undeclared.Pins[0].TypeKey = "never-declared"
	if _, err := translateErr(undeclared); err == nil || !strings.Contains(err.Error(), "no category declares") {
		t.Fatalf("undeclared type: %v", err)
	}

	unsurveyed := fixture()
	unsurveyed.Levels = nil
	if _, err := translateErr(unsurveyed); err == nil {
		t.Fatal("a capture with no observed levels was accepted")
	}

	flat := fixture()
	flat.Map.Transform = Transform{}
	if _, err := translateErr(flat); err == nil {
		t.Fatal("a capture with no transformation was accepted")
	}
}

func translateErr(capture Capture) ([]byte, error) {
	doc, err := json.Marshal(capture)
	if err != nil {
		return nil, err
	}
	return Translate(doc)
}

func TestMaybeTranslatePassesOthersThrough(t *testing.T) {
	doc := []byte(`{"id": 5}`)
	out, err := MaybeTranslate("ign-map", doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, doc) {
		t.Fatal("another source's snapshot was rewritten")
	}
}
