package cells

import (
	"strings"

	"github.com/golang/geo/s2"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// The S2 side: the sphere divided by the six faces of a cube projected onto
// it, each face a quadtree of exactly-nested quadrilaterals addressed by
// Hilbert-curve tokens.
//
// Nothing spherical is written here. `github.com/golang/geo/s2` is the
// canonical Google library and the seam's s2js is a port of it, so the two
// lanes hold the same arithmetic by provenance rather than by agreement —
// which is the whole reason the server may answer a containment question at
// all instead of asking the seam for its answer. What this file adds is the
// two things the port has that the library does not: the bridge from world
// pixels to the sphere through the ground's declared flattening, and the level
// bookkeeping in which the root is a place.
//
// LEVELS. The port counts the root as 0 and the six faces as 1; S2 counts the
// faces as 0. The two bookkeepings meet in [S2Level] and nowhere else, exactly
// as they meet in `portLevel` in analysis/cellsystems/s2.ts.
//
// THE ROOT. The root is `""` in every system, and S2's token vocabulary has no
// spelling for it — `CellIDFromToken("")` answers a cell id that is neither
// the sphere nor an error. Every function below that could be handed the root
// answers for the whole ground instead of asking the library a question it
// cannot hear.

// deepestS2Level is where the telescope stops: cells a few kilometres across
// on a Mars-sized body, tokens six characters long. It is the seam's floor
// (analysis/cellsystems/s2.ts) and is spelled here so that an address arriving
// from a request is held to the same floor the navigator would have produced.
const deepestS2Level = 10

// Mapping is a world's declared flattening, read once and inverted per point.
//
// A geodesic system addresses a sphere, and a packed position is a pixel on a
// raster. The mapping is the whole of what turns one into the other, and a
// world that declares none cannot be divided by S2 at all — which is the
// refusal [Applicable] makes on the session's behalf, before any of this is
// reached.
type Mapping struct{ window semconv.Equirect }

// MappingOf reads the world's declared flattening out of its attributes, or
// refuses. It mirrors `geoMapping` in analysis/semconv/geometry.ts, including
// its one subtlety: the window is only read when `atlas.geometry.projection`
// says which flattening the numbers are for. A world carrying an equirect
// window under some other projection is declaring something this cannot read,
// and a plausible answer would be worse than none.
func MappingOf(attrs map[string]string) (Mapping, bool) {
	if attrs[semconv.KeyGeometryProjection] != semconv.ProjectionEquirect {
		return Mapping{}, false
	}
	window, declared, err := semconv.EquirectOf(attrs)
	if !declared || err != nil {
		return Mapping{}, false
	}
	return Mapping{window: window}, true
}

// LatLng puts a point back on the sphere: world pixels in, true degrees out.
//
// The x, y are the application's own — y **negative-down**, the space a pin's
// position lives in — and the sign is flipped here, on the one line where the
// two conventions meet. [semconv.Equirect.Invert] reads y down from the top of
// the raster window, which is the convention the attributes are written in and
// the one the seam's mapping speaks (see the same flip at `leafOf` in
// analysis/cellsystems/s2.ts).
func (m Mapping) LatLng(x, y float64) (lat, lon float64) {
	return m.window.Invert(x, -y)
}

// S2Held is the held-cell question for an S2 address: the point goes back on
// the sphere, becomes the leaf cell holding it, and the held cell is asked
// whether that leaf is one of its descendants.
//
// Two addresses hold nothing rather than something. An empty token is the
// root, and the root is the whole sphere — but the caller reaching this
// function has a cell held, and "no cell" is not a narrowing it should be
// asking about, so it is spelled out here as the whole ground rather than left
// to the library. An address that does not parse is not a place: nothing is
// standing in it, which is the conservative half of the disagreement — a map
// that draws nothing is wrong in a way a reader can see, where a server that
// narrows nothing while the seam narrows is wrong in a way nobody can.
func S2Held(mapping Mapping, token string) Held {
	if token == "" {
		return func(float64, float64) bool { return true }
	}
	cell, ok := s2Cell(token)
	if !ok {
		return func(float64, float64) bool { return false }
	}
	return func(x, y float64) bool {
		lat, lon := mapping.LatLng(x, y)
		return cell.Contains(s2.CellIDFromLatLng(s2.LatLngFromDegrees(lat, lon)))
	}
}

// S2CellAt is the leaf-most address of a point at the telescope's floor: the
// answer the pin card carries and `locate` in the port. It is empty when the
// point is nowhere the mapping can name.
func S2CellAt(mapping Mapping, x, y float64) string {
	lat, lon := mapping.LatLng(x, y)
	leaf := s2.CellIDFromLatLng(s2.LatLngFromDegrees(lat, lon))
	if !leaf.IsValid() {
		return ""
	}
	return leaf.Parent(deepestS2Level).ToToken()
}

// S2Level is the port level of an address: 0 for the root, 1 for a face, and
// so on down. A token that is not a place has no level, and says so with -1.
func S2Level(token string) int {
	if token == "" {
		return 0
	}
	cell, ok := s2Cell(token)
	if !ok {
		return -1
	}
	return cell.Level() + 1
}

// S2Parent is the address one level out. The root has no parent and neither
// does an address nobody can read; both answer the root, which is where a
// reader telescoping out of an address they cannot be standing in belongs.
//
// This is the arithmetic that makes S2 the proof the contract holds nothing
// geohash-shaped: an S2 address does not refine by appending characters, so
// telescoping out of `47a1cb` is not dropping its last character — it is
// `47a1`, three characters shorter and spelled differently.
func S2Parent(token string) string {
	cell, ok := s2Cell(token)
	if !ok || cell.Level() == 0 {
		return ""
	}
	return cell.Parent(cell.Level() - 1).ToToken()
}

// S2Children is the address's four children in their stable order, or the six
// faces when the address is the root. An address nobody can read has none.
func S2Children(token string) []string {
	if token == "" {
		out := make([]string, 0, 6)
		for face := range 6 {
			out = append(out, s2.CellIDFromFace(face).ToToken())
		}
		return out
	}
	cell, ok := s2Cell(token)
	if !ok {
		return nil
	}
	children := cell.Children()
	out := make([]string, 0, len(children))
	for _, child := range children {
		out = append(out, child.ToToken())
	}
	return out
}

// S2ParseCell reads an address the way the navigator's field does: the root is
// a place, a token deeper than the telescope's floor clamps to the floor, and
// anything else is either a canonically spelled token or not a place at all.
// It is the port's `parseInput` (analysis/cellsystems/s2.ts) minus the
// keystroke normalization, which belongs to the field and not to the server.
func S2ParseCell(text string) (string, bool) {
	if text == "" {
		return "", true
	}
	cell, ok := s2Cell(text)
	if !ok {
		return "", false
	}
	if cell.Level() > deepestS2Level {
		return cell.Parent(deepestS2Level).ToToken(), true
	}
	return cell.ToToken(), true
}

// s2Cell reads a token, refusing what is not a cell.
//
// The guard is not a formality. A token the library cannot read answers the
// zero cell id, whose descendant range is the whole of the space — so an
// unguarded `Contains` on it says yes to every point on the sphere, and a
// misspelled address would quietly stop narrowing anything.
func s2Cell(token string) (s2.CellID, bool) {
	cell := s2.CellIDFromToken(strings.TrimSpace(token))
	if cell == 0 || !cell.IsValid() {
		return 0, false
	}
	return cell, true
}
