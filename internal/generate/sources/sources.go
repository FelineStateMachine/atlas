// Package sources is the seam every capture source is read through.
//
// A source knows one publisher's shape: how its captures are laid out, what its
// numbers mean, what it refuses to translate. Everything it knows stays inside
// its own directory under this one. What comes out is an Atlas interchange
// document, and from there nothing -- not composition, not enrichment, not the
// application -- can tell which source it read.
//
// The interface is small on purpose. A source is handed an opened archive and
// one volume of it, and hands back a document. It does not choose where the
// archive is, does not decide whether the result is written, and never reaches
// the network: capture is a separate, hand-run, network-touching step, and a
// translator is a pure function of bytes already on disk. That is what lets an
// editorial change replay over years of captures instead of re-crawling them.
//
// The interface is spelled in plain arguments rather than a request struct so
// that a source package can satisfy it without importing this one, which is
// what keeps registration a one-way edge: this package knows the sources, and
// no source knows this package.
//
// Registration is in registry.go, the one file outside a source's own directory
// permitted to name a source. docs/generate.md carries the source-authoring
// guide.
package sources

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// A Source translates one publisher's archived captures into the interchange
// document.
type Source interface {
	// Describe is the source's own account of itself: the slug a ledger names
	// it by, the label a person reads, what the data is licensed under, and
	// which id space its documents' numbers live in. It is copied verbatim into
	// every document the source emits.
	Describe() doc.Provenance

	// Translate reads one volume's captures and emits its document. It is
	// deterministic: the same archived bytes give the same document, on any
	// machine, however many times it is run. The logger is never nil.
	//
	// A volume the source cannot read yet -- a half-finished crawl, a world
	// with no capture -- is reported by wrapping ErrNotReady, which a caller
	// skips. Everything else is a failure: a source that cannot honour its own
	// gates says so rather than translating something it does not understand.
	Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error)
}

// ErrNotReady marks a volume the archive holds but cannot yet answer for. It
// wraps the archive's own signal so a caller has one thing to test.
var ErrNotReady = archive.ErrNotReady

// ErrUnknownSource is reported for a volume whose archive entry names a source
// nothing is registered for.
var ErrUnknownSource = errors.New("no source is registered for this volume")

// For finds the source registered under a name.
func For(name string) (Source, error) {
	for _, source := range All() {
		if source.Describe().Name == name {
			return source, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownSource, name)
}

// Names lists the registered sources, in registration order.
func Names() []string {
	sources := All()
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.Describe().Name)
	}
	return out
}
