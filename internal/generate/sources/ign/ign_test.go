package ign

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The capture these tests read is testdata/wikimap.json: an invented wikimap,
// as small as one can be while still exercising the three judgements this
// reader owns -- the image-relative projection, the identities minted from
// stable names, and the flat type list arranged into a two-level legend. It is
// the whole input, so what the assertions below say is exactly what the
// translation of it is.

// openWikimap stages the testdata capture as the smallest archive holding one
// volume of one world, plus whatever artwork the caller dropped in.
func openWikimap(t *testing.T, icons map[string]string) *archive.Archive {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "wikimap.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	worldDir := filepath.Join(root, "games", "g-1", "maps", "world-1")
	mustWrite(t, filepath.Join(root, "archive.json"),
		`{"games":[{"directory":"games/g-1","id":1,"title":"Hollow Vale","source":"ign"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Hollow Vale","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"surface","title":"The Surface"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-02-01T00:00:00Z","contentHash":"h","kind":"ign-map","sourceId":1,"sourceUrl":"/x"}]`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "map", "h.json"), string(body))
	for key, svg := range icons {
		mustWrite(t, filepath.Join(root, "games", "g-1", "icons", key+".svg"), svg)
	}
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func translated(t *testing.T, icons map[string]string) doc.Document {
	t.Helper()
	store := openWikimap(t, icons)
	document, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// claim mints the number one stable name derives to, in a space of its own, so
// a test can say which identity a thing must carry without re-walking the
// reader's own claim order.
func claim(t *testing.T, name string) int64 {
	t.Helper()
	id, err := doc.NewIDSpace().Claim(name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestWikimapTranslatesExactly walks the whole invented capture into the
// document and asserts everything it becomes: identity, projection, legend
// order, and the frame handed to the deriver.
func TestWikimapTranslatesExactly(t *testing.T) {
	document := translated(t, nil)

	if document.Volume.Slug != "hollow-vale" || document.Volume.Title != "Hollow Vale" {
		t.Errorf("volume %s/%s", document.Volume.Slug, document.Volume.Title)
	}
	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("IGN numbers markers with opaque strings, so the id space must be %q, not %q",
			doc.IDSpaceDerived, document.Source.IDSpace)
	}
	if len(document.Worlds) != 1 {
		t.Fatalf("%d worlds from one map", len(document.Worlds))
	}
	world := document.Worlds[0]
	if world.Slug != "surface" || world.Title != "The Surface" {
		t.Errorf("world %s/%s", world.Slug, world.Title)
	}
	if world.ID != claim(t, "ign:map:hollow-vale/surface") {
		t.Errorf("world id %d is not the number its stable name mints", world.ID)
	}
	// The opening view runs through the same projection every marker does.
	if world.Center != imagePosition(0.5, -0.25) {
		t.Errorf("center %+v, want the image position of the captured initial view", world.Center)
	}

	// The lens: the publisher's abbreviation never travels, the tile set is the
	// canonical <object>/<map> path, and the frame is the pyramid the crawler
	// must fetch and the deriver must expect -- including the half-height top
	// level a landscape image leaves at zoom one.
	if len(world.Lenses) != 1 {
		t.Fatalf("%d lenses", len(world.Lenses))
	}
	lens := world.Lenses[0]
	if lens.Name != "IGN Wiki" || lens.TileSet != TileSetPath("hollow-vale", "surface") {
		t.Errorf("lens %s/%s", lens.Name, lens.TileSet)
	}
	if lens.Frame == nil {
		t.Fatal("a wikimap is not cut from the shared window, so its frame must be declared whole")
	}
	if lens.Frame.MinZoom != 0 || lens.Frame.MaxZoom != 1 || lens.Frame.Format != "png" {
		t.Errorf("frame %d..%d/%s; the encoding reads off the tile template, lowercased",
			lens.Frame.MinZoom, lens.Frame.MaxZoom, lens.Frame.Format)
	}
	if window := lens.Frame.Windows["0"]; window != (doc.TileWindow{MaxX: 0, MaxY: 0}) {
		t.Errorf("zoom 0 window %+v", window)
	}
	if window := lens.Frame.Windows["1"]; window != (doc.TileWindow{MaxX: 1, MaxY: 0}) {
		t.Errorf("zoom 1 window %+v; a half-height image fills only the top row", window)
	}

	// The legend: types sort by slug, gather under the heading of the parent
	// each names, and a type no marker uses is left out rather than dimming the
	// legend. Bench and fountain share "Street Life"; the parentless gate files
	// under the plain "Markers"; the ghost type has no markers and no row.
	if len(world.Collections) != 3 {
		t.Fatalf("%d collections, want benches, fountains and gates", len(world.Collections))
	}
	benches, fountains, gates := world.Collections[0], world.Collections[1], world.Collections[2]
	for _, check := range []struct {
		got                doc.Collection
		title, group, icon string
		features           int
	}{
		{benches, "Benches", "Street Life", "bench", 2},
		{fountains, "Fountains", "Street Life", "fountain", 1},
		{gates, "Gates", "Markers", "gate", 1},
	} {
		if check.got.Title != check.title || check.got.Group != check.group ||
			check.got.Icon != check.icon || len(check.got.Features) != check.features {
			t.Errorf("collection %q under %q with icon %q and %d features, want %q/%q/%q/%d",
				check.got.Title, check.got.Group, check.got.Icon, len(check.got.Features),
				check.title, check.group, check.icon, check.features)
		}
		if check.got.Kind != doc.KindPoint || !check.got.Visible {
			t.Errorf("collection %q is %s/%t; a wikimap publishes visible markers",
				check.got.Title, check.got.Kind, check.got.Visible)
		}
		if check.got.Attrs[semconv.KeyRenderAs] != semconv.RenderAsPin {
			t.Errorf("collection %q renders as %q", check.got.Title, check.got.Attrs[semconv.KeyRenderAs])
		}
		if check.got.ID != claim(t, "ign:type:hollow-vale/surface:"+check.icon) {
			t.Errorf("collection %q carries id %d, not the number its type slug mints",
				check.got.Title, check.got.ID)
		}
	}

	// Markers sort by id within their type, carry identities minted from IGN's
	// opaque strings, and land on the picture through the image projection: a
	// marker's longitude runs across the normalized image and its negative
	// latitude runs down it, both scaled by the world square's edge.
	north := benches.Features[0]
	if north.Title != "North Bench" || benches.Features[1].Title != "South Bench" {
		t.Errorf("benches read %q then %q; markers sort by id", north.Title, benches.Features[1].Title)
	}
	if north.ID != claim(t, "ign:marker:m-b1") {
		t.Errorf("marker id %d is not the number its stable name mints", north.ID)
	}
	for _, check := range []struct {
		got  doc.Feature
		x, y float64
	}{
		{north, 0.25 * doc.SyntheticWorldSize, 0.25 * doc.SyntheticWorldSize},
		{benches.Features[1], 0.5 * doc.SyntheticWorldSize, 0.75 * doc.SyntheticWorldSize},
		{fountains.Features[0], 0.125 * doc.SyntheticWorldSize, 0.0625 * doc.SyntheticWorldSize},
		{gates.Features[0], 0.875 * doc.SyntheticWorldSize, 0.5 * doc.SyntheticWorldSize},
	} {
		want := doc.SyntheticPosition(check.x, check.y)
		if check.got.At == nil || *check.got.At != want {
			t.Errorf("%q stands at %+v, want the synthetic position of pixel %v,%v",
				check.got.Title, check.got.At, check.x, check.y)
		}
	}

	// The same archived bytes give the same document, identities included.
	again := translated(t, nil)
	if again.Worlds[0].ID != world.ID || again.Worlds[0].Collections[0].Features[0].ID != north.ID {
		t.Error("two translations of one capture minted different identities")
	}
}

// TestArchivedArtworkAttachesByTypeKey: the archive holds a sprite per marker
// type, keyed by the type slug, and the document carries the ones its
// collections name so composition never reaches back into the archive.
func TestArchivedArtworkAttachesByTypeKey(t *testing.T) {
	document := translated(t, map[string]string{"bench": "<svg/>"})
	if len(document.Icons) != 1 {
		t.Fatalf("the document carries %d icons; one type has archived artwork", len(document.Icons))
	}
	icon := document.Icons[0]
	if icon.Key != "bench" || icon.File != "bench.svg" || string(icon.Data) != "<svg/>" {
		t.Errorf("artwork travelled as %s/%s %q", icon.Key, icon.File, icon.Data)
	}
}

// TestTranslateRefusesWhatItCannotStandFor states the reader's preconditions:
// a capture of another source's kind, a map with no size, a type declared
// twice, and a marker of a type nothing declares are refusals, because the
// alternative to each is a document that quietly lies.
func TestTranslateRefusesWhatItCannotStandFor(t *testing.T) {
	tests := []struct {
		name  string
		spoil func(*capture)
		says  string
	}{
		{"another source's bytes", func(c *capture) { c.Source = "piggyback" }, "not \"ign-wikimaps\""},
		{"a map with no size", func(c *capture) { c.Map.Width = 0 }, "no size"},
		{"a type declared twice", func(c *capture) { c.Types = append(c.Types, kind{TypeSlug: "bench"}) }, "declared twice"},
		{"a marker of an undeclared type", func(c *capture) {
			c.Markers = append(c.Markers, marker{ID: "m-x", TypeSlug: "mystery", MarkerName: "X"})
		}, "which nothing declares"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := capture{
				Source: source, ObjectSlug: "hollow-vale", MapSlug: "surface",
				Map:     sheet{Width: 1, Height: 1, MaxZoom: 1},
				Types:   []kind{{TypeSlug: "bench", TypeName: "Benches"}},
				Markers: []marker{{ID: "m-b1", TypeSlug: "bench", MarkerName: "One"}},
			}
			tt.spoil(&raw)
			ids := doc.NewIDSpace()
			raw.normalize()
			// The shape gates live in translateWorld and the legend gates in
			// collectionsOf; both are exercised through the reader's own checks.
			var err error
			switch {
			case raw.Source != source:
				err = errFrom(t, raw)
			case raw.Map.Width <= 0:
				err = errFrom(t, raw)
			default:
				_, err = collectionsOf(&raw, ids, "hollow-vale/surface")
			}
			if err == nil || !strings.Contains(err.Error(), tt.says) {
				t.Fatalf("the capture was read anyway: %v", err)
			}
		})
	}
}

// errFrom runs one spoiled capture through the whole reader and reports what it
// refused with.
func errFrom(t *testing.T, raw capture) error {
	t.Helper()
	root := t.TempDir()
	body, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(root, "games", "g-1", "maps", "world-1")
	mustWrite(t, filepath.Join(root, "archive.json"),
		`{"games":[{"directory":"games/g-1","id":1,"title":"Hollow Vale","source":"ign"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Hollow Vale","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"surface","title":"The Surface"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-02-01T00:00:00Z","contentHash":"h","kind":"ign-map","sourceId":1,"sourceUrl":"/x"}]`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "map", "h.json"), string(body))
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	return err
}
