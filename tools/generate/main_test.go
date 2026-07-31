package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvedCategoryColor(t *testing.T) {
	tests := []struct {
		name     string
		group    rawGroup
		category rawCategory
		want     string
	}{
		{
			name:     "category overrides group",
			group:    rawGroup{Color: "38344C"},
			category: rawCategory{Color: "aaacae"},
			want:     "#AAACAE",
		},
		{
			name:     "group fallback",
			group:    rawGroup{Color: "38344C"},
			category: rawCategory{},
			want:     "#38344C",
		},
		{
			name:     "invalid color omitted",
			group:    rawGroup{Color: "not-a-color"},
			category: rawCategory{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvedCategoryColor(tt.group, tt.category); got != tt.want {
				t.Fatalf("resolvedCategoryColor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttachGameIcons(t *testing.T) {
	source := t.TempDir()
	output := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	const svg = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M0 0h24v24H0z"/></svg>`
	if err := os.WriteFile(filepath.Join(source, "icons", "pokemon_center.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}

	game := catalogGame{
		Slug: "pokemon-red-blue-yellow",
		Maps: []catalogMap{{
			Groups: []catalogGroup{{
				Categories: []catalogCategory{
					{Icon: "pokemon_center"},
					{Icon: "missing"},
				},
			}},
		}},
	}
	if err := attachGameIcons(source, output, &game); err != nil {
		t.Fatal(err)
	}

	categories := game.Maps[0].Groups[0].Categories
	if got, want := categories[0].IconAsset, "pokemon-red-blue-yellow/pokemon_center.svg"; got != want {
		t.Fatalf("icon asset = %q, want %q", got, want)
	}
	if categories[1].IconAsset != "" {
		t.Fatalf("missing icon asset = %q, want empty", categories[1].IconAsset)
	}
	data, err := os.ReadFile(filepath.Join(output, categories[0].IconAsset))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != svg {
		t.Fatalf("copied SVG = %q, want %q", got, svg)
	}
}

func TestBuildGameSkipsMapWithoutSnapshotIndex(t *testing.T) {
	archiveRoot := t.TempDir()
	gamePath := filepath.Join(archiveRoot, "games", "pokemon-red-blue-yellow-246")
	if err := os.MkdirAll(filepath.Join(gamePath, "maps", "red-blue-847"), 0o755); err != nil {
		t.Fatal(err)
	}

	game, err := buildGame(
		archiveRoot,
		t.TempDir(),
		nil,
		archiveGame{
			Directory: "games/pokemon-red-blue-yellow-246",
			ID:        246,
			Title:     "Pokémon Red/Blue/Yellow",
		},
		tileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Maps) != 0 {
		t.Fatalf("maps = %d, want 0", len(game.Maps))
	}
}

func TestSortGameMapsPrefersPrimaryMap(t *testing.T) {
	maps := []catalogMap{
		{Title: "Big MT", Slug: "big-mt"},
		{Title: "Zion Canyon", Slug: "zion-canyon"},
		{Title: "Mojave Wasteland", Slug: "mojave-wasteland"},
		{Title: "Sierra Madre", Slug: "sierra-madre"},
	}

	sortGameMaps("fallout-new-vegas", maps)

	want := []string{"mojave-wasteland", "big-mt", "sierra-madre", "zion-canyon"}
	for index, slug := range want {
		if maps[index].Slug != slug {
			t.Fatalf("map %d = %q, want %q", index, maps[index].Slug, slug)
		}
	}
}
