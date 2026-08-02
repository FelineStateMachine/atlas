package compose

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// splitGrid is the shared corpus window, so a test's world pixels are the ones
// every other part of the lane measures in.
func splitGrid() surfaceGrid {
	return surfaceGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192}
}

// squareAt outlines a box of world pixels as an area feature's geometry, so a
// test can say where a piece of a sheet is drawn without spelling degrees.
func squareAt(x, y, size float64, grid surfaceGrid) []doc.Geometry {
	corners := [][2]float64{
		{x, y}, {x + size, y}, {x + size, y + size}, {x, y + size}, {x, y},
	}
	ring := make([][2]float64, 0, len(corners))
	for _, corner := range corners {
		ring = append(ring, [2]float64{
			unprojectLng(corner[0], grid),
			unprojectLat(corner[1], grid),
		})
	}
	raw, err := json.Marshal([][][2]float64{ring})
	if err != nil {
		panic(err)
	}
	return []doc.Geometry{{Type: "Polygon", Coordinates: raw}}
}

func pointAt(id int64, member int64, x, y float64, grid surfaceGrid) composedFeature {
	return composedFeature{Feature: doc.Feature{
		ID:     id,
		Title:  fmt.Sprintf("pin %d", id),
		Member: member,
		At: &doc.Position{
			Lat: unprojectLat(y, grid),
			Lng: unprojectLng(x, grid),
		},
	}}
}

// sheet is two squares side by side with a gap between them, each with two pins
// on it: the smallest thing that is really two places.
func sheet(grid surfaceGrid) composedWorld {
	return composedWorld{
		ID:       7,
		Slug:     "sheet",
		Title:    "Sheet",
		Lenses:   []lens{{Name: "Default", Tiles: "sheet"}},
		Pyramids: []tiles.Pyramid{{Name: "sheet", Stamp: "stamp"}},
		Collections: []composedCollection{
			{
				ID:      1,
				Title:   "Pins",
				Kind:    doc.KindPoint,
				Visible: true,
				Features: []composedFeature{
					pointAt(11, 100, 600, 600, grid),
					pointAt(12, 100, 1400, 1400, grid),
					pointAt(21, 200, 4600, 600, grid),
					pointAt(22, 200, 5400, 1400, grid),
				},
			},
			{
				ID:      2,
				Key:     "regions",
				Title:   "Regions",
				Kind:    doc.KindArea,
				Visible: true,
				Features: []composedFeature{
					{Feature: doc.Feature{ID: 100, Title: "West", Geometry: squareAt(512, 512, 1024, grid)}},
					{Feature: doc.Feature{ID: 200, Title: "East", Geometry: squareAt(4096, 512, 2048, grid)}},
					{Feature: doc.Feature{
						ID: 201, Title: "East Quarter", Parent: 200,
						Geometry: squareAt(4096, 512, 512, grid),
					}},
				},
			},
		},
	}
}

func TestSplitLeavesAnUncuratedSheetWhole(t *testing.T) {
	grid := splitGrid()
	pieces, err := splitWorld(sheet(grid), curation.ShardNone, grid)
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) != 1 || pieces[0].Slug != "sheet" {
		t.Fatalf("an uncurated sheet became %d worlds", len(pieces))
	}
	if pieces[0].Lenses[0].Bounds != nil {
		t.Errorf("an uncurated sheet had its raster clipped")
	}
}

func TestSplitIntoWorlds(t *testing.T) {
	grid := splitGrid()
	pieces, err := splitWorld(sheet(grid), curation.ShardIntoWorlds, grid)
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) != 2 {
		t.Fatalf("two places on one sheet became %d worlds", len(pieces))
	}

	// The largest piece leads and keeps the sheet's identity; the rest are named
	// after it so they sort together in the picker.
	if pieces[0].Slug != "sheet" || pieces[0].Parent != "" {
		t.Errorf("the leading piece is %s (parent %q), want the sheet itself", pieces[0].Slug, pieces[0].Parent)
	}
	if pieces[1].Slug != "sheet-west" || pieces[1].Parent != "sheet" {
		t.Errorf("the second piece is %s (parent %q), want sheet-west under sheet",
			pieces[1].Slug, pieces[1].Parent)
	}
	if pieces[1].Title != "Sheet — West" {
		t.Errorf("the second piece is titled %q", pieces[1].Title)
	}
	if pieces[1].ID != 100 {
		t.Errorf("a piece carries id %d, want the area it was cut around", pieces[1].ID)
	}

	// Each piece keeps its own pins and only its own.
	for _, want := range []struct {
		world string
		ids   []int64
	}{
		{"sheet", []int64{21, 22}},
		{"sheet-west", []int64{11, 12}},
	} {
		var got []int64
		for _, part := range pieces {
			if part.Slug != want.world {
				continue
			}
			for _, collection := range part.Collections {
				if collection.Kind != doc.KindPoint {
					continue
				}
				for _, feature := range collection.Features {
					got = append(got, feature.ID)
				}
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want.ids) {
			t.Errorf("%s holds pins %v, want %v", want.world, got, want.ids)
		}
	}

	// The outline a piece was cut around is dropped; a descendant survives.
	var areas []int64
	for _, collection := range pieces[0].Collections {
		if collection.Kind != doc.KindArea {
			continue
		}
		for _, feature := range collection.Features {
			areas = append(areas, feature.ID)
		}
	}
	if fmt.Sprint(areas) != fmt.Sprint([]int64{201}) {
		t.Errorf("the leading piece keeps areas %v, want only the descendant 201", areas)
	}

	// The raster is clipped to the piece, and the ground it covers travels
	// beside the window because they are not the same rectangle.
	window := pieces[1].Lenses[0].Bounds
	ground := pieces[1].Lenses[0].Surface
	if window == nil || ground == nil {
		t.Fatalf("a piece's lens carries no window or no ground")
	}
	if window.X > ground.X || window.Y > ground.Y ||
		window.X+window.Width < ground.X+ground.Width ||
		window.Y+window.Height < ground.Y+ground.Height {
		t.Errorf("a piece's ground %+v reaches outside its window %+v", *ground, *window)
	}
}

func TestSplitIntoLenses(t *testing.T) {
	grid := splitGrid()
	pieces, err := splitWorld(sheet(grid), curation.ShardIntoLenses, grid)
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) != 1 {
		t.Fatalf("a sheet split into lenses became %d worlds, want one", len(pieces))
	}
	world := pieces[0]
	if len(world.Lenses) != 2 {
		t.Fatalf("a sheet of two places offers %d lenses", len(world.Lenses))
	}
	if len(world.Pyramids) != len(world.Lenses) {
		t.Fatalf("%d lenses draw from %d pyramids", len(world.Lenses), len(world.Pyramids))
	}
	// Lenses read top to bottom, and both pieces sit at the same height here, so
	// the tie breaks on title.
	if world.Lenses[0].Name != "East" || world.Lenses[1].Name != "West" {
		t.Errorf("lenses are named %q then %q", world.Lenses[0].Name, world.Lenses[1].Name)
	}
	if world.Lenses[0].Shard != 200 || world.Lenses[1].Shard != 100 {
		t.Errorf("lenses carry shards %d and %d", world.Lenses[0].Shard, world.Lenses[1].Shard)
	}
	// Every feature says which piece it belongs to, so a reader can show one at
	// a time without the world underneath it changing.
	for _, collection := range world.Collections {
		for _, feature := range collection.Features {
			if feature.Shard == 0 {
				t.Errorf("feature %d belongs to no piece", feature.ID)
			}
		}
	}
}

func TestSplitRefusesAPointBelongingToNoArea(t *testing.T) {
	grid := splitGrid()
	world := sheet(grid)
	world.Collections[0].Features = append(world.Collections[0].Features,
		pointAt(99, 0, 2000, 2000, grid))
	if _, err := splitWorld(world, curation.ShardIntoWorlds, grid); err == nil {
		t.Fatal("a sheet with a homeless point split anyway, so the point vanished")
	}
}

func TestSplitRefusesASheetOfOnePlace(t *testing.T) {
	grid := splitGrid()
	world := sheet(grid)
	// One family, so there is nothing to take apart.
	world.Collections[1].Features = world.Collections[1].Features[1:]
	world.Collections[0].Features = world.Collections[0].Features[2:]
	if _, err := splitWorld(world, curation.ShardIntoWorlds, grid); err == nil {
		t.Fatal("a sheet holding one place split anyway")
	}
}

// A point drawn on a piece that does not claim it moves to the piece it is
// actually on: where it sits is not in doubt, and the claim is the mistake.
func TestSplitRehomesAStrayPoint(t *testing.T) {
	grid := splitGrid()
	world := sheet(grid)
	world.Collections[0].Features[0] = pointAt(11, 200, 600, 600, grid)

	pieces, err := splitWorld(world, curation.ShardIntoWorlds, grid)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range pieces {
		if part.Slug != "sheet-west" {
			continue
		}
		for _, collection := range part.Collections {
			for _, feature := range collection.Features {
				if feature.ID == 11 {
					return
				}
			}
		}
	}
	t.Error("a point drawn on the west piece stayed with the east one that claimed it")
}
