package cells_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// The Go copy of the geohash halving is held to the same oracle the
// TypeScript one is: the cell extents recorded in the parity baselines.
//
// This is the price of cells.go existing at all. A second copy of an
// arithmetic is a second thing to drift, so it is not enough that it looks
// right — every extent it produces has to be one the reference implementation
// actually drew, read out of the committed tours rather than out of a
// hand-written table. When a Go twin of the analysis lane lands, this test and
// the file it guards go together.
func TestGeohashExtentsMatchTheRecordedGrids(t *testing.T) {
	root := filepath.Join("..", "parity")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("no parity baselines to read: %v", err)
	}

	type cell struct {
		Hash   string     `json:"hash"`
		Extent [4]float64 `json:"extent"`
	}
	type snapshot struct {
		Grid struct {
			Enabled bool        `json:"enabled"`
			System  string      `json:"system"`
			Prefix  string      `json:"prefix"`
			Extent  *[4]float64 `json:"extent"`
			Cells   []cell      `json:"cells"`
		} `json:"grid"`
	}
	type step struct {
		Name     string   `json:"name"`
		Snapshot snapshot `json:"snapshot"`
	}
	type tour struct {
		Volume string `json:"volume"`
		Steps  []step `json:"steps"`
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "private" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, entry.Name(), "tour.json"))
		if err != nil {
			continue
		}
		var walk tour
		if err := json.Unmarshal(body, &walk); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		for _, held := range walk.Steps {
			grid := held.Snapshot.Grid
			if !grid.Enabled || grid.System != "geohash" || grid.Extent == nil {
				continue
			}
			// The recorded extent of the held cell is the ground every cell
			// below it was halved out of, so the walk is checkable from the
			// baseline alone: each drawn cell's address, halved out of the
			// *root* the same run recorded, must land on the same rectangle.
			// The root itself is the prefix's own extent.
			ground := rootOf(*grid.Extent, grid.Prefix)
			for _, drawn := range grid.Cells {
				want := drawn.Extent
				got := cells.GeohashExtent(ground, drawn.Hash)
				if !near(got.MinX, want[0]) || !near(got.MinY, want[1]) ||
					!near(got.MaxX, want[2]) || !near(got.MaxY, want[3]) {
					t.Fatalf("%s/%s: cell %q = [%g %g %g %g], the tour recorded %v",
						walk.Volume, held.Name, drawn.Hash,
						got.MinX, got.MinY, got.MaxX, got.MaxY, want)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Skip("no recorded geohash grids in the baselines")
	}
	t.Logf("%d recorded cells reproduced", checked)
}

// rootOf undoes the prefix: the recorded extent is the ground after the held
// address was halved out of it, and every cell in the plan is addressed from
// the whole ground.
func rootOf(held [4]float64, prefix string) cells.Extent {
	// An empty prefix is already the ground; anything else is undone whole,
	// character by character, by grow.
	return grow(cells.Extent{held[0], held[1], held[2], held[3]}, prefix)
}

// grow finds the ground whose halving by `prefix` is `held`, by search over
// the two candidate directions of each halving. It is exact: every halving is
// a division by two of a finite float, so the inverse is a doubling.
func grow(held cells.Extent, prefix string) cells.Extent {
	// One character, five halvings: undo them in reverse, choosing the side
	// the character's bits name.
	out := held
	for i := len(prefix) - 1; i >= 0; i-- {
		value := indexOf(prefix[i])
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

func indexOf(character byte) int {
	for i := 0; i < len(cells.GeohashAlphabet); i++ {
		if cells.GeohashAlphabet[i] == character {
			return i
		}
	}
	return -1
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
