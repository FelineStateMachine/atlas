// Package oshost is the hostenv contract implemented against an operating
// system: a library that is a directory of .atlas files, session records that
// are files, and a file picker the host either has or does not.
//
// It is separate from hostenv proper so that the interfaces stay portable.
// The handler imports hostenv; only a host that runs on a machine with a
// filesystem imports this. A WASM service worker host would sit beside it
// with OPFS-backed stores and the same handler above it (issue #5 §3.3).
package oshost

import (
	"context"
	"fmt"
	"io"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// Host is an OS-backed hostenv.
type Host struct {
	volumes  *Volumes
	sessions hostenv.SessionStore
	picker   Picker
}

// Picker puts a file picker in front of the reader. A host with a window
// supplies one -- the Wails host's native dialog is exactly this function --
// and a headless host supplies nothing, which answers hostenv.ErrNotAvailable.
type Picker func(ctx context.Context) (io.ReadCloser, string, error)

// Options is what an OS host needs to know: where the library is, where
// session records go, and whether there is anyone to show a dialog to.
type Options struct {
	// BundlesDir is the library of .atlas files. It need not exist yet.
	BundlesDir string

	// SessionsDir is where session records are written. Empty means the
	// records live in memory for the length of the process, which is what a
	// dev server pointed at somebody else's library wants: it reads the
	// library and writes nothing near it.
	SessionsDir string

	// Pick is the host's file picker, or nil for a host with no window.
	Pick Picker
}

// New opens a host over the given directories, scanning the library once.
func New(opts Options) (*Host, error) {
	volumes, err := NewVolumes(opts.BundlesDir)
	if err != nil {
		return nil, err
	}
	var sessions hostenv.SessionStore = hostenv.NewMemorySessions()
	if opts.SessionsDir != "" {
		sessions, err = NewSessions(opts.SessionsDir)
		if err != nil {
			return nil, err
		}
	}
	return &Host{volumes: volumes, sessions: sessions, picker: opts.Pick}, nil
}

// Volumes is the library.
func (h *Host) Volumes() hostenv.VolumeStore { return h.volumes }

// Sessions is where session records are kept.
func (h *Host) Sessions() hostenv.SessionStore { return h.sessions }

// PickFile asks the host's picker for a file. A host without one refuses
// rather than pretending the reader cancelled: an import that cannot happen
// and an import nobody wanted are different answers, and the application
// tells the reader which it got.
func (h *Host) PickFile(ctx context.Context) (io.ReadCloser, string, error) {
	if h.picker == nil {
		return nil, "", fmt.Errorf("choosing a bundle: %w", hostenv.ErrNotAvailable)
	}
	return h.picker(ctx)
}
