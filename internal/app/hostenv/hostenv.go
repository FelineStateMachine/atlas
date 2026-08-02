// Package hostenv is the seam between the application and the machine it runs
// on. The application is one pure http.Handler; everything OS-shaped -- the
// library of .atlas files, the place session records are kept, the file
// picker -- arrives through the small interfaces declared here (issue #5
// §3.3).
//
// The rule the interfaces exist to enforce: the handler never touches os,
// file paths, or dialogs. It asks a [VolumeStore] what is installed and opens
// entries out of it; it asks a [SessionStore] for bytes under a name; it asks
// [Hostenv.PickFile] for a file the reader chose. Three hosts can then mount
// the same handler -- a Wails webview, the headless atlas serve, and, later, a
// WASM service worker whose stores are backed by OPFS -- without the handler
// knowing which one it is running under.
//
// This package declares the contract and nothing that binds it to a machine:
// no os, no path/filepath, no toolkit. The OS implementations live beside it
// in hostenv/oshost, so a host that is not an OS links none of them.
package hostenv

import (
	"context"
	"errors"
	"io"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

// ErrNoSession is what a [SessionStore] returns for a record it does not
// hold. A first launch reads every record and finds none; that is not a
// failure, and a store says so with a sentinel rather than an empty result
// that cannot be told from an empty record.
var ErrNoSession = errors.New("hostenv: no such session record")

// ErrNotAvailable is what a host returns for a capability it does not have:
// the headless server has no window to put a file dialog in front of, so its
// PickFile refuses rather than pretending the reader cancelled.
var ErrNotAvailable = errors.New("hostenv: not available on this host")

// Hostenv is everything the application needs from the machine it runs on.
type Hostenv interface {
	// Volumes is the library of installed .atlas files.
	Volumes() VolumeStore

	// Sessions is where per-volume session records are kept.
	Sessions() SessionStore

	// PickFile puts a file picker in front of the reader and returns what
	// they chose: an open reader over the file, the name it was chosen
	// under, and an error. A host with no window returns [ErrNotAvailable];
	// a reader who cancelled gets [ErrNoSelection]. The caller closes the
	// reader.
	PickFile(ctx context.Context) (io.ReadCloser, string, error)
}

// ErrNoSelection is what PickFile returns when the reader closed the picker
// without choosing anything. It is not an error condition -- nothing failed --
// but it is not a file either, and the caller has to be able to tell the two
// apart.
var ErrNoSelection = errors.New("hostenv: nothing was chosen")

// VolumeStore is the library of installed volumes as the handler sees it: the
// serving build of each volume, a way to open entries out of one, and the two
// operations that change what is installed.
//
// The registry model is scan at launch, rescan on import -- no directory
// watching (issue #5 §2, decision 15). A file dropped into the library from
// outside appears at the next launch. The fold that decides which build of a
// volume serves is pure and lives in format/bundle; a store only has to walk
// whatever it keeps its bundles in and hand the fold its descriptors.
type VolumeStore interface {
	// Volumes lists the serving build of every installed volume, one per
	// slug, sorted by slug. The result is a snapshot: it does not change
	// under a request that is already reading it.
	Volumes() []Volume

	// Rescan re-reads the library and reports the volumes whose serving
	// build moved -- arrivals, departures, and winners whose stamp changed
	// (see [bundle.Changed]).
	Rescan() ([]string, error)

	// Install takes bundle content into the library under the name it was
	// offered as, validating before anything is kept, and rescans. A bundle
	// that is already installed is a no-op that still reports what it is.
	Install(name string, content io.Reader) (Installed, error)

	// Location names where the library lives, in words the host's reader
	// would recognise -- a directory path on an OS host, whatever an OPFS
	// host calls its root. The catalog carries it so an empty library can
	// say where a bundle goes. It is a label, never a path the handler
	// takes apart.
	Location() string
}

// Volume is one serving build: what the bundle says about itself, and a door
// to the entries inside it.
type Volume interface {
	// Manifest is the volume's own account of itself, already validated.
	Manifest() bundle.Manifest

	// Open reads one entry of the archive by its full name --
	// "worlds/hyrule.json", "tiles/hyrule/0/0/0.jpg" -- and reports its
	// uncompressed size, so a server can announce a length before streaming.
	Open(entry string) (io.ReadCloser, int64, error)
}

// Installed is what an import turned out to be: the volume it was a build of,
// and what the rescan that followed it changed.
type Installed struct {
	// Slug and Title identify the volume the bundle is a build of.
	Slug  string
	Title string

	// Stamp is the build's content fingerprint, in full.
	Stamp string

	// Bytes is the size of the installed file where the store knows it.
	Bytes int64

	// Already is true when the library already held this exact build, which
	// is a successful import that copied nothing.
	Already bool

	// Changed names the volumes whose serving build moved as a result,
	// which is what the application announces over SSE.
	Changed []string
}

// SessionStore keeps the application's discrete state across restarts: one
// record per volume, plus the small record that remembers which volume was
// last open (issue #5 §4.1).
//
// The store is bytes under a name. What those bytes mean is the handler's
// business -- the record schema is documented in docs/app.md and versioned
// there -- which is what lets a file, an OPFS handle, and a test's map all be
// the same store.
type SessionStore interface {
	// Load reads a record. A store that does not hold it returns
	// [ErrNoSession].
	Load(name string) ([]byte, error)

	// Save writes a record whole, replacing whatever was there.
	Save(name string, data []byte) error

	// Names lists the records held, sorted.
	Names() ([]string, error)
}

// ValidName admits the record names a [SessionStore] may be asked for:
// lowercase letters, digits, hyphen, underscore and dot, not leading with a
// separator and never carrying a path separator.
//
// The rule exists because a name reaches a filesystem on one host and a key
// space on another. Session names are built from volume slugs, which
// [bundle.ValidSlug] already holds to a narrower rule; this is the second
// gate, stated where the store is, so a store implementation may trust it.
func ValidName(name string) error {
	if name == "" {
		return errors.New("empty")
	}
	if name[0] == '-' || name[0] == '_' || name[0] == '.' {
		return errors.New("starts with a separator")
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return errors.New("contains an unusable character")
	}
	return nil
}
