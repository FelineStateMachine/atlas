package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRoutesServeEmbeddedExplorer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	routes(assets).ServeHTTP(rec, req)

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
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	routes(assets).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// readPackedLocations is the reader for the layout tools/generate writes, kept
// here so a change to the packing that the viewer could not decode fails the
// build rather than the map.
func readPackedLocations(t *testing.T, mapID int64) []packedLocation {
	t.Helper()
	raw, err := fs.ReadFile(assets, fmt.Sprintf("assets/catalog/%d.bin", mapID))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[:8]) != "ATLASLOC" {
		t.Fatalf("map %d location payload is not in the expected form", mapID)
	}
	if version := binary.LittleEndian.Uint16(raw[8:]); version != 2 {
		t.Fatalf("map %d location payload is version %d, and this reads 2", mapID, version)
	}
	count := int(binary.LittleEndian.Uint32(raw[10:]))
	at := 16
	u32 := func(index int) uint32 { return binary.LittleEndian.Uint32(raw[at+index*4:]) }

	ids := make([]int32, count)
	for index := range ids {
		ids[index] = int32(u32(index))
	}
	at += count * 4
	lat := make([]float32, count)
	for index := range lat {
		lat[index] = math.Float32frombits(u32(index))
	}
	at += count * 4
	lng := make([]float32, count)
	for index := range lng {
		lng[index] = math.Float32frombits(u32(index))
	}
	at += count * 8 // longitudes, then region ids, which this reader ignores
	shards := make([]int32, count)
	for index := range shards {
		shards[index] = int32(u32(index))
	}
	at += count * 4
	offsets := make([]uint32, count+1)
	for index := range offsets {
		offsets[index] = u32(index)
	}
	at += (count + 1) * 4
	owners := make([]uint16, count)
	for index := range owners {
		owners[index] = binary.LittleEndian.Uint16(raw[at+index*2:])
	}
	at += count * 2

	titles := raw[at:]
	out := make([]packedLocation, count)
	for index := range out {
		out[index] = packedLocation{
			ID:       int64(ids[index]),
			Title:    string(titles[offsets[index]:offsets[index+1]]),
			Latitude: float64(lat[index]),
			Category: int(owners[index]),
			Shard:    int64(shards[index]),
		}
	}
	return out
}

type packedLocation struct {
	ID       int64
	Title    string
	Latitude float64
	Category int
	Shard    int64
}

type mapPayload struct {
	Variants []struct {
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
	} `json:"variants"`
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

func readMapPayload(t *testing.T, mapID int64) mapPayload {
	t.Helper()
	raw, err := fs.ReadFile(assets, fmt.Sprintf("assets/catalog/%d.json", mapID))
	if err != nil {
		t.Fatal(err)
	}
	var payload mapPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

type catalogIndex struct {
	TileGrid struct {
		TileSize int `json:"tileSize"`
		Size     int `json:"size"`
	} `json:"tileGrid"`
	Games []struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
		Maps  []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			PinCount int    `json:"pinCount"`
		} `json:"maps"`
	} `json:"games"`
}

func readIndex(t *testing.T) catalogIndex {
	t.Helper()
	raw, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var index catalogIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	return index
}

// The index is fetched every time Atlas opens, so its size is the cost of
// starting up. It must not grow with what the catalog holds.
func TestCatalogIndexStaysSmall(t *testing.T) {
	raw, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 64*1024 {
		t.Errorf("catalog index = %d bytes, want under 64 KiB", len(raw))
	}
	index := readIndex(t)
	var maps int
	for _, game := range index.Games {
		maps += len(game.Maps)
	}
	if maps < 20 {
		t.Errorf("index lists %d maps, want at least 20", maps)
	}
}

// Every map the index names must have all three of its parts, and the packed
// locations must agree with the count the index advertises.
func TestEveryListedMapHasItsPayload(t *testing.T) {
	index := readIndex(t)
	for _, game := range index.Games {
		for _, listed := range game.Maps {
			for _, suffix := range []string{".json", ".bin", ".text"} {
				name := fmt.Sprintf("assets/catalog/%d%s", listed.ID, suffix)
				if _, err := fs.Stat(assets, name); err != nil {
					t.Errorf("%s / %s: %v", game.Title, listed.Title, err)
				}
			}
			if got := len(readPackedLocations(t, listed.ID)); got != listed.PinCount {
				t.Errorf("%s / %s packed %d locations, index says %d",
					game.Title, listed.Title, got, listed.PinCount)
			}
		}
	}
}

// A map split into layers offers one at a time, and a location that does not
// name its layer is drawn on all of them -- over the empty space beside a
// layer it does not belong to. The packed shard is what prevents that, so
// every location of a layered map must carry one its variants know.
func TestLayeredMapLocationsNameTheirLayer(t *testing.T) {
	const hyrule = 536
	payload := readMapPayload(t, hyrule)
	layers := make(map[int64]string, len(payload.Variants))
	for _, variant := range payload.Variants {
		if variant.Shard == 0 {
			t.Fatalf("Tears of the Kingdom layer %q names no shard", variant.Name)
		}
		layers[variant.Shard] = variant.Name
	}
	if len(layers) < 2 {
		t.Fatalf("Tears of the Kingdom has %d layers, want the Sky, Surface and Depths", len(layers))
	}

	counts := make(map[int64]int, len(layers))
	for _, location := range readPackedLocations(t, hyrule) {
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

func TestEmbeddedCatalogCarriesTextLabelsAndZones(t *testing.T) {
	index := readIndex(t)
	find := func(game, name string) int64 {
		for _, candidate := range index.Games {
			if candidate.Title != game {
				continue
			}
			for _, listed := range candidate.Maps {
				if listed.Title == name {
					return listed.ID
				}
			}
		}
		t.Fatalf("%s / %s is missing from the index", game, name)
		return 0
	}

	cryo := find("Marathon", "Cryo Archive")
	payload := readMapPayload(t, cryo)
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
	for _, location := range readPackedLocations(t, cryo) {
		if location.Title == "PANOPTICON" && location.Category == areaOrdinal {
			foundPanopticon = true
		}
	}
	if !foundPanopticon {
		t.Error("PANOPTICON is not preserved as a text-display location")
	}

	japan := readMapPayload(t, find("Forza Horizon 6", "Japan"))
	if len(japan.Zones) != 10 {
		t.Errorf("Forza Japan zones = %d, want 10", len(japan.Zones))
	}
	var bounded int
	for _, variant := range japan.Variants {
		if variant.Bounds != nil && variant.Bounds.Width == 4096 && variant.Bounds.Height == 4096 {
			bounded++
		}
	}
	if bounded != 4 {
		t.Errorf("bounded Forza Japan variants = %d, want 4", bounded)
	}
}

func TestEmbeddedCatalogIncludesOnlyCompleteNewMaps(t *testing.T) {
	index := readIndex(t)
	got := make(map[string]bool)
	for _, game := range index.Games {
		for _, listed := range game.Maps {
			got[game.Title+" / "+listed.Title] = true
			payload := readMapPayload(t, listed.ID)
			for _, variant := range payload.Variants {
				if len(variant.Formats) != variant.MaxZoom-variant.MinZoom+1 {
					t.Errorf("%s / %s tile formats = %d, want %d",
						game.Title, listed.Title, len(variant.Formats), variant.MaxZoom-variant.MinZoom+1)
					continue
				}
				for zoom := variant.MinZoom; zoom <= variant.MaxZoom; zoom++ {
					level := "assets/tiles/" + variant.Tiles + "/" + strconv.Itoa(zoom)
					entries, err := fs.ReadDir(assets, level)
					if err != nil {
						t.Errorf("%s / %s references missing tile level %q: %v",
							game.Title, listed.Title, level, err)
						continue
					}
					if len(entries) == 0 {
						t.Errorf("%s / %s tile level %q is empty", game.Title, listed.Title, level)
					}
				}
			}
		}
	}
	for _, name := range []string{
		"Skyrim / Skyrim",
		"Temtem / Airborne Archipelago",
		"Zelda: Breath of the Wild / Hyrule",
		"Pokémon Red/Blue/Yellow / Yellow",
	} {
		if !got[name] {
			t.Errorf("complete map %q is missing", name)
		}
	}
	for _, name := range []string{
		"Grand Theft Auto 5 / Los Santos",
		"Red Dead Redemption 2 / Red Dead Redemption 2",
	} {
		if got[name] {
			t.Errorf("incomplete map %q was included", name)
		}
	}
}

func TestEmbeddedFrontendUsesTilesWithoutRuntimeCDNs(t *testing.T) {
	index, err := fs.ReadFile(assets, "assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if body := string(index); strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Fatal("application shell references a runtime network dependency")
	}
	for _, path := range []string{"assets/app.js", "assets/app.css", "assets/tiles/index.json"} {
		if _, err := fs.Stat(assets, path); err != nil {
			t.Errorf("embedded frontend asset %q: %v", path, err)
		}
	}
}

func TestEmbeddedCatalogCarriesCategoryIconsAndColors(t *testing.T) {
	index := readIndex(t)
	var iconAssets int
	var foundPokemonCenter bool
	for _, game := range index.Games {
		for _, listed := range game.Maps {
			for _, group := range readMapPayload(t, listed.ID).Groups {
				for _, category := range group.Categories {
					if category.IconAsset != "" {
						iconAssets++
						if _, err := fs.Stat(assets, "assets/icons/"+category.IconAsset); err != nil {
							t.Errorf("embedded icon %q: %v", category.IconAsset, err)
						}
					}
					if game.Title != "Pokémon Red/Blue/Yellow" ||
						listed.Title != "Yellow" || category.Title != "Pokémon Center" {
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

// Atlas runs with no network. Source descriptions cite mapgenie and YouTube
// URLs that would simply be dead here, so generation strips them and keeps the
// ones pointing at this map as in-catalog cross-references instead.
func TestEmbeddedCatalogCarriesNoExternalURLs(t *testing.T) {
	entries, err := fs.ReadDir(assets, "assets/catalog")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := fs.ReadFile(assets, "assets/catalog/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, scheme := range []string{"http://", "https://"} {
			if at := strings.Index(string(data), scheme); at >= 0 {
				extract := string(data[at:min(at+120, len(data))])
				t.Errorf("%s carries a runtime URL: %q", entry.Name(), extract)
			}
		}
	}
}
