package maturity

import (
	"fmt"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// Comparison is what the monotonicity gate found.
type Comparison struct {
	// Before and After are the two builds' totals.
	Before, After int
	// Delta is After minus Before.
	Delta int
	// Allowance is how much decrease the later build's ledger accounts for.
	Allowance int
	// Comparable is false when the two were scored under different point table
	// versions, in which case nothing is concluded.
	Comparable bool
	// Reasons are the corrections the allowance came from.
	Reasons []string
}

// Compare measures two scores against each other without deciding anything.
func Compare(before, after *Score) Comparison {
	out := Comparison{Before: before.Total, After: after.Total, Delta: after.Total - before.Total}
	out.Comparable = before.TableVersion == after.TableVersion
	for _, line := range after.Ledger {
		for _, correction := range line.Account.Corrections {
			out.Allowance += correction.Points
			out.Reasons = append(out.Reasons, correction.Reason)
		}
	}
	return out
}

// Gate is the build gate: an enrichment whose score declines fails.
//
// Two builds are compared only under the same point table version. A
// re-weighting is a new version of the table, not a mass failure of every build
// in the library, so a comparison across versions concludes nothing and passes.
//
// A decline is permitted up to what the later build's ledger accounts for in
// corrections. That is the one shape a decrease may take, and it has to be
// written down: the gate exists to reward good data, never to punish the
// removal of data that was wrong.
func Gate(before, after *Score) error {
	comparison := Compare(before, after)
	if !comparison.Comparable {
		return nil
	}
	if comparison.Delta >= 0 {
		return nil
	}
	if -comparison.Delta <= comparison.Allowance {
		return nil
	}
	return fmt.Errorf(
		"maturity fell from %d to %d (%d points) and the ledger accounts for %d of it: "+
			"a build that lost quality does not supersede the build it came from "+
			"(see docs/enrich.md, the monotonicity gate)",
		comparison.Before, comparison.After, -comparison.Delta, comparison.Allowance)
}

// Serving picks the build a registry would serve out of several scores of one
// volume: the registry's own fold, restated over measurements, so a report
// lists builds in the order a reader would actually meet them.
func Serving(scores []*Score) *Score {
	var winner *Score
	for _, score := range scores {
		if winner == nil || newer(score, winner) {
			winner = score
		}
	}
	return winner
}

// newer is bundle.Newer over scores. The descriptor is built rather than
// re-implemented, so the ordering has exactly one definition and it lives in
// the format.
func newer(a, b *Score) bool {
	return bundle.Newer(descriptorOf(a), descriptorOf(b))
}

func descriptorOf(s *Score) bundle.Descriptor {
	return bundle.Descriptor{
		Locator:   s.Path,
		Slug:      s.Volume,
		Title:     s.Title,
		Stamp:     s.Stamp,
		CreatedAt: s.CreatedAt,
		Revision:  s.Revision,
	}
}
