package main

import (
	"os"
	"path/filepath"
	"testing"
)

func samplePlan() tilePlan {
	return tilePlan{
		AssetPath:       "tunic__world",
		SourcePath:      "tunic/world/default-v2",
		MaxFullZoom:     12,
		MaxSourceZoom:   13,
		Interpolate:     true,
		PreferredFormat: "jpg",
		Bounds:          &contentBounds{X: 16, Y: 32, Width: 512, Height: 512},
		Levels: map[int][]tileFile{
			12: {
				{Record: tileRecord{X: 1, Y: 1, ContentHash: "aaa"}, Format: "jpg"},
				{Record: tileRecord{X: 1, Y: 2, ContentHash: "bbb"}, Format: "jpg"},
			},
			13: {
				{Record: tileRecord{X: 2, Y: 2, ContentHash: "ccc"}, Format: "jpg"},
			},
		},
	}
}

// The stamp decides whether half a minute of derivation is skipped, so it has
// to say the same thing about the same capture and something different about
// every part of it that changes what would be written.
func TestPlanStampAnswersForEveryInput(t *testing.T) {
	base := planStamp(samplePlan())
	if base == planStamp(tilePlan{}) {
		t.Fatal("an empty plan stamps the same as a captured one")
	}

	// The order tiles happen to be listed in is not an input.
	shuffled := samplePlan()
	shuffled.Levels[12] = []tileFile{shuffled.Levels[12][1], shuffled.Levels[12][0]}
	if got := planStamp(shuffled); got != base {
		t.Error("the same tiles in another order stamp differently")
	}

	changes := map[string]func(*tilePlan){
		"a tile recaptured": func(plan *tilePlan) {
			plan.Levels[12][0].Record.ContentHash = "zzz"
		},
		"a tile added": func(plan *tilePlan) {
			plan.Levels[12] = append(plan.Levels[12],
				tileFile{Record: tileRecord{X: 9, Y: 9, ContentHash: "ddd"}, Format: "jpg"})
		},
		"a level captured deeper":  func(plan *tilePlan) { plan.MaxSourceZoom = 14 },
		"the complete level moved": func(plan *tilePlan) { plan.MaxFullZoom = 11 },
		"the content bounds moved": func(plan *tilePlan) { plan.Bounds.Width = 1024 },
		"the reduction changed":    func(plan *tilePlan) { plan.Interpolate = false },
		"the format changed":       func(plan *tilePlan) { plan.PreferredFormat = "png" },
		"the layer renamed":        func(plan *tilePlan) { plan.AssetPath = "tunic__other" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			plan := samplePlan()
			change(&plan)
			if planStamp(plan) == base {
				t.Errorf("%s stamps the same as before it", name)
			}
		})
	}
}

// The tool derives the pyramid, so a change to the tool has to invalidate one.
func TestToolStampIsRecorded(t *testing.T) {
	stamp := toolStamp()
	if stamp == "" || stamp == "unstamped" {
		t.Fatalf("tool stamp = %q, so nothing would notice the tool changing", stamp)
	}
	if planStamp(samplePlan()) == "" {
		t.Fatal("a plan stamped as nothing")
	}
}

func TestLinkTreeBringsEveryFileAcross(t *testing.T) {
	from, to := filepath.Join(t.TempDir(), "from"), filepath.Join(t.TempDir(), "to")
	for _, name := range []string{"3/0/0.jpg", "3/0/1.jpg", "4/1/0.png"} {
		path := filepath.Join(from, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tiles, err := linkTree(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if tiles != 3 {
		t.Errorf("linked %d tiles, want 3", tiles)
	}
	for _, name := range []string{"3/0/0.jpg", "3/0/1.jpg", "4/1/0.png"} {
		data, err := os.ReadFile(filepath.Join(to, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != name {
			t.Errorf("%s = %q, want %q", name, data, name)
		}
	}
}

// A run installs what it derived and takes out what the archive no longer
// offers, leaving everything it carried where it already was.
func TestInstallPyramidsKeepsCarriedAndPrunesRetired(t *testing.T) {
	temp, output := t.TempDir(), t.TempDir()
	write := func(root, name string, body string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(output, "carried/3/0/0.jpg", "kept")
	write(output, "derived/3/0/0.jpg", "old")
	write(output, "retired/3/0/0.jpg", "gone")
	write(temp, "derived/3/0/0.jpg", "new")

	out := manifest{Variants: []variantManifest{
		{AssetPath: "carried", SourcePath: "a"},
		{AssetPath: "derived", SourcePath: "b"},
	}}
	if err := installPyramids(temp, output, out, map[string]bool{"derived": true}); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"carried/3/0/0.jpg": "kept",
		"derived/3/0/0.jpg": "new",
	} {
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", name, data, want)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "retired")); !os.IsNotExist(err) {
		t.Error("a layer the archive no longer offers was left behind")
	}
	if _, err := os.Stat(filepath.Join(output, "index.json")); err != nil {
		t.Error("the index was not written")
	}
}
