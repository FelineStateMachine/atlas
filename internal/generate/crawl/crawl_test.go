package crawl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryName(t *testing.T) {
	tests := []struct {
		title string
		id    int64
		want  string
	}{
		{"Skyrim", 59, "skyrim-59"},
		{"L.A. Noire", 59, "l-a-noire-59"},
		{"Pokémon Red/Blue/Yellow", 59, "pokemon-red-blue-yellow-59"},
		{"Assassin's Creed (Resynced)", 59, "assassin-s-creed-resynced-59"},
		{"IGN Cyberpunk 2077", 4295212345, "ign-cyberpunk-2077-4295212345"},
	}
	for _, test := range tests {
		if got := DirectoryName(test.title, test.id); got != test.want {
			t.Errorf("%q/%d named %q, want %q", test.title, test.id, got, test.want)
		}
	}
}

// The property everything replayable downstream stands on: a re-crawl of
// unchanged bytes appends nothing, so capturedAt is first-seen and a rebuild
// computes the same stamp.
func TestAnUnchangedCaptureRecordsNothing(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	world := filepath.Join(root, "games", "thing-1", "maps", "world-2")
	body := []byte(`{"source":"test","hello":"world"}`)

	first, fresh, err := store.WriteCapture(world, Capture{Kind: "test", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Fatal("the first sight of a capture recorded nothing")
	}
	index := filepath.Join(world, "snapshots", "index.json")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	again, fresh, err := store.WriteCapture(world, Capture{Kind: "test", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Error("re-crawling unchanged bytes recorded a second capture")
	}
	if again != first {
		t.Errorf("the same bytes addressed as %s then %s", first, again)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the index moved:\n%s\n%s", before, after)
	}

	// Different bytes do record.
	if _, fresh, err = store.WriteCapture(world, Capture{
		Kind: "test", Body: []byte(`{"source":"test","hello":"elsewhere"}`),
	}); err != nil || !fresh {
		t.Errorf("changed bytes recorded nothing (fresh=%t, err=%v)", fresh, err)
	}
}

// A register entry is found by identity, merged in place, and never renamed --
// which is what keeps a directory named once named forever, and what lets a
// field this package has never heard of survive a run.
func TestRegisteringAVolumeTwiceKeepsItsDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	volume := Volume{ID: 7, Title: "Cyberpunk 2077", Source: "ign", DirectoryTitle: "IGN Cyberpunk 2077"}
	first, err := store.RegisterVolume(volume)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "ign-cyberpunk-2077-7" {
		t.Errorf("first sight opened %s", first)
	}

	// A stranger's field, and a run that would have named the directory
	// differently.
	register := filepath.Join(root, "archive.json")
	var held map[string]any
	data, err := os.ReadFile(register)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &held); err != nil {
		t.Fatal(err)
	}
	held["games"].([]any)[0].(map[string]any)["curatedBy"] = "somebody"
	data, _ = json.MarshalIndent(held, "", "  ")
	if err := os.WriteFile(register, data, 0o644); err != nil {
		t.Fatal(err)
	}

	volume.DirectoryTitle = "Something Else Entirely"
	again, err := store.RegisterVolume(volume)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Errorf("a second run renamed the directory to %s", again)
	}
	data, _ = os.ReadFile(register)
	if err := json.Unmarshal(data, &held); err != nil {
		t.Fatal(err)
	}
	if held["games"].([]any)[0].(map[string]any)["curatedBy"] != "somebody" {
		t.Error("a field the crawler has never heard of was dropped")
	}
}

// The tile index resumes a run: a numbered directory a previous run assigned is
// recovered rather than reassigned, or every tile already on disk is orphaned.
func TestTileIndexRecoversItsSetIDs(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	world := filepath.Join(root, "w")
	index, err := store.OpenTileIndex(world, 42)
	if err != nil {
		t.Fatal(err)
	}
	const scope = "cyberpunk-2077/night-city"
	id := index.SetID(scope)
	index.Put(TileRecord{
		URL:       "https://x/wikimaps/" + scope + "/3/1/2.jpg",
		Zoom:      3,
		X:         1,
		Y:         2,
		Status:    StatusCached,
		TileSetID: id,
	})
	if err := index.Close(world); err != nil {
		t.Fatal(err)
	}

	resumed, err := store.OpenTileIndex(world, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got := resumed.SetID(scope); got != id {
		t.Errorf("a resumed run numbered %s %d, the first run numbered it %d", scope, got, id)
	}
	if _, known := resumed.Held("https://x/wikimaps/" + scope + "/3/1/2.jpg"); !known {
		t.Error("a resumed run did not find the tile the first run cached")
	}
	if want := "42:" + itoa(id) + ":3:1:2"; resumed.CoverageKey(id, 3, 1, 2) != want {
		t.Errorf("coverage key %q, want %q", resumed.CoverageKey(id, 3, 1, 2), want)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
