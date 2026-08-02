package cells_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// The Go S2 side is held to the analysis vectors -- the same language-neutral
// oracle the TypeScript lane is held to, read out of the same files by the
// same rules (golden/analysis/README.md).
//
// This is what makes a second implementation of a cell system tolerable. The
// geohash test beside this one reproduces extents the reference implementation
// actually drew; S2 has no such recording in the parity baselines, because the
// tour never cycles to it, and the hand-derived vectors are what stands in
// their place. They are the Go library's own test values -- leaf
// 0x47a1cbd595522b39 and its ancestors -- read through a declared flattening,
// which is exactly the arithmetic the server now has to do for itself.
//
// The Go side answers a subset of the eighteen contract methods: containment,
// the address arithmetic around it, and the two questions about the ground.
// Everything a plan, a ring or a style token needs stays in the seam, and a
// case naming one of those methods is skipped by name rather than silently --
// the counts are logged, and a family that suddenly answers nothing is a
// difference a reader of the log can see.

// ground is one entry of vectors/grounds.json: what the seam's `Ground`
// descriptor carries, and the two derived facts the gate checks. `lens` is
// null when the application had no lens open, and its two fields are
// independently nullable, which is the whole of the surface ladder's input.
type ground struct {
	Key   string  `json:"key"`
	Size  float64 `json:"tileGridSize"`
	Lens  *lensOf `json:"lens"`
	World struct {
		Attrs map[string]string `json:"attrs"`
	} `json:"world"`
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

// TestGroundsAgreeOnSurfaceAndSystems holds the two derived fields every
// recorded ground carries: the rectangle it hands its systems, and which
// systems are willing to take it. The second is the answer the session's
// validation is made of, so a divergence here is a request the server would
// accept and the navigator would never have offered, or the reverse.
func TestGroundsAgreeOnSurfaceAndSystems(t *testing.T) {
	grounds := loadGrounds(t)
	if len(grounds) < 9 {
		t.Fatalf("read %d grounds, the fixture records nine", len(grounds))
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
	if total(checked) < 70 || checked["s2.contains"] < 10 {
		t.Fatalf("only %d vectors were answered (%d of them S2 containment); the dispatch has come loose",
			total(checked), checked["s2.contains"])
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
	}

	switch held.System {
	case cells.SystemGeohash:
		if held.Call == "contains" {
			var hash string
			var point [2]float64
			args(t, held, &hash, &point)
			return cells.GeohashHeld(at.extent(), hash)(point[0], point[1]), true
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

const vectorsDir = "../analysis/vectors"

func loadGrounds(t *testing.T) map[string]ground {
	t.Helper()
	var file struct {
		Grounds map[string]ground `json:"grounds"`
	}
	read(t, "grounds", &file)
	return file.Grounds
}

func loadCases(t *testing.T, family string) []vector {
	t.Helper()
	var file struct {
		Cases []vector `json:"cases"`
	}
	read(t, family, &file)
	if len(file.Cases) == 0 {
		t.Fatalf("%s.json records no cases", family)
	}
	return file.Cases
}

func read(t *testing.T, name string, into any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(vectorsDir, name+".json"))
	if err != nil {
		t.Fatalf("the analysis vectors are the oracle and could not be read: %v", err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s.json: %v", name, err)
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
