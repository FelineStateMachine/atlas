// Package ign reads a captured IGN wikimap and translates it into the Atlas
// interchange document.
//
// IGN publishes community wikimaps as a flat image tiled like a world, markers
// placed on it in image-relative coordinates, and a flat list of marker types
// that names a parent to make two levels of legend. This package is the only
// thing in Atlas that knows any of that.
//
// # The coordinate design
//
// A wikimap's image is normalized so its taller dimension spans 1. A marker's
// latitude runs down that span and is negative, its longitude across it, so a
// marker at (lng, -lat) times the world square's edge is the pixel it is drawn
// on. The pixel becomes a synthetic position through the same inverse Mercator
// every picture-publishing source uses, and the raster and the markers land in
// one space.
//
// # The gate: an embedded MapGenie map is not an IGN map
//
// Some of IGN's wikimap pages are MapGenie maps in an IGN frame -- the page
// declares a MapGenie game id and serves MapGenie's tiles. Capturing one through
// this source would archive a second, worse copy of data Atlas already reads
// properly, and a merge would then fold a source into itself. The refusal lives
// at capture, where the page's own declaration is still in front of the crawler
// (internal/generate/crawl), because by the time a capture exists the evidence
// has been thrown away. What this reader can still refuse it does: a capture of
// another source's kind, a capture naming no map, a map with no size, a type
// declared twice, a marker of a type nothing declared.
//
// # Determinism
//
// Types sort by slug and markers by id -- the crawler does it before archiving,
// and this reader does it again because an archive holds captures older than the
// habit. Legend order then follows the capture: types in slug order, gathered
// under the heading of whichever parent each names, headings in the order their
// first type appears.
package ign

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// captureKind is what the archive calls an IGN capture, and source is what a
// well-formed capture says it is.
const (
	captureKind = "ign-map"
	source      = "ign-wikimaps"
)

// lensName rides into a reader's lens picker -- and, when this raster is
// resampled into a finer source's world, it names the aligned variant there too,
// so a reader can always see where a picture came from.
const lensName = "IGN Wiki"

// artworkExtensions are the forms the archive holds a type's icon in. IGN serves
// PNG sprites; the SVG probe is there so a hand-drawn replacement wins.
var artworkExtensions = []string{".svg", ".png"}

// Source reads IGN wikimap captures.
type Source struct{}

// New builds the source. It holds no state.
func New() Source { return Source{} }

// Describe is the source's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  "ign",
		Label: "IGN Wiki",
		License: "All rights reserved. IGN's wikimaps are editorial work published " +
			"under ign.com's terms; a volume carrying them is for personal use.",
		Attribution: "Map imagery and marker data by IGN and its wiki contributors, " +
			"captured from ign.com.",
		// IGN numbers markers with opaque strings and types with slugs, neither
		// of which a bundle's wire can carry, so every identity is minted from
		// a stable name.
		IDSpace: doc.IDSpaceDerived,
	}
}

// Translate reads one archived volume.
func (s Source) Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error) {
	log = log.With(logging.Source("ign"))
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
			"capture %s is of kind %q; the IGN reader answers only for %q",
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
	switch {
	case raw.Source != source:
		return doc.World{}, doc.Volume{}, fmt.Errorf(
			"capture %s says its source is %q, not %q", archived.ContentHash, raw.Source, source)
	case raw.ObjectSlug == "" || raw.MapSlug == "":
		return doc.World{}, doc.Volume{}, fmt.Errorf("capture %s names no map", archived.ContentHash)
	case raw.Map.Width <= 0 || raw.Map.Height <= 0:
		return doc.World{}, doc.Volume{}, fmt.Errorf("capture %s draws a map with no size", archived.ContentHash)
	}
	raw.normalize()

	ids := doc.NewIDSpace()
	scope := raw.ObjectSlug + "/" + raw.MapSlug
	worldID, err := ids.Claim("ign:map:" + scope)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	collections, err := collectionsOf(&raw, ids, scope)
	if err != nil {
		return doc.World{}, doc.Volume{}, fmt.Errorf("map %s: %w", scope, err)
	}

	world := doc.World{
		ID:     worldID,
		Slug:   raw.MapSlug,
		Title:  named(raw.MapTitle, raw.MapSlug),
		Center: imagePosition(raw.Map.InitialLng, raw.Map.InitialLat),
		Capture: doc.Capture{
			Kind:        archived.Kind,
			ID:          archived.SourceID,
			Locator:     archived.SourceURL,
			ContentHash: archived.ContentHash,
			CapturedAt:  archived.CapturedAt,
		},
		Lenses: []doc.Lens{{
			Name:    lensName,
			TileSet: scope,
			Frame:   frameOf(&raw),
		}},
		Collections: collections,
	}
	log.Debug("world translated", logging.World(raw.MapSlug),
		"collections", len(collections), "capture", archived.ContentHash)
	return world, doc.Volume{Slug: raw.ObjectSlug, Title: raw.GameTitle}, nil
}

// collectionsOf arranges IGN's flat marker types into the legend order a
// document carries: one collection per type, gathered under the heading of the
// parent it names, headings in the order their first type appears.
//
// A type no marker uses is left out -- an empty collection would only dim the
// legend -- but it stays in the capture, so a marker appearing under it later
// revives the collection without a policy change.
func collectionsOf(raw *capture, ids *doc.IDSpace, scope string) ([]doc.Collection, error) {
	declared := make(map[string]bool, len(raw.Types))
	for _, t := range raw.Types {
		if declared[t.TypeSlug] {
			return nil, fmt.Errorf("type %q is declared twice", t.TypeSlug)
		}
		declared[t.TypeSlug] = true
	}
	byType := make(map[string][]marker, len(raw.Types))
	for _, m := range raw.Markers {
		if !declared[m.TypeSlug] {
			return nil, fmt.Errorf("marker %s is of type %q, which nothing declares", m.ID, m.TypeSlug)
		}
		byType[m.TypeSlug] = append(byType[m.TypeSlug], m)
	}

	// Grouped by heading, then flattened in heading order: the flat array is the
	// legend, and a heading is a string on the collections that share it.
	grouped := make(map[string][]doc.Collection)
	var headings []string
	for _, t := range raw.Types {
		markers := byType[t.TypeSlug]
		if len(markers) == 0 {
			continue
		}
		id, err := ids.Claim("ign:type:" + scope + ":" + t.TypeSlug)
		if err != nil {
			return nil, err
		}
		collection := doc.Collection{
			ID:      id,
			Title:   t.TypeName,
			Kind:    doc.KindPoint,
			Icon:    t.TypeSlug,
			Visible: true,
			Attrs:   map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin},
		}
		for _, m := range markers {
			featureID, err := ids.Claim("ign:marker:" + m.ID)
			if err != nil {
				return nil, err
			}
			at := imagePosition(m.Lng, m.Lat)
			collection.Features = append(collection.Features, doc.Feature{
				ID:    featureID,
				Title: m.MarkerName,
				At:    &at,
			})
		}
		if _, seen := grouped[t.ParentTypeSlug]; !seen {
			headings = append(headings, t.ParentTypeSlug)
		}
		grouped[t.ParentTypeSlug] = append(grouped[t.ParentTypeSlug], collection)
	}

	var out []doc.Collection
	for _, heading := range headings {
		title := "Markers"
		if heading != "" {
			title = doc.Title(heading)
		}
		for _, collection := range grouped[heading] {
			collection.Group = title
			out = append(out, collection)
		}
	}
	return out, nil
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

// imagePosition lands a marker on the picture. IGN measures down the image with
// a negative latitude and across it with a positive longitude, both against an
// image whose taller dimension spans 1, so the world square's edge scales them
// into pixels.
func imagePosition(lng, lat float64) doc.Position {
	return doc.SyntheticPosition(lng*doc.SyntheticWorldSize, -lat*doc.SyntheticWorldSize)
}

// frameOf declares the wikimap's pyramid for the deriver. Every level is
// declared, because a wikimap is not cut from the corpus's shared window and a
// level left unsaid would be measured against a window it does not sit in.
func frameOf(raw *capture) *doc.Frame {
	frame := &doc.Frame{
		MinZoom: raw.Map.MinZoom,
		MaxZoom: raw.Map.MaxZoom,
		Format:  TileExtension(raw.Map.Tileset),
		Windows: make(map[string]doc.TileWindow, raw.Map.MaxZoom+1),
	}
	for zoom := 0; zoom <= raw.Map.MaxZoom; zoom++ {
		maxX, maxY := LevelExtent(raw.Map.Width, raw.Map.Height, zoom)
		frame.Windows[strconv.Itoa(zoom)] = doc.TileWindow{MaxX: maxX, MaxY: maxY}
	}
	return frame
}

func named(given, slug string) string {
	if given != "" {
		return given
	}
	return doc.Title(slug)
}

// TileSetPath is the path a wikimap's tiles are captured under. It is exported
// because the crawler writes tiles at it and the reader names it, and the two
// have to agree.
func TileSetPath(objectSlug, mapSlug string) string { return objectSlug + "/" + mapSlug }

// LevelExtent reports the last tile column and row holding any of the image at a
// zoom. The crawler asks for exactly these tiles and the deriver expects exactly
// these bounds, because it is the same call.
func LevelExtent(width, height float64, zoom int) (maxX, maxY int) {
	across := float64(int(1) << zoom)
	return lastTile(width, across), lastTile(height, across)
}

func lastTile(extent, across float64) int {
	last := int(math.Ceil(extent*across)) - 1
	if last < 0 {
		return 0
	}
	return last
}

// TileExtension reads the image format off IGN's tile URL template.
func TileExtension(template string) string {
	if dot := strings.LastIndex(template, "."); dot >= 0 && dot < len(template)-1 {
		return strings.ToLower(template[dot+1:])
	}
	return "jpg"
}
