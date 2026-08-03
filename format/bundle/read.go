package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// MaxManifestSize bounds what a reader will read as a manifest. A manifest
// lists a volume's worlds and nothing per feature, so a megabyte is already
// generous; a larger one is some other file wearing the name.
const MaxManifestSize = 1 << 20

// Reader is an opened bundle. The archive stays open for the reader's
// lifetime, so serving an entry is a seek and a read against one held
// descriptor rather than a fresh open of a multi-hundred-megabyte file.
//
// A Reader is safe for concurrent use: entries are opened independently.
type Reader struct {
	// Manifest is what the bundle says about itself, validated at open.
	Manifest Manifest

	// Path is where the bundle was opened from, empty for a reader opened
	// over bytes. It is a label and a tie-breaker, never an identity.
	Path string

	zip     *zip.Reader
	closer  io.Closer
	entries map[string]*zip.File

	// size and modTime are the file as it was opened, letting a rescan tell an
	// untouched file from one rewritten in place under the same name. Both are
	// zero for a reader opened over bytes.
	size    int64
	modTime time.Time
}

// Open reads the manifest of the bundle at path and refuses anything that is
// not a bundle this package understands.
//
// It does not check the manifest's promises against the archive's contents;
// that is [Reader.Validate], which costs a pass over every payload and is for
// producers and importers, not for every launch.
func Open(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	reader, err := NewReader(file, info.Size())
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	reader.Path = path
	reader.closer = file
	reader.size = info.Size()
	reader.modTime = info.ModTime()
	return reader, nil
}

// NewReader opens a bundle over bytes already in hand: a mapped file, an
// in-memory buffer, an object store's ranged reader. It is the form that
// keeps this package free of any particular filesystem; [Open] is the
// convenience over it.
//
// The caller keeps ownership of ra: [Reader.Close] on a reader made this way
// releases nothing.
func NewReader(ra io.ReaderAt, size int64) (*Reader, error) {
	archive, err := zip.NewReader(ra, size)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	reader := &Reader{zip: archive, entries: make(map[string]*zip.File, len(archive.File))}
	for _, file := range archive.File {
		reader.entries[file.Name] = file
	}
	if err := reader.readManifest(); err != nil {
		return nil, err
	}
	return reader, nil
}

func (r *Reader) readManifest() error {
	entry, held := r.entries[ManifestName]
	if !held {
		return fmt.Errorf("archive carries no %s, so it is not a bundle", ManifestName)
	}
	if entry.UncompressedSize64 > MaxManifestSize {
		return fmt.Errorf("manifest is implausibly large")
	}
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(source, MaxManifestSize))
	source.Close()
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if err := json.Unmarshal(data, &r.Manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if err := r.Manifest.Validate(); err != nil {
		return err
	}
	return nil
}

// Has reports whether the bundle holds an entry by this name.
func (r *Reader) Has(name string) bool {
	_, held := r.entries[name]
	return held
}

// Names lists every entry in the archive's own order, which is the order a
// producer wrote them: the manifest first, then payloads, tiles, and icons.
func (r *Reader) Names() []string {
	out := make([]string, 0, len(r.zip.File))
	for _, file := range r.zip.File {
		out = append(out, file.Name)
	}
	return out
}

// OpenEntry opens one entry for reading and reports its uncompressed size, so
// a server can announce a length before streaming.
func (r *Reader) OpenEntry(name string) (io.ReadCloser, int64, error) {
	entry, held := r.entries[name]
	if !held {
		return nil, 0, fmt.Errorf("bundle holds no %s", name)
	}
	source, err := entry.Open()
	if err != nil {
		return nil, 0, fmt.Errorf("open %s: %w", name, err)
	}
	return source, int64(entry.UncompressedSize64), nil
}

// ReadEntry reads one entry whole.
func (r *Reader) ReadEntry(name string) ([]byte, error) {
	source, _, err := r.OpenEntry(name)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return data, nil
}

// Stored reports whether an entry is held uncompressed. Tiles are, which is
// what lets a server answer a byte range out of the archive without inflating
// anything.
func (r *Reader) Stored(name string) bool {
	entry, held := r.entries[name]
	return held && entry.Method == zip.Store
}

// Locations decodes one world's packed point features into a borrowing view.
// The view holds the payload bytes, so it outlives neither them nor a caller
// that discards them.
func (r *Reader) Locations(worldSlug string) (*Packed, error) {
	data, err := r.ReadEntry(WorldEntryName(worldSlug, PackedSuffix))
	if err != nil {
		return nil, fmt.Errorf("world %s: %w", worldSlug, err)
	}
	packed, err := OpenPacked(data)
	if err != nil {
		return nil, fmt.Errorf("world %s: %w", worldSlug, err)
	}
	return packed, nil
}

// Descriptor is the bundle as the registry fold sees it: identity and
// version, with no open file behind it.
func (r *Reader) Descriptor() Descriptor {
	return DescriptorOf(r.Path, r.Manifest, r.size)
}

// Unchanged reports whether the file this reader was opened from still has
// the size and modification time it had at open. A rescan uses it to carry an
// already-open bundle across rather than reopening a file that has not moved.
// A reader opened over bytes is never unchanged, because nothing here knows
// what those bytes were.
func (r *Reader) Unchanged(size int64, modTime time.Time) bool {
	return r.closer != nil && r.size == size && r.modTime.Equal(modTime)
}

// Close releases the archive. Entries opened before must not be read after.
// Closing a reader made by [NewReader] does nothing, since it owns nothing.
func (r *Reader) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
