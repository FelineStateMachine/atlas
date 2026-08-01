package bundle

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	registry.snap.Store(emptySnapshot())
	return registry
}

// Dir is the directory the registry scans.
func (r *Registry) Dir() string { return r.dir }

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
	next.Catalog = composeCatalog(next.Games)
	r.snap.Store(next)

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
	return nil
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

func emptySnapshot() *Snapshot {
	return &Snapshot{Games: map[string]*Bundle{}, Catalog: composeCatalog(nil)}
}

func composeCatalog(games map[string]*Bundle) []byte {
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
	composed, err := json.Marshal(struct {
		Games []catalogGame `json:"games"`
	}{Games: listed})
	if err != nil {
		// Nothing in a validated manifest can fail to marshal; if something
		// does, an empty catalog is a saner face than a panic.
		slog.Error("atlas: compose catalog", "error", err)
		return []byte(`{"games":[]}`)
	}
	return composed
}
