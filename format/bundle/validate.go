package bundle

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// runtimeSchemes are what the offline scan looks for. Atlas runs with no
// network, so a URL in a payload is dead weight at best and a silent
// dependency on somebody else's server at worst.
var runtimeSchemes = []string{"http://", "https://"}

// urlExcerpt bounds how much of an offending payload an error quotes: enough
// to find the line, not enough to fill a terminal.
const urlExcerpt = 120

// worldPeek is the sliver of a world payload validation reads: which pyramids
// its lenses draw from, which icons its collections name, what kind of
// features each collection declares and inlines, and what the payload says of
// itself in the shared conventions.
//
// It is deliberately partial. Validation holds the format's promises; it does
// not model the payload, which stays a producer's and a renderer's business.
type worldPeek struct {
	Lenses []struct {
		Tiles   string   `json:"tiles"`
		MinZoom int      `json:"minZoom"`
		MaxZoom int      `json:"maxZoom"`
		Formats []string `json:"formats"`
	} `json:"lenses"`
	Collections []struct {
		ID        int64             `json:"id"`
		Title     string            `json:"title"`
		Kind      string            `json:"kind"`
		IconAsset string            `json:"iconAsset"`
		Attrs     map[string]string `json:"attrs"`
		Features  []struct {
			Geometry []struct {
				Type string `json:"type"`
			} `json:"geometry"`
			Attrs map[string]string `json:"attrs"`
		} `json:"features"`
	} `json:"collections"`
	Attrs map[string]string `json:"attrs"`
}

// textPeek is the sliver of a feature's text entry validation reads: its
// attributes, which are the only conventions that ride the text file.
type textPeek struct {
	Attrs map[string]string `json:"a"`
}

// Validate checks the manifest's promises against the archive:
//
//   - every world has its three payload entries;
//   - no payload carries a runtime URL (the offline-purity invariant);
//   - the packed locations and inline shape features agree with the
//     advertised per-kind counts;
//   - every collection's features are the kind it declares, and every packed
//     location is owned by a point collection that exists;
//   - every lens names as many tile formats as it has levels, and every level
//     holds tiles;
//   - every named icon exists;
//   - and, for a bundle that declares conventions, every attribute is
//     registered, well-formed, and attached where the registry says it lives.
//
// Producers and importers run this. Opening a bundle does not: it costs a
// pass over every payload.
func (r *Reader) Validate() error {
	levels := r.entryCountsByDirectory()
	for _, entry := range r.Manifest.Worlds {
		if err := r.validateWorld(entry, levels); err != nil {
			return err
		}
	}
	return nil
}

// entryCountsByDirectory counts the archive's entries per immediate
// directory, so a lens can ask whether a level holds tiles without a scan per
// level.
func (r *Reader) entryCountsByDirectory() map[string]int {
	counts := make(map[string]int)
	for name := range r.entries {
		if directory := name[:strings.LastIndexByte(name, '/')+1]; directory != "" {
			counts[directory]++
		}
	}
	return counts
}

func (r *Reader) validateWorld(entry WorldEntry, levels map[string]int) error {
	detail, err := r.ReadEntry(WorldEntryName(entry.Slug, WorldSuffix))
	if err != nil {
		return fmt.Errorf("world %s: %w", entry.Slug, err)
	}
	text, err := r.ReadEntry(WorldEntryName(entry.Slug, TextSuffix))
	if err != nil {
		return fmt.Errorf("world %s: %w", entry.Slug, err)
	}
	for _, payload := range [][]byte{detail, text} {
		if err := scanOffline(entry.Slug, payload); err != nil {
			return err
		}
	}

	packed, err := r.ReadEntry(WorldEntryName(entry.Slug, PackedSuffix))
	if err != nil {
		return fmt.Errorf("world %s: %w", entry.Slug, err)
	}
	locations, err := UnpackLocations(packed)
	if err != nil {
		return fmt.Errorf("world %s: %w", entry.Slug, err)
	}
	if len(locations) != entry.Points {
		return fmt.Errorf("world %s packs %d locations, and the manifest says %d points",
			entry.Slug, len(locations), entry.Points)
	}

	var peek worldPeek
	if err := json.Unmarshal(detail, &peek); err != nil {
		return fmt.Errorf("world %s: decode payload: %w", entry.Slug, err)
	}
	if err := r.validateLenses(entry, peek, levels); err != nil {
		return err
	}
	if err := r.validateCollections(entry, peek, locations); err != nil {
		return err
	}
	// The strictness belongs to the claim: a bundle that declares no
	// conventions is held to none.
	if r.Manifest.Conventions >= 1 {
		if err := validateConventions(entry.Slug, peek, text); err != nil {
			return err
		}
	}
	return nil
}

// scanOffline is the offline-purity invariant, enforced: a payload may hold
// no URL a reader could be tempted to fetch.
func scanOffline(slug string, payload []byte) error {
	body := string(payload)
	for _, scheme := range runtimeSchemes {
		if at := strings.Index(body, scheme); at >= 0 {
			return fmt.Errorf("world %s carries a runtime URL: %q",
				slug, body[at:min(at+urlExcerpt, len(body))])
		}
	}
	return nil
}

// validateLenses holds a world's pictures to what they promise: a world has
// at least one lens, a lens names one tile format per level, and every level
// it claims holds tiles.
func (r *Reader) validateLenses(entry WorldEntry, peek worldPeek, levels map[string]int) error {
	if len(peek.Lenses) == 0 {
		return fmt.Errorf("world %s has no lenses", entry.Slug)
	}
	for _, lens := range peek.Lenses {
		if len(lens.Formats) != lens.MaxZoom-lens.MinZoom+1 {
			return fmt.Errorf("world %s lens %s names %d tile formats for %d levels",
				entry.Slug, lens.Tiles, len(lens.Formats), lens.MaxZoom-lens.MinZoom+1)
		}
		for zoom := lens.MinZoom; zoom <= lens.MaxZoom; zoom++ {
			prefix := TilesPrefix + lens.Tiles + "/" + strconv.Itoa(zoom) + "/"
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
	return nil
}

// validateCollections holds a world's collections array to the shape the
// format declares, whatever vocabulary the bundle speaks: every collection a
// known kind with a distinct id, point features packed rather than inlined,
// inline geometry agreeing with its collection's kind, every path collection
// carrying the stroke width a path is drawn at, label policy only where
// labels draw on ground, the per-kind counts matching the manifest, and every
// packed location owned by a point collection that exists.
func (r *Reader) validateCollections(entry WorldEntry, peek worldPeek, locations []Location) error {
	ids := make(map[int64]string, len(peek.Collections))
	var paths, areas int
	for _, collection := range peek.Collections {
		if holder, taken := ids[collection.ID]; taken {
			return fmt.Errorf("world %s collections %q and %q share id %d",
				entry.Slug, holder, collection.Title, collection.ID)
		}
		ids[collection.ID] = collection.Title
		if collection.IconAsset != "" && !r.Has(IconsPrefix+collection.IconAsset) {
			return fmt.Errorf("world %s names a missing icon %s", entry.Slug, collection.IconAsset)
		}
		switch collection.Kind {
		case semconv.GeometryPoint:
			if len(collection.Features) > 0 {
				return fmt.Errorf("world %s point collection %q carries %d inline features; points ride the packed payload",
					entry.Slug, collection.Title, len(collection.Features))
			}
		case semconv.GeometryPath, semconv.GeometryArea:
			for _, feature := range collection.Features {
				for _, geometry := range feature.Geometry {
					if !GeometryFitsKind(collection.Kind, geometry.Type) {
						return fmt.Errorf("world %s %s collection %q inlines a %s",
							entry.Slug, collection.Kind, collection.Title, geometry.Type)
					}
				}
			}
			if collection.Kind == semconv.GeometryPath {
				paths += len(collection.Features)
				if collection.Attrs[semconv.KeyStrokeWidthPx] == "" {
					return fmt.Errorf("world %s path collection %q declares no %s",
						entry.Slug, collection.Title, semconv.KeyStrokeWidthPx)
				}
			} else {
				areas += len(collection.Features)
			}
		default:
			return fmt.Errorf("world %s collection %q declares kind %q, which is none of %s, %s, %s",
				entry.Slug, collection.Title, collection.Kind,
				semconv.GeometryPoint, semconv.GeometryPath, semconv.GeometryArea)
		}
		if _, spoken := collection.Attrs[semconv.KeyLabelPolicy]; spoken && collection.Kind != semconv.GeometryArea {
			return fmt.Errorf("world %s %s collection %q declares a label policy; only areas curate their labels",
				entry.Slug, collection.Kind, collection.Title)
		}
	}
	if paths != entry.Paths || areas != entry.Areas {
		return fmt.Errorf("world %s inlines %d paths and %d areas, and the manifest says %d and %d",
			entry.Slug, paths, areas, entry.Paths, entry.Areas)
	}
	for _, location := range locations {
		if int(location.Owner) >= len(peek.Collections) ||
			peek.Collections[location.Owner].Kind != semconv.GeometryPoint {
			return fmt.Errorf("world %s location %d names collection index %d, which is no point collection",
				entry.Slug, location.ID, location.Owner)
		}
	}
	return nil
}

// GeometryFitsKind says which GeoJSON geometry types a collection kind may
// inline: ground for areas, lines for paths. Points inline nothing at all --
// they ride the packed payload -- so no geometry type fits them.
func GeometryFitsKind(kind, geometryType string) bool {
	switch kind {
	case semconv.GeometryArea:
		return geometryType == "MultiPolygon" || geometryType == "Polygon"
	case semconv.GeometryPath:
		return geometryType == "MultiLineString" || geometryType == "LineString"
	}
	return false
}

// validateConventions holds a declaring bundle to the vocabulary it claims:
// every attribute registered, well-formed, and attached where the registry
// says it lives -- collections speak for their kind, stroke, and labels;
// features speak only for themselves -- a declared standard icon actually
// resolved, a declared kind agreeing with the wire's own field, and a declared
// sphere carrying a mapping that parses.
func validateConventions(slug string, peek worldPeek, text []byte) error {
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
	for _, collection := range peek.Collections {
		if err := semconv.Validate(semconv.EntityCollection, collection.Attrs); err != nil {
			return fmt.Errorf("world %s collection %q: %w", slug, collection.Title, err)
		}
		if declared, spoken := collection.Attrs[semconv.KeyGeometryKind]; spoken && declared != collection.Kind {
			return fmt.Errorf("world %s collection %q says kind %s while its attributes say %s",
				slug, collection.Title, collection.Kind, declared)
		}
		if collection.Attrs[semconv.KeyIconStd] != "" && collection.IconAsset == "" {
			return fmt.Errorf("world %s declares a standard icon that was never resolved", slug)
		}
		for _, feature := range collection.Features {
			if err := semconv.Validate(semconv.EntityFeature, feature.Attrs); err != nil {
				return fmt.Errorf("world %s: %w", slug, err)
			}
		}
	}
	var entries map[string]textPeek
	if err := json.Unmarshal(text, &entries); err != nil {
		return fmt.Errorf("world %s: decode text: %w", slug, err)
	}
	// Sorted, so a payload with several faults always reports the same one.
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := semconv.Validate(semconv.EntityFeature, entries[id].Attrs); err != nil {
			return fmt.Errorf("world %s feature %s: %w", slug, id, err)
		}
	}
	return nil
}
