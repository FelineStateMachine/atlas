// Package blend finds how two sources' pictures of the same ground line up.
//
// Two captures of one game map draw the same places at different scales and
// offsets: a scan of the official map and a rasterized in-game rendering do
// not share a pixel grid, and neither says how to reach the other. What they
// do share is places -- fast travel points, shops, landmarks -- pinned by
// name in both. Every name both sources pin, unambiguously, is a calibration
// point, and enough of them determine the affine transformation between the
// two pixel spaces deterministically: the same captures always align the
// same way, with a residual report that says how well.
package blend

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Anchor is one named point in a source's own world pixels.
type Anchor struct {
	Title string
	X     float64
	Y     float64
}

// Affine carries base = A·donor + t, spelled per axis:
// x' = AX·x + BX·y + CX, and y' likewise. The cross terms carry any small
// rotation or shear between the renderings; north-up sources leave them
// near zero.
type Affine struct {
	AX, BX, CX float64
	AY, BY, CY float64
}

// Apply sends a donor-space point into base space.
func (a Affine) Apply(x, y float64) (float64, float64) {
	return a.AX*x + a.BX*y + a.CX, a.AY*x + a.BY*y + a.CY
}

// Invert solves the transformation the other way.
func (a Affine) Invert() (Affine, bool) {
	det := a.AX*a.BY - a.BX*a.AY
	if math.Abs(det) < 1e-12 {
		return Affine{}, false
	}
	inv := Affine{
		AX: a.BY / det, BX: -a.BX / det,
		AY: -a.AY / det, BY: a.AX / det,
	}
	inv.CX = -(inv.AX*a.CX + inv.BX*a.CY)
	inv.CY = -(inv.AY*a.CX + inv.BY*a.CY)
	return inv, true
}

// Scale reports how many base pixels one donor pixel spans, averaged over
// the axes.
func (a Affine) Scale() float64 {
	sx := math.Hypot(a.AX, a.AY)
	sy := math.Hypot(a.BX, a.BY)
	return (sx + sy) / 2
}

// Report says what the fit stood on and how well it closed, in base pixels.
type Report struct {
	Anchors  int
	MedianPx float64
	P90Px    float64
	WorstPx  float64
}

func (r Report) String() string {
	return fmt.Sprintf("%d anchors, median %.1fpx, p90 %.1fpx, worst %.1fpx",
		r.Anchors, r.MedianPx, r.P90Px, r.WorstPx)
}

// The fit stands only on names that are unambiguous in both sources, needs
// enough of them to mean something, trims its worst tenth twice to shed the
// pins one source simply placed sloppily, and refuses to answer at all when
// what remains still does not close: a bad alignment drawn confidently is
// worse than none.
const (
	minAnchors     = 12
	trimRounds     = 2
	trimKeep       = 0.9
	maxMedianPx    = 96
	maxConditioned = 1e-12
)

// Fit finds the affine sending donor pixels onto base pixels from the places
// both name. It is pure: the same anchors always yield the same answer.
func Fit(donor, base []Anchor) (Affine, Report, error) {
	matchedDonor, matchedBase := matchByTitle(donor, base)
	if len(matchedDonor) < minAnchors {
		return Affine{}, Report{}, fmt.Errorf(
			"only %d unambiguous shared names; %d needed", len(matchedDonor), minAnchors)
	}

	keep := make([]int, len(matchedDonor))
	for index := range keep {
		keep[index] = index
	}
	var affine Affine
	for round := 0; ; round++ {
		var ok bool
		affine, ok = solve(matchedDonor, matchedBase, keep)
		if !ok {
			return Affine{}, Report{}, fmt.Errorf("anchors are degenerate; no transformation stands")
		}
		if round == trimRounds {
			break
		}
		sort.Slice(keep, func(a, b int) bool {
			return residual(affine, matchedDonor[keep[a]], matchedBase[keep[a]]) <
				residual(affine, matchedDonor[keep[b]], matchedBase[keep[b]])
		})
		trimmed := int(float64(len(keep)) * trimKeep)
		if trimmed < minAnchors {
			trimmed = min(minAnchors, len(keep))
		}
		keep = keep[:trimmed]
	}

	residuals := make([]float64, len(keep))
	for at, index := range keep {
		residuals[at] = residual(affine, matchedDonor[index], matchedBase[index])
	}
	sort.Float64s(residuals)
	report := Report{
		Anchors:  len(keep),
		MedianPx: residuals[len(residuals)/2],
		P90Px:    residuals[int(float64(len(residuals))*0.9)],
		WorstPx:  residuals[len(residuals)-1],
	}
	if report.MedianPx > maxMedianPx {
		return Affine{}, report, fmt.Errorf(
			"alignment does not close: %s", report)
	}
	return affine, report, nil
}

func residual(a Affine, donor, base Anchor) float64 {
	x, y := a.Apply(donor.X, donor.Y)
	return math.Hypot(x-base.X, y-base.Y)
}

// matchByTitle pairs anchors by normalized name, using only names that
// appear exactly once in each source: a name two pins share could pair
// either way, and a guessed pair is a corrupted measurement.
func matchByTitle(donor, base []Anchor) ([]Anchor, []Anchor) {
	donorByName := uniqueByName(donor)
	baseByName := uniqueByName(base)
	names := make([]string, 0, len(donorByName))
	for name := range donorByName {
		if _, shared := baseByName[name]; shared {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	matchedDonor := make([]Anchor, len(names))
	matchedBase := make([]Anchor, len(names))
	for at, name := range names {
		matchedDonor[at] = donorByName[name]
		matchedBase[at] = baseByName[name]
	}
	return matchedDonor, matchedBase
}

func uniqueByName(anchors []Anchor) map[string]Anchor {
	counts := make(map[string]int, len(anchors))
	held := make(map[string]Anchor, len(anchors))
	for _, anchor := range anchors {
		name := normalizeTitle(anchor.Title)
		if name == "" {
			continue
		}
		counts[name]++
		held[name] = anchor
	}
	for name, count := range counts {
		if count > 1 {
			delete(held, name)
		}
	}
	return held
}

var (
	fastTravelSuffix = regexp.MustCompile(`\s*[-–—]?\s*fast travel( point)?$`)
	nonAlphanumeric  = regexp.MustCompile(`[^a-z0-9]+`)
)

// normalizeTitle folds the spellings apart sources give one place. The fast
// travel suffix goes because one source titles the place and another titles
// the service at it, and they are the same anchor.
func normalizeTitle(title string) string {
	title = strings.ToLower(title)
	title = fastTravelSuffix.ReplaceAllString(title, "")
	return strings.TrimSpace(nonAlphanumeric.ReplaceAllString(title, " "))
}

// solve fits one axis at a time: three unknowns against every kept anchor,
// by the normal equations, eliminated in place.
func solve(donor, base []Anchor, keep []int) (Affine, bool) {
	xRow, ok := solveAxis(donor, base, keep, func(a Anchor) float64 { return a.X })
	if !ok {
		return Affine{}, false
	}
	yRow, ok := solveAxis(donor, base, keep, func(a Anchor) float64 { return a.Y })
	if !ok {
		return Affine{}, false
	}
	return Affine{
		AX: xRow[0], BX: xRow[1], CX: xRow[2],
		AY: yRow[0], BY: yRow[1], CY: yRow[2],
	}, true
}

func solveAxis(donor, base []Anchor, keep []int, target func(Anchor) float64) ([3]float64, bool) {
	var sxx, sxy, syy, sx, sy, n float64
	var stx, sty, st float64
	for _, index := range keep {
		x, y := donor[index].X, donor[index].Y
		t := target(base[index])
		sxx += x * x
		sxy += x * y
		syy += y * y
		sx += x
		sy += y
		n++
		stx += t * x
		sty += t * y
		st += t
	}
	matrix := [3][3]float64{{sxx, sxy, sx}, {sxy, syy, sy}, {sx, sy, n}}
	vector := [3]float64{stx, sty, st}
	for i := range 3 {
		pivot := matrix[i][i]
		if math.Abs(pivot) < maxConditioned {
			return [3]float64{}, false
		}
		for j := i + 1; j < 3; j++ {
			factor := matrix[j][i] / pivot
			for k := range 3 {
				matrix[j][k] -= factor * matrix[i][k]
			}
			vector[j] -= factor * vector[i]
		}
	}
	var out [3]float64
	for i := 2; i >= 0; i-- {
		out[i] = vector[i]
		for k := i + 1; k < 3; k++ {
			out[i] -= matrix[i][k] * out[k]
		}
		out[i] /= matrix[i][i]
	}
	return out, true
}
