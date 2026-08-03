// Package align finds how two readings of one ground line up.
//
// Two captures of one map draw the same places at different scales and offsets:
// a scan of the official map and a rasterized in-game rendering do not share a
// pixel grid, and neither says how to reach the other. What they do share is
// places -- fast travel points, shops, landmarks -- pinned by name in both.
// Every name both readings pin, unambiguously, is a calibration point, and
// enough of them determine the affine transformation between the two pixel
// spaces deterministically: the same anchors always align the same way, with a
// residual report that says how well.
//
// The fit refuses rather than guesses. It stands only on names that are
// unambiguous in both readings, needs enough of them to mean something, trims
// its worst tenth twice to shed the places one side simply pinned sloppily, and
// declines to answer at all when what remains still does not close. A bad
// alignment drawn confidently is worse than none: it moves every feature it
// touches to somewhere plausible and wrong.
package align

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Anchor is one named point in a reading's own world pixels.
type Anchor struct {
	Title string
	X     float64
	Y     float64
}

// Affine carries base = A·donor + t, spelled per axis: x' = AX·x + BX·y + CX,
// and y' likewise. The cross terms carry any small rotation or shear between
// the renderings; north-up readings leave them near zero.
type Affine struct {
	AX, BX, CX float64
	AY, BY, CY float64
}

// Apply sends a point from the fitted reading's space into the base space.
func (a Affine) Apply(x, y float64) (float64, float64) {
	return a.AX*x + a.BX*y + a.CX, a.AY*x + a.BY*y + a.CY
}

// Invert solves the transformation the other way. The false result is a
// degenerate transformation, which nothing may be inverted through.
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

// Scale reports how many base pixels one fitted pixel spans, averaged over the
// axes.
func (a Affine) Scale() float64 {
	return (math.Hypot(a.AX, a.AY) + math.Hypot(a.BX, a.BY)) / 2
}

// Report says what the fit stood on and how well it closed, in base pixels. Its
// string form is what a merge ledger records as the alignment, so two builds
// that aligned the same way say so in the same words.
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

// The fit's own limits.
const (
	// MinAnchors is how many unambiguous shared names a fit needs before it
	// means anything.
	MinAnchors = 12
	// TrimRounds and TrimKeep are the trimming: two rounds, each keeping the
	// nine tenths that fit best, never dropping below MinAnchors.
	TrimRounds = 2
	TrimKeep   = 0.9
	// MaxMedianPx is how far the middle anchor may sit from where the fit
	// predicts before the fit is refused as one that does not close.
	MaxMedianPx = 96
	// minPivot is the smallest pivot the elimination will divide by. Below it
	// the anchors are degenerate -- collinear, or all in one place -- and no
	// transformation stands.
	minPivot = 1e-12
)

// Fit finds the affine sending donor pixels onto base pixels from the places
// both name. It is pure: the same anchors always yield the same answer.
func Fit(donor, base []Anchor) (Affine, Report, error) {
	matchedDonor, matchedBase := matchByTitle(donor, base)
	if len(matchedDonor) < MinAnchors {
		return Affine{}, Report{}, fmt.Errorf(
			"only %d unambiguous shared names; %d needed", len(matchedDonor), MinAnchors)
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
		if round == TrimRounds {
			break
		}
		sort.Slice(keep, func(a, b int) bool {
			return residual(affine, matchedDonor[keep[a]], matchedBase[keep[a]]) <
				residual(affine, matchedDonor[keep[b]], matchedBase[keep[b]])
		})
		trimmed := int(float64(len(keep)) * TrimKeep)
		if trimmed < MinAnchors {
			trimmed = min(MinAnchors, len(keep))
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
	if report.MedianPx > MaxMedianPx {
		return Affine{}, report, fmt.Errorf("alignment does not close: %s", report)
	}
	return affine, report, nil
}

func residual(a Affine, donor, base Anchor) float64 {
	x, y := a.Apply(donor.X, donor.Y)
	return math.Hypot(x-base.X, y-base.Y)
}

// matchByTitle pairs anchors by normalized name, using only names that appear
// exactly once in each reading: a name two features share could pair either
// way, and a guessed pair is a corrupted measurement.
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
		name := NormalizeTitle(anchor.Title)
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

// NormalizeTitle folds the spellings apart readings give one place. The fast
// travel suffix goes because one reading titles the place and another titles
// the service at it, and they are the same anchor. Anything comparing features
// across readings compares through this, so a name means the same thing to the
// fit and to whoever consumes it.
func NormalizeTitle(title string) string {
	title = strings.ToLower(title)
	title = fastTravelSuffix.ReplaceAllString(title, "")
	return strings.TrimSpace(nonAlphanumeric.ReplaceAllString(title, " "))
}

// Tokens is a normalized name's words, as a set.
func Tokens(normalized string) map[string]bool {
	tokens := make(map[string]bool)
	for _, token := range strings.Fields(normalized) {
		tokens[token] = true
	}
	return tokens
}

// solve fits one axis at a time: three unknowns against every kept anchor, by
// the normal equations, eliminated in place.
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
		if math.Abs(pivot) < minPivot {
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
