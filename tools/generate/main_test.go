package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvedCategoryColor(t *testing.T) {
	tests := []struct {
		name     string
		group    rawGroup
		category rawCategory
		want     string
	}{
		{
			name:     "category overrides group",
			group:    rawGroup{Color: "38344C"},
			category: rawCategory{Color: "aaacae"},
			want:     "#AAACAE",
		},
		{
			name:     "group fallback",
			group:    rawGroup{Color: "38344C"},
			category: rawCategory{},
			want:     "#38344C",
		},
		{
			name:     "invalid color omitted",
			group:    rawGroup{Color: "not-a-color"},
			category: rawCategory{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvedCategoryColor(tt.group, tt.category); got != tt.want {
				t.Fatalf("resolvedCategoryColor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttachGameIcons(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	const svg = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M0 0h24v24H0z"/></svg>`
	if err := os.WriteFile(filepath.Join(source, "icons", "pokemon_center.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}

	game := catalogVolume{
		Slug: "pokemon-red-blue-yellow",
		Worlds: []catalogWorld{{
			Collections: []worldCollection{
				{Kind: kindPoint, Icon: "pokemon_center"},
				{Kind: kindPoint, Icon: "missing"},
			},
		}},
	}
	if err := attachVolumeIcons(source, &game); err != nil {
		t.Fatal(err)
	}

	collections := game.Worlds[0].Collections
	if got, want := collections[0].IconAsset, "pokemon_center.svg"; got != want {
		t.Fatalf("icon asset = %q, want %q", got, want)
	}
	if collections[1].IconAsset != "" {
		t.Fatalf("missing icon asset = %q, want empty", collections[1].IconAsset)
	}
	if got := string(game.Icons["pokemon_center.svg"]); got != svg {
		t.Fatalf("carried SVG = %q, want %q", got, svg)
	}
}

func TestBuildGameSkipsMapWithoutSnapshotIndex(t *testing.T) {
	archiveRoot := t.TempDir()
	gamePath := filepath.Join(archiveRoot, "games", "pokemon-red-blue-yellow-246")
	if err := os.MkdirAll(filepath.Join(gamePath, "maps", "red-blue-847"), 0o755); err != nil {
		t.Fatal(err)
	}

	game, err := buildVolume(
		archiveRoot,
		nil,
		nil,
		archiveGame{
			Directory: "games/pokemon-red-blue-yellow-246",
			ID:        246,
			Title:     "Pokémon Red/Blue/Yellow",
		},
		tileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Worlds) != 0 {
		t.Fatalf("maps = %d, want 0", len(game.Worlds))
	}
}

func TestSortGameMapsPrefersPrimaryMap(t *testing.T) {
	maps := []catalogWorld{
		{Title: "Big MT", Slug: "big-mt"},
		{Title: "Zion Canyon", Slug: "zion-canyon"},
		{Title: "Mojave Wasteland", Slug: "mojave-wasteland"},
		{Title: "Sierra Madre", Slug: "sierra-madre"},
	}

	sortVolumeWorlds("fallout-new-vegas", maps)

	want := []string{"mojave-wasteland", "big-mt", "sierra-madre", "zion-canyon"}
	for index, slug := range want {
		if maps[index].Slug != slug {
			t.Fatalf("map %d = %q, want %q", index, maps[index].Slug, slug)
		}
	}
}

// The packed payload is the only thing the frontend reads a location from, so
// a field left out of it is a field the map does not have. Shard went missing
// this way once, and every layer's pins drew over every other.
func TestBuildPayloadPacksEveryColumn(t *testing.T) {
	region := int64(77)
	m := catalogWorld{Collections: []worldCollection{
		{Kind: kindPoint, Features: []feature{
			{ID: 11, Title: "Sky Mine", Lat: 1.5, Lng: -2.5, Shard: 1783},
		}},
		{Kind: kindPoint, Features: []feature{
			{ID: 12, Title: "Deep Well", Lat: -3, Lng: 4, Member: &region, Shard: 1785},
		}},
	}}

	_, packed, _ := buildPayload(m)

	if magic := string(packed[:8]); magic != "ATLASLOC" {
		t.Fatalf("magic = %q, want %q", magic, "ATLASLOC")
	}
	if version := binary.LittleEndian.Uint16(packed[8:]); version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}
	count := int(binary.LittleEndian.Uint32(packed[10:]))
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	column := func(index int) []int32 {
		at := 16 + index*count*4
		values := make([]int32, count)
		for i := range values {
			values[i] = int32(binary.LittleEndian.Uint32(packed[at+i*4:]))
		}
		return values
	}

	if ids := column(0); ids[0] != 11 || ids[1] != 12 {
		t.Fatalf("ids = %v, want [11 12]", ids)
	}
	if members := column(3); members[0] != 0 || members[1] != 77 {
		t.Fatalf("members = %v, want [0 77]", members)
	}
	if shards := column(4); shards[0] != 1783 || shards[1] != 1785 {
		t.Fatalf("shards = %v, want [1783 1785]", shards)
	}

	titles := string(packed[len(packed)-len("Sky MineDeep Well"):])
	if titles != "Sky MineDeep Well" {
		t.Fatalf("titles = %q, want %q", titles, "Sky MineDeep Well")
	}
}

// A piece is cropped from the sheet by growing its ground into the space around
// it and then easing it away from its neighbours. Easing may only give back
// what the growth added: it once cut into the ground itself, which took the
// bottom border off the Camp McCarran panel -- the crop stopped inside the
// frame the panel is drawn with.
func TestGrowIntoGapsKeepsEveryPieceAroundItsGround(t *testing.T) {
	// Two pieces set corner to corner, sharing a span on neither axis, so both
	// grow into the same diagonal space and have to be eased apart again.
	ground := [][4]float64{
		{0, 0, 100, 100},
		{110, 110, 210, 210},
	}

	grown := growIntoGaps(ground, 8192)

	for index, box := range ground {
		got := grown[index]
		if got[0] > box[0] || got[1] > box[1] || got[2] < box[2] || got[3] < box[3] {
			t.Errorf("piece %d cropped to %v, which cuts into its ground %v", index, got, box)
		}
	}
}

// The window a raster is cut from is not the ground the map is about: a sheet
// drawn inside a printed border or a title panel spends most of its thirty-two
// named cells on nothing. What the grid divides is measured from the map's own
// contents instead, and a location drawn off the sheet entirely does not get to
// stretch that measurement back over everything it left out.
func TestMarkSurfacesMeasuresContentsNotTheWindow(t *testing.T) {
	grid := tileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192}
	at := func(x, y float64) feature {
		return feature{
			Lat: unprojectLatitude(y, grid),
			Lng: unprojectLongitude(x, grid),
		}
	}
	window := contentBounds{X: 2000, Y: 2000, Width: 3000, Height: 3000}
	m := catalogWorld{
		Lenses: []lens{{Bounds: &window}},
		Collections: []worldCollection{{
			Kind: kindPoint,
			Features: []feature{
				at(3000, 3000),
				at(4000, 4000),
				// Off the sheet, and so no part of what the map covers.
				at(6000, 6000),
			},
		}},
	}

	markSurfaces(&m, grid)

	surface := m.Lenses[0].Surface
	if surface == nil {
		t.Fatal("no surface measured")
	}
	near := func(got, want int) bool { return got >= want-2 && got <= want+2 }
	// The margin is the larger of 32 pixels and a fiftieth of the longer side.
	if !near(surface.X, 2968) || !near(surface.Y, 2968) ||
		!near(surface.Width, 1064) || !near(surface.Height, 1064) {
		t.Fatalf("surface = %+v, want about {2968 2968 1064 1064}", *surface)
	}
	if surface.X < window.X || surface.Y < window.Y ||
		surface.X+surface.Width > window.X+window.Width ||
		surface.Y+surface.Height > window.Y+window.Height {
		t.Errorf("surface %+v reaches outside the window %+v", *surface, window)
	}
}

// A piece of a sheet already knows the ground its regions cover, and that
// ground is kept: the pieces are cropped to it, and their locations need not
// reach every corner of what the piece is drawn to show.
func TestMarkSurfacesKeepsGroundAlreadyMeasured(t *testing.T) {
	grid := tileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192}
	window := contentBounds{X: 2000, Y: 2000, Width: 3000, Height: 3000}
	ground := contentBounds{X: 2500, Y: 2500, Width: 2000, Height: 2000}
	m := catalogWorld{
		Lenses: []lens{{Bounds: &window, Surface: &ground}},
		Collections: []worldCollection{{
			Kind: kindPoint,
			Features: []feature{{
				Lat: unprojectLatitude(3000, grid),
				Lng: unprojectLongitude(3000, grid),
			}},
		}},
	}

	markSurfaces(&m, grid)

	surface := m.Lenses[0].Surface
	if surface.X > ground.X || surface.Y > ground.Y ||
		surface.X+surface.Width < ground.X+ground.Width ||
		surface.Y+surface.Height < ground.Y+ground.Height {
		t.Fatalf("surface = %+v, which drops part of the ground %+v", *surface, ground)
	}
}
