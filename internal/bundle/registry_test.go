package bundle_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

func decodeCatalog(t *testing.T, snapshot *bundle.Snapshot) []map[string]any {
	t.Helper()
	var catalog struct {
		Games []map[string]any `json:"games"`
	}
	if err := json.Unmarshal(snapshot.Catalog, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog.Games
}

func TestRegistryAnswersEmptyForAMissingDirectory(t *testing.T) {
	registry := bundle.NewRegistry(filepath.Join(t.TempDir(), "never-created"))
	if err := registry.Rescan(); err != nil {
		t.Fatalf("a missing directory is an error: %v", err)
	}
	if games := decodeCatalog(t, registry.Snapshot()); len(games) != 0 {
		t.Fatalf("games = %v, want none", games)
	}
}

func TestRegistryComposesTheCatalogSortedByTitle(t *testing.T) {
	dir := t.TempDir()
	bundletest.Build(t, dir, bundletest.Spec{Slug: "zebra-quest", Title: "Zebra Quest"})
	bundletest.Build(t, dir, bundletest.Spec{Slug: "aardvark-saga", Title: "Aardvark Saga"})

	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	games := decodeCatalog(t, registry.Snapshot())
	if len(games) != 2 || games[0]["title"] != "Aardvark Saga" || games[1]["title"] != "Zebra Quest" {
		t.Fatalf("games = %v, want Aardvark Saga then Zebra Quest", games)
	}

	stamp, _ := games[0]["stamp"].(string)
	base, _ := games[0]["base"].(string)
	want := bundle.BasePath + "/aardvark-saga/" + bundle.ShortStamp(stamp)
	if base != want {
		t.Errorf("base = %q, want %q", base, want)
	}
	if _, ok := games[0]["tileGrid"].(map[string]any); !ok {
		t.Error("a game carries no tile grid of its own")
	}
}

func TestRegistryLetsTheNewestBundleOfAGameWin(t *testing.T) {
	dir := t.TempDir()
	bundletest.Build(t, dir, bundletest.Spec{Slug: "game", Title: "Old Title", CreatedAt: "2026-01-01T00:00:00Z"})
	// A second file for the same game, as a versioned download would land.
	sideBySide := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "game", Title: "New Title", CreatedAt: "2026-06-01T00:00:00Z",
	})
	moved := filepath.Join(dir, "game-v2.atlas")
	if err := os.Rename(sideBySide, moved); err != nil {
		t.Fatal(err)
	}

	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot()
	if len(snapshot.Games) != 1 {
		t.Fatalf("games = %d, want the two files folded into one", len(snapshot.Games))
	}
	if won := snapshot.Games["game"].Manifest.Game.Title; won != "New Title" {
		t.Errorf("the winning bundle is %q, want the newer one", won)
	}
}

func TestRegistrySurvivesABundleThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	bundletest.Build(t, dir, bundletest.Spec{Slug: "sound", Title: "Sound"})
	if err := os.WriteFile(filepath.Join(dir, "broken.atlas"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatalf("one broken file fails the scan: %v", err)
	}
	if games := decodeCatalog(t, registry.Snapshot()); len(games) != 1 {
		t.Fatalf("games = %v, want the sound one alone", games)
	}
}

func TestRescanSeesArrivalsUpdatesAndDepartures(t *testing.T) {
	dir := t.TempDir()
	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}

	// Arrival.
	path := bundletest.Build(t, dir, bundletest.Spec{Slug: "game", Title: "First", CreatedAt: "2026-01-01T00:00:00Z"})
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	before := registry.Snapshot()
	if before.Games["game"].Manifest.Game.Title != "First" {
		t.Fatal("an arriving bundle is not seen")
	}

	// An untouched file keeps its open bundle across rescans.
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if registry.Snapshot().Games["game"] != before.Games["game"] {
		t.Error("an untouched file was reopened")
	}

	// Update in place, as a downloaded replacement would land.
	replacement := bundletest.Build(t, t.TempDir(), bundletest.Spec{
		Slug: "game", Title: "Second", CreatedAt: "2026-06-01T00:00:00Z",
	})
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	after := registry.Snapshot()
	if after.Games["game"].Manifest.Game.Title != "Second" {
		t.Fatal("a replaced file still serves its old content")
	}
	// The old snapshot still reads until its grace ends, so a request that
	// loaded it before the swap finishes cleanly.
	if _, err := before.Games["game"].ReadEntry("maps/overworld.json"); err != nil {
		t.Errorf("the retired bundle is already unreadable: %v", err)
	}

	// Departure.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if games := decodeCatalog(t, registry.Snapshot()); len(games) != 0 {
		t.Fatalf("games = %v, want none after removal", games)
	}
}

func TestRescanTellsItsListenerWhatChanged(t *testing.T) {
	dir := t.TempDir()
	registry := bundle.NewRegistry(dir)
	var told [][]string
	registry.SetOnChange(func(changed []string) { told = append(told, changed) })

	// Arrival.
	path := bundletest.Build(t, dir, bundletest.Spec{Slug: "game", CreatedAt: "2026-01-01T00:00:00Z"})
	bundletest.Build(t, dir, bundletest.Spec{Slug: "other", CreatedAt: "2026-01-01T00:00:00Z"})
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || !slices.Equal(told[0], []string{"game", "other"}) {
		t.Fatalf("arrivals told as %v, want [[game other]]", told)
	}

	// Nothing moved: nothing said.
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 {
		t.Fatalf("an unchanged rescan spoke: %v", told)
	}

	// A replacement and a departure, together.
	replacement := bundletest.Build(t, t.TempDir(), bundletest.Spec{Slug: "game", CreatedAt: "2026-06-01T00:00:00Z"})
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "other.atlas")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if len(told) != 2 || !slices.Equal(told[1], []string{"game", "other"}) {
		t.Fatalf("changes told as %v, want the replaced and the departed", told)
	}
}

func TestWatchSeesADroppedBundleOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	if err := registry.Watch(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Assembled elsewhere and renamed in, the way the bundler and the
	// importer both deliver.
	staged := bundletest.Build(t, t.TempDir(), bundletest.Spec{Slug: "game"})
	if err := os.Rename(staged, filepath.Join(dir, "game.atlas")); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		if _, ok := registry.Snapshot().Games["game"]; ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the watcher never noticed the bundle")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func TestInstallCopiesTheSoundAndRefusesTheBroken(t *testing.T) {
	source := t.TempDir()
	sound := bundletest.Build(t, source, bundletest.Spec{Slug: "game", Title: "Game"})
	broken := filepath.Join(source, "broken.atlas")
	if err := os.WriteFile(broken, []byte("not a bundle"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := bundle.NewRegistry(t.TempDir())
	installed, refused := registry.Install([]string{sound, broken})
	if !slices.Equal(installed, []string{"game"}) {
		t.Fatalf("installed = %v, want [game]", installed)
	}
	if len(refused) != 1 {
		t.Fatalf("refused = %v, want the one broken file", refused)
	}
	// The copy lands under the game's canonical name, whatever the source
	// file was called, and the rescan has already made it servable.
	if _, err := os.Stat(filepath.Join(registry.Dir(), "game.atlas")); err != nil {
		t.Error(err)
	}
	if _, ok := registry.Snapshot().Games["game"]; !ok {
		t.Error("the installed game is not being served")
	}
}
