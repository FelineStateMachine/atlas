package piggyback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// A capture that is everything a good one is, so a test can spoil exactly one
// thing about it and see the reader refuse for exactly that reason.
func wellFormed() capture {
	return capture{
		Source:    source,
		GameSlug:  "cyberpunk-2077",
		MapSlug:   "night-city",
		GameTitle: "Cyberpunk 2077",
		MapTitle:  "Night City",
		Map: sheet{
			TileServer: "https://tiles.example/tiles/cbp/{z}/{x}/{y}.webp",
			MaxZoom:    5,
			Transform:  verifiedTransforms["cyberpunk-2077"],
		},
		Levels: []level{{Zoom: 0, MaxX: 0, MaxY: 0}},
		Categories: []category{{
			Key:   "shops",
			Types: []kind{{Key: "weapon-shop"}},
		}},
		Pins: []pin{{ID: "a", X: "0", Y: "0", CategoryKey: "shops", TypeKey: "weapon-shop", Name: "One"}},
	}
}

func TestCheckAcceptsAVerifiedCapture(t *testing.T) {
	raw := wellFormed()
	if err := check(&raw); err != nil {
		t.Fatalf("a well-formed capture was refused: %v", err)
	}
}

// The gate issue #5 §5.2 names: Piggyback refuses unverified transforms. A
// Piggyback map is placed by numbers read off its own page, and a wrong pair
// puts every pin in the volume somewhere plausible and wrong.
func TestCheckRefusesAnUnverifiedTransform(t *testing.T) {
	tests := []struct {
		name  string
		spoil func(*capture)
		says  string
	}{
		{
			name:  "a game nobody has checked",
			spoil: func(c *capture) { c.GameSlug = "some-other-game" },
			says:  "no transformation has been verified",
		},
		{
			name:  "numbers that are not the verified ones",
			spoil: func(c *capture) { c.Map.Transform.A = 0.03125 },
			says:  "not the verified",
		},
		{
			name:  "no transformation at all",
			spoil: func(c *capture) { c.Map.Transform = Transform{} },
			says:  "not the verified",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := wellFormed()
			test.spoil(&raw)
			err := check(&raw)
			if err == nil {
				t.Fatal("the capture was accepted, so every pin in it would be misplaced")
			}
			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("refused with %q, which does not say %q", err, test.says)
			}
		})
	}
}

func TestCheckRefusesAMalformedCapture(t *testing.T) {
	tests := []struct {
		name  string
		spoil func(*capture)
		says  string
	}{
		{"another source's bytes", func(c *capture) { c.Source = "ign-wikimaps" }, "not \"piggyback\""},
		{"no map", func(c *capture) { c.MapSlug = "" }, "names no map"},
		{"no observed level", func(c *capture) { c.Levels = nil }, "observed no tile level"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := wellFormed()
			test.spoil(&raw)
			if err := check(&raw); err == nil {
				t.Fatal("the capture was accepted")
			} else if !strings.Contains(err.Error(), test.says) {
				t.Errorf("refused with %q, which does not say %q", err, test.says)
			}
		})
	}
}

// A pin of a type no category declares would be silently dropped by anything
// that filtered instead of failing, and a dropped pin is invisible in a bundle.
func TestCollectionsRefuseAnUndeclaredType(t *testing.T) {
	raw := wellFormed()
	raw.Pins = append(raw.Pins, pin{
		ID: "b", X: "0", Y: "0", CategoryKey: "shops", TypeKey: "mystery", Name: "Two",
	})
	raw.normalize()
	if _, err := collectionsOf(&raw, doc.NewIDSpace(), "cyberpunk-2077/night-city"); err == nil {
		t.Fatal("a pin of an undeclared type translated anyway, so it would have vanished")
	}
}

// District names are the map's own region labels, filed by Piggyback under the
// reader's favourites. They are text on its map and they stay text here.
func TestDistrictNamesBecomeTextLabels(t *testing.T) {
	raw := wellFormed()
	raw.Categories = append(raw.Categories, category{Key: "favorites", Types: []kind{}})
	raw.Pins = append(raw.Pins, pin{
		ID: "c", X: "0", Y: "0", CategoryKey: "favorites", TypeKey: "province", Name: "Watson",
	})
	raw.normalize()
	collections, err := collectionsOf(&raw, doc.NewIDSpace(), "cyberpunk-2077/night-city")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, collection := range collections {
		if collection.Icon != "province" {
			continue
		}
		found = true
		if collection.Group != "Districts" {
			t.Errorf("district labels file under %q", collection.Group)
		}
		if collection.Attrs[semconv.KeyRenderAs] != semconv.RenderAsText {
			t.Errorf("district labels render as %q", collection.Attrs[semconv.KeyRenderAs])
		}
	}
	if !found {
		t.Error("the district labels were dropped")
	}
}

func TestTileSetPath(t *testing.T) {
	tests := []struct {
		template string
		want     string
		refuse   bool
	}{
		{"https://tiles.example/tiles/cbp/{z}/{x}/{y}.webp", "cbp", false},
		{"https://tiles.example/cbp/{z}/{x}/{y}.webp", "", true},
		{"https://tiles.example/tiles/{z}/{x}/{y}.webp", "", true},
	}
	for _, test := range tests {
		got, err := TileSetPath(test.template)
		if test.refuse {
			if err == nil {
				t.Errorf("%s named layer %q, want a refusal", test.template, got)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("%s named %q (%v), want %q", test.template, got, err, test.want)
		}
	}
}

// A capture round-trips through JSON on its way to the archive, so the shape
// this package declares has to be the shape the archive holds.
func TestCaptureRoundTrips(t *testing.T) {
	raw := wellFormed()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var back capture
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Map.Transform != raw.Map.Transform || back.GameSlug != raw.GameSlug {
		t.Errorf("a capture came back as %+v", back)
	}
}
