// Package archive reads the capture archive: content-addressed bytes a crawler
// wrote, arranged so a translator can find them again without a network.
//
// The archive is a source-neutral concept -- captures addressed by the hash of
// their bytes, grouped by volume and by world, each carrying the time it was
// first seen -- but the archive on disk is a specific, historical layout. Years
// of captured history are data, not code, so that layout is kept verbatim as
// input and read here, behind the vocabulary of this package. Nothing new
// inherits its naming: a caller asks for a volume's worlds and their captures,
// and never learns which directory answered.
//
// # The layout this package reads
//
//	<root>/archive.json                  the volume register
//	<root>/<volumeDir>/game.json         the world register, and where icons live
//	<root>/<volumeDir>/icons/<key>.svg   collection artwork, svg or png
//	<root>/<volumeDir>/<worldDir>/snapshots/index.json
//	<root>/<volumeDir>/<worldDir>/snapshots/map/<contentHash>.json
//	<root>/<volumeDir>/<worldDir>/tiles/index.json
//	<root>/<volumeDir>/<worldDir>/tiles/set-<id>/<z>/<x>/<y>.<ext>
//
// The register files spell their directories as slash-separated paths relative
// to the root, and the JSON keys are the ones the layout was written with. Two
// of its habits are load-bearing and are translated here rather than passed on:
// a volume whose register entry names no source is a MapGenie capture, because
// MapGenie predates the field; and a capture's identity lives in the path it
// sits at rather than in the record, so the volume and world a capture belongs
// to are recovered structurally.
//
// # What a reader may assume
//
// Captures are deduplicated by content hash alone: unchanged bytes record
// nothing, so a re-crawl that fetched the same thing leaves the index byte for
// byte as it was and a rebuild produces the same stamp. Only the newest capture
// of a world is ever read; older ones are history kept on disk. Times compare
// as strings, which is the order that means something for RFC 3339.
package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultSource is the source a volume register entry with no source names.
// The field was added after the first crawler, so its absence is not a gap.
const DefaultSource = "mapgenie"

// Archive is an opened capture archive.
type Archive struct {
	root    string
	volumes []VolumeRef
}

// VolumeRef is one volume as the archive registers it. It is a pointer into the
// layout, not a document: opening it is what reads anything.
type VolumeRef struct {
	// Source names the crawler that filled this directory, defaulted.
	Source string
	// ID is the volume's identity in that source's id space.
	ID int64
	// Title is the volume's name as the source published it.
	Title string
	// Locator is where the source published it. Provenance only.
	Locator string

	dir string
}

// WorldRef is one world of a volume as the archive registers it.
type WorldRef struct {
	ID    int64
	Slug  string
	Title string

	dir string
}

// CaptureRef is one archived capture: bytes, when they were first seen, and
// what the source called them.
type CaptureRef struct {
	Kind        string
	SourceID    int64
	SourceURL   string
	ContentHash string
	CapturedAt  string
}

// ErrNotReady marks an archive entry that is present but not yet worth reading:
// a world crawled halfway, a register naming a directory that holds no capture.
// A caller skips these rather than failing, because a partial crawl is a normal
// state of an archive that is filled by hand.
var ErrNotReady = errors.New("capture is not ready")

// Open reads an archive's volume register.
func Open(root string) (*Archive, error) {
	var register struct {
		Volumes []struct {
			Directory string `json:"directory"`
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			URL       string `json:"url"`
			Source    string `json:"source"`
		} `json:"games"`
	}
	if err := readJSON(filepath.Join(root, "archive.json"), &register); err != nil {
		return nil, err
	}
	out := &Archive{root: root}
	for _, entry := range register.Volumes {
		source := entry.Source
		if source == "" {
			source = DefaultSource
		}
		out.volumes = append(out.volumes, VolumeRef{
			Source:  source,
			ID:      entry.ID,
			Title:   entry.Title,
			Locator: entry.URL,
			dir:     filepath.Join(root, filepath.FromSlash(entry.Directory)),
		})
	}
	return out, nil
}

// Root is where the archive was opened.
func (a *Archive) Root() string { return a.root }

// Volumes lists every registered volume, in register order.
func (a *Archive) Volumes() []VolumeRef {
	out := make([]VolumeRef, len(a.volumes))
	copy(out, a.volumes)
	return out
}

// Dir is where a volume's captures sit. It is exported because a source that
// needs a file the archive has no vocabulary for -- a sprite sheet, a stylesheet
// -- reads it from here, and because a caller reporting a failure needs to name
// the directory that failed.
func (v VolumeRef) Dir() string { return v.dir }

// Dir is where a world's captures sit.
func (w WorldRef) Dir() string { return w.dir }

// Worlds lists a volume's worlds. The directory listing is the truth -- the
// world register is a convenience that a crawl interrupted mid-world may not
// have updated -- and the register supplies each world's title and slug where
// it knows them.
func (a *Archive) Worlds(v VolumeRef) ([]WorldRef, error) {
	var register struct {
		Worlds []struct {
			Directory string `json:"directory"`
			ID        int64  `json:"id"`
			Slug      string `json:"slug"`
			Title     string `json:"title"`
		} `json:"maps"`
	}
	if err := readJSON(filepath.Join(v.dir, "game.json"), &register); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The register names a directory nothing has filled yet. An archive
			// is crawled by hand, one volume at a time, so this is an ordinary
			// state and not a fault.
			return nil, fmt.Errorf("%w: %s holds no world register", ErrNotReady, v.dir)
		}
		return nil, err
	}
	known := make(map[string]int, len(register.Worlds))
	for index, entry := range register.Worlds {
		known[filepath.Base(filepath.FromSlash(entry.Directory))] = index
	}
	entries, err := os.ReadDir(filepath.Join(v.dir, "maps"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s holds no world", ErrNotReady, v.dir)
	}
	if err != nil {
		return nil, fmt.Errorf("list worlds of %s: %w", v.dir, err)
	}
	var out []WorldRef
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		world := WorldRef{dir: filepath.Join(v.dir, "maps", entry.Name())}
		if index, ok := known[entry.Name()]; ok {
			world.ID = register.Worlds[index].ID
			world.Slug = register.Worlds[index].Slug
			world.Title = register.Worlds[index].Title
		}
		out = append(out, world)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out, nil
}

// Captures lists a world's captures, oldest first.
func (a *Archive) Captures(w WorldRef) ([]CaptureRef, error) {
	var index []struct {
		CapturedAt  string `json:"capturedAt"`
		ContentHash string `json:"contentHash"`
		Kind        string `json:"kind"`
		SourceID    int64  `json:"sourceId"`
		SourceURL   string `json:"sourceUrl"`
	}
	path := filepath.Join(w.dir, "snapshots", "index.json")
	if err := readJSON(path, &index); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s has no capture index", ErrNotReady, w.dir)
		}
		return nil, err
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("%w: %s has an empty capture index", ErrNotReady, w.dir)
	}
	out := make([]CaptureRef, 0, len(index))
	for _, entry := range index {
		out = append(out, CaptureRef{
			Kind:        entry.Kind,
			SourceID:    entry.SourceID,
			SourceURL:   entry.SourceURL,
			ContentHash: entry.ContentHash,
			CapturedAt:  entry.CapturedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CapturedAt < out[j].CapturedAt })
	return out, nil
}

// Newest is a world's most recent capture.
func (a *Archive) Newest(w WorldRef) (CaptureRef, error) {
	captures, err := a.Captures(w)
	if err != nil {
		return CaptureRef{}, err
	}
	return captures[len(captures)-1], nil
}

// Body reads a capture's archived bytes. The bytes are what the source
// published, untouched: translation happens here, at read time, so an editorial
// change replays over the archive rather than needing a re-crawl.
func (a *Archive) Body(w WorldRef, c CaptureRef) ([]byte, error) {
	path := filepath.Join(w.dir, "snapshots", "map", c.ContentHash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capture %s: %w", c.ContentHash, err)
	}
	return data, nil
}

// Artwork reads one piece of a volume's collection artwork, trying each
// extension in turn and reporting the asset name of the one that answered. A
// key nothing answers for is not an error: a collection may name artwork the
// archive never captured, and it simply goes without.
func (a *Archive) Artwork(v VolumeRef, key string, extensions ...string) (name string, data []byte, err error) {
	if !ValidArtworkKey(key) {
		return "", nil, nil
	}
	for _, extension := range extensions {
		path := filepath.Join(v.dir, "icons", key+extension)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("read artwork %s: %w", path, err)
		}
		return key + extension, data, nil
	}
	return "", nil, nil
}

// ValidArtworkKey admits the keys that can name a file without escaping the
// artwork directory. A key that needs more than letters, digits, underscore and
// hyphen is not a name this archive holds.
func ValidArtworkKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// TrimRoot shortens a path for a log line, so an event names the archive
// entry rather than the reader's home directory.
func TrimRoot(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}
