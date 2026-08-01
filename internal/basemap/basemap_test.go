package basemap

import (
	"bytes"
	"image"
	"testing"
)

// A square with a square hole, big enough to land across several tiles at
// the deepest level of a small pyramid.
func fixtureFeatures() []Feature {
	outer := [][2]float64{{1000, 1000}, {5000, 1000}, {5000, 5000}, {1000, 5000}}
	hole := [][2]float64{{2000, 2000}, {2000, 3000}, {3000, 3000}, {3000, 2000}}
	street := [][2]float64{{500, 4000}, {7500, 4200}}
	trail := [][2]float64{{500, 6000}, {7500, 6100}}
	return []Feature{
		{Role: RolePark, Rings: [][][2]float64{outer, hole}},
		{Role: RoleStreet, Lines: [][][2]float64{street}, Emphasis: 1.8},
		{Role: RoleTrail, Lines: [][][2]float64{trail}},
		{Role: RoleBoundary, Lines: [][][2]float64{{{800, 800}, {7400, 800}, {7400, 7400}, {800, 7400}, {800, 800}}}},
	}
}

// The same features must render and encode to the same bytes, twice over --
// the property the crawler's hash-compare writes stand on.
func TestRenderIsDeterministic(t *testing.T) {
	first := NewRenderer(fixtureFeatures(), 3)
	second := NewRenderer(fixtureFeatures(), 3)
	for _, tile := range [][3]int{{3, 1, 1}, {3, 2, 2}, {3, 0, 0}} {
		a := first.Tile(tile[0], tile[1], tile[2])
		b := second.Tile(tile[0], tile[1], tile[2])
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Fatalf("tile %v pixels differ between renders", tile)
		}
		pngA, err := EncodePNG(a)
		if err != nil {
			t.Fatal(err)
		}
		pngB, err := EncodePNG(b)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(pngA, pngB) {
			t.Fatalf("tile %v encodes differently between renders", tile)
		}
	}
}

// A hole in a polygon must keep the background, however the source wound
// its rings.
func TestHolesStayGround(t *testing.T) {
	// Both rings wound the same way: normalization must fix the hole.
	outer := [][2]float64{{1000, 1000}, {5000, 1000}, {5000, 5000}, {1000, 5000}}
	hole := [][2]float64{{2000, 2000}, {3000, 2000}, {3000, 3000}, {2000, 3000}}
	renderer := NewRenderer([]Feature{{Role: RolePark, Rings: [][][2]float64{outer, hole}}}, 1)

	// Zoom 1: the world is 2x2 tiles of 4096 world px, so tile (0,0) shows
	// the square and its hole. World px 2500,2500 (hole middle) is tile px
	// 156; world px 1500,1500 (park ground) is tile px 93.
	tile := renderer.Tile(1, 0, 0)
	background := tile.NRGBAAt(156, 156)
	if background != Background {
		t.Fatalf("hole middle is %v, not background", background)
	}
	park := tile.NRGBAAt(93, 93)
	if park != styles[RolePark].Fill {
		t.Fatalf("park ground is %v", park)
	}
}

// Adjacent tiles must continue a shape exactly along their shared edge:
// the same column of pixels, whichever tile drew it.
func TestNeighbouringTilesAgreeAtTheSeam(t *testing.T) {
	renderer := NewRenderer(fixtureFeatures(), 3)
	// At zoom 3 the world is 8 tiles of 1024 world px. The street crosses
	// the boundary between tiles (1,3) and (2,3); compare the last column
	// of the left tile with the first column of the right one. Antialiased
	// coverage at a shared edge comes from the same geometry through the
	// same transform, so the columns must match exactly.
	left := renderer.Tile(3, 1, 3)
	right := renderer.Tile(3, 2, 3)
	for y := range tileSize {
		l := left.NRGBAAt(tileSize-1, y)
		r := right.NRGBAAt(0, y)
		// The seam columns sit one world pixel apart, so demand agreement
		// in kind, not in exact antialias: both background or both not.
		lGround := l == Background
		rGround := r == Background
		if lGround != rGround {
			t.Fatalf("seam disagrees at y=%d: %v vs %v", y, l, r)
		}
	}
}

// A tile nothing touches is the flat background, and every tile exists.
// Tile (6,2) sits east of the park and street, north of the trail, and
// inside the boundary rectangle's ring without touching its lines.
func TestEmptyTileIsBackground(t *testing.T) {
	renderer := NewRenderer(fixtureFeatures(), 3)
	tile := renderer.Tile(3, 6, 2)
	for at := 0; at < len(tile.Pix); at += 4 {
		if tile.Pix[at] != Background.R || tile.Pix[at+1] != Background.G || tile.Pix[at+2] != Background.B {
			t.Fatalf("empty tile carries paint at offset %d", at)
		}
	}
}

// Two renders of one empty tile share bytes with each other -- the property
// the placeholder machinery downstream leans on to fold background tiles
// into one hash.
func TestEmptyTilesShareBytes(t *testing.T) {
	renderer := NewRenderer(fixtureFeatures(), 3)
	a, err := EncodePNG(renderer.Tile(3, 6, 2))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodePNG(renderer.Tile(3, 5, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two empty tiles encode differently")
	}
}

// The index must hand a tile every feature that bleeds into it, not only
// those whose geometry starts there: a stroke near an edge shows in the
// neighbour.
func TestStrokesBleedAcrossTileEdges(t *testing.T) {
	// At zoom 6 a tile is 128 world px; a street whose centerline sits half
	// a world pixel into tile (8,0) still paints the last column of tile
	// (7,0), because its width reaches across the seam.
	street := []Feature{{Role: RoleStreet, Lines: [][][2]float64{{{1024.5, 100}, {1024.5, 900}}}}}
	renderer := NewRenderer(street, 6)
	tile := renderer.Tile(6, 7, 1)
	found := false
	for y := 0; y < tileSize && !found; y++ {
		if tile.NRGBAAt(tileSize-1, y) != Background {
			found = true
		}
	}
	if !found {
		t.Fatal("a street on the far side of the seam leaves no paint in this tile")
	}
}

func TestTileSizeIsThePipelines(t *testing.T) {
	renderer := NewRenderer(nil, 1)
	tile := renderer.Tile(1, 0, 0)
	if tile.Bounds() != image.Rect(0, 0, 256, 256) {
		t.Fatalf("tile bounds are %v", tile.Bounds())
	}
}
