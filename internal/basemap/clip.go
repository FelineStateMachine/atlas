package basemap

// box is an axis-aligned window in world pixels.
type box struct {
	minX, minY, maxX, maxY float64
}

func (b box) contains(p [2]float64) bool {
	return p[0] >= b.minX && p[0] <= b.maxX && p[1] >= b.minY && p[1] <= b.maxY
}

func (b box) overlaps(o box) bool {
	return b.minX <= o.maxX && o.minX <= b.maxX && b.minY <= o.maxY && o.minY <= b.maxY
}

// bounds measures a set of rings or lines.
func bounds(paths [][][2]float64) box {
	out := box{minX: 1e18, minY: 1e18, maxX: -1e18, maxY: -1e18}
	for _, path := range paths {
		for _, p := range path {
			out.minX = min(out.minX, p[0])
			out.minY = min(out.minY, p[1])
			out.maxX = max(out.maxX, p[0])
			out.maxY = max(out.maxY, p[1])
		}
	}
	return out
}

// clipRing cuts a polygon ring to a window, Sutherland–Hodgman: one pass
// per edge, each keeping the inside and splicing in the crossings. The
// window carries bleed beyond the visible tile, so the cut edges land
// outside the picture and adjacent tiles continue the shape exactly.
func clipRing(ring [][2]float64, window box) [][2]float64 {
	type edge func(p [2]float64) bool
	inside := []edge{
		func(p [2]float64) bool { return p[0] >= window.minX },
		func(p [2]float64) bool { return p[0] <= window.maxX },
		func(p [2]float64) bool { return p[1] >= window.minY },
		func(p [2]float64) bool { return p[1] <= window.maxY },
	}
	cross := []func(a, b [2]float64) [2]float64{
		func(a, b [2]float64) [2]float64 { return lerpX(a, b, window.minX) },
		func(a, b [2]float64) [2]float64 { return lerpX(a, b, window.maxX) },
		func(a, b [2]float64) [2]float64 { return lerpY(a, b, window.minY) },
		func(a, b [2]float64) [2]float64 { return lerpY(a, b, window.maxY) },
	}
	current := ring
	for at := range inside {
		if len(current) == 0 {
			return nil
		}
		next := make([][2]float64, 0, len(current)+4)
		for i := range current {
			a := current[(i+len(current)-1)%len(current)]
			b := current[i]
			aIn, bIn := inside[at](a), inside[at](b)
			switch {
			case aIn && bIn:
				next = append(next, b)
			case aIn && !bIn:
				next = append(next, cross[at](a, b))
			case !aIn && bIn:
				next = append(next, cross[at](a, b), b)
			}
		}
		current = next
	}
	if len(current) < 3 {
		return nil
	}
	return current
}

func lerpX(a, b [2]float64, x float64) [2]float64 {
	t := (x - a[0]) / (b[0] - a[0])
	return [2]float64{x, a[1] + t*(b[1]-a[1])}
}

func lerpY(a, b [2]float64, y float64) [2]float64 {
	t := (y - a[1]) / (b[1] - a[1])
	return [2]float64{a[0] + t*(b[0]-a[0]), y}
}

// clipSegment cuts one segment to a window, Liang–Barsky, reporting whether
// anything survives.
func clipSegment(a, b [2]float64, window box) ([2]float64, [2]float64, bool) {
	t0, t1 := 0.0, 1.0
	dx, dy := b[0]-a[0], b[1]-a[1]
	clip := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		t := q / p
		if p < 0 {
			if t > t1 {
				return false
			}
			if t > t0 {
				t0 = t
			}
		} else {
			if t < t0 {
				return false
			}
			if t < t1 {
				t1 = t
			}
		}
		return true
	}
	if !clip(-dx, a[0]-window.minX) || !clip(dx, window.maxX-a[0]) ||
		!clip(-dy, a[1]-window.minY) || !clip(dy, window.maxY-a[1]) {
		return a, b, false
	}
	from := [2]float64{a[0] + t0*dx, a[1] + t0*dy}
	to := [2]float64{a[0] + t1*dx, a[1] + t1*dy}
	return from, to, true
}

// signedArea is a ring's area with the sign of its winding, in the y-down
// pixel plane: positive is clockwise on the screen.
func signedArea(ring [][2]float64) float64 {
	area := 0.0
	for i := range ring {
		j := (i + 1) % len(ring)
		area += ring[i][0]*ring[j][1] - ring[j][0]*ring[i][1]
	}
	return area / 2
}

// reverse flips a ring's winding in place.
func reverse(ring [][2]float64) {
	for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
		ring[i], ring[j] = ring[j], ring[i]
	}
}
