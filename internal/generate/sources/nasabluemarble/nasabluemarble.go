// Package nasabluemarble reads a captured NASA Blue Marble base map and
// translates it into the Atlas interchange document.
//
// The capture is one pinned publication: Blue Marble Next Generation, NASA
// Earth Observatory's global, cloud-free, true-color composite of the whole
// planet. Where the NASA Trek reader marries a mosaic to a gazetteer, this
// reader marries a picture to the lightest cut of ground truth: one world, one
// lens, the primary capitals as a point collection per continent, and the
// country borders as one quiet area collection -- Natural Earth's 1:110m
// public-domain vectors, distilled into the capture beside the raster. It
// exists so that Earth can travel as an ordinary volume: the included build
// every library starts with, a working demo of pins and ground, and the base
// later readings enrich.
//
// # The coordinate design
//
// The image is the shared whole-sphere equirectangular window (doc.EquirectPx):
// a 2:1 global image filling the top half of the world square, -180..180 west
// to east and 90..-90 top to bottom. The world declares the flattening as
// registered conventions, so a reader can run any position backward through it
// and stand on the planet, and the cell systems recognise the ground through
// the same declarations every spherical world makes.
//
// # What is refused
//
// A capture of another source's kind or naming another source; a capture of a
// body that is not the Earth; one declaring no pyramid, no digest, or an image
// with no size. The reader states its preconditions and refuses what does not
// meet them rather than guessing.
package nasabluemarble

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// CaptureKind is what the archive calls a Blue Marble capture, and SourceName
// is what a well-formed capture says it is. Both are exported because the
// crawler writes them and the reader answers only for them, and the two have to
// agree.
const (
	CaptureKind = "blue-marble-map"
	SourceName  = "nasa-blue-marble"
)

// Body is the one body this source pictures. The volume and its world both
// answer to it, which is the point: another source's capture of the same planet
// registers the same slug and the two builds answer for one library entry.
const Body = "earth"

// TileSet is the path the base map's tiles are captured under. It is exported
// because the crawler writes tiles at it and the reader names it, and the two
// have to agree.
const TileSet = Body + "/blue-marble"

// radiusKM is the Earth's mean radius as the IAU nominal value spells it,
// declared on the world so a reader that measures ground has the number the
// picture was flattened from.
const radiusKM = "6371.0088"

// Source reads NASA Blue Marble captures.
type Source struct{}

// New builds the source. It holds no state.
func New() Source { return Source{} }

// Describe is the source's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  SourceName,
		Label: "NASA Earth Observatory",
		License: "Public domain. NASA imagery carries no copyright; NASA's media guidelines " +
			"ask that NASA Earth Observatory be credited and that no NASA endorsement be implied. " +
			"Natural Earth data is in the public domain.",
		Attribution: "Blue Marble: Next Generation, a global true-color base map by " +
			"NASA Earth Observatory, from NASA Goddard Space Flight Center imagery. " +
			"Country borders and capitals from Natural Earth, drawn de facto as that " +
			"project draws them.",
		// Nothing in the publication numbers a base map, so every identity here
		// is derived from a stable name.
		IDSpace: doc.IDSpaceDerived,
	}
}

// Translate reads one archived volume.
func (s Source) Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error) {
	log = log.With(logging.Source(SourceName))
	worlds, err := a.Worlds(v)
	if err != nil {
		return doc.Document{}, err
	}
	out := doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Volume:  doc.Volume{Slug: Body, Title: doc.Title(Body)},
		Source:  s.Describe(),
	}
	for _, ref := range worlds {
		world, err := s.translateWorld(a, ref, log)
		if err != nil {
			if errors.Is(err, archive.ErrNotReady) {
				log.Debug("world skipped", logging.Path(archive.TrimRoot(a.Root(), ref.Dir())),
					"reason", err.Error())
				continue
			}
			return doc.Document{}, err
		}
		out.Worlds = append(out.Worlds, world)
	}
	if len(out.Worlds) == 0 {
		return doc.Document{}, fmt.Errorf("%w: volume %s has no readable world", archive.ErrNotReady, v.Title)
	}
	log.Info("volume translated", logging.Volume(out.Volume.Slug), "worlds", len(out.Worlds))
	return out, nil
}

func (s Source) translateWorld(a *archive.Archive, ref archive.WorldRef, log *slog.Logger) (doc.World, error) {
	archived, err := a.Newest(ref)
	if err != nil {
		return doc.World{}, err
	}
	if archived.Kind != CaptureKind {
		return doc.World{}, fmt.Errorf(
			"capture %s is of kind %q; the Blue Marble reader answers only for %q",
			archived.ContentHash, archived.Kind, CaptureKind)
	}
	body, err := a.Body(ref, archived)
	if err != nil {
		return doc.World{}, err
	}
	var raw capture
	if err := json.Unmarshal(body, &raw); err != nil {
		return doc.World{}, fmt.Errorf("decode capture %s: %w", archived.ContentHash, err)
	}
	if err := check(&raw); err != nil {
		return doc.World{}, fmt.Errorf("capture %s: %w", archived.ContentHash, err)
	}

	ids := doc.NewIDSpace()
	worldID, err := ids.Claim("bluemarble:map:" + raw.Body)
	if err != nil {
		return doc.World{}, err
	}

	world := doc.World{
		ID:    worldID,
		Slug:  raw.Body,
		Title: doc.Title(raw.Body),
		// A reader opens on the middle of the planet's picture.
		Center: doc.EquirectCenter(),
		Capture: doc.Capture{
			Kind:        archived.Kind,
			ID:          archived.SourceID,
			Locator:     archived.SourceURL,
			ContentHash: archived.ContentHash,
			CapturedAt:  archived.CapturedAt,
		},
		// The world says what it pictures: the Earth, flattened by the
		// equirectangular projection into the top half of the world square.
		// The declarations are the shared whole-sphere ones, plus the body and
		// its radius, so the cell systems and any measuring reader stand on
		// the same ground the raster pictures.
		Attrs: map[string]string{
			semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
			semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
			semconv.KeyGeometryEquirectPx:  doc.EquirectPx,
			semconv.KeyGeometryEquirectDeg: doc.EquirectDeg,
			semconv.KeyGeometryBody:        raw.Body,
			semconv.KeyGeometryRadiusKM:    radiusKM,
		},
		Lenses: []doc.Lens{{
			Name:    named(raw.Map.LayerTitle, "blue-marble"),
			TileSet: TileSet,
			Frame:   doc.EquirectFrame(raw.Map.MaxZoom, raw.Map.Extension),
		}},
	}
	collections, err := collectionsOf(&raw, ids)
	if err != nil {
		return doc.World{}, err
	}
	world.Collections = collections
	log.Debug("world translated", logging.World(world.Slug), "capture", archived.ContentHash)
	return world, nil
}

// check states what a capture has to be before anything is read out of it.
func check(raw *capture) error {
	switch {
	case raw.Source != SourceName:
		return fmt.Errorf("capture says its source is %q, not %q", raw.Source, SourceName)
	case raw.Body != Body:
		return fmt.Errorf("capture pictures %q; the Blue Marble reader answers only for %q", raw.Body, Body)
	case raw.AssetSHA256 == "":
		return errors.New("capture names no source digest")
	case raw.Width <= 0 || raw.Height <= 0:
		return errors.New("capture names no source image size")
	case raw.Map.MaxZoom < 1:
		return errors.New("capture declares no pyramid")
	case len(raw.Features.Countries) == 0 || len(raw.Features.Capitals) == 0:
		return errors.New("capture carries no features")
	}
	for _, entry := range raw.Features.Countries {
		if entry.Name == "" || entry.A3 == "" || entry.Continent == "" {
			return fmt.Errorf("country %q carries no identity", entry.Name)
		}
	}
	for _, entry := range raw.Features.Capitals {
		switch {
		case entry.Name == "":
			return errors.New("a capital carries no name")
		case entry.Lat < -90 || entry.Lat > 90 || entry.Lon < -180 || entry.Lon > 180:
			return fmt.Errorf("capital %q sits at %v, %v", entry.Name, entry.Lat, entry.Lon)
		}
	}
	return nil
}

func named(given, slug string) string {
	if given != "" {
		return given
	}
	return doc.Title(slug)
}
