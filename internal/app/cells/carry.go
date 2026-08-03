package cells

import "math"

// Carrying a place across systems.
//
// The two hierarchies share no boundaries -- a geohash rectangle and an S2
// quadrilateral agree nowhere except by accident -- so nothing about an address
// survives the crossing. What survives is the *place*: the point the reader was
// looking at, and how closely they were looking at it. [Equivalent] is that
// sentence as arithmetic, and it is the seam's `equivalentCell`
// (analysis/cellsystems/registry.ts) rather than a second idea of the same
// thing: the shared carry vectors in analysis/testdata/cells hold this
// function to the answers the TypeScript lane gives.
//
// WHY THE SERVER CARRIES AT ALL. Cycling the system is a session write like any
// other, and the record it writes has to name a cell the *server* can narrow by
// -- the count above the panel is the count of what the map is drawing
// (docs/render-seam.md §4.2), and a cell carried by the seam alone would be a
// cell the server never heard of. The seam draws the same crossing from the
// same recorded oracle, which is what keeps the two answers one answer.

// bound is one system standing on one ground: the four questions the carry
// asks of a hierarchy, and the floor it stops at. It exists because the carry
// is written once and walks both directions -- the alternative is the same loop
// twice with the two systems' names swapped through it.
type bound interface {
	// level is the depth of an address, the root counting 0.
	level(id string) int
	// floor is how deep this system's telescope goes on this ground.
	floor() int
	// descend is the child of an address holding a point, or nothing at the
	// floor.
	descend(id string, x, y float64) string
	// extent is an address's rectangle in world pixels.
	extent(id string) Extent
	// center is a point inside an address, in world pixels.
	center(id string) (x, y float64)
}

// Equivalent carries a held cell from one system to another: the cell of the
// new hierarchy holding the old cell's centre, taken to the depth whose cells
// cover closest to the same ground.
//
// Area is compared on a LOG scale, because levels grow geometrically and
// "closest" should mean closest in kind rather than closest in pixels: a level
// four times too coarse and a level four times too fine are equally wrong, and
// on a linear scale the coarse one would win every time.
//
// The root carries to the root, which is the only address the two hierarchies
// spell the same way. An empty answer is a carry that could not be made -- a
// system this world does not divide, or a centre off the surface -- and the
// caller's own answer to that is to hold no cell rather than to hold a wrong
// one.
func Equivalent(ground Extent, attrs map[string]string, from, to, id string) string {
	if id == "" {
		return ""
	}
	source, standing := boundTo(from, ground, attrs)
	target, divides := boundTo(to, ground, attrs)
	if !standing || !divides {
		return ""
	}
	x, y := source.center(id)
	area := areaOf(source.extent(id))
	held, best := "", ""
	fit := math.Inf(1)
	for target.level(held) < target.floor() {
		next := target.descend(held, x, y)
		if next == "" {
			break
		}
		nextArea := areaOf(target.extent(next))
		nextFit := math.Abs(math.Log(nextArea / area))
		if nextFit < fit {
			best, fit = next, nextFit
		}
		// Areas only shrink on the way down: past the target, deeper is worse.
		if nextArea <= area {
			break
		}
		held = next
	}
	return best
}

// boundTo stands a system on a ground, or refuses one that does not divide it.
func boundTo(system string, ground Extent, attrs map[string]string) (bound, bool) {
	switch system {
	case SystemGeohash:
		return geohashOn{ground: ground}, true
	case SystemS2:
		mapping, mapped := MappingOf(attrs)
		if !mapped || !Applicable(attrs, SystemS2) {
			return nil, false
		}
		return s2On{ground: ground, mapping: mapping}, true
	}
	return nil, false
}

// areaOf is a rectangle's ground in world pixels squared. Both hierarchies are
// measured this way and only this way; see [S2Extent] for why a spherical area
// would not do.
func areaOf(e Extent) float64 {
	return (e.MaxX - e.MinX) * (e.MaxY - e.MinY)
}

type geohashOn struct{ ground Extent }

func (g geohashOn) level(id string) int { return len(id) }

func (g geohashOn) floor() int { return GeohashMaxDepth }

func (g geohashOn) descend(id string, x, y float64) string {
	return GeohashCellAt(g.ground, x, y, len(id)+1)
}

func (g geohashOn) extent(id string) Extent { return GeohashExtent(g.ground, id) }

func (g geohashOn) center(id string) (x, y float64) {
	held := GeohashExtent(g.ground, id)
	return (held.MinX + held.MaxX) / 2, (held.MinY + held.MaxY) / 2
}

type s2On struct {
	ground  Extent
	mapping Mapping
}

func (s s2On) level(id string) int { return S2Level(id) }

// The floor is the port's, not the library's: the deepest cell counted in a
// bookkeeping where the root is a place (see [S2Level]).
func (s s2On) floor() int { return deepestS2Level + 1 }

func (s s2On) descend(id string, x, y float64) string {
	return S2Descend(s.mapping, id, x, y)
}

func (s s2On) extent(id string) Extent {
	if id == "" {
		return s.ground
	}
	return S2Extent(s.mapping, s.ground, id)
}

func (s s2On) center(id string) (x, y float64) { return S2Center(s.mapping, s.ground, id) }
