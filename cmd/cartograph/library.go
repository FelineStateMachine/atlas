package main

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// library reads the bundles directory whole on every ask and answers with
// games grouped from what it found. Measuring a bundle costs a pass over its
// table of contents and payload JSON, so measurements are cached by the
// file's size and modification time -- the same test the reader's registry
// uses to tell an untouched file from one rewritten under its name -- and a
// page load after the first is a stat per file.
type library struct {
	dir string

	mu    sync.Mutex
	cache map[string]cachedBuild
}

type cachedBuild struct {
	size    int64
	modTime time.Time
	build   *build
}

// game is every build of one slug, newest first: the same order the reader's
// registry folds by, so the first build of every game is the one the reader
// serves.
type game struct {
	Slug   string
	Title  string
	Builds []*build
}

// Serving is the build the reader would pick.
func (g *game) Serving() *build { return g.Builds[0] }

// Build finds one build by file name, which is how diff URLs name them.
func (g *game) Build(file string) *build {
	for _, b := range g.Builds {
		if b.File == file {
			return b
		}
	}
	return nil
}

// games measures every bundle in the directory and groups the builds by
// game. A bundle that fails to measure is reported in skipped rather than
// failing the scan: one bad download should not take the dashboard down.
func (l *library) games() (games []*game, skipped []string, err error) {
	paths, err := filepath.Glob(filepath.Join(l.dir, "*.atlas"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)

	byGame := make(map[string]*game)
	for _, path := range paths {
		measured, err := l.measure(path)
		if err != nil {
			skipped = append(skipped, filepath.Base(path)+": "+err.Error())
			continue
		}
		held, ok := byGame[measured.GameSlug]
		if !ok {
			held = &game{Slug: measured.GameSlug}
			byGame[measured.GameSlug] = held
			games = append(games, held)
		}
		held.Builds = append(held.Builds, measured)
	}

	for _, held := range byGame {
		// The registry's own order: newest capture first, then the newer
		// policy revision, then stamp, then path -- total, so two scans of
		// one directory always agree.
		sort.Slice(held.Builds, func(a, b int) bool {
			left, right := held.Builds[a], held.Builds[b]
			if left.CreatedAt != right.CreatedAt {
				return left.CreatedAt > right.CreatedAt
			}
			if left.Revision != right.Revision {
				return left.Revision > right.Revision
			}
			if left.Stamp != right.Stamp {
				return left.Stamp > right.Stamp
			}
			return left.Path > right.Path
		})
		held.Title = held.Serving().GameTitle
	}
	sort.Slice(games, func(a, b int) bool { return games[a].Title < games[b].Title })
	return games, skipped, nil
}

// gameBySlug scans and returns one game, or nil if the slug has no builds.
func (l *library) gameBySlug(slug string) (*game, error) {
	games, _, err := l.games()
	if err != nil {
		return nil, err
	}
	for _, held := range games {
		if held.Slug == slug {
			return held, nil
		}
	}
	return nil, nil
}

func (l *library) measure(path string) (*build, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	held, ok := l.cache[path]
	l.mu.Unlock()
	if ok && held.size == info.Size() && held.modTime.Equal(info.ModTime()) {
		return held.build, nil
	}
	measured, err := measureBundle(path)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.cache == nil {
		l.cache = make(map[string]cachedBuild)
	}
	l.cache[path] = cachedBuild{size: info.Size(), modTime: info.ModTime(), build: measured}
	l.mu.Unlock()
	return measured, nil
}
