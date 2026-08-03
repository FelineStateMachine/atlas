// Package enrich makes a volume richer without making it something else.
//
// # The contract
//
// A volume goes in and a new build of the same volume comes out: same slug, new
// stamp. Nothing here mutates a published bundle -- the registry model is a
// library of immutable files and a fold over them, so publication is a new file
// landing beside the old one. Nothing here removes or rewrites what a source
// said. Every contribution is written down in a ledger the build gate audits.
// And no change means no build: an enrichment that finds nothing to say leaves
// the library exactly as it was.
//
// # The seam
//
// The enrich lane and the generate lane never import each other (issue #5
// §3.2). The composed multi-source result is generate ⊕ enrich, and the ⊕ is
// performed by the one binary that holds both: `atlas enrich` translates
// archived captures into interchange documents with the generate lane, adapts
// them into the [Volume] model this package owns, runs the ordered queue,
// applies the contributions, adapts the result back into a document, and hands
// it to composition. Neither lane learns about the other; what travels between
// them is data.
//
// That is why this package has its own volume model rather than taking a
// pointer to the generate lane's document. The model carries the same
// information -- worlds, lenses, collections, features, attributes, provenance
// -- because both are shaped by what the format needs, and the adaptation is a
// mechanical copy that a round-trip test holds to identity.
//
// # The ordered queue
//
// Enrichers run in an order declared in curation data (internal/enrich/curation),
// never in an order that emerges from a map iteration or a registration side
// effect. Ordering is a real decision from the first enricher onward: standard
// icons resolve after a merge so that merged-in collections resolve too, and a
// lens attaches to a world after anything that might have added that world.
//
// # The evidence ethic
//
// Every enricher in this lane is held to one rule: silence over plausibility.
// An enricher that cannot be sure says nothing and, where the doubt is about a
// particular thing, writes down what it was unsure about. A held feature with
// its reason recorded costs a curiosity; a wrong claim costs a place.
package enrich

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// An Enricher contributes to a volume under composition. It declares the
// semantic-conventions keys it may write, which are checked producer-strict; it
// sees the volume and whatever context the run supplies; and it returns
// additions. Every contribution is ledgered.
type Enricher interface {
	// Name is how curation orders it and how a log line names it.
	Name() string
	// Declares lists the attribute keys this enricher may write. An attribute
	// it writes that is not declared here fails the build, so what an enricher
	// can say is visible without reading it.
	Declares() []string
	// Enrich reads the volume and says what it has to add. It must not modify
	// the volume: the driver applies contributions, so every change is one that
	// went through the ledger and the gate.
	Enrich(v *Volume, ctx Context) (Contribution, error)
}

// Context is everything an enricher may consult beyond the volume itself.
//
// Every field is data the caller supplies, which is what keeps this lane
// testable and offline: an enricher never opens a file, never reaches for the
// network, and never learns which source anything came from except through a
// ledger it is handed.
type Context struct {
	// Donors are other readings of the same volume -- other sources' documents,
	// adapted -- for the enricher that folds readings together.
	Donors []*Volume
	// Evidence is the archived material an enricher re-runs its judgement
	// against, so a policy change replays over the capture instead of over the
	// network. Nil is an evidence base with nothing in it.
	Evidence Evidence
	// Lenses offers the pictures derived for this volume's worlds.
	Lenses LensOffers
	// Curation is the lane's editorial data.
	Curation Curation
	// Log is the run's event stream; nil is a logger that discards.
	Log *slog.Logger
}

// Evidence is the archived material an enricher reads. It is an interface
// rather than a directory because the evidence base travels in the capture and
// a test's evidence base travels in the test.
type Evidence interface {
	// Open returns the named evidence, and false where the base does not carry
	// it. Absent evidence is not an error: an enricher whose evidence is not
	// there says nothing, which is the ethic.
	Open(name string) ([]byte, bool, error)
}

// LensOffers reports the pictures derived for one world but not yet attached to
// it: the tile pyramids something rendered, with the stamps that say how.
type LensOffers interface {
	Offers(world string) []Lens
}

// Curation is the enrich lane's editorial data, as the driver and the enrichers
// read it. The concrete tables live in internal/enrich/curation; this interface
// is what keeps a test from having to state the whole corpus to exercise one
// rule.
type Curation interface {
	// Queue is the order the enrichers run in.
	Queue() []string
	// MatchRadiusPx, SeparateRadiusPx and NearbyFloorPx are the resolution
	// distances, in world pixels of the volume's own square.
	MatchRadiusPx() float64
	SeparateRadiusPx() float64
	NearbyFloorPx() float64
	// DonorFillsEmpty reports whether a matched feature takes this attribute
	// from its counterpart when it has none of its own. Everything else is
	// serving-wins.
	DonorFillsEmpty(key string) bool
	// EvidenceCollections lists the collections a national evidence base
	// contributed to a volume, which are the ones it may not then make claims
	// about.
	EvidenceCollections(volume string) []string
}

// Result is what one run of the queue did.
type Result struct {
	// Contributions is every enricher's offering, in queue order, including the
	// empty ones: a run says what each enricher had to say, and "nothing" is an
	// answer.
	Contributions []Contribution
	// Changed reports whether anything at all was contributed. False is the
	// no-change-no-build case, and the caller writes nothing.
	Changed bool
}

// Run drives the queue over one volume.
//
// Each enricher sees the volume as the ones before it left it, which is what
// makes the order meaningful: standard icons resolve for collections a merge
// added, and a lens attaches to a world a merge contributed. Each contribution
// is checked against what the enricher declared, applied, and ledgered, and the
// world's whole ledger is audited at the end.
//
// The volume is modified in place. A caller that wants the original back keeps
// its own copy -- which is what the enrich subcommand does, so that a run that
// gates can say what it would have written.
func Run(v *Volume, queue []Enricher, ctx Context) (Result, error) {
	log := ctx.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op("enrich"), logging.Volume(v.Slug))

	// Every world's own account is opened before anything is contributed: the
	// origin account says what the world arrived with, and once an enricher has
	// run there is no way to find that out again.
	OpenOrigin(v)

	var out Result
	for _, enricher := range queue {
		started := time.Now()
		contribution, err := enricher.Enrich(v, ctx)
		if err != nil {
			return Result{}, fmt.Errorf("enricher %s: %w", enricher.Name(), err)
		}
		contribution.Contribution = ContributionDoc
		contribution.Version = ContributionVersion
		if contribution.Enricher == "" {
			contribution.Enricher = enricher.Name()
		}
		if contribution.Volume == "" {
			contribution.Volume = v.Slug
		}
		if err := declared(enricher, contribution); err != nil {
			return Result{}, fmt.Errorf("enricher %s: %w", enricher.Name(), err)
		}
		if contribution.Empty() {
			log.Debug("enricher had nothing to add", logging.Enricher(enricher.Name()),
				logging.Dur(time.Since(started)))
			out.Contributions = append(out.Contributions, contribution)
			continue
		}
		if err := Apply(v, contribution); err != nil {
			return Result{}, err
		}
		digest, err := contribution.Digest()
		if err != nil {
			return Result{}, err
		}
		out.Contributions = append(out.Contributions, contribution)
		out.Changed = true
		log.Info("enriched", logging.Enricher(enricher.Name()),
			"ops", len(contribution.Ops), "contribution", digest[:12],
			logging.Dur(time.Since(started)))
	}
	for index := range v.Worlds {
		if err := GateWorld(&v.Worlds[index]); err != nil {
			return Result{}, err
		}
	}
	return out, nil
}

// declared holds an enricher to what it said it would write. An attribute key
// outside the declaration fails the build: what an enricher can say about a
// volume is part of its contract, not an emergent property of its code.
func declared(e Enricher, c Contribution) error {
	allowed := make(map[string]bool, len(e.Declares()))
	for _, key := range e.Declares() {
		if _, known := semconv.EntityOf(key); !known {
			return fmt.Errorf("declares %q, which the conventions registry does not know", key)
		}
		allowed[key] = true
	}
	for _, op := range c.Ops {
		if op.Kind != OpSetAttr {
			continue
		}
		if !allowed[op.Key] {
			return fmt.Errorf("writes %q, which it does not declare", op.Key)
		}
	}
	return nil
}

// Queue resolves the declared order against the enrichers a binary offers.
//
// It is strict both ways. A name curation declares that nothing implements is a
// build failure, because the queue is the statement of what runs. An enricher
// the binary offers that curation does not name is also a failure, because an
// enricher that runs in an order nobody wrote down is folklore, which is the
// thing the ordered queue exists to prevent.
func Queue(order []string, offered []Enricher) ([]Enricher, error) {
	available := make(map[string]Enricher, len(offered))
	for _, enricher := range offered {
		if _, twice := available[enricher.Name()]; twice {
			return nil, fmt.Errorf("two enrichers are called %s", enricher.Name())
		}
		available[enricher.Name()] = enricher
	}
	queue := make([]Enricher, 0, len(order))
	named := make(map[string]bool, len(order))
	for _, name := range order {
		enricher, held := available[name]
		if !held {
			return nil, fmt.Errorf("curation queues %q, which no enricher answers to", name)
		}
		if named[name] {
			return nil, fmt.Errorf("curation queues %q twice", name)
		}
		named[name] = true
		queue = append(queue, enricher)
	}
	for _, enricher := range offered {
		if !named[enricher.Name()] {
			return nil, fmt.Errorf("enricher %s runs in no declared order; "+
				"add it to the curated queue or stop offering it", enricher.Name())
		}
	}
	return queue, nil
}
