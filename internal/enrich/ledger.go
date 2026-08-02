package enrich

import (
	"fmt"
	"sort"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// The ledger is how the enrich lane accounts for itself.
//
// Every contribution to a world is written down in one vocabulary, whatever
// made it: what was recognized as something the world already held, what was
// added, what joined a collection the world already had, what was held back
// undecided, and what was refused outright. The vocabulary is small on purpose
// -- five words -- because a ledger nobody can hold in their head is a log, and
// a log is not an account.
//
//	matched   the world already held this, and the two are one thing
//	added     the world did not hold this, and now does
//	adopted   an added thing joined a collection the world already had
//	held      the contribution could not decide, and says why
//	rejected  the contribution refused this outright, and says why
//
// The gate below reads an account back and fails the build when it does not add
// up. A merge that quietly lost a feature, or quietly agreed with itself twice
// about one place, is worse than a merge that did not happen.

// Account is one contributor's account of what it did to one world. The origin
// account is the world's own: where the ground came from, and what it held when
// it got there.
type Account struct {
	// Source is the contributor spelled for a person, Slug the name a registry
	// knows it by.
	Source string `json:"source"`
	Slug   string `json:"slug,omitempty"`
	// Origin marks the account of the source the world itself came from, as
	// against a contribution folded in later.
	Origin bool `json:"origin,omitempty"`
	// DonorFeatures is the whole offering counted per kind. On an origin
	// account it is simply the world's own tally.
	DonorFeatures Counts `json:"donorFeatures"`
	// The order of the fields from here down is the order they are written in,
	// and it is frozen rather than incidental: an account rides a world payload
	// and a world payload is a stamped part, so re-spelling one restamps every
	// build in every library.
	Matched []MatchedPair `json:"matched,omitempty"`
	// Added is how many point features the world did not have and now has.
	Added int `json:"added"`
	// AddedShapes holds the ledger's place for the day a shape feature merges.
	// Nothing writes it yet, and the gate refuses a non-zero value rather than
	// letting a number nobody produced pass for an account.
	AddedShapes int              `json:"addedShapes,omitempty"`
	Adopted     []AdoptedItem    `json:"adopted,omitempty"`
	Held        []HeldItem       `json:"held,omitempty"`
	Rejected    []HeldItem       `json:"rejected,omitempty"`
	Enriched    []CollectionTake `json:"enrichedCategories,omitempty"`
	// Alignment is what the fit this account stood on reported, where it stood
	// on one.
	Alignment string `json:"alignment,omitempty"`
	// Corrections are the ledgered removals of data that was wrong. Nothing in
	// this lane removes anything, so they are always empty today; they exist
	// because the maturity gate's one permitted decrease is a correction, and a
	// permitted decrease has to be permitted by something written down.
	Corrections []Correction `json:"corrections,omitempty"`
}

// Counts is features by kind, as a ledger speaks of them.
type Counts struct {
	Point int `json:"point"`
	Path  int `json:"path"`
	Area  int `json:"area"`
}

// Total is every kind together.
func (c Counts) Total() int { return c.Point + c.Path + c.Area }

// MatchedPair records one thing both readings hold: the contributor's feature,
// the serving feature it resolved to, how far apart the alignment put them, and
// every attribute the serving feature took from the contributor's.
type MatchedPair struct {
	Donor      int64 `json:"d"`
	Winner     int64 `json:"w"`
	DistancePx int   `json:"px"`
	// Enriched marks a pair whose serving feature had nothing to say and took
	// the contributor's prose. It is derived from Took and kept because readers
	// of the older spelling still ask for it.
	Enriched bool     `json:"e,omitempty"`
	Took     []string `json:"took,omitempty"`
}

// AdoptedItem records a contributed feature that joined one of the serving
// world's own collections: provenance for a feature the legend does not single
// out.
type AdoptedItem struct {
	Donor int64  `json:"d"`
	Into  string `json:"into"`
}

// HeldItem is a contributed feature that was not carried, with the reason.
type HeldItem struct {
	Donor  int64  `json:"d"`
	Title  string `json:"t"`
	Reason string `json:"why"`
}

// CollectionTake records one attribute a serving collection took from its
// counterpart: which collection, which key.
type CollectionTake struct {
	Collection string `json:"cat"`
	Key        string `json:"k"`
}

// Correction is data removed because it was wrong, and why. The maturity gate
// permits a decrease worth this many points; nothing else in the lane reads it.
type Correction struct {
	World  string `json:"world,omitempty"`
	Reason string `json:"reason"`
	Points int    `json:"points"`
}

// HeldShapeReason is the one reason a contributed shape feature is held:
// matching is point-only, so every contributed path and area goes on the record
// rather than vanishing. The gate reads the reason back off the ledger to tell
// shape holds from point holds.
const HeldShapeReason = "shape features do not merge yet"

// MatchedN, AdoptedN, HeldN and RejectedN are the counts a report prints.
func (a Account) MatchedN() int  { return len(a.Matched) }
func (a Account) AdoptedN() int  { return len(a.Adopted) }
func (a Account) HeldN() int     { return len(a.Held) }
func (a Account) RejectedN() int { return len(a.Rejected) }

// MedianMatchPx is the middle distance of the account's matched pairs: the
// figure that says how well the alignment held, unmoved by one outlier.
func (a Account) MedianMatchPx() int {
	if len(a.Matched) == 0 {
		return 0
	}
	distances := make([]int, 0, len(a.Matched))
	for _, pair := range a.Matched {
		distances = append(distances, pair.DistancePx)
	}
	sort.Ints(distances)
	return distances[len(distances)/2]
}

// EnrichedN is how many matched pairs took prose they did not have.
func (a Account) EnrichedN() int {
	count := 0
	for _, pair := range a.Matched {
		if pair.Enriched {
			count++
		}
	}
	return count
}

// GateAccount is the ledger's own audit of one account: every offered feature
// of every kind accounted for, every match one-to-one, and every attribute a
// take claims answering to the registry.
//
// It fails the build rather than letting a bundle be written that quietly lost
// something -- or quietly agreed too much.
func GateAccount(a Account) error {
	if a.Origin {
		if a.Added != 0 || len(a.Matched) > 0 || len(a.Held) > 0 || len(a.Rejected) > 0 {
			return fmt.Errorf("the origin account of %s claims a contribution; "+
				"an origin account only says what the world arrived with", a.Source)
		}
		return nil
	}
	heldPoints, heldShapes := 0, 0
	for _, held := range a.Held {
		if held.Reason == HeldShapeReason {
			heldShapes++
		} else {
			heldPoints++
		}
	}
	accounted := len(a.Matched) + a.Added + heldPoints + len(a.Rejected)
	if accounted != a.DonorFeatures.Point {
		return fmt.Errorf("%s accounts for %d of %d offered points", a.Source, accounted, a.DonorFeatures.Point)
	}
	if shapes := a.DonorFeatures.Path + a.DonorFeatures.Area; heldShapes != shapes {
		return fmt.Errorf("%s holds %d shape features of the %d it offered", a.Source, heldShapes, shapes)
	}
	if a.AddedShapes != 0 {
		return fmt.Errorf("%s claims %d added shapes; no shape feature merges yet", a.Source, a.AddedShapes)
	}
	claimed := make(map[int64]int64, len(a.Matched))
	for _, pair := range a.Matched {
		if first, taken := claimed[pair.Winner]; taken {
			return fmt.Errorf("serving feature %d matched by %d and %d; a place is one place",
				pair.Winner, first, pair.Donor)
		}
		claimed[pair.Winner] = pair.Donor
		tookProse := false
		for _, key := range pair.Took {
			if key == semconv.KeyNoteText {
				tookProse = true
				continue
			}
			if entity, known := semconv.EntityOf(key); !known || entity != semconv.EntityFeature {
				return fmt.Errorf("pair %d took %q, which no feature may carry", pair.Donor, key)
			}
		}
		if pair.Enriched != tookProse {
			return fmt.Errorf("pair %d says enriched=%t but its takes say %t",
				pair.Donor, pair.Enriched, tookProse)
		}
	}
	for _, take := range a.Enriched {
		if entity, known := semconv.EntityOf(take.Key); !known || entity != semconv.EntityCollection {
			return fmt.Errorf("collection %q took %q, which no collection may carry", take.Collection, take.Key)
		}
	}
	return nil
}

// GateWorld holds a world to its own ledger: one identifier space across every
// kind, and a tally that is the sum of what every account claims. The counts a
// world reports are not fields that may drift -- they are what its accounts add
// up to, and the world has to actually hold that many of each kind.
func GateWorld(w *World) error {
	seen := make(map[int64]string)
	var counted Counts
	for _, collection := range w.Collections {
		for _, feature := range collection.Features {
			if holder, taken := seen[feature.ID]; taken {
				return fmt.Errorf("world %s: feature id %d held by both %q and %q",
					w.Slug, feature.ID, holder, feature.Title)
			}
			seen[feature.ID] = feature.Title
			switch collection.Kind {
			case KindPoint:
				counted.Point++
			case KindPath:
				counted.Path++
			default:
				counted.Area++
			}
		}
	}
	var ledgered Counts
	for _, account := range w.Ledger {
		if err := GateAccount(account); err != nil {
			return fmt.Errorf("world %s: %w", w.Slug, err)
		}
		if account.Origin {
			ledgered.Point += account.DonorFeatures.Point
			ledgered.Path += account.DonorFeatures.Path
			ledgered.Area += account.DonorFeatures.Area
			continue
		}
		ledgered.Point += account.Added
	}
	if counted.Point != ledgered.Point {
		return fmt.Errorf("world %s holds %d points but its ledger claims %d",
			w.Slug, counted.Point, ledgered.Point)
	}
	if counted.Path != ledgered.Path || counted.Area != ledgered.Area {
		return fmt.Errorf("world %s holds %d paths and %d areas but its ledger claims %d and %d",
			w.Slug, counted.Path, counted.Area, ledgered.Path, ledgered.Area)
	}
	return nil
}

// OpenOrigin opens each world's own account: where its ground came from, and
// what it held when it got there.
//
// Composition opens one of these itself for a volume nobody enriched. A volume
// that goes through the queue has to open its accounts first, before anything
// is contributed, because the origin account says what the world arrived with
// -- and after an enricher has run, nothing can tell what that was.
//
// A world that already has an account is left alone: this is opening a ledger,
// not rewriting one.
func OpenOrigin(v *Volume) {
	for index := range v.Worlds {
		world := &v.Worlds[index]
		if len(world.Ledger) > 0 {
			continue
		}
		world.Ledger = []Account{{
			Source:        v.Source.Label,
			Slug:          v.Source.Name,
			Origin:        true,
			DonorFeatures: Tally(world),
		}}
	}
}

// Tally counts a world's features by kind.
func Tally(w *World) Counts {
	var out Counts
	for _, collection := range w.Collections {
		switch collection.Kind {
		case KindPoint:
			out.Point += len(collection.Features)
		case KindPath:
			out.Path += len(collection.Features)
		default:
			out.Area += len(collection.Features)
		}
	}
	return out
}
