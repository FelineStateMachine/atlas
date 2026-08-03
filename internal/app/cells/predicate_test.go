package cells_test

import (
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// The held-cell predicate, asked the questions a reader's pins actually ask
// it: points on a cell's edge, points a hair past it, and the same place
// spelled twice at the two ends of a world that wraps.
//
// The shared vectors (analysis/testdata/cells, read by tests/cells) hold the
// arithmetic to the oracle both lanes answer. This file holds the *edges* of
// it -- the cases a vector file never quite reaches, because a fixture records
// what a map did and these ask what a map must never do.
//
// Everything here is a table and nothing reads a file: this package lives
// under internal/app, where the hostenv rule says the arithmetic may not know
// what a filesystem is. The one declaration spelled out below -- the Mars
// flattening -- is the corpus bundle's own, and tests/cells holds the shared
// grounds fixture to the shipped payload so a drift in either copy is a
// failure somewhere rather than a quiet disagreement.

// square is the hand-derived 1024 ground of the analysis suite: a world
// declaring the whole of itself, which is the ground `m6` was halved out of.
// Its rectangle is [672, -704] to [704, -672]
// (analysis/testdata/cells/containment.json).
var square = cells.Extent{MinX: 0, MinY: -1024, MaxX: 1024, MaxY: 0}

// marsAttrs is the declaration the shipped Mars bundle makes
// (testdata/corpus/bundles/mars, and `mars/global/0` in the shared grounds):
// a sphere, flattened equirectangularly over the whole 8192x4096 square.
var marsAttrs = map[string]string{
	semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
	semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
	semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
	semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
}

func TestGeohashHoldsItsBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		hash  string
		x, y  float64
		holds bool
	}{
		{"the middle of the cell", "m6", 688, -688, true},
		{"the maximum corner", "m6", 704, -672, true},
		{"the minimum corner", "m6", 672, -704, true},
		{"the top edge", "m6", 688, -672, true},
		{"the left edge", "m6", 672, -688, true},
		// Boundaries are inclusive on both sides, so a pin on the line
		// between two cells is standing in both. That is the contract's
		// answer and not an accident of the comparison: the line is drawn a
		// pixel wide and the reader put the pin on it.
		{"the neighbour shares the edge", "m4", 672, -688, true},
		{"a hair past the maximum", "m6", 704.0001, -672, false},
		{"a hair past the minimum", "m6", 672, -704.0001, false},
		{"the other side of the world", "m6", 128, -128, false},
		// The parent holds everything its children do, which is what makes
		// telescoping in narrow rather than move.
		{"the parent holds the child's corner", "m", 704, -672, true},
		// The root is the ground: an address with nothing in it halves
		// nothing away.
		{"the root holds the far corner", "", 1024, 0, true},
		{"the root does not hold what is off the ground", "", 1024.5, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cells.GeohashHeld(square, test.hash)(test.x, test.y); got != test.holds {
				t.Errorf("%q holds (%g, %g) = %v, want %v", test.hash, test.x, test.y, got, test.holds)
			}
		})
	}
}

// TestS2HoldsOnMarsAsTheBundleDeclaresIt asks the predicate the questions a
// reader standing on Mars asks it, through the flattening the shipped bundle
// declares.
func TestS2HoldsOnMarsAsTheBundleDeclaresIt(t *testing.T) {
	mapping, ok := cells.MappingOf(marsAttrs)
	if !ok {
		t.Fatal("the Mars declaration is a flattening and the reader refused it")
	}

	// Mars's world square is 8192 x 4096 picturing the whole body, so the
	// centre of the raster is (0, 0) on the ground and the corners are the
	// antimeridian at either pole. World pixels are y-negative-down, which is
	// the space a packed position lands in.
	const width, height = 8192.0, 4096.0
	centre := [2]float64{width / 2, -height / 2}
	held := cells.S2CellAt(mapping, centre[0], centre[1])
	if held == "" {
		t.Fatal("the middle of Mars is nowhere")
	}

	tests := []struct {
		name  string
		cell  string
		x, y  float64
		holds bool
	}{
		{"the cell under a point holds it", held, centre[0], centre[1], true},
		{"and so does its parent", cells.S2Parent(held), centre[0], centre[1], true},
		{"and so does the face they are on", faceOf(held), centre[0], centre[1], true},
		{"but not one of the other five", otherFace(t, held), centre[0], centre[1], false},
		{"a level-10 cell is small", held, centre[0] + 64, centre[1], false},
		{"the antipode is elsewhere", held, 0, centre[1], false},
		// The root is the whole sphere. Nothing narrows, which is the same
		// answer the seam gives and the reason a session with no cell held
		// never reaches this predicate at all.
		{"the root holds every place", "", 12, -3000, true},
		// An address nobody can read holds nothing. The guard matters: the
		// library's zero cell id spans the entire curve, so an unguarded
		// containment on a misspelling would quietly stop narrowing.
		{"a misspelling holds nothing", "not-a-token", centre[0], centre[1], false},
		{"an empty-looking misspelling holds nothing", "zzzz", centre[0], centre[1], false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cells.S2Held(mapping, test.cell)(test.x, test.y); got != test.holds {
				t.Errorf("%q holds (%g, %g) = %v, want %v", test.cell, test.x, test.y, got, test.holds)
			}
		})
	}

	// The two ends of the raster are one meridian on the body. The picture
	// has a seam there and the sphere does not, and the arithmetic says so:
	// the far west edge and the far east edge land on the same face, on
	// either side of a cell boundary that happens to fall exactly on the
	// antimeridian. They are neighbours rather than the same address, which
	// is as close as floating point gets to "the same place" -- and it is the
	// seam's answer too, since it is the same library's.
	west := cells.S2CellAt(mapping, 0, -height/2)
	east := cells.S2CellAt(mapping, width, -height/2)
	if west == "" || east == "" || faceOf(west) != faceOf(east) {
		t.Errorf("the antimeridian is two places: west %q on face %q, east %q on face %q",
			west, faceOf(west), east, faceOf(east))
	}

	// A pin straddling the edge between two S2 cells is in both of them, the
	// same way it is in both of two geohash cells: the leaf under the point
	// is one cell, and the neighbours that share the edge are asked about the
	// same leaf. This is the weaker claim the arithmetic actually makes --
	// containment is by descendancy, and a point is in exactly the chain of
	// cells above its leaf.
	for level, ancestor := 1, held; ancestor != ""; level, ancestor = level+1, cells.S2Parent(ancestor) {
		if !cells.S2Held(mapping, ancestor)(centre[0], centre[1]) {
			t.Errorf("the ancestor %q of %q does not hold the point they were both found from",
				ancestor, held)
		}
		if got := cells.S2Level(ancestor); got != cells.S2Level(held)-level+1 {
			t.Errorf("%q is at level %d, %d levels above %q", ancestor, got, level-1, held)
		}
	}
}

func TestApplicableSystemsAsksTheWorldTwoQuestions(t *testing.T) {
	tests := []struct {
		name    string
		attrs   map[string]string
		systems []string
	}{
		{"a world that says nothing is a plane", nil, []string{"geohash"}},
		{"and so is one that says so", map[string]string{
			semconv.KeyGeometrySurface: "plane",
		}, []string{"geohash"}},
		{"a sphere with a flattening takes both", marsAttrs, []string{"geohash", "s2"}},
		// The included Earth volume's own declaration: the body and its
		// radius change nothing the systems read. In particular, naming the
		// earth on a *sphere* does not put geohash into its real mode --
		// that mode is a plane's (see TestEarthMercatorAsksAllThreeQuestions)
		// -- so the whole-planet picture divides synthetically, like Mars.
		{"the included Earth's declaration takes both", map[string]string{
			semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
			semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
			semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
			semconv.KeyGeometryBody:        "earth",
			semconv.KeyGeometryRadiusKM:    "6371.0088",
		}, []string{"geohash", "s2"}},
		{"a sphere that declares no flattening takes neither but geohash", map[string]string{
			semconv.KeyGeometrySurface: "sphere",
		}, []string{"geohash"}},
		{"nor one whose window is unreadable", map[string]string{
			semconv.KeyGeometrySurface:     "sphere",
			semconv.KeyGeometryProjection:  "equirect",
			semconv.KeyGeometryEquirectPx:  "0,0,0,0",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
		}, []string{"geohash"}},
		{"nor one that pictures no ground", map[string]string{
			semconv.KeyGeometrySurface:     "sphere",
			semconv.KeyGeometryProjection:  "equirect",
			semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
			semconv.KeyGeometryEquirectDeg: "-180,90,-180,-90",
		}, []string{"geohash"}},
		{"nor one whose numbers are not numbers", map[string]string{
			semconv.KeyGeometrySurface:     "sphere",
			semconv.KeyGeometryProjection:  "equirect",
			semconv.KeyGeometryEquirectPx:  "0,0,8192",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
		}, []string{"geohash"}},
		// The window is only read when the world says which flattening the
		// numbers are for, which is the reader's rule in the seam too
		// (analysis/semconv/geometry.ts, declaredWindow).
		{"nor one that does not say what its numbers are", map[string]string{
			semconv.KeyGeometrySurface:     "sphere",
			semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
		}, []string{"geohash"}},
		{"a flattening without a sphere is a picture of nothing in particular", map[string]string{
			semconv.KeyGeometryProjection:  "equirect",
			semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
			semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
		}, []string{"geohash"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cells.ApplicableSystems(test.attrs)
			if len(got) != len(test.systems) {
				t.Fatalf("systems = %v, want %v", got, test.systems)
			}
			for at := range got {
				if got[at] != test.systems[at] {
					t.Fatalf("systems = %v, want %v", got, test.systems)
				}
			}
			// The list and the question have to answer alike, because the
			// navigator offers from one and the session validates with the
			// other.
			for _, system := range []string{"geohash", "s2", "", "h3", "GEOHASH"} {
				want := false
				for _, offered := range got {
					want = want || offered == system
				}
				if cells.Applicable(test.attrs, system) != want {
					t.Errorf("Applicable(%q) disagrees with %v", system, got)
				}
			}
		})
	}
}

// TestS2ParseCellRefusesWhatIsNotAPlace holds the address reader to the
// navigator's rules: the root is a place, a token past the telescope's floor
// clamps to it, and everything else is either canonical or nothing.
func TestS2ParseCellRefusesWhatIsNotAPlace(t *testing.T) {
	tests := []struct {
		name string
		text string
		cell string
		ok   bool
	}{
		{"the root", "", "", true},
		{"a face", "5", "5", true},
		{"a token at the floor", "47a1cb", "47a1cb", true},
		{"a leaf clamps to the floor", "47a1cbd595522b39", "47a1cb", true},
		{"a non-token", "xyz", "", false},
		{"a word", "geohash", "", false},
		{"a number that is not a cell", "0", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cell, ok := cells.S2ParseCell(test.text)
			if ok != test.ok || cell != test.cell {
				t.Errorf("S2ParseCell(%q) = %q, %v; want %q, %v", test.text, cell, ok, test.cell, test.ok)
			}
		})
	}
}

// bendAttrs is the declaration the shipped bend-or bundle makes
// (testdata/corpus/bundles/bend-or, and `bend-or/2026-08-02/0` in the shared
// grounds): a plane city anchored to real earth by a Mercator window.
var bendAttrs = map[string]string{
	semconv.KeyGeometrySurface:     semconv.SurfacePlane,
	semconv.KeyGeometryBody:        "earth",
	semconv.KeyGeometryMercatorPx:  "0,0,8192,8192",
	semconv.KeyGeometryMercatorDeg: "-121.48204667177453,44.17572950664809,-121.1529533282255,43.93922950850393",
}

// TestBendOrComesOutBaseDepthFourFloorSix pins the anchored telescope to the
// window it was derived from: cell spans halve from 360°/180° per the 5-bit
// alternation, depth 3 is 1.40625° both ways (too coarse for either axis of
// the 0.32909° × 0.23650° window), and depth 4's latitude span (0.17578125°)
// fits where its longitude span (0.3515625°) just misses. Base 4, floor 6 --
// exactly, and the seam's geohash.test.ts asserts the same two numbers.
func TestBendOrComesOutBaseDepthFourFloorSix(t *testing.T) {
	mapping, anchored := cells.EarthMercator(bendAttrs)
	if !anchored {
		t.Fatal("the bend-or declaration is an earth anchoring and the reader refused it")
	}
	if got := cells.GeohashBaseDepth(mapping); got != 4 {
		t.Errorf("base depth = %d, want 4", got)
	}
	if got := cells.GeohashDepthOf(bendAttrs); got != 6 {
		t.Errorf("depth floor = %d, want 6", got)
	}
	// And a synthetic plane keeps the constant.
	if got := cells.GeohashDepthOf(nil); got != cells.GeohashMaxDepth {
		t.Errorf("a synthetic plane answers %d, want %d", got, cells.GeohashMaxDepth)
	}
}

// TestEarthMercatorAsksAllThreeQuestions: a plane, an invertible Mercator
// window, and a body that says earth -- drop any one and real mode never
// starts. Mars is a sphere with an equirect window and no earth body, and
// must be completely unaffected.
func TestEarthMercatorAsksAllThreeQuestions(t *testing.T) {
	without := func(key string) map[string]string {
		out := map[string]string{}
		for k, v := range bendAttrs {
			if k != key {
				out[k] = v
			}
		}
		return out
	}
	if _, anchored := cells.EarthMercator(bendAttrs); !anchored {
		t.Error("the whole declaration anchors, and was refused")
	}
	// Surface ABSENT still means plane; the other clauses hold it up.
	if _, anchored := cells.EarthMercator(without(semconv.KeyGeometrySurface)); !anchored {
		t.Error("an undeclared surface is a plane, and was refused")
	}
	for _, key := range []string{
		semconv.KeyGeometryBody,
		semconv.KeyGeometryMercatorPx,
		semconv.KeyGeometryMercatorDeg,
	} {
		if _, anchored := cells.EarthMercator(without(key)); anchored {
			t.Errorf("dropping %s still anchored", key)
		}
	}
	if _, anchored := cells.EarthMercator(marsAttrs); anchored {
		t.Error("mars anchored to earth")
	}
	// A world that declares its numbers are a DIFFERENT flattening is
	// declaring something this cannot read.
	contradicted := map[string]string{}
	for k, v := range bendAttrs {
		contradicted[k] = v
	}
	contradicted[semconv.KeyGeometryProjection] = semconv.ProjectionEquirect
	if _, anchored := cells.EarthMercator(contradicted); anchored {
		t.Error("a contradicting projection was read as a Mercator window")
	}
}

// TestRealGeohashHoldsItsBoundaries is [TestGeohashHoldsItsBoundaries] in the
// real mode: the cell's ground is a degree rectangle, the question travels
// through the flattening, and the boundaries are inclusive in DEGREES. The
// probes are the shared vectors' own (analysis/testdata/cells,
// containment.json, the real-* cases), plus the edges a fixture never quite
// reaches.
func TestRealGeohashHoldsItsBoundaries(t *testing.T) {
	mapping, anchored := cells.EarthMercator(bendAttrs)
	if !anchored {
		t.Fatal("bend-or did not anchor")
	}
	tests := []struct {
		name  string
		hash  string
		x, y  float64
		holds bool
	}{
		{"the raster centre is in its own base cell", "9rcd", 4096, -4096, true},
		{"and in its own floor cell", "9rcdxk", 4096, -4096, true},
		{"and not in the neighbour", "9rcf", 4096, -4096, false},
		{"a point east of the window is still on real ground", "9rcf", 9000, -4096, true},
		{"the root is the whole earth", "", 9000, -4096, true},
		{"the north-west quarter probe", "9rcdux", 1024, -2048, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cells.GeohashRealHeld(mapping, test.hash)(test.x, test.y); got != test.holds {
				t.Errorf("%q holds (%g, %g) = %v, want %v", test.hash, test.x, test.y, got, test.holds)
			}
		})
	}

	// The address under a point at every layer, against the vectors' own
	// derivation: 9rcd / 9rcdx / 9rcdxk under the raster centre.
	for depth, want := range map[int]string{4: "9rcd", 5: "9rcdx", 6: "9rcdxk"} {
		if got := cells.GeohashRealCellAt(mapping, 4096, -4096, depth); got != want {
			t.Errorf("the centre at depth %d = %q, want %q", depth, got, want)
		}
	}

	// The drawn rectangle overhangs the window, honestly: 9rcd's west edge
	// (-121.640625°) is west of the raster, so its extent starts at a
	// negative x. The shared geometry vectors carry the full-precision
	// numbers; this holds the shape of the fact.
	extent := cells.GeohashRealExtent(mapping, "9rcd")
	if extent.MinX >= 0 {
		t.Errorf("9rcd's west edge is on the raster (MinX %g); it belongs west of it", extent.MinX)
	}
	if !extent.Holds(4096, -4096) {
		t.Error("9rcd's own extent does not hold the raster centre")
	}
}

// faceOf telescopes an address all the way out to the face it is on.
func faceOf(cell string) string {
	for cells.S2Level(cell) > 1 {
		cell = cells.S2Parent(cell)
	}
	return cell
}

// otherFace names one of the five faces an address is not on.
func otherFace(t *testing.T, cell string) string {
	t.Helper()
	for _, face := range cells.S2Children("") {
		if face != faceOf(cell) {
			return face
		}
	}
	t.Fatalf("%q is on every face", cell)
	return ""
}
