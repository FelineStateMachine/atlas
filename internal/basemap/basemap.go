// Package basemap draws a city's ground from its own vector data: an
// offline bundle owes its reader a raster to stand on, and no outside tile
// service is welcome in one, so the streets, trails and boundaries a city
// publishes are rendered into the tile pyramid themselves.
//
// The renderer is deliberately deterministic. Features draw in a fixed
// z-order, within a role in the order given, onto NRGBA pixels, and the
// encoder runs with fixed settings -- so the same data and the same style
// code make byte-identical tiles within one Go toolchain, and the crawler's
// hash-compare writes keep the archive churn-free. A toolchain bump may
// re-spell the PNG bytes once; the pixels do not change.
//
// Only the deepest level of a pyramid is ever rendered: tools/tiles folds
// every lower level down from the deepest complete one and ignores captured
// intermediates, so rendering them would be work the pipeline throws away.
// Stroke widths are spelled at a reference zoom and scaled with depth,
// which under fold-down reads as constant width on the ground.
package basemap

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"golang.org/x/image/vector"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
)

// Feature is one thing to draw: ground as rings, lines as polylines, in
// world pixels of the 8192 square. Emphasis scales the role's stroke --
// zero means one, the unremarkable width.
type Feature struct {
	Role     Role
	Rings    [][][2]float64
	Lines    [][][2]float64
	Emphasis float64
}

// Renderer draws tiles of one city's basemap. Build it once per render with
// NewRenderer and ask it for tiles; it holds a spatial index over the
// deepest level so a tile only visits the features that touch it.
type Renderer struct {
	features []Feature
	maxZoom  int
	// index buckets feature ordinals by deepest-level tile, the level the
	// crawler actually renders.
	index map[[2]int][]int
}

// NewRenderer prepares features for drawing. Ring winding is normalized
// here, once: the first ring of a polygon is ground and winds one way,
// every further ring is a hole and winds the other, whatever the source
// said -- real exports do not reliably follow the GeoJSON convention.
func NewRenderer(features []Feature, maxZoom int) *Renderer {
	r := &Renderer{features: features, maxZoom: maxZoom, index: make(map[[2]int][]int)}
	tiles := 1 << maxZoom
	worldTile := float64(mgdoc.WorldSize) / float64(tiles)
	for at := range r.features {
		feature := &r.features[at]
		normalizeWinding(feature.Rings)
		reach := bounds(feature.Rings)
		lineReach := bounds(feature.Lines)
		reach.minX = min(reach.minX, lineReach.minX)
		reach.minY = min(reach.minY, lineReach.minY)
		reach.maxX = max(reach.maxX, lineReach.maxX)
		reach.maxY = max(reach.maxY, lineReach.maxY)
		if reach.minX > reach.maxX {
			continue
		}
		// A stroke reaches beyond its centerline; index with a margin so a
		// wide boundary still lands in the neighbouring tile it bleeds into.
		margin := strokeMargin * worldTile / tileSize
		firstX := clampTile(int(math.Floor((reach.minX-margin)/worldTile)), tiles)
		lastX := clampTile(int(math.Floor((reach.maxX+margin)/worldTile)), tiles)
		firstY := clampTile(int(math.Floor((reach.minY-margin)/worldTile)), tiles)
		lastY := clampTile(int(math.Floor((reach.maxY+margin)/worldTile)), tiles)
		for x := firstX; x <= lastX; x++ {
			for y := firstY; y <= lastY; y++ {
				key := [2]int{x, y}
				r.index[key] = append(r.index[key], at)
			}
		}
	}
	return r
}

// tileSize is the pipeline's tile edge.
const tileSize = 256

// strokeMargin is the bleed a tile draws beyond its own edge, in tile
// pixels: wide enough for the widest stroke's half plus antialiasing, so
// the cut never shows and neighbours continue each other exactly.
const strokeMargin = 8

func clampTile(tile, tiles int) int {
	if tile < 0 {
		return 0
	}
	if tile >= tiles {
		return tiles - 1
	}
	return tile
}

// normalizeWinding forces a polygon's rings into the drawing's convention:
// ground winds positive, holes negative, so the rasterizer's winding
// accumulation cuts the holes out.
func normalizeWinding(rings [][][2]float64) {
	for at, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		area := signedArea(ring)
		if at == 0 && area < 0 || at > 0 && area > 0 {
			reverse(ring)
		}
	}
}

// Tile draws one tile of one level. Every tile exists -- a tile nothing
// touches is the flat background -- because the pipeline needs the deepest
// level complete, and the empty ones cost almost nothing to keep.
func (r *Renderer) Tile(zoom, x, y int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(Background), image.Point{}, draw.Src)

	worldTile := float64(mgdoc.WorldSize) / float64(int(1)<<zoom)
	scale := tileSize / worldTile
	originX := float64(x) * worldTile
	originY := float64(y) * worldTile
	window := box{
		minX: originX - strokeMargin/scale,
		minY: originY - strokeMargin/scale,
		maxX: originX + worldTile + strokeMargin/scale,
		maxY: originY + worldTile + strokeMargin/scale,
	}
	widthScale := math.Pow(2, float64(zoom-referenceZoom))

	var candidates []int
	if zoom == r.maxZoom {
		candidates = r.index[[2]int{x, y}]
	} else {
		candidates = make([]int, 0, len(r.features))
		for at := range r.features {
			candidates = append(candidates, at)
		}
	}

	local := func(p [2]float64) [2]float64 {
		return [2]float64{(p[0] - originX) * scale, (p[1] - originY) * scale}
	}

	for _, role := range zOrder {
		style := styles[role]
		var fills, strokes *vector.Rasterizer
		for _, at := range candidates {
			feature := &r.features[at]
			if feature.Role != role {
				continue
			}
			if style.Fill.A > 0 && len(feature.Rings) > 0 {
				if fills == nil {
					fills = vector.NewRasterizer(tileSize, tileSize)
				}
				for _, ring := range feature.Rings {
					clipped := clipRing(ring, window)
					if clipped == nil {
						continue
					}
					first := local(clipped[0])
					moveTo(fills, first[0], first[1])
					for _, p := range clipped[1:] {
						at := local(p)
						lineTo(fills, at[0], at[1])
					}
					fills.ClosePath()
				}
			}
			if style.Stroke.A > 0 && style.StrokeWidth > 0 {
				width := style.StrokeWidth * widthScale
				if feature.Emphasis > 0 {
					width *= feature.Emphasis
				}
				if strokes == nil {
					strokes = vector.NewRasterizer(tileSize, tileSize)
				}
				strokePaths(strokes, feature.Lines, window, local, width, style.Dash, widthScale)
				// A filled role with a rim strokes its ring outlines too.
				if len(feature.Lines) == 0 && len(feature.Rings) > 0 {
					strokePaths(strokes, closedPaths(feature.Rings), window, local, width, style.Dash, widthScale)
				}
			}
		}
		if fills != nil {
			fills.Draw(img, img.Bounds(), image.NewUniform(style.Fill), image.Point{})
		}
		if strokes != nil {
			strokes.Draw(img, img.Bounds(), image.NewUniform(style.Stroke), image.Point{})
		}
	}
	return img
}

// strokePaths clips each polyline to the bled window and strokes what
// survives, keeping the dash rhythm riding along each path.
func strokePaths(r *vector.Rasterizer, paths [][][2]float64, window box, local func([2]float64) [2]float64, width float64, dash [2]float64, widthScale float64) {
	scaledDash := [2]float64{dash[0] * widthScale, dash[1] * widthScale}
	period := scaledDash[0] + scaledDash[1]
	lscale := localScale(local)
	for _, path := range paths {
		phase := 0.0
		for at := 0; at+1 < len(path); at++ {
			from, to, kept := clipSegment(path[at], path[at+1], window)
			if kept {
				a, b := local(from), local(to)
				if dash[0] > 0 {
					// The clipped head keeps the phase its cut-off lead
					// carried, so neighbouring tiles agree on the rhythm.
					lead := math.Hypot(from[0]-path[at][0], from[1]-path[at][1]) * lscale
					strokeDashed(r, a, b, width, scaledDash, math.Mod(phase+lead, max(period, 1)))
				} else {
					strokeSegment(r, a, b, width)
				}
			}
			// The rhythm advances by the whole segment whether or not its
			// middle was drawn here.
			length := math.Hypot(path[at+1][0]-path[at][0], path[at+1][1]-path[at][1]) * lscale
			if period > 0 {
				phase = math.Mod(phase+length, period)
			}
		}
	}
}

// localScale reports how many tile pixels one world pixel spans under the
// local transform -- the transform is a uniform scale, so any unit segment
// measures it.
func localScale(local func([2]float64) [2]float64) float64 {
	a := local([2]float64{0, 0})
	b := local([2]float64{1, 0})
	return math.Abs(b[0] - a[0])
}

// closedPaths spells rings as polylines whose last point returns to the
// first, for stroking a filled shape's rim.
func closedPaths(rings [][][2]float64) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		closed := make([][2]float64, 0, len(ring)+1)
		closed = append(closed, ring...)
		if ring[0] != ring[len(ring)-1] {
			closed = append(closed, ring[0])
		}
		out = append(out, closed)
	}
	return out
}

// EncodePNG spells a tile once, with fixed settings: archive tiles are
// written once and read forever, so the slowest, smallest compression is
// the right trade, and fixing it is part of determinism.
func EncodePNG(img *image.NRGBA) ([]byte, error) {
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BackgroundColor reports the flat ground color, for the checks that the
// pipeline's placeholder detection sees the same background this package
// painted.
func BackgroundColor() color.NRGBA {
	return Background
}
