package basemap

// Clipping, and the winding convention the fills stand on.
//
// A tile draws against a window bled a little past its own edge, so every cut
// the clipper makes lands outside the picture. That is what lets neighbouring
// tiles be drawn independently and still continue each other exactly: the seam
// is drawn twice, from the same numbers, and never falls on a pixel either tile
// keeps.

// box is an axis-aligned rectangle in world pixels.
type box struct {
	minX, minY, maxX, maxY float64
}

// span measures a set of rings or lines. An empty set comes back inverted --
// minimum above maximum -- which is how a caller tells "nothing here" from a
// rectangle of zero size.
func span(paths [][][2]float64) box {
	out := box{minX: 1e18, minY: 1e18, maxX: -1e18, maxY: -1e18}
	for _, path := range paths {
		for _, at := range path {
			out.minX = min(out.minX, at[0])
			out.minY = min(out.minY, at[1])
			out.maxX = max(out.maxX, at[0])
			out.maxY = max(out.maxY, at[1])
		}
	}
	return out
}

func (b box) empty() bool { return b.minX > b.maxX }

// join is the rectangle covering both, used to reach one feature's rings and
// lines with a single measurement.
func (b box) join(o box) box {
	if b.empty() {
		return o
	}
	if o.empty() {
		return b
	}
	return box{
		minX: min(b.minX, o.minX), minY: min(b.minY, o.minY),
		maxX: max(b.maxX, o.maxX), maxY: max(b.maxY, o.maxY),
	}
}

// clipRing cuts a polygon ring to a window by Sutherland--Hodgman: four
// passes, one per window edge, each keeping what is inside and splicing in the
// crossings. A ring that comes out with fewer than three corners encloses
// nothing and is dropped.
func clipRing(ring [][2]float64, window box) [][2]float64 {
	inside := [4]func([2]float64) bool{
		func(p [2]float64) bool { return p[0] >= window.minX },
		func(p [2]float64) bool { return p[0] <= window.maxX },
		func(p [2]float64) bool { return p[1] >= window.minY },
		func(p [2]float64) bool { return p[1] <= window.maxY },
	}
	crossing := [4]func(a, b [2]float64) [2]float64{
		func(a, b [2]float64) [2]float64 { return atX(a, b, window.minX) },
		func(a, b [2]float64) [2]float64 { return atX(a, b, window.maxX) },
		func(a, b [2]float64) [2]float64 { return atY(a, b, window.minY) },
		func(a, b [2]float64) [2]float64 { return atY(a, b, window.maxY) },
	}

	current := ring
	for edge := range inside {
		if len(current) == 0 {
			return nil
		}
		next := make([][2]float64, 0, len(current)+4)
		for i := range current {
			from := current[(i+len(current)-1)%len(current)]
			to := current[i]
			fromIn, toIn := inside[edge](from), inside[edge](to)
			switch {
			case fromIn && toIn:
				next = append(next, to)
			case fromIn:
				next = append(next, crossing[edge](from, to))
			case toIn:
				next = append(next, crossing[edge](from, to), to)
			}
		}
		current = next
	}
	if len(current) < 3 {
		return nil
	}
	return current
}

// atX and atY are the point where a segment meets a vertical or horizontal
// line. Both are only ever called for a segment that actually crosses it, so
// the denominator cannot be zero.
func atX(a, b [2]float64, x float64) [2]float64 {
	t := (x - a[0]) / (b[0] - a[0])
	return [2]float64{x, a[1] + t*(b[1]-a[1])}
}

func atY(a, b [2]float64, y float64) [2]float64 {
	t := (y - a[1]) / (b[1] - a[1])
	return [2]float64{a[0] + t*(b[0]-a[0]), y}
}

// clipSegment cuts one segment to a window by Liang--Barsky, reporting whether
// any of it survives. A polyline is clipped a segment at a time rather than as
// a whole, because a stroke's dash rhythm has to keep riding the original path
// however much of it is off the tile.
func clipSegment(a, b [2]float64, window box) ([2]float64, [2]float64, bool) {
	enter, leave := 0.0, 1.0
	dx, dy := b[0]-a[0], b[1]-a[1]

	// narrow tightens the surviving interval against one boundary, stated as
	// the usual p/q pair: p is the segment's motion across it, q the distance
	// to it. A segment parallel to the boundary survives only if it starts on
	// the inside of it.
	narrow := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		t := q / p
		switch {
		case p < 0:
			if t > leave {
				return false
			}
			enter = max(enter, t)
		default:
			if t < enter {
				return false
			}
			leave = min(leave, t)
		}
		return true
	}

	if !narrow(-dx, a[0]-window.minX) || !narrow(dx, window.maxX-a[0]) ||
		!narrow(-dy, a[1]-window.minY) || !narrow(dy, window.maxY-a[1]) {
		return a, b, false
	}
	return [2]float64{a[0] + enter*dx, a[1] + enter*dy},
		[2]float64{a[0] + leave*dx, a[1] + leave*dy}, true
}

// normalizeWinding forces a polygon's rings into the convention the fills
// stand on: the first ring is ground and winds one way, every ring after it is
// a hole and winds the other, whatever the publisher said. Real exports do not
// reliably follow the GeoJSON rule, and a hole wound the wrong way fills itself
// in rather than cutting out.
func normalizeWinding(rings [][][2]float64) {
	for at, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		// A ring whose signed area is exactly zero encloses nothing either
		// way round, and is left exactly as it came: flipping it would change
		// the winding a self-crossing ring contributes without changing the
		// area that made it look degenerate.
		switch area := signedArea(ring); {
		case at == 0 && area < 0, at > 0 && area > 0:
			reverse(ring)
		}
	}
}

// signedArea is a ring's area carrying the sign of its winding, measured in the
// y-down pixel plane where positive reads as clockwise on the screen.
func signedArea(ring [][2]float64) float64 {
	total := 0.0
	for i := range ring {
		j := (i + 1) % len(ring)
		total += ring[i][0]*ring[j][1] - ring[j][0]*ring[i][1]
	}
	return total / 2
}

// reverse flips a ring's winding where it lies.
func reverse(ring [][2]float64) {
	for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
		ring[i], ring[j] = ring[j], ring[i]
	}
}
