// Package arcgishub reads a captured city and translates it into the Atlas
// interchange document.
//
// A capture comes from a city's ArcGIS Hub open-data site: the municipal
// datasets a city publishes about itself -- its limits, its zoning, its trails,
// its historic buildings -- kept verbatim under a reviewed field allowlist, plus
// the national hydrography every US city is enriched with by bounding box.
//
// # What a city is to a bundle
//
// A city is a volume, and **each crawl day is a world**. That is the whole
// design: a volume's world picker becomes the city's version history, four
// captured days are four worlds, and the differences between them are
// differences in the city rather than differences in the pipeline. It is why a
// world's slug is a date and why the reader is told to read them newest first.
//
// Polygon and line datasets become shape collections -- one collection per
// dataset, one feature per curated bucket -- and point datasets become pins. The
// one picture is a basemap rendered offline from the city's own vector data,
// because a bundle owes its reader ground to stand on and ships without a
// network.
//
// # The membership join
//
// A capture carrying the national subwatersheds can say which subwatershed each
// of the city's own zones lies in, computed from the captured polygons at
// translate time. See hydro.go: the claim is made only when every sampled part
// of a zone agrees, so a zone that straddles two says nothing.
//
// # What is refused, and what is passed over
//
// Refused, because the capture is wrong: another source's kind; a world whose
// slug is not a capture day; no basemap pyramid, no window or no datasets; a
// dataset captured twice or not curated at all; a point dataset whose rows are
// all untitled, or too many of whose rows fall outside the declared window; a
// dataset whose curation makes more features than a legend can hold. The reader
// states its preconditions and refuses what does not meet them rather than
// guessing.
//
// Passed over, because the capture is not this reader's: **a city the table does
// not curate**. That is not a malformed capture, it is a capture nothing here
// can answer for, so it wraps ErrNotReady and the caller skips it. The case is
// real rather than theoretical -- an operator may have crawled their own city
// into the same archive, and the privacy rule keeps that city's name out of this
// table -- and a hard refusal would take every other volume in the archive down
// with it.
//
// # Determinism
//
// Datasets are read in the curation table's order, buckets are emitted in sorted
// key order, every identifier is derived from a stable name, and no clock or map
// iteration reaches the output. The same archived bytes give the same document
// on any machine.
package arcgishub

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// captureKind is what the archive calls a city capture, and source is what a
// well-formed capture says it is.
const (
	captureKind = "arcgis-map"
	source      = "arcgis-hub"
)

// Source reads ArcGIS Hub city captures.
type Source struct{}

// New builds the source. It holds no state.
func New() Source { return Source{} }

// Describe is the source's account of itself.
func (Source) Describe() doc.Provenance {
	return doc.Provenance{
		Name:  source,
		Label: "ArcGIS Open Data",
		License: "Open data published by each city under its own terms, and public-domain " +
			"federal hydrography from the USGS Watershed Boundary and National " +
			"Hydrography Datasets.",
		Attribution: "Municipal layers from each city's ArcGIS Hub open-data site. " +
			"Watersheds, subwatersheds, streams and waterbodies from the USGS " +
			"National Hydrography services.",
		// A hub numbers its rows with object ids that are load artifacts and
		// churn between refreshes, and it numbers nothing at all above a row --
		// no map, no layer, no bucket. Every identity here is minted from a
		// stable name instead.
		IDSpace: doc.IDSpaceDerived,
	}
}

// Translate reads one archived city: every crawl day it holds, in the archive's
// own order, each one a world.
func (s Source) Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error) {
	log = log.With(logging.Source(source))
	refs, err := a.Worlds(v)
	if err != nil {
		return doc.Document{}, err
	}
	out := doc.Document{
		Doc:     doc.Doc,
		Version: doc.Version,
		Source:  s.Describe(),
	}
	for _, ref := range refs {
		world, subject, err := s.translateWorld(a, ref, log)
		if err != nil {
			if errors.Is(err, archive.ErrNotReady) {
				log.Debug("world skipped", logging.Path(archive.TrimRoot(a.Root(), ref.Dir())),
					"reason", err.Error())
				continue
			}
			return doc.Document{}, err
		}
		if out.Volume.Slug == "" {
			out.Volume = subject
		}
		if out.Volume.Slug != subject.Slug {
			return doc.Document{}, fmt.Errorf(
				"the archive directory holds captures of two cities, %s and %s",
				out.Volume.Slug, subject.Slug)
		}
		out.Worlds = append(out.Worlds, world)
	}
	if len(out.Worlds) == 0 {
		return doc.Document{}, fmt.Errorf("%w: volume %s has no readable world", archive.ErrNotReady, v.Title)
	}
	log.Info("volume translated", logging.Volume(out.Volume.Slug), "worlds", len(out.Worlds))
	return out, nil
}

// daySlug holds a world's slug to a capture day, which is what makes a world
// picker read as a version history.
var daySlug = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s Source) translateWorld(
	a *archive.Archive,
	ref archive.WorldRef,
	log *slog.Logger,
) (doc.World, doc.Volume, error) {
	archived, err := a.Newest(ref)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	if archived.Kind != captureKind {
		return doc.World{}, doc.Volume{}, fmt.Errorf(
			"capture %s is of kind %q; the ArcGIS Hub reader answers only for %q",
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
	curated, err := check(&raw)
	if err != nil {
		return doc.World{}, doc.Volume{}, fmt.Errorf("capture %s: %w", archived.ContentHash, err)
	}
	raw.normalize()

	pairs, err := curatedOrder(curated, &raw)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}

	ids := doc.NewIDSpace()
	scope := raw.City + "/" + raw.MapSlug
	worldID, err := ids.Claim("arcgis:map:" + scope)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}

	px, deg := raw.Window.pxDeg()
	world := doc.World{
		ID: worldID,
		// The day is the world: its slug and its title, because a picker
		// listing dates is the version history reading as itself.
		Slug:  raw.MapSlug,
		Title: raw.MapSlug,
		// A reader opens on the middle of the city's picture.
		Center: doc.SyntheticPosition(doc.SyntheticWorldSize/2, doc.SyntheticWorldSize/2),
		Capture: doc.Capture{
			Kind:        archived.Kind,
			ID:          archived.SourceID,
			Locator:     archived.SourceURL,
			ContentHash: archived.ContentHash,
			CapturedAt:  archived.CapturedAt,
		},
		// The world says what it pictures: a plane -- no globe pretends a city
		// is a world -- cut from Earth by the declared Mercator window, which is
		// exactly the transform worldPixel projects features through, so the two
		// cannot disagree without a test noticing.
		Attrs: map[string]string{
			semconv.KeyGeometrySurface:     semconv.SurfacePlane,
			semconv.KeyGeometryBody:        "earth",
			semconv.KeyGeometryMercatorPx:  px,
			semconv.KeyGeometryMercatorDeg: deg,
		},
		Lenses: []doc.Lens{{
			Name:    "Basemap",
			TileSet: TileSetPath(raw.City, raw.MapSlug),
			Frame:   frameOf(raw.Basemap),
		}},
	}

	pins, err := pinCollections(&raw, pairs, ids)
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	shapes, err := shapeCollections(&raw, pairs, ids, buildHydroIndex(&raw))
	if err != nil {
		return doc.World{}, doc.Volume{}, err
	}
	// Pins first, then ground, which is the order a legend reads and the order
	// the packed payload's owner column counts point collections in.
	world.Collections = append(pins, shapes...)

	log.Debug("world translated", logging.World(raw.MapSlug),
		"collections", len(world.Collections), "capture", archived.ContentHash)
	return world, doc.Volume{Slug: raw.City, Title: named(raw.Title, curated.Title)}, nil
}

// check states what a capture has to be before anything is read out of it, and
// answers with the curation it is read through.
func check(raw *capture) (city, error) {
	if raw.Source != source {
		return city{}, fmt.Errorf("capture says its source is %q, not %q", raw.Source, source)
	}
	curated, known := cities[raw.City]
	if !known {
		// Not an error: a capture of an uncurated city is a volume this source
		// cannot answer for, and the archive is allowed to hold one. A machine
		// whose operator crawled their own city has exactly that, and the
		// public table may not name it (issue #5, the privacy rule), so the
		// volume is passed over the way any unreadable one is rather than
		// failing every other volume in the archive with it.
		return city{}, fmt.Errorf("%w: city %q is not curated, and an unverified window would "+
			"hang every pin on the wrong pixel", archive.ErrNotReady, raw.City)
	}
	if !daySlug.MatchString(raw.MapSlug) {
		return city{}, fmt.Errorf("world slug %q is not a capture day", raw.MapSlug)
	}
	if raw.Basemap.MaxZoom < 1 {
		return city{}, fmt.Errorf("capture declares no basemap pyramid")
	}
	if raw.Window.East <= raw.Window.West || raw.Window.North <= raw.Window.South {
		return city{}, fmt.Errorf("capture window has no ground")
	}
	if len(raw.Datasets) == 0 {
		return city{}, fmt.Errorf("capture carries no datasets")
	}
	return curated, nil
}

// pairing is one curated dataset joined to the rows captured for it.
type pairing struct {
	curated *dataset
	rows    *capturedDataset
}

// curatedOrder joins a capture's datasets back to the table, in the table's own
// order, refusing anything the table does not name -- the same posture the
// crawler takes at the door, held again at read time so a capture taken under an
// older table cannot smuggle a layer past a newer one.
func curatedOrder(curated city, raw *capture) ([]pairing, error) {
	captured := make(map[string]*capturedDataset, len(raw.Datasets))
	for at := range raw.Datasets {
		held := &raw.Datasets[at]
		if _, doubled := captured[held.Slug]; doubled {
			return nil, fmt.Errorf("dataset %q is captured twice", held.Slug)
		}
		captured[held.Slug] = held
	}
	var out []pairing
	table := curated.datasets()
	for at := range table {
		entry := &table[at]
		if rows, taken := captured[entry.Slug]; taken {
			out = append(out, pairing{entry, rows})
			delete(captured, entry.Slug)
		}
	}
	for slug := range captured {
		return nil, fmt.Errorf("dataset %q is not curated for %s", slug, curated.Slug)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("capture carries no curated datasets")
	}
	return out, nil
}

// frameOf declares the rendered pyramid the way the deriver reads one: every
// level complete, from a single tile at zero down to the deepest level drawn.
// A rendered basemap has no partial level by construction -- the renderer draws
// the whole square -- and saying so is what lands the derivation on the
// translated world's own grid rather than on the shared window a city does not
// sit in.
func frameOf(config mapConfig) *doc.Frame {
	frame := &doc.Frame{
		MinZoom: 0,
		MaxZoom: config.MaxZoom,
		Format:  archive.NormalizeFormat(named(config.Extension, "png")),
		Windows: make(map[string]doc.TileWindow, config.MaxZoom+1),
	}
	for zoom := 0; zoom <= config.MaxZoom; zoom++ {
		last := 1<<zoom - 1
		frame.Windows[strconv.Itoa(zoom)] = doc.TileWindow{MaxX: last, MaxY: last}
	}
	return frame
}

// strayTolerance is how much of a point dataset may fall outside the window
// before the capture is refused rather than trimmed: a hub layer occasionally
// holds one stray row, and one stray is not a wrong window.
const strayTolerance = 0.01

// pinCollections turns the point datasets into collections, one per dataset,
// each filed under its curated heading and in the table's own order.
func pinCollections(raw *capture, pairs []pairing, ids *doc.IDSpace) ([]doc.Collection, error) {
	var out []doc.Collection
	for _, pair := range pairs {
		curated, rows := pair.curated, pair.rows
		if curated.Group == "" {
			continue
		}
		id, err := ids.Claim("arcgis:cat:" + raw.City + "/" + curated.Slug)
		if err != nil {
			return nil, err
		}
		attrs := map[string]string{semconv.KeyRenderAs: semconv.RenderAsPin}
		// The standard glyph is named, not resolved: resolving it against the
		// icon library is composition's work, and a source that resolved it
		// would be deciding what artwork a bundle carries.
		if curated.Icon != "" {
			attrs[semconv.KeyIconStd] = curated.Icon
		}
		collection := doc.Collection{
			ID:       id,
			Title:    curated.Title,
			Group:    curated.Group,
			Kind:     doc.KindPoint,
			Icon:     doc.Slugify(curated.Title),
			Visible:  true,
			Attrs:    attrs,
			Features: make([]doc.Feature, 0, len(rows.Features)),
		}
		strays, untitled := 0, 0
		for _, row := range rows.Features {
			if row.Geometry.Type != geometryPoint || len(row.Geometry.Point) < 2 {
				return nil, fmt.Errorf("%s row %d has no point", curated.Slug, row.ID)
			}
			lon, lat := row.Geometry.Point[0], row.Geometry.Point[1]
			x, y := raw.Window.worldPixel(lon, lat)
			if x < 0 || x > doc.SyntheticWorldSize || y < 0 || y > doc.SyntheticWorldSize {
				strays++
				continue
			}
			// A nameless row is the publisher's hygiene, not the curation's: a
			// pin nobody could search for is left out. A dataset nameless
			// throughout is different -- that is a wrong field name, and it is
			// refused below rather than shipped blank.
			name := scrub(curated.TitleOf(row.Fields))
			if name == "" {
				untitled++
				continue
			}
			featureID, err := ids.Claim("arcgis:loc:" + raw.City + "/" + curated.Slug +
				"/" + strconv.FormatInt(row.ID, 10))
			if err != nil {
				return nil, err
			}
			at := doc.SyntheticPosition(x, y)
			collection.Features = append(collection.Features, doc.Feature{
				ID:          featureID,
				Title:       name,
				Description: scrub(curated.Describe(row.Fields)),
				At:          &at,
				// The coordinates as the city published them, verbatim:
				// provenance beside the synthetic position, so no reader has to
				// invert the window to learn where a pin truly stands.
				Attrs: map[string]string{
					semconv.KeyGeoLat: strconv.FormatFloat(lat, 'f', -1, 64),
					semconv.KeyGeoLon: strconv.FormatFloat(lon, 'f', -1, 64),
				},
			})
		}
		if total := len(rows.Features); total > 0 && float64(strays) > strayTolerance*float64(total) {
			return nil, fmt.Errorf("%s: %d of %d rows fall outside the window", curated.Slug, strays, total)
		}
		if total := len(rows.Features); total > 0 && untitled == total {
			return nil, fmt.Errorf("%s: no row carries a title; is the title field curated right?", curated.Slug)
		}
		out = append(out, collection)
	}
	return out, nil
}

// zoneLimit is the most features one shape collection may hold. The reader's
// legend renders every one of them a button and a title; an uncurated explosion
// -- every parcel a zone -- is refused while the mistake is one table edit old.
const zoneLimit = 256

// shapeCollections folds the polygon and line datasets into ground: one
// collection per dataset, and within it one feature per curated bucket, holding
// every row's geometry that fell into that bucket. Buckets of the city's own
// datasets -- never the national ones -- learn their subwatershed from the hydro
// index when all their ground agrees on one.
func shapeCollections(
	raw *capture,
	pairs []pairing,
	ids *doc.IDSpace,
	hydro *hydroIndex,
) ([]doc.Collection, error) {
	var out []doc.Collection
	for _, pair := range pairs {
		curated, rows := pair.curated, pair.rows
		if curated.ZoneOf == nil {
			continue
		}
		kind := doc.KindArea
		if curated.Geometry == "line" {
			kind = doc.KindPath
		}
		attrs := map[string]string{}
		if kind == doc.KindPath {
			attrs[semconv.KeyStrokeWidthPx] = strconv.FormatFloat(curated.StrokeWidth, 'f', -1, 64)
		}
		// Label policy is an area's word alone: a path is quiet by the
		// registry's own default, so a line dataset's curation says nothing.
		if curated.Label != "" && kind == doc.KindArea {
			attrs[semconv.KeyLabelPolicy] = curated.Label
		}
		if len(attrs) == 0 {
			attrs = nil
		}
		collection := doc.Collection{
			Key:     curated.Slug,
			Title:   curated.Title,
			Kind:    kind,
			Visible: true,
			Attrs:   attrs,
		}

		buckets := make(map[string]*doc.Feature)
		claims := make(map[string]*hydroClaims)
		var order []string
		for _, row := range rows.Features {
			key := curated.ZoneOf(row.Fields)
			if key.Key == "" {
				continue
			}
			if hydro != nil && !curated.National {
				claim, tracked := claims[key.Key]
				if !tracked {
					claim = &hydroClaims{}
					claims[key.Key] = claim
				}
				claim.observe(hydro, row.Geometry)
			}
			zone, made := buckets[key.Key]
			if !made {
				if len(buckets) >= zoneLimit {
					return nil, fmt.Errorf("%s makes more than %d features", curated.Slug, zoneLimit)
				}
				zoneID, err := ids.Claim("arcgis:zone:" + raw.City + "/" + curated.Slug + "/" + key.Key)
				if err != nil {
					return nil, err
				}
				zone = &doc.Feature{
					ID:       zoneID,
					Title:    scrub(key.Title),
					Subtitle: scrub(named(key.Subtitle, curated.Title)),
				}
				buckets[key.Key] = zone
				order = append(order, key.Key)
			}
			parts, err := shapeGeometry(raw.Window, curated, row)
			if err != nil {
				return nil, err
			}
			if parts != nil {
				zone.Geometry = append(zone.Geometry, *parts)
			}
		}
		sort.Strings(order)
		for _, key := range order {
			if len(buckets[key].Geometry) == 0 {
				continue
			}
			if claim := claims[key]; claim != nil {
				claim.apply(hydro, buckets[key])
			}
			collection.Features = append(collection.Features, *buckets[key])
		}
		out = append(out, collection)
	}
	return out, nil
}

// shapeGeometry lands one row's ground in the world in synthetic coordinates:
// rings project position by position into a MultiPolygon, and lines stay the
// MultiLineString they are -- the collection's declared stroke width says how
// wide the reader draws them.
func shapeGeometry(w window, curated *dataset, row feature) (*doc.Geometry, error) {
	switch row.Geometry.Type {
	case geometryRings:
		var polygons [][][][]float64
		for _, polygon := range row.Geometry.Rings {
			var rings [][][]float64
			for _, ring := range polygon {
				projected := make([][]float64, 0, len(ring))
				for _, position := range ring {
					if len(position) < 2 {
						return nil, fmt.Errorf("%s row %d has a malformed ring", curated.Slug, row.ID)
					}
					projected = append(projected, w.syntheticRing(position[0], position[1]))
				}
				// A ring needs three corners and a closing repeat to enclose
				// anything; anything shorter is a capture artifact.
				if len(projected) >= 4 {
					rings = append(rings, projected)
				}
			}
			if len(rings) > 0 {
				polygons = append(polygons, rings)
			}
		}
		if len(polygons) == 0 {
			return nil, nil
		}
		return marshalGeometry("MultiPolygon", polygons)
	case geometryLines:
		if curated.StrokeWidth <= 0 {
			return nil, fmt.Errorf("%s draws lines without a stroke width", curated.Slug)
		}
		var lines [][][]float64
		for _, line := range row.Geometry.Lines {
			projected := make([][]float64, 0, len(line))
			for _, position := range line {
				if len(position) < 2 {
					continue
				}
				projected = append(projected, w.syntheticRing(position[0], position[1]))
			}
			if len(projected) >= 2 {
				lines = append(lines, projected)
			}
		}
		if len(lines) == 0 {
			return nil, nil
		}
		return marshalGeometry("MultiLineString", lines)
	default:
		return nil, fmt.Errorf("%s row %d has no ground to draw", curated.Slug, row.ID)
	}
}

func marshalGeometry(kind string, coordinates any) (*doc.Geometry, error) {
	encoded, err := json.Marshal(coordinates)
	if err != nil {
		return nil, err
	}
	return &doc.Geometry{Type: kind, Coordinates: encoded}, nil
}

// scrub keeps a live URL out of everything this source emits. Bundles ship
// offline and the format refuses an http spelling anywhere in a payload; a
// curated field should never carry one, and any that slips through loses the
// token rather than failing a build a capture too late.
func scrub(text string) string {
	if !strings.Contains(strings.ToLower(text), "http") {
		return text
	}
	kept := []string{}
	for _, token := range strings.Fields(text) {
		if strings.Contains(strings.ToLower(token), "http") {
			continue
		}
		kept = append(kept, token)
	}
	return strings.Join(kept, " ")
}
