// Command generate turns an FMG archive into the compact catalog consumed by
// the demo. It intentionally keeps the source archive outside the Go module;
// only the browser-ready catalog and stitched maps are embedded in the app.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type archive struct {
	Games []archiveGame `json:"games"`
}

type archiveGame struct {
	Directory string `json:"directory"`
	ID        int64  `json:"id"`
	Title     string `json:"title"`
}

type snapshotIndex struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
}

type rawMap struct {
	ID               int64       `json:"id"`
	Title            string      `json:"title"`
	Slug             string      `json:"slug"`
	InitialLatitude  float64     `json:"initial_latitude"`
	InitialLongitude float64     `json:"initial_longitude"`
	InitialZoom      float64     `json:"initial_zoom"`
	Config           rawConfig   `json:"config"`
	Game             rawGame     `json:"game"`
	Groups           []rawGroup  `json:"groups"`
	Regions          []rawRegion `json:"regions"`
}

type rawGame struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type rawConfig struct {
	TileSets []rawTileSet `json:"tile_sets"`
}

type rawTileSet struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type rawGroup struct {
	ID         int64         `json:"id"`
	Title      string        `json:"title"`
	Color      string        `json:"color"`
	IconColor  string        `json:"icon_color"`
	Categories []rawCategory `json:"categories"`
}

type rawCategory struct {
	ID          int64         `json:"id"`
	Title       string        `json:"title"`
	Icon        string        `json:"icon"`
	Color       string        `json:"color"`
	IconColor   string        `json:"icon_color"`
	DisplayType string        `json:"display_type"`
	Visible     bool          `json:"visible"`
	Locations   []rawLocation `json:"locations"`
}

type rawLocation struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Latitude    json.RawMessage `json:"latitude"`
	Longitude   json.RawMessage `json:"longitude"`
	RegionID    *int64          `json:"region_id"`
}

type rawRegion struct {
	ID             int64           `json:"id"`
	Title          string          `json:"title"`
	Subtitle       string          `json:"subtitle"`
	ParentRegionID *int64          `json:"parent_region_id"`
	CenterX        json.RawMessage `json:"center_x"`
	CenterY        json.RawMessage `json:"center_y"`
	Features       []rawFeature    `json:"features"`
}

type rawFeature struct {
	Geometry geometry `json:"geometry"`
}

type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type catalog struct {
	Source   string        `json:"source"`
	TileGrid tileGrid      `json:"tileGrid"`
	Games    []catalogGame `json:"games"`
}

type tileGrid struct {
	Zoom      int `json:"zoom"`
	FirstTile int `json:"firstTile"`
	TileSize  int `json:"tileSize"`
	Size      int `json:"size"`
}

type catalogGame struct {
	ID    int64        `json:"id"`
	Title string       `json:"title"`
	Slug  string       `json:"slug"`
	Maps  []catalogMap `json:"maps"`
}

type catalogMap struct {
	ID        int64          `json:"id"`
	Title     string         `json:"title"`
	Slug      string         `json:"slug"`
	Center    coordinate     `json:"center"`
	Variants  []variant      `json:"variants"`
	Groups    []catalogGroup `json:"groups"`
	Zones     []zone         `json:"zones,omitempty"`
	PinCount  int            `json:"pinCount"`
	UpdatedAt string         `json:"updatedAt"`
}

type coordinate struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

type variant struct {
	Name   string         `json:"name"`
	Image  string         `json:"image"`
	Bounds *contentBounds `json:"bounds,omitempty"`
}

type contentBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type tileRecord struct {
	ContentHash string `json:"contentHash"`
	URL         string `json:"url"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Zoom        int    `json:"zoom"`
}

type catalogGroup struct {
	ID         int64             `json:"id"`
	Title      string            `json:"title"`
	Categories []catalogCategory `json:"categories"`
}

type catalogCategory struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Icon        string            `json:"icon,omitempty"`
	IconAsset   string            `json:"iconAsset,omitempty"`
	Color       string            `json:"color,omitempty"`
	IconColor   string            `json:"iconColor,omitempty"`
	DisplayType string            `json:"displayType"`
	Visible     bool              `json:"visible"`
	Locations   []catalogLocation `json:"locations"`
}

type catalogLocation struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Latitude    float64 `json:"lat"`
	Longitude   float64 `json:"lng"`
	RegionID    *int64  `json:"regionId,omitempty"`
}

type zone struct {
	ID             int64       `json:"id"`
	Title          string      `json:"title"`
	Subtitle       string      `json:"subtitle,omitempty"`
	ParentRegionID *int64      `json:"parentRegionId,omitempty"`
	Center         *coordinate `json:"center,omitempty"`
	Features       []geometry  `json:"features"`
}

var errMapNotReady = errors.New("map is not ready for embedding")

func main() {
	source := flag.String("source", "", "path containing fmg-archive and z13-stitched-maps")
	output := flag.String("output", "", "catalog JSON destination")
	flag.Parse()

	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "generate: -source and -output are required")
		os.Exit(2)
	}
	if err := run(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run(source, output string) error {
	archiveRoot := filepath.Join(source, "fmg-archive")
	var index archive
	if err := readJSON(filepath.Join(archiveRoot, "archive.json"), &index); err != nil {
		return err
	}

	out := catalog{
		Source:   "FMG archive",
		TileGrid: tileGrid{Zoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
	}
	imageRoot := filepath.Join(filepath.Dir(output), "maps")
	iconRoot := filepath.Join(filepath.Dir(output), "icons")
	for _, gameRef := range index.Games {
		game, err := buildGame(archiveRoot, imageRoot, iconRoot, gameRef)
		if err != nil {
			return fmt.Errorf("%s: %w", gameRef.Title, err)
		}
		if len(game.Maps) > 0 {
			out.Games = append(out.Games, game)
		}
	}

	sort.Slice(out.Games, func(i, j int) bool { return out.Games[i].Title < out.Games[j].Title })
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	return nil
}

func buildGame(archiveRoot, imageRoot, iconRoot string, ref archiveGame) (catalogGame, error) {
	gamePath := filepath.Join(archiveRoot, ref.Directory)
	mapDirs, err := filepath.Glob(filepath.Join(gamePath, "maps", "*"))
	if err != nil {
		return catalogGame{}, err
	}
	game := catalogGame{ID: ref.ID, Title: ref.Title}
	for _, mapDir := range mapDirs {
		info, err := os.Stat(mapDir)
		if err != nil || !info.IsDir() {
			continue
		}
		m, gameSlug, err := buildMap(mapDir, imageRoot)
		if err != nil {
			if errors.Is(err, errMapNotReady) {
				continue
			}
			return catalogGame{}, err
		}
		if game.Slug == "" {
			game.Slug = gameSlug
		}
		game.Maps = append(game.Maps, m)
	}
	sort.Slice(game.Maps, func(i, j int) bool { return game.Maps[i].Title < game.Maps[j].Title })
	if err := attachGameIcons(gamePath, iconRoot, &game); err != nil {
		return catalogGame{}, err
	}
	return game, nil
}

func buildMap(mapDir, imageRoot string) (catalogMap, string, error) {
	var snapshots []snapshotIndex
	indexPath := filepath.Join(mapDir, "snapshots", "index.json")
	if err := readJSON(indexPath, &snapshots); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return catalogMap{}, "", fmt.Errorf("%w: snapshot index is missing", errMapNotReady)
		}
		return catalogMap{}, "", err
	}
	if len(snapshots) == 0 {
		return catalogMap{}, "", fmt.Errorf("%w: snapshot index is empty", errMapNotReady)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })
	latest := snapshots[len(snapshots)-1]

	var raw rawMap
	snapshotPath := filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json")
	if err := readJSON(snapshotPath, &raw); err != nil {
		return catalogMap{}, "", err
	}

	m := catalogMap{
		ID:        raw.ID,
		Title:     raw.Title,
		Slug:      raw.Slug,
		Center:    coordinate{Latitude: raw.InitialLatitude, Longitude: raw.InitialLongitude},
		UpdatedAt: latest.CapturedAt,
	}
	boundsByPath, err := tileContentBounds(mapDir)
	if err != nil {
		return catalogMap{}, "", err
	}
	if len(raw.Config.TileSets) == 0 {
		m.Variants = []variant{{
			Name:  "Default",
			Image: imageName(raw.Game.Slug, raw.Slug, ""),
		}}
	} else {
		for _, set := range raw.Config.TileSets {
			suffix := ""
			if len(raw.Config.TileSets) > 1 {
				suffix = slugify(set.Name)
			}
			m.Variants = append(m.Variants, variant{
				Name:   set.Name,
				Image:  imageName(raw.Game.Slug, raw.Slug, suffix),
				Bounds: boundsByPath[set.Path],
			})
		}
	}
	for _, variant := range m.Variants {
		info, err := os.Stat(filepath.Join(imageRoot, variant.Image))
		if errors.Is(err, os.ErrNotExist) {
			return catalogMap{}, "", fmt.Errorf("%w: image %s is missing", errMapNotReady, variant.Image)
		}
		if err != nil {
			return catalogMap{}, "", err
		}
		if !info.Mode().IsRegular() {
			return catalogMap{}, "", fmt.Errorf("%w: image %s is not a file", errMapNotReady, variant.Image)
		}
	}
	for _, rawGroup := range raw.Groups {
		group := catalogGroup{ID: rawGroup.ID, Title: rawGroup.Title}
		for _, rawCategory := range rawGroup.Categories {
			category := catalogCategory{
				ID:          rawCategory.ID,
				Title:       rawCategory.Title,
				Icon:        rawCategory.Icon,
				Color:       resolvedCategoryColor(rawGroup, rawCategory),
				IconColor:   resolvedIconColor(rawGroup, rawCategory),
				DisplayType: rawCategory.DisplayType,
				Visible:     rawCategory.Visible,
			}
			for _, rawLocation := range rawCategory.Locations {
				lat, err := number(rawLocation.Latitude)
				if err != nil {
					return catalogMap{}, "", fmt.Errorf("location %d latitude: %w", rawLocation.ID, err)
				}
				lng, err := number(rawLocation.Longitude)
				if err != nil {
					return catalogMap{}, "", fmt.Errorf("location %d longitude: %w", rawLocation.ID, err)
				}
				category.Locations = append(category.Locations, catalogLocation{
					ID:          rawLocation.ID,
					Title:       rawLocation.Title,
					Description: rawLocation.Description,
					Latitude:    lat,
					Longitude:   lng,
					RegionID:    rawLocation.RegionID,
				})
				m.PinCount++
			}
			group.Categories = append(group.Categories, category)
		}
		m.Groups = append(m.Groups, group)
	}
	for _, rawRegion := range raw.Regions {
		z := zone{
			ID:             rawRegion.ID,
			Title:          rawRegion.Title,
			Subtitle:       rawRegion.Subtitle,
			ParentRegionID: rawRegion.ParentRegionID,
		}
		centerX, hasX, err := optionalNumber(rawRegion.CenterX)
		if err != nil {
			return catalogMap{}, "", fmt.Errorf("region %d center_x: %w", rawRegion.ID, err)
		}
		centerY, hasY, err := optionalNumber(rawRegion.CenterY)
		if err != nil {
			return catalogMap{}, "", fmt.Errorf("region %d center_y: %w", rawRegion.ID, err)
		}
		if hasX && hasY {
			z.Center = &coordinate{Latitude: centerY, Longitude: centerX}
		}
		for _, feature := range rawRegion.Features {
			if feature.Geometry.Type != "" && len(feature.Geometry.Coordinates) > 0 {
				z.Features = append(z.Features, feature.Geometry)
			}
		}
		if len(z.Features) > 0 {
			m.Zones = append(m.Zones, z)
		}
	}
	return m, raw.Game.Slug, nil
}

func attachGameIcons(gamePath, iconRoot string, game *catalogGame) error {
	if game.Slug == "" {
		return nil
	}
	copied := make(map[string]string)
	for mapIndex := range game.Maps {
		for groupIndex := range game.Maps[mapIndex].Groups {
			categories := game.Maps[mapIndex].Groups[groupIndex].Categories
			for categoryIndex := range categories {
				category := &categories[categoryIndex]
				if !validIconKey(category.Icon) {
					continue
				}
				asset, found := copied[category.Icon]
				if !found {
					var err error
					asset, err = copyGameIcon(gamePath, iconRoot, game.Slug, category.Icon)
					if err != nil {
						return err
					}
					copied[category.Icon] = asset
				}
				category.IconAsset = asset
			}
		}
	}
	return nil
}

func copyGameIcon(gamePath, iconRoot, gameSlug, icon string) (string, error) {
	source := filepath.Join(gamePath, "icons", icon+".svg")
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read icon %s: %w", source, err)
	}
	asset := filepath.ToSlash(filepath.Join(gameSlug, icon+".svg"))
	destination := filepath.Join(iconRoot, filepath.FromSlash(asset))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("create icon directory: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return "", fmt.Errorf("write icon %s: %w", destination, err)
	}
	return asset, nil
}

func validIconKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func resolvedCategoryColor(group rawGroup, category rawCategory) string {
	if color := normalizeHexColor(category.Color); color != "" {
		return color
	}
	return normalizeHexColor(group.Color)
}

func resolvedIconColor(group rawGroup, category rawCategory) string {
	if color := normalizeHexColor(category.IconColor); color != "" {
		return color
	}
	return normalizeHexColor(group.IconColor)
}

func normalizeHexColor(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	switch len(value) {
	case 3, 4, 6, 8:
	default:
		return ""
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return ""
	}
	return "#" + strings.ToUpper(value)
}

func tileContentBounds(mapDir string) (map[string]*contentBounds, error) {
	const (
		zoom      = 13
		firstTile = 4064
		tileSize  = 256
		gridSize  = 8192
	)

	indexPath := filepath.Join(mapDir, "tiles", "index.json")
	var records []tileRecord
	if err := readJSON(indexPath, &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	byPath := make(map[string][]tileRecord)
	for _, record := range records {
		if record.Zoom != zoom || record.ContentHash == "" {
			continue
		}
		path := tileSetPath(record.URL, zoom)
		if path != "" {
			byPath[path] = append(byPath[path], record)
		}
	}

	result := make(map[string]*contentBounds)
	for path, tiles := range byPath {
		counts := make(map[string]int)
		var placeholder string
		minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
		maxX, maxY := -1, -1
		for _, tile := range tiles {
			counts[tile.ContentHash]++
			if counts[tile.ContentHash] > counts[placeholder] {
				placeholder = tile.ContentHash
			}
			minX = min(minX, tile.X)
			minY = min(minY, tile.Y)
			maxX = max(maxX, tile.X)
			maxY = max(maxY, tile.Y)
		}
		if counts[placeholder] > len(tiles)/2 {
			minX, minY = int(^uint(0)>>1), int(^uint(0)>>1)
			maxX, maxY = -1, -1
			for _, tile := range tiles {
				if tile.ContentHash == placeholder {
					continue
				}
				minX = min(minX, tile.X)
				minY = min(minY, tile.Y)
				maxX = max(maxX, tile.X)
				maxY = max(maxY, tile.Y)
			}
		}
		if maxX < minX || maxY < minY {
			continue
		}
		if minX == firstTile && minY == firstTile &&
			(maxX-minX+1)*tileSize == gridSize &&
			(maxY-minY+1)*tileSize == gridSize {
			continue
		}
		result[path] = &contentBounds{
			X:      (minX - firstTile) * tileSize,
			Y:      (minY - firstTile) * tileSize,
			Width:  (maxX - minX + 1) * tileSize,
			Height: (maxY - minY + 1) * tileSize,
		}
	}
	return result, nil
}

func tileSetPath(rawURL string, zoom int) string {
	const marker = "/games/"
	start := strings.Index(rawURL, marker)
	if start < 0 {
		return ""
	}
	path := rawURL[start+len(marker):]
	end := strings.Index(path, "/"+strconv.Itoa(zoom)+"/")
	if end < 0 {
		return ""
	}
	return path[:end]
}

func imageName(game, mapSlug, suffix string) string {
	name := game + "__" + mapSlug
	if suffix != "" {
		name += "__" + suffix
	}
	return name + "__z13.png"
}

func slugify(value string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, value), "-")
}

func number(raw json.RawMessage) (float64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		value = text
	}
	return strconv.ParseFloat(value, 64)
}

func optionalNumber(raw json.RawMessage) (float64, bool, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false, nil
	}
	n, err := number(raw)
	return n, err == nil, err
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
