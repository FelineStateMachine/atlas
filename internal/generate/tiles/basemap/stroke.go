package basemap

import (
	"math"

	"golang.org/x/image/vector"
)

// How a line becomes an area.
//
// There is no stroker here in the usual sense -- no joins, no miter limit, no
// offset curves. Every segment of a polyline is added to the path as its own
// closed capsule: the segment widened to the stroke width, with a semicircular
// cap at each end. Capsules overlap at a vertex, and because they all wind the
// same way and the rasterizer saturates rather than alternating, the overlap
// reads as one continuous line with a round join for free. It is the cheapest
// stroker that never shows a seam, which is what a deterministic offline
// renderer wants.

// kappa is the control-point offset that makes four cubic Béziers spell a
// circle as closely as cubics can.
const kappa = 0.5522847498307936

// capsule adds one segment's stroked outline to the path, in tile pixels.
func capsule(into *vector.Rasterizer, a, b [2]float64, width float64) {
	half := width / 2
	dx, dy := b[0]-a[0], b[1]-a[1]
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	// The unit direction, and the left normal already scaled to the half
	// width. Because the normal carries the half width, kappa applied to it is
	// the same offset k as along the tangent.
	ux, uy := dx/length, dy/length
	nx, ny := -uy*half, ux*half
	k := kappa * half

	moveTo(into, a[0]+nx, a[1]+ny)
	lineTo(into, b[0]+nx, b[1]+ny)
	// The far cap: two quarter arcs from b's left shoulder, around the nose at
	// b + u*half, to b's right shoulder.
	cubeTo(into,
		b[0]+nx+ux*k, b[1]+ny+uy*k,
		b[0]+ux*half+nx*kappa, b[1]+uy*half+ny*kappa,
		b[0]+ux*half, b[1]+uy*half)
	cubeTo(into,
		b[0]+ux*half-nx*kappa, b[1]+uy*half-ny*kappa,
		b[0]-nx+ux*k, b[1]-ny+uy*k,
		b[0]-nx, b[1]-ny)
	lineTo(into, a[0]-nx, a[1]-ny)
	// And the near cap, back around a - u*half.
	cubeTo(into,
		a[0]-nx-ux*k, a[1]-ny-uy*k,
		a[0]-ux*half-nx*kappa, a[1]-uy*half-ny*kappa,
		a[0]-ux*half, a[1]-uy*half)
	cubeTo(into,
		a[0]-ux*half+nx*kappa, a[1]-uy*half+ny*kappa,
		a[0]+nx-ux*k, a[1]+ny-uy*k,
		a[0]+nx, a[1]+ny)
	into.ClosePath()
}

// dashedCapsules walks a segment in the dash rhythm and strokes only the
// on-runs. The phase is handed in and rides along the polyline, so the rhythm
// survives a vertex instead of restarting at every corner.
func dashedCapsules(into *vector.Rasterizer, a, b [2]float64, width float64, dash [2]float64, phase float64) {
	on, off := dash[0], dash[1]
	period := on + off
	if period <= 0 {
		capsule(into, a, b, width)
		return
	}
	dx, dy := b[0]-a[0], b[1]-a[1]
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	ux, uy := dx/length, dy/length
	for at := 0.0; at < length; {
		place := math.Mod(phase+at, period)
		if place >= on {
			at += period - place
			continue
		}
		run := math.Min(on-place, length-at)
		capsule(into,
			[2]float64{a[0] + ux*at, a[1] + uy*at},
			[2]float64{a[0] + ux*(at+run), a[1] + uy*(at+run)},
			width)
		at += run
	}
}

// The rasterizer speaks float32; every coordinate crosses that boundary in one
// place so the narrowing is a property of the drawing rather than a detail
// scattered through it.
func moveTo(into *vector.Rasterizer, x, y float64) { into.MoveTo(float32(x), float32(y)) }
func lineTo(into *vector.Rasterizer, x, y float64) { into.LineTo(float32(x), float32(y)) }
func cubeTo(into *vector.Rasterizer, x1, y1, x2, y2, x3, y3 float64) {
	into.CubeTo(float32(x1), float32(y1), float32(x2), float32(y2), float32(x3), float32(y3))
}
