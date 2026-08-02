package workbench

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
)

// Reading the registry.
//
// This is the one part of the workbench that touches a filesystem, and it is
// kept to one file for that reason: everything above it works on scores that
// are already in memory. The registry is read whole on every ask -- a build
// installed by an operation is visible on the next page load, with no watcher
// and no cache to invalidate by hand -- and scoring is cached by the file's
// size and modification time, the same test the format's own registry uses to
// tell an untouched file from one rewritten under its name. A page load after
// the first is a stat per file.

// library is a registry directory, scored.
type library struct {
	dir   string
	table maturity.Table

	mu    sync.Mutex
	cache map[string]scored
}

type scored struct {
	size    int64
	modTime time.Time
	score   *maturity.Score
}

// volume is every build of one slug, newest first: the registry's own fold, so
// the first build of every volume is the one a reader is served.
type volume struct {
	Slug   string
	Title  string
	Builds []*maturity.Score
}

// Serving is the build a reader would be given.
func (v *volume) Serving() *maturity.Score { return v.Builds[0] }

// Previous is the build before the serving one, or nil when there is only one.
func (v *volume) Previous() *maturity.Score {
	if len(v.Builds) < 2 {
		return nil
	}
	return v.Builds[1]
}

// Build finds one build by file name, which is how a diff URL names them.
func (v *volume) Build(file string) *maturity.Score {
	for _, build := range v.Builds {
		if build.File == file {
			return build
		}
	}
	return nil
}

// Movement is what the serving build did to the one before it: the score delta
// that headlines a measurement page. A volume with one build has none.
func (v *volume) Movement() *maturity.Comparison {
	previous := v.Previous()
	if previous == nil {
		return nil
	}
	moved := maturity.Compare(previous, v.Serving())
	return &moved
}

// Sources are the readings the serving build carries, in the order its ledger
// records them and without repetition: one badge per source, whatever number of
// worlds it contributed to.
func (v *volume) Sources() []string {
	var seen []string
	held := map[string]bool{}
	for _, line := range v.Serving().Ledger {
		name := line.Account.Source
		if name == "" || held[name] {
			continue
		}
		held[name] = true
		seen = append(seen, name)
	}
	return seen
}

// volumes scores every bundle in the directory and groups the builds by volume.
//
// A bundle that will not measure is reported in skipped rather than failing the
// scan: one half-written file should not take the workbench down.
func (l *library) volumes() (volumes []*volume, skipped []string, err error) {
	paths, err := filepath.Glob(filepath.Join(l.dir, "*"+bundle.Extension))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)

	bySlug := make(map[string]*volume)
	for _, path := range paths {
		score, err := l.score(path)
		if err != nil {
			skipped = append(skipped, filepath.Base(path)+": "+err.Error())
			continue
		}
		held, ok := bySlug[score.Volume]
		if !ok {
			held = &volume{Slug: score.Volume}
			bySlug[score.Volume] = held
			volumes = append(volumes, held)
		}
		held.Builds = append(held.Builds, score)
	}

	for _, held := range bySlug {
		sort.Slice(held.Builds, func(a, b int) bool {
			return bundle.Newer(descriptorOf(held.Builds[a]), descriptorOf(held.Builds[b]))
		})
		held.Title = held.Serving().Title
	}
	sort.Slice(volumes, func(a, b int) bool {
		if volumes[a].Title != volumes[b].Title {
			return volumes[a].Title < volumes[b].Title
		}
		return volumes[a].Slug < volumes[b].Slug
	})
	return volumes, skipped, nil
}

// volumeBySlug scans and returns one volume, or nil when the slug has no builds.
func (l *library) volumeBySlug(slug string) (*volume, []string, error) {
	volumes, skipped, err := l.volumes()
	if err != nil {
		return nil, nil, err
	}
	for _, held := range volumes {
		if held.Slug == slug {
			return held, skipped, nil
		}
	}
	return nil, skipped, nil
}

func (l *library) score(path string) (*maturity.Score, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	held, ok := l.cache[path]
	l.mu.Unlock()
	if ok && held.size == info.Size() && held.modTime.Equal(info.ModTime()) {
		return held.score, nil
	}
	measured, err := maturity.Measure(path, l.table)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.cache == nil {
		l.cache = make(map[string]scored)
	}
	l.cache[path] = scored{size: info.Size(), modTime: info.ModTime(), score: measured}
	l.mu.Unlock()
	return measured, nil
}

// descriptorOf is a scored build as the format's registry sees it, so newest
// first means here exactly what it means to a reader opening the library.
// Ordering has one definition and it lives in format/bundle.
func descriptorOf(s *maturity.Score) bundle.Descriptor {
	return bundle.Descriptor{
		Locator:   s.Path,
		Slug:      s.Volume,
		Title:     s.Title,
		Stamp:     s.Stamp,
		CreatedAt: s.CreatedAt,
		Revision:  s.Revision,
	}
}

// features reads one build's point features, by world, for a diff. Only a
// comparison ever needs them, so they are unpacked on demand rather than kept
// beside every score.
func features(score *maturity.Score) (map[string]map[int64]string, error) {
	reader, err := bundle.Open(score.Path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	byWorld := make(map[string]map[int64]string, len(score.Worlds))
	for _, world := range score.Worlds {
		packed, err := reader.Locations(world.Slug)
		if err != nil {
			return nil, err
		}
		held := make(map[int64]string, packed.Len())
		for at := 0; at < packed.Len(); at++ {
			held[packed.ID(at)] = packed.Title(at)
		}
		byWorld[world.Slug] = held
	}
	return byWorld, nil
}
