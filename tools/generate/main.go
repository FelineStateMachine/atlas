// Command generate turns an FMG archive and the generated raster manifest into
// the compact catalog consumed by Atlas. The source archive stays outside the
// Go module; only browser-ready data is embedded in the application.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
	TileSize   int `json:"tileSize"`
	Size       int `json:"size"`
}

type catalogGame struct {
	ID    int64        `json:"id"`
	Title string       `json:"title"`
	Slug  string       `json:"slug"`
	Maps  []catalogMap `json:"maps"`
}

type catalogMap struct {
	ID         int64          `json:"id"`
	Title      string         `json:"title"`
	Slug       string         `json:"slug"`
	IconOutset string         `json:"iconOutset,omitempty"`
	Center     coordinate     `json:"center"`
	Variants   []variant      `json:"variants"`
	Groups     []catalogGroup `json:"groups"`
	Zones      []zone         `json:"zones,omitempty"`
	// Parent names the map this one was split out of, so an inset sorts with
	// the sheet it came from rather than alphabetically among unrelated maps.
	Parent    string `json:"parent,omitempty"`
	PinCount  int    `json:"pinCount"`
	UpdatedAt string `json:"updatedAt"`
}

type coordinate struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

type variant struct {
	Name       string         `json:"name"`
	Tiles      string         `json:"tiles"`
	MinZoom    int            `json:"minZoom"`
	MaxZoom    int            `json:"maxZoom"`
	FullZoom   int            `json:"fullZoom"`
	SourceZoom int            `json:"sourceZoom"`
	Formats    []string       `json:"formats"`
	Bounds     *contentBounds `json:"bounds,omitempty"`
	// Surface is the ground the map covers, where Bounds is the window cut from
	// the tile pyramid to draw it. On a piece of a sheet the window is grown to
	// take in the title drawn beside it, so anything dividing the map up -- the
	// geohash grid -- measures the surface instead and leaves no cell on margin.
	Surface     *contentBounds            `json:"surface,omitempty"`
	Interpolate bool                      `json:"interpolate"`
	Background  string                    `json:"background,omitempty"`
	Shard       int64                     `json:"shard,omitempty"`
	Coverage    map[string]*levelCoverage `json:"coverage,omitempty"`
}

type levelCoverage struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Bits string `json:"bits"`
}

type contentBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type tileManifest struct {
	TileSize int                   `json:"tileSize"`
	Size     int                   `json:"size"`
	Variants []tileVariantManifest `json:"variants"`
}

type tileVariantManifest struct {
	SourcePath  string                    `json:"sourcePath"`
	AssetPath   string                    `json:"assetPath"`
	MinZoom     int                       `json:"minZoom"`
	MaxZoom     int                       `json:"maxZoom"`
	FullZoom    int                       `json:"fullZoom"`
	SourceZoom  int                       `json:"sourceZoom"`
	Formats     []string                  `json:"formats"`
	Bounds      *contentBounds            `json:"bounds"`
	Interpolate bool                      `json:"interpolate"`
	Background  string                    `json:"background"`
	Coverage    map[string]*levelCoverage `json:"coverage"`
}

type catalogGroup struct {
	ID         int64             `json:"id"`
	Title      string            `json:"title"`
	Categories []catalogCategory `json:"categories"`
}

type catalogCategory struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`
	IconAsset string `json:"iconAsset,omitempty"`
	// IconPicture marks an icon that already carries its own colours, as
	// against a monochrome glyph whose silhouette is tinted with the category
	// colour. Sliced marker sprites are pictures; icon-font glyphs are not.
	IconPicture bool              `json:"iconPicture,omitempty"`
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
	// Shard names the layer this location belongs to on a map split into
	// layers, and is absent on maps that are not.
	Shard int64         `json:"shard,omitempty"`
	Links []catalogLink `json:"links,omitempty"`
}

// catalogLink is a cross-reference the source wrote as a mapgenie URL, resolved
// to a location in this same map. Atlas is offline, so the URL itself is
// dropped and only the in-catalog target survives.
type catalogLink struct {
	Title      string `json:"title"`
	LocationID int64  `json:"locationId"`
}

type zone struct {
	ID             int64       `json:"id"`
	Title          string      `json:"title"`
	Subtitle       string      `json:"subtitle,omitempty"`
	ParentRegionID *int64      `json:"parentRegionId,omitempty"`
	Center         *coordinate `json:"center,omitempty"`
	Shard          int64       `json:"shard,omitempty"`
	Features       []geometry  `json:"features"`
}

var errMapNotReady = errors.New("map is not ready for embedding")

// preferredMapOrder keeps a game's primary map ahead of secondary areas.
// Unlisted maps follow in title order, so adding one entry is enough to curate
// a game without restating its complete archive.
var preferredMapOrder = map[string][]string{
	"fallout-new-vegas": {"mojave-wasteland"},
}

// The raster beneath an icon decides which outline is legible, so this is
// declared rather than derived. A game whose maps are all drawn the same way
// says so once; a single map that differs from its game overrides it.
var iconOutsetByGame = map[string]string{
	"clair-obscur-expedition-33": "dark",
	"fallout-new-vegas":          "dark", // pale Pip-Boy rasters throughout
	"fallout76":                  "dark", // a parchment survey map, pale throughout
	"la-noire":                   "dark",
	"sonic-frontiers":            "dark",
}

var iconOutsetByMap = map[int64]string{
	3:  "dark", // Skyrim
	18: "dark", // Solstheim
}

func iconOutsetFor(raw rawMap) string {
	if outset, ok := iconOutsetByMap[raw.ID]; ok {
		return outset
	}
	return iconOutsetByGame[raw.Game.Slug]
}

func main() {
	source := flag.String("source", "", "path containing fmg-archive")
	tiles := flag.String("tiles", "", "generated tile manifest")
	output := flag.String("output", "", "catalog JSON destination")
	flag.Parse()

	if *source == "" || *tiles == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "generate: -source, -tiles, and -output are required")
		os.Exit(2)
	}
	if err := run(*source, *tiles, *output); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run(source, tileManifestPath, output string) error {
	archiveRoot := filepath.Join(source, "fmg-archive")
	var index archive
	if err := readJSON(filepath.Join(archiveRoot, "archive.json"), &index); err != nil {
		return err
	}
	var tiles tileManifest
	if err := readJSON(tileManifestPath, &tiles); err != nil {
		return err
	}
	tilesByPath := make(map[string]tileVariantManifest, len(tiles.Variants))
	for _, variant := range tiles.Variants {
		tilesByPath[variant.SourcePath] = variant
	}

	out := catalog{
		Source: "FMG archive",
		TileGrid: tileGrid{
			SourceZoom: 13,
			FirstTile:  4064,
			TileSize:   tiles.TileSize,
			Size:       tiles.Size,
		},
	}
	iconRoot := filepath.Join(filepath.Dir(output), "icons")
	for _, gameRef := range index.Games {
		game, err := buildGame(archiveRoot, iconRoot, tilesByPath, gameRef, out.TileGrid)
		if err != nil {
			return fmt.Errorf("%s: %w", gameRef.Title, err)
		}
		if len(game.Maps) > 0 {
			out.Games = append(out.Games, game)
		}
	}

	sort.Slice(out.Games, func(i, j int) bool { return out.Games[i].Title < out.Games[j].Title })
	return writeCatalog(out, output)
}

func buildGame(
	archiveRoot string,
	iconRoot string,
	tilesByPath map[string]tileVariantManifest,
	ref archiveGame,
	grid tileGrid,
) (catalogGame, error) {
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
		pieces, gameSlug, err := buildMap(mapDir, tilesByPath, grid)
		if err != nil {
			if errors.Is(err, errMapNotReady) {
				continue
			}
			return catalogGame{}, err
		}
		if game.Slug == "" {
			game.Slug = gameSlug
		}
		game.Maps = append(game.Maps, pieces...)
	}
	sortGameMaps(game.Slug, game.Maps)
	if err := attachGameIcons(gamePath, iconRoot, &game); err != nil {
		return catalogGame{}, err
	}
	return game, nil
}

func sortGameMaps(gameSlug string, maps []catalogMap) {
	order := make(map[string]int)
	for index, slug := range preferredMapOrder[gameSlug] {
		order[slug] = index
	}
	titles := make(map[string]string, len(maps))
	for _, m := range maps {
		titles[m.Slug] = m.Title
	}
	// Maps sort as families: an inset carries its parent's position and follows
	// it, so a split sheet stays together in the picker.
	family := func(m catalogMap) (string, string) {
		if m.Parent == "" {
			return m.Slug, m.Title
		}
		return m.Parent, titles[m.Parent]
	}
	sort.SliceStable(maps, func(i, j int) bool {
		leftSlug, leftTitle := family(maps[i])
		rightSlug, rightTitle := family(maps[j])
		left, leftPreferred := order[leftSlug]
		right, rightPreferred := order[rightSlug]
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		if leftPreferred && left != right {
			return left < right
		}
		if leftTitle != rightTitle {
			return leftTitle < rightTitle
		}
		if (maps[i].Parent == "") != (maps[j].Parent == "") {
			return maps[i].Parent == ""
		}
		return maps[i].Title < maps[j].Title
	})
}

// buildMap returns one entry per map, or several when a sheet holding separate
// places is declared for splitting.
func buildMap(
	mapDir string,
	tilesByPath map[string]tileVariantManifest,
	grid tileGrid,
) ([]catalogMap, string, error) {
	var snapshots []snapshotIndex
	indexPath := filepath.Join(mapDir, "snapshots", "index.json")
	if err := readJSON(indexPath, &snapshots); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("%w: snapshot index is missing", errMapNotReady)
		}
		return nil, "", err
	}
	if len(snapshots) == 0 {
		return nil, "", fmt.Errorf("%w: snapshot index is empty", errMapNotReady)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })
	latest := snapshots[len(snapshots)-1]

	var raw rawMap
	snapshotPath := filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json")
	if err := readJSON(snapshotPath, &raw); err != nil {
		return nil, "", err
	}

	m := catalogMap{
		ID:         raw.ID,
		Title:      raw.Title,
		Slug:       raw.Slug,
		IconOutset: iconOutsetFor(raw),
		Center:     coordinate{Latitude: raw.InitialLatitude, Longitude: raw.InitialLongitude},
		UpdatedAt:  latest.CapturedAt,
	}
	if len(raw.Config.TileSets) == 0 {
		return nil, "", fmt.Errorf("%w: no tile sets", errMapNotReady)
	}
	for _, set := range raw.Config.TileSets {
		tiles, ok := tilesByPath[set.Path]
		if !ok {
			return nil, "", fmt.Errorf("%w: tile layer %s is missing", errMapNotReady, set.Path)
		}
		m.Variants = append(m.Variants, variant{
			Name:        set.Name,
			Tiles:       tiles.AssetPath,
			MinZoom:     tiles.MinZoom,
			MaxZoom:     tiles.MaxZoom,
			FullZoom:    tiles.FullZoom,
			SourceZoom:  tiles.SourceZoom,
			Formats:     tiles.Formats,
			Bounds:      tiles.Bounds,
			Interpolate: tiles.Interpolate,
			Background:  tiles.Background,
			Coverage:    tiles.Coverage,
		})
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
					return nil, "", fmt.Errorf("location %d latitude: %w", rawLocation.ID, err)
				}
				lng, err := number(rawLocation.Longitude)
				if err != nil {
					return nil, "", fmt.Errorf("location %d longitude: %w", rawLocation.ID, err)
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
			return nil, "", fmt.Errorf("region %d center_x: %w", rawRegion.ID, err)
		}
		centerY, hasY, err := optionalNumber(rawRegion.CenterY)
		if err != nil {
			return nil, "", fmt.Errorf("region %d center_y: %w", rawRegion.ID, err)
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
	resolveDescriptionLinks(&m)
	pieces, err := splitMap(m, raw, grid)
	if err != nil {
		return nil, "", err
	}
	for index := range pieces {
		markSurfaces(&pieces[index], grid)
	}
	return pieces, raw.Game.Slug, nil
}

// Labels may themselves contain a bracketed aside, as in
// "[Oh Baby! [Super Sledge]](url)", so one level of nesting is allowed.
var markdownLink = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\[\]]*\])*)\]\(([^)\s]+)[^)]*\)`)
var mapgenieLocation = regexp.MustCompile(`locationIds=(\d+)`)

// Some descriptions carry malformed markdown whose URL never sat inside a link,
// e.g. "[Boss] (Lv. 55) (https://…)". Nothing may ship a live URL, so any that
// survive link rewriting are removed outright.
var bareURL = regexp.MustCompile(`\s*\(?\s*https?://[^\s)]+\)?`)

// resolveDescriptionLinks strips every external URL out of location
// descriptions. Atlas ships with no network, so a mapgenie or YouTube link is
// dead weight at best. Where the link pointed at another location in this same
// map, the target is kept as a structured cross-reference the viewer can
// navigate to instead.
func resolveDescriptionLinks(m *catalogMap) {
	known := make(map[int64]bool)
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				known[location.ID] = true
			}
		}
	}
	for groupIndex := range m.Groups {
		categories := m.Groups[groupIndex].Categories
		for categoryIndex := range categories {
			locations := categories[categoryIndex].Locations
			for locationIndex := range locations {
				location := &locations[locationIndex]
				if !strings.Contains(location.Description, "http") {
					continue
				}
				location.Description = markdownLink.ReplaceAllStringFunc(
					location.Description,
					func(match string) string {
						parts := markdownLink.FindStringSubmatch(match)
						label, target := parts[1], parts[2]
						id := mapgenieLocation.FindStringSubmatch(target)
						if id != nil && !strings.HasPrefix(label, "!") {
							if value, err := strconv.ParseInt(id[1], 10, 64); err == nil &&
								known[value] && value != location.ID {
								location.Links = append(location.Links, catalogLink{
									Title:      label,
									LocationID: value,
								})
							}
						}
						return label
					},
				)
				location.Description = strings.TrimSpace(bareURL.ReplaceAllString(location.Description, ""))
			}
		}
	}
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
				category.IconPicture = strings.HasSuffix(asset, ".png")
			}
		}
	}
	return nil
}

// copyGameIcon takes whichever form the archive holds. Most games publish an
// icon font that renders to SVG; some publish a marker strip instead, which
// slices into PNG.
func copyGameIcon(gamePath, iconRoot, gameSlug, icon string) (string, error) {
	var data []byte
	var extension string
	for _, candidate := range []string{".svg", ".png"} {
		source := filepath.Join(gamePath, "icons", icon+candidate)
		contents, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read icon %s: %w", source, err)
		}
		data, extension = contents, candidate
		break
	}
	if extension == "" {
		return "", nil
	}
	asset := filepath.ToSlash(filepath.Join(gameSlug, icon+extension))
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
