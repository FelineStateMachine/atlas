package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Writer builds a bundle around a manifest it validates up front, so a
// producer cannot finish writing an archive a reader would refuse to open.
type Writer struct {
	zip   *zip.Writer
	names map[string]bool
}

// NewWriter starts a bundle on w. The manifest is written first, so a listing
// of the archive leads with what it is.
//
// The manifest must already carry its stamp and creation time: the name a
// bundle is written under is derived from them, and a producer that has not
// decided them has not finished composing.
func NewWriter(w io.Writer, manifest Manifest) (*Writer, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	data, err := MarshalManifest(manifest)
	if err != nil {
		return nil, err
	}
	out := &Writer{zip: zip.NewWriter(w), names: map[string]bool{ManifestName: true}}
	entry, err := out.zip.CreateHeader(&zip.FileHeader{Name: ManifestName, Method: zip.Deflate})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", ManifestName, err)
	}
	if _, err := entry.Write(data); err != nil {
		return nil, fmt.Errorf("write %s: %w", ManifestName, err)
	}
	return out, nil
}

// MarshalManifest encodes a manifest exactly as the container carries it.
// Producers stamp over these bytes, so this is the one encoding of a manifest
// that counts: encoding/json's own output, HTML escaping and all, with no
// indentation and no trailing newline.
func MarshalManifest(m Manifest) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return data, nil
}

// AddStored copies r into the bundle uncompressed. Tiles and packed locations
// go in this way: they are already dense, and a stored entry is served as a
// straight byte range where a deflated one is inflated on every read.
func (w *Writer) AddStored(name string, r io.Reader) error {
	entry, err := w.create(name, zip.Store)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, r); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// AddDeflated writes data into the bundle compressed, which suits the JSON
// payloads: text shrinks severalfold and is read whole when a world opens.
func (w *Writer) AddDeflated(name string, data []byte) error {
	entry, err := w.create(name, zip.Deflate)
	if err != nil {
		return err
	}
	if _, err := entry.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// Close finishes the archive. The bundle is not readable until it returns.
func (w *Writer) Close() error {
	if err := w.zip.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	return nil
}

func (w *Writer) create(name string, method uint16) (io.Writer, error) {
	if err := ValidEntryName(name); err != nil {
		return nil, err
	}
	if w.names[name] {
		return nil, fmt.Errorf("entry %s is written twice", name)
	}
	w.names[name] = true
	entry, err := w.zip.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", name, err)
	}
	return entry, nil
}

// ValidEntryName admits the names the format uses -- slash-separated relative
// paths of plain segments -- and nothing a hostile or careless producer could
// use to climb out of the archive's own namespace. A reader that unpacks a
// bundle to disk is entitled to assume this held.
func ValidEntryName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("entry name %q is not a relative file path", name)
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("entry name %q climbs or stutters", name)
		}
	}
	return nil
}
