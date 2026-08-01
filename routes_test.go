package main

import (
	"encoding/json"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/FelineStateMachine/atlas/internal/bundle/bundletest"
)

func fixtureRegistry(t *testing.T, specs ...bundletest.Spec) *bundle.Registry {
	t.Helper()
	dir := t.TempDir()
	for _, spec := range specs {
		bundletest.Build(t, dir, spec)
	}
	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRoutesServeEmbeddedExplorer(t *testing.T) {
	rec := get(t, routes(assets, fixtureRegistry(t)), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, `id="map"`) {
		t.Fatal("root response does not contain the map explorer")
	}
	if body := rec.Body.String(); strings.Contains(body, "OFFLINE") {
		t.Fatal("root response contains the redundant offline status chip")
	}
	if body := rec.Body.String(); strings.Contains(body, "Z13 EMBEDDED ASSETS") {
		t.Fatal("root response contains the redundant asset credit")
	}
	// Text-overlay categories are no longer promoted into their own row; they
	// sit in their own group alongside the other categories, and zones are a
	// layer section like any other rather than a separate always-open panel.
	if body := rec.Body.String(); strings.Contains(body, `id="text-overlay-controls"`) {
		t.Fatal("root response still promotes text-overlay controls out of their group")
	}
	if body := rec.Body.String(); !strings.Contains(body, `id="layers"`) {
		t.Fatal("root response does not contain the layers list")
	}
	if body := rec.Body.String(); !strings.Contains(body, `id="zone-toggle"`) {
		t.Fatal("root response does not contain the zone boundaries switch")
	}
}

func TestRoutesReturnNotFoundForUnknownPage(t *testing.T) {
	if rec := get(t, routes(assets, fixtureRegistry(t)), "/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// The shell must stand on its own: it is what greets a reader before any
// bundle is installed, and nothing in it may lean on a network that Atlas
// promises not to need.
func TestEmbeddedShellUsesNoRuntimeCDNs(t *testing.T) {
	index, err := fs.ReadFile(assets, "assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if body := string(index); strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Fatal("application shell references a runtime network dependency")
	}
	for _, path := range []string{"assets/app.js", "assets/app.css"} {
		if _, err := fs.Stat(assets, path); err != nil {
			t.Errorf("embedded frontend asset %q: %v", path, err)
		}
	}
}

func TestCatalogAnswersForInstalledBundles(t *testing.T) {
	handler := routes(assets, fixtureRegistry(t,
		bundletest.Spec{Slug: "zebra-quest", Title: "Zebra Quest"},
		bundletest.Spec{Slug: "aardvark-saga", Title: "Aardvark Saga"},
	))

	rec := get(t, handler, "/data/catalog.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Installing a bundle must show up on the very next fetch, so the catalog
	// is the one response nothing may hold on to.
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("catalog Cache-Control = %q, want no-store", cache)
	}
	var catalog struct {
		Volumes []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
			Base  string `json:"base"`
			Maps  []struct {
				Slug string `json:"slug"`
			} `json:"worlds"`
		} `json:"volumes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Volumes) != 2 ||
		catalog.Volumes[0].Title != "Aardvark Saga" || catalog.Volumes[1].Title != "Zebra Quest" {
		t.Fatalf("games = %+v, want Aardvark Saga then Zebra Quest", catalog.Volumes)
	}
	if base := catalog.Volumes[0].Base; !strings.HasPrefix(base, "/data/v/aardvark-saga/") {
		t.Errorf("base = %q, want a stamped /data/v/aardvark-saga/ prefix", base)
	}
}

func TestCatalogIsEmptyRatherThanAbsentWithNoBundles(t *testing.T) {
	rec := get(t, routes(assets, fixtureRegistry(t)), "/data/catalog.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var catalog struct {
		Volumes    []any  `json:"volumes"`
		BundlesDir string `json:"bundlesDir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Volumes == nil || len(catalog.Volumes) != 0 {
		t.Errorf("volumes = %v, want a present, empty list", catalog.Volumes)
	}
	// The empty state tells the reader where bundles go, in the words of
	// their own machine.
	if catalog.BundlesDir == "" {
		t.Error("catalog names no bundles directory")
	}
}

// Without a window there is no native picker to raise, and the request says
// so rather than hanging or pretending.
func TestImportWithoutAWindowIsRefused(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(assets, fixtureRegistry(t)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/data/bundles/import", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestDataServesEveryKindOfBundleEntry(t *testing.T) {
	registry := fixtureRegistry(t, bundletest.Spec{
		Slug: "game", Title: "Game",
		Worlds: []bundletest.WorldSpec{{Slug: "overworld", Pins: []bundletest.Pin{{Title: "Origin"}}}},
	})
	handler := routes(assets, registry)
	base := gameBase(t, handler, "game")

	served := []struct{ path, kind string }{
		{"/worlds/overworld.json", "application/json"},
		{"/worlds/overworld.text", "application/json"},
		{"/worlds/overworld.bin", "application/octet-stream"},
		{"/tiles/overworld/0/0/0.jpg", "image/jpeg"},
		{"/icons/marker.svg", "image/svg+xml"},
	}
	for _, entry := range served {
		rec := get(t, handler, base+entry.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", entry.path, rec.Code)
			continue
		}
		if kind := rec.Header().Get("Content-Type"); kind != entry.kind {
			t.Errorf("%s: Content-Type = %q, want %q", entry.path, kind, entry.kind)
		}
		// Stamped URLs name one build of one game forever, so everything
		// under them may be kept for as long as anyone likes.
		if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Errorf("%s: Cache-Control = %q, want immutable", entry.path, cache)
		}
	}

	packed := get(t, handler, base+"/worlds/overworld.bin").Body.Bytes()
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 || locations[0].Title != "Origin" {
		t.Fatalf("served locations = %+v", locations)
	}
}

func TestDataRefusesWhatItDoesNotHold(t *testing.T) {
	registry := fixtureRegistry(t, bundletest.Spec{Slug: "game", Title: "Game"})
	handler := routes(assets, registry)
	base := gameBase(t, handler, "game")

	refused := []string{
		"/data/v/other-volume/000000000000/worlds/overworld.json",
		// A stale stamp is a build that has been replaced: the frontend takes
		// the 404 as its cue to refetch the catalog.
		"/data/v/game/000000000000/worlds/overworld.json",
		// The manifest and anything outside the content trees stay private.
		base + "/atlas.json",
		base + "/maps/missing.json",
		base + "/worlds/overworld",
	}
	for _, path := range refused {
		if rec := get(t, handler, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	// A climbing path never reaches a bundle: the mux redirects it to its
	// cleaned form, and the cleaned form names nothing.
	climb := get(t, handler, base+"/tiles/../../atlas.json")
	if climb.Code != http.StatusTemporaryRedirect && climb.Code != http.StatusNotFound {
		t.Errorf("climbing path: status = %d, want a redirect or refusal", climb.Code)
	}
	if location := climb.Header().Get("Location"); location != "" {
		if rec := get(t, handler, location); rec.Code != http.StatusNotFound {
			t.Errorf("climbing path lands on %s with status %d, want %d",
				location, rec.Code, http.StatusNotFound)
		}
	}
}

func gameBase(t *testing.T, handler http.Handler, slug string) string {
	t.Helper()
	var catalog struct {
		Volumes []struct {
			Slug string `json:"slug"`
			Base string `json:"base"`
		} `json:"volumes"`
	}
	if err := json.Unmarshal(get(t, handler, "/data/catalog.json").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, game := range catalog.Volumes {
		if game.Slug == slug {
			return game.Base
		}
	}
	t.Fatalf("game %s is not in the catalog", slug)
	return ""
}

// ---------------------------------------------------------------------------
// Everything below runs against the real bundles when they have been built
// into the central library, and is skipped when they have not: the corpus
// lives outside the repository, so a checkout without ../gamemap still tests
// everything above.

func builtRegistry(t *testing.T) *bundle.Registry {
	t.Helper()
	library, err := bundle.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	registry := bundle.NewRegistry(library)
	if err := registry.Rescan(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func builtBundles(t *testing.T) map[string]*bundle.Bundle {
	t.Helper()
	games := builtRegistry(t).Snapshot().Volumes
	if len(games) == 0 {
		t.Skip("no built bundles in the library; run go generate first")
	}
	return games
}

func builtGame(t *testing.T, games map[string]*bundle.Bundle, slug string) *bundle.Bundle {
	t.Helper()
	held, ok := games[slug]
	if !ok {
		t.Fatalf("game %s is not among the built bundles", slug)
	}
	return held
}

func builtMapSlug(t *testing.T, held *bundle.Bundle, title string) string {
	t.Helper()
	for _, entry := range held.Manifest.Worlds {
		if entry.Title == title {
			return entry.Slug
		}
	}
	t.Fatalf("%s has no map titled %q", held.Manifest.Volume.Slug, title)
	return ""
}

type mapPayload struct {
	// Grid is carried only by a map cut from a window of its own; the rest are
	// placed against the game's.
	Grid *struct {
		SourceZoom int `json:"sourceZoom"`
		FirstTile  int `json:"firstTile"`
	} `json:"grid"`
	Lenses []struct {
		Name    string   `json:"name"`
		Tiles   string   `json:"tiles"`
		Formats []string `json:"formats"`
		Shard   int64    `json:"shard"`
		MinZoom int      `json:"minZoom"`
		MaxZoom int      `json:"maxZoom"`
		Bounds  *struct {
			X, Y, Width, Height int
		} `json:"bounds"`
		Surface *struct {
			X, Y, Width, Height int
		} `json:"surface"`
	} `json:"lenses"`
	Groups []struct {
		Categories []struct {
			Title       string `json:"title"`
			DisplayType string `json:"displayType"`
			Color       string `json:"color"`
			IconAsset   string `json:"iconAsset"`
		} `json:"categories"`
	} `json:"groups"`
	Zones []any `json:"zones"`
}

func readBuiltPayload(t *testing.T, held *bundle.Bundle, slug string) mapPayload {
	t.Helper()
	raw, err := held.ReadEntry("worlds/" + slug + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var payload mapPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func readBuiltLocations(t *testing.T, held *bundle.Bundle, slug string) []bundle.Location {
	t.Helper()
	raw, err := held.ReadEntry("worlds/" + slug + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	locations, err := bundle.UnpackLocations(raw)
	if err != nil {
		t.Fatal(err)
	}
	return locations
}

// Every built bundle keeps every promise its manifest makes: payloads
// present, pin counts agreed, tile levels filled, icons resolvable, and no
// live URLs anywhere. Validate is the same check the bundler runs, repeated
// here so a bundle edited or corrupted after generation still fails loudly.
func TestBuiltBundlesKeepTheirPromises(t *testing.T) {
	games := builtBundles(t)
	var maps int
	for _, held := range games {
		if err := held.Validate(); err != nil {
			t.Errorf("%s: %v", held.Manifest.Volume.Slug, err)
		}
		maps += len(held.Manifest.Worlds)
	}
	if maps < 20 {
		t.Errorf("built bundles list %d maps, want at least 20", maps)
	}
	for _, name := range []string{
		"skyrim", "temtem", "zelda-breath-of-the-wild", "pokemon-red-blue-yellow", "gta5",
	} {
		builtGame(t, games, name)
	}
}

// The catalog is fetched every time Atlas opens, so its size is the cost of
// starting up. It must not grow with what the bundles hold.
func TestComposedCatalogStaysSmall(t *testing.T) {
	registry := builtRegistry(t)
	if len(registry.Snapshot().Volumes) == 0 {
		t.Skip("no built bundles in the library; run go generate first")
	}
	if size := len(registry.Snapshot().Catalog); size > 64*1024 {
		t.Errorf("composed catalog = %d bytes, want under 64 KiB", size)
	}
}

// A map split into layers offers one at a time, and a location that does not
// name its layer is drawn on all of them -- over the empty space beside a
// layer it does not belong to. The packed shard is what prevents that, so
// every location of a layered map must carry one its variants know.
func TestLayeredMapLocationsNameTheirLayer(t *testing.T) {
	games := builtBundles(t)
	totk := builtGame(t, games, "zelda-tears-of-the-kingdom")
	hyrule := builtMapSlug(t, totk, "Hyrule")
	payload := readBuiltPayload(t, totk, hyrule)
	layers := make(map[int64]string, len(payload.Lenses))
	for _, variant := range payload.Lenses {
		if variant.Shard == 0 {
			t.Fatalf("Tears of the Kingdom layer %q names no shard", variant.Name)
		}
		layers[variant.Shard] = variant.Name
	}
	if len(layers) < 2 {
		t.Fatalf("Tears of the Kingdom has %d layers, want the Sky, Surface and Depths", len(layers))
	}

	counts := make(map[int64]int, len(layers))
	for _, location := range readBuiltLocations(t, totk, hyrule) {
		if _, ok := layers[location.Shard]; !ok {
			t.Fatalf("%q is on shard %d, which is no layer of this map", location.Title, location.Shard)
		}
		counts[location.Shard]++
	}
	for shard, name := range layers {
		if counts[shard] == 0 {
			t.Errorf("the %s layer holds no locations", name)
		}
	}
}

// A piece cut from a sheet keeps a margin in its raster so the title drawn
// beside it is not sliced in half. The geohash grid divides the ground instead,
// or a small map spends whole cells on blank sheet -- so the two rectangles
// travel separately, with the ground inside the window it is drawn through.
func TestSheetPiecesCarryTheirGroundSeparately(t *testing.T) {
	games := builtBundles(t)
	vegas := builtGame(t, games, "fallout-new-vegas")
	mccarran := builtMapSlug(t, vegas, "Mojave Wasteland — Camp McCarran")
	for _, variant := range readBuiltPayload(t, vegas, mccarran).Lenses {
		if variant.Bounds == nil || variant.Surface == nil {
			t.Fatalf("Camp McCarran layer %q has bounds %v and surface %v, wanting both",
				variant.Name, variant.Bounds, variant.Surface)
		}
		if variant.Surface.Width <= 0 || variant.Surface.Height <= 0 {
			t.Fatalf("Camp McCarran surface is %dx%d", variant.Surface.Width, variant.Surface.Height)
		}
		if variant.Surface.X < variant.Bounds.X || variant.Surface.Y < variant.Bounds.Y ||
			variant.Surface.X+variant.Surface.Width > variant.Bounds.X+variant.Bounds.Width ||
			variant.Surface.Y+variant.Surface.Height > variant.Bounds.Y+variant.Bounds.Height {
			t.Fatalf("Camp McCarran ground %v reaches outside the window %v drawn for it",
				*variant.Surface, *variant.Bounds)
		}
		if variant.Surface.Width >= variant.Bounds.Width &&
			variant.Surface.Height >= variant.Bounds.Height {
			t.Error("Camp McCarran keeps no margin, so the split no longer grows its pieces")
		}
	}

	// Every map names its ground, split or not: the sheets that were never cut
	// up are the ones with a printed border or a title panel around them, and
	// the grid has to know the difference there too.
	for _, place := range []struct{ game, title string }{
		{"skyrim", "Skyrim"},
		{"tunic", "World"},
		{"sonic-frontiers", "Kronos Island"},
		{"la-noire", "Los Angeles"},
	} {
		held := builtGame(t, games, place.game)
		slug := builtMapSlug(t, held, place.title)
		for _, variant := range readBuiltPayload(t, held, slug).Lenses {
			if variant.Surface == nil || variant.Surface.Width <= 0 || variant.Surface.Height <= 0 {
				t.Errorf("%s / %s layer %q measures no ground", place.game, place.title, variant.Name)
				continue
			}
			if variant.Bounds == nil {
				continue
			}
			if variant.Surface.X < variant.Bounds.X || variant.Surface.Y < variant.Bounds.Y ||
				variant.Surface.X+variant.Surface.Width > variant.Bounds.X+variant.Bounds.Width ||
				variant.Surface.Y+variant.Surface.Height > variant.Bounds.Y+variant.Bounds.Height {
				t.Errorf("%s / %s ground %v reaches outside its window %v",
					place.game, place.title, *variant.Surface, *variant.Bounds)
			}
		}
	}

	// Tunic is drawn inside a solid border filling a full-world window, so its
	// ground has to be a fraction of what it is cut from.
	tunic := builtGame(t, games, "tunic")
	for _, variant := range readBuiltPayload(t, tunic, builtMapSlug(t, tunic, "World")).Lenses {
		if variant.Surface.Width > 6000 || variant.Surface.Height > 6000 {
			t.Errorf("Tunic ground is %dx%d, so the border is still being divided up",
				variant.Surface.Width, variant.Surface.Height)
		}
	}
}

func TestBuiltBundlesCarryTextLabelsAndZones(t *testing.T) {
	games := builtBundles(t)

	marathon := builtGame(t, games, "marathon")
	cryo := builtMapSlug(t, marathon, "Cryo Archive")
	payload := readBuiltPayload(t, marathon, cryo)
	var areaOrdinal = -1
	var ordinal int
	for _, group := range payload.Groups {
		for _, category := range group.Categories {
			if category.Title == "Area" && category.DisplayType == "text" {
				areaOrdinal = ordinal
			}
			ordinal++
		}
	}
	if areaOrdinal < 0 {
		t.Fatal("Cryo Archive has no text-display Area category")
	}
	var foundPanopticon bool
	for _, location := range readBuiltLocations(t, marathon, cryo) {
		if location.Title == "PANOPTICON" && int(location.Owner) == areaOrdinal {
			foundPanopticon = true
		}
	}
	if !foundPanopticon {
		t.Error("PANOPTICON is not preserved as a text-display location")
	}

	forza := builtGame(t, games, "forza-horizon-6")
	japan := readBuiltPayload(t, forza, builtMapSlug(t, forza, "Japan"))
	if len(japan.Zones) != 10 {
		t.Errorf("Forza Japan zones = %d, want 10", len(japan.Zones))
	}
	var bounded int
	for _, variant := range japan.Lenses {
		if variant.Bounds != nil && variant.Bounds.Width == 4096 && variant.Bounds.Height == 4096 {
			bounded++
		}
	}
	if bounded != 4 {
		t.Errorf("bounded Forza Japan variants = %d, want 4", bounded)
	}
}

// Los Santos is cut from a window of its own: five levels shallower than the
// one the other maps share, and anchored at the origin rather than at tile
// 4064. A pin arrives as a latitude and longitude in that window, so the map
// has to carry it -- placed against the shared numbers instead, every pin would
// land in a corner of the world with no map beneath it. This is the frontend's
// own projection, kept honest against the numbers actually shipped.
func TestAMapCutFromItsOwnWindowPlacesItsPins(t *testing.T) {
	games := builtBundles(t)
	gta := builtGame(t, games, "gta5")
	slug := builtMapSlug(t, gta, "Los Santos")
	payload := readBuiltPayload(t, gta, slug)
	if payload.Grid == nil {
		t.Fatal("Los Santos carries no window of its own")
	}
	if payload.Grid.SourceZoom != 6 || payload.Grid.FirstTile != 0 {
		t.Errorf("window = zoom %d at tile %d, want zoom 6 at tile 0",
			payload.Grid.SourceZoom, payload.Grid.FirstTile)
	}
	bounds := payload.Lenses[0].Bounds
	if bounds == nil {
		t.Fatal("the first layer has no bounds")
	}

	tileSize := float64(gta.Manifest.TileGrid.TileSize)
	worldTiles := math.Pow(2, float64(payload.Grid.SourceZoom))
	first := float64(payload.Grid.FirstTile)
	var placed int
	locations := readBuiltLocations(t, gta, slug)
	for _, location := range locations {
		x := (((location.Lng+180)/360)*worldTiles - first) * tileSize
		radians := location.Lat * math.Pi / 180
		down := (1 - math.Asinh(math.Tan(radians))/math.Pi) / 2 * worldTiles
		y := -(down - first) * tileSize
		if x >= float64(bounds.X) && x <= float64(bounds.X+bounds.Width) &&
			y <= -float64(bounds.Y) && y >= -float64(bounds.Y+bounds.Height) {
			placed++
		}
	}
	// A couple of the captured records are jokes standing at the pole -- "Tommy
	// Vercetti" among them -- and belong nowhere on this map. Everything real
	// is on the raster.
	if ratio := float64(placed) / float64(len(locations)); ratio < 0.99 {
		t.Errorf("%d of %d pins land on the raster (%.1f%%), want at least 99%%",
			placed, len(locations), ratio*100)
	}
}

func TestBuiltBundlesCarryCategoryIconsAndColors(t *testing.T) {
	games := builtBundles(t)
	var iconAssets int
	var foundPokemonCenter bool
	for _, held := range games {
		for _, entry := range held.Manifest.Worlds {
			for _, group := range readBuiltPayload(t, held, entry.Slug).Groups {
				for _, category := range group.Categories {
					if category.IconAsset != "" {
						iconAssets++
						if strings.Contains(category.IconAsset, "/") {
							t.Errorf("%s names icon %q, which is not bundle-relative",
								held.Manifest.Volume.Slug, category.IconAsset)
						}
						if !held.Has("icons/" + category.IconAsset) {
							t.Errorf("%s icon %q is missing", held.Manifest.Volume.Slug, category.IconAsset)
						}
					}
					if held.Manifest.Volume.Slug != "pokemon-red-blue-yellow" ||
						entry.Title != "Yellow" || category.Title != "Pokémon Center" {
						continue
					}
					foundPokemonCenter = true
					if category.Color != "#38344C" {
						t.Errorf("Pokémon Center color = %q, want #38344C", category.Color)
					}
					if category.IconAsset == "" {
						t.Error("Pokémon Center has no icon asset")
					}
				}
			}
		}
	}
	if !foundPokemonCenter {
		t.Error("Yellow Pokémon Center category not found")
	}
	if iconAssets < 200 {
		t.Errorf("catalog icon assets = %d, want at least 200", iconAssets)
	}
}
