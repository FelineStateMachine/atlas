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
// Atlas the application.
//
// The vocabulary has one machine-readable source, spec/registry.yaml, from
// which three artifacts are cut: registry_gen.go here, the TypeScript lanes'
// key constants, and docs/semconv/REGISTRY.md, the document a reader learns
// the conventions from. Edit the spec, not the artifacts.
package semconv

//go:generate go run ../../spec/gen

// Entity is what an attribute attaches to. A key registered against one
// entity is a producer error on any other, which is what keeps a collection's
// vocabulary from drifting onto its features.
// The entities themselves are in registry_gen.go: which entities exist is
// vocabulary, and vocabulary comes from the spec.
type Entity string

// Stability says how settled a key is. Experimental keys may still change
// spelling or vocabulary; stable keys only break with a [Version] move. The
// tiers, like the entities, are generated.
type Stability string
