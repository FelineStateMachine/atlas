package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// BasePath is the URL prefix game content is served under. It appears in the
// composed catalog so the frontend never assembles a content URL from parts
// it has to guess.
const BasePath = "/data/g"

// closeGrace is how long a retired bundle stays open after a rescan replaces
// it. A request served from the old snapshot finishes in milliseconds; the
// grace period outlives any of them without keeping descriptors around.
const closeGrace = 5 * time.Second

// Registry holds the bundles of one directory and answers for them as a
// single catalog. Every rescan builds a complete immutable snapshot and swaps
// it in whole, so a request sees one consistent world however the directory
// is churning.
type Registry struct {
	dir  string
	snap atomic.Pointer[Snapshot]

	// rescan runs alone: two concurrent scans of one directory would race to
	// swap snapshots and could close each other's bundles.
	rescan sync.Mutex

	// onChange, when set, is told which games a rescan added, replaced, or
	// removed. Set it before watching begins; it is read under the rescan
	// lock.
	onChange func(changed []string)
}

// Snapshot is the registry's world at one moment: the winning bundle per
// game and the catalog those winners compose.
type Snapshot struct {
	// Games maps each game slug to its bundle.
	Games map[string]*Bundle
	// Catalog is the merged /data/catalog.json body, marshaled once at scan
	// time because every launch fetches it and it only changes here.
	Catalog []byte
}

// catalogGame is one game as the composed catalog lists it. The base URL
// carries the version stamp, so everything beneath it may be cached as
// immutable and a new version of the game arrives at new URLs.
type catalogGame struct {
	Slug     string     `json:"slug"`
	Title    string     `json:"title"`
	Stamp    string     `json:"stamp"`
	Base     string     `json:"base"`
	TileGrid TileGrid   `json:"tileGrid"`
	Maps     []MapEntry `json:"maps"`
}

// NewRegistry answers for dir, which need not exist yet: a directory that is
// missing or empty is a catalog with no games, not an error.
func NewRegistry(dir string) *Registry {
	registry := &Registry{dir: dir}
	registry.snap.Store(&Snapshot{Games: map[string]*Bundle{}, Catalog: composeCatalog(dir, nil)})
	return registry
}

// Dir is the directory the registry scans.
func (r *Registry) Dir() string { return r.dir }

// SetOnChange registers the listener a rescan tells about arrivals, updates,
// and departures. Call it before Watch.
func (r *Registry) SetOnChange(fn func(changed []string)) {
	r.rescan.Lock()
	defer r.rescan.Unlock()
	r.onChange = fn
}

// Snapshot returns the current world. The result is immutable; callers keep
// it for the length of one request and load afresh for the next.
func (r *Registry) Snapshot() *Snapshot {
	return r.snap.Load()
}

// Rescan reads the directory whole and swaps in what it finds. A bundle that
// fails to open is logged and skipped rather than failing the scan: one bad
// download should not take the library down with it. Bundles retired by the
// swap close after a grace period, so requests already streaming from them
// finish undisturbed.
func (r *Registry) Rescan() error {
	r.rescan.Lock()
	defer r.rescan.Unlock()

	paths, err := filepath.Glob(filepath.Join(r.dir, "*.atlas"))
	if err != nil {
		return fmt.Errorf("scan %s: %w", r.dir, err)
	}
	sort.Strings(paths)

	previous := r.snap.Load()
	next := &Snapshot{Games: make(map[string]*Bundle, len(paths))}
	opened := make([]*Bundle, 0, len(paths))
	for _, path := range paths {
		// A bundle already serving is not reopened: the file it was opened
		// from still holds its content even if the name has since been
		// rewritten, and its manifest still describes it. A changed file
		// arrives as a fresh open below and wins or loses on its manifest.
		loaded, err := openOrCarry(previous, path)
		if err != nil {
			slog.Warn("atlas: skipping bundle", "path", path, "error", err)
			continue
		}
		opened = append(opened, loaded)
		if standing, ok := next.Games[loaded.Manifest.Game.Slug]; !ok || MoreRecent(loaded, standing) {
			if ok {
				slog.Info("atlas: bundle shadowed",
					"game", loaded.Manifest.Game.Slug, "shadowed", standing.Path)
			}
			next.Games[loaded.Manifest.Game.Slug] = loaded
		}
	}
	next.Catalog = composeCatalog(r.dir, next.Games)
	r.snap.Store(next)

	// What the rescan changed, by game: an arrival, a departure, or a winner
	// whose version stamp moved.
	var changed []string
	for slug, winner := range next.Games {
		before, had := previous.Games[slug]
		if !had || before.Manifest.Version.Stamp != winner.Manifest.Version.Stamp {
			changed = append(changed, slug)
		}
	}
	for slug := range previous.Games {
		if _, still := next.Games[slug]; !still {
			changed = append(changed, slug)
		}
	}
	sort.Strings(changed)

	// Everything opened by this scan or held by the last snapshot that did
	// not win a slug is retired together.
	serving := make(map[*Bundle]bool, len(next.Games))
	for _, winner := range next.Games {
		serving[winner] = true
	}
	var retired []*Bundle
	for _, candidate := range opened {
		if !serving[candidate] {
			retired = append(retired, candidate)
		}
	}
	for _, held := range previous.Games {
		if !serving[held] {
			retired = append(retired, held)
		}
	}
	if len(retired) > 0 {
		time.AfterFunc(closeGrace, func() {
			for _, old := range retired {
				old.Close()
			}
		})
	}
	if len(changed) > 0 && r.onChange != nil {
		r.onChange(changed)
	}
	return nil
}

// Watch rescans whenever the directory changes, until ctx ends. Events are
// debounced: a download that arrives as a burst of writes costs one rescan,
// and the rescan reads the directory whole, so no event is ever missed for
// having been coalesced.
func (r *Registry) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch %s: %w", r.dir, err)
	}
	if err := watcher.Add(r.dir); err != nil {
		watcher.Close()
		return fmt.Errorf("watch %s: %w", r.dir, err)
	}
	go func() {
		defer watcher.Close()
		debounce := time.NewTimer(0)
		if !debounce.Stop() {
			<-debounce.C
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Only finished bundles matter: the importer and the bundler
				// both assemble under other names and rename into place.
				if strings.HasSuffix(event.Name, ".atlas") {
					debounce.Reset(500 * time.Millisecond)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("atlas: watching bundles", "error", err)
			case <-debounce.C:
				if err := r.Rescan(); err != nil {
					slog.Warn("atlas: rescanning bundles", "error", err)
				}
			}
		}
	}()
	return nil
}

// Release closes the open archive behind one game so its file can be renamed
// over on platforms that lock open files. The snapshot still lists the game
// until the next rescan; a request in that window is answered with a 404,
// which the frontend already treats as its cue to refetch the catalog.
func (r *Registry) Release(slug string) {
	r.rescan.Lock()
	defer r.rescan.Unlock()
	if held := r.snap.Load().Games[slug]; held != nil {
		held.Close()
	}
}

// Install copies bundle files into the directory under their versioned
// names -- <game>-<capture-day>-<stamp>.atlas -- validating each before it
// is let in, and rescans once at the end. Versions sit side by side and the
// newest-wins fold serves the right one, so importing an old file can never
// overwrite a newer build; it just arrives already shadowed. It reports the
// games installed and, separately, what was wrong with anything refused:
// one bad file does not turn the rest of a multi-selection away.
func (r *Registry) Install(paths []string) (installed []string, refused []string) {
	for _, path := range paths {
		slug, err := r.installOne(path)
		if err != nil {
			refused = append(refused, err.Error())
			continue
		}
		installed = append(installed, slug)
	}
	if len(installed) > 0 {
		if err := r.Rescan(); err != nil {
			refused = append(refused, err.Error())
		}
	}
	return installed, refused
}

func (r *Registry) installOne(path string) (string, error) {
	opened, err := Open(path)
	if err != nil {
		return "", err
	}
	defer opened.Close()
	if err := opened.Validate(); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	slug := opened.Manifest.Game.Slug

	target := filepath.Join(r.dir, VersionedFileName(opened.Manifest))
	if same, err := filepath.Abs(path); err == nil {
		if resolved, err := filepath.Abs(target); err == nil && same == resolved {
			return slug, nil
		}
	}
	// This exact build already installed under its own name: nothing to copy.
	if _, err := os.Stat(target); err == nil {
		return slug, nil
	}
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	staged, err := os.CreateTemp(r.dir, ".importing-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(staged.Name())
	if _, err := io.Copy(staged, source); err != nil {
		staged.Close()
		return "", fmt.Errorf("copy %s: %w", filepath.Base(path), err)
	}
	if err := staged.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(staged.Name(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		// Windows refuses to rename over a file that is open. Closing the
		// game's archive first is what makes replacing an installed game
		// possible there; the moment of unreadability ends at the rescan
		// Install finishes with.
		r.Release(slug)
		if err := os.Rename(staged.Name(), target); err != nil {
			return "", fmt.Errorf("install %s: %w", filepath.Base(path), err)
		}
	}
	return slug, nil
}

// openOrCarry reuses the already-open bundle for a path whose size and
// modification time have not moved, and opens the file fresh otherwise.
func openOrCarry(previous *Snapshot, path string) (*Bundle, error) {
	for _, held := range previous.Games {
		if held.Path != path {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Size() == held.size && info.ModTime().Equal(held.modTime) {
			return held, nil
		}
		break
	}
	return Open(path)
}

func composeCatalog(dir string, games map[string]*Bundle) []byte {
	listed := make([]catalogGame, 0, len(games))
	for slug, held := range games {
		manifest := held.Manifest
		listed = append(listed, catalogGame{
			Slug:     slug,
			Title:    manifest.Game.Title,
			Stamp:    manifest.Version.Stamp,
			Base:     strings.Join([]string{BasePath, slug, ShortStamp(manifest.Version.Stamp)}, "/"),
			TileGrid: manifest.TileGrid,
			Maps:     manifest.Maps,
		})
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Title < listed[j].Title })
	// The bundles directory rides along so an empty library can tell the
	// reader where a bundle goes, in the words of their own machine.
	composed, err := json.Marshal(struct {
		Games      []catalogGame `json:"games"`
		BundlesDir string        `json:"bundlesDir,omitempty"`
	}{Games: listed, BundlesDir: dir})
	if err != nil {
		// Nothing in a validated manifest can fail to marshal; if something
		// does, an empty catalog is a saner face than a panic.
		slog.Error("atlas: compose catalog", "error", err)
		return []byte(`{"games":[]}`)
	}
	return composed
}
