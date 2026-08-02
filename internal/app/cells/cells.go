// Package cells is the one piece of cell-system arithmetic the *server* needs.
package cells

import "strings"

// The held cell, as a rectangle of world pixels.
//
// A grid narrows what stands the same way a highlight does (docs/render-seam.md
// §4.2, rule 4), and the count above the panel beside the map has to be the
// count of what the map is drawing — which means the application has to be able
// to answer "is this feature inside the held cell" without asking anyone.
//
// WHY THIS IS HERE AND NOT IN THE ANALYSIS LANE. Cell systems belong to
// `analysis/cellsystems` (issue #5 §5.4) and that lane is TypeScript, consumed
// by the seam. The seam is deletable and the application is not, so a fact the
// *server* needs cannot live behind it: the alternative to this file is a
// third upward flow from the seam carrying the cell's extent, which
// docs/render-seam.md §5 forbids in as many words.
//
// It is nevertheless a second copy of one system's arithmetic, and that is a
// cost rather than a design. Two things hold it honest. The arithmetic is the
// smallest possible piece of it — the recursive halving and nothing else: no
// plan, no ring, no pole closure, no style token, none of which the server has
// any use for. And it is gated: `cells_test.go` reproduces the cell extents
// recorded in every parity baseline, which is the same oracle
// `analysis-vectors` holds the TypeScript to. When a Go twin of the analysis
// lane exists, this file is the first thing it deletes.

// GeohashAlphabet is the canonical order, which is also the child order. No
// `a`, `i`, `l` or `o`, so a hash read aloud cannot be misheard.
const GeohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// Masks are the five bits of one character, most significant first.
var Masks = [5]int{16, 8, 4, 2, 1}

// extent is a rectangle of world pixels: minimum x, minimum y, maximum x,
// maximum y, in the coordinates everything downstream of the projection
// speaks (y decreasing downward).
// Extent is a rectangle of world pixels.
type Extent struct{ MinX, MinY, MaxX, MaxY float64 }

// Holds answers whether a point is inside.
func (e Extent) Holds(x, y float64) bool {
	return x >= e.MinX && x <= e.MaxX && y >= e.MinY && y <= e.MaxY
}

// surfaceExtent is the ground a system divides: the lens's declared surface
// where it has one, its raster window otherwise, and the whole world square
// where it declares neither.
//
// The distinction is the split sheet's: `bounds` is the window the pyramid
// fills and `surface` is the ground that window pictures, which is smaller
// wherever the window was grown to take in a title drawn beside the map.
// Anything *dividing* the world measures the ground.
func SurfaceExtent(surface, bounds *Rect, size float64) Extent {
	if surface != nil {
		return Extent{surface.X, -(surface.Y + surface.Height), surface.X + surface.Width, -surface.Y}
	}
	if bounds != nil {
		return Extent{bounds.X, -(bounds.Y + bounds.Height), bounds.X + bounds.Width, -bounds.Y}
	}
	return Extent{0, -size, size, 0}
}

// The axes alternate, five bits to a character, x first. A character outside
// the alphabet is skipped without consuming a halving, so the axis does not
// alternate past it — which is what the field's own normalization makes
// unreachable and what this keeps true anyway.
// GeohashExtent halves the ground down to the cell an address names.
func GeohashExtent(ground Extent, hash string) Extent {
	out := ground
	splitX := true
	for _, character := range strings.ToLower(hash) {
		value := strings.IndexRune(GeohashAlphabet, character)
		if value < 0 {
			continue
		}
		for _, mask := range Masks {
			if splitX {
				middle := (out.MinX + out.MaxX) / 2
				if value&mask != 0 {
					out.MinX = middle
				} else {
					out.MaxX = middle
				}
			} else {
				middle := (out.MinY + out.MaxY) / 2
				if value&mask != 0 {
					out.MinY = middle
				} else {
					out.MaxY = middle
				}
			}
			splitX = !splitX
		}
	}
	return out
}

// Rect is a rectangle of the volume's own raster pixels, y down.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
