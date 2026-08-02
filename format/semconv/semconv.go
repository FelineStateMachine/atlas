// Package semconv is the registry of Atlas's semantic conventions: the
// attribute keys that carry display and geometry meaning through a bundle as
// a shared language, the way OpenTelemetry's semantic conventions carry
// meaning between systems that never met.
//
// The conventions are Atlas's own, so the format is beholden to no upstream's
// habits. Every rule that would otherwise be an unspoken promotion inside one
// tool is a named key any producer can write and any reader can act on.
//
// The contract has two sides, deliberately asymmetric:
//
//   - Producers are strict. An attribute in the atlas namespace that the
//     registry does not know, or a value outside its vocabulary, or a key
//     attached to the wrong entity, fails the build. [Validate] is that gate.
//   - Readers are lenient. An unknown key is ignored, never refused, so a
//     bundle written by a newer pipeline still opens in an older reader.
//     [EntityOf] reporting false is a reader's cue to skip, not to fail.
//
// New keys arrive with [Experimental] stability and earn [Stable]. Only a
// breaking change to an existing key's meaning moves [Version].
//
// The package depends on the Go standard library alone and knows nothing of
// Atlas the application. docs/semconv/REGISTRY.md is its prose twin; a test
// holds the two to the same vocabulary.
package semconv

// Version names the vocabulary a bundle was written against. It rides the
// manifest as "conventions" and moves only when an existing key's meaning
// breaks -- additions ride on per-key stability instead.
//
// v2 collapsed the v1 entities zone, category, and location into collection
// and feature when the format unified them, and renamed atlas.category.key to
// atlas.collection.key to match.
const Version = 2

// Entity is what an attribute attaches to. A key registered against one
// entity is a producer error on any other, which is what keeps a collection's
// vocabulary from drifting onto its features.
type Entity string

// The entities attributes attach to, from the widest to the narrowest.
const (
	EntityBundle     Entity = "bundle"
	EntityWorld      Entity = "world"
	EntityCollection Entity = "collection"
	EntityFeature    Entity = "feature"
)

// Stability says how settled a key is. Experimental keys may still change
// spelling or vocabulary; stable keys only break with a [Version] move.
type Stability string

// The stability tiers.
const (
	Stable       Stability = "stable"
	Experimental Stability = "experimental"
)
