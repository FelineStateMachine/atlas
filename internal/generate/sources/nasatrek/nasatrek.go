// Package nasatrek reads a captured planetary map and translates it into the
// Atlas interchange document.
//
// A capture marries two public publications that know nothing of each other: a
// global equirectangular mosaic served by NASA's Trek tile services, and the IAU
// Gazetteer of Planetary Nomenclature's feature list for the same body. This
// package is where a body's named places land on its picture.
//
// # The coordinate design
//
// Trek mosaics are equirectangular and two tiles wide by one tall at their own
// zoom zero, so a Trek level sits one zoom up in the square pyramid the corpus
// cuts: square zoom z holds Trek zoom z-1, 2^z tiles across and 2^(z-1) down,
// the planet filling the top half of the world square. A feature's planetary
// coordinates become a pixel on that image -- x from longitude across the full
// width, y from latitude down the top half -- and the pixel becomes a synthetic
// position through the same inverse Mercator every picture-publishing source
// uses. The projection's distortion cancels exactly, because the raster and the
// features ride one mapping.
//
// Nothing is flattened away by that. The world declares the flattening as
// registered conventions, so any reader can run a packed position backward
// through it and stand on the planet, and every feature carries the coordinates
// the Gazetteer published, verbatim, beside its synthetic ones.
//
// # What is refused
//
// A capture of another source's kind; a capture naming no body, layer or map; a
// capture declaring no pyramid, or a sibling mosaic that declares none; the same
// mosaic captured twice; a Gazetteer entry with no identifier, no name or no
// type, or one sitting off the planet. The reader states its preconditions and
// refuses what does not meet them rather than guessing.
//
// # Determinism
//
// Feature types are sorted, features within a type keep the Gazetteer's own
// identifier order, and every identifier is derived from a stable name. The same
// archived bytes give the same document on any machine.
package nasatrek

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// captureKind is what the archive calls a Trek capture, and source is what a
// well-formed capture says it is.
const (
	captureKind = "trek-map"
	source      = "nasa-trek"
)

// The declared flattening, spelled once for the world's attributes and once for
// the projection below: the mosaic fills the top half of the world square and
// pictures the whole ground, -180..180 west to east and 90..-90 top to bottom.
const (
	equirectPx  = "0,0,8192,4096"
	equirectDeg = "-180,90,180,-90"
)

// Source reads NASA Trek captures.
type Source struct{}

// New builds the source. It holds no state.
func New() Source { return Source{} }

// Describe is the source's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  source,
		Label: "NASA Trek",
		License: "Public domain. NASA and USGS imagery carries no copyright, and " +
			"the IAU Gazetteer of Planetary Nomenclature is published freely.",
		Attribution: "Global mosaics from NASA's Solar System Treks (trek.nasa.gov), " +
			"assembled by NASA, USGS and the mission teams named in each layer. " +
			"Feature names from the IAU Gazetteer of Planetary Nomenclature.",
		// Neither publication numbers things the way a bundle needs: the
		// Gazetteer's identifiers are its own and there is no numbering at all
		// for a mosaic or a feature type, so every identity here is derived from
		// a stable name.
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
		world, body, err := s.translateWorld(a, ref, log)
		if err != nil {
			if errors.Is(err, archive.ErrNotReady) {
				log.Debug("world skipped", logging.Path(archive.TrimRoot(a.Root(), ref.Dir())),
					"reason", err.Error())
				continue
			}
			return doc.Document{}, err
		}
		if out.Volume.Slug == "" {
			// A planet is a volume, and its slug is the body's own lowercase
			// name -- free for another source's capture of the same body to
			// answer to, which is the point of naming it that way.
			out.Volume = doc.Volume{Slug: body, Title: doc.Title(body)}
		}
		out.Worlds = append(out.Worlds, world)
	}
	if len(out.Worlds) == 0 {
		return doc.Document{}, fmt.Errorf("%w: volume %s has no readable world", archive.ErrNotReady, v.Title)
	}
	log.Info("volume translated", logging.Volume(out.Volume.Slug), "worlds", len(out.Worlds))
	return out, nil
}

func (s Source) translateWorld(a *archive.Archive, ref archive.WorldRef, log *slog.Logger) (doc.World, string, error) {
	archived, err := a.Newest(ref)
	if err != nil {
		return doc.World{}, "", err
	}
	if archived.Kind != captureKind {
		return doc.World{}, "", fmt.Errorf(
			"capture %s is of kind %q; the NASA Trek reader answers only for %q",
			archived.ContentHash, archived.Kind, captureKind)
	}
	body, err := a.Body(ref, archived)
	if err != nil {
		return doc.World{}, "", err
	}
	var raw capture
	if err := json.Unmarshal(body, &raw); err != nil {
		return doc.World{}, "", fmt.Errorf("decode capture %s: %w", archived.ContentHash, err)
	}
	if err := check(&raw); err != nil {
		return doc.World{}, "", fmt.Errorf("capture %s: %w", archived.ContentHash, err)
	}
	raw.normalize()

	ids := doc.NewIDSpace()
	scope := raw.Body + "/" + raw.MapSlug
	worldID, err := ids.Claim("trek:map:" + scope)
	if err != nil {
		return doc.World{}, "", err
	}

	world := doc.World{
		ID:    worldID,
		Slug:  raw.MapSlug,
		Title: named(raw.MapTitle, raw.MapSlug),
		// A reader opens on the middle of the planet's picture: halfway across
		// the world square, a quarter down it, which is where a 2:1 image's
		// centre sits in a square.
		Center: doc.SyntheticPosition(doc.SyntheticWorldSize/2, doc.SyntheticWorldSize/4),
		Capture: doc.Capture{
			Kind:        archived.Kind,
			ID:          archived.SourceID,
			Locator:     archived.SourceURL,
			ContentHash: archived.ContentHash,
			CapturedAt:  archived.CapturedAt,
		},
		// The world says what it pictures: a sphere, flattened by the
		// equirectangular projection into the top half of the world square. The
		// mapping is the whole story, and it is exactly the transform
		// worldPixel projects features through, so the two cannot disagree
		// without a test noticing.
		Attrs: map[string]string{
			semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
			semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
			semconv.KeyGeometryEquirectPx:  equirectPx,
			semconv.KeyGeometryEquirectDeg: equirectDeg,
			semconv.KeyGeometryBody:        raw.Body,
		},
	}
	world.Lenses = append(world.Lenses, doc.Lens{
		Name:    named(raw.Map.LayerTitle, "default"),
		TileSet: TileSetPath(raw.Body, raw.Layer),
	})
	for _, sibling := range raw.Variants {
		world.Lenses = append(world.Lenses, doc.Lens{
			Name:    named(sibling.Title, "default"),
			TileSet: TileSetPath(raw.Body, sibling.Layer),
		})
	}

	collections, err := collectionsOf(&raw, ids, scope)
	if err != nil {
		return doc.World{}, "", err
	}
	world.Collections = collections
	log.Debug("world translated", logging.World(raw.MapSlug),
		"collections", len(collections), "capture", archived.ContentHash)
	return world, raw.Body, nil
}

// check states what a capture has to be before anything is read out of it.
func check(raw *capture) error {
	if raw.Source != source {
		return fmt.Errorf("capture says its source is %q, not %q", raw.Source, source)
	}
	if raw.Body == "" || raw.Layer == "" || raw.MapSlug == "" {
		return fmt.Errorf("capture names no map")
	}
	if raw.Map.MaxZoom < 1 {
		return fmt.Errorf("capture declares no pyramid")
	}
	if len(raw.Features) == 0 {
		return fmt.Errorf("capture carries no features")
	}
	taken := map[string]bool{raw.Layer: true}
	for _, sibling := range raw.Variants {
		if sibling.Layer == "" || sibling.MaxZoom < 1 {
			return fmt.Errorf("sibling mosaic %q declares no pyramid", sibling.Layer)
		}
		if taken[sibling.Layer] {
			return fmt.Errorf("mosaic %q is captured twice", sibling.Layer)
		}
		taken[sibling.Layer] = true
	}
	return nil
}

// TileSetPath is the path a body's mosaic is captured under. It is exported
// because the crawler writes tiles at it and the reader names it, and the two
// have to agree.
func TileSetPath(body, layer string) string { return body + "/EQ/" + layer }

// LevelExtent reports the last tile column and row of one square level: the full
// width of the world square, and the top half of its height. The crawler asks
// for exactly these tiles and the deriver expects exactly these bounds, because
// it is the same call.
func LevelExtent(zoom int) (maxX, maxY int) {
	if zoom == 0 {
		return 0, 0
	}
	return 1<<zoom - 1, 1<<(zoom-1) - 1
}

// collectionsOf arranges the Gazetteer's flat feature list the way a legend
// reads: one collection per feature type, all under one heading. The type's
// descriptor -- "Crater, craters" names the singular and the plural -- keeps its
// singular half as the collection title and lends its slug as the artwork key,
// so artwork dropped into an archive later attaches without a policy change.
func collectionsOf(raw *capture, ids *doc.IDSpace, scope string) ([]doc.Collection, error) {
	byType := make(map[string][]feature)
	var order []string
	for _, entry := range raw.Features {
		if err := checkFeature(entry); err != nil {
			return nil, err
		}
		if _, seen := byType[entry.Type]; !seen {
			order = append(order, entry.Type)
		}
		byType[entry.Type] = append(byType[entry.Type], entry)
	}
	sort.Strings(order)

	out := make([]doc.Collection, 0, len(order))
	for _, featureType := range order {
		name := typeName(featureType)
		id, err := ids.Claim("trek:type:" + scope + ":" + name)
		if err != nil {
			return nil, err
		}
		attrs := map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin}
		// The standard glyph is named, not resolved: resolving it against the
		// icon library is an enricher's work, and a source that resolved it
		// would be deciding what artwork a bundle carries.
		if standard := standardIcons[byType[featureType][0].Code]; standard != "" {
			attrs[semconv.KeyIconStd] = standard
		}
		collection := doc.Collection{
			ID:       id,
			Title:    name,
			Group:    "Nomenclature",
			Kind:     doc.KindPoint,
			Icon:     doc.Slugify(name),
			Visible:  true,
			Attrs:    attrs,
			Features: make([]doc.Feature, 0, len(byType[featureType])),
		}
		for _, entry := range byType[featureType] {
			featureID, err := ids.Claim("trek:feature:" + strconv.FormatInt(entry.ID, 10))
			if err != nil {
				return nil, err
			}
			x, y := worldPixel(entry.Longitude, entry.Latitude)
			at := doc.SyntheticPosition(x, y)
			collection.Features = append(collection.Features, doc.Feature{
				ID:          featureID,
				Title:       entry.Name,
				Description: describe(entry),
				At:          &at,
				// The coordinates as the Gazetteer published them, verbatim:
				// provenance beside the synthetic position, so no reader has to
				// parse a card to learn where a place truly is.
				Attrs: map[string]string{
					semconv.KeyGeoLat: strconv.FormatFloat(entry.Latitude, 'f', -1, 64),
					semconv.KeyGeoLon: strconv.FormatFloat(entry.Longitude, 'f', -1, 64),
				},
			})
		}
		out = append(out, collection)
	}
	return out, nil
}

func checkFeature(entry feature) error {
	switch {
	case entry.ID <= 0:
		return fmt.Errorf("feature %q has no identifier", entry.Name)
	case entry.Name == "":
		return fmt.Errorf("feature %d has no name", entry.ID)
	case entry.Type == "":
		return fmt.Errorf("feature %q has no type", entry.Name)
	case entry.Latitude < -90 || entry.Latitude > 90:
		return fmt.Errorf("feature %q sits at latitude %v", entry.Name, entry.Latitude)
	case entry.Longitude < 0 || entry.Longitude > 360:
		return fmt.Errorf("feature %q sits at longitude %v", entry.Name, entry.Longitude)
	}
	return nil
}

// worldPixel lands a feature's planetary coordinates on the picture. The mosaic
// spans longitude -180..180 west to east and latitude 90..-90 top to bottom; the
// Gazetteer speaks east-positive 0..360, so a longitude wraps into the mosaic's
// half-open window first. X crosses the full world square, y only its top half,
// because that is where a 2:1 image sits in a square.
func worldPixel(longitude, latitude float64) (x, y float64) {
	wrapped := math.Mod(longitude+180, 360) - 180
	x = (wrapped + 180) / 360 * doc.SyntheticWorldSize
	y = (90 - latitude) / 180 * (doc.SyntheticWorldSize / 2)
	return x, y
}

func named(given, slug string) string {
	if given != "" {
		return given
	}
	return doc.Title(slug)
}

// typeName keeps the singular half of the Gazetteer's "Crater, craters"
// descriptor; a descriptor without a plural half is already the name.
func typeName(descriptor string) string {
	for at, r := range descriptor {
		if r == ',' {
			return descriptor[:at]
		}
	}
	return descriptor
}

// describe writes a feature's card: its real place on the planet, its size where
// the Gazetteer gives one, and the origin of its name.
func describe(entry feature) string {
	text := place(entry.Latitude, entry.Longitude)
	if entry.DiameterKM > 0 {
		text += fmt.Sprintf(" · %s km across", trimmed(entry.DiameterKM))
	}
	if entry.Origin != "" {
		text += " — " + entry.Origin
	}
	return text
}

func place(latitude, longitude float64) string {
	ns := "N"
	if latitude < 0 {
		ns, latitude = "S", -latitude
	}
	return fmt.Sprintf("%s°%s %s°E", trimmed(latitude), ns, trimmed(longitude))
}

// trimmed spells a value to two decimals and no further, dropping the trailing
// zeroes a card does not need.
func trimmed(value float64) string {
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64)
}

// standardIcons names a library glyph for each Gazetteer feature-type code, in
// the set/name vocabulary of atlas.icon.std. The codes are the IAU's own and
// hold across bodies, so the Moon's craters will wear the same rim the day a
// capture arrives. The reading leans semantic -- a mons is a mountain, a patera
// a volcano, a palus literally a marsh -- and falls back to shape language where
// no glyph says the thing: rimmed circles for depressions, squares for plains,
// triangles for relief. A code missing here is not an error; its collection goes
// without, and re-curating is one table edit and a re-translation of captures
// already on disk.
var standardIcons = map[string]string{
	"AA": "maki/circle-stroked",   // crater: a rim
	"CA": "maki/circle-stroked",   // catena: a chain of them
	"CB": "maki/circle-stroked",   // cavus: a hollow
	"AL": "maki/circle",           // albedo feature: a patch of tone
	"MA": "maki/circle",           // macula: a dark spot
	"MO": "maki/mountain",         // mons
	"TH": "maki/mountain",         // tholus: a small domical mountain
	"CO": "maki/triangle-stroked", // collis: small hills
	"DO": "maki/triangle-stroked", // dorsum: a ridge
	"RU": "maki/triangle",         // rupes: a scarp
	"SC": "maki/triangle",         // scopulus: a lobate scarp
	"PA": "maki/volcano",          // patera: an irregular volcanic crater
	"CH": "maki/caution",          // chaos: broken terrain
	"LA": "maki/caution",          // labes: a landslide
	"CM": "maki/wetland",          // chasma: parallel-walled canyon, read as lines
	"FO": "maki/wetland",          // fossa: a long narrow trench
	"LB": "maki/wetland",          // labyrinthus: intersecting valleys
	"SU": "maki/wetland",          // sulcus: subparallel furrows
	"PE": "maki/wetland",          // palus: literally a marsh
	"VA": "maki/water",            // vallis: a channel something carved
	"SE": "maki/water",            // serpens: a sinuous feature
	"FL": "maki/waterfall",        // fluctus: flow terrain
	"PL": "maki/square-stroked",   // planitia: a low plain
	"VS": "maki/square-stroked",   // vastitas: a vast one
	"PM": "maki/square",           // planum: a plateau
	"MN": "maki/square",           // mensa: a mesa
	"TA": "maki/landuse",          // terra: extensive land
	"LN": "maki/landuse",          // lingula: a tongue of plateau
	"UN": "maki/beach",            // unda: dunes
}
