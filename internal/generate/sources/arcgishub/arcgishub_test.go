package arcgishub

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// The capture these tests read is testdata/city.json: one invented crawl day of
// the proof city, as small as a day can be while still exercising what this
// reader owns -- the Mercator window, the curated fold of rows into zones, a
// multipart polygon, a named trail, titled pins, the drawing the basemap is
// rasterized from, and the membership join against a captured subwatershed. It
// is the whole input, so what the assertions below say is exactly what the
// translation of it is.

// stageCity lays the testdata capture out as the smallest archive holding one
// city of one day, optionally spoiled first.
func stageCity(t *testing.T, spoil func(map[string]any)) *archive.Archive {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "city.json"))
	if err != nil {
		t.Fatal(err)
	}
	if spoil != nil {
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatal(err)
		}
		spoil(raw)
		if body, err = json.Marshal(raw); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	worldDir := filepath.Join(root, "games", "g-1", "maps", "world-1")
	mustWrite(t, filepath.Join(root, "archive.json"),
		`{"games":[{"directory":"games/g-1","id":1,"title":"Bend, Oregon","source":"arcgis-hub"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Bend, Oregon","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"2026-03-01","title":"2026-03-01"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-03-01T12:00:00Z","contentHash":"h","kind":"arcgis-map","sourceId":1,"sourceUrl":"/x"}]`)
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

func translated(t *testing.T) doc.Document {
	t.Helper()
	store := stageCity(t, nil)
	document, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// claim mints the number one stable name derives to, in a space of its own, so
// a test can say which identity a thing must carry without re-walking the
// reader's own claim order.
func claim(t *testing.T, name string) int64 {
	t.Helper()
	id, err := doc.NewIDSpace().Claim(name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// cityWindowOf is the capture's own window, spelled as this package's type so
// the expectations below run through the very projection under test.
func cityWindowOf() window {
	return window{West: -121.6, North: 44.12, East: -121.1, South: 44.04}
}

// TestCityTranslatesExactly walks the invented crawl day into the document:
// the day becomes the world, the declared Mercator window becomes the world's
// attributes and every feature's projection, pins come before ground, and the
// curated buckets fold exactly as the table says.
func TestCityTranslatesExactly(t *testing.T) {
	document := translated(t)

	if document.Volume.Slug != "bend-or" || document.Volume.Title != "Bend, Oregon" {
		t.Errorf("volume %s/%s", document.Volume.Slug, document.Volume.Title)
	}
	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("a hub numbers rows with churning object ids, so the id space must be %q, not %q",
			doc.IDSpaceDerived, document.Source.IDSpace)
	}
	if len(document.Icons) != 0 {
		t.Errorf("the document ships %d pieces of artwork; a city's hub publishes none", len(document.Icons))
	}
	world := document.Worlds[0]

	// The day is the world: slug and title both, because a picker listing dates
	// is the version history reading as itself.
	if world.Slug != "2026-03-01" || world.Title != "2026-03-01" {
		t.Errorf("world %s/%s", world.Slug, world.Title)
	}
	if world.ID != claim(t, "arcgis:map:bend-or/2026-03-01") {
		t.Errorf("world id %d is not the number its stable name mints", world.ID)
	}

	// The declared window is the whole coordinate design: a plane cut from
	// Earth, spelled exactly as the capture declared it.
	px, deg := cityWindowOf().pxDeg()
	want := map[string]string{
		semconv.KeyGeometrySurface:     semconv.SurfacePlane,
		semconv.KeyGeometryBody:        "earth",
		semconv.KeyGeometryMercatorPx:  px,
		semconv.KeyGeometryMercatorDeg: deg,
	}
	for key, value := range want {
		if world.Attrs[key] != value {
			t.Errorf("world says %s=%q, want %q", key, world.Attrs[key], value)
		}
	}
	if len(world.Attrs) != len(want) {
		t.Errorf("world carries %d attributes %v, want %d", len(world.Attrs), world.Attrs, len(want))
	}

	// The one picture is drawn, not fetched: the lens carries the rendered
	// pyramid's frame and the drawing its deepest level is rasterized from --
	// the boundary's ring and both trail rows, proposed or not, because a
	// drawing is what the ground looks like rather than what a legend lists.
	if len(world.Lenses) != 1 {
		t.Fatalf("%d lenses", len(world.Lenses))
	}
	lens := world.Lenses[0]
	if lens.Name != "Basemap" || lens.TileSet != TileSetPath("bend-or", "2026-03-01") {
		t.Errorf("lens %s/%s", lens.Name, lens.TileSet)
	}
	if lens.Frame == nil || lens.Frame.MaxZoom != 1 || lens.Frame.Format != "png" {
		t.Fatalf("frame %+v", lens.Frame)
	}
	if window := lens.Frame.Windows["1"]; window != (doc.TileWindow{MaxX: 1, MaxY: 1}) {
		t.Errorf("a rendered level is complete by construction, got window %+v", window)
	}
	if lens.Drawing == nil || lens.Drawing.Zoom != 1 {
		t.Fatalf("the basemap declares drawing %+v; a hub serves geometry and no tiles", lens.Drawing)
	}
	var roles []string
	for _, shape := range lens.Drawing.Shapes {
		roles = append(roles, shape.Role)
	}
	if strings.Join(roles, ",") != "boundary,trail,trail" {
		t.Errorf("the drawing paints %v; curation names the boundary and both trail rows", roles)
	}

	// Pins first, then ground, in the curation table's own order.
	if len(world.Collections) != 5 {
		t.Fatalf("%d collections, want pins then four grounds", len(world.Collections))
	}
	sites := world.Collections[0]
	if sites.Title != "Historic Resources" || sites.Group != "Heritage" || sites.Kind != doc.KindPoint {
		t.Errorf("the pin collection is %s/%s/%s", sites.Title, sites.Group, sites.Kind)
	}
	if sites.Attrs[semconv.KeyIconStd] != "maki/monument" {
		t.Errorf("the glyph is named for composition to resolve, got %q", sites.Attrs[semconv.KeyIconStd])
	}
	// Three rows were captured; the untitled one is the publisher's hygiene and
	// is left out rather than shipped blank.
	if len(sites.Features) != 2 {
		t.Fatalf("%d pins from three rows, one of them untitled", len(sites.Features))
	}
	mill := sites.Features[0]
	if mill.Title != "Old Mill" || mill.Description != "Mill District" {
		t.Errorf("the first pin is %q described %q", mill.Title, mill.Description)
	}
	if mill.ID != claim(t, "arcgis:loc:bend-or/historic-sites/5") {
		t.Errorf("pin id %d is not the number its stable name mints", mill.ID)
	}
	// The pin stands where the window puts it, and the city's own coordinates
	// travel verbatim beside the synthetic position.
	x, y := cityWindowOf().worldPixel(-121.31, 44.05)
	if at := doc.SyntheticPosition(x, y); mill.At == nil || *mill.At != at {
		t.Errorf("the pin stands at %+v, the window puts -121.31,44.05 at %+v", mill.At, at)
	}
	if mill.Attrs[semconv.KeyGeoLat] != "44.05" || mill.Attrs[semconv.KeyGeoLon] != "-121.31" {
		t.Errorf("the published coordinates travel as %v", mill.Attrs)
	}

	boundary, zoning, trails, subs := world.Collections[1], world.Collections[2], world.Collections[3], world.Collections[4]
	if boundary.Title != "MPO Boundary" || zoning.Title != "Zoning" ||
		trails.Title != "Paths & Trails" || subs.Title != "Subwatersheds" {
		t.Fatalf("ground reads %s, %s, %s, %s; the table's order is the legend order",
			boundary.Title, zoning.Title, trails.Title, subs.Title)
	}

	// Zoning: rows fold into buckets by code, buckets emit in sorted key order,
	// and a bucket holds one geometry part per row that drew -- the RS zone is
	// the multipart one, two parts, the first of them two polygons.
	if zoning.Key != "zoning" || zoning.Kind != doc.KindArea {
		t.Errorf("zoning is %s/%s", zoning.Key, zoning.Kind)
	}
	if len(zoning.Features) != 2 || zoning.Features[0].Title != "CB" || zoning.Features[1].Title != "RS" {
		t.Fatalf("zoning holds %+v; buckets emit in sorted key order", zoning.Features)
	}
	rs := zoning.Features[1]
	if rs.ID != claim(t, "arcgis:zone:bend-or/zoning/rs") {
		t.Errorf("zone id %d is not the number its stable name mints", rs.ID)
	}
	if len(rs.Geometry) != 2 || rs.Geometry[0].Type != "MultiPolygon" {
		t.Fatalf("the RS zone draws %d parts of %s; two rows drew, one of them multipart",
			len(rs.Geometry), rs.Geometry[0].Type)
	}
	var multipart [][][][]float64
	if err := json.Unmarshal(rs.Geometry[0].Coordinates, &multipart); err != nil {
		t.Fatal(err)
	}
	if len(multipart) != 2 {
		t.Errorf("the multipart row carries %d polygons, the capture drew two", len(multipart))
	}
	// One position, followed all the way through: true degrees to world pixel
	// to the pair a shape's coordinates speak.
	if corner := cityWindowOf().syntheticRing(-121.45, 44.10); multipart[0][0][0][0] != corner[0] ||
		multipart[0][0][0][1] != corner[1] {
		t.Errorf("the first corner landed at %v, the window puts it at %v", multipart[0][0][0], corner)
	}

	// Trails: only the named, existing trail earns a zone, clickable end to end
	// as one multipart line, at the stroke width the curation declares.
	if trails.Kind != doc.KindPath || trails.Attrs[semconv.KeyStrokeWidthPx] != "12" {
		t.Errorf("trails are %s at width %q", trails.Kind, trails.Attrs[semconv.KeyStrokeWidthPx])
	}
	if len(trails.Features) != 1 || trails.Features[0].Title != "River Trail" ||
		trails.Features[0].Subtitle != "Riverside" {
		t.Fatalf("trails hold %+v; the proposed row makes no zone", trails.Features)
	}
	if len(trails.Features[0].Geometry) != 1 || trails.Features[0].Geometry[0].Type != "MultiLineString" {
		t.Errorf("the trail draws %+v", trails.Features[0].Geometry)
	}

	// The national layer labels quiet and buckets by the HUC code, and it makes
	// no claim about itself.
	if subs.Attrs[semconv.KeyLabelPolicy] != semconv.LabelQuiet {
		t.Errorf("subwatersheds label %q; the hydrology is context, not headline",
			subs.Attrs[semconv.KeyLabelPolicy])
	}
	unit := subs.Features[0]
	if unit.Title != "Whychus Creek" || unit.Subtitle != "Subwatershed · HUC 170703010801" {
		t.Errorf("the unit reads %q/%q", unit.Title, unit.Subtitle)
	}
	if unit.Attrs[semconv.KeyHydroHUC12] != "" {
		t.Error("a subwatershed does not need a sentence telling it which subwatershed it lies in")
	}

	// The membership join: every zone whose sampled ground lies wholly in the
	// one captured subwatershed claims it -- the code as the machine-readable
	// key, and one earned sentence for the card.
	for _, zone := range [][2]string{{zoning.Features[0].Title, zoning.Features[0].Attrs[semconv.KeyHydroHUC12]},
		{rs.Title, rs.Attrs[semconv.KeyHydroHUC12]}} {
		if zone[1] != "170703010801" {
			t.Errorf("zone %s claims %q, its ground lies in the captured subwatershed", zone[0], zone[1])
		}
	}
	if rs.Description != "Lies in the Whychus Creek subwatershed (HUC 170703010801)." {
		t.Errorf("the earned sentence reads %q", rs.Description)
	}

	// The same archived bytes give the same document, identities included.
	again := translated(t)
	if again.Worlds[0].ID != world.ID || again.Worlds[0].Collections[2].Features[1].ID != rs.ID {
		t.Error("two translations of one capture minted different identities")
	}
}

// TestTranslateRefusesWhatItCannotStandFor states the reader's gates: an
// uncurated city is passed over as not ready -- an operator's own city may sit
// in the archive and the public table may not name it -- while a wrong capture
// is refused outright.
func TestTranslateRefusesWhatItCannotStandFor(t *testing.T) {
	t.Run("an uncurated city is not ready, not broken", func(t *testing.T) {
		store := stageCity(t, func(raw map[string]any) { raw["city"] = "somewhere-else" })
		_, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
		if !errors.Is(err, archive.ErrNotReady) {
			t.Fatalf("an uncurated city answered %v, want a not-ready pass-over", err)
		}
	})
	t.Run("a world slug that is not a capture day", func(t *testing.T) {
		store := stageCity(t, func(raw map[string]any) { raw["mapSlug"] = "downtown" })
		_, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
		if err == nil || !strings.Contains(err.Error(), "not a capture day") {
			t.Fatalf("a dateless world was read: %v", err)
		}
	})
	t.Run("a dataset the table does not curate", func(t *testing.T) {
		store := stageCity(t, func(raw map[string]any) {
			datasets := raw["datasets"].([]any)
			raw["datasets"] = append(datasets, map[string]any{"slug": "water-accounts", "features": []any{}})
		})
		_, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
		if err == nil || !strings.Contains(err.Error(), "not curated") {
			t.Fatalf("an uncurated dataset was read: %v", err)
		}
	})
	t.Run("a point dataset with no titled row", func(t *testing.T) {
		store := stageCity(t, func(raw map[string]any) {
			for _, entry := range raw["datasets"].([]any) {
				dataset := entry.(map[string]any)
				if dataset["slug"] != "historic-sites" {
					continue
				}
				for _, held := range dataset["features"].([]any) {
					row := held.(map[string]any)
					row["fields"] = map[string]any{}
				}
			}
		})
		_, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
		if err == nil || !strings.Contains(err.Error(), "no row carries a title") {
			t.Fatalf("a nameless dataset was shipped blank: %v", err)
		}
	})
}
