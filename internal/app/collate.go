package app

import (
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// The order titles are read in.
//
// The reference implementation sorted in a browser, with
// `String.prototype.localeCompare`, and the parity baselines record the answer
// it gave: punctuation before digits before letters, `¡La Fantoma!` at the top
// of a list that a byte comparison starts with `188 Trading Post`. That is the
// CLDR root collation, not an ASCII ordering, and there is no version of
// `strings.ToLower` that arrives at it.
//
// WHY A DEPENDENCY. `golang.org/x/text/collate` implements the same root
// collation the browser does, and it was already in this module's graph --
// `golang.org/x/text` arrives with the desktop host and is listed indirect in
// go.mod -- so the cost of using it is one line moving from the indirect block
// to the direct one. The alternative was a hand-written weight table, and a
// hand-written table is a re-implementation of DUCET that would be wrong for
// the first title nobody thought of. The corpus is not hypothetical: eighty-five
// of the fixture set's titles are not ASCII.
//
// TWO ORDERS, because the reference used two:
//
//   - the panel's list (`frontend/src/visibility.js`) sorted with a plain
//     `localeCompare`, so case and accents separate two titles that would
//     otherwise tie;
//   - a shape collection's feature index (`frontend/src/areas.js`) sorted with
//     `{ numeric: true, sensitivity: "base" }`, so `Level 2` precedes
//     `Level 10` and case is not a difference at all.
//
// Both are reproduced here rather than collapsed into one, even though the
// fixture corpus cannot tell them apart (every shape title in it is ASCII and
// only two carry digits): the difference is in the reference and a fixture set
// that cannot see it today is not an argument for losing it.

// titleCollator is the tertiary-strength root collation the panel's list used.
var titleCollator = collate.New(language.Und)

// indexCollator is primary strength: case and diacritics are not differences,
// which is what `sensitivity: "base"` asks for.
var indexCollator = collate.New(language.Und, collate.IgnoreCase, collate.IgnoreDiacritics)

// quoteFold maps the typographic quotation marks onto their ASCII twins.
//
// The root collation gives `'` and `’` the same primary weight and separates
// them only at the end, which x/text (comparing at tertiary strength) spells as
// a difference in the wrong direction: it sorts every `Miner's` before every
// `Miner’s` rather than sorting both under one `Miner`. Folding the pair before
// the compare puts them back on one primary, and the raw-string tiebreak below
// restores the order the browser gave two titles that differ in nothing else.
var quoteFold = strings.NewReplacer(
	"’", "'", "‘", "'", "“", `"`, "”", `"`,
)

// compareTitles is `left.localeCompare(right)`: the order the panel lists what
// the map is drawing.
func compareTitles(left, right string) int {
	if held := titleCollator.CompareString(quoteFold.Replace(left), quoteFold.Replace(right)); held != 0 {
		return held
	}
	// Two titles equal under the fold differ only in which quotation mark they
	// spell, and the browser separates them: the ASCII one first, which is the
	// order of the code points themselves.
	return strings.Compare(left, right)
}

// compareIndexTitles is `left.localeCompare(right, undefined, {numeric: true,
// sensitivity: "base"})`: the order the siblings of a feature index stand in.
//
// The numeric half is a rewrite rather than a second comparison. A run of
// digits becomes the same number written to a fixed width, so the collation
// that follows puts `Level 2` before `Level 10` without ever being told that
// either is a number -- and, crucially, everything around the run goes on being
// compared in the one pass the collation is defined over. Comparing digit runs
// separately, segment by segment, is what gets `Hydrant 3x` and
// `Hydrant [Chest]` the wrong way round: a bracket and a digit have to meet
// each other, and they cannot if one of them has been taken out of the string
// first.
//
// It is done here rather than with `collate.Numeric` because that option
// mis-ranks a run whose value is zero: `Manual Page 0 / 1` lands after
// `Manual Page 54 / 55`.
func compareIndexTitles(left, right string) int {
	return indexCollator.CompareString(
		numericKey(quoteFold.Replace(left)),
		numericKey(quoteFold.Replace(right)),
	)
}

// numericWidth is the width every run of digits is written to. Twenty digits
// is past any number a title carries and past what a float could hold anyway;
// a longer run is left alone rather than truncated, which costs it the numeric
// reading and keeps it comparable.
const numericWidth = 20

// numericKey rewrites every run of digits as that number, zero-padded, so that
// comparing the keys as text compares the numbers as numbers. Leading zeros go
// with it: `020` and `20` are one number written two ways.
func numericKey(title string) string {
	if !strings.ContainsAny(title, "0123456789") {
		return title
	}
	var out strings.Builder
	out.Grow(len(title))
	for at := 0; at < len(title); {
		if !isDigit(title[at]) {
			out.WriteByte(title[at])
			at++
			continue
		}
		run, rest := digitRun(title[at:])
		digits := strings.TrimLeft(run, "0")
		if digits == "" {
			digits = "0"
		}
		if len(digits) > numericWidth {
			out.WriteString(run)
		} else {
			out.WriteString(strings.Repeat("0", numericWidth-len(digits)))
			out.WriteString(digits)
		}
		at = len(title) - len(rest)
	}
	return out.String()
}

func isDigit(character byte) bool { return character >= '0' && character <= '9' }

// digitRun splits the leading run of digits off a string.
func digitRun(text string) (string, string) {
	at := 0
	for at < len(text) && isDigit(text[at]) {
		at++
	}
	return text[:at], text[at:]
}
