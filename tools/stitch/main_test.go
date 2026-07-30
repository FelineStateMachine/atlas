package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTilePathUsesXThenY(t *testing.T) {
	mapDir := t.TempDir()
	want := filepath.Join(mapDir, "tiles", "set-42", "13", "4076", "4064.jpg")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := tilePath(mapDir, tileRecord{TileSetID: 42, X: 4076, Y: 4064})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("tilePath() = %q, want %q", got, want)
	}
}

func TestTileSetPath(t *testing.T) {
	const rawURL = "https://tiles.mapgenie.io/games/skyrim/skyrim/default-v1/13/4076/4064.jpg"
	if got, want := tileSetPath(rawURL), "skyrim/skyrim/default-v1"; got != want {
		t.Fatalf("tileSetPath() = %q, want %q", got, want)
	}
}

func TestRequiredTileCount(t *testing.T) {
	if got, want := requiredTileCount(rawTileSet{}), 1024; got != want {
		t.Fatalf("unbounded requiredTileCount() = %d, want %d", got, want)
	}
	set := rawTileSet{Bounds: map[string]rawTileSetBound{
		"13": {X: rawRange{Min: 4064, Max: 4078}, Y: rawRange{Min: 4064, Max: 4075}},
	}}
	if got, want := requiredTileCount(set), 180; got != want {
		t.Fatalf("bounded requiredTileCount() = %d, want %d", got, want)
	}
}
