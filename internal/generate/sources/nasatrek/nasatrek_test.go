package nasatrek

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The Mars corpus document: the one real capture this repository keeps, because
// everything in it is public domain. It is the reference tree's reading of the
// same NASA Trek capture this package reads, and the tests below hold this
// package's coordinate design against it fact by fact -- every feature's pixel,
// every level's window, every canonical tile-set path -- with no archive, no
// network and no environment.
const (
	corpusDoc     = "../../../../testdata/corpus/translators/nasa-trek.doc.json"
	corpusSummary = "../../../../testdata/corpus/translators/nasa-trek.fixture.json"
)

// marsDocument is as much of the corpus document as these tests read. The field
// names are the reference tree's, which were MapGenie's; nothing outside this
// file names them.
type marsDocument struct {
	ID     int64             `json:"id"`
	Slug   string            `json:"slug"`
	Title  string            `json:"title"`
	Lat    float64           `json:"initial_latitude"`
	Lng    float64           `json:"initial_longitude"`
	Attrs  map[string]string `json:"atlas_attrs"`
	Game   struct{ Slug, Title string }
	Config struct {
		TileSets []struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			MinZoom   int    `json:"min_zoom"`
			MaxZoom   int    `json:"max_zoom"`
			Extension string `json:"extension"`
			Bounds    map[string]struct {
				X struct{ Min, Max int }
				Y struct{ Min, Max int }
			} `json:"bounds"`
		} `json:"tile_sets"`
	} `json:"config"`
	Groups []struct {
		Title      string `json:"title"`
		Categories []struct {
			ID        int64             `json:"id"`
			Title     string            `json:"title"`
			Icon      string            `json:"icon"`
			Visible   bool              `json:"visible"`
			Attrs     map[string]string `json:"atlas_attrs"`
			Locations []struct {
				ID          int64             `json:"id"`
				Title       string            `json:"title"`
				Description string            `json:"description"`
				Latitude    float64           `json:"latitude"`
				Longitude   float64           `json:"longitude"`
				Attrs       map[string]string `json:"atlas_attrs"`
			} `json:"locations"`
		} `json:"categories"`
	} `json:"groups"`
}

func readMars(t *testing.T) marsDocument {
	t.Helper()
	var out marsDocument
	readJSON(t, corpusDoc, &out)
	return out
}

// readSummary reads the fixture's own account of its size, so the counts these
// tests hold the document to are the capture's rather than constants copied in
// beside it.
func readSummary(t *testing.T) (categories, locations, tileSets int) {
	t.Helper()
	var summary struct {
		Document struct {
			TileSets   int `json:"tileSets"`
			Categories int `json:"categories"`
			Locations  int `json:"locations"`
		} `json:"document"`
	}
	readJSON(t, corpusSummary, &summary)
	return summary.Document.Categories, summary.Document.Locations, summary.Document.TileSets
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// near compares a coordinate the way a fixture round-tripped through JSON has
// to be compared: the reference document was written out and read back, so its
// last bit is a decimal spelling rather than the float that produced it.
func near(got, want float64) bool {
	return math.Abs(got-want) <= 1e-9*math.Max(1, math.Abs(want))
}

// TestEveryMarsFeatureLandsOnItsPixel is the coordinate design, held against
// all two thousand real places at once: the Gazetteer coordinates each location
// carries verbatim, pushed through this package's projection, land on exactly
// the position the reference tree published for it.
func TestEveryMarsFeatureLandsOnItsPixel(t *testing.T) {
	mars := readMars(t)
	_, locations, _ := readSummary(t)
	checked := 0
	for _, group := range mars.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				lat, err := strconv.ParseFloat(location.Attrs[semconv.KeyGeoLat], 64)
				if err != nil {
					t.Fatalf("%s carries no readable %s: %v", location.Title, semconv.KeyGeoLat, err)
				}
				lon, err := strconv.ParseFloat(location.Attrs[semconv.KeyGeoLon], 64)
				if err != nil {
					t.Fatalf("%s carries no readable %s: %v", location.Title, semconv.KeyGeoLon, err)
				}
				x, y := worldPixel(lon, lat)
				at := doc.SyntheticPosition(x, y)
				if !near(at.Lat, location.Latitude) || !near(at.Lng, location.Longitude) {
					t.Fatalf("%s at %v°,%v°E lands on %v,%v; the fixture put it at %v,%v",
						location.Title, lat, lon, at.Lat, at.Lng, location.Latitude, location.Longitude)
				}
				// And the card opens with the place the projection was fed, so a
				// reader is never told one thing and shown another.
				if !strings.HasPrefix(location.Description, place(lat, lon)) {
					t.Fatalf("%s describes itself as %q, which does not open with %q",
						location.Title, location.Description, place(lat, lon))
				}
				checked++
			}
		}
	}
	if checked != locations {
		t.Errorf("checked %d locations, the fixture summary says %d", checked, locations)
	}
}

// TestMarsMosaicsDeclareTheHalfHeightPyramid is the Trek zoom quirk: a mosaic is
// two tiles wide and one tall at its own zoom zero, so the square level z holds
// 2^z columns and 2^(z-1) rows. The fixture's per-level bounds for both real
// mosaics are exactly what LevelExtent computes, and what frameOf therefore
// declares to the deriver.
func TestMarsMosaicsDeclareTheHalfHeightPyramid(t *testing.T) {
	mars := readMars(t)
	_, _, tileSets := readSummary(t)
	if len(mars.Config.TileSets) != tileSets {
		t.Fatalf("%d tile sets, the fixture summary says %d", len(mars.Config.TileSets), tileSets)
	}
	for _, set := range mars.Config.TileSets {
		frame := frameOf(set.MaxZoom, set.Extension)
		if frame.MaxZoom != set.MaxZoom || frame.Format != set.Extension {
			t.Errorf("%s frames %d/%s, fixture %d/%s",
				set.Name, frame.MaxZoom, frame.Format, set.MaxZoom, set.Extension)
		}
		if len(frame.Windows) != len(set.Bounds) {
			t.Errorf("%s declares %d windows, fixture %d", set.Name, len(frame.Windows), len(set.Bounds))
		}
		for zoom := 0; zoom <= set.MaxZoom; zoom++ {
			bound, held := set.Bounds[strconv.Itoa(zoom)]
			if !held {
				t.Fatalf("%s records no bounds for zoom %d", set.Name, zoom)
			}
			maxX, maxY := LevelExtent(zoom)
			if bound.X.Min != 0 || bound.Y.Min != 0 || bound.X.Max != maxX || bound.Y.Max != maxY {
				t.Errorf("%s zoom %d spans x %d..%d y %d..%d, LevelExtent says 0..%d and 0..%d",
					set.Name, zoom, bound.X.Min, bound.X.Max, bound.Y.Min, bound.Y.Max, maxX, maxY)
			}
			window := frame.Windows[strconv.Itoa(zoom)]
			if window.MaxX != maxX || window.MaxY != maxY || window.MinX != 0 || window.MinY != 0 {
				t.Errorf("%s zoom %d window %+v disagrees with its own extent", set.Name, zoom, window)
			}
		}
	}
}

// TestMarsTileSetPathsAreCanonical holds the one spelling the crawler and the
// reader must share: a body's mosaic is captured under <body>/EQ/<layer>, and
// both real mosaics answer to it.
func TestMarsTileSetPathsAreCanonical(t *testing.T) {
	mars := readMars(t)
	for _, set := range mars.Config.TileSets {
		layer, found := strings.CutPrefix(set.Path, mars.Game.Slug+"/EQ/")
		if !found || layer == "" || strings.Contains(layer, "/") {
			t.Errorf("%s sits at %q, which is not <body>/EQ/<layer>", set.Name, set.Path)
			continue
		}
		if TileSetPath(mars.Game.Slug, layer) != set.Path {
			t.Errorf("TileSetPath spells %q, the capture was taken under %q",
				TileSetPath(mars.Game.Slug, layer), set.Path)
		}
	}
}

// TestMarsWorldSpeaksTheDeclaredFlattening: the world attributes are the whole
// coordinate design -- a sphere flattened into the top half of the square by
// the equirectangular projection -- and the fixture carries exactly the values
// this package's constants spell.
func TestMarsWorldSpeaksTheDeclaredFlattening(t *testing.T) {
	mars := readMars(t)
	want := map[string]string{
		semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
		semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
		semconv.KeyGeometryEquirectPx:  equirectPx,
		semconv.KeyGeometryEquirectDeg: equirectDeg,
		semconv.KeyGeometryBody:        mars.Game.Slug,
	}
	if len(mars.Attrs) != len(want) {
		t.Errorf("the fixture carries %d world attributes %v, this reader writes %d",
			len(mars.Attrs), mars.Attrs, len(want))
	}
	for key, value := range want {
		if mars.Attrs[key] != value {
			t.Errorf("world says %s=%q, fixture %q", key, value, mars.Attrs[key])
		}
	}
	// A reader opens on the middle of the picture: halfway across the square, a
	// quarter down it.
	center := doc.SyntheticPosition(doc.SyntheticWorldSize/2, doc.SyntheticWorldSize/4)
	if !near(mars.Lat, center.Lat) || mars.Lng != center.Lng {
		t.Errorf("the fixture opens at %v,%v, the picture's centre is %v,%v",
			mars.Lat, mars.Lng, center.Lat, center.Lng)
	}
}

// TestMarsIdentitiesDeriveFromStableNames: the Gazetteer numbers nothing a
// bundle can use, so every identity is minted from a stable name -- and the
// fixture's real numbers are exactly the ones those names mint, world and
// collections alike.
func TestMarsIdentitiesDeriveFromStableNames(t *testing.T) {
	mars := readMars(t)
	categories, _, _ := readSummary(t)
	scope := mars.Game.Slug + "/" + mars.Slug

	ids := doc.NewIDSpace()
	worldID, err := ids.Claim("trek:map:" + scope)
	if err != nil {
		t.Fatal(err)
	}
	if worldID != mars.ID {
		t.Errorf("the world's name mints %d, the fixture recorded %d", worldID, mars.ID)
	}

	counted := 0
	var previous string
	for _, group := range mars.Groups {
		for _, category := range group.Categories {
			counted++
			id, err := ids.Claim("trek:type:" + scope + ":" + category.Title)
			if err != nil {
				t.Fatal(err)
			}
			if id != category.ID {
				t.Errorf("%s mints %d, the fixture recorded %d", category.Title, id, category.ID)
			}
			// One legend rule and one artwork rule, held across the whole real
			// nomenclature: types read in sorted order, and a collection's
			// artwork key is its own title made a slug.
			if previous != "" && category.Title < previous {
				t.Errorf("%s reads before %s; feature types sort", previous, category.Title)
			}
			previous = category.Title
			if category.Icon != doc.Slugify(category.Title) {
				t.Errorf("%s lends artwork key %q, want %q",
					category.Title, category.Icon, doc.Slugify(category.Title))
			}
			if category.Attrs[semconv.KeyRenderAs] != semconv.RenderAsPin {
				t.Errorf("%s renders as %q; a gazetteer publishes markers", category.Title,
					category.Attrs[semconv.KeyRenderAs])
			}
			// Every real feature type earned a standard glyph, and the glyph is
			// one this reader's table can actually name.
			std := category.Attrs[semconv.KeyIconStd]
			if std == "" {
				t.Errorf("%s names no standard glyph and a gazetteer has no artwork of its own",
					category.Title)
				continue
			}
			known := false
			for _, candidate := range standardIcons {
				if candidate == std {
					known = true
					break
				}
			}
			if !known {
				t.Errorf("%s wears %q, which this reader's table never names", category.Title, std)
			}
		}
	}
	if counted != categories {
		t.Errorf("%d categories, the fixture summary says %d", counted, categories)
	}
}

// --- the reader itself, over the smallest capture that exercises it ----------

// wellFormed is a capture that is everything a good one is, so a test can spoil
// exactly one thing about it and see the reader refuse for exactly that reason.
func wellFormed() capture {
	return capture{
		Source:   source,
		Body:     "mars",
		Layer:    "viking",
		MapSlug:  "global",
		MapTitle: "Global",
		Map:      mosaic{MaxZoom: 2, Extension: "jpg", LayerTitle: "Viking"},
		Variants: []variant{{Layer: "mola", Title: "MOLA", MaxZoom: 1, Extension: "png"}},
		Features: []feature{
			{ID: 2, Name: "Olympus Mons", Type: "Mons, montes", Code: "MO",
				Latitude: 18.65, Longitude: 226.2, DiameterKM: 610.13,
				Origin: "The tallest mountain."},
			{ID: 1, Name: "Gale", Type: "Crater, craters", Code: "AA",
				Latitude: -5.37, Longitude: 137.81},
		},
	}
}

// writeArchive lays out the smallest archive holding one volume of one world,
// carrying the given capture.
func writeArchive(t *testing.T, kind string, raw capture) *archive.Archive {
	t.Helper()
	root := t.TempDir()
	body, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(root, "games", "g-1", "maps", "world-1")
	mustWrite(t, filepath.Join(root, "archive.json"),
		`{"games":[{"directory":"games/g-1","id":1,"title":"Mars","source":"nasa-trek"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Mars","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"global","title":"Global"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-01-01T00:00:00Z","contentHash":"h","kind":"`+kind+`","sourceId":1,"sourceUrl":"/x"}]`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "map", "h.json"), string(body))
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTranslateReadsACaptureWhole walks one hand-written capture into the
// interchange document: the body becomes the volume, the map's mosaic leads the
// lens order and its siblings follow, and the flat feature list arranges itself
// into sorted typed collections.
func TestTranslateReadsACaptureWhole(t *testing.T) {
	store := writeArchive(t, captureKind, wellFormed())
	document, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if document.Volume.Slug != "mars" || document.Volume.Title != "Mars" {
		t.Errorf("volume %s/%s; a planet is a volume named for the body", document.Volume.Slug, document.Volume.Title)
	}
	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("id space %q; the Gazetteer numbers nothing a bundle can use", document.Source.IDSpace)
	}
	if len(document.Icons) != 0 {
		t.Errorf("the document ships %d pieces of artwork; a gazetteer has none", len(document.Icons))
	}
	world := document.Worlds[0]
	if len(world.Lenses) != 2 {
		t.Fatalf("%d lenses, want the mosaic and its sibling", len(world.Lenses))
	}
	if world.Lenses[0].Name != "Viking" || world.Lenses[0].TileSet != TileSetPath("mars", "viking") {
		t.Errorf("the body's own picture leads, got %s/%s", world.Lenses[0].Name, world.Lenses[0].TileSet)
	}
	if world.Lenses[1].Name != "MOLA" || world.Lenses[1].TileSet != TileSetPath("mars", "mola") {
		t.Errorf("the sibling follows, got %s/%s", world.Lenses[1].Name, world.Lenses[1].TileSet)
	}
	if world.Lenses[1].Frame == nil || world.Lenses[1].Frame.MaxZoom != 1 {
		t.Errorf("a sibling's pyramid is its own: %+v", world.Lenses[1].Frame)
	}

	if len(world.Collections) != 2 {
		t.Fatalf("%d collections, want the two feature types", len(world.Collections))
	}
	craters, mons := world.Collections[0], world.Collections[1]
	if craters.Title != "Crater" || mons.Title != "Mons" {
		t.Errorf("collections read %q then %q; types sort and keep the singular half",
			craters.Title, mons.Title)
	}
	if craters.Attrs[semconv.KeyIconStd] != "maki/circle-stroked" ||
		mons.Attrs[semconv.KeyIconStd] != "maki/mountain" {
		t.Errorf("glyphs %q and %q; the IAU code names the artwork",
			craters.Attrs[semconv.KeyIconStd], mons.Attrs[semconv.KeyIconStd])
	}
	olympus := mons.Features[0]
	if olympus.Attrs[semconv.KeyGeoLat] != "18.65" || olympus.Attrs[semconv.KeyGeoLon] != "226.2" {
		t.Errorf("the Gazetteer's own coordinates travel verbatim, got %v", olympus.Attrs)
	}
	if olympus.Description != "18.65°N 226.2°E · 610.13 km across — The tallest mountain." {
		t.Errorf("the card reads %q", olympus.Description)
	}

	// The same archived bytes give the same document: identities included.
	again, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if again.Worlds[0].ID != world.ID ||
		again.Worlds[0].Collections[0].Features[0].ID != craters.Features[0].ID {
		t.Error("two translations of one capture minted different identities")
	}
}

// TestTranslateRefusesWhatItCannotStandFor states the reader's preconditions:
// another source's bytes, a mosaic captured twice, and a feature off the planet
// are refusals, not guesses.
func TestTranslateRefusesWhatItCannotStandFor(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		spoil func(*capture)
		says  string
	}{
		{"another source's bytes", "mapgenie-map", func(*capture) {}, "answers only for"},
		{"a mosaic captured twice", captureKind, func(c *capture) {
			c.Variants = append(c.Variants, variant{Layer: "viking", Title: "Again", MaxZoom: 1})
		}, "captured twice"},
		{"a sibling with no pyramid", captureKind, func(c *capture) {
			c.Variants[0].MaxZoom = 0
		}, "declares no pyramid"},
		{"a feature off the planet", captureKind, func(c *capture) {
			c.Features[0].Latitude = 91
		}, "sits at latitude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := wellFormed()
			tt.spoil(&raw)
			store := writeArchive(t, tt.kind, raw)
			_, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
			if err == nil || !strings.Contains(err.Error(), tt.says) {
				t.Fatalf("the capture was read anyway: %v", err)
			}
		})
	}
}
