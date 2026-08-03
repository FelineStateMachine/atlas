package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/compose"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles/basemap"
)

// The whole pipeline, hermetic.
//
// This file builds a synthetic capture archive in a temporary directory --
// two readings of one invented game and one crawl day of the proof city --
// and runs the shipped stages over it exactly as a person runs them: `atlas
// tiles` derives the pyramids, `atlas compose` builds the single-source city,
// and `atlas enrich` folds the game's two readings together and builds the
// merged volume. Then it runs all of that a second time, into fresh
// directories, and holds the two results byte for byte.
//
// That equality is the determinism law the registry stands on, asserted with
// no carve-out: nothing in the lane reads a clock, a build's creation time is
// its newest capture time, and the same archive therefore writes the same
// bytes under the same names on any run. The corpus is small but it is not
// thin -- a two-level raster pyramid per reading, a drawn basemap held to its
// witness hashes, a multipart polygon, a multipart path, titled pins, a
// sixteen-anchor alignment, three donor-only features, and a standard glyph
// resolved from the vendored library all travel through it.

// The capture times. They are the only times anywhere in the corpus, and the
// stamp test at the bottom holds each manifest to them, which is the direct
// form of "no clock reaches a build".
const (
	valeServingCapturedAt = "2026-01-02T03:04:05Z"
	valeDonorCapturedAt   = "2026-01-01T03:04:05Z"
	cityCapturedAt        = "2026-03-05T06:07:08Z"
	cityDay               = "2026-03-05"
)

// valeAnchors are the sixteen places both readings of the vale name, laid out
// as a grid so the alignment they determine is never degenerate. Sixteen
// clears align.MinAnchors with room to spare.
var valeAnchors = []string{
	"Ash Gate", "Bell Tower", "Cinder Row", "Dry Well",
	"Elder Oak", "Fox Market", "Glass Bridge", "Hearth Hall",
	"Iron Steps", "Juniper Yard", "Kings Rest", "Lantern Pier",
	"Moss Court", "North Mill", "Old Forge", "Pale Arch",
}

// donorOnly are the three places only the donor reading knows, placed far from
// every anchor so the merge can only read them as new.
var donorOnly = []struct {
	name string
	x, y float64
}{
	{"Hidden Grove", 512, 512},
	{"Sunken Pier", 7680, 640},
	{"Quiet Steps", 640, 7680},
}

func anchorPixel(index int) (x, y float64) {
	return 2048 + 1024*float64(index%4), 2048 + 1024*float64(index/4)
}

// --- the corpus ---------------------------------------------------------------

// buildPipelineCorpus lays out the synthetic archive: the vale captured by
// MapGenie (serving, newer) and by IGN (donor, older), and the city captured
// by its hub, each with the captured rasters its deriver folds down.
func buildPipelineCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeJSONFile(t, filepath.Join(root, "archive.json"), map[string]any{
		"games": []any{
			map[string]any{"directory": "games/vale-mg", "id": 1, "title": "Vale"},
			map[string]any{"directory": "games/vale-ign", "id": 2, "title": "Vale", "source": "ign"},
			map[string]any{"directory": "games/bend", "id": 3, "title": "Bend, Oregon", "source": "arcgis-hub"},
		},
	})
	writeValeServing(t, root)
	writeValeDonor(t, root)
	writeCity(t, root)
	return root
}

// scaffoldWorld writes one volume directory holding one world and one capture.
func scaffoldWorld(t *testing.T, root, volumeDir, title, slug, kind, capturedAt string, capture any) string {
	t.Helper()
	writeJSONFile(t, filepath.Join(root, volumeDir, "game.json"), map[string]any{
		"id": 1, "title": title,
		"maps": []any{map[string]any{
			"directory": volumeDir + "/maps/w-1", "id": 1, "slug": slug, "title": slug,
		}},
	})
	worldDir := filepath.Join(root, volumeDir, "maps", "w-1")
	writeJSONFile(t, filepath.Join(worldDir, "snapshots", "index.json"), []any{map[string]any{
		"capturedAt": capturedAt, "contentHash": "cap", "kind": kind, "sourceId": 1, "sourceUrl": "/x",
	}})
	writeJSONFile(t, filepath.Join(worldDir, "snapshots", "map", "cap.json"), capture)
	return worldDir
}

// paintTile writes one 256-pixel PNG: a flat ground crossed by a band whose
// row follows the seed, so every tile in the corpus is its own picture.
func paintTile(t *testing.T, path string, ground color.NRGBA, seed int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	band := (seed * 37) % 224
	for y := range 256 {
		for x := range 256 {
			at := ground
			if y >= band && y < band+16 {
				at = color.NRGBA{R: 255 - ground.R, G: 255 - ground.G, B: 255 - ground.B, A: 255}
			}
			img.SetNRGBA(x, y, at)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return hashFileAt(t, path)
}

// writeCapturedLevel writes the four tiles of one complete zoom-one level and
// the tile index that records them, under the URL shape whose marker the
// archive recovers the tile set from.
func writeCapturedLevel(t *testing.T, worldDir, urlPrefix string, ground color.NRGBA, seed int) {
	t.Helper()
	var records []any
	for y := range 2 {
		for x := range 2 {
			path := filepath.Join(worldDir, "tiles", "set-1", "1", strconv.Itoa(x), strconv.Itoa(y)+".png")
			sum := paintTile(t, path, ground, seed+3*x+5*y)
			records = append(records, map[string]any{
				"contentHash": sum, "status": "cached", "tileSetId": 1,
				"url": urlPrefix + "/1/" + strconv.Itoa(x) + "/" + strconv.Itoa(y) + ".png",
				"x":   x, "y": y, "zoom": 1,
			})
		}
	}
	writeJSONFile(t, filepath.Join(worldDir, "tiles", "index.json"), records)
}

// writeValeServing is the MapGenie reading: native identities, the sixteen
// anchors, and one region drawing a multipart area.
func writeValeServing(t *testing.T, root string) {
	t.Helper()
	locations := make([]any, 0, len(valeAnchors))
	for index, name := range valeAnchors {
		x, y := anchorPixel(index)
		at := doc.SyntheticPosition(x, y)
		location := map[string]any{"id": 200 + index, "title": name, "latitude": at.Lat, "longitude": at.Lng}
		if index == 0 {
			location["region_id"] = 900
		}
		locations = append(locations, location)
	}
	center := doc.SyntheticPosition(2700, 2900)
	capture := map[string]any{
		"id": 10, "slug": "world", "title": "World",
		"initial_latitude": center.Lat, "initial_longitude": center.Lng,
		"config": map[string]any{"tile_sets": []any{map[string]any{
			"name": "Default", "path": "vale/world/v1",
			"min_zoom": 0, "max_zoom": 1, "extension": "png",
			"bounds": map[string]any{
				"0": tileBound(0, 0), "1": tileBound(1, 1),
			},
		}}},
		"game": map[string]any{"id": 1, "slug": "vale", "title": "Vale"},
		"groups": []any{map[string]any{
			"id": 1, "title": "Places", "color": "6984F2",
			"categories": []any{map[string]any{
				"id": 100, "title": "Landmarks", "icon": "landmark", "visible": true,
				"locations": locations,
			}},
		}},
		"regions": []any{map[string]any{
			"id": 900, "title": "Old Town",
			"center_x": center.Lng, "center_y": center.Lat,
			"features": []any{
				map[string]any{"geometry": map[string]any{"type": "MultiPolygon", "coordinates": []any{
					[]any{syntheticRing(2400, 2400, 2800, 2800)},
					[]any{syntheticRing(3000, 2400, 3300, 2700)},
				}}},
				map[string]any{"geometry": map[string]any{"type": "Polygon", "coordinates": []any{
					syntheticRing(2400, 3000, 2900, 3400),
				}}},
			},
		}},
	}
	worldDir := scaffoldWorld(t, root, "games/vale-mg", "Vale", "world", "map", valeServingCapturedAt, capture)
	writeCapturedLevel(t, worldDir, "https://tiles.example.invalid/games/vale/world/v1",
		color.NRGBA{R: 0x2a, G: 0x4d, B: 0x69, A: 0xff}, 1)
}

// writeValeDonor is the IGN reading: the same sixteen places at the same
// pixels, three places of its own, and a raster of its own for the warp to
// resample.
func writeValeDonor(t *testing.T, root string) {
	t.Helper()
	markers := make([]any, 0, len(valeAnchors)+len(donorOnly))
	for index, name := range valeAnchors {
		x, y := anchorPixel(index)
		markers = append(markers, map[string]any{
			"id": "va-" + strconv.Itoa(10+index), "lat": -y / doc.SyntheticWorldSize, "lng": x / doc.SyntheticWorldSize,
			"markerName": name, "markerSlug": doc.Slugify(name), "typeSlug": "landmark",
		})
	}
	for index, extra := range donorOnly {
		markers = append(markers, map[string]any{
			"id": "vx-" + strconv.Itoa(index), "lat": -extra.y / doc.SyntheticWorldSize, "lng": extra.x / doc.SyntheticWorldSize,
			"markerName": extra.name, "markerSlug": doc.Slugify(extra.name), "typeSlug": "extra",
		})
	}
	capture := map[string]any{
		"source": "ign-wikimaps", "objectSlug": "vale", "mapSlug": "world",
		"gameTitle": "Vale", "mapTitle": "World",
		"map": map[string]any{
			"width": 1, "height": 1, "minZoom": 0, "maxZoom": 1,
			"initialLat": -0.5, "initialLng": 0.5,
			"tileset": "https://cdn.example.invalid/wikimaps/vale/world/{z}/{x}/{y}.png",
		},
		"types": []any{
			map[string]any{"typeSlug": "landmark", "typeName": "Landmarks", "parentTypeSlug": "places"},
			map[string]any{"typeSlug": "extra", "typeName": "Extras", "parentTypeSlug": "places"},
		},
		"markers": markers,
	}
	worldDir := scaffoldWorld(t, root, "games/vale-ign", "Vale", "world", "ign-map", valeDonorCapturedAt, capture)
	writeCapturedLevel(t, worldDir, "https://cdn.example.invalid/wikimaps/vale/world",
		color.NRGBA{R: 0x69, G: 0x33, B: 0x22, A: 0xff}, 2)
}

// writeCity is one crawl day of the proof city: a multipart zoning polygon, a
// named trail in two lines, two titled pins, and a basemap drawn rather than
// fetched. The drawn level's captured tiles are rendered here with the same
// renderer the deriver uses, which is exactly how the real archive came to
// hold them -- the crawl that first drew them wrote them down, and every later
// derivation is held to those hashes as its witness.
func writeCity(t *testing.T, root string) {
	t.Helper()
	capture := map[string]any{
		"source": "arcgis-hub", "city": "bend-or", "title": "Bend, Oregon", "mapSlug": cityDay,
		"window":  map[string]any{"west": -121.6, "north": 44.12, "east": -121.1, "south": 44.04},
		"basemap": map[string]any{"maxZoom": 1, "extension": "png"},
		"datasets": []any{
			map[string]any{"slug": "zoning", "features": []any{
				map[string]any{"id": 1, "fields": map[string]any{"ZONE": "RS"},
					"geometry": map[string]any{"type": "rings", "rings": []any{
						[]any{degreeRing(-121.45, 44.07, -121.40, 44.10)},
						[]any{degreeRing(-121.38, 44.08, -121.35, 44.10)},
					}}},
				map[string]any{"id": 2, "fields": map[string]any{"ZONE": "CB"},
					"geometry": map[string]any{"type": "rings", "rings": []any{
						[]any{degreeRing(-121.28, 44.06, -121.25, 44.08)},
					}}},
			}},
			map[string]any{"slug": "trails", "features": []any{
				map[string]any{"id": 11,
					"fields": map[string]any{"Status": "Existing", "Trail_Name": "River Trail", "Park": "Riverside"},
					"geometry": map[string]any{"type": "lines", "lines": []any{
						[]any{lonLat(-121.50, 44.060), lonLat(-121.40, 44.065), lonLat(-121.30, 44.070)},
						[]any{lonLat(-121.30, 44.070), lonLat(-121.20, 44.075)},
					}}},
			}},
			map[string]any{"slug": "historic-sites", "features": []any{
				map[string]any{"id": 5, "fields": map[string]any{"NAME": "Old Mill", "TAB_NAME": "Mill District"},
					"geometry": map[string]any{"type": "point", "point": lonLat(-121.31, 44.05)}},
				map[string]any{"id": 6, "fields": map[string]any{"NAME": "Pilot Butte"},
					"geometry": map[string]any{"type": "point", "point": lonLat(-121.30, 44.06)}},
			}},
		},
	}
	worldDir := scaffoldWorld(t, root, "games/bend", "Bend, Oregon", cityDay, "arcgis-map", cityCapturedAt, capture)

	// The witness tiles: translate the capture just written, take the drawing
	// its lens carries, and render the deepest level the way the deriver will.
	store, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var city archive.VolumeRef
	for _, volume := range store.Volumes() {
		if volume.Source == "arcgis-hub" {
			city = volume
		}
	}
	source, err := sources.For("arcgis-hub")
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Translate(store, city, quiet())
	if err != nil {
		t.Fatal(err)
	}
	drawing := document.Worlds[0].Lenses[0].Drawing
	if drawing == nil || drawing.Zoom != 1 {
		t.Fatalf("the city's lens carries drawing %+v; the corpus depends on a drawn basemap", drawing)
	}
	shapes := make([]basemap.Feature, 0, len(drawing.Shapes))
	for _, shape := range drawing.Shapes {
		shapes = append(shapes, basemap.Feature{
			Role:     basemap.Role(shape.Role),
			Rings:    shape.Rings,
			Lines:    shape.Lines,
			Emphasis: shape.Emphasis,
		})
	}
	renderer := basemap.NewRenderer(shapes, drawing.Zoom)
	var records []any
	for y := range 2 {
		for x := range 2 {
			body, err := basemap.EncodePNG(renderer.Tile(x, y))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(worldDir, "tiles", "set-1", "1", strconv.Itoa(x), strconv.Itoa(y)+".png")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			records = append(records, map[string]any{
				"contentHash": hex.EncodeToString(sum[:]), "status": "cached", "tileSetId": 1,
				"url": "https://hub.example.invalid/tiles/bend-or/" + cityDay + "/basemap/1/" + strconv.Itoa(x) + "/" + strconv.Itoa(y) + ".png",
				"x":   x, "y": y, "zoom": 1,
			})
		}
	}
	writeJSONFile(t, filepath.Join(worldDir, "tiles", "index.json"), records)
}

// --- the run --------------------------------------------------------------

// pipelineRun is where one full run of the stages put its artefacts.
type pipelineRun struct {
	tiles    string
	registry string
}

// runWholePipeline drives the shipped stages over the archive: derive, compose
// the single-source city, enrich the twice-read game.
func runWholePipeline(t *testing.T, root string, into string) pipelineRun {
	t.Helper()
	run := pipelineRun{
		tiles:    filepath.Join(into, "tiles"),
		registry: filepath.Join(into, "bundles"),
	}
	if err := runTiles([]string{"-archive", root, "-output", run.tiles, "-log-level", "warn"}); err != nil {
		t.Fatalf("atlas tiles: %v", err)
	}
	index := filepath.Join(run.tiles, "index.json")
	if err := runCompose([]string{"-archive", root, "-tiles", index,
		"-bundles", run.registry, "-log-level", "warn", "bend-or"}); err != nil {
		t.Fatalf("atlas compose: %v", err)
	}
	if err := runEnrich([]string{"-archive", root, "-tiles", index,
		"-bundles", run.registry, "-log-level", "warn", "vale"}); err != nil {
		t.Fatalf("atlas enrich: %v", err)
	}
	return run
}

// TestPipelineWritesTheSameBytesTwice is the determinism gate: the same
// archive, taken through the real stages twice into two fresh directories,
// derives the same tiles and installs the same bundles, byte for byte and
// name for name -- stamps, file names, registry index and all.
func TestPipelineWritesTheSameBytesTwice(t *testing.T) {
	root := buildPipelineCorpus(t)
	first := runWholePipeline(t, root, t.TempDir())
	second := runWholePipeline(t, root, t.TempDir())

	compareTrees(t, "tile set", hashTree(t, first.tiles), hashTree(t, second.tiles))
	compareTrees(t, "registry", hashTree(t, first.registry), hashTree(t, second.registry))

	// And running the stages again over what the first run left is a no-op in
	// substance: every pyramid carries, every build is already installed under
	// the name its stamp gives it, and the trees do not move.
	before := hashTree(t, first.registry)
	beforeTiles := hashTree(t, first.tiles)
	again := runWholePipeline(t, root, filepath.Dir(first.tiles))
	if again.tiles != first.tiles || again.registry != first.registry {
		t.Fatalf("the rerun landed elsewhere: %+v", again)
	}
	compareTrees(t, "registry after a rerun", before, hashTree(t, first.registry))
	compareTrees(t, "tile set after a rerun", beforeTiles, hashTree(t, first.tiles))

	verifyBundles(t, first.registry)
}

// TestADrawnTileIsHeldToItsWitness ports the drawn path's guard: the deriver
// refuses a tile whose bytes disagree with the capture that recorded them,
// rather than quietly shipping a picture nothing vouches for. Disagreement is
// arranged the smallest way there is -- one recorded hash is turned over --
// and the whole tiles stage must fail naming the witness.
func TestADrawnTileIsHeldToItsWitness(t *testing.T) {
	root := buildPipelineCorpus(t)
	indexPath := filepath.Join(root, "games", "bend", "maps", "w-1", "tiles", "index.json")
	var records []map[string]any
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	records[0]["contentHash"] = strings.Repeat("ab", 32)
	writeJSONFile(t, indexPath, records)

	err = runTiles([]string{"-archive", root, "-output", filepath.Join(t.TempDir(), "tiles"),
		"-log-level", "error"})
	if err == nil {
		t.Fatal("a drawing that disagrees with its capture derived without complaint")
	}
	if !strings.Contains(err.Error(), "witnessed") {
		t.Errorf("the refusal is %q, which does not say the capture disagreed", err)
	}
}

// verifyBundles opens what the pipeline installed and holds each build to what
// the corpus says it must be.
func verifyBundles(t *testing.T, registry string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(registry, "*.atlas"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("the registry holds %d bundles, want the city and the merged game", len(paths))
	}
	byVolume := map[string]*bundle.Reader{}
	for _, path := range paths {
		reader, err := bundle.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", filepath.Base(path), err)
		}
		t.Cleanup(func() { reader.Close() })
		if err := reader.Validate(); err != nil {
			t.Fatalf("validate %s: %v", filepath.Base(path), err)
		}
		byVolume[reader.Manifest.Volume.Slug] = reader
	}

	t.Run("the merged game", func(t *testing.T) {
		reader := byVolume["vale"]
		if reader == nil {
			t.Fatal("no build of the vale was installed")
		}
		// The version is the capture, not the build clock, and an enriched
		// build outranks the plain build of the same capture by revision.
		if got := reader.Manifest.Version.CreatedAt; got != valeServingCapturedAt {
			t.Errorf("createdAt %q, want the serving capture time %q", got, valeServingCapturedAt)
		}
		enriched, err := enrich.BuildRevision(compose.PolicyRevision)
		if err != nil {
			t.Fatal(err)
		}
		if reader.Manifest.Version.Revision != enriched {
			t.Errorf("revision %d, want the enriched %d", reader.Manifest.Version.Revision, enriched)
		}

		// The ledger records the merge: the origin account and the donor's,
		// every anchor corroborated at no distance, and the donor's three own
		// places added.
		var payload struct {
			Merged      []enrich.Account `json:"merged"`
			Collections []struct {
				Title string `json:"title"`
				Group string `json:"group"`
				Kind  string `json:"kind"`
			} `json:"collections"`
		}
		readBundleJSON(t, reader, bundle.WorldEntryName("world", bundle.WorldSuffix), &payload)
		if len(payload.Merged) != 2 {
			t.Fatalf("the ledger carries %d accounts, want origin and donor", len(payload.Merged))
		}
		origin, donor := payload.Merged[0], payload.Merged[1]
		if !origin.Origin || origin.Slug != "mapgenie" {
			t.Errorf("the first account is %s/origin=%t, want the serving reading's own", origin.Slug, origin.Origin)
		}
		if donor.Origin || donor.Slug != "ign-wiki" {
			t.Errorf("the second account is %s/origin=%t, want the folded donor", donor.Slug, donor.Origin)
		}
		if donor.MatchedN() != len(valeAnchors) || donor.MedianMatchPx() != 0 {
			t.Errorf("the donor matched %d places at median %dpx, want all %d anchors at 0",
				donor.MatchedN(), donor.MedianMatchPx(), len(valeAnchors))
		}
		if donor.Added != len(donorOnly) || donor.HeldN() != 0 || donor.RejectedN() != 0 {
			t.Errorf("the donor added %d, held %d, rejected %d; it offered exactly %d places of its own",
				donor.Added, donor.HeldN(), donor.RejectedN(), len(donorOnly))
		}
		if donor.Alignment == "" {
			t.Error("the donor's account names no alignment, and the merge stood on one")
		}
		// The donor-only collection travelled, filed under its source's name.
		var extras bool
		for _, collection := range payload.Collections {
			if collection.Title == "Extras" && collection.Group == "IGN Wiki" {
				extras = true
			}
		}
		if !extras {
			t.Error("the donor's own collection did not arrive under its source's heading")
		}
	})

	t.Run("the city", func(t *testing.T) {
		reader := byVolume["bend-or"]
		if reader == nil {
			t.Fatal("no build of the city was installed")
		}
		if got := reader.Manifest.Version.CreatedAt; got != cityCapturedAt {
			t.Errorf("createdAt %q, want the capture time %q", got, cityCapturedAt)
		}
		if reader.Manifest.Version.Revision != compose.PolicyRevision {
			t.Errorf("revision %d, want the plain %d", reader.Manifest.Version.Revision, compose.PolicyRevision)
		}
		// The standard glyph the pins declare was resolved from the vendored
		// library into the bundle's own artwork.
		var monument, drawn bool
		for _, name := range reader.Names() {
			if name == "icons/std--maki-monument.svg" {
				monument = true
			}
			if strings.HasPrefix(name, "tiles/"+cityDay+"/") && strings.HasSuffix(name, ".png") {
				drawn = true
			}
		}
		if !monument {
			t.Error("the bundle carries no resolved standard glyph")
		}
		if !drawn {
			t.Error("the bundle carries no drawn basemap tiles")
		}
		// All three feature kinds crossed the pipeline: pins, the multipart
		// zoning ground, and the trail path.
		var payload struct {
			Collections []struct {
				Title    string `json:"title"`
				Kind     string `json:"kind"`
				Features []struct {
					Title    string            `json:"title"`
					Geometry []json.RawMessage `json:"geometry"`
				} `json:"features"`
			} `json:"collections"`
		}
		readBundleJSON(t, reader, bundle.WorldEntryName(cityDay, bundle.WorldSuffix), &payload)
		for _, collection := range payload.Collections {
			if collection.Title == "Zoning" {
				for _, zone := range collection.Features {
					if zone.Title == "RS" && len(zone.Geometry) != 1 {
						t.Errorf("the RS zone draws %d parts; its one captured row is the multipart one",
							len(zone.Geometry))
					}
				}
			}
		}
		// Point features travel packed rather than inline in the payload, so
		// the kinds are read off the manifest's own tally: the pins, the two
		// zoning grounds and the trail path all crossed the pipeline.
		entry := reader.Manifest.Worlds[0]
		if entry.Points != 2 || entry.Areas != 2 || entry.Paths != 1 {
			t.Errorf("the world holds %d pins, %d areas and %d paths, want 2, 2 and 1",
				entry.Points, entry.Areas, entry.Paths)
		}
	})
}

// --- small helpers ----------------------------------------------------------

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readBundleJSON(t *testing.T, reader *bundle.Reader, entry string, dst any) {
	t.Helper()
	data, err := reader.ReadEntry(entry)
	if err != nil {
		t.Fatalf("read %s: %v", entry, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("decode %s: %v", entry, err)
	}
}

// tileBound spells one zoom's inclusive window the way a MapGenie capture does.
func tileBound(maxX, maxY int) map[string]any {
	return map[string]any{
		"x": map[string]any{"min": 0, "max": maxX},
		"y": map[string]any{"min": 0, "max": maxY},
	}
}

// syntheticRing is a closed rectangle in the degrees a picture-publishing
// source speaks, from pixel corners of the world square.
func syntheticRing(x0, y0, x1, y1 float64) []any {
	corner := func(x, y float64) []any {
		return []any{doc.SyntheticLng(x), doc.SyntheticLat(y)}
	}
	return []any{corner(x0, y0), corner(x1, y0), corner(x1, y1), corner(x0, y1), corner(x0, y0)}
}

// degreeRing is a closed rectangle in true degrees, corner to corner.
func degreeRing(west, south, east, north float64) []any {
	return []any{
		lonLat(west, north), lonLat(east, north),
		lonLat(east, south), lonLat(west, south), lonLat(west, north),
	}
}

func lonLat(lon, lat float64) []any { return []any{lon, lat} }

func hashFileAt(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// hashTree digests every file under a directory, keyed by its path within it.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hashFileAt(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// compareTrees holds two directory digests to identity and names the first
// file that moved, because "the trees differ" is not a diff anybody reads.
func compareTrees(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	for name, sum := range want {
		other, held := got[name]
		if !held {
			t.Errorf("%s: %s is in one run and not the other", what, name)
			continue
		}
		if other != sum {
			t.Errorf("%s: %s is %s in one run and %s in the other", what, name, sum[:12], other[:12])
		}
	}
	for name := range got {
		if _, held := want[name]; !held {
			t.Errorf("%s: %s is in the second run and not the first", what, name)
		}
	}
}
