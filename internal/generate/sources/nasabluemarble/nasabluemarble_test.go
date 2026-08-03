package nasabluemarble

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

func wellFormed() capture {
	return capture{
		Source:      SourceName,
		Body:        Body,
		Product:     "Blue Marble: Next Generation, July 2004",
		Credit:      "NASA Earth Observatory",
		AssetSHA256: strings.Repeat("d", 64),
		Width:       21600,
		Height:      10800,
		Map:         mosaic{MaxZoom: 5, Extension: "jpg", LayerTitle: "Blue Marble"},
		Derive:      derivation{Resampler: "catmull-rom-fixed15", JPEGQuality: 90},
		Features: features{
			Edition:       "stated",
			BordersSHA256: "bb", PlacesSHA256: "pp",
			Countries: []country{
				{Name: "Vale", A3: "VAL", Continent: "Europe", LabelLon: 10, LabelLat: 50,
					Polygons: [][][][2]float64{{{{5, 45}, {15, 45}, {15, 55}, {5, 55}, {5, 45}}}}},
				{Name: "Mar", A3: "MAR2", Continent: "Oceania", LabelLon: 150, LabelLat: -20,
					Polygons: [][][][2]float64{{{{145, -25}, {155, -25}, {155, -15}, {145, -15}, {145, -25}}}}},
			},
			Capitals: []capital{
				{Name: "Vale City", Country: "Vale", A3: "VAL", Lat: 50, Lon: 10},
				// A microstate no border draws at this scale: its continent is
				// the nearest drawn ground's.
				{Name: "Atoll", Country: "Atoll", A3: "ATL", Lat: -18, Lon: 152},
			},
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
		`{"games":[{"directory":"games/g-1","id":1,"title":"Earth","source":"nasa-blue-marble"}]}`)
	mustWrite(t, filepath.Join(root, "games", "g-1", "game.json"),
		`{"id":1,"title":"Earth","maps":[{"directory":"games/g-1/maps/world-1","id":1,"slug":"earth","title":"Earth"}]}`)
	mustWrite(t, filepath.Join(worldDir, "snapshots", "index.json"),
		`[{"capturedAt":"2026-08-03T14:30:39Z","contentHash":"h","kind":"`+kind+`","sourceId":1,"sourceUrl":"/x"}]`)
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
// interchange document: a raster-only Earth -- one world, one lens, the shared
// whole-sphere declarations, and nothing else.
func TestTranslateReadsACaptureWhole(t *testing.T) {
	store := writeArchive(t, CaptureKind, wellFormed())
	document, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if document.Volume.Slug != "earth" || document.Volume.Title != "Earth" {
		t.Errorf("volume %s/%s; the planet is the volume", document.Volume.Slug, document.Volume.Title)
	}
	if document.Source.Name != SourceName || document.Source.Label != "NASA Earth Observatory" {
		t.Errorf("provenance %s/%s; the ledger and the credit both read from here",
			document.Source.Name, document.Source.Label)
	}
	if document.Source.IDSpace != doc.IDSpaceDerived {
		t.Errorf("id space %q; a base map numbers nothing", document.Source.IDSpace)
	}
	if len(document.Icons) != 0 {
		t.Errorf("the document ships %d pieces of artwork; a base map has none", len(document.Icons))
	}
	if len(document.Worlds) != 1 {
		t.Fatalf("%d worlds, want the one Earth", len(document.Worlds))
	}
	world := document.Worlds[0]
	if world.Slug != "earth" || world.Title != "Earth" {
		t.Errorf("world %s/%s", world.Slug, world.Title)
	}
	if world.Center != doc.EquirectCenter() {
		t.Errorf("the world opens at %v, not the picture's centre", world.Center)
	}
	if world.Capture.Kind != CaptureKind || world.Capture.CapturedAt != "2026-08-03T14:30:39Z" {
		t.Errorf("capture provenance did not travel: %+v", world.Capture)
	}

	want := map[string]string{
		semconv.KeyGeometrySurface:     semconv.SurfaceSphere,
		semconv.KeyGeometryProjection:  semconv.ProjectionEquirect,
		semconv.KeyGeometryEquirectPx:  doc.EquirectPx,
		semconv.KeyGeometryEquirectDeg: doc.EquirectDeg,
		semconv.KeyGeometryBody:        "earth",
		semconv.KeyGeometryRadiusKM:    "6371.0088",
	}
	if len(world.Attrs) != len(want) {
		t.Errorf("the world carries %d attributes %v, want %d", len(world.Attrs), world.Attrs, len(want))
	}
	for key, value := range want {
		if world.Attrs[key] != value {
			t.Errorf("world says %s=%q, want %q", key, world.Attrs[key], value)
		}
	}

	if len(world.Lenses) != 1 {
		t.Fatalf("%d lenses, want the one base map", len(world.Lenses))
	}
	lens := world.Lenses[0]
	if lens.Name != "Blue Marble" || lens.TileSet != TileSet {
		t.Errorf("lens %s/%s", lens.Name, lens.TileSet)
	}
	if lens.Frame == nil || lens.Frame.MaxZoom != 5 || lens.Frame.Format != "jpg" {
		t.Fatalf("frame %+v", lens.Frame)
	}
	for zoom := 0; zoom <= 5; zoom++ {
		maxX, maxY := doc.EquirectLevelExtent(zoom)
		window, held := lens.Frame.Windows[strconv.Itoa(zoom)]
		if !held || window.MaxX != maxX || window.MaxY != maxY {
			t.Errorf("zoom %d window %+v, want the whole-sphere extent %d,%d", zoom, window, maxX, maxY)
		}
	}

	if len(world.Collections) != 3 {
		t.Fatalf("%d collections, want two continents of capitals and the countries", len(world.Collections))
	}
	europe, oceania, countries := world.Collections[0], world.Collections[1], world.Collections[2]
	if europe.Title != "Europe" || oceania.Title != "Oceania" {
		t.Errorf("capitals read %q then %q; continents sort", europe.Title, oceania.Title)
	}
	for _, held := range []doc.Collection{europe, oceania} {
		if held.Group != "Capitals" || held.Kind != doc.KindPoint ||
			held.Attrs[semconv.KeyRenderAs] != semconv.RenderAsPin ||
			held.Attrs[semconv.KeyIconStd] != capitalIcon {
			t.Errorf("capitals collection %q is arranged as %+v", held.Title, held.Attrs)
		}
	}
	if countries.Title != "Countries" || countries.Kind != doc.KindArea ||
		countries.Attrs[semconv.KeyLabelPolicy] != semconv.LabelQuiet {
		t.Errorf("the ground is arranged as %q %s %+v", countries.Title, countries.Kind, countries.Attrs)
	}
	if len(countries.Features) != 2 {
		t.Fatalf("%d countries", len(countries.Features))
	}

	vale := countries.Features[0]
	capitalOfVale := europe.Features[0]
	if capitalOfVale.Title != "Vale City" || capitalOfVale.Subtitle != "Vale" {
		t.Errorf("the capital reads %q / %q", capitalOfVale.Title, capitalOfVale.Subtitle)
	}
	if capitalOfVale.Member != vale.ID {
		t.Errorf("the capital stands on member %d, its country is %d", capitalOfVale.Member, vale.ID)
	}
	if capitalOfVale.Attrs[semconv.KeyGeoLat] != "50" || capitalOfVale.Attrs[semconv.KeyGeoLon] != "10" {
		t.Errorf("the published coordinates did not travel verbatim: %v", capitalOfVale.Attrs)
	}
	if at := worldPoint(10, 50); capitalOfVale.At == nil || *capitalOfVale.At != at {
		t.Errorf("the capital stands at %v, want %v", capitalOfVale.At, at)
	}

	atoll := oceania.Features[0]
	if atoll.Title != "Atoll" || atoll.Member != 0 {
		t.Errorf("a microstate capital should stand on open map: %+v", atoll)
	}

	if len(vale.Geometry) != 1 || vale.Geometry[0].Type != "MultiPolygon" {
		t.Fatalf("a country draws %+v", vale.Geometry)
	}
	var rings [][][][2]float64
	if err := json.Unmarshal(vale.Geometry[0].Coordinates, &rings); err != nil {
		t.Fatal(err)
	}
	corner := worldPoint(5, 45)
	if rings[0][0][0] != [2]float64{rounded(corner.Lng), rounded(corner.Lat)} {
		t.Errorf("the ground's first vertex is %v, want the converted corner %v", rings[0][0][0], corner)
	}
	if vale.Center == nil || *vale.Center != worldPoint(10, 50) {
		t.Errorf("the country frames at %v, want its label point", vale.Center)
	}

	// The same archived bytes give the same document: identities included.
	again, err := New().Translate(store, store.Volumes()[0], slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if again.Worlds[0].ID != world.ID {
		t.Error("two translations of one capture minted different identities")
	}
}

// TestTranslateRefusesWhatItCannotStandFor states the reader's preconditions:
// another source's bytes, another body, a capture with no pyramid, no digest or
// no size are refusals, not guesses.
func TestTranslateRefusesWhatItCannotStandFor(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		spoil func(*capture)
		says  string
	}{
		{"another source's kind", "trek-map", func(*capture) {}, "answers only for"},
		{"another source's name", CaptureKind, func(c *capture) { c.Source = "nasa-trek" }, "says its source is"},
		{"another body", CaptureKind, func(c *capture) { c.Body = "mars" }, "answers only for"},
		{"no pyramid", CaptureKind, func(c *capture) { c.Map.MaxZoom = 0 }, "declares no pyramid"},
		{"no features", CaptureKind, func(c *capture) { c.Features = features{} }, "carries no features"},
		{"a country with no identity", CaptureKind, func(c *capture) {
			c.Features.Countries[0].Continent = ""
		}, "carries no identity"},
		{"a capital off the planet", CaptureKind, func(c *capture) {
			c.Features.Capitals[0].Lat = 91
		}, "sits at"},
		{"no digest", CaptureKind, func(c *capture) { c.AssetSHA256 = "" }, "names no source digest"},
		{"no size", CaptureKind, func(c *capture) { c.Width = 0 }, "names no source image size"},
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
