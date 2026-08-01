package basemap

import (
	"math"

	"golang.org/x/image/vector"
)

// kappa is the control-point offset that makes four cubic Béziers spell a
// circle as closely as cubics can.
const kappa = 0.5522847498307936

// strokeSegment adds one capsule to the path: the segment widened to its
// stroke, with a semicircular cap at each end. Every capsule winds the same
// way, and the rasterizer's saturating accumulation renders the union --
// overlapping capsules at a joint read as one continuous line, no seams.
// Coordinates are tile-local pixels.
func strokeSegment(r *vector.Rasterizer, a, b [2]float64, width float64) {
	half := width / 2
	dx, dy := b[0]-a[0], b[1]-a[1]
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	// The unit direction and left normal, scaled.
	ux, uy := dx/length, dy/length
	nx, ny := -uy*half, ux*half
	k := kappa * half

	moveTo(r, a[0]+nx, a[1]+ny)
	lineTo(r, b[0]+nx, b[1]+ny)
	// The far cap: from b's left shoulder, around the nose at b+u*half, to
	// b's right shoulder, two quarter arcs. A tangent control point moves
	// k along the tangent; n is already half long, so n scaled by kappa is
	// the same k in the normal's direction.
	cubeTo(r,
		b[0]+nx+ux*k, b[1]+ny+uy*k,
		b[0]+ux*half+nx*kappa, b[1]+uy*half+ny*kappa,
		b[0]+ux*half, b[1]+uy*half)
	cubeTo(r,
		b[0]+ux*half-nx*kappa, b[1]+uy*half-ny*kappa,
		b[0]-nx+ux*k, b[1]-ny+uy*k,
		b[0]-nx, b[1]-ny)
	lineTo(r, a[0]-nx, a[1]-ny)
	// The near cap, back around a-u*half.
	cubeTo(r,
		a[0]-nx-ux*k, a[1]-ny-uy*k,
		a[0]-ux*half-nx*kappa, a[1]-uy*half-ny*kappa,
		a[0]-ux*half, a[1]-uy*half)
	cubeTo(r,
		a[0]-ux*half+nx*kappa, a[1]-uy*half+ny*kappa,
		a[0]+nx-ux*k, a[1]+ny-uy*k,
		a[0]+nx, a[1]+ny)
	r.ClosePath()
}

// strokeDashed walks a segment in the dash rhythm, stroking the on-runs.
// The phase rides along the polyline so the rhythm survives vertices, and
// the caller threads it between segments.
func strokeDashed(r *vector.Rasterizer, a, b [2]float64, width float64, dash [2]float64, phase float64) float64 {
	on, off := dash[0], dash[1]
	period := on + off
	if period <= 0 {
		strokeSegment(r, a, b, width)
		return phase
	}
	dx, dy := b[0]-a[0], b[1]-a[1]
	length := math.Hypot(dx, dy)
	if length == 0 {
		return phase
	}
	ux, uy := dx/length, dy/length
	at := 0.0
	for at < length {
		place := math.Mod(phase+at, period)
		if place < on {
			run := math.Min(on-place, length-at)
			from := [2]float64{a[0] + ux*at, a[1] + uy*at}
			to := [2]float64{a[0] + ux*(at+run), a[1] + uy*(at+run)}
			strokeSegment(r, from, to, width)
			at += run
		} else {
			at += period - place
		}
	}
	return math.Mod(phase+length, period)
}

func moveTo(r *vector.Rasterizer, x, y float64) { r.MoveTo(float32(x), float32(y)) }
func lineTo(r *vector.Rasterizer, x, y float64) { r.LineTo(float32(x), float32(y)) }
func cubeTo(r *vector.Rasterizer, x1, y1, x2, y2, x3, y3 float64) {
	r.CubeTo(float32(x1), float32(y1), float32(x2), float32(y2), float32(x3), float32(y3))
}
