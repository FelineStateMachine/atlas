// Package piggyback reads a captured Piggyback map and translates it into the
// Atlas interchange document.
//
// Piggyback is the official guide house, and its maps carry what a community
// wikimap does not: prose. Pins arrive with names and descriptions, and both
// survive into a volume.
//
// # The coordinate design, and the gate on it
//
// Piggyback draws its world in a game's own coordinates on a Leaflet
// CRS.Simple map: a linear transformation squeezes them onto the unit tile at
// zoom zero. The capture records that transformation, and a pin passes through
// it and then through the shared inverse Mercator to land on the pixel Piggyback
// draws it at.
//
// That transformation is the one thing here that cannot be checked from the
// capture alone. It is read off the page's own scripts, it decides where every
// pin in the volume stands, and a wrong one puts a whole map's contents
// somewhere plausible and wrong -- which is exactly the failure a merge would
// then try to fit an affine to. So this reader refuses a transformation nobody
// has verified: verifiedTransforms is the list of games whose numbers have been
// checked against the published map, and a capture whose numbers are not in it
// fails the build rather than quietly misplacing a city.
//
// # What else is refused
//
// A capture of another source's kind; a capture naming no map; a capture whose
// crawler observed no tile level, since Piggyback publishes no bounds and the
// survey is the only account of the pyramid there is; a pin of a type no
// category declares, because the alternative is a pin silently dropped.
//
// # Determinism
//
// Categories and their types sort by declared position and then by key, pins by
// id, levels by zoom. The crawler does it before archiving and this reader does
// it again, because an archive holds captures older than the habit.
package piggyback

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// captureKind is what the archive calls a Piggyback capture, and source is what
// a well-formed capture says it is.
const (
	captureKind = "piggyback-map"
	source      = "piggyback"
)

// verifiedTransforms is the gate. A game appears here once its transformation
// has been checked against the published map -- a handful of known landmarks
// picked off both and compared -- and a capture carrying different numbers is
// refused rather than translated into a volume whose every pin is subtly
// somewhere else.
//
// Cyberpunk's pair maps the game's ±8192-unit world onto the 256-unit tile at
// zoom zero: 0.015625 is 4/256, and the offsets put the origin at the middle.
var verifiedTransforms = map[string]Transform{
	"cyberpunk-2077": {A: 0.015625, B: 128, C: -0.015625, D: 128},
}

// districtTypes are Piggyback's region name markers. They arrive filed under the
// reader-state "favorites" category, which is nothing to build a legend from, so
// they become floating text labels -- which is what they are on Piggyback's own
// map.
var districtTypes = map[string]bool{
	"province":   true,
	"sub-region": true,
}

// artworkExtensions are the forms the archive holds a type's icon in.
var artworkExtensions = []string{".svg", ".png"}

// Source reads Piggyback captures.
type Source struct{}

// New builds the source. It holds no state.
func New() Source { return Source{} }

// Describe is the source's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  "piggyback",
		Label: "Piggyback",
		License: "All rights reserved. Piggyback's maps are the cartography of a " +
			"published strategy guide; a volume carrying them is for personal use.",
		Attribution: "Map imagery and annotations by Piggyback Interactive, " +
			"captured from maps.piggyback.com.",
		// Piggyback numbers pins, categories and types with opaque string ids,
		// none of which a bundle's wire can carry, so every identity is minted
		// from a stable name.
		IDSpace: doc.IDSpaceDerived,
	}
}

// Translate reads one archived volume.
func (s Source) Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error) {
	log = log.With(logging.Source(source))
	worlds, err := a.Worlds(v)
	if err != nil {
		return doc.Document{}, err
	}
	out := doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Source:  s.Describe(),
	}
	for _, ref := range worlds {
		world, volume, err := s.translateWorld(a, ref, log)
		if err != nil {
			if errors.Is(err, archive.ErrNotReady) {
				log.Debug("world skipped", logging.Path(archive.TrimRoot(a.Root(), ref.Dir())),
					"reason", err.Error())
				continue
			}
			return doc.Document{}, err
		}
		if out.Volume.Slug == "" {
			// The plain slug, deliberately shared with every other source's
			// capture of the same game: their volumes answer for one library
			// entry, and the newest capture is the one a reader sees.
			out.Volume = volume
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

func (s Source) translateWorld(a *archive.Archive, ref archive.WorldRef, log *slog.Logger) (doc.World, doc.Volume, error) {
	archived, err := a.Newest(ref)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	if archived.Kind != captureKind {
		return doc.World{}, doc.Volume{}, fmt.Errorf(
			"capture %s is of kind %q; the Piggyback reader answers only for %q",
			archived.ContentHash, archived.Kind, captureKind)
	}
	body, err := a.Body(ref, archived)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	var raw capture
	if err := json.Unmarshal(body, &raw); err != nil {
		return doc.World{}, doc.Volume{}, fmt.Errorf("decode capture %s: %w", archived.ContentHash, err)
	}
	if err := check(&raw); err != nil {
		return doc.World{}, doc.Volume{}, fmt.Errorf("capture %s: %w", archived.ContentHash, err)
	}
	raw.normalize()

	ids := doc.NewIDSpace()
	scope := raw.GameSlug + "/" + raw.MapSlug
	worldID, err := ids.Claim("pb:map:" + scope)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	tileSet, err := TileSetPath(raw.Map.TileServer)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	collections, err := collectionsOf(&raw, ids, scope)
	if err != nil {
		return doc.World{}, doc.Volume{}, fmt.Errorf("map %s: %w", scope, err)
	}

	centerX, centerY := deepestWindowCenter(&raw)
	world := doc.World{
		ID:     worldID,
		Slug:   raw.MapSlug,
		Title:  named(raw.MapTitle, raw.MapSlug),
		Center: doc.SyntheticPosition(centerX, centerY),
		Capture: doc.Capture{
			Kind:        archived.Kind,
			ID:          archived.SourceID,
			Locator:     archived.SourceURL,
			ContentHash: archived.ContentHash,
			CapturedAt:  archived.CapturedAt,
		},
		Lenses:      []doc.Lens{{Name: "Default", TileSet: tileSet}},
		Collections: collections,
	}
	log.Debug("world translated", logging.World(raw.MapSlug),
		"collections", len(collections), "capture", archived.ContentHash)
	return world, doc.Volume{
		Slug:  raw.GameSlug,
		Title: named(raw.GameTitle, raw.GameSlug),
	}, nil
}

// check states what a capture has to be before anything is read out of it, and
// carries the verified-transform gate.
func check(raw *capture) error {
	if raw.Source != source {
		return fmt.Errorf("capture says its source is %q, not %q", raw.Source, source)
	}
	if raw.GameSlug == "" || raw.MapSlug == "" {
		return fmt.Errorf("capture names no map")
	}
	if len(raw.Levels) == 0 {
		return fmt.Errorf("capture observed no tile level, so nothing knows where the pyramid is drawn")
	}
	verified, known := verifiedTransforms[raw.GameSlug]
	if !known {
		return fmt.Errorf(
			"no transformation has been verified for %s; a Piggyback map is placed by numbers read "+
				"off its own page, and an unchecked pair would misplace every pin in the volume",
			raw.GameSlug)
	}
	if raw.Map.Transform != verified {
		return fmt.Errorf(
			"%s carries transformation %+v, which is not the verified %+v",
			raw.GameSlug, raw.Map.Transform, verified)
	}
	return nil
}

// collectionsOf turns Piggyback's category tree into the legend order a document
// carries: a declared category becomes a heading, its types become collections
// under it, and a type no pin uses is left out. District name pins -- filed under
// the reader-state favorites category -- gather instead under their own heading
// as floating text labels. Anything else a pin references without a declaration
// is an error: better a loud build than a pin silently dropped.
func collectionsOf(raw *capture, ids *doc.IDSpace, scope string) ([]doc.Collection, error) {
	categoryLabels := index(raw.Labels.Categories)
	typeLabels := index(raw.Labels.Types)

	byType := make(map[string][]pin)
	for _, p := range raw.Pins {
		byType[p.TypeKey] = append(byType[p.TypeKey], p)
	}

	declared := make(map[string]bool)
	var out []doc.Collection
	for _, group := range raw.Categories {
		if group.Key == "favorites" {
			// A reader's own favourites, not the map's data. Its stowaway
			// district pins are picked up below.
			continue
		}
		var built []doc.Collection
		for _, t := range group.Types {
			declared[t.Key] = true
			collection, ok, err := collectionOf(raw, ids, scope, group.Key, t.Key,
				spelled(typeLabels, t.Key), semconv.RenderAsPin, byType)
			if err != nil {
				return nil, err
			}
			if ok {
				built = append(built, collection)
			}
		}
		if len(built) == 0 {
			continue
		}
		heading := spelled(categoryLabels, group.Key)
		for _, collection := range built {
			collection.Group = heading
			out = append(out, collection)
		}
	}

	// District names, and a loud failure for anything else undeclared.
	var districts []doc.Collection
	for _, key := range sortedKeys(byType) {
		if declared[key] {
			continue
		}
		if !districtTypes[key] {
			return nil, fmt.Errorf("pins are of type %q, which no category declares", key)
		}
		collection, ok, err := collectionOf(raw, ids, scope, "districts", key,
			spelled(typeLabels, key), semconv.RenderAsText, byType)
		if err != nil {
			return nil, err
		}
		if ok {
			districts = append(districts, collection)
		}
	}
	for _, collection := range districts {
		collection.Group = "Districts"
		out = append(out, collection)
	}
	return out, nil
}

func collectionOf(
	raw *capture,
	ids *doc.IDSpace,
	scope, categoryKey, typeKey, typeLabel, renderAs string,
	byType map[string][]pin,
) (doc.Collection, bool, error) {
	pins := byType[typeKey]
	if len(pins) == 0 {
		return doc.Collection{}, false, nil
	}
	id, err := ids.Claim("pb:type:" + scope + ":" + categoryKey + ":" + typeKey)
	if err != nil {
		return doc.Collection{}, false, err
	}
	collection := doc.Collection{
		ID:    id,
		Title: typeLabel,
		Kind:  doc.KindPoint,
		// The type key, so artwork dropped into the archive later attaches on
		// the next build without a policy change.
		Icon:     typeKey,
		Visible:  true,
		Attrs:    map[string]string{semconv.KeyRenderAs: renderAs},
		Features: make([]doc.Feature, 0, len(pins)),
	}
	for _, p := range pins {
		featureID, err := ids.Claim("pb:pin:" + p.ID)
		if err != nil {
			return doc.Collection{}, false, err
		}
		x, err := strconv.ParseFloat(p.X, 64)
		if err != nil {
			return doc.Collection{}, false, fmt.Errorf("pin %s x %q: %w", p.ID, p.X, err)
		}
		y, err := strconv.ParseFloat(p.Y, 64)
		if err != nil {
			return doc.Collection{}, false, fmt.Errorf("pin %s y %q: %w", p.ID, p.Y, err)
		}
		pixelX, pixelY := worldPixel(raw.Map.Transform, x, y)
		name := p.Name
		if name == "" {
			name = typeLabel
		}
		at := doc.SyntheticPosition(pixelX, pixelY)
		collection.Features = append(collection.Features, doc.Feature{
			ID:          featureID,
			Title:       name,
			Description: p.Description,
			At:          &at,
		})
	}
	return collection, true, nil
}

// attachArtwork reads the archived icon for every collection that names one,
// once per key, in the order the collections name them.
func (s Source) attachArtwork(a *archive.Archive, v archive.VolumeRef, out *doc.Document) error {
	seen := make(map[string]bool)
	for _, world := range out.Worlds {
		for _, collection := range world.Collections {
			if collection.Icon == "" || seen[collection.Icon] {
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

// worldPixel runs a pin's game coordinates through the map's transformation onto
// the zoom-zero tile, then scales to the world square.
func worldPixel(transform Transform, x, y float64) (float64, float64) {
	const zoomZeroSpan = 256
	scale := float64(doc.SyntheticWorldSize) / zoomZeroSpan
	return (transform.A*x + transform.B) * scale, (transform.C*y + transform.D) * scale
}

// deepestWindowCenter is the middle of the deepest window the crawler observed,
// which is where a reader opens: Piggyback publishes no opening view, and the
// tiles it actually served are the best account of where the map is.
func deepestWindowCenter(raw *capture) (float64, float64) {
	deepest := raw.Levels[len(raw.Levels)-1]
	span := float64(doc.SyntheticWorldSize) / float64(int(1)<<deepest.Zoom)
	x := (float64(deepest.MinX) + float64(deepest.MaxX+1)) / 2 * span
	y := (float64(deepest.MinY) + float64(deepest.MaxY+1)) / 2 * span
	return x, y
}

// TileSetPath cuts the layer path out of the tile URL template: everything
// between "/tiles/" and the zoom placeholder, which is how tile records group
// into pyramids downstream. It is exported because the crawler writes tiles
// under it and the reader names it, and the two have to agree.
func TileSetPath(template string) (string, error) {
	_, rest, found := strings.Cut(template, "/tiles/")
	if !found {
		return "", fmt.Errorf("tile template %q has no /tiles/ segment", template)
	}
	path, _, found := strings.Cut(rest, "/{z}")
	if !found || path == "" {
		return "", fmt.Errorf("tile template %q names no layer before its zoom", template)
	}
	return path, nil
}

// TileExtension reads the image format off the tile URL template.
func TileExtension(template string) string {
	if dot := strings.LastIndex(template, "."); dot >= 0 && dot < len(template)-1 {
		return strings.ToLower(template[dot+1:])
	}
	return "webp"
}

func index(in []label) map[string]string {
	out := make(map[string]string, len(in))
	for _, l := range in {
		out[l.Key] = l.Label
	}
	return out
}

func spelled(index map[string]string, key string) string {
	if l, ok := index[key]; ok && l != "" {
		return l
	}
	return doc.Title(key)
}

func named(given, slug string) string {
	if given != "" {
		return given
	}
	return doc.Title(slug)
}

func sortedKeys(byType map[string][]pin) []string {
	keys := make([]string, 0, len(byType))
	for key := range byType {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
