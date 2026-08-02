// Package cells is the cell-system arithmetic the *server* needs: enough of
// every system a session can hold to answer two questions without asking the
// seam.
package cells

import (
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// THE FIRST QUESTION is the held cell's: is this feature inside it.
//
// A grid narrows what stands the same way a highlight does (docs/render-seam.md
// §4.2, rule 4), and the count above the panel beside the map has to be the
// count of what the map is drawing — which means the application has to be able
// to answer it for *every* system, not merely for the first one. A package that
// answers for geohash and says "no cell" for the rest is worse than one that
// answers for neither: the seam narrows the drawn set, the server narrows
// nothing, and the map, the footer and the dock stop agreeing while every one
// of them believes it is right.
//
// THE SECOND QUESTION is the world's: which systems divide it at all. Geohash
// divides anything, because it never asks what the picture is of. S2 asks, and
// refuses a ground that cannot say (analysis/cellsystems/s2.ts, `appliesTo`).
// The navigator offers only the systems that answer yes, so a system value
// arriving on a request means nothing except against a world — which is what
// [ApplicableSystems] answers and what the session spends it on.
//
// THE THIRD QUESTION arrived with the second system, and it is the reader's: I
// am standing here, now divide it that way instead. Cycling the navigator
// carries the held place across (carry.go, [Equivalent]), and the *record* has
// to name the cell that lands, because the record is what the first question
// is asked of. A crossing the seam made alone would be a cell the server never
// heard of, and the first question would start answering about a cell nobody
// is holding.
//
// WHY THIS IS HERE AND NOT IN THE ANALYSIS LANE. Cell systems belong to
// `analysis/cellsystems` (issue #5 §5.4) and that lane is TypeScript, consumed
// by the seam. The seam is deletable and the application is not, so a fact the
// *server* needs cannot live behind it: the alternative to this file is a
// third upward flow from the seam carrying the cell's extent, which
// docs/render-seam.md §5 forbids in as many words.
//
// TWO SYSTEMS, TWO PROVENANCES. The geohash halving here is a second copy of
// an arithmetic, and that is a cost rather than a design: it is kept to the
// smallest piece of it the three questions need — the halving, the same
// halving run backward, and nothing else. No plan, no style token, no pole
// closure, none of which the server has any use for. S2 is not a copy at all.
// Its spherical math is github.com/golang/geo/s2, the canonical Google
// library; the seam's s2js is that library ported, test vectors and all. The
// two lanes agree because they are the same arithmetic, not because two
// authors read the same paper, and what is written here is only the bridge
// from world pixels to the sphere, the port's own level bookkeeping, and the
// one measurement the third question needs: a cell's rectangle *as drawn*,
// which is an edge walk rather than a spherical area and says why at
// [S2Extent].
//
// AND IT IS GATED. `golden/cells` reproduces every geohash cell extent
// recorded in the parity baselines, and holds the S2 side, the surface ladder
// and the applicable-systems answer to `golden/analysis/vectors` — the same
// language-neutral oracle the TypeScript lane is held to. When a Go twin of
// the analysis lane exists, this package is the first thing it deletes.
//
// COORDINATES. Everything exported here speaks world pixels in the orientation
// the application's own projection produces: x east, y **negative-down**
// (internal/app/world.go, tileGrid.project), which is the space a pin's
// position lives in and the space the seam's cell systems work in. A declared
// flattening reads the other way round — y down from the top of the raster
// window (format/semconv, [semconv.Equirect.Invert]) — and the sign is flipped
// at that one call, which is exactly where analysis/semconv/geometry.ts says
// the two conventions meet.

// The systems this package divides by, named in the order the navigator offers
// them (analysis/cellsystems/registry.ts): geohash first because it divides
// anything, S2 second because it asks the ground a question.
const (
	SystemGeohash = "geohash"
	SystemS2      = "s2"
)

// Held is the held cell as a question rather than as a shape: does this cell
// hold the point at these world pixels.
//
// A predicate rather than a rectangle because only one of the two systems has
// a rectangle. A geohash cell is axis-aligned in world pixels and answers by
// comparing four numbers; an S2 cell is a spherical quadrilateral whose edges
// are straight on no projection, and the only honest way to ask it is to put
// the point back on the sphere. The seam's own test is the same shape
// (`cellTest` in render/chart/grid.ts: `(at) => on.contains(cell, at)`), which
// is what makes the two sides comparable at all.
//
// A nil Held is no narrowing: no grid, or no cell held inside it.
type Held func(x, y float64) bool

// ApplicableSystems answers which systems will divide a world, in the
// navigator's order. It is the server's half of the seam's
// `applicableSystems(ground)`, and it is deliberately the same two questions
// in the same order: S2 wants a surface that says it is a sphere and a
// flattening that can be inverted, and geohash wants nothing.
//
// The attributes are the world's own (`atlas.geometry.*`, format/semconv).
// Only the lens-shaped half of a seam `Ground` is missing from that, and
// neither question reads it — which is why this takes attributes rather than
// a ground.
//
// ONE PROJECTION, FOR NOW. The seam accepts any invertible flattening it can
// read, which today means equirectangular or Web-Mercator. The registry knows
// one projection value (`equirect`, format/semconv), so a Mercator world
// cannot pass bundle validation and cannot reach this function; when the
// vocabulary grows the second value, this is where it lands, alongside the
// mapping reader in s2.go.
func ApplicableSystems(attrs map[string]string) []string {
	out := []string{SystemGeohash}
	if Applicable(attrs, SystemS2) {
		out = append(out, SystemS2)
	}
	return out
}

// Applicable answers whether one system divides this world. It is the whole
// of the session's system-value validation: a value that is not a system, or
// is a system this world refuses, is not a place a reader can be standing.
func Applicable(attrs map[string]string, system string) bool {
	switch system {
	case SystemGeohash:
		return true
	case SystemS2:
		if attrs[semconv.KeyGeometrySurface] != semconv.SurfaceSphere {
			return false
		}
		_, mapped := MappingOf(attrs)
		return mapped
	}
	return false
}

// GeohashAlphabet is the canonical order, which is also the child order. No
// `a`, `i`, `l` or `o`, so a hash read aloud cannot be misheard.
const GeohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// GeohashMaxDepth is where the geohash telescope stops: three characters,
// fifteen halvings, a cell a few world pixels across on an 8192 square. It is
// the seam's floor (analysis/cellsystems/geohash.ts) and the length the
// navigator's field accepts, which is why a request carrying more than this
// is clamped rather than refused.
const GeohashMaxDepth = 3

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

// GeohashCellAt is the halving run backward: the same divisions, choosing at
// each one the side the point is on, to the depth asked for. It is the seam's
// `cellAt` (analysis/cellsystems/geohash.ts) and the descent the carry uses to
// walk a new hierarchy down toward an old cell's centre.
//
// A point off the surface names nothing, which is the empty answer -- not the
// root, which is a place.
func GeohashCellAt(ground Extent, x, y float64, depth int) string {
	if !ground.Holds(x, y) {
		return ""
	}
	out := ground
	splitX := true
	hash := make([]byte, 0, depth)
	for range depth {
		value := 0
		for _, mask := range Masks {
			if splitX {
				middle := (out.MinX + out.MaxX) / 2
				if x >= middle {
					value |= mask
					out.MinX = middle
				} else {
					out.MaxX = middle
				}
			} else {
				middle := (out.MinY + out.MaxY) / 2
				if y >= middle {
					value |= mask
					out.MinY = middle
				} else {
					out.MaxY = middle
				}
			}
			splitX = !splitX
		}
		hash = append(hash, GeohashAlphabet[value])
	}
	return string(hash)
}

// GeohashHeld is the held-cell question for a geohash address: the halving,
// and then four comparisons. It is the fast path — an axis-aligned rectangle
// asked about a point — and it is boundary-inclusive, which is the contract's
// own word for it (golden/analysis/vectors/containment.json): a pin on the
// line between two cells is standing in both, because the line is drawn a
// pixel wide and the reader put the pin on it.
func GeohashHeld(ground Extent, hash string) Held {
	held := GeohashExtent(ground, hash)
	return held.Holds
}

// Rect is a rectangle of the volume's own raster pixels, y down.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
