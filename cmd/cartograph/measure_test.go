package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

// fixtureMerged is a merge ledger the shape tools/generate writes, handed to
// bundletest verbatim so measurement reads it the way it reads a real build.
func fixtureMerged() []map[string]any {
	return []map[string]any{{
		"source":    "ign-wiki",
		"donorPins": 5,
		"matched": []map[string]any{
			{"d": 11, "w": 1, "px": 12},
			{"d": 12, "w": 2, "px": 40, "e": true},
		},
		"added":     1,
		"adopted":   []map[string]any{{"d": 13, "into": "markers"}},
		"held":      []map[string]any{{"d": 14, "t": "Old Well", "why": "name 200px away"}},
		"rejected":  []map[string]any{{"d": 15, "t": "Off World", "why": "outside the world"}},
		"alignment": "affine over 9 anchors, median 26px",
	}}
}

func TestMeasureBundle(t *testing.T) {
	path := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug:      "hollowmere",
		Title:     "Hollowmere",
		CreatedAt: "2026-03-01T00:00:00Z",
		Maps: []bundletest.MapSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{
				{Title: "Gate", Description: "The way in."},
				{Title: "Well", Silent: true},
				{Title: "Shrine"},
			},
			Merged: fixtureMerged(),
		}},
	})

	measured, err := measureBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	if measured.GameSlug != "hollowmere" || measured.GameTitle != "Hollowmere" {
		t.Fatalf("identity read as %s (%s)", measured.GameTitle, measured.GameSlug)
	}

	// Annotation: three pins, two with words. The medians sort "The way in."
	// (11 chars) under "About Shrine" (12), so the upper middle is 12.
	if measured.Pins != 3 || measured.Described != 2 {
		t.Errorf("annotation: %d pins, %d described; want 3 and 2", measured.Pins, measured.Described)
	}
	if measured.MedianLength != 12 {
		t.Errorf("median description length %d, want 12", measured.MedianLength)
	}
	if got := measured.DescribedPct(); got != "66%" {
		t.Errorf("described share %s, want 66%%", got)
	}

	// Cartography: the fixture writes two distinct tiles, one at each level.
	if measured.TileCount != 2 || measured.Depth != 1 || measured.DepthTiles != 1 {
		t.Errorf("cartography: %d tiles, depth z%d holds %d; want 2, z1, 1",
			measured.TileCount, measured.Depth, measured.DepthTiles)
	}
	if measured.RasterBytes == 0 {
		t.Error("raster bytes read as zero")
	}

	// Structure and icons: one group of one iconed marker category, one layer.
	if measured.Categories != 1 || measured.Groups != 1 || measured.Variants != 1 {
		t.Errorf("structure: %d categories, %d groups, %d layers; want 1 each",
			measured.Categories, measured.Groups, measured.Variants)
	}
	if measured.IconsCarried != 1 || measured.IconsWanted != 1 {
		t.Errorf("icons: %d of %d; want 1 of 1", measured.IconsCarried, measured.IconsWanted)
	}

	// The merge account, kept whole rather than counted.
	if len(measured.Merges) != 1 {
		t.Fatalf("read %d merge accounts, want 1", len(measured.Merges))
	}
	account := measured.Merges[0]
	if account.Map != "overworld" || account.Source != "ign-wiki" || account.DonorPins != 5 {
		t.Errorf("account identity: map %s source %s donors %d", account.Map, account.Source, account.DonorPins)
	}
	if account.MatchedN() != 2 || account.MedianMatchPx() != 40 {
		t.Errorf("matched %d at median %dpx, want 2 at 40px", account.MatchedN(), account.MedianMatchPx())
	}
	if account.Added != 1 || account.AdoptedN() != 1 || account.RejectedN() != 1 {
		t.Errorf("added %d adopted %d rejected %d, want 1 each",
			account.Added, account.AdoptedN(), account.RejectedN())
	}
	if account.HeldN() != 1 || account.Held[0].Title != "Old Well" || account.Held[0].Reason != "name 200px away" {
		t.Errorf("held ledger read wrong: %+v", account.Held)
	}
	if got := measured.SourcesSeen(); len(got) != 1 || got[0] != "ign-wiki" {
		t.Errorf("sources seen %v, want [ign-wiki]", got)
	}
}

// install builds a fixture and moves it into the scanned directory under its
// own name, the way versioned builds of one game sit side by side.
func install(t *testing.T, dir string, name string, spec bundletest.Spec) string {
	t.Helper()
	built := bundletest.Build(t, t.TempDir(), spec)
	target := filepath.Join(dir, name)
	if err := os.Rename(built, target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestLibraryOrdersBuilds(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "hollowmere-jan.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-01-01T00:00:00Z",
	})
	install(t, dir, "hollowmere-feb.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-02-01T00:00:00Z",
	})
	// Same capture as February, newer policy: the revision must win the top.
	install(t, dir, "hollowmere-feb-r1.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-02-01T00:00:00Z", Revision: 1,
	})
	install(t, dir, "briar.atlas", bundletest.Spec{
		Slug: "briar", Title: "Briar", CreatedAt: "2026-01-15T00:00:00Z",
	})

	lib := &library{dir: dir}
	games, skipped, err := lib.games()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(games) != 2 {
		t.Fatalf("read %d games, want 2", len(games))
	}
	// Sorted by title: Briar before Hollowmere.
	if games[0].Slug != "briar" || games[1].Slug != "hollowmere" {
		t.Fatalf("order %s, %s", games[0].Slug, games[1].Slug)
	}
	builds := games[1].Builds
	if len(builds) != 3 {
		t.Fatalf("hollowmere has %d builds, want 3", len(builds))
	}
	if builds[0].File != "hollowmere-feb-r1.atlas" {
		t.Errorf("serving build is %s, want the revised February build", builds[0].File)
	}
	if builds[1].File != "hollowmere-feb.atlas" || builds[2].File != "hollowmere-jan.atlas" {
		t.Errorf("order below serving: %s, %s", builds[1].File, builds[2].File)
	}
	if games[1].Serving() != builds[0] {
		t.Error("Serving is not the first build")
	}
	if games[1].Build("hollowmere-jan.atlas") != builds[2] {
		t.Error("Build by file name missed")
	}
}

func TestLibrarySkipsBadBundles(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "briar.atlas", bundletest.Spec{Slug: "briar"})
	if err := os.WriteFile(filepath.Join(dir, "broken.atlas"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	games, skipped, err := (&library{dir: dir}).games()
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || len(skipped) != 1 {
		t.Fatalf("read %d games and %d skipped, want 1 and 1", len(games), len(skipped))
	}
}

func TestLoadPins(t *testing.T) {
	path := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "hollowmere",
		Maps: []bundletest.MapSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{{Title: "Gate"}, {Title: "Well"}},
		}},
	})
	pins, err := loadPins(path, []string{"overworld"})
	if err != nil {
		t.Fatal(err)
	}
	titles := pins["overworld"]
	if len(titles) != 2 || titles[1] != "Gate" || titles[2] != "Well" {
		t.Fatalf("pins read as %v", titles)
	}
}
