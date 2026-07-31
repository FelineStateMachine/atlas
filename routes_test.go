package main

import (
	"encoding/json"
	"errors"
	"io/fs"
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

func TestEmbeddedCatalogCarriesNoExternalURLs(t *testing.T) {
	data, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	// Atlas runs with no network. Source descriptions cite mapgenie and YouTube
	// URLs that would simply be dead here, so generation strips them and keeps
	// the ones pointing at this map as in-catalog cross-references instead.
	for _, scheme := range []string{"http://", "https://"} {
		if index := strings.Index(string(data), scheme); index >= 0 {
			extract := string(data[index:min(index+120, len(data))])
			t.Errorf("catalog carries a runtime URL: %q", extract)
		}
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

func TestEmbeddedCatalogCarriesTextLabelsAndZones(t *testing.T) {
	data, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Games []struct {
			Title string `json:"title"`
			Maps  []struct {
				Title    string `json:"title"`
				Zones    []any  `json:"zones"`
				Variants []struct {
					Name   string `json:"name"`
					Bounds *struct {
						X      int `json:"x"`
						Y      int `json:"y"`
						Width  int `json:"width"`
						Height int `json:"height"`
					} `json:"bounds"`
				} `json:"variants"`
				Groups []struct {
					Categories []struct {
						Title       string `json:"title"`
						DisplayType string `json:"displayType"`
						Locations   []struct {
							Title    string `json:"title"`
							RegionID *int64 `json:"regionId"`
						} `json:"locations"`
					} `json:"categories"`
				} `json:"groups"`
			} `json:"maps"`
		} `json:"games"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}

	var foundPanopticon bool
	var forzaZones, forzaPinsWithRegion, boundedForzaVariants int
	for _, game := range catalog.Games {
		for _, gameMap := range game.Maps {
			if game.Title == "Forza Horizon 6" && gameMap.Title == "Japan" {
				forzaZones = len(gameMap.Zones)
				for _, variant := range gameMap.Variants {
					if variant.Bounds != nil &&
						variant.Bounds.X == 2048 && variant.Bounds.Y == 2048 &&
						variant.Bounds.Width == 4096 && variant.Bounds.Height == 4096 {
						boundedForzaVariants++
					}
				}
			}
			for _, group := range gameMap.Groups {
				for _, category := range group.Categories {
					for _, location := range category.Locations {
						if game.Title == "Marathon" && gameMap.Title == "Cryo Archive" &&
							category.Title == "Area" && category.DisplayType == "text" &&
							location.Title == "PANOPTICON" {
							foundPanopticon = true
						}
						if game.Title == "Forza Horizon 6" && gameMap.Title == "Japan" &&
							location.RegionID != nil {
							forzaPinsWithRegion++
						}
					}
				}
			}
		}
	}
	if !foundPanopticon {
		t.Fatal("PANOPTICON is not preserved as a text-display location")
	}
	if forzaZones != 10 {
		t.Fatalf("Forza Japan zones = %d, want 10", forzaZones)
	}
	if forzaPinsWithRegion != 806 {
		t.Fatalf("Forza Japan pins with a region = %d, want 806", forzaPinsWithRegion)
	}
	if boundedForzaVariants != 4 {
		t.Fatalf("bounded Forza Japan variants = %d, want 4", boundedForzaVariants)
	}
}

func TestEmbeddedCatalogIncludesOnlyCompleteNewMaps(t *testing.T) {
	data, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Games []struct {
			Title string `json:"title"`
			Maps  []struct {
				Title    string `json:"title"`
				Variants []struct {
					Tiles   string   `json:"tiles"`
					Formats []string `json:"formats"`
					MinZoom int      `json:"minZoom"`
					MaxZoom int      `json:"maxZoom"`
				} `json:"variants"`
			} `json:"maps"`
		} `json:"games"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, game := range catalog.Games {
		for _, gameMap := range game.Maps {
			got[game.Title+" / "+gameMap.Title] = true
			for _, variant := range gameMap.Variants {
				if len(variant.Formats) != variant.MaxZoom-variant.MinZoom+1 {
					t.Errorf("%s / %s tile formats = %d, want %d",
						game.Title, gameMap.Title, len(variant.Formats), variant.MaxZoom-variant.MinZoom+1)
					continue
				}
				for zoom := variant.MinZoom; zoom <= variant.MaxZoom; zoom++ {
					level := "assets/tiles/" + variant.Tiles + "/" + strconv.Itoa(zoom)
					entries, err := fs.ReadDir(assets, level)
					if err != nil {
						t.Errorf("%s / %s references missing tile level %q: %v",
							game.Title, gameMap.Title, level, err)
						continue
					}
					if len(entries) == 0 {
						t.Errorf("%s / %s tile level %q is empty", game.Title, gameMap.Title, level)
					}
				}
			}
		}
	}
	for _, name := range []string{
		"Skyrim / Skyrim",
		"Skyrim / Solstheim",
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
		"Pokémon Red/Blue/Yellow / Red/Blue",
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
	body := string(index)
	if strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Fatal("application shell references a runtime network dependency")
	}
	if strings.Contains(body, `id="map-image"`) || strings.Contains(body, `id="world"`) {
		t.Fatal("application shell still contains the stitched image viewport")
	}
	for _, path := range []string{"assets/app.js", "assets/app.css", "assets/tiles/index.json"} {
		if _, err := fs.Stat(assets, path); err != nil {
			t.Errorf("embedded frontend asset %q: %v", path, err)
		}
	}
	if _, err := fs.Stat(assets, "assets/maps"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("legacy stitched map directory still exists: %v", err)
	}
}

func TestEmbeddedCatalogCarriesCategoryIconsAndColors(t *testing.T) {
	data, err := fs.ReadFile(assets, "assets/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Games []struct {
			Title string `json:"title"`
			Maps  []struct {
				Title  string `json:"title"`
				Groups []struct {
					Categories []struct {
						Title     string `json:"title"`
						Color     string `json:"color"`
						IconAsset string `json:"iconAsset"`
					} `json:"categories"`
				} `json:"groups"`
			} `json:"maps"`
		} `json:"games"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}

	var foundPokemonCenter bool
	var iconAssets int
	for _, game := range catalog.Games {
		for _, gameMap := range game.Maps {
			for _, group := range gameMap.Groups {
				for _, category := range group.Categories {
					if category.IconAsset != "" {
						iconAssets++
						if _, err := fs.Stat(assets, "assets/icons/"+category.IconAsset); err != nil {
							t.Errorf("embedded icon %q: %v", category.IconAsset, err)
						}
					}
					if game.Title != "Pokémon Red/Blue/Yellow" ||
						gameMap.Title != "Yellow" || category.Title != "Pokémon Center" {
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
