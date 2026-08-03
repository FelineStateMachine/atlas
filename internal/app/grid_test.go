package app

import (
	"slices"
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
// attributes, which tests/cells reads back against the shipped bundle itself.
var sphere = map[string]string{
	semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
	semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
	semconv.KeyGeometryEquirectPx:  "0,0,8192,4096",
	semconv.KeyGeometryEquirectDeg: "-180,90,180,-90",
}

// plane is a world that says nothing about what its picture is of, which is
// every game map: geohash divides it and nothing else offers to.
var plane = map[string]string{semconv.KeyIconOutset: semconv.OutsetDark}

// worldOn is a world standing on the ground the carry vectors are read on:
// the top half of an 8192 world square, declared whole as the lens's
// surface (`test/sphere-8192x4096` in analysis/testdata/cells/grounds.json).
// Every question about a *place* -- as against a system's name -- needs one,
// because a hierarchy divides a rectangle and a world with no rectangle
// divides nothing.
func worldOn(attrs map[string]string) *worldModel {
	return &worldModel{
		Attrs:  attrs,
		Grid:   tileGrid{Size: 8192},
		Lenses: []payloadLens{{Name: "Surface", Surface: &cells.Rect{Width: 8192, Height: 4096}}},
	}
}

func TestApplyGridRefusesASystemTheWorldDoesNotDivide(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		held  Grid
		sent  string
		want  Grid
	}{
		{
			// The address does not survive and the place does: the two
			// hierarchies share no boundaries, so what crosses is the ground
			// the reader was looking at, at the depth they were looking at it
			// (internal/app/cells, Equivalent -- held to the recorded carry
			// vectors by TestCyclingCarriesTheHeldPlace below).
			name:  "a sphere takes S2, carrying the held place across",
			attrs: sphere, held: Grid{System: "geohash", Cell: "9q"},
			sent: "s2", want: Grid{System: "s2", Cell: "80b"},
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
			sent: "geohash", want: Grid{System: "geohash", Cell: "u2b"},
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
			held := &concernContext{session: &session, world: worldOn(test.attrs)}
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
// sits beside: neither is a destination, so neither can be refused. Opening
// always lands on the system that divides anything; cycling always lands on a
// system this world offers, which on most worlds is the one it started on.
func TestApplyGridStillOpensAndCyclesOnAnyWorld(t *testing.T) {
	for _, attrs := range []map[string]string{plane, sphere, nil} {
		session := Session{}
		held := &concernContext{session: &session, world: worldOn(attrs)}
		if err := applyGrid(held, formValues{values: map[string][]string{"system": {"toggle"}}}); err != nil {
			t.Fatalf("applyGrid: %v", err)
		}
		if session.Grid.System != defaultCellSystem {
			t.Errorf("toggling a grid open on %v opened %q", attrs, session.Grid.System)
		}
		offered := cells.ApplicableSystems(attrs)
		if err := applyGrid(held, formValues{values: map[string][]string{"system": {"cycle"}}}); err != nil {
			t.Fatalf("applyGrid: %v", err)
		}
		if !slices.Contains(offered, session.Grid.System) {
			t.Errorf("cycling on %v landed on %q, which it does not offer %v",
				attrs, session.Grid.System, offered)
		}
		// A world with one system has nowhere to step; a world with two steps.
		want := defaultCellSystem
		if len(offered) > 1 {
			want = offered[1]
		}
		if session.Grid.System != want {
			t.Errorf("cycling on %v landed on %q, want %q", attrs, session.Grid.System, want)
		}
	}
}

// TestCyclingIsANoOpWithNoGridOpen: the button that would cycle is not on the
// page when there is no grid, and a request arriving anyway must not open one.
// ⌘G in the reference changed which system *would* divide the map and left the
// map alone; a record that spells "no grid" as "no system" has nothing to
// remember, so it does nothing instead of opening something nobody asked for.
func TestCyclingIsANoOpWithNoGridOpen(t *testing.T) {
	session := Session{Grid: Grid{Subgrid: 1}}
	held := &concernContext{session: &session, world: worldOn(sphere)}
	if err := applyGrid(held, formValues{values: map[string][]string{"system": {"cycle"}}}); err != nil {
		t.Fatalf("applyGrid: %v", err)
	}
	if (session.Grid != Grid{Subgrid: 1}) {
		t.Errorf("cycling with no grid open wrote %+v", session.Grid)
	}
}

// TestCyclingCarriesTheHeldPlace holds the Go carry to the shared
// `equivalentCell` vectors -- the same language-neutral oracle the TypeScript
// lane answers to (analysis/testdata/cells/carry.json, family "carry", read
// on the ground `test/sphere-8192x4096`). These five are the carry seen from
// inside the application lane, where the answer lands in a session.
//
// The table is transcribed rather than read, because the application lane may
// not open a file (the hostenv rule, held by tools/depcheck) and a test file
// is still the application lane. The durable *reading* of this family is
// tests/cells/conformance_test.go, which loads carry.json and answers it
// through cells.Equivalent; the case names below are that file's own keys, so
// the two can be diffed by eye.
func TestCyclingCarriesTheHeldPlace(t *testing.T) {
	vectors := []struct {
		name     string
		from, to string
		id, want string
	}{
		// The root carries to the root, which is the only address the two
		// hierarchies spell the same way.
		{"root-carries-to-root", "geohash", "s2", "", ""},
		// A level-2 geohash cell is 256x128 of the 8192x4096 world; the S2
		// cell holding its centre at the nearest area sits within a level
		// either way.
		{"geohash-m6-to-s2", "geohash", "s2", "m6", "213"},
		{"and-back-to-geohash", "s2", "geohash", "213", "m6"},
		{"geohash-m-to-s2", "geohash", "s2", "m", "24"},
		// A polar face carried onto a plane's halving: the face's centre is
		// the north pole itself.
		{"s2-face-to-geohash", "s2", "geohash", "5", "b"},
	}

	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			session := Session{Grid: Grid{System: vector.from, Cell: vector.id, Subgrid: 1}}
			held := &concernContext{session: &session, world: worldOn(sphere)}
			setGridSystem(held, vector.to)
			if session.Grid.System != vector.to || session.Grid.Cell != vector.want {
				t.Errorf("carrying %q from %s to %s = %+v, the vector records %q",
					vector.id, vector.from, vector.to, session.Grid, vector.want)
			}
		})
	}
}

// The two containment cross-checks recorded beside the crossings: the carried
// cell holds the ground the old one was standing on, which is the whole of
// what "the same place at like precision" means.
func TestTheCarriedCellHoldsTheOldCentre(t *testing.T) {
	world := worldOn(sphere)
	ground := gridGround(&concernContext{session: &Session{}, world: world})
	mapping, mapped := cells.MappingOf(sphere)
	if !mapped {
		t.Fatal("the sphere declares no flattening")
	}
	// carried-cell-holds-the-old-centre: s2 "213" contains m6's centre.
	if !cells.S2Held(mapping, "213")(5504, -2752) {
		t.Error("the S2 cell m6 carried to does not hold m6's centre")
	}
	// round-trip-holds-the-s2-centre: geohash "m6" contains 213's centre.
	if !cells.GeohashHeld(ground, "m6")(5560.028045868422, -2708.7329376470884) {
		t.Error("the geohash cell 213 carried back to does not hold 213's centre")
	}
}

// TestCyclingBackAndForthComesHome is the property the vectors record two
// halves of: a place carried across and back names the same ground again. It
// is not an identity in general -- the two hierarchies are not nested, and a
// cell near a boundary can come home one neighbour over -- which is why this
// asks for the ground rather than for the spelling: the carried cell holds
// the centre it was carried from, both ways.
func TestCyclingBackAndForthComesHome(t *testing.T) {
	world := worldOn(sphere)
	ground := gridGround(&concernContext{session: &Session{}, world: world})
	mapping, mapped := cells.MappingOf(sphere)
	if !mapped {
		t.Fatal("the sphere declares no flattening")
	}
	for _, hash := range []string{"m", "m6", "u2b", "9q"} {
		t.Run(hash, func(t *testing.T) {
			token := cells.Equivalent(ground, sphere, "geohash", "s2", hash)
			if token == "" {
				t.Fatalf("%q carried to nothing", hash)
			}
			extent := cells.GeohashExtent(ground, hash)
			x, y := (extent.MinX+extent.MaxX)/2, (extent.MinY+extent.MaxY)/2
			if !cells.S2Held(mapping, token)(x, y) {
				t.Errorf("%q carried to %q, which does not hold its centre", hash, token)
			}
			back := cells.Equivalent(ground, sphere, "s2", "geohash", token)
			cx, cy := cells.S2Center(mapping, ground, token)
			if !cells.GeohashHeld(ground, back)(cx, cy) {
				t.Errorf("%q carried back to %q, which does not hold %q's centre", token, back, token)
			}
		})
	}
}

// TestNormalizingTheNavigatorsField is the input contract, per system: what
// the field keeps of a keystroke, and when the draft has become a place.
//
// It is the half of the reference that used to live in the browser. Every
// submission is a navigation for geohash -- the empty string is the root and
// every prefix is an address -- and a whole-or-nothing token for S2, where a
// partial spelling leaves the reader exactly where they were.
func TestNormalizingTheNavigatorsField(t *testing.T) {
	tests := []struct {
		name string
		held Grid
		sent string
		want Grid
	}{
		{"a hash arrives lowercased and stripped of what it cannot spell",
			Grid{System: "geohash"}, "M6X!", Grid{System: "geohash", Cell: "m6x"}},
		{"the letters the alphabet leaves out are not letters",
			Grid{System: "geohash"}, "aM6iLo", Grid{System: "geohash", Cell: "m6"}},
		{"and a hash past the floor is cut at it",
			Grid{System: "geohash"}, "m6w9", Grid{System: "geohash", Cell: "m6w"}},
		{"the root is a place, so an empty field is the whole ground",
			Grid{System: "geohash", Cell: "m6"}, "", Grid{System: "geohash"}},
		{"a token arrives lowercased and hex-only",
			Grid{System: "s2"}, "47A1 CB!", Grid{System: "s2", Cell: "47a1cb"}},
		{"the root is a place in every system",
			Grid{System: "s2", Cell: "47a1cb"}, "", Grid{System: "s2"}},
		{"half a token is a draft, and a draft moves nobody",
			Grid{System: "s2", Cell: "47a1cb"}, "4", Grid{System: "s2", Cell: "47a1cb"}},
		{"and so is a whole one that is not an address",
			Grid{System: "s2", Cell: "47a1cb"}, "47a1cbd5955ff", Grid{System: "s2", Cell: "47a1cb"}},
		// Text with nothing of the system's alphabet in it normalizes to
		// nothing, and nothing is the root -- which is the field emptying
		// itself in front of the reader and the map opening out, exactly as
		// the reference's `input` handler did it.
		{"text a system cannot spell at all empties the field to the root",
			Grid{System: "s2", Cell: "47a1cb"}, "xyz", Grid{System: "s2"}},
		{"a token past the floor clamps to it and comes back respelled",
			Grid{System: "s2"}, "47a1cbd595522b39", Grid{System: "s2", Cell: "47a1cb"}},
		// The field takes six characters, so the seventeen-character leaf
		// above arrives cut to six -- which is the same cell, spelled the way
		// the navigator would have spelled it.
		{"and one typed past the field's own length is cut first",
			Grid{System: "s2"}, "47a1cbd5", Grid{System: "s2", Cell: "47a1cb"}},
		{"a cell arriving at a closed grid opens it on the system that divides anything",
			Grid{Subgrid: 1}, "m6", Grid{System: "geohash", Cell: "m6", Subgrid: 1}},
		// Opening the grid with the G key starts the subdivision over; going
		// somewhere does not. A reader who put the mesh away and then picked a
		// cell off the map asked for the cell, not for the mesh back.
		{"and does not bring the subdivision back with it",
			Grid{}, "m6", Grid{System: "geohash", Cell: "m6"}},
		// There is no such thing as a draft at a closed grid: the system a
		// closed grid opens on is the one that never refuses a spelling.
		{"and whatever arrives at a closed grid is a geohash",
			Grid{}, "47a1cb", Grid{System: "geohash", Cell: "471"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := Session{Grid: test.held}
			held := &concernContext{session: &session, world: worldOn(sphere)}
			form := formValues{values: map[string][]string{"cell": {test.sent}}}
			if err := applyGrid(held, form); err != nil {
				t.Fatalf("applyGrid: %v", err)
			}
			if session.Grid != test.want {
				t.Errorf("grid = %+v, want %+v", session.Grid, test.want)
			}
		})
	}
}

// TestAscendingTelescopesOutOfTheHeldSystem is the regression the S2 side was
// written for: Escape steps one LEVEL out, and only the address's own system
// knows what that is. A geohash drops a character; an S2 token does not --
// `47a1cb` telescopes out to `47a1`, and shortening it by a character would
// have landed the reader in a cell they never asked for.
func TestAscendingTelescopesOutOfTheHeldSystem(t *testing.T) {
	tests := []struct {
		name string
		held Grid
		want Grid
	}{
		{"a geohash drops its last character", Grid{System: "geohash", Cell: "m6x", Subgrid: 1},
			Grid{System: "geohash", Cell: "m6", Subgrid: 1}},
		{"down to the root, which keeps the grid open",
			Grid{System: "geohash", Cell: "m", Subgrid: 1}, Grid{System: "geohash", Subgrid: 1}},
		// The shared hierarchy vector, read the other way round:
		// parent("47a1ca4") is "47a1cb" (analysis/testdata/cells/hierarchy.json).
		// A truncation would have answered "47a1ca", one of its own siblings'
		// neighbours.
		{"an S2 token telescopes out to a token, not to a prefix",
			Grid{System: "s2", Cell: "47a1ca4", Subgrid: 1}, Grid{System: "s2", Cell: "47a1cb", Subgrid: 1}},
		// And the level after that is the same length again: an S2 address is
		// a Hilbert index, so the character count says nothing about depth.
		{"and out again, to an address the same length",
			Grid{System: "s2", Cell: "47a1cb", Subgrid: 1}, Grid{System: "s2", Cell: "47a1cc", Subgrid: 1}},
		{"and out again, to a shorter one", Grid{System: "s2", Cell: "47a1cc", Subgrid: 1},
			Grid{System: "s2", Cell: "47a1d", Subgrid: 1}},
		{"a face telescopes out to the root", Grid{System: "s2", Cell: "5", Subgrid: 1},
			Grid{System: "s2", Subgrid: 1}},
		// The root is the last address; the press after it closes the grid,
		// and takes nothing but the grid with it.
		{"the root closes the grid", Grid{System: "geohash", Subgrid: 1}, Grid{Subgrid: 1}},
		{"in every system", Grid{System: "s2", Subgrid: 1}, Grid{Subgrid: 1}},
		{"and a reader who put the subdivision away keeps it away",
			Grid{System: "geohash"}, Grid{}},
		{"a grid nobody opened has nothing to telescope out of", Grid{Subgrid: 1}, Grid{Subgrid: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := Session{Grid: test.held}
			held := &concernContext{session: &session, world: worldOn(sphere)}
			// The back button posts the held cell alongside the press, the way
			// the navigator's markup does; telescoping out must win over it.
			form := formValues{values: map[string][]string{
				"ascend": {"1"}, "cell": {test.held.Cell},
			}}
			if err := applyGrid(held, form); err != nil {
				t.Fatalf("applyGrid: %v", err)
			}
			if session.Grid != test.want {
				t.Errorf("grid = %+v, want %+v", session.Grid, test.want)
			}
		})
	}
}

// TestOpeningTheGridDividesTheCellAgain: the G key starts the subdivision
// over, and closing the grid leaves the reader's own setting where it was.
func TestOpeningTheGridDividesTheCellAgain(t *testing.T) {
	session := Session{Grid: Grid{System: "geohash", Cell: "m6"}}
	held := &concernContext{session: &session, world: worldOn(plane)}
	toggle := formValues{values: map[string][]string{"system": {"toggle"}}}

	if err := applyGrid(held, toggle); err != nil {
		t.Fatalf("applyGrid: %v", err)
	}
	// Closing takes the cell with it -- a cell nobody can see is not a place
	// anybody is standing -- and leaves the subdivision hidden.
	if (session.Grid != Grid{}) {
		t.Fatalf("closing wrote %+v", session.Grid)
	}
	if err := applyGrid(held, toggle); err != nil {
		t.Fatalf("applyGrid: %v", err)
	}
	if want := (Grid{System: defaultCellSystem, Subgrid: 1}); session.Grid != want {
		t.Errorf("opening wrote %+v, want %+v", session.Grid, want)
	}
}

// TestTheCycleButtonIsOfferedOnlyWhereItLeadsSomewhere: a control that cycles
// between one thing is a control that does nothing, so a world offering one
// system does not carry it. The mark, the name and the next system are the
// registry's own spellings, because the seam draws the same words.
func TestTheCycleButtonIsOfferedOnlyWhereItLeadsSomewhere(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		grid  Grid
		want  GridView
	}{
		{"a game map divides one way and says so quietly", plane,
			Grid{System: "geohash", Subgrid: 1},
			GridView{Enabled: true, System: "geohash", Name: "Geohash", Mark: "G", Length: 3,
				Subgrid: true}},
		{"a planet divides two ways and offers the step", sphere,
			Grid{System: "geohash", Cell: "m6", Subgrid: 1},
			GridView{Enabled: true, System: "geohash", Name: "Geohash", Mark: "G", Cell: "m6",
				Length: 3, Subgrid: true, Cycles: true, Next: "S2"}},
		{"and the step wraps back", sphere,
			Grid{System: "s2", Cell: "47a1cb", Subgrid: 1},
			GridView{Enabled: true, System: "s2", Name: "S2", Mark: "S2", Cell: "47a1cb",
				Length: 6, Subgrid: true, Cycles: true, Next: "Geohash"}},
		// A closed grid is a navigator nobody can see; there is no current
		// system to step from, so there is nothing for the button to say.
		{"a closed grid offers nothing at all", sphere, Grid{Subgrid: 1},
			GridView{Subgrid: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			world := worldOn(test.attrs)
			want := test.want
			want.Offered = true
			if got := gridView(Session{Grid: test.grid}, world); got != want {
				t.Errorf("grid view = %+v, want %+v", got, want)
			}
		})
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
