package arcgismap

import (
	"math"
	"testing"
)

func squareRings(west, south, size float64) [][][][]float64 {
	return [][][][]float64{{{
		{west, south}, {west + size, south}, {west + size, south + size}, {west, south + size}, {west, south},
	}}}
}

func TestPointInRings(t *testing.T) {
	holed := [][][][]float64{{
		{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}},
		{{4, 4}, {6, 4}, {6, 6}, {4, 6}, {4, 4}},
	}}
	cases := []struct {
		lon, lat float64
		want     bool
	}{
		{5, 2, true},   // on the ground
		{5, 5, false},  // in the hole
		{15, 5, false}, // outside
		{-1, -1, false},
	}
	for _, tc := range cases {
		if got := pointInRings(tc.lon, tc.lat, holed); got != tc.want {
			t.Fatalf("pointInRings(%v,%v) = %v", tc.lon, tc.lat, got)
		}
	}
}

func TestFeatureAnchor(t *testing.T) {
	lon, lat, ok := featureAnchor(Geometry{Type: GeometryRings, Rings: squareRings(2, 4, 2)})
	if !ok || math.Abs(lon-3) > 1e-9 || math.Abs(lat-5) > 1e-9 {
		t.Fatalf("square anchors at (%v,%v), %v", lon, lat, ok)
	}
	// Two polygons: the larger one's centroid answers.
	both := append(squareRings(0, 0, 1), squareRings(10, 10, 3)...)
	lon, lat, ok = featureAnchor(Geometry{Type: GeometryRings, Rings: both})
	if !ok || math.Abs(lon-11.5) > 1e-9 || math.Abs(lat-11.5) > 1e-9 {
		t.Fatalf("the larger polygon should anchor, got (%v,%v)", lon, lat)
	}
	// The longest line's middle vertex answers.
	lon, lat, ok = featureAnchor(Geometry{Type: GeometryLines, Lines: [][][]float64{
		{{0, 0}, {0.1, 0}},
		{{5, 5}, {6, 5}, {7, 5}},
	}})
	if !ok || lon != 6 || lat != 5 {
		t.Fatalf("line anchors at (%v,%v), %v", lon, lat, ok)
	}
	if _, _, ok := featureAnchor(Geometry{Type: GeometryRings}); ok {
		t.Fatal("an empty geometry anchored")
	}
}

func TestLocateFindsTheHoldingUnit(t *testing.T) {
	index := &hydroIndex{units: []hydroUnit{
		{code: "101900030301", name: "Walnut Creek", rings: squareRings(0, 0, 1)},
		{code: "101900030304", name: "Big Dry Creek", rings: squareRings(1, 0, 1)},
	}}
	unit, found := index.locate(1.5, 0.5)
	if !found || unit.name != "Big Dry Creek" {
		t.Fatalf("located %+v, %v", unit, found)
	}
	if _, found := index.locate(5, 5); found {
		t.Fatal("a point in no unit located one")
	}
	var none *hydroIndex
	if _, found := none.locate(0, 0); found {
		t.Fatal("a nil index located a unit")
	}
}

func TestClaimPositionsSamplesTheBoundary(t *testing.T) {
	// A long line: the anchor plus an even sample, never the whole ring.
	line := make([][]float64, 100)
	for at := range line {
		line[at] = []float64{float64(at), 0}
	}
	positions := claimPositions(Geometry{Type: GeometryLines, Lines: [][][]float64{line}})
	if len(positions) < 2 || len(positions) > claimSamples+2 {
		t.Fatalf("%d positions sampled", len(positions))
	}
}

func TestBuildHydroIndexReadsTheCapturedSubwatersheds(t *testing.T) {
	capture := fixtureCapture()
	index := buildHydroIndex(&capture)
	if index == nil || len(index.units) != 2 {
		t.Fatalf("index is %+v", index)
	}
	without := fixtureCapture()
	kept := without.Datasets[:0]
	for _, dataset := range without.Datasets {
		if dataset.Slug != SlugSubwatersheds {
			kept = append(kept, dataset)
		}
	}
	without.Datasets = kept
	if buildHydroIndex(&without) != nil {
		t.Fatal("a capture without subwatersheds built an index")
	}
}
