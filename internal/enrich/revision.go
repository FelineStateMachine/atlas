package enrich

import "fmt"

// How an enriched build wins the registry fold.
//
// The registry serves the newest build of each volume, and its ordering is
// creation time, then policy revision, then stamp, then locator. Creation time
// is the newest capture the build was made from and never the build clock --
// that is a format invariant and this lane does not touch it. An enrichment of
// the same captures therefore ties on creation time with the plain build beside
// it, and the revision is what has to decide.
//
// # The mechanism
//
// The revision is one integer carrying two policy numbers: the enrich lane's in
// the high field, the generate lane's in the low one.
//
//	revision = enrichPolicy*RevisionSpan + generatePolicy
//
// A plain single-source build writes its generate policy revision alone, which
// is the same number with an enrich policy of zero. So an enriched build of one
// capture always outranks the plain build of that capture, deterministically,
// with no scan of the library and no clock: the number is a pure function of
// the two lanes' compiled-in policy revisions, which is what keeps the
// determinism invariant -- same archive in, same stamp, same file name -- true
// of enriched builds as well.
//
// # Which axis dominates, and why
//
// Packing two axes into one integer makes one of them dominant, and this one
// picks the enrich axis on purpose: within one set of captures an enriched
// build is a superset of the plain build -- it holds everything the plain build
// holds plus what was contributed -- so serving the plain build over it would
// lose data the library already has. A generate policy change therefore takes
// effect for an enriched volume when the pipeline is re-run, which is how a
// policy change reaches a merged volume anyway: enrichment is a pipeline stage,
// not a separate ritual.
//
// RevisionSpan is the room the low field has. It is generous rather than tight
// because the cost of a wrong guess is asymmetric: too much room wastes
// nothing, and too little would silently reorder builds.
const (
	// PolicyRevision is the enrich lane's own policy revision. It moves when a
	// change here should supersede the enriched builds already in every
	// library.
	//
	//	1  the first enriched builds: merge, national, stdicons, lenses
	PolicyRevision = 1

	// RevisionSpan is how many generate policy revisions fit under one enrich
	// policy revision.
	RevisionSpan = 100
)

// BuildRevision is the revision an enriched build carries, given the revision
// the generate lane would have written on its own.
//
// It refuses a generate revision that does not fit the span rather than
// wrapping into the next enrich band, because a silently reordered library is
// exactly the failure this mechanism exists to prevent. A generate lane that
// reaches a hundred policy revisions gets a wider span and one restamp, which
// is a decision somebody makes on purpose.
func BuildRevision(generatePolicy int) (int, error) {
	if generatePolicy < 0 || generatePolicy >= RevisionSpan {
		return 0, fmt.Errorf(
			"generate policy revision %d does not fit the enrich revision span of %d; "+
				"widening the span is a deliberate restamp of every enriched build (see docs/enrich.md)",
			generatePolicy, RevisionSpan)
	}
	return PolicyRevision*RevisionSpan + generatePolicy, nil
}

// Enriched reports whether a build's revision was written by this lane, and
// which enrich policy wrote it. It is what a report uses to say "this build was
// enriched" without opening a payload.
func Enriched(revision int) (policy int, enriched bool) {
	if revision < RevisionSpan {
		return 0, false
	}
	return revision / RevisionSpan, true
}
