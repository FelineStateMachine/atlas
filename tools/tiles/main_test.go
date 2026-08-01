package main

import "testing"

func TestWorldTileRange(t *testing.T) {
	tests := []struct {
		zoom    int
		wantMin int
		wantMax int
	}{
		{zoom: 8, wantMin: 127, wantMax: 127},
		{zoom: 10, wantMin: 508, wantMax: 511},
		{zoom: 13, wantMin: 4064, wantMax: 4095},
		{zoom: 14, wantMin: 8128, wantMax: 8191},
	}
	for _, test := range tests {
		minimum, maximum := worldTileRange(test.zoom)
		if minimum != test.wantMin || maximum != test.wantMax {
			t.Errorf("worldTileRange(%d) = (%d, %d), want (%d, %d)",
				test.zoom, minimum, maximum, test.wantMin, test.wantMax)
		}
	}
}

func TestExpectedBoundsUsesConfiguredLayerBounds(t *testing.T) {
	set := rawTileSet{Bounds: map[string]rawTileSetBound{
		"13": {
			X: rawRange{Min: 4064, Max: 4078},
			Y: rawRange{Min: 4064, Max: 4075},
		},
	}}
	got := expectedBounds(set, 13)
	if got.X.Min != 4064 || got.X.Max != 4078 || got.Y.Min != 4064 || got.Y.Max != 4075 {
		t.Fatalf("expectedBounds() = %#v", got)
	}
}

// The two windows the archive holds. A layer that declares nothing is cut from
// the shared square at zoom 13, and lands on the base and world grid the viewer
// assumed before any of this was measured; GTA 5 declares its own, five levels
// shallower and anchored at the origin.
func TestFrameForMeasuresEachWindow(t *testing.T) {
	tests := []struct {
		name     string
		set      rawTileSet
		wantBase int
		wantGrid gridSpec
	}{
		{
			name:     "the shared window",
			set:      rawTileSet{MaxZoom: 15},
			wantBase: 8,
			wantGrid: gridSpec{SourceZoom: 13, FirstTile: 4064},
		},
		{
			name: "a window of its own",
			set: rawTileSet{MaxZoom: 7, Bounds: map[string]rawTileSetBound{
				"7": {X: rawRange{Min: 0, Max: 42}, Y: rawRange{Min: 0, Max: 42}},
			}},
			wantBase: 1,
			wantGrid: gridSpec{SourceZoom: 6, FirstTile: 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame, ok := frameFor(test.set)
			if !ok {
				t.Fatal("frameFor() found no window")
			}
			if frame.BaseZoom != test.wantBase {
				t.Errorf("BaseZoom = %d, want %d", frame.BaseZoom, test.wantBase)
			}
			if got := frame.grid(); got != test.wantGrid {
				t.Errorf("grid() = %#v, want %#v", got, test.wantGrid)
			}
		})
	}
}

// A window that is one tile on one axis and two on the other would place pins
// against an origin it does not have.
func TestFrameForRejectsAnUnsquareWindow(t *testing.T) {
	set := rawTileSet{MaxZoom: 4, Bounds: map[string]rawTileSetBound{
		"4": {X: rawRange{Min: 0, Max: 3}, Y: rawRange{Min: 8, Max: 11}},
	}}
	if _, ok := frameFor(set); ok {
		t.Fatal("frameFor() accepted a window whose axes rest on different tiles")
	}
}

func TestTileSetPath(t *testing.T) {
	const rawURL = "https://tiles.mapgenie.io/games/skyrim/skyrim/default-v1/13/4076/4064.jpg"
	if got, want := tileSetPath(rawURL, 13), "skyrim/skyrim/default-v1"; got != want {
		t.Fatalf("tileSetPath() = %q, want %q", got, want)
	}
}

func TestPixelArtUsesNearestNeighbor(t *testing.T) {
	if !isPixelArt("pokemon-red-blue-yellow") {
		t.Fatal("Pokémon maps must be classified as pixel art")
	}
	if isPixelArt("fallout-new-vegas") {
		t.Fatal("photographic maps must retain smooth interpolation")
	}
}
