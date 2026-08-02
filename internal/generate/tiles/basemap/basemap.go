// Package basemap draws a city's ground from the city's own vector data.
//
// A bundle owes its reader a surface to stand on, and the offline invariant
// (issue #5 §2) means no tile service may be reached for one. A city has no
// tile server of its own either: what it publishes is geometry -- limits,
// zoning, water, streets, trails -- so the picture is drawn here, from that
// geometry, into the deepest level of the lens's pyramid. Every shallower level
// then folds down from it exactly as any other pyramid's does, which is why
// this package draws one level and knows nothing about pyramids.
//
// # Determinism
//
// The drawing is a pure function of the shapes handed to it. Roles draw in a
// fixed order; within a role, shapes are unioned rather than painted over, so
// the order they arrive in cannot be read off the result; the arithmetic is
// float64 down to the rasterizer's own float32 edge; and the encoder runs with
// fixed settings. The same shapes make the same bytes, which is what lets a
// derivation be compared against the capture that witnessed it.
//
// The one thing outside this package's control is the toolchain: a change to
// the standard library's DEFLATE would re-spell the PNG bytes once without
// moving a pixel. That is why the deriver compares content hashes and the
// fixtures carry decoded-pixel digests beside them -- the two together tell a
// re-encoding from a picture that actually moved.
package basemap

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"golang.org/x/image/vector"
)

// The grid the drawing is cut on: the corpus's tile edge, and the world square
// every source's picture is measured in. They are repeated rather than imported
// so this package stands alone as the thing that draws a tile.
const (
	TileSize  = 256
	WorldSize = 8192
)

// Bleed is how far past its own edge a tile draws, in tile pixels: wide enough
// for the widest stroke's half plus its antialiasing, so a cut never lands on a
// pixel the tile keeps and two neighbours continue each other exactly.
const Bleed = 8

// Feature is one thing to draw, in world pixels of the square: ground as rings
// -- the first the outline, the rest holes -- and linework as polylines.
// Emphasis scales the role's stroke width for this one shape, an arterial
// against a lane; zero means the role's own width, unremarkable.
type Feature struct {
	Role     Role
	Rings    [][][2]float64
	Lines    [][][2]float64
	Emphasis float64
}

// Renderer draws the tiles of one level of one city. Build it once per level
// and ask it for tiles: it normalizes the shapes' winding once and indexes them
// by the tile they touch, so drawing a tile visits only the shapes that reach
// it rather than the whole city.
type Renderer struct {
	features []Feature
	zoom     int
	touching map[[2]int][]int
}

// NewRenderer prepares a level's shapes for drawing. It takes ownership of the
// slice: ring winding is normalized in place, once, rather than on every tile.
func NewRenderer(features []Feature, zoom int) *Renderer {
	r := &Renderer{features: features, zoom: zoom, touching: make(map[[2]int][]int)}
	across := 1 << zoom
	worldTile := float64(WorldSize) / float64(across)

	for at := range r.features {
		feature := &r.features[at]
		normalizeWinding(feature.Rings)
		reach := span(feature.Rings).join(span(feature.Lines))
		if reach.empty() {
			continue
		}
		// A stroke reaches past its own centreline, so the reach is widened by
		// the bleed before it is indexed: a wide boundary has to be found by
		// the neighbouring tile it spills into, not only by the tiles its
		// centreline crosses.
		margin := Bleed * worldTile / TileSize
		firstX := tileOf(reach.minX-margin, worldTile, across)
		lastX := tileOf(reach.maxX+margin, worldTile, across)
		firstY := tileOf(reach.minY-margin, worldTile, across)
		lastY := tileOf(reach.maxY+margin, worldTile, across)
		for x := firstX; x <= lastX; x++ {
			for y := firstY; y <= lastY; y++ {
				key := [2]int{x, y}
				r.touching[key] = append(r.touching[key], at)
			}
		}
	}
	return r
}

// tileOf is which tile a world coordinate falls in, held inside the level.
func tileOf(world, worldTile float64, across int) int {
	tile := int(math.Floor(world / worldTile))
	return min(max(tile, 0), across-1)
}

// Tile draws one tile of the level the renderer was built for. Every tile of
// the level exists: one nothing touches is the flat background, and the deriver
// wants the level complete so it has something to fold down from. Empty tiles
// cost almost nothing, and the deriver omits the one they all share anyway.
func (r *Renderer) Tile(x, y int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(Background), image.Point{}, draw.Src)

	worldTile := float64(WorldSize) / float64(int(1)<<r.zoom)
	scale := TileSize / worldTile
	originX, originY := float64(x)*worldTile, float64(y)*worldTile
	// The window the geometry is cut to, bled past the tile on every side.
	window := box{
		minX: originX - Bleed/scale,
		minY: originY - Bleed/scale,
		maxX: originX + worldTile + Bleed/scale,
		maxY: originY + worldTile + Bleed/scale,
	}
	// Widths are spelled at the reference zoom, so a level deeper draws them
	// twice as wide -- which, folded back down, is one constant width on the
	// ground.
	widthScale := math.Pow(2, float64(r.zoom-ReferenceZoom))

	local := func(p [2]float64) [2]float64 {
		return [2]float64{(p[0] - originX) * scale, (p[1] - originY) * scale}
	}
	// How many tile pixels one world pixel spans, measured through the very
	// transform the geometry crosses, so the dash rhythm is counted in the
	// units it is drawn in.
	pixels := math.Abs(local([2]float64{1, 0})[0] - local([2]float64{0, 0})[0])

	for _, role := range zOrder {
		style := styles[role]
		// One rasterizer per role for the ground and one for the linework, made
		// only if the role has anything on this tile. Every shape of a role
		// accumulates into the same path, and the rasterizer saturates, so a
		// role's shapes are unioned and their order cannot be seen.
		var ground, lines *vector.Rasterizer

		for _, at := range r.candidates(x, y) {
			feature := &r.features[at]
			if feature.Role != role {
				continue
			}
			if style.Fill.A > 0 && len(feature.Rings) > 0 {
				if ground == nil {
					ground = vector.NewRasterizer(TileSize, TileSize)
				}
				for _, ring := range feature.Rings {
					clipped := clipRing(ring, window)
					if clipped == nil {
						continue
					}
					head := local(clipped[0])
					moveTo(ground, head[0], head[1])
					for _, corner := range clipped[1:] {
						corner := local(corner)
						lineTo(ground, corner[0], corner[1])
					}
					ground.ClosePath()
				}
			}
			if style.Stroke.A == 0 || style.StrokeWidth <= 0 {
				continue
			}
			width := style.StrokeWidth * widthScale
			if feature.Emphasis > 0 {
				width *= feature.Emphasis
			}
			if lines == nil {
				lines = vector.NewRasterizer(TileSize, TileSize)
			}
			stroke(lines, feature.Lines, window, local, pixels, width, style.Dash, widthScale)
			// A filled role that also carries a stroke wears it as a rim around
			// its own ground -- but only when the shape brought no linework of
			// its own to spend it on.
			if len(feature.Lines) == 0 && len(feature.Rings) > 0 {
				stroke(lines, closed(feature.Rings), window, local, pixels, width, style.Dash, widthScale)
			}
		}

		if ground != nil {
			ground.Draw(img, img.Bounds(), image.NewUniform(style.Fill), image.Point{})
		}
		if lines != nil {
			lines.Draw(img, img.Bounds(), image.NewUniform(style.Stroke), image.Point{})
		}
	}
	return img
}

// candidates is the shapes that reach one tile, in the order they were given.
func (r *Renderer) candidates(x, y int) []int { return r.touching[[2]int{x, y}] }

// stroke lays every polyline of one shape into the path: each is clipped
// segment by segment, and what survives is stroked as capsules.
//
// Clipping a segment at a time rather than the polyline as a whole is what
// keeps a dashed line's rhythm agreeing across a tile seam. The phase advances
// by the whole of every segment, drawn or not, and a segment whose head was cut
// off starts at the phase its lost lead had already carried -- so the dash a
// neighbouring tile draws lands in exactly the same place.
func stroke(
	into *vector.Rasterizer,
	paths [][][2]float64,
	window box,
	local func([2]float64) [2]float64,
	pixels, width float64,
	dash [2]float64,
	widthScale float64,
) {
	scaled := [2]float64{dash[0] * widthScale, dash[1] * widthScale}
	period := scaled[0] + scaled[1]

	for _, path := range paths {
		phase := 0.0
		for at := 0; at+1 < len(path); at++ {
			from, to, kept := clipSegment(path[at], path[at+1], window)
			if kept {
				a, b := local(from), local(to)
				if dash[0] > 0 {
					lead := math.Hypot(from[0]-path[at][0], from[1]-path[at][1]) * pixels
					dashedCapsules(into, a, b, width, scaled, math.Mod(phase+lead, max(period, 1)))
				} else {
					capsule(into, a, b, width)
				}
			}
			if period > 0 {
				length := math.Hypot(path[at+1][0]-path[at][0], path[at+1][1]-path[at][1]) * pixels
				phase = math.Mod(phase+length, period)
			}
		}
	}
}

// closed spells rings as polylines that return to where they began, which is
// what stroking a shape's rim means.
func closed(rings [][][2]float64) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		path := make([][2]float64, 0, len(ring)+1)
		path = append(path, ring...)
		if ring[0] != ring[len(ring)-1] {
			path = append(path, ring[0])
		}
		out = append(out, path)
	}
	return out
}

// EncodePNG spells a tile with the settings the archive is written under:
// slowest and smallest, because a tile is encoded once and read forever, and
// fixed, because a settings knob left to a caller is a way for two runs of the
// same pipeline to disagree.
func EncodePNG(img *image.NRGBA) ([]byte, error) {
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BackgroundColor reports the flat ground, so the deriver's placeholder
// detection and this drawing cannot disagree about what empty looks like.
func BackgroundColor() color.NRGBA { return Background }
