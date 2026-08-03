// Package cells_test holds the server's cell arithmetic to the shared
// contract vectors -- the same language-neutral fixtures the TypeScript lane
// answers in analysis/test/vectors.test.ts.
//
// It lives here, outside internal/app, because it reads files and the hostenv
// rule says nothing under internal/app may know what a filesystem is. The
// arithmetic on trial is internal/app/cells; this package is only the reader
// standing between it and the fixtures.
//
// This is what makes a second implementation of a cell system tolerable. The
// TypeScript one at analysis/cellsystems owns the contract; the Go copy
// exists so the server can answer the held-cell question without asking the
// seam, and the two agree because both are held to
// analysis/testdata/cells/*.json rather than to each other's company. The
// hand-derived cases there are marked as such -- the geohash numbers halved
// out on paper, the S2 ones the Go library's own test values -- and the
// corpus grounds are read back against the shipped bundles below, so neither
// lane's descriptor can drift from the declaration a reader really meets.
package cells_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// ground is one entry of grounds.json: what the seam's `Ground` descriptor
// carries, and the two derived facts every consumer re-checks. `lens` is null
// when the application had no lens open, and its two fields are independently
// nullable, which is the whole of the surface ladder's input.
type ground struct {
	Key   string  `json:"key"`
	Size  float64 `json:"tileGridSize"`
	Lens  *lensOf `json:"lens"`
	World struct {
		Attrs map[string]string `json:"attrs"`
	} `json:"world"`
	Provenance *struct {
		Volume    string `json:"volume"`
		World     string `json:"world"`
		Lens      string `json:"lens"`
		LensIndex int    `json:"lensIndex"`
	} `json:"provenance"`
	Surface [4]float64 `json:"surfaceExtent"`
	Systems []string   `json:"systems"`
}

type lensOf struct {
	Surface *cells.Rect `json:"surface"`
	Bounds  *cells.Rect `json:"bounds"`
}

// extent is the ground every system divides, through the Go ladder.
func (g ground) extent() cells.Extent {
	var surface, bounds *cells.Rect
	if g.Lens != nil {
		surface, bounds = g.Lens.Surface, g.Lens.Bounds
	}
	return cells.SurfaceExtent(surface, bounds, g.Size)
}

// geohashExtent is the ground GEOHASH divides -- the world square on a plane
// with no earth anchoring, the surface ladder everywhere else (Part 1 of the
// plane ruling; internal/app/cells, GeohashGround).
func (g ground) geohashExtent() cells.Extent {
	var surface, bounds *cells.Rect
	if g.Lens != nil {
		surface, bounds = g.Lens.Surface, g.Lens.Bounds
	}
	return cells.GeohashGround(surface, bounds, g.Size, g.World.Attrs)
}

// TestGroundsAgreeOnSurfaceAndSystems holds the two derived fields every
// recorded ground carries: the rectangle it hands its systems, and which
// systems are willing to take it. The second is the answer the session's
// validation is made of, so a divergence here is a request the server would
// accept and the navigator would never have offered, or the reverse.
func TestGroundsAgreeOnSurfaceAndSystems(t *testing.T) {
	grounds := loadGrounds(t)
	keys := make([]string, 0, len(grounds))
	for key := range grounds {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{
		"bend-or/2026-08-02/0",
		"mars/global/0",
		"test/earth-8192x4096",
		"test/sphere-8192x4096",
		"test/square-1024",
		"test/square-1024-bounds",
		"test/square-1024-no-lens",
	}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("the fixture records grounds %v, want %v", keys, want)
	}
	for key, at := range grounds {
		got := at.extent()
		if !nearAll(got, at.Surface) {
			t.Errorf("%s: surface extent = [%g %g %g %g], recorded %v",
				key, got.MinX, got.MinY, got.MaxX, got.MaxY, at.Surface)
		}
		systems := cells.ApplicableSystems(at.World.Attrs)
		if fmt.Sprint(systems) != fmt.Sprint(at.Systems) {
			t.Errorf("%s: systems = %v, recorded %v", key, systems, at.Systems)
		}
	}
	t.Logf("%d grounds reproduced", len(grounds))
}

// TestCorpusGroundsMatchTheShippedBundles ties the recorded descriptors back
// to the public corpus: a ground naming a bundle in its provenance must carry
// exactly the declaration that bundle ships, read from the payload rather
// than trusted. This is the fixture's anchor now that the recorded tours are
// gone -- a descriptor edited to make a test pass would stop matching a file
// nobody edits by hand.
func TestCorpusGroundsMatchTheShippedBundles(t *testing.T) {
	corpus := filepath.Join("..", "..", "testdata", "corpus", "bundles")
	tied := 0
	for key, at := range loadGrounds(t) {
		if at.Provenance == nil {
			continue
		}
		from := at.Provenance
		var payload struct {
			Attrs  map[string]string `json:"attrs"`
			Lenses []struct {
				Name    string      `json:"name"`
				Surface *cells.Rect `json:"surface"`
				Bounds  *cells.Rect `json:"bounds"`
			} `json:"lenses"`
		}
		readInto(t, filepath.Join(corpus, from.Volume, "worlds", from.World+".payload.json"), &payload)
		if fmt.Sprint(payload.Attrs) != fmt.Sprint(at.World.Attrs) {
			t.Errorf("%s: the recorded attrs %v are not the bundle's %v",
				key, at.World.Attrs, payload.Attrs)
		}
		if from.LensIndex >= len(payload.Lenses) {
			t.Fatalf("%s: the bundle has no lens %d", key, from.LensIndex)
		}
		lens := payload.Lenses[from.LensIndex]
		if lens.Name != from.Lens {
			t.Errorf("%s: lens %d is %q, the ground says %q", key, from.LensIndex, lens.Name, from.Lens)
		}
		if at.Lens == nil ||
			fmt.Sprint(lens.Surface) != fmt.Sprint(at.Lens.Surface) ||
			fmt.Sprint(lens.Bounds) != fmt.Sprint(at.Lens.Bounds) {
			t.Errorf("%s: the recorded lens does not match the bundle's", key)
		}
		var volume struct {
			TileGrid struct {
				Size float64 `json:"size"`
			} `json:"tileGrid"`
		}
		readInto(t, filepath.Join(corpus, from.Volume, "volume.json"), &volume)
		if volume.TileGrid.Size != at.Size {
			t.Errorf("%s: tileGridSize %g, the bundle declares %g", key, at.Size, volume.TileGrid.Size)
		}
		tied++
	}
	if tied < 2 {
		t.Fatalf("only %d grounds are tied to the corpus; bend-or and mars both should be", tied)
	}
}

// TestVectorsHoldTheGoSide walks every case of every family the server has an
// answer for, and reports what it skipped.
func TestVectorsHoldTheGoSide(t *testing.T) {
	grounds := loadGrounds(t)
	checked, skipped := map[string]int{}, map[string]int{}

	for _, family := range []string{"surface", "containment", "identity", "hierarchy", "input", "carry"} {
		for _, held := range loadCases(t, family) {
			at, known := grounds[held.Ground]
			if !known {
				t.Fatalf("%s/%s: names a ground the fixture does not record: %q",
					family, held.Name, held.Ground)
			}
			got, answered := answer(t, at, held)
			if !answered {
				skipped[callOf(held)]++
				continue
			}
			if want := string(held.Expect); !sameJSON(t, got, want) {
				t.Errorf("%s/%s: %s%s = %s, the vector records %s",
					family, held.Name, callOf(held), string(held.Args), toJSON(t, got), want)
			}
			checked[callOf(held)]++
		}
	}

	// The floor is not a target. It is there so that a dispatch that quietly
	// stopped recognizing a call -- a renamed method, a family that moved --
	// fails instead of passing with nothing to say.
	if total(checked) < 80 || checked["s2.contains"] < 10 || checked["s2.descendTarget"] < 5 {
		t.Fatalf("only %d vectors were answered (%d s2.contains, %d s2.descendTarget); the dispatch has come loose",
			total(checked), checked["s2.contains"], checked["s2.descendTarget"])
	}
	t.Logf("%d vectors reproduced", total(checked))
	for _, line := range tally(checked) {
		t.Logf("  held: %s", line)
	}
	t.Logf("%d skipped, every one of them a seam concern", total(skipped))
	for _, line := range tally(skipped) {
		t.Logf("  seam: %s", line)
	}
}

// TestGeohashHalvingUndoesItself pins the arithmetic fact the grid stands
// on. The halving forward (an address to its
// rectangle) and the halving backward (a point to its address) are one walk,
// and undoing the halving character by character lands exactly on the ground
// -- every division is a halving of a finite float, so the inverse is a
// doubling and the comparison is exact, no epsilon.
func TestGeohashHalvingUndoesItself(t *testing.T) {
	grounds := loadGrounds(t)
	checked := 0
	for key, at := range grounds {
		surface := at.extent()
		for _, hash := range sampleHashes() {
			held := cells.GeohashExtent(surface, hash)
			// The centre of the cell, asked back at the same depth, spells
			// the same address.
			centre := [2]float64{(held.MinX + held.MaxX) / 2, (held.MinY + held.MaxY) / 2}
			if got := cells.GeohashCellAt(surface, centre[0], centre[1], len(hash)); got != hash {
				t.Fatalf("%s: the centre of %q addresses to %q", key, hash, got)
			}
			// And undoing the halvings recovers the ground, exactly.
			if got := grow(held, hash); got != surface {
				t.Fatalf("%s: undoing %q gives %+v, the ground is %+v", key, hash, got, surface)
			}
			checked++
		}
	}
	t.Logf("%d cells halved and un-halved across %d grounds", checked, len(grounds))
}

// sampleHashes is a deterministic spread of addresses: every depth-1 cell,
// and a diagonal of deeper ones so every character of the alphabet appears at
// every position at least once.
func sampleHashes() []string {
	alphabet := cells.GeohashAlphabet
	out := make([]string, 0, 3*len(alphabet))
	for at := 0; at < len(alphabet); at++ {
		first := alphabet[at]
		second := alphabet[(at+7)%len(alphabet)]
		third := alphabet[(at+19)%len(alphabet)]
		out = append(out,
			string(first),
			string([]byte{first, second}),
			string([]byte{first, second, third}),
		)
	}
	return out
}

// grow finds the ground whose halving by `hash` is `held`, by undoing the two
// candidate directions of each halving. It is exact: every halving is a
// division by two of a finite float, so the inverse is a doubling.
func grow(held cells.Extent, hash string) cells.Extent {
	// One character, five halvings: undo them in reverse, choosing the side
	// the character's bits name.
	out := held
	for i := len(hash) - 1; i >= 0; i-- {
		value := strings.IndexByte(cells.GeohashAlphabet, hash[i])
		if value < 0 {
			continue
		}
		splitX := (i*5)%2 == 0
		for m := 4; m >= 0; m-- {
			// Walk the masks backwards, alternating from where this
			// character's last halving left off.
			axisX := splitX == (m%2 == 0)
			width := out.MaxX - out.MinX
			height := out.MaxY - out.MinY
			if axisX {
				if value&cells.Masks[m] != 0 {
					out.MinX -= width
				} else {
					out.MaxX += width
				}
			} else {
				if value&cells.Masks[m] != 0 {
					out.MinY -= height
				} else {
					out.MaxY += height
				}
			}
		}
	}
	return out
}

func callOf(held vector) string {
	if held.System == "" {
		return held.Call
	}
	return held.System + "." + held.Call
}

func total(counts map[string]int) int {
	sum := 0
	for _, count := range counts {
		sum += count
	}
	return sum
}

func tally(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for call, count := range counts {
		out = append(out, fmt.Sprintf("%3d  %s", count, call))
	}
	sort.Strings(out)
	return out
}

// vector is one recorded call. The arguments are heterogeneous by family, so
// they stay raw until the call is known.
type vector struct {
	Name   string          `json:"name"`
	Ground string          `json:"ground"`
	System string          `json:"system"`
	Call   string          `json:"call"`
	Args   json.RawMessage `json:"args"`
	Expect json.RawMessage `json:"expect"`
	Note   string          `json:"note"`
}

// answer is the whole dispatch: the calls the server implements, and nothing
// else. The false result means "this call is the seam's", which is a skip
// rather than a failure -- the plan, the ring, the pole closure and the style
// tokens are not arithmetic the application has any use for.
func answer(t *testing.T, at ground, held vector) (any, bool) {
	t.Helper()
	switch held.Call {
	case "surfaceExtent":
		got := at.extent()
		return [4]float64{got.MinX, got.MinY, got.MaxX, got.MaxY}, true
	case "applicableSystems":
		return cells.ApplicableSystems(at.World.Attrs), true
	case "appliesTo":
		return cells.Applicable(at.World.Attrs, held.System), true
	case "geohashCellAt":
		var point [2]float64
		var depth int
		args(t, held, &point, &depth)
		if mapping, anchored := cells.EarthMercator(at.World.Attrs); anchored {
			return cells.GeohashRealCellAt(mapping, point[0], point[1], depth), true
		}
		return cells.GeohashCellAt(at.geohashExtent(), point[0], point[1], depth), true
	case "equivalentCell":
		// The carry is server arithmetic now: cycling the grid system moves
		// the held cell through cells.Equivalent, so the recorded pairs bind
		// the Go side the same way they bind the seam's.
		var from, to, id string
		args(t, held, &from, &to, &id)
		return cells.Equivalent(at.extent(), at.World.Attrs, from, to, id), true
	}

	// The navigator's field length is a server fact -- grid.go answers it to
	// the template -- so the vector binds the Go side too, per system.
	if held.Call == "inputLength" {
		switch held.System {
		case cells.SystemGeohash:
			return cells.GeohashDepthOf(at.World.Attrs), true
		case cells.SystemS2:
			return cells.S2InputLength, true
		}
	}

	switch held.System {
	case cells.SystemGeohash:
		mapping, anchored := cells.EarthMercator(at.World.Attrs)
		switch held.Call {
		case "contains":
			var hash string
			var point [2]float64
			args(t, held, &hash, &point)
			if anchored {
				return cells.GeohashRealHeld(mapping, hash)(point[0], point[1]), true
			}
			return cells.GeohashHeld(at.geohashExtent(), hash)(point[0], point[1]), true
		case "descendTarget":
			var hash string
			var point [2]float64
			args(t, held, &hash, &point)
			if anchored {
				depth := cells.GeohashBaseDepth(mapping)
				if hash != "" {
					depth = len(hash) + 1
				}
				return cells.GeohashRealCellAt(mapping, point[0], point[1], depth), true
			}
			return cells.GeohashCellAt(at.geohashExtent(), point[0], point[1], len(hash)+1), true
		}
	case cells.SystemS2:
		mapping, mapped := cells.MappingOf(at.World.Attrs)
		if !mapped {
			t.Fatalf("%s: an S2 case on a ground with no flattening", held.Name)
		}
		switch held.Call {
		case "contains":
			var token string
			var point [2]float64
			args(t, held, &token, &point)
			return cells.S2Held(mapping, token)(point[0], point[1]), true
		case "descendTarget":
			var token string
			var point [2]float64
			args(t, held, &token, &point)
			return cells.S2Descend(mapping, token, point[0], point[1]), true
		case "locate":
			var point [2]float64
			args(t, held, &point)
			cell := cells.S2CellAt(mapping, point[0], point[1])
			if cell == "" {
				return nil, true
			}
			return map[string]string{"label": "S2", "value": cell}, true
		case "level":
			var token string
			args(t, held, &token)
			return cells.S2Level(token), true
		case "parent":
			var token string
			args(t, held, &token)
			return cells.S2Parent(token), true
		case "children":
			var token string
			args(t, held, &token)
			return cells.S2Children(token), true
		case "childIndex":
			// The sibling ordinal is not a function the server has; it is a
			// position in the child list, and asking for it that way holds
			// the list's *order* as well as its members.
			var token string
			args(t, held, &token)
			for at, sibling := range cells.S2Children(cells.S2Parent(token)) {
				if sibling == token {
					return at, true
				}
			}
			t.Errorf("%s: %q is not among its parent's children", held.Name, token)
			return -1, true
		case "parseInput":
			var text string
			args(t, held, &text)
			cell, ok := cells.S2ParseCell(text)
			if !ok {
				return nil, true
			}
			return cell, true
		}
	}
	return nil, false
}

// args decodes the recorded argument list positionally into the pointers
// given. A case whose shape does not match the call it names is a broken
// fixture, and says so rather than passing.
func args(t *testing.T, held vector, into ...any) {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal(held.Args, &raw); err != nil {
		t.Fatalf("%s: args are not a list: %v", held.Name, err)
	}
	if len(raw) != len(into) {
		t.Fatalf("%s: %s takes %d arguments, the vector records %d",
			held.Name, held.Call, len(into), len(raw))
	}
	for at, target := range into {
		if err := json.Unmarshal(raw[at], target); err != nil {
			t.Fatalf("%s: argument %d: %v", held.Name, at, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Reading the fixtures
// ---------------------------------------------------------------------------

const vectorsDir = "../../analysis/testdata/cells"

func loadGrounds(t *testing.T) map[string]ground {
	t.Helper()
	var file struct {
		Grounds map[string]ground `json:"grounds"`
	}
	readInto(t, filepath.Join(vectorsDir, "grounds.json"), &file)
	return file.Grounds
}

func loadCases(t *testing.T, family string) []vector {
	t.Helper()
	var file struct {
		Cases []vector `json:"cases"`
	}
	readInto(t, filepath.Join(vectorsDir, family+".json"), &file)
	if len(file.Cases) == 0 {
		t.Fatalf("%s.json records no cases", family)
	}
	return file.Cases
}

func readInto(t *testing.T, path string, into any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared vectors are the oracle and could not be read: %v", err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// sameJSON compares an answer with a recorded expectation through JSON, which
// is the form the vectors are written in and the only form both lanes share.
func sameJSON(t *testing.T, got any, want string) bool {
	t.Helper()
	var mine, theirs any
	if err := json.Unmarshal([]byte(toJSON(t, got)), &mine); err != nil {
		t.Fatalf("answer is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &theirs); err != nil {
		t.Fatalf("expectation is not JSON: %v", err)
	}
	return equalJSON(mine, theirs)
}

func toJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("answer will not marshal: %v", err)
	}
	return string(body)
}

// equalJSON compares decoded JSON with a tolerance on numbers: a recorded
// coordinate is a decimal literal and an answer is a float, and the two agree
// to further places than any cell edge cares about.
func equalJSON(mine, theirs any) bool {
	switch left := mine.(type) {
	case nil:
		return theirs == nil
	case bool:
		right, ok := theirs.(bool)
		return ok && left == right
	case float64:
		right, ok := theirs.(float64)
		return ok && math.Abs(left-right) < 1e-6
	case string:
		right, ok := theirs.(string)
		return ok && left == right
	case []any:
		right, ok := theirs.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for at := range left {
			if !equalJSON(left[at], right[at]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := theirs.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, held := right[key]
			if !held || !equalJSON(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

func nearAll(got cells.Extent, want [4]float64) bool {
	return near(got.MinX, want[0]) && near(got.MinY, want[1]) &&
		near(got.MaxX, want[2]) && near(got.MaxY, want[3])
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
