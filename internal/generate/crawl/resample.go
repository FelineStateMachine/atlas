package crawl

import (
	"image"
	"image/draw"
	"math"
)

// A deterministic, high-quality downsampler.
//
// The Blue Marble capture cuts a pinned source image down to the reference
// level the deriver folds, and the cut has to be the same bytes on every
// machine: the tiles' content hashes feed the derivation stamp, and the stamp
// feeds the bundle's name. The deriver's own 2×2 box fold cannot do this cut --
// the ratio is not a power of two -- so this file is the one other resampler in
// the lane, and it is built for the same property the fold has: integer
// arithmetic wherever a pixel is decided.
//
// The kernel is Catmull-Rom, widened by the scale factor so a downsample
// averages over the ground a destination pixel covers instead of aliasing
// across it. Tap weights are computed once per output column and row -- the
// only floating-point step -- with every operation explicitly rounded, so the
// compiler may not fuse two of them into an differently-rounded one, and are
// then frozen to 15-bit fixed point, normalized to sum exactly to one.
// Everything per-pixel after that is integer multiply-accumulate.

// resampleShift is the fixed-point precision a frozen weight carries.
const resampleShift = 15

// resampleTaps is the weights one output index takes over the source axis.
type resampleTaps struct {
	first   int
	weights []int32
}

// resample scales src to width×height through the widened Catmull-Rom kernel,
// separably: columns first into a fixed-point intermediate, then rows.
func resample(src *image.NRGBA, width, height int) *image.NRGBA {
	bounds := src.Bounds()
	columns := resampleAxis(bounds.Dx(), width)
	rows := resampleAxis(bounds.Dy(), height)

	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Each output row needs only a window of horizontally-resampled source
	// rows, and output rows walk downward, so the intermediates live in a
	// small cache that trails the walk instead of a whole-image buffer.
	cache := make(map[int][]int32)
	for y := range height {
		taps := rows[y]
		for r := range taps.weights {
			row := taps.first + r
			if _, held := cache[row]; !held {
				cache[row] = resampleRow(src, row, columns, width)
			}
		}
		for column := range width {
			var acc [4]int64
			for r, weight := range taps.weights {
				held := cache[taps.first+r]
				for channel := range 4 {
					acc[channel] += int64(weight) * int64(held[column*4+channel])
				}
			}
			at := out.PixOffset(column, y)
			for channel := range 4 {
				out.Pix[at+channel] = clamp8(acc[channel])
			}
		}
		for row := range cache {
			if row < taps.first {
				delete(cache, row)
			}
		}
	}
	return out
}

// resampleRow runs the horizontal pass over one source row, answering each
// output column's channels in 15-bit fixed point.
func resampleRow(src *image.NRGBA, row int, columns []resampleTaps, width int) []int32 {
	bounds := src.Bounds()
	out := make([]int32, width*4)
	base := src.PixOffset(bounds.Min.X, bounds.Min.Y+row)
	line := src.Pix[base : base+bounds.Dx()*4]
	for column := range width {
		taps := columns[column]
		var acc [4]int32
		for t, weight := range taps.weights {
			at := (taps.first + t) * 4
			for channel := range 4 {
				acc[channel] += weight * int32(line[at+channel])
			}
		}
		copy(out[column*4:], acc[:])
	}
	return out
}

// clamp8 rounds a doubly-scaled accumulator back to a byte, half away from
// zero, clamped to the channel's range.
func clamp8(value int64) uint8 {
	value = (value + 1<<(2*resampleShift-1)) >> (2 * resampleShift)
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

// resampleAxis freezes the tap weights for one axis. Taps are clamped inside
// the source, folding an edge overhang onto the edge sample, which extends the
// picture rather than darkening its border.
func resampleAxis(src, dst int) []resampleTaps {
	scale := float64(src) / float64(dst)
	if scale < 1 {
		scale = 1
	}
	support := float64(2 * scale)
	out := make([]resampleTaps, dst)
	for i := range dst {
		// The output pixel's centre on the source axis.
		center := float64(float64(2*i+1)*float64(src)) / float64(2*dst)
		first := int(math.Ceil(float64(center - support - 0.5)))
		last := int(math.Floor(float64(center + support - 0.5)))
		raw := make([]float64, 0, last-first+1)
		sum := 0.0
		for j := first; j <= last; j++ {
			t := float64(float64(float64(j)+0.5-center) / scale)
			weight := catmullRom(t)
			raw = append(raw, weight)
			sum = float64(sum + weight)
		}
		frozen := freezeWeights(raw, sum)
		// Clamp the window inside the source, folding out-of-range taps onto
		// the edge sample they would have read past.
		taps := resampleTaps{first: first, weights: frozen}
		out[i] = clampTaps(taps, src)
	}
	return out
}

// catmullRom is the kernel, spelled so every floating-point operation rounds on
// its own: an explicit float64 conversion is a rounding point the compiler must
// honour, which keeps a fused multiply-add from computing a differently-rounded
// weight on one architecture than on another.
func catmullRom(t float64) float64 {
	if t < 0 {
		t = -t
	}
	switch {
	case t < 1:
		// 1.5t³ − 2.5t² + 1
		return float64(float64(float64(float64(1.5*t)-2.5)*float64(t*t)) + 1)
	case t < 2:
		// −0.5t³ + 2.5t² − 4t + 2
		return float64(float64(float64(float64(float64(-0.5*t)+2.5)*t)-4)*t + 2)
	default:
		return 0
	}
}

// freezeWeights quantizes one window of weights to fixed point summing exactly
// to one, handing the rounding residual to the heaviest tap.
func freezeWeights(raw []float64, sum float64) []int32 {
	frozen := make([]int32, len(raw))
	total := int32(0)
	heaviest := 0
	for i, weight := range raw {
		frozen[i] = int32(math.RoundToEven(float64(weight/sum) * (1 << resampleShift)))
		total += frozen[i]
		if frozen[i] > frozen[heaviest] {
			heaviest = i
		}
	}
	frozen[heaviest] += 1<<resampleShift - total
	return frozen
}

// clampTaps folds taps that fall outside the source onto its first and last
// samples.
func clampTaps(taps resampleTaps, src int) resampleTaps {
	out := resampleTaps{first: taps.first}
	if out.first < 0 {
		out.first = 0
	}
	last := min(taps.first+len(taps.weights)-1, src-1)
	if out.first > last {
		out.first, last = 0, 0
	}
	out.weights = make([]int32, last-out.first+1)
	for i, weight := range taps.weights {
		at := min(max(taps.first+i, out.first), last)
		out.weights[at-out.first] += weight
	}
	return out
}

// toNRGBA lands a decoded image in the one layout the resampler reads. The
// conversion is the standard library's own integer arithmetic, so a JPEG
// decodes to the same bytes on every machine.
func toNRGBA(src image.Image) *image.NRGBA {
	if held, ok := src.(*image.NRGBA); ok {
		return held
	}
	out := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}
