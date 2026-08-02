package app

import (
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// The two halves of the grid's server side: what a request is allowed to put
// in the session, and what the session then narrows.
//
// They are one subject rather than two. A system value that reaches the record
// is a system the filter has to be able to divide by, because the seam divides
// by it whatever the server thinks -- so the refusal below and the predicate
// beneath it are the same invariant said at the two ends.

// sphere is the flattening a planetary world declares: the Mars bundle's own
// attributes, which golden/cells holds against the fixture itself.
var sphere = map[string]string{
	semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
	semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
	semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
	semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
}

// plane is a world that says nothing about what its picture is of, which is
// every game map: geohash divides it and nothing else offers to.
var plane = map[string]string{semconv.KeyIconOutset: semconv.OutsetDark}

func TestApplyGridRefusesASystemTheWorldDoesNotDivide(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		held  Grid
		sent  string
		want  Grid
	}{
		{
			name:  "a sphere takes S2",
			attrs: sphere, held: Grid{System: "geohash", Cell: "9q"},
			sent: "s2", want: Grid{System: "s2", Cell: "9q"},
		},
		{
			// The refusal keeps the record where it was rather than closing
			// the grid or resetting it: a request the application does not
			// understand is one it did not happen, and a reader who never
			// asked for this should not lose the cell they were standing in.
			name:  "a plane refuses S2 and keeps what it had",
			attrs: plane, held: Grid{System: "geohash", Cell: "9q", Subgrid: 1},
			sent: "s2", want: Grid{System: "geohash", Cell: "9q", Subgrid: 1},
		},
		{
			name:  "a sphere with no flattening refuses it too",
			attrs: map[string]string{semconv.KeyGeometrySurface: semconv.SurfaceSphere},
			held:  Grid{}, sent: "s2", want: Grid{},
		},
		{
			name:  "a world nobody could stand up refuses it",
			attrs: nil, held: Grid{System: "geohash"}, sent: "s2",
			want: Grid{System: "geohash"},
		},
		{
			name:  "geohash divides anything, including a sphere",
			attrs: sphere, held: Grid{System: "s2", Cell: "47a1cb"},
			sent: "geohash", want: Grid{System: "geohash", Cell: "47a1cb"},
		},
		{
			name:  "a system nobody has written is not a system",
			attrs: sphere, held: Grid{System: "geohash"}, sent: "h3",
			want: Grid{System: "geohash"},
		},
		{
			// Slugs are the registry's spelling and nothing else is a near
			// miss worth guessing at.
			name:  "and neither is one spelled differently",
			attrs: sphere, held: Grid{System: "geohash"}, sent: "S2",
			want: Grid{System: "geohash"},
		},
		{
			name:  "an empty value is the grid closing, which is not a system at all",
			attrs: sphere, held: Grid{System: "s2", Cell: "47a1cb", Subgrid: 1},
			sent: "", want: Grid{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := Session{Grid: test.held}
			held := &concernContext{session: &session, world: &worldModel{Attrs: test.attrs}}
			form := formValues{values: map[string][]string{"system": {test.sent}}}
			if err := applyGrid(held, form); err != nil {
				t.Fatalf("applyGrid: %v", err)
			}
			if session.Grid != test.want {
				t.Errorf("grid = %+v, want %+v", session.Grid, test.want)
			}
		})
	}
}

// TestApplyGridStillOpensAndCyclesOnAnyWorld guards the two moves the refusal
// sits beside: neither is a destination, and both land on a system that
// divides anything, so no world can refuse them.
func TestApplyGridStillOpensAndCyclesOnAnyWorld(t *testing.T) {
	for _, attrs := range []map[string]string{plane, sphere, nil} {
		session := Session{}
		held := &concernContext{session: &session, world: &worldModel{Attrs: attrs}}
		if err := applyGrid(held, formValues{values: map[string][]string{"system": {"toggle"}}}); err != nil {
			t.Fatalf("applyGrid: %v", err)
		}
		if session.Grid.System != defaultCellSystem {
			t.Errorf("toggling a grid open on %v opened %q", attrs, session.Grid.System)
		}
		if err := applyGrid(held, formValues{values: map[string][]string{"system": {"cycle"}}}); err != nil {
			t.Fatalf("applyGrid: %v", err)
		}
		if session.Grid.System != defaultCellSystem {
			t.Errorf("cycling on %v landed on %q", attrs, session.Grid.System)
		}
	}
}

// TestHeldCellNarrowsForEverySystemTheSessionCanHold is the invariant the
// defect broke: whatever the session holds, the server narrows the same set
// the seam draws, or the count above the panel is a count of something else.
func TestHeldCellNarrowsForEverySystemTheSessionCanHold(t *testing.T) {
	// A whole-body world: the raster is the sphere, so the lens's surface and
	// the world square are the same 8192 x 4096.
	world := &worldModel{Attrs: sphere, Grid: tileGrid{Size: 8192}}
	lens := &payloadLens{Surface: &cells.Rect{X: 0, Y: 0, Width: 8192, Height: 4096}}

	// Two probes. The middle of the raster is lat 0, lng 0 on the body --
	// S2 cell 100001 at the telescope's floor, and the corner where four
	// geohash quarters meet, which every one of them holds. The other is an
	// ordinary point well inside one quarter.
	middle := [2]float64{4096, -2048}
	inland := [2]float64{5000, -1000}

	tests := []struct {
		name   string
		grid   Grid
		world  *worldModel
		at     [2]float64
		narrow bool // a predicate is expected at all
		inside bool // and this is what it says about the probe
	}{
		{"no grid narrows nothing", Grid{}, world, inland, false, false},
		{"an open grid with no cell narrows nothing", Grid{System: "s2"}, world, middle, false, false},
		{"a geohash cell holds its own quarter", Grid{System: "geohash", Cell: "u"}, world, inland, true, true},
		{"and not the quarter beside it", Grid{System: "geohash", Cell: "s"}, world, inland, true, false},
		{"the corner belongs to every quarter that touches it",
			Grid{System: "geohash", Cell: "7"}, world, middle, true, true},
		{"an S2 cell holds the point it was found from",
			Grid{System: "s2", Cell: "100001"}, world, middle, true, true},
		{"and not a point a level-10 cell away", Grid{System: "s2", Cell: "100001"}, world, inland, true, false},
		{"a face on the other side of the body holds neither",
			Grid{System: "s2", Cell: "9"}, world, middle, true, false},
		{"a system this world does not divide narrows nothing",
			Grid{System: "s2", Cell: "100001"},
			&worldModel{Attrs: plane, Grid: tileGrid{Size: 8192}}, middle, false, false},
		{"and neither does one nobody has written",
			Grid{System: "h3", Cell: "8928308280fffff"}, world, middle, false, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cell := heldCell(Session{Grid: test.grid}, test.world, lens)
			if (cell != nil) != test.narrow {
				t.Fatalf("narrowing = %v, want %v", cell != nil, test.narrow)
			}
			if cell == nil {
				return
			}
			if got := cell(test.at[0], test.at[1]); got != test.inside {
				t.Errorf("holds (%g, %g) = %v, want %v", test.at[0], test.at[1], got, test.inside)
			}
		})
	}
}
