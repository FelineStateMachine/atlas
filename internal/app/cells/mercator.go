package cells

import (
	"math"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// The Web-Mercator flattening, and the real-mode geohash it anchors.
//
// A plane world that declares an earth Mercator window and names its body
// (bend-or) is a picture OF somewhere, and its geohash addresses are the real
// ones: classic WGS84 geohash — longitude first, five bits to a character,
// the same alphabet — run in degrees and put back on the picture through the
// declared flattening. This file is the server's half of that arithmetic,
// mirroring analysis/semconv/geometry.ts (the mapping) and the real mode of
// analysis/cellsystems/geohash.ts (the walk), because the server computes
// held-cell membership, the footer counts and the navigator's field length
// without asking the seam.
//
// The window reader is deliberately NOT [MappingOf]: that one is S2's, gated
// on `atlas.geometry.projection = equirect` exactly as the registry requires
// of a sphere. A plane's Mercator window carries no projection key at all —
// the registry's projection enum names only equirect, so there is nothing a
// plane could declare — and the `atlas.geometry.mercator.*` keys themselves
// say what the numbers are. A world that declares a DIFFERENT projection
// outright is declaring these numbers are something else, and is refused.

// Mercator is a declared Web-Mercator flattening: x linear in longitude,
// y linear in the projected latitude asinh(tan φ), which is what a
// real-world tile window actually is (spec/registry.yaml, mercator.px).
type Mercator struct {
	X, Y, W, H               float64
	West, North, East, South float64
	top, bottom              float64
}

// mercatorProjection is the projection value the seam's reader accepts for
// this window (analysis/semconv/geometry.ts). It is not in the registry's
// enum — no validated bundle can declare it — and it is spelled here only so
// a world that DOES declare a projection is read the same way on both lanes.
const mercatorProjection = "mercator"

// MercatorOf reads a world's declared Mercator window out of its attributes,
// or refuses. It mirrors `mercatorMapping` in analysis/semconv/geometry.ts,
// relaxation and all: the pair is read when the projection key says
// `mercator` or says nothing, and refused under any other declaration, a
// missing half, a window with no area or a ground with no extent.
func MercatorOf(attrs map[string]string) (Mercator, bool) {
	if declared, has := attrs[semconv.KeyGeometryProjection]; has && declared != mercatorProjection {
		return Mercator{}, false
	}
	px, hasPx := attrs[semconv.KeyGeometryMercatorPx]
	deg, hasDeg := attrs[semconv.KeyGeometryMercatorDeg]
	if !hasPx || !hasDeg {
		return Mercator{}, false
	}
	// The same shape checks the seam's `declaredWindow` makes: inverting a
	// degenerate window would divide by zero, and silence beats a NaN.
	window, err := semconv.ParseEquirect(px, deg)
	if err != nil {
		return Mercator{}, false
	}
	m := Mercator{
		X: window.X, Y: window.Y, W: window.W, H: window.H,
		West: window.West, North: window.North, East: window.East, South: window.South,
	}
	m.top, m.bottom = mercatorY(m.North), mercatorY(m.South)
	return m, true
}

// mercatorY is the projected latitude: asinh(tan φ), the quantity y is
// linear in.
func mercatorY(lat float64) float64 {
	return math.Asinh(math.Tan(lat * math.Pi / 180))
}

// LatLng puts a point back on the earth: world pixels in, true degrees out.
// The x, y are the application's own — y **negative-down** — and the sign is
// flipped here, on the one line where the two conventions meet, exactly as
// [Mapping.LatLng] flips it for the equirect window.
func (m Mercator) LatLng(x, y float64) (lat, lng float64) {
	down := -y
	lng = m.West + (x-m.X)/m.W*(m.East-m.West)
	lat = math.Atan(math.Sinh(m.top-(down-m.Y)/m.H*(m.top-m.bottom))) * 180 / math.Pi
	return lat, lng
}

// World lands true coordinates on the picture: degrees in, world pixels out,
// y negative-down — the reverse of [Mercator.LatLng] and the same one flip.
func (m Mercator) World(lat, lng float64) (x, y float64) {
	x = m.X + (lng-m.West)/(m.East-m.West)*m.W
	down := m.Y + (m.top-mercatorY(lat))/(m.top-m.bottom)*m.H
	return x, -down
}

// EarthMercator answers the real-mode condition whole, and the mapping with
// it: the world is a PLANE (absent means plane — a sphere's geohash is the
// synthetic one over its picture), it declares an invertible Mercator
// window, and it names the body pictured as earth. Mars is a sphere with an
// equirect window and no earth body, and fails every clause.
func EarthMercator(attrs map[string]string) (Mercator, bool) {
	surface := attrs[semconv.KeyGeometrySurface]
	if surface != "" && surface != semconv.SurfacePlane {
		return Mercator{}, false
	}
	if attrs[semconv.KeyGeometryBody] != "earth" {
		return Mercator{}, false
	}
	return MercatorOf(attrs)
}

// GeohashBaseDepth is where the anchored telescope's layers start: the
// smallest depth ≥ 1 at which a cell's longitude span fits the window's OR
// its latitude span does, capped at 9. Cell spans halve from 360°/180° per
// the 5-bit alternation — `d` characters split longitude ceil(5d/2) times
// and latitude floor(5d/2) — so every span is a power-of-two division and
// exact. For bend-or's window (0.32909° × 0.23650°) this is 4: the depth-4
// latitude span (0.17578125°) fits where the longitude span (0.3515625°)
// just misses.
func GeohashBaseDepth(m Mercator) int {
	lngSpan := m.East - m.West
	latSpan := m.North - m.South
	for depth := 1; depth < 9; depth++ {
		bits := 5 * depth
		lng := 360 / math.Pow(2, math.Ceil(float64(bits)/2))
		lat := 180 / math.Pow(2, math.Floor(float64(bits)/2))
		if lng <= lngSpan || lat <= latSpan {
			return depth
		}
	}
	return 9
}

// GeohashDepthOf is the ground-aware hash-length floor: the base depth plus
// two — three layers of precision, wherever the layers start — which is 6
// for bend-or and [GeohashMaxDepth] everywhere the world is not anchored.
// It is the length the navigator's field accepts, so `maxlength` on the
// input and the cut in the record agree with the seam's `geohashDepth`.
func GeohashDepthOf(attrs map[string]string) int {
	if m, anchored := EarthMercator(attrs); anchored {
		return GeohashBaseDepth(m) + 2
	}
	return GeohashMaxDepth
}

// geohashDegrees is the classic mask walk, forward: a hash to its own ground
// in true degrees. The same skip rule as [GeohashExtent]: a character
// outside the alphabet does not consume a halving.
func geohashDegrees(hash string) (west, south, east, north float64) {
	west, east = -180, 180
	south, north = -90, 90
	splitLng := true
	for _, character := range hash {
		value := indexOfLower(character)
		if value < 0 {
			continue
		}
		for _, mask := range Masks {
			if splitLng {
				middle := (west + east) / 2
				if value&mask != 0 {
					west = middle
				} else {
					east = middle
				}
			} else {
				middle := (south + north) / 2
				if value&mask != 0 {
					south = middle
				} else {
					north = middle
				}
			}
			splitLng = !splitLng
		}
	}
	return west, south, east, north
}

// indexOfLower is the alphabet ordinal of a character, lowercased, or -1.
func indexOfLower(character rune) int {
	if character >= 'A' && character <= 'Z' {
		character += 'a' - 'A'
	}
	for at, own := range GeohashAlphabet {
		if own == character {
			return at
		}
	}
	return -1
}

// GeohashRealCellAt is the real address under a point, at the depth asked:
// world pixels back to true degrees through the flattening, then the
// textbook walk. A point outside the declared window still has a real
// geohash — the ground is real earth — so nothing here refuses, which is
// the one place real mode and [GeohashCellAt] part company.
func GeohashRealCellAt(m Mercator, x, y float64, depth int) string {
	lat, lng := m.LatLng(x, y)
	west, east := -180.0, 180.0
	south, north := -90.0, 90.0
	splitLng := true
	hash := make([]byte, 0, depth)
	for range depth {
		value := 0
		for _, mask := range Masks {
			if splitLng {
				middle := (west + east) / 2
				if lng >= middle {
					value |= mask
					west = middle
				} else {
					east = middle
				}
			} else {
				middle := (south + north) / 2
				if lat >= middle {
					value |= mask
					south = middle
				} else {
					north = middle
				}
			}
			splitLng = !splitLng
		}
		hash = append(hash, GeohashAlphabet[value])
	}
	return string(hash)
}

// GeohashRealHeld is the held-cell question for a real address: the point
// goes back to true degrees and the cell's own degree rectangle is asked,
// boundaries inclusive on every edge exactly as the synthetic rectangle is.
// The root is the whole earth and holds every point of the picture's plane.
func GeohashRealHeld(m Mercator, hash string) Held {
	if hash == "" {
		return func(float64, float64) bool { return true }
	}
	west, south, east, north := geohashDegrees(hash)
	return func(x, y float64) bool {
		lat, lng := m.LatLng(x, y)
		return lng >= west && lng <= east && lat >= south && lat <= north
	}
}

// GeohashRealExtent is a real cell's rectangle in world pixels. Mercator
// keeps latitude lines horizontal, so the cell stays a rectangle and two
// corners are enough; the extents may overhang the declared window, which
// is honest — the cell is the planet's even where the picture stops.
func GeohashRealExtent(m Mercator, hash string) Extent {
	west, south, east, north := geohashDegrees(hash)
	minX, minY := m.World(south, west)
	maxX, maxY := m.World(north, east)
	return Extent{MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY}
}
