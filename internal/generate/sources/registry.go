package sources

import (
	"github.com/FelineStateMachine/atlas/internal/generate/sources/arcgishub"
	"github.com/FelineStateMachine/atlas/internal/generate/sources/ign"
	"github.com/FelineStateMachine/atlas/internal/generate/sources/mapgenie"
	"github.com/FelineStateMachine/atlas/internal/generate/sources/nasatrek"
	"github.com/FelineStateMachine/atlas/internal/generate/sources/piggyback"
)

// All is the registered sources, in the order a report lists them.
//
// This is the assembly point, and it is the one file outside a source's own
// directory that is allowed to name a source (issue #5 §5.1). It may name only
// a constructor: no capture kind, no field, no id-space rule, nothing about how
// a source's captures are shaped. Adding a source is one line here, and if it
// needs a second line, the source is leaking.
//
// It is a function rather than a package variable so that registration is
// explicit and ordered, with no init doing it behind a reader's back.
func All() []Source {
	return []Source{
		mapgenie.New(),
		ign.New(),
		piggyback.New(),
		nasatrek.New(),
		arcgishub.New(),
	}
}
