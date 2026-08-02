package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// maxManifestSize bounds what Open will read as a manifest. A manifest lists
// a volume's worlds and nothing per pin, so a megabyte is already generous; a
// larger one is some other file wearing the name.
const maxManifestSize = 1 << 20

// Bundle is an opened .atlas file. The archive stays open for the bundle's
// lifetime, so serving an entry is a seek and a read against the one held
// descriptor rather than a fresh open of a multi-hundred-megabyte file.
type Bundle struct {
	Path     string
	Manifest Manifest

	archive *zip.ReadCloser
	entries map[string]*zip.File

	// size and modTime are the file as it was opened, letting a rescan tell
	// an untouched file from one rewritten in place under the same name.
	size    int64
	modTime time.Time
}

// Open reads the manifest of the bundle at path and refuses anything that is
// not a bundle this reader understands. It does not check the manifest's
// promises against the archive's contents; that is Validate, which costs a
// pass over every payload and is for producers and importers, not for every
// launch.
func Open(path string) (*Bundle, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	bundle := &Bundle{
		Path:    path,
		archive: archive,
		entries: make(map[string]*zip.File, len(archive.File)),
	}
	if info, err := os.Stat(path); err == nil {
		bundle.size, bundle.modTime = info.Size(), info.ModTime()
	}
	for _, file := range archive.File {
		bundle.entries[file.Name] = file
	}

	manifest, ok := bundle.entries[ManifestName]
	if !ok {
		archive.Close()
		return nil, fmt.Errorf("%s carries no %s, so it is not a bundle", path, ManifestName)
	}
	if manifest.UncompressedSize64 > maxManifestSize {
		archive.Close()
		return nil, fmt.Errorf("%s: manifest is implausibly large", path)
	}
	reader, err := manifest.Open()
	if err != nil {
		archive.Close()
		return nil, fmt.Errorf("%s: open manifest: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxManifestSize))
	reader.Close()
	if err != nil {
		archive.Close()
		return nil, fmt.Errorf("%s: read manifest: %w", path, err)
	}
	if err := json.Unmarshal(data, &bundle.Manifest); err != nil {
		archive.Close()
		return nil, fmt.Errorf("%s: decode manifest: %w", path, err)
	}
	if err := bundle.Manifest.Validate(); err != nil {
		archive.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return bundle, nil
}

// Has reports whether the bundle holds an entry by this name.
func (b *Bundle) Has(name string) bool {
	_, ok := b.entries[name]
	return ok
}

// OpenEntry opens one entry for reading and reports its uncompressed size, so
// a server can announce a length before streaming.
func (b *Bundle) OpenEntry(name string) (io.ReadCloser, int64, error) {
	entry, ok := b.entries[name]
	if !ok {
		return nil, 0, fmt.Errorf("%s holds no %s", b.Path, name)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, 0, err
	}
	return reader, int64(entry.UncompressedSize64), nil
}

// ReadEntry reads one entry whole.
func (b *Bundle) ReadEntry(name string) ([]byte, error) {
	reader, _, err := b.OpenEntry(name)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// Close releases the archive. Entries opened before must not be read after.
func (b *Bundle) Close() error {
	return b.archive.Close()
}

// payloadPeek is the sliver of a world payload that validation reads: which
// pyramids its layers draw from, which icons its categories name, and what
// the payload says of itself in the shared conventions.
type payloadPeek struct {
	Lenses []struct {
		Tiles   string   `json:"tiles"`
		MinZoom int      `json:"minZoom"`
		MaxZoom int      `json:"maxZoom"`
		Formats []string `json:"formats"`
	} `json:"lenses"`
	Groups []struct {
		Categories []struct {
			IconAsset   string            `json:"iconAsset"`
			DisplayType string            `json:"displayType"`
			Attrs       map[string]string `json:"attrs"`
		} `json:"categories"`
	} `json:"groups"`
	Zones []struct {
		Attrs map[string]string `json:"attrs"`
	} `json:"zones"`
	Attrs map[string]string `json:"attrs"`
}

// textPeek is the sliver of a pin's text entry that validation reads: its
// attributes, which are the only conventions that ride the text file.
type textPeek struct {
	Attrs map[string]string `json:"a"`
}

// Validate checks the manifest's promises against the archive: // world has its three payloads, the packed locations agree with the advertised
// pin count, every lens's tile levels hold tiles, every named icon exists,
// and nothing carries a live URL -- Atlas runs with no network, so a URL in a
// bundle is dead weight at best. Producers and importers run this; opening a
// bundle at launch does not.
func (b *Bundle) Validate() error {
	levels := make(map[string]int)
	for name := range b.entries {
		if directory := name[:strings.LastIndexByte(name, '/')+1]; directory != "" {
			levels[directory]++
		}
	}

	for _, entry := range b.Manifest.Worlds {
		detail, err := b.ReadEntry("worlds/" + entry.Slug + ".json")
		if err != nil {
			return fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		text, err := b.ReadEntry("worlds/" + entry.Slug + ".text")
		if err != nil {
			return fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		for _, payload := range [][]byte{detail, text} {
			for _, scheme := range []string{"http://", "https://"} {
				if at := strings.Index(string(payload), scheme); at >= 0 {
					return fmt.Errorf("world %s carries a runtime URL: %q",
						entry.Slug, payload[at:min(at+120, len(payload))])
				}
			}
		}

		packed, err := b.ReadEntry("worlds/" + entry.Slug + ".bin")
		if err != nil {
			return fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		locations, err := UnpackLocations(packed)
		if err != nil {
			return fmt.Errorf("world %s: %w", entry.Slug, err)
		}
		if len(locations) != entry.PinCount {
			return fmt.Errorf("world %s packs %d locations, and the manifest says %d",
				entry.Slug, len(locations), entry.PinCount)
		}

		var peek payloadPeek
		if err := json.Unmarshal(detail, &peek); err != nil {
			return fmt.Errorf("world %s: decode payload: %w", entry.Slug, err)
		}
		if len(peek.Lenses) == 0 {
			return fmt.Errorf("world %s has no lenses", entry.Slug)
		}
		for _, lens := range peek.Lenses {
			if len(lens.Formats) != lens.MaxZoom-lens.MinZoom+1 {
				return fmt.Errorf("world %s lens %s names %d tile formats for %d levels",
					entry.Slug, lens.Tiles, len(lens.Formats), lens.MaxZoom-lens.MinZoom+1)
			}
			for zoom := lens.MinZoom; zoom <= lens.MaxZoom; zoom++ {
				prefix := "tiles/" + lens.Tiles + "/" + strconv.Itoa(zoom) + "/"
				var held int
				for directory, count := range levels {
					if strings.HasPrefix(directory, prefix) {
						held += count
					}
				}
				if held == 0 {
					return fmt.Errorf("world %s lens %s has an empty tile level %d",
						entry.Slug, lens.Tiles, zoom)
				}
			}
		}
		for _, group := range peek.Groups {
			for _, category := range group.Categories {
				if category.IconAsset != "" && !b.Has("icons/"+category.IconAsset) {
					return fmt.Errorf("world %s names a missing icon %s", entry.Slug, category.IconAsset)
				}
			}
		}
		if b.Manifest.Conventions >= 1 {
			if err := validateConventions(entry.Slug, peek, text); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateConventions holds a declaring bundle to the vocabulary it claims:
// every attribute registered and well-formed, the render attribute agreeing
// with the legacy field it will one day retire, a declared standard icon
// actually resolved, and a declared sphere carrying a mapping that parses.
// A bundle that declares no conventions is not held to any -- the strictness
// belongs to the claim.
func validateConventions(slug string, peek payloadPeek, text []byte) error {
	if err := semconv.Validate(semconv.EntityWorld, peek.Attrs); err != nil {
		return fmt.Errorf("world %s: %w", slug, err)
	}
	if peek.Attrs[semconv.KeyGeometrySurface] == semconv.SurfaceSphere {
		if peek.Attrs[semconv.KeyGeometryProjection] == "" {
			return fmt.Errorf("world %s declares a sphere with no projection", slug)
		}
		if _, err := semconv.ParseEquirect(
			peek.Attrs[semconv.KeyGeometryEquirectPx],
			peek.Attrs[semconv.KeyGeometryEquirectDeg]); err != nil {
			return fmt.Errorf("world %s: %w", slug, err)
		}
	}
	for _, zone := range peek.Zones {
		// A zone's attributes attach to the feature it is -- except its
		// stroke width, which the v2 wire spells on each zone though the
		// registry attaches it to the collection. The zone answers for its
		// collection until the unified wire moves the write.
		attrs := zone.Attrs
		if width, carried := attrs[semconv.KeyStrokeWidthPx]; carried {
			if err := semconv.Validate(semconv.EntityCollection,
				map[string]string{semconv.KeyStrokeWidthPx: width}); err != nil {
				return fmt.Errorf("world %s: %w", slug, err)
			}
			rest := make(map[string]string, len(attrs)-1)
			for key, value := range attrs {
				if key != semconv.KeyStrokeWidthPx {
					rest[key] = value
				}
			}
			attrs = rest
		}
		if err := semconv.Validate(semconv.EntityFeature, attrs); err != nil {
			return fmt.Errorf("world %s: %w", slug, err)
		}
	}
	for _, group := range peek.Groups {
		for _, category := range group.Categories {
			if err := semconv.Validate(semconv.EntityCollection, category.Attrs); err != nil {
				return fmt.Errorf("world %s: %w", slug, err)
			}
			if declared, ok := category.Attrs[semconv.KeyRenderAs]; ok {
				if legacy := semconv.RenderAs(nil, category.DisplayType); declared != legacy {
					return fmt.Errorf("world %s renders a category as %s while its legacy field says %s",
						slug, declared, legacy)
				}
			}
			if category.Attrs[semconv.KeyIconStd] != "" && category.IconAsset == "" {
				return fmt.Errorf("world %s declares a standard icon that was never resolved", slug)
			}
		}
	}
	var entries map[string]textPeek
	if err := json.Unmarshal(text, &entries); err != nil {
		return fmt.Errorf("world %s: decode text: %w", slug, err)
	}
	for id, entry := range entries {
		if err := semconv.Validate(semconv.EntityFeature, entry.Attrs); err != nil {
			return fmt.Errorf("world %s pin %s: %w", slug, id, err)
		}
	}
	return nil
}
