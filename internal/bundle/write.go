package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Writer builds a bundle around a manifest it validates up front, so a
// producer cannot finish writing an archive the reader would refuse to open.
type Writer struct {
	zw    *zip.Writer
	names map[string]bool
}

// NewWriter starts a bundle on w. The manifest is written first, so a listing
// of the archive leads with what it is.
func NewWriter(w io.Writer, manifest Manifest) (*Writer, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	out := &Writer{zw: zip.NewWriter(w), names: map[string]bool{ManifestName: true}}
	entry, err := out.zw.CreateHeader(&zip.FileHeader{Name: ManifestName, Method: zip.Deflate})
	if err != nil {
		return nil, err
	}
	if _, err := entry.Write(data); err != nil {
		return nil, err
	}
	return out, nil
}

// AddStored copies r into the bundle uncompressed. Tiles and packed locations
// go in this way: they are already dense, and a stored entry is served as a
// straight byte range where a deflated one is inflated on every read.
func (w *Writer) AddStored(name string, r io.Reader) error {
	entry, err := w.create(name, zip.Store)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, r)
	return err
}

// AddDeflated writes data into the bundle compressed, which suits the JSON
// payloads: text shrinks severalfold and is read whole when a map opens.
func (w *Writer) AddDeflated(name string, data []byte) error {
	entry, err := w.create(name, zip.Deflate)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

// Close finishes the archive. The bundle is not readable until it returns.
func (w *Writer) Close() error {
	return w.zw.Close()
}

func (w *Writer) create(name string, method uint16) (io.Writer, error) {
	if err := validEntryName(name); err != nil {
		return nil, err
	}
	if w.names[name] {
		return nil, fmt.Errorf("entry %s is written twice", name)
	}
	w.names[name] = true
	return w.zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
}

// validEntryName admits the names the format uses -- slash-separated relative
// paths of plain segments -- and nothing a hostile or careless producer could
// use to climb out of the archive's own namespace.
func validEntryName(name string) error {
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
