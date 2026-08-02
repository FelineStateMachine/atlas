package bundle

import (
	"encoding/json"
	"fmt"
	"sort"
)

// The registry model: a library of immutable .atlas files, scanned and
// folded. New builds land beside old ones and nothing ever mutates a
// published bundle, so the question a registry answers -- which build of each
// volume serves? -- is a fold over descriptors, not a state machine.
//
// The fold in this file is pure: it touches no filesystem, opens no file, and
// logs nothing. Walking a directory to produce descriptors lives in scan.go,
// so a host that keeps its library somewhere other than a directory can
// supply descriptors from wherever it likes and get the same answer.

// Descriptor is one candidate build as the fold sees it: identity, version,
// and a locator. It carries no open file, which is what lets the fold be a
// function.
//
// Locator is opaque to the fold except as the last tie-break. A filesystem
// registry puts a path there; another host may put a key, a URL, anything
// that orders stably.
type Descriptor struct {
	Locator   string
	Slug      string
	Title     string
	Stamp     string
	CreatedAt string
	Revision  int
	Size      int64
	Worlds    int
}

// DescriptorOf reads a descriptor off a manifest. Size is the bundle's size
// in bytes where the host knows it, and zero where it does not; it is
// reported by the index and never consulted by the fold.
func DescriptorOf(locator string, m Manifest, size int64) Descriptor {
	return Descriptor{
		Locator:   locator,
		Slug:      m.Volume.Slug,
		Title:     m.Volume.Title,
		Stamp:     m.Version.Stamp,
		CreatedAt: m.Version.CreatedAt,
		Revision:  m.Version.Revision,
		Size:      size,
		Worlds:    len(m.Worlds),
	}
}

// Newer reports whether a should shadow b when both claim the same volume.
//
// The build ordering, in full: creation time decides, because a newer capture
// is newer data; among builds of one capture the newer policy revision
// decides, because the data has not moved but what a producer makes of it
// has; the stamp and then the locator break the remaining ties, so the choice
// is total and two folds of the same library always agree.
//
// Creation time and stamp compare as strings. Both are written in forms where
// lexical order is the order that means something -- RFC 3339 for the time,
// hex for the stamp -- so no parsing is needed and a malformed value cannot
// make the ordering partial.
func Newer(a, b Descriptor) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	if a.Revision != b.Revision {
		return a.Revision > b.Revision
	}
	if a.Stamp != b.Stamp {
		return a.Stamp > b.Stamp
	}
	return a.Locator > b.Locator
}

// Fold picks the winning build of every volume: newest per slug under
// [Newer]. It is pure and order-independent -- the same descriptors in any
// arrangement give the same winners -- and it never mutates its input.
func Fold(candidates []Descriptor) map[string]Descriptor {
	winners := make(map[string]Descriptor, len(candidates))
	for _, candidate := range candidates {
		if standing, held := winners[candidate.Slug]; held && !Newer(candidate, standing) {
			continue
		}
		winners[candidate.Slug] = candidate
	}
	return winners
}

// Shadowed lists the builds a fold did not pick, in the fold's own order:
// slug, then newest first. It is what a registry reports when it wants to say
// what else is in the library.
func Shadowed(candidates []Descriptor) []Descriptor {
	winners := Fold(candidates)
	var out []Descriptor
	for _, candidate := range candidates {
		if winner, held := winners[candidate.Slug]; held && winner.Locator == candidate.Locator {
			continue
		}
		out = append(out, candidate)
	}
	sortBuilds(out)
	return out
}

// Changed names the volumes whose serving build moved between two folds:
// arrivals, departures, and winners whose stamp changed. The result is
// sorted, so a caller can compare two rescans without minding map order.
func Changed(before, after map[string]Descriptor) []string {
	var changed []string
	for slug, winner := range after {
		if standing, had := before[slug]; !had || standing.Stamp != winner.Stamp {
			changed = append(changed, slug)
		}
	}
	for slug := range before {
		if _, still := after[slug]; !still {
			changed = append(changed, slug)
		}
	}
	sort.Strings(changed)
	return changed
}

// Index is the registry listing: every volume, every build of it, newest
// first. It is always derived from the files it sits with, never the other
// way around, so a hand-copied or hand-deleted bundle is reflected by the next
// scan rather than contradicted by a stale listing.
//
// The JSON field names are the format's history showing through: the listing
// predates the volume/world vocabulary and its wire keys were never renamed,
// because nothing reads the index that would benefit and every tool that
// writes one would have to agree at once. See docs/format.md.
type Index struct {
	Volumes []IndexVolume `json:"games"`
}

// IndexVolume is one volume's whole build history as the index lists it.
type IndexVolume struct {
	Slug     string       `json:"slug"`
	Title    string       `json:"title"`
	Versions []IndexBuild `json:"versions"`
}

// IndexBuild is one build of one volume as the index lists it.
type IndexBuild struct {
	File      string `json:"file"`
	Stamp     string `json:"stamp"`
	CreatedAt string `json:"createdAt"`
	Revision  int    `json:"revision,omitempty"`
	Size      int64  `json:"size"`
	Worlds    int    `json:"maps"`
}

// BuildIndex folds descriptors into the derived listing. It is pure: the
// caller supplies the descriptors and decides where, if anywhere, the result
// is written.
//
// Volumes sort by title, builds within a volume by the build ordering, and
// the title shown for a volume is the newest build's -- a volume that was
// renamed is called what it is called now.
//
// File names each build by the base of its locator, which for a filesystem
// registry is the file name. A host whose locators are not paths should
// expect whatever its locators look like.
func BuildIndex(candidates []Descriptor) Index {
	bySlug := make(map[string][]Descriptor)
	for _, candidate := range candidates {
		bySlug[candidate.Slug] = append(bySlug[candidate.Slug], candidate)
	}

	listed := make([]IndexVolume, 0, len(bySlug))
	for slug, builds := range bySlug {
		sortBuilds(builds)
		volume := IndexVolume{Slug: slug, Title: builds[0].Title, Versions: make([]IndexBuild, 0, len(builds))}
		for _, build := range builds {
			volume.Versions = append(volume.Versions, IndexBuild{
				File:      baseName(build.Locator),
				Stamp:     build.Stamp,
				CreatedAt: build.CreatedAt,
				Revision:  build.Revision,
				Size:      build.Size,
				Worlds:    build.Worlds,
			})
		}
		listed = append(listed, volume)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Title < listed[j].Title })
	return Index{Volumes: listed}
}

// MarshalIndex encodes an index as the registry directory carries it: one
// line, newline-terminated.
func MarshalIndex(index Index) ([]byte, error) {
	data, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal index: %w", err)
	}
	return append(data, '\n'), nil
}

// sortBuilds puts the newest build first under the build ordering.
func sortBuilds(builds []Descriptor) {
	sort.Slice(builds, func(i, j int) bool { return Newer(builds[i], builds[j]) })
}

// baseName is the last slash-separated segment of a locator, which for a path
// is its file name. It is spelled here rather than taken from path/filepath so
// that a locator is treated as an opaque string on every platform.
func baseName(locator string) string {
	for at := len(locator) - 1; at >= 0; at-- {
		if locator[at] == '/' || locator[at] == '\\' {
			return locator[at+1:]
		}
	}
	return locator
}
