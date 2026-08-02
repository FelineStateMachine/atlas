package arcgismap

import (
	"encoding/json"
	"reflect"
	"testing"
)

// clipWindow is a plain unit-square window: edges on whole degrees, so every
// expected crossing is exact.
var clipWindow = Window{West: 0, South: 0, East: 1, North: 1}

func ring(positions ...[2]float64) [][]float64 {
	out := make([][]float64, 0, len(positions)+1)
	for _, p := range positions {
		out = append(out, []float64{p[0], p[1]})
	}
	return append(out, []float64{positions[0][0], positions[0][1]})
}

func TestClipRingsKeepsAnInsideRing(t *testing.T) {
	inside := [][][][]float64{{ring(
		[2]float64{0.2, 0.2}, [2]float64{0.8, 0.2}, [2]float64{0.8, 0.8}, [2]float64{0.2, 0.8},
	)}}
	got := ClipRings(clipWindow, inside)
	if !reflect.DeepEqual(got, inside) {
		t.Fatalf("an inside ring changed: %v", got)
	}
}

func TestClipRingsCutsACrossingRing(t *testing.T) {
	crossing := [][][][]float64{{ring(
		[2]float64{0.5, 0.5}, [2]float64{1.5, 0.5}, [2]float64{1.5, 0.7}, [2]float64{0.5, 0.7},
	)}}
	got := ClipRings(clipWindow, crossing)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("crossing ring clipped to %v", got)
	}
	want := ring([2]float64{0.5, 0.5}, [2]float64{1, 0.5}, [2]float64{1, 0.7}, [2]float64{0.5, 0.7})
	if !reflect.DeepEqual(got[0][0], want) {
		t.Fatalf("crossing ring clipped to %v, want %v", got[0][0], want)
	}
}

func TestClipRingsDropsAnOutsideRing(t *testing.T) {
	outside := [][][][]float64{{ring(
		[2]float64{2, 2}, [2]float64{3, 2}, [2]float64{3, 3}, [2]float64{2, 3},
	)}}
	if got := ClipRings(clipWindow, outside); got != nil {
		t.Fatalf("an outside ring survived: %v", got)
	}
}

func TestClipRingsKeepsAHole(t *testing.T) {
	holed := [][][][]float64{{
		ring([2]float64{0.1, 0.1}, [2]float64{0.9, 0.1}, [2]float64{0.9, 0.9}, [2]float64{0.1, 0.9}),
		ring([2]float64{0.4, 0.4}, [2]float64{0.6, 0.4}, [2]float64{0.6, 0.6}, [2]float64{0.4, 0.6}),
	}}
	got := ClipRings(clipWindow, holed)
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("the hole did not survive: %v", got)
	}
}

func TestClipRingsDropsAPolygonWhoseGroundIsGone(t *testing.T) {
	// The outer ring misses the window even though its hole would clip.
	gone := [][][][]float64{{
		ring([2]float64{2, 2}, [2]float64{3, 2}, [2]float64{3, 3}, [2]float64{2, 3}),
		ring([2]float64{0.4, 0.4}, [2]float64{0.6, 0.4}, [2]float64{0.6, 0.6}, [2]float64{0.4, 0.6}),
	}}
	if got := ClipRings(clipWindow, gone); got != nil {
		t.Fatalf("a polygon without ground survived: %v", got)
	}
}

func TestClipRingsCoversTheWindowCorner(t *testing.T) {
	// A huge ring swallowing the whole window clips to the window itself.
	swallowing := [][][][]float64{{ring(
		[2]float64{-5, -5}, [2]float64{5, -5}, [2]float64{5, 5}, [2]float64{-5, 5},
	)}}
	got := ClipRings(clipWindow, swallowing)
	if len(got) != 1 || len(got[0]) != 1 || len(got[0][0]) != 5 {
		t.Fatalf("the window itself should remain, got %v", got)
	}
}

func TestClipLinesSplitsAtTheWindow(t *testing.T) {
	// In at the west, out at the east, back in through the north: two parts.
	zigzag := [][][]float64{{
		{-0.5, 0.5}, {0.5, 0.5}, {1.5, 0.5}, {1.5, 2}, {0.5, 2}, {0.5, 0.9},
	}}
	got := ClipLines(clipWindow, zigzag)
	want := [][][]float64{
		{{0, 0.5}, {0.5, 0.5}, {1, 0.5}},
		{{0.5, 1}, {0.5, 0.9}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zigzag clipped to %v, want %v", got, want)
	}
}

func TestClipLinesKeepsAnInsideLine(t *testing.T) {
	inside := [][][]float64{{{0.1, 0.1}, {0.5, 0.5}, {0.9, 0.2}}}
	got := ClipLines(clipWindow, inside)
	if !reflect.DeepEqual(got, inside) {
		t.Fatalf("an inside line changed: %v", got)
	}
}

func TestClipLinesDropsAnOutsideLine(t *testing.T) {
	outside := [][][]float64{{{2, 2}, {3, 3}}}
	if got := ClipLines(clipWindow, outside); got != nil {
		t.Fatalf("an outside line survived: %v", got)
	}
}

// Clipping the same shape twice spells the same bytes, which is what capture
// determinism asks of every transformation.
func TestClipIsDeterministic(t *testing.T) {
	shape := [][][][]float64{{ring(
		[2]float64{0.123456789, -0.4}, [2]float64{1.7, 0.3}, [2]float64{0.6, 1.9},
	)}}
	first, err := json.Marshal(ClipRings(clipWindow, shape))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(ClipRings(clipWindow, [][][][]float64{{ring(
		[2]float64{0.123456789, -0.4}, [2]float64{1.7, 0.3}, [2]float64{0.6, 1.9},
	)}}))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("clipping is not deterministic: %s vs %s", first, second)
	}
	again := ClipRings(clipWindow, ClipRings(clipWindow, shape))
	raw, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(first) {
		t.Fatalf("clipping is not idempotent: %s vs %s", raw, first)
	}
}
