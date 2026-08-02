package compose

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

func tables(t *testing.T) curation.Tables {
	t.Helper()
	out, err := curation.Load()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOrderWorlds(t *testing.T) {
	worlds := func(names ...string) []composedWorld {
		out := make([]composedWorld, 0, len(names))
		for _, name := range names {
			slug, title, parent := name, name, ""
			if left, right, split := strings.Cut(name, "<"); split {
				slug, title, parent = left, left, right
			}
			out = append(out, composedWorld{Slug: slug, Title: title, Parent: parent})
		}
		return out
	}
	names := func(in []composedWorld) []string {
		out := make([]string, 0, len(in))
		for _, world := range in {
			out = append(out, world.Slug)
		}
		return out
	}

	tests := []struct {
		name   string
		volume string
		in     []composedWorld
		want   []string
	}{
		{
			name:   "unlisted worlds sort by title",
			volume: "tunic",
			in:     worlds("c", "a", "b"),
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "the curated world leads",
			volume: "fallout-new-vegas",
			in:     worlds("big-mt", "mojave-wasteland", "zion-canyon"),
			want:   []string{"mojave-wasteland", "big-mt", "zion-canyon"},
		},
		{
			name:   "a piece follows the sheet it came from",
			volume: "fallout-new-vegas",
			in:     worlds("big-mt", "north<mojave-wasteland", "mojave-wasteland"),
			want:   []string{"mojave-wasteland", "north", "big-mt"},
		},
		{
			name:   "a version history opens on the present",
			volume: "bend-or",
			in:     worlds("2024-01-01", "2026-01-01", "2025-01-01"),
			want:   []string{"2026-01-01", "2025-01-01", "2024-01-01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderWorlds(tables(t), tt.volume, tt.in)
			got := names(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("order %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("order %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestNumberCollections pins the derivation a collection with a key but no
// number gets, and the collision that fails a build rather than renaming
// somebody's collection quietly.
func TestNumberCollections(t *testing.T) {
	world := doc.World{ID: 427, Collections: []doc.Collection{
		{ID: 5984, Title: "Fox Shrine", Kind: doc.KindPoint},
		{Key: "regions", Title: "Regions", Kind: doc.KindArea},
	}}
	got, err := numberCollections(world)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != 5984 {
		t.Errorf("a native number was not passed through: %d", got[0].ID)
	}
	if got[1].ID == 0 || got[1].ID > 0x7fffffff {
		t.Errorf("a derived number is %d, want a positive int31", got[1].ID)
	}

	// The derivation is stable, and stable across the world it names.
	again, err := numberCollections(world)
	if err != nil {
		t.Fatal(err)
	}
	if again[1].ID != got[1].ID {
		t.Errorf("the derivation moved between runs: %d then %d", got[1].ID, again[1].ID)
	}
	elsewhere, err := numberCollections(doc.World{ID: 428, Collections: world.Collections[1:]})
	if err != nil {
		t.Fatal(err)
	}
	if elsewhere[0].ID == got[1].ID {
		t.Errorf("one key derived one number in two worlds: %d", elsewhere[0].ID)
	}

	t.Run("a collision fails the build", func(t *testing.T) {
		colliding := doc.World{ID: 427, Collections: []doc.Collection{
			{ID: got[1].ID, Title: "Taken", Kind: doc.KindPoint},
			{Key: "regions", Title: "Regions", Kind: doc.KindArea},
		}}
		if _, err := numberCollections(colliding); err == nil ||
			!strings.Contains(err.Error(), "collides") {
			t.Fatalf("a collision was accepted: %v", err)
		}
	})

	t.Run("neither a number nor a key", func(t *testing.T) {
		if _, err := numberCollections(doc.World{Collections: []doc.Collection{{Title: "?"}}}); err == nil {
			t.Fatal("a nameless collection was accepted")
		}
	})
}

// TestSpeakConventions holds the producer-strict gate and what composition
// speaks on a volume's behalf.
func TestSpeakConventions(t *testing.T) {
	world := func(collections ...composedCollection) []composedWorld {
		return []composedWorld{{Slug: "night-city", Collections: collections}}
	}

	t.Run("kind is mirrored, artwork is declared", func(t *testing.T) {
		worlds := world(composedCollection{
			Title: "Ripperdocs", Kind: doc.KindPoint, Icon: "ripper-doc",
			IconAsset: "ripper-doc.svg",
			Attrs:     map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
		})
		if err := speakConventions(tables(t), "cyberpunk-2077", worlds); err != nil {
			t.Fatal(err)
		}
		attrs := worlds[0].Collections[0].Attrs
		if attrs[semconv.KeyGeometryKind] != doc.KindPoint {
			t.Errorf("kind is %q", attrs[semconv.KeyGeometryKind])
		}
		if attrs[semconv.KeyIconKind] != semconv.IconKindGlyph {
			t.Errorf("artwork is %q, want a glyph", attrs[semconv.KeyIconKind])
		}
		if attrs[semconv.KeyCollectionKey] != "ripperdoc" {
			t.Errorf("merge identity is %q, want the curated one", attrs[semconv.KeyCollectionKey])
		}
	})

	t.Run("a picture says so", func(t *testing.T) {
		worlds := world(composedCollection{
			Title: "Shops", Kind: doc.KindPoint, IconAsset: "shops.png", IconPicture: true,
			Attrs: map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
		})
		if err := speakConventions(tables(t), "tunic", worlds); err != nil {
			t.Fatal(err)
		}
		if got := worlds[0].Collections[0].Attrs[semconv.KeyIconKind]; got != semconv.IconKindPicture {
			t.Errorf("artwork is %q, want a picture", got)
		}
	})

	t.Run("what a source said is not overwritten", func(t *testing.T) {
		worlds := world(composedCollection{
			Title: "Labels", Kind: doc.KindPoint,
			Attrs: map[string]string{
				semconv.KeyRenderAs:      semconv.RenderAsText,
				semconv.KeyGeometryKind:  doc.KindPoint,
				semconv.KeyCollectionKey: "mine",
			},
			Icon: "ripper-doc",
		})
		if err := speakConventions(tables(t), "cyberpunk-2077", worlds); err != nil {
			t.Fatal(err)
		}
		attrs := worlds[0].Collections[0].Attrs
		if attrs[semconv.KeyRenderAs] != semconv.RenderAsText {
			t.Errorf("render-as was overwritten with %q", attrs[semconv.KeyRenderAs])
		}
		if attrs[semconv.KeyCollectionKey] != "mine" {
			t.Errorf("a declared merge identity was overwritten with %q", attrs[semconv.KeyCollectionKey])
		}
	})

	t.Run("an outset comes from curation", func(t *testing.T) {
		worlds := []composedWorld{{Slug: "skyrim", IconOutset: "dark"}}
		if err := speakConventions(tables(t), "skyrim", worlds); err != nil {
			t.Fatal(err)
		}
		if worlds[0].Attrs[semconv.KeyIconOutset] != semconv.OutsetDark {
			t.Errorf("outset is %q", worlds[0].Attrs[semconv.KeyIconOutset])
		}
	})

	t.Run("an unregistered key fails the build", func(t *testing.T) {
		worlds := world(composedCollection{
			Title: "Odd", Kind: doc.KindArea,
			//depcheck:allow semconvlit the point of the case is a key the registry does not know, which by definition has no constant
			Attrs: map[string]string{"atlas.made.up": "yes"},
		})
		if err := speakConventions(tables(t), "tunic", worlds); err == nil {
			t.Fatal("an unregistered key was accepted")
		}
	})

	t.Run("a key on the wrong entity fails the build", func(t *testing.T) {
		worlds := world(composedCollection{
			Title: "Odd", Kind: doc.KindArea,
			Features: []composedFeature{{Feature: doc.Feature{
				Title: "Ground",
				Attrs: map[string]string{semconv.KeyIconOutset: semconv.OutsetDark},
			}}},
		})
		if err := speakConventions(tables(t), "tunic", worlds); err == nil {
			t.Fatal("a world's key on a feature was accepted")
		}
	})

	t.Run("speaking for one piece does not speak for another", func(t *testing.T) {
		shared := map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin}
		worlds := []composedWorld{
			{Slug: "a", Collections: []composedCollection{{Title: "A", Kind: doc.KindPoint, Attrs: shared}}},
			{Slug: "b", Collections: []composedCollection{{Title: "B", Kind: doc.KindPoint, Attrs: shared}}},
		}
		if err := speakConventions(tables(t), "tunic", worlds); err != nil {
			t.Fatal(err)
		}
		if _, leaked := shared[semconv.KeyGeometryKind]; leaked {
			t.Error("composition wrote through a shared attribute map")
		}
	})
}

func TestContentExtent(t *testing.T) {
	grid := surfaceGrid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192}
	window := tiles.Box{Width: grid.Size, Height: grid.Size}
	at := func(lat, lng float64) composedFeature {
		return composedFeature{Feature: doc.Feature{At: &doc.Position{Lat: lat, Lng: lng}}}
	}

	t.Run("nothing to measure", func(t *testing.T) {
		if _, ok := contentExtent(nil, 0, window, grid); ok {
			t.Error("an empty world reported an extent")
		}
	})

	t.Run("a margin surrounds the outermost feature", func(t *testing.T) {
		collections := []composedCollection{{Kind: doc.KindPoint, Features: []composedFeature{
			at(0, 0), at(0, 0.1),
		}}}
		box, ok := contentExtent(collections, 0, window, grid)
		if !ok {
			t.Fatal("no extent")
		}
		// Two points a hair apart: the margin floor decides the size.
		if box.Width < 2*int(surfaceMarginFloor) {
			t.Errorf("box %+v is narrower than two margins", box)
		}
	})

	t.Run("a feature off the sheet is left out of the reckoning", func(t *testing.T) {
		inside := []composedCollection{{Kind: doc.KindPoint, Features: []composedFeature{at(0, 0)}}}
		withStray := []composedCollection{{Kind: doc.KindPoint, Features: []composedFeature{
			at(0, 0), at(85, 179),
		}}}
		small, _ := contentExtent(inside, 0, tiles.Box{X: 4000, Y: 4000, Width: 200, Height: 200}, grid)
		large, _ := contentExtent(withStray, 0, tiles.Box{X: 4000, Y: 4000, Width: 200, Height: 200}, grid)
		if small != large {
			t.Errorf("a feature outside the window changed the extent: %+v then %+v", small, large)
		}
	})

	t.Run("a shard measures only its own", func(t *testing.T) {
		collections := []composedCollection{{Kind: doc.KindPoint, Features: []composedFeature{
			{Feature: doc.Feature{At: &doc.Position{}}, Shard: 1},
			{Feature: doc.Feature{At: &doc.Position{Lat: 40, Lng: 40}}, Shard: 2},
		}}}
		one, _ := contentExtent(collections, 1, window, grid)
		both, _ := contentExtent(collections, 0, window, grid)
		if one == both {
			t.Errorf("a shard measured the whole world: %+v", one)
		}
	})
}

func TestFlattenCoordinates(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"a polygon ring", `[[[1,2],[3,4],[5,6]]]`, 3},
		{"a multipolygon", `[[[[1,2],[3,4]]],[[[5,6]]]]`, 3},
		{"a line", `[[1,2],[3,4]]`, 2},
		{"nothing", `null`, 0},
		{"not coordinates", `{"a":1}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flattenCoordinates(json.RawMessage(tt.raw)); len(got) != tt.want {
				t.Errorf("flattened %d positions, want %d", len(got), tt.want)
			}
		})
	}
}

func TestBoxes(t *testing.T) {
	a := tiles.Box{X: 0, Y: 0, Width: 10, Height: 10}
	b := tiles.Box{X: 5, Y: 5, Width: 10, Height: 10}
	apart := tiles.Box{X: 100, Y: 100, Width: 1, Height: 1}

	if got := unionBox(a, b); got != (tiles.Box{X: 0, Y: 0, Width: 15, Height: 15}) {
		t.Errorf("union is %+v", got)
	}
	if got := intersectBox(a, b); got != (tiles.Box{X: 5, Y: 5, Width: 5, Height: 5}) {
		t.Errorf("intersection is %+v", got)
	}
	if got := intersectBox(a, apart); got != apart {
		t.Errorf("rectangles that do not meet give %+v, want the second", got)
	}
}

// TestOptionalID pins the one translation between the document's spelling of
// absence and the wire's.
func TestOptionalID(t *testing.T) {
	if optionalID(0) != nil {
		t.Error("zero should be absent on the wire")
	}
	if got := optionalID(7); got == nil || *got != 7 {
		t.Errorf("optionalID(7) = %v", got)
	}
}

func TestIsPicture(t *testing.T) {
	tests := []struct {
		file string
		want bool
	}{
		{"a.png", true},
		{"a.svg", false},
		{"", false},
		{"png", false},
	}
	for _, tt := range tests {
		if got := isPicture(tt.file); got != tt.want {
			t.Errorf("isPicture(%q) = %t", tt.file, got)
		}
	}
}
