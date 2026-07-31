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
