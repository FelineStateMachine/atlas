package basemap_test

import (
	"bytes"
	"image/color"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/tiles/basemap"
)

// These tests say what the drawing is for. What it *is* -- byte for byte -- is
// settled elsewhere: the pipeline test in cmd/atlas derives a drawn level and
// holds every tile against the capture that witnessed it, which is a stronger
// statement than any assertion here could make. What that gate cannot say is
// why the renderer is shaped the way it is, so that is what these are.

// The zoom every test draws at: the zoom the style table's widths are spelled
// at, so a width in the table is a width in pixels and a test can count them.
const zoom = basemap.ReferenceZoom

// worldPerTile is how much of the world square one tile covers at that zoom,
// and pixels is how many tile pixels one world pixel spans.
const (
	worldPerTile = basemap.WorldSize >> zoom
	pixels       = basemap.TileSize / worldPerTile
)

// square is a ring around a rectangle of the world, wound whichever way; the
// renderer normalizes it.
func square(minX, minY, maxX, maxY float64) [][2]float64 {
	return [][2]float64{{minX, minY}, {maxX, minY}, {maxX, maxY}, {minX, maxY}}
}

// whole is a ring around the entire world square.
func whole() [][2]float64 { return square(0, 0, basemap.WorldSize, basemap.WorldSize) }

// middle is the tile's centre pixel, the one furthest from any edge effect.
const middle = basemap.TileSize / 2

func TestNothingDrawnIsTheFlatGround(t *testing.T) {
	// Every tile of a level exists, because the deriver needs the level
	// complete to fold it down. A tile no shape reaches is the background, and
	// the deriver goes on to omit the one they all share.
	img := basemap.NewRenderer(nil, zoom).Tile(0, 0)
	for y := range basemap.TileSize {
		for x := range basemap.TileSize {
			if got := img.NRGBAAt(x, y); got != basemap.Background {
				t.Fatalf("pixel %d,%d is %v, want the flat ground %v", x, y, got, basemap.Background)
			}
		}
	}
}

func TestRolesLandInTheDrawingsOwnOrder(t *testing.T) {
	// Water draws over parks so a lake inside a park reads as a lake, and the
	// order is the drawing's, not the order the shapes arrived in. Both
	// orderings are asked for, because "the z-order wins" is the claim.
	for _, reversed := range []bool{false, true} {
		shapes := []basemap.Feature{
			{Role: basemap.RolePark, Rings: [][][2]float64{whole()}},
			{Role: basemap.RoleWater, Rings: [][][2]float64{whole()}},
		}
		if reversed {
			shapes[0], shapes[1] = shapes[1], shapes[0]
		}
		got := basemap.NewRenderer(shapes, zoom).Tile(0, 0).NRGBAAt(middle, middle)
		want := color.NRGBA{R: 0x15, G: 0x25, B: 0x39, A: 0xff}
		if got != want {
			t.Errorf("park and water overlapping draw %v, want the water %v (shapes reversed: %t)",
				got, want, reversed)
		}
	}
}

func TestEveryRingAfterTheFirstIsAHole(t *testing.T) {
	// Publishers do not reliably wind their holes the way GeoJSON asks, so the
	// convention is positional rather than geometric: ring zero is ground and
	// everything after it cuts out, whichever way round it was given.
	for _, name := range []string{"as given", "reversed"} {
		hole := square(worldPerTile/4, worldPerTile/4, worldPerTile*3/4, worldPerTile*3/4)
		if name == "reversed" {
			for i, j := 0, len(hole)-1; i < j; i, j = i+1, j-1 {
				hole[i], hole[j] = hole[j], hole[i]
			}
		}
		shapes := []basemap.Feature{{
			Role:  basemap.RolePark,
			Rings: [][][2]float64{square(0, 0, worldPerTile, worldPerTile), hole},
		}}
		img := basemap.NewRenderer(shapes, zoom).Tile(0, 0)
		if got := img.NRGBAAt(middle, middle); got != basemap.Background {
			t.Errorf("%s: the hole's middle is %v, want the ground showing through %v",
				name, got, basemap.Background)
		}
		// And the ring outside the hole is still park.
		if got := img.NRGBAAt(4, middle); got == basemap.Background {
			t.Errorf("%s: the ring around the hole was cut out too", name)
		}
	}
}

func TestAnUnknownRoleDrawsNothing(t *testing.T) {
	// A role the style table has no entry for is a curation table running ahead
	// of the drawing. Silence is the honest answer: inventing a style would put
	// a layer on the map that nobody chose the look of.
	shapes := []basemap.Feature{{Role: "chartreuse", Rings: [][][2]float64{whole()}}}
	if got := basemap.NewRenderer(shapes, zoom).Tile(0, 0).NRGBAAt(middle, middle); got != basemap.Background {
		t.Errorf("an unstyled role drew %v", got)
	}
}

func TestAStrokeReachesInFromOutsideTheTile(t *testing.T) {
	// The bleed is why neighbouring tiles continue each other. A line whose
	// centre falls outside a tile still lays its shoulder inside it, and a tile
	// that clipped at its own edge would cut that shoulder off and show a seam.
	//
	// The line here sits four tenths of a world pixel to the left of tile 1's
	// left edge -- outside the tile, inside the bleed.
	const justOutside = worldPerTile - 0.4
	shapes := []basemap.Feature{{
		Role:  basemap.RoleBoundary,
		Lines: [][][2]float64{{{justOutside, 0}, {justOutside, basemap.WorldSize}}},
	}}
	img := basemap.NewRenderer(shapes, zoom).Tile(1, 0)
	if got := img.NRGBAAt(0, middle); got == basemap.Background {
		t.Error("a stroke just outside the tile left nothing in it, so the bleed is not being drawn")
	}
	// And it really is only a shoulder: the line's own width does not carry
	// far into the tile.
	if got := img.NRGBAAt(basemap.TileSize/4, middle); got != basemap.Background {
		t.Errorf("the stroke reached a quarter of the way across the tile: %v", got)
	}
}

func TestEmphasisWidensOneShapesStroke(t *testing.T) {
	// An arterial is a street drawn wider, not a role of its own -- which is
	// what lets one curated table say both what a layer is and how much of it
	// matters, without a second table to drift from the first.
	line := [][][2]float64{{{0, worldPerTile / 2}, {worldPerTile, worldPerTile / 2}}}
	count := func(emphasis float64) int {
		shapes := []basemap.Feature{{Role: basemap.RoleStreet, Lines: line, Emphasis: emphasis}}
		img := basemap.NewRenderer(shapes, zoom).Tile(0, 0)
		drawn := 0
		for y := range basemap.TileSize {
			if img.NRGBAAt(middle, y) != basemap.Background {
				drawn++
			}
		}
		return drawn
	}
	plain, emphasized := count(0), count(1.8)
	if plain == 0 {
		t.Fatal("an unemphasized street drew nothing")
	}
	if emphasized <= plain {
		t.Errorf("emphasis 1.8 drew %d pixels across, plain drew %d", emphasized, plain)
	}
}

func TestWidthsAreWorldTrue(t *testing.T) {
	// Widths are spelled at the reference zoom and scale with depth, so a
	// street is the same width on the ground however deep the pyramid goes --
	// which, folded back down, is the same width on the screen.
	line := func(at int) [][][2]float64 {
		across := float64(int(basemap.WorldSize) >> at)
		return [][][2]float64{{{0, across / 2}, {across, across / 2}}}
	}
	count := func(at int) int {
		shapes := []basemap.Feature{{Role: basemap.RoleStreet, Lines: line(at)}}
		img := basemap.NewRenderer(shapes, at).Tile(0, 0)
		drawn := 0
		for y := range basemap.TileSize {
			if img.NRGBAAt(middle, y) != basemap.Background {
				drawn++
			}
		}
		return drawn
	}
	shallow, deep := count(zoom), count(zoom+1)
	if deep <= shallow {
		t.Errorf("a level deeper drew the same street %d pixels wide, the reference level %d; "+
			"a world-true width doubles with the zoom", deep, shallow)
	}
}

func TestTheSameShapesMakeTheSameBytes(t *testing.T) {
	// The claim the whole city pyramid rests on. Two renderers, built and drawn
	// independently, must not disagree by a bit -- there is no clock, no map
	// iteration and no concurrency inside a tile for them to disagree over.
	build := func() []byte {
		shapes := []basemap.Feature{
			{Role: basemap.RoleWater, Rings: [][][2]float64{whole()}},
			{Role: basemap.RoleTrail, Lines: [][][2]float64{{{0, 0}, {worldPerTile, worldPerTile}}}},
			{Role: basemap.RoleStreet, Lines: [][][2]float64{{{0, worldPerTile}, {worldPerTile, 0}}}, Emphasis: 1.4},
		}
		body, err := basemap.EncodePNG(basemap.NewRenderer(shapes, zoom).Tile(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	if first, second := build(), build(); !bytes.Equal(first, second) {
		t.Errorf("two renders of the same shapes wrote %d and %d bytes", len(first), len(second))
	}
}

func TestATileIsOpaqueTruecolour(t *testing.T) {
	// A tile has no transparency to carry -- the ground is always painted --
	// and the encoder is left to notice that, because an alpha channel nothing
	// uses is a quarter of the pixels wasted in every tile of every city.
	body, err := basemap.EncodePNG(basemap.NewRenderer(nil, zoom).Tile(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	// The colour type is the 26th byte of a PNG: 8-byte signature, then the
	// IHDR chunk's length, type, width, height and bit depth.
	const colourType = 8 + 4 + 4 + 4 + 4 + 1
	if len(body) <= colourType {
		t.Fatalf("a tile encoded to %d bytes", len(body))
	}
	if got := body[colourType]; got != 2 {
		t.Errorf("the tile's PNG colour type is %d, want 2 (truecolour, no alpha)", got)
	}
}

func TestTheDrawingAndTheDeriverAgreeOnEmpty(t *testing.T) {
	// The deriver omits a level's background tile and asks a reader to paint
	// the colour behind the raster instead. That only works if the colour it
	// samples is the colour this package painted.
	if basemap.BackgroundColor() != basemap.Background {
		t.Error("the drawing reports a background it does not paint")
	}
	if pixels != 2 {
		t.Fatalf("the tests assume the reference zoom halves the world into %d-pixel tiles", pixels)
	}
}
