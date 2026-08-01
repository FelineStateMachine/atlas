package main

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

// diffFixture builds two builds of one game the way a policy change leaves
// them: the newer one loses a pin, keeps one matched pair, moves one, and
// makes one it did not have.
func diffFixture(t *testing.T) (*build, *build, map[string]map[int64]string, map[string]map[int64]string) {
	t.Helper()
	dir := t.TempDir()
	install(t, dir, "hollowmere-a.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-01-01T00:00:00Z",
		Worlds: []bundletest.WorldSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{
				{Title: "Gate"}, {Title: "Well", Silent: true}, {Title: "Shrine"},
			},
			Merged: []map[string]any{{
				"source": "ign-wiki", "donorPins": 4,
				"matched": []map[string]any{
					{"d": 11, "w": 1, "px": 10},
					{"d": 12, "w": 2, "px": 20},
				},
				"alignment": "affine, median 20px",
			}},
		}},
	})
	install(t, dir, "hollowmere-b.atlas", bundletest.Spec{
		Slug: "hollowmere", Title: "Hollowmere", CreatedAt: "2026-02-01T00:00:00Z",
		Worlds: []bundletest.WorldSpec{{
			Slug: "overworld",
			Pins: []bundletest.Pin{
				{Title: "Gate", Description: "The way in."}, {Title: "Well"},
			},
			Merged: []map[string]any{{
				"source": "ign-wiki", "donorPins": 4,
				"matched": []map[string]any{
					{"d": 11, "w": 1, "px": 10},
					{"d": 12, "w": 3, "px": 30},
					{"d": 13, "w": 2, "px": 15},
				},
				"alignment": "affine, median 15px",
			}},
		}},
	})

	games, _, err := (&library{dir: dir}).volumes()
	if err != nil {
		t.Fatal(err)
	}
	held := games[0]
	older, newer := held.Build("hollowmere-a.atlas"), held.Build("hollowmere-b.atlas")
	pinsA, err := loadPins(older.Path, older.MapSlugs)
	if err != nil {
		t.Fatal(err)
	}
	pinsB, err := loadPins(newer.Path, newer.MapSlugs)
	if err != nil {
		t.Fatal(err)
	}
	return older, newer, pinsA, pinsB
}

func TestDiffBuilds(t *testing.T) {
	older, newer, pinsA, pinsB := diffFixture(t)
	d := diffBuilds(older, newer, pinsA, pinsB)

	// The newer build packs two pins where the older packed three: id 3 is
	// gone and nothing arrived, since ids 1 and 2 stand in both.
	if len(d.Added) != 0 {
		t.Errorf("added %v, want none", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != 3 || d.Removed[0].Title != "Shrine" ||
		d.Removed[0].Map != "overworld" {
		t.Errorf("removed %v, want overworld pin 3 Shrine", d.Removed)
	}

	// Pair (11,1) stands in both ledgers. Pair (12,2) became (12,3), which
	// must read as one lost and one gained -- and (13,2) is a fresh gain.
	if len(d.Pairs) != 1 {
		t.Fatalf("pair lines %v, want one", d.Pairs)
	}
	line := d.Pairs[0]
	if line.Map != "overworld" || line.Source != "ign-wiki" {
		t.Errorf("pair line identity %+v", line)
	}
	if line.Kept != 1 || line.Gained != 2 || line.Lost != 1 {
		t.Errorf("stability kept %d gained %d lost %d, want 1, 2, 1", line.Kept, line.Gained, line.Lost)
	}

	// Axis deltas read in A-to-B direction.
	byName := make(map[string]axisRow)
	for _, row := range d.Axes {
		byName[row.Name] = row
	}
	if row := byName["pins"]; row.Delta != "-1" || row.Sign != -1 {
		t.Errorf("pins delta %q sign %d", row.Delta, row.Sign)
	}
	// Described goes 2 of 3 to 2 of 2: count flat, share up 34 points.
	if row := byName["described"]; row.Delta != "±0" || row.Sign != 0 {
		t.Errorf("described delta %q sign %d", row.Delta, row.Sign)
	}
	if row := byName["described share"]; row.Delta != "+34 pt" || row.Sign != 1 {
		t.Errorf("described share delta %q sign %d", row.Delta, row.Sign)
	}
	if row := byName["icon coverage"]; row.A != "100%" || row.B != "100%" || row.Sign != 0 {
		t.Errorf("icon coverage row %+v", row)
	}
	if row := byName["unique raster"]; !strings.HasSuffix(row.Delta, "MB") {
		t.Errorf("raster delta %q carries no unit", row.Delta)
	}
}

func TestDiffPage(t *testing.T) {
	server, _ := testServer(t)
	ts := newTestSite(t, server)

	status, body := get(t, ts, "/volume/hollowmere/diff?a=hollowmere-jan.atlas&b=hollowmere-feb.atlas")
	if status != 200 {
		t.Fatalf("diff page answered %d", status)
	}
	for _, want := range []string{"what moved", "Matched-pair stability", "described share", "ign-wiki"} {
		if !strings.Contains(body, want) {
			t.Errorf("diff page misses %q", want)
		}
	}
	if status, _ := get(t, ts, "/volume/hollowmere/diff?a=hollowmere-jan.atlas&b=nowhere.atlas"); status != 400 {
		t.Errorf("diff with a missing build answered %d", status)
	}
}
