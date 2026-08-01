package blend

import (
	"fmt"
	"math"
	"testing"
)

// truth is the kind of transformation two renderings of one map actually
// differ by: a scale, an offset, and a whisper of shear.
var truth = Affine{
	AX: 1.38, BX: 0.02, CX: -1620,
	AY: 0.005, BY: 1.41, CY: -2050,
}

// jitter is deterministic pin sloppiness: sources place the same shop a few
// pixels apart, and the fit has to see through that without any randomness
// in the test.
func jitter(seed int) (float64, float64) {
	return float64((seed*37)%17) - 8, float64((seed*53)%19) - 9
}

func anchors() ([]Anchor, []Anchor) {
	var donor, base []Anchor
	for i := range 60 {
		x := float64(500 + (i%10)*700)
		y := float64(400 + (i/10)*1200)
		donor = append(donor, Anchor{Title: fmt.Sprintf("Place %d", i), X: x, Y: y})
		bx, by := truth.Apply(x, y)
		jx, jy := jitter(i)
		base = append(base, Anchor{Title: fmt.Sprintf("place %d", i), X: bx + jx, Y: by + jy})
	}
	return donor, base
}

func TestFitRecoversTheTransformation(t *testing.T) {
	donor, base := anchors()
	affine, report, err := Fit(donor, base)
	if err != nil {
		t.Fatal(err)
	}
	// The fit sees jittered pins, so it recovers the truth to within the
	// jitter's reach, not exactly.
	if math.Abs(affine.AX-truth.AX) > 0.01 || math.Abs(affine.BY-truth.BY) > 0.01 {
		t.Fatalf("scale drifted: %+v", affine)
	}
	if math.Abs(affine.CX-truth.CX) > 25 || math.Abs(affine.CY-truth.CY) > 25 {
		t.Fatalf("offset drifted: %+v", affine)
	}
	if report.MedianPx > 15 {
		t.Fatalf("fit did not close: %s", report)
	}

	again, _, err := Fit(donor, base)
	if err != nil || again != affine {
		t.Fatalf("fit is not deterministic: %+v then %+v (%v)", affine, again, err)
	}
}

func TestFitShedsOutliers(t *testing.T) {
	donor, base := anchors()
	// A handful of pins one source simply misplaced.
	for i := 0; i < 5; i++ {
		base[i*7].X += 900
		base[i*7].Y -= 700
	}
	affine, report, err := Fit(donor, base)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(affine.AX-truth.AX) > 0.02 {
		t.Fatalf("outliers pulled the fit: %+v", affine)
	}
	if report.WorstPx > 60 {
		t.Fatalf("an outlier survived the trim: %s", report)
	}
}

func TestFitRefusesThinEvidence(t *testing.T) {
	donor, base := anchors()
	if _, _, err := Fit(donor[:5], base[:5]); err == nil {
		t.Fatal("five anchors were enough for a transformation")
	}

	// Ambiguous names carry no evidence: every anchor named alike must drop,
	// leaving too few.
	for i := range donor {
		if i >= 10 {
			donor[i].Title = "Duplicate"
			base[i].Title = "Duplicate"
		}
	}
	if _, _, err := Fit(donor, base); err == nil {
		t.Fatal("ambiguous names were treated as calibration points")
	}
}

func TestFitRefusesNonsense(t *testing.T) {
	donor, base := anchors()
	// The same names placed with no geometric relationship at all.
	for i := range base {
		base[i].X = float64((i * 811) % 8000)
		base[i].Y = float64((i * 977) % 8000)
	}
	if _, _, err := Fit(donor, base); err == nil {
		t.Fatal("a fit that cannot close was still offered")
	}
}

func TestNormalizeTitleFoldsFastTravelSpellings(t *testing.T) {
	cases := map[string]string{
		"Kennedy North - Fast Travel Point": "kennedy north",
		"Kennedy North Fast Travel":         "kennedy north",
		"KENNEDY  NORTH":                    "kennedy north",
		"Skippy, The Talking Gun ":          "skippy the talking gun",
	}
	for given, want := range cases {
		if got := normalizeTitle(given); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", given, got, want)
		}
	}
}

func TestInvertRoundTrips(t *testing.T) {
	inverse, ok := truth.Invert()
	if !ok {
		t.Fatal("the truth is singular")
	}
	x, y := truth.Apply(1234, 5678)
	backX, backY := inverse.Apply(x, y)
	if math.Abs(backX-1234) > 1e-6 || math.Abs(backY-5678) > 1e-6 {
		t.Fatalf("round trip landed at %.6f,%.6f", backX, backY)
	}
}
