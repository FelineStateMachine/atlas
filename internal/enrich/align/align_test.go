package align

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// anchors builds a donor set and its image under a known transformation, so a
// test can ask whether the fit recovers what it was given.
func anchors(count int, apply func(x, y float64) (float64, float64)) (donor, base []Anchor) {
	for index := range count {
		x := float64(100 + index*37)
		y := float64(50 + (index*53)%400)
		title := fmt.Sprintf("place %d", index)
		donor = append(donor, Anchor{Title: title, X: x, Y: y})
		bx, by := apply(x, y)
		base = append(base, Anchor{Title: title, X: bx, Y: by})
	}
	return donor, base
}

func TestFitRecoversTheTransformationItWasGiven(t *testing.T) {
	cases := []struct {
		what  string
		apply func(x, y float64) (float64, float64)
	}{
		{"a translation", func(x, y float64) (float64, float64) { return x + 300, y - 120 }},
		{"a scale", func(x, y float64) (float64, float64) { return 2*x + 5, 2*y - 5 }},
		{"a small rotation", func(x, y float64) (float64, float64) {
			const angle = 0.03
			return math.Cos(angle)*x - math.Sin(angle)*y + 10,
				math.Sin(angle)*x + math.Cos(angle)*y - 10
		}},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			donor, base := anchors(40, c.apply)
			affine, report, err := Fit(donor, base)
			if err != nil {
				t.Fatal(err)
			}
			if report.MedianPx > 1e-6 || report.WorstPx > 1e-6 {
				t.Errorf("an exact transformation fitted with residuals: %s", report)
			}
			for _, anchor := range donor {
				wantX, wantY := c.apply(anchor.X, anchor.Y)
				gotX, gotY := affine.Apply(anchor.X, anchor.Y)
				if math.Hypot(gotX-wantX, gotY-wantY) > 1e-6 {
					t.Fatalf("%s landed at %.3f,%.3f, want %.3f,%.3f",
						anchor.Title, gotX, gotY, wantX, wantY)
				}
			}
			back, ok := affine.Invert()
			if !ok {
				t.Fatal("the transformation would not invert")
			}
			x, y := back.Apply(affine.Apply(donor[0].X, donor[0].Y))
			if math.Hypot(x-donor[0].X, y-donor[0].Y) > 1e-6 {
				t.Errorf("a round trip through the inverse landed at %.3f,%.3f", x, y)
			}
		})
	}
}

func TestFitRefusesRatherThanGuesses(t *testing.T) {
	shift := func(x, y float64) (float64, float64) { return x + 10, y + 10 }

	t.Run("too few shared names", func(t *testing.T) {
		donor, base := anchors(MinAnchors-1, shift)
		if _, _, err := Fit(donor, base); err == nil ||
			!strings.Contains(err.Error(), "unambiguous shared names") {
			t.Fatalf("error %v", err)
		}
	})

	t.Run("names that are not shared", func(t *testing.T) {
		donor, base := anchors(30, shift)
		for index := range base {
			base[index].Title = "different " + base[index].Title
		}
		if _, _, err := Fit(donor, base); err == nil {
			t.Fatal("a fit stood on nothing")
		}
	})

	t.Run("a name that appears twice is no anchor", func(t *testing.T) {
		donor, base := anchors(30, shift)
		for index := range donor {
			donor[index].Title = "the same place"
			base[index].Title = "the same place"
		}
		if _, _, err := Fit(donor, base); err == nil {
			t.Fatal("ambiguous names were used as calibration")
		}
	})

	t.Run("collinear anchors are degenerate", func(t *testing.T) {
		var donor, base []Anchor
		for index := range 30 {
			title := fmt.Sprintf("place %d", index)
			donor = append(donor, Anchor{Title: title, X: float64(index), Y: 0})
			base = append(base, Anchor{Title: title, X: float64(index), Y: 0})
		}
		if _, _, err := Fit(donor, base); err == nil {
			t.Fatal("a transformation stood on a line")
		}
	})

	t.Run("an alignment that will not close", func(t *testing.T) {
		donor, base := anchors(40, shift)
		// Scatter the base far beyond what trimming can shed.
		for index := range base {
			base[index].X += float64((index%7)*400) - 800
			base[index].Y -= float64((index%5)*300) - 600
		}
		_, report, err := Fit(donor, base)
		if err == nil {
			t.Fatalf("a fit with a median of %.1fpx was accepted", report.MedianPx)
		}
		if !strings.Contains(err.Error(), "does not close") {
			t.Fatalf("refused for the wrong reason: %v", err)
		}
	})
}

func TestFitTrimsTheWorstAnchors(t *testing.T) {
	donor, base := anchors(40, func(x, y float64) (float64, float64) { return x + 10, y + 10 })
	// Four anchors one source simply placed badly.
	for index := range 4 {
		base[index].X += 900
	}
	affine, report, err := Fit(donor, base)
	if err != nil {
		t.Fatal(err)
	}
	if report.Anchors >= 40 {
		t.Errorf("trimming kept all %d anchors", report.Anchors)
	}
	if report.MedianPx > 1 {
		t.Errorf("the sloppy anchors dragged the fit: %s", report)
	}
	x, y := affine.Apply(1000, 1000)
	if math.Hypot(x-1010, y-1010) > 1 {
		t.Errorf("the fit landed 1000,1000 at %.1f,%.1f", x, y)
	}
}

func TestFitIsPure(t *testing.T) {
	donor, base := anchors(30, func(x, y float64) (float64, float64) { return 1.5*x + 3, 1.5*y - 3 })
	first, firstReport, err := Fit(donor, base)
	if err != nil {
		t.Fatal(err)
	}
	second, secondReport, err := Fit(donor, base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || firstReport != secondReport {
		t.Errorf("two fits of one set disagree:\n%+v %s\n%+v %s",
			first, firstReport, second, secondReport)
	}
}

func TestNormalizeTitleFoldsTheSpellingsApart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Northside Apartment", "northside apartment"},
		{"NORTHSIDE  Apartment", "northside apartment"},
		{"Afterlife - Fast Travel", "afterlife"},
		{"Afterlife Fast Travel Point", "afterlife"},
		{"Ripperdoc: Vik's", "ripperdoc vik s"},
		{"   ", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeTitle(c.in); got != c.want {
			t.Errorf("%q normalized to %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTokensAreTheWordsOfAName(t *testing.T) {
	tokens := Tokens(NormalizeTitle("Northside, Watson Apartment"))
	for _, word := range []string{"northside", "watson", "apartment"} {
		if !tokens[word] {
			t.Errorf("%q is not among the tokens %v", word, tokens)
		}
	}
	if len(Tokens("")) != 0 {
		t.Error("an empty name has tokens")
	}
}

func TestReportReadsAsTheLedgerRecordsIt(t *testing.T) {
	report := Report{Anchors: 99, MedianPx: 26.0, P90Px: 52.0, WorstPx: 67.4}
	const want = "99 anchors, median 26.0px, p90 52.0px, worst 67.4px"
	if got := report.String(); got != want {
		t.Errorf("report reads %q, want %q", got, want)
	}
}
