package arcgismap

// Clipping for national datasets. A watershed intersecting a city's window
// may extend a hundred windows past it, and the synthetic projection is
// linear in the window's own terms: a vertex far outside would land on an
// absurd longitude and wrap the viewer's world. City data never needs this
// -- a hub publishes ground near its own city -- so only features fetched by
// window are cut down to it, at capture time, where the trimmed shape rides
// the content addressing like any other fact about the day.
//
// The window rectangle is cut in plain degrees. Its edges are lines of
// constant longitude and latitude, and both the Mercator forward and the
// synthetic spelling are monotone per axis, so the degree rectangle and the
// projected square bound exactly the same ground.

// ClipRings cuts MultiPolygon-nested rings to the window, ring by ring with
// Sutherland–Hodgman. A polygon whose outer ring is cut to nothing is
// dropped whole; a hole cut to nothing drops alone. Introduced vertices are
// rounded like every captured coordinate, and output rings close on their
// first position.
func ClipRings(w Window, polygons [][][][]float64) [][][][]float64 {
	var out [][][][]float64
	for _, polygon := range polygons {
		var kept [][][]float64
		for at, ring := range polygon {
			clipped := clipRing(w, ring)
			if clipped == nil {
				if at == 0 {
					kept = nil
					break
				}
				continue
			}
			kept = append(kept, clipped)
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// ClipLines cuts MultiLineString-nested lines to the window. A line that
// leaves and returns splits into one part per crossing, and parts too short
// to draw are dropped.
func ClipLines(w Window, lines [][][]float64) [][][]float64 {
	var out [][][]float64
	var part [][]float64
	flush := func() {
		if len(part) >= 2 {
			out = append(out, part)
		}
		part = nil
	}
	for _, line := range lines {
		for at := 0; at+1 < len(line); at++ {
			a, b := line[at], line[at+1]
			if len(a) < 2 || len(b) < 2 {
				flush()
				continue
			}
			enter, exit, held := clipSegment(w, a[0], a[1], b[0], b[1])
			if !held {
				flush()
				continue
			}
			if len(part) == 0 || part[len(part)-1][0] != enter[0] || part[len(part)-1][1] != enter[1] {
				flush()
				part = [][]float64{enter}
			}
			part = append(part, exit)
			if exit[0] != Round7(b[0]) || exit[1] != Round7(b[1]) {
				flush()
			}
		}
		flush()
	}
	return out
}

// edge is one of the window's four half-planes.
type edge struct {
	axis  int // 0 clips longitude, 1 latitude
	bound float64
	keep  float64 // +1 keeps values >= bound, -1 keeps values <= bound
}

func (w Window) edges() [4]edge {
	return [4]edge{
		{0, w.West, 1},
		{0, w.East, -1},
		{1, w.South, 1},
		{1, w.North, -1},
	}
}

func (e edge) inside(p []float64) bool {
	return (p[e.axis]-e.bound)*e.keep >= 0
}

// cross lands on the edge along the segment from a to b, rounded the way
// every captured coordinate is.
func (e edge) cross(a, b []float64) []float64 {
	t := (e.bound - a[e.axis]) / (b[e.axis] - a[e.axis])
	p := []float64{
		Round7(a[0] + t*(b[0]-a[0])),
		Round7(a[1] + t*(b[1]-a[1])),
	}
	p[e.axis] = e.bound
	return p
}

// clipRing is Sutherland–Hodgman against the window's four edges. The ring
// arrives closed and leaves closed; nil means nothing of it remains.
func clipRing(w Window, ring [][]float64) [][]float64 {
	open := ring
	if len(open) > 1 && open[0][0] == open[len(open)-1][0] && open[0][1] == open[len(open)-1][1] {
		open = open[:len(open)-1]
	}
	current := make([][]float64, 0, len(open))
	for _, p := range open {
		if len(p) >= 2 {
			current = append(current, p)
		}
	}
	for _, e := range w.edges() {
		if len(current) == 0 {
			return nil
		}
		next := make([][]float64, 0, len(current)+4)
		for at, point := range current {
			previous := current[(at+len(current)-1)%len(current)]
			switch {
			case e.inside(point) && e.inside(previous):
				next = append(next, point)
			case e.inside(point):
				next = append(next, e.cross(previous, point), point)
			case e.inside(previous):
				next = append(next, e.cross(previous, point))
			}
		}
		current = dedupe(next)
	}
	if len(current) < 3 {
		return nil
	}
	return append(current, current[0])
}

// dedupe drops consecutive repeats, which clipping at a corner introduces
// and rounding can too.
func dedupe(points [][]float64) [][]float64 {
	out := points[:0]
	for _, p := range points {
		if len(out) > 0 && out[len(out)-1][0] == p[0] && out[len(out)-1][1] == p[1] {
			continue
		}
		out = append(out, p)
	}
	if len(out) > 1 && out[0][0] == out[len(out)-1][0] && out[0][1] == out[len(out)-1][1] {
		out = out[:len(out)-1]
	}
	return out
}

// clipSegment cuts one segment to the window, answering the kept piece's two
// ends rounded, or nothing when the segment misses the window entirely.
func clipSegment(w Window, ax, ay, bx, by float64) (enter, exit []float64, held bool) {
	t0, t1 := 0.0, 1.0
	clip := func(delta, room float64) bool {
		if delta == 0 {
			return room >= 0
		}
		t := room / delta
		if delta < 0 {
			if t > t1 {
				return false
			}
			if t > t0 {
				t0 = t
			}
			return true
		}
		if t < t0 {
			return false
		}
		if t < t1 {
			t1 = t
		}
		return true
	}
	dx, dy := bx-ax, by-ay
	if !clip(-dx, ax-w.West) || !clip(dx, w.East-ax) ||
		!clip(-dy, ay-w.South) || !clip(dy, w.North-ay) {
		return nil, nil, false
	}
	enter = []float64{Round7(ax + t0*dx), Round7(ay + t0*dy)}
	exit = []float64{Round7(ax + t1*dx), Round7(ay + t1*dy)}
	if enter[0] == exit[0] && enter[1] == exit[1] {
		return nil, nil, false
	}
	return enter, exit, true
}
