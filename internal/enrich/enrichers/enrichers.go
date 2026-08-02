// Package enrichers is the set of enrichers a binary offers.
//
// It exists so that the driver never imports an enricher and an enricher never
// imports the driver's registry: the queue is a list of names in curation, this
// is a list of implementations, and enrich.Queue holds the two to each other --
// a name nobody answers to fails, and an implementation nobody queues fails.
package enrichers

import (
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/lenses"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/merge"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/national"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/stdicons"
)

// All is every enricher this binary can run, in no particular order: the order
// is curation's to declare.
func All() []enrich.Enricher {
	return []enrich.Enricher{
		merge.New(),
		national.New(),
		stdicons.New(),
		lenses.New(),
	}
}
