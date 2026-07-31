package main

import "testing"

func TestDominantHashIgnoresTinySamples(t *testing.T) {
	// The top of a pyramid is a handful of tiles showing the whole map. Calling
	// the only tile there "background" would stop a crawl before it started.
	single := map[[2]int]string{{0, 0}: "a"}
	if got := dominantHash(single); got != "" {
		t.Fatalf("dominantHash(single tile) = %q, want %q", got, "")
	}

	four := map[[2]int]string{{0, 0}: "a", {1, 0}: "a", {0, 1}: "a", {1, 1}: "b"}
	if got := dominantHash(four); got != "" {
		t.Fatalf("dominantHash(4 tiles) = %q, want %q", got, "")
	}
}

func TestDominantHashFindsBackgroundBelowMajority(t *testing.T) {
	// A map whose content fills most of its bounds still has empty corners, so
	// the background need not be a majority to be the background.
	hashes := make(map[[2]int]string, 32)
	for index := range 32 {
		key := [2]int{index, 0}
		if index < 10 {
			hashes[key] = "empty"
			continue
		}
		hashes[key] = string(rune('a' + index))
	}
	if got := dominantHash(hashes); got != "empty" {
		t.Fatalf("dominantHash = %q, want %q", got, "empty")
	}
}

func TestDominantHashRejectsUniformLevel(t *testing.T) {
	// Every tile identical gives nothing to contrast against; the question is
	// left to a deeper level rather than answered wrongly.
	hashes := make(map[[2]int]string, 20)
	for index := range 20 {
		hashes[[2]int{index, 0}] = "same"
	}
	if got := dominantHash(hashes); got != "" {
		t.Fatalf("dominantHash(uniform) = %q, want %q", got, "")
	}
}

func TestDominantHashRejectsDistinctTiles(t *testing.T) {
	hashes := make(map[[2]int]string, 40)
	for index := range 40 {
		hashes[[2]int{index, 0}] = string(rune('a' + index))
	}
	if got := dominantHash(hashes); got != "" {
		t.Fatalf("dominantHash(all distinct) = %q, want %q", got, "")
	}
}

func TestLiveTilesDropsBackground(t *testing.T) {
	hashes := map[[2]int]string{
		{0, 0}: "empty",
		{1, 0}: "content",
		{2, 0}: "empty",
	}
	live := liveTiles(hashes, "empty")
	if len(live) != 1 || !live[[2]int{1, 0}] {
		t.Fatalf("liveTiles = %v, want only {1,0}", live)
	}
	// With no background known, nothing is pruned.
	if all := liveTiles(hashes, ""); len(all) != 3 {
		t.Fatalf("liveTiles(no background) kept %d, want 3", len(all))
	}
}

func TestChildrenOfStaysInsideWindow(t *testing.T) {
	live := map[[2]int]bool{{2, 2}: true}
	window := tileWindow{minX: 4, minY: 4, maxX: 5, maxY: 4}
	children := childrenOf(live, window)
	if len(children) != 2 {
		t.Fatalf("childrenOf = %v, want the two children inside the window", children)
	}
	for _, child := range children {
		if !window.holds(child) {
			t.Errorf("child %v falls outside the window", child)
		}
	}
}

func TestChildrenOfPrunesEmptySpace(t *testing.T) {
	// One live parent out of a 4x4 level yields 4 requests at the next zoom
	// rather than the 64 a whole level would need.
	live := map[[2]int]bool{{0, 0}: true}
	window := tileWindow{minX: 0, minY: 0, maxX: 7, maxY: 7}
	if children := childrenOf(live, window); len(children) != 4 {
		t.Fatalf("childrenOf = %d tiles, want 4", len(children))
	}
}

func TestWindowTiles(t *testing.T) {
	window := tileWindow{minX: 3, minY: 7, maxX: 4, maxY: 8}
	tiles := window.tiles()
	if len(tiles) != 4 {
		t.Fatalf("tiles = %v, want 4", tiles)
	}
	if tiles[0] != [2]int{3, 7} || tiles[3] != [2]int{4, 8} {
		t.Fatalf("tiles = %v, want row-major from the minimum corner", tiles)
	}
}

func TestTileSetWindowMissingZoom(t *testing.T) {
	set := apiTileSet{Bounds: map[string]apiBound{
		"13": {X: apiRange{Min: 4064, Max: 4095}, Y: apiRange{Min: 4064, Max: 4095}},
	}}
	if _, ok := set.window(14); ok {
		t.Fatal("window(14) reported bounds the layer does not publish")
	}
	window, ok := set.window(13)
	if !ok || window.minX != 4064 || window.maxY != 4095 {
		t.Fatalf("window(13) = %+v, %v", window, ok)
	}
}

func TestDirectoryName(t *testing.T) {
	cases := map[string]string{
		"L.A. Noire":                  "l-a-noire-59",
		"Pokémon Red/Blue/Yellow":     "pokemon-red-blue-yellow-59",
		"Assassin's Creed (Resynced)": "assassin-s-creed-resynced-59",
		"Skyrim":                      "skyrim-59",
	}
	for title, want := range cases {
		if got := directoryName(title, 59); got != want {
			t.Errorf("directoryName(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestTileSetPathOf(t *testing.T) {
	url := "https://tiles.mapgenie.io/games/la-noire/los-angeles/default-v2/10/508/508.png"
	if got := tileSetPathOf(url, 10); got != "la-noire/los-angeles/default-v2" {
		t.Fatalf("tileSetPathOf = %q", got)
	}
	if got := tileSetPathOf(url, 13); got != "" {
		t.Fatalf("tileSetPathOf with the wrong zoom = %q, want empty", got)
	}
}

func TestTileIndexReusesExistingTileSetDirectory(t *testing.T) {
	// Re-crawling an archive the extension captured must not orphan its files.
	index := newTileIndex(203)
	index.put(tileRecord{
		URL:       "https://tiles.mapgenie.io/games/a/b/c-v1/10/1/2.png",
		Zoom:      10,
		TileSetID: 206,
		Status:    "cached",
	})
	index.setIDs["a/b/c-v1"] = 206
	index.usedIDs[206] = true

	if got := index.tileSetID("a/b/c-v1"); got != 206 {
		t.Fatalf("tileSetID(known layer) = %d, want 206", got)
	}
	fresh := index.tileSetID("x/y/z-v1")
	if fresh == 206 {
		t.Fatal("a new layer reused the existing directory")
	}
	if again := index.tileSetID("x/y/z-v1"); again != fresh {
		t.Fatalf("tileSetID is unstable: %d then %d", fresh, again)
	}
}

func TestUpsertByIDReplacesInPlace(t *testing.T) {
	existing := []any{
		map[string]any{"id": float64(1), "directory": "games/one", "extra": "kept"},
		map[string]any{"id": float64(2), "directory": "games/two"},
	}
	result := upsertByID(existing, map[string]any{"directory": "games/one-moved"}, 1)
	if len(result) != 2 {
		t.Fatalf("upsertByID changed the entry count to %d", len(result))
	}
	first := result[0].(map[string]any)
	if first["directory"] != "games/one-moved" {
		t.Errorf("directory = %v, want the replacement", first["directory"])
	}
	if first["extra"] != "kept" {
		t.Errorf("upsertByID dropped a field the archive already had")
	}

	appended := upsertByID(existing, map[string]any{"id": float64(3)}, 3)
	if len(appended) != 3 {
		t.Fatalf("upsertByID did not append an unknown id")
	}
}
