package app

import (
	"sort"
	"testing"
)

// The two orders, held to the browser that recorded the baselines.
//
// Both expectations below were produced by asking a browser the same question
// the reference implementation asked it —
//
//	titles.sort((a, b) => a.localeCompare(b))
//	titles.sort((a, b) => a.localeCompare(b, undefined, {numeric: true, sensitivity: "base"}))
//
// — over titles taken from the six fixture volumes, plus a handful of pairs
// the fixture set does not happen to contain (a case pair, a numeric ladder)
// so the two orders can be told apart here, over this very corpus. The
// input order is part of the fixture: both sorts are stable, and where the
// collation calls two titles equal it is the input order that decides.
var collationInput = []string{
	"¡La Fantoma!",
	"¡La Fantoma! (x4)",
	"??? Fire Sword",
	".357 Magnum Revolver",
	`"Droplet"`,
	"020 - Solitary Moving Stable",
	"101 North",
	"15th Street Trail Connector",
	"9mm Pistol",
	"A.C. Lucas House",
	"Batty’s Hotel",
	"Batty's Hotel",
	"Cádiz",
	"Cañas",
	"Handlová",
	"John's Landmark",
	"Kerry Eurodyne’s Residence",
	"O'Donnel Building",
	"Octavio’s Clinic",
	"Z-43 Innovative Toxins Plant",
	"Zetatech Sandevistan Mk.1",
	"Zetatech Sandevistan Mk.3",
	"apple",
	"Apple",
	"Banana",
	"banana",
	"Level 2",
	"Level 10",
	"Level 1",
}

func TestCompareTitlesIsTheBrowsersOrder(t *testing.T) {
	want := []string{
		"¡La Fantoma!",
		"¡La Fantoma! (x4)",
		"??? Fire Sword",
		".357 Magnum Revolver",
		`"Droplet"`,
		"020 - Solitary Moving Stable",
		"101 North",
		"15th Street Trail Connector",
		"9mm Pistol",
		"A.C. Lucas House",
		"apple",
		"Apple",
		"banana",
		"Banana",
		"Batty's Hotel",
		"Batty’s Hotel",
		"Cádiz",
		"Cañas",
		"Handlová",
		"John's Landmark",
		"Kerry Eurodyne’s Residence",
		"Level 1",
		"Level 10",
		"Level 2",
		"O'Donnel Building",
		"Octavio’s Clinic",
		"Z-43 Innovative Toxins Plant",
		"Zetatech Sandevistan Mk.1",
		"Zetatech Sandevistan Mk.3",
	}
	got := append([]string(nil), collationInput...)
	sort.SliceStable(got, func(i, j int) bool { return compareTitles(got[i], got[j]) < 0 })
	assertOrder(t, want, got)
}

func TestCompareIndexTitlesIsNumericAndCaseBlind(t *testing.T) {
	want := []string{
		"¡La Fantoma!",
		"¡La Fantoma! (x4)",
		"??? Fire Sword",
		".357 Magnum Revolver",
		`"Droplet"`,
		"9mm Pistol",
		"15th Street Trail Connector",
		"020 - Solitary Moving Stable",
		"101 North",
		"A.C. Lucas House",
		"apple",
		"Apple",
		"Banana",
		"banana",
		"Batty’s Hotel",
		"Batty's Hotel",
		"Cádiz",
		"Cañas",
		"Handlová",
		"John's Landmark",
		"Kerry Eurodyne’s Residence",
		"Level 1",
		"Level 2",
		"Level 10",
		"O'Donnel Building",
		"Octavio’s Clinic",
		"Z-43 Innovative Toxins Plant",
		"Zetatech Sandevistan Mk.1",
		"Zetatech Sandevistan Mk.3",
	}
	got := append([]string(nil), collationInput...)
	sort.SliceStable(got, func(i, j int) bool { return compareIndexTitles(got[i], got[j]) < 0 })
	assertOrder(t, want, got)
}

// A byte comparison is what this replaced, and it is worth one line saying so:
// the title the browser puts first is the one `strings.ToLower` puts last.
func TestCollationIsNotAByteOrder(t *testing.T) {
	if compareTitles("¡La Fantoma!", "188 Trading Post") >= 0 {
		t.Fatal("punctuation must sort before digits, as the reference ordered it")
	}
	if compareIndexTitles("Akkala Highlands", "akkala highlands") != 0 {
		t.Fatal("the feature index ignores case, as {sensitivity: base} asked it to")
	}
}

func assertOrder(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("sorted %d titles, expected %d", len(got), len(want))
	}
	for at := range want {
		if got[at] != want[at] {
			t.Fatalf("position %d is %q, the browser put %q there\n got: %q\nwant: %q",
				at, got[at], want[at], got, want)
		}
	}
}
