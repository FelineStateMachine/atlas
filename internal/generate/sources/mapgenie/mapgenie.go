// Package mapgenie reads MapGenie's archived captures and translates them into
// the Atlas interchange document.
//
// MapGenie publishes community game maps as a slippy map: an image tiled like a
// world, positions encoded as latitude and longitude that are really pixels, and
// a legend of groups holding categories holding pins. A capture is one map's
// whole API response, archived verbatim; this package is the only thing in
// Atlas that knows what is inside it.
//
// Nothing of that shape leaves this directory. Groups become a heading string
// on a collection, categories become collections, pins and regions become
// features of one kind or another, and the deep links MapGenie writes into its
// own prose become resolved cross-references -- because Atlas serves offline and
// a live URL in a payload is a defect.
//
// # What is refused
//
// A world with no capture, or with no tile set, is not ready: it is skipped, not
// failed, because an archive is filled by hand and a half-crawled map is a
// normal state. A capture whose kind is not this source's is a failure: reading
// another source's bytes through this reader would produce a document that
// quietly lies about where it came from.
//
// # Determinism
//
// Every ordering here comes from the capture: groups in capture order,
// categories within a group in capture order, pins within a category in capture
// order, regions in capture order. Nothing is sorted, because sorting would
// throw away an editorial order the publisher chose, and nothing is a map, so
// two runs over the same bytes emit the same document.
package mapgenie

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// captureKind is what the archive calls a MapGenie map capture.
const captureKind = "map"

// regionsKey is the collection MapGenie's regions fold into. MapGenie has no
// concept of a collection of regions -- they stand apart from the legend
// entirely -- so one is declared for them here, and the interchange document
// carries an ordinary area collection like any other.
const regionsKey = "regions"

// artworkExtensions are the forms the archive holds a category's icon in, in
// the order they are tried. Most games publish an icon font that renders to
// SVG; some publish a marker strip that slices into PNG.
var artworkExtensions = []string{".svg", ".png"}

// Source reads MapGenie captures.
type Source struct{}

// New builds the source. It holds no state: a translation is a function of the
// bytes it is handed.
func New() Source { return Source{} }

// Describe is MapGenie's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  "mapgenie",
		Label: "MapGenie",
		Attribution: "Maps and pin data by MapGenie and its contributors, " +
			"captured from mapgenie.io.",
		// MapGenie captures carry their own numeric identities for maps,
		// categories, pins and regions, and those numbers are stable across
		// captures, so the document passes them through untouched.
		IDSpace: doc.IDSpaceNative,
	}
}

// Translate reads one archived volume.
func (s Source) Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error) {
	log = log.With(logging.Source("mapgenie"))
	worlds, err := a.Worlds(v)
	if err != nil {
		return doc.Document{}, err
	}
	out := doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Volume:  doc.Volume{Title: v.Title},
		Source:  s.Describe(),
	}
	for _, ref := range worlds {
		world, slug, err := s.translateWorld(a, ref, log)
		if err != nil {
			if errors.Is(err, archive.ErrNotReady) {
				log.Debug("world skipped", logging.Path(archive.TrimRoot(a.Root(), ref.Dir())),
					"reason", err.Error())
				continue
			}
			return doc.Document{}, err
		}
		if out.Volume.Slug == "" {
			out.Volume.Slug = slug
		}
		out.Worlds = append(out.Worlds, world)
	}
	if len(out.Worlds) == 0 {
		return doc.Document{}, fmt.Errorf("%w: volume %s has no readable world", archive.ErrNotReady, v.Title)
	}
	if err := s.attachArtwork(a, v, &out); err != nil {
		return doc.Document{}, err
	}
	log.Info("volume translated", logging.Volume(out.Volume.Slug),
		"worlds", len(out.Worlds), "icons", len(out.Icons))
	return out, nil
}

func (s Source) translateWorld(a *archive.Archive, ref archive.WorldRef, log *slog.Logger) (doc.World, string, error) {
	capture, err := a.Newest(ref)
	if err != nil {
		return doc.World{}, "", err
	}
	if capture.Kind != captureKind {
		return doc.World{}, "", fmt.Errorf(
			"capture %s is of kind %q; the MapGenie reader answers only for %q",
			capture.ContentHash, capture.Kind, captureKind)
	}
	body, err := a.Body(ref, capture)
	if err != nil {
		return doc.World{}, "", err
	}
	var raw rawMap
	if err := json.Unmarshal(body, &raw); err != nil {
		return doc.World{}, "", fmt.Errorf("decode capture %s: %w", capture.ContentHash, err)
	}
	if raw.Slug == "" {
		return doc.World{}, "", fmt.Errorf("capture %s names no map", capture.ContentHash)
	}
	if len(raw.Config.TileSets) == 0 {
		return doc.World{}, "", fmt.Errorf("%w: map %s has no tile set", archive.ErrNotReady, raw.Slug)
	}

	world := doc.World{
		ID:    raw.ID,
		Slug:  raw.Slug,
		Title: raw.Title,
		Center: doc.Position{
			Lat: raw.InitialLatitude,
			Lng: raw.InitialLongitude,
		},
		Capture: doc.Capture{
			Kind:        capture.Kind,
			ID:          capture.SourceID,
			Locator:     capture.SourceURL,
			ContentHash: capture.ContentHash,
			CapturedAt:  capture.CapturedAt,
		},
	}
	for _, set := range raw.Config.TileSets {
		world.Lenses = append(world.Lenses, doc.Lens{Name: set.Name, TileSet: set.Path})
	}
	collections, err := collectionsOf(raw)
	if err != nil {
		return doc.World{}, "", fmt.Errorf("map %s: %w", raw.Slug, err)
	}
	resolveLinks(collections)
	world.Collections = collections
	log.Debug("world translated", logging.World(raw.Slug),
		"collections", len(collections), "capture", capture.ContentHash)
	return world, raw.Game.Slug, nil
}

// collectionsOf folds a capture into the interchange document's one model.
//
// The order is the legend order and it is significant downstream: categories as
// the publisher grouped them, then the regions gathered under their own
// collection. A region whose geometry all came through empty is dropped, because
// a shape with nothing to draw is not a shape.
func collectionsOf(raw rawMap) ([]doc.Collection, error) {
	var out []doc.Collection
	for _, group := range raw.Groups {
		for _, category := range group.Categories {
			collection := doc.Collection{
				ID:        category.ID,
				Title:     category.Title,
				Group:     group.Title,
				Kind:      doc.KindPoint,
				Icon:      category.Icon,
				Color:     firstColor(category.Color, group.Color),
				IconColor: firstColor(category.IconColor, group.IconColor),
				Visible:   category.Visible,
				// MapGenie's display type is a legacy field of its own that no
				// payload carries; it is spoken here, once, in the registered
				// vocabulary, and never travels further.
				Attrs: map[string]string{
					semconv.KeyRenderAs: semconv.RenderAs(nil, category.DisplayType),
				},
			}
			for _, location := range category.Locations {
				lat, err := number(location.Latitude)
				if err != nil {
					return nil, fmt.Errorf("location %d latitude: %w", location.ID, err)
				}
				lng, err := number(location.Longitude)
				if err != nil {
					return nil, fmt.Errorf("location %d longitude: %w", location.ID, err)
				}
				feature := doc.Feature{
					ID:          location.ID,
					Title:       location.Title,
					Description: location.Description,
					At:          &doc.Position{Lat: lat, Lng: lng},
				}
				if location.RegionID != nil {
					feature.Member = *location.RegionID
				}
				collection.Features = append(collection.Features, feature)
			}
			out = append(out, collection)
		}
	}

	regions := doc.Collection{
		Key:     regionsKey,
		Title:   "Regions",
		Kind:    doc.KindArea,
		Visible: true,
	}
	for _, region := range raw.Regions {
		shape := doc.Feature{
			ID:          region.ID,
			Title:       region.Title,
			Subtitle:    region.Subtitle,
			Description: region.Description,
		}
		if region.ParentRegionID != nil {
			shape.Parent = *region.ParentRegionID
		}
		centerX, hasX, err := optionalNumber(region.CenterX)
		if err != nil {
			return nil, fmt.Errorf("region %d center_x: %w", region.ID, err)
		}
		centerY, hasY, err := optionalNumber(region.CenterY)
		if err != nil {
			return nil, fmt.Errorf("region %d center_y: %w", region.ID, err)
		}
		if hasX && hasY {
			shape.Center = &doc.Position{Lat: centerY, Lng: centerX}
		}
		for _, part := range region.Features {
			if part.Geometry.Type == "" || len(part.Geometry.Coordinates) == 0 {
				continue
			}
			// MapGenie's regions are ground. A line drawn among them would be
			// a path collection somebody meant to declare, and this source has
			// no way to be told about one, so it is refused rather than drawn
			// as an area with no interior.
			if part.Geometry.Type == "MultiLineString" {
				return nil, fmt.Errorf("region %q draws lines, which are not ground", region.Title)
			}
			shape.Geometry = append(shape.Geometry, doc.Geometry{
				Type:        part.Geometry.Type,
				Coordinates: part.Geometry.Coordinates,
			})
		}
		if len(shape.Geometry) > 0 {
			regions.Features = append(regions.Features, shape)
		}
	}
	if len(regions.Features) > 0 {
		out = append(out, regions)
	}
	return out, nil
}

// attachArtwork reads the archived icon for every category that names one, once
// per key, in the order the collections name them.
func (s Source) attachArtwork(a *archive.Archive, v archive.VolumeRef, out *doc.Document) error {
	seen := make(map[string]bool)
	for _, world := range out.Worlds {
		for _, collection := range world.Collections {
			if collection.Kind != doc.KindPoint || collection.Icon == "" || seen[collection.Icon] {
				continue
			}
			seen[collection.Icon] = true
			file, data, err := a.Artwork(v, collection.Icon, artworkExtensions...)
			if err != nil {
				return err
			}
			if file == "" {
				continue
			}
			out.Icons = append(out.Icons, doc.Icon{Key: collection.Icon, File: file, Data: data})
		}
	}
	return nil
}

// Labels may themselves contain a bracketed aside, as in
// "[Oh Baby! [Super Sledge]](url)", so one level of nesting is allowed.
var markdownLink = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\[\]]*\])*)\]\(([^)\s]+)[^)]*\)`)

// mapgenieLocation is how MapGenie deep-links a pin of the same map.
var mapgenieLocation = regexp.MustCompile(`locationIds=(\d+)`)

// Some descriptions carry malformed markdown whose URL never sat inside a link,
// as in "[Boss] (Lv. 55) (https://...)". Nothing may ship a live URL, so any
// that survive link rewriting are removed outright.
var bareURL = regexp.MustCompile(`\s*\(?\s*https?://[^\s)]+\)?`)

// resolveLinks strips every external URL out of a world's prose. Atlas ships
// with no network, so a MapGenie or YouTube link is dead weight at best. Where
// the link pointed at another pin of this same world, the target survives as a
// cross-reference a reader can follow.
func resolveLinks(collections []doc.Collection) {
	known := make(map[int64]bool)
	for _, collection := range collections {
		if collection.Kind != doc.KindPoint {
			continue
		}
		for _, feature := range collection.Features {
			known[feature.ID] = true
		}
	}
	for collectionIndex := range collections {
		collection := &collections[collectionIndex]
		if collection.Kind != doc.KindPoint {
			continue
		}
		for featureIndex := range collection.Features {
			feature := &collection.Features[featureIndex]
			if !strings.Contains(feature.Description, "http") {
				continue
			}
			feature.Description = markdownLink.ReplaceAllStringFunc(
				feature.Description,
				func(match string) string {
					parts := markdownLink.FindStringSubmatch(match)
					label, target := parts[1], parts[2]
					id := mapgenieLocation.FindStringSubmatch(target)
					if id != nil && !strings.HasPrefix(label, "!") {
						if value, err := strconv.ParseInt(id[1], 10, 64); err == nil &&
							known[value] && value != feature.ID {
							feature.Links = append(feature.Links, doc.Link{Title: label, Feature: value})
						}
					}
					return label
				},
			)
			feature.Description = strings.TrimSpace(bareURL.ReplaceAllString(feature.Description, ""))
		}
	}
}

// firstColor resolves a category's colour over its group's, normalized. A
// capture spells colours with and without the hash and in either case, and the
// document publishes one spelling so nothing downstream has to know that.
func firstColor(category, group string) string {
	if color := normalizeHexColor(category); color != "" {
		return color
	}
	return normalizeHexColor(group)
}

func normalizeHexColor(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	switch len(value) {
	case 3, 4, 6, 8:
	default:
		return ""
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return ""
	}
	return "#" + strings.ToUpper(value)
}

// number reads a coordinate. MapGenie spells them as JSON numbers on some maps
// and as quoted strings on others, and both mean the same place.
func number(raw json.RawMessage) (float64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		value = text
	}
	return strconv.ParseFloat(value, 64)
}

func optionalNumber(raw json.RawMessage) (float64, bool, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false, nil
	}
	n, err := number(raw)
	return n, err == nil, err
}
