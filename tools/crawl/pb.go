// The Piggyback mode fills the archive from maps.piggyback.com. The page
// carries its settings and its display names; the categories and pins come
// from two open API requests under the map's own address; the tiles are
// surveyed level by level, because Piggyback publishes no bounds -- the
// crawler's own account of where tiles exist is what the capture carries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/mgdoc"
	"github.com/FelineStateMachine/atlas/internal/pbmap"
)

// pbPage is what the crawl needs from the page's embedded state: the tile
// server and zoom range, and the language bundle's names for category and
// type keys, which the data API never repeats.
type pbPage struct {
	tileServer     string
	minZoom        int
	maxZoom        int
	premiumMaxZoom int
	categoryLabels map[string]string
	typeLabels     map[string]string
}

// pbTransform is the transformation Piggyback's Cyberpunk map projects
// through: game coordinates spanning ±8192 onto the unit tile at zoom zero.
// It lives in the map's scripts rather than its data, so the crawler asserts
// it instead of reading it; the capture records what was assumed, and a game
// drawn to a different rule would need this looked up in its own scripts.
var pbTransform = pbmap.Transform{A: 0.015625, B: 128, C: -0.015625, D: 128}

func runPB(ctx context.Context, fetcher *fetcher, o options) error {
	gameSlug, mapSlug, ok := strings.Cut(o.piggyback, "/")
	if !ok || gameSlug == "" || mapSlug == "" {
		return errors.New("-piggyback wants gameSlug/mapSlug, as the address bar spells it")
	}
	pageURL := fmt.Sprintf("https://maps.piggyback.com/%s/maps/%s", gameSlug, mapSlug)

	page, err := fetchPBPage(ctx, fetcher, pageURL)
	if err != nil {
		return err
	}
	categories, err := fetchPBCategories(ctx, fetcher, pageURL)
	if err != nil {
		return err
	}
	pins, err := fetchPBPins(ctx, fetcher, pageURL)
	if err != nil {
		return err
	}

	capture := composePBCapture(gameSlug, mapSlug, page, categories, pins)
	scope, err := pbmap.TileSetPath(page.tileServer)
	if err != nil {
		return err
	}

	gameID := pbArchiveID("pb:" + gameSlug)
	mapID := pbArchiveID("pb:" + gameSlug + "/" + mapSlug)
	game := &apiGameFull{ID: gameID, Title: capture.GameTitle,
		Config: apiGameConfig{URL: pageURL}}
	full := &apiMapFull{ID: mapID, Title: capture.MapTitle, Slug: mapSlug}
	// The directory says where the capture came from even though the game
	// keeps its plain title: other sources' captures of the same game live in
	// their own directories, and they must never resolve to one.
	gameDirectory := resolveGameDirectory(o.archive,
		&apiGameFull{ID: gameID, Title: "Piggyback " + capture.GameTitle})
	mapDirectory := resolveMapDirectory(o.archive, gameDirectory, full)
	mapDir := filepath.Join(o.archive, mapDirectory)

	fmt.Printf("\n== Piggyback %s / %s (%d pins, %d categories)\n",
		capture.GameTitle, capture.MapTitle, len(capture.Pins), len(capture.Categories))

	if o.dryRun {
		fmt.Printf("   would survey tiles z%d-%d and write the capture to %s\n",
			page.minZoom, pbDeepest(page, o), mapDir)
		return nil
	}

	index, err := readTileIndex(mapDir)
	if err != nil {
		return err
	}
	index.mapID = mapID
	stats, levels, err := capturePBTiles(ctx, fetcher, o, page, pageURL, scope, mapDir, index)
	if err != nil {
		return err
	}
	capture.Levels = levels

	raw, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	// A capture that does not translate would poison the archive for both
	// tools that read it, so it is refused here, while the problem is one
	// crawl old rather than buried in history.
	if _, err := pbmap.Translate(raw); err != nil {
		return fmt.Errorf("capture does not translate: %w", err)
	}
	if err := writeSnapshotRecord(mapDir, raw, pbmap.Kind, mapID, pageURL); err != nil {
		return err
	}
	if err := writeTileIndex(mapDir, index); err != nil {
		return err
	}
	if err := writeMapMetadata(mapDir, game, full, index, len(capture.Pins)); err != nil {
		return err
	}
	if err := registerMap(o.archive, game, full, gameDirectory, mapDirectory, "piggyback"); err != nil {
		return err
	}
	fmt.Printf("   %d fetched · %d held · %d not published · %d failed · %.1f MB new\n",
		stats.fetched, stats.skipped, stats.absent, stats.failed, float64(stats.bytes)/1e6)
	return nil
}

func fetchPBPage(ctx context.Context, f *fetcher, pageURL string) (*pbPage, error) {
	body, _, err := f.get(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	const openTag = `<script id="__NEXT_DATA__" type="application/json">`
	_, rest, found := strings.Cut(string(body), openTag)
	if !found {
		return nil, fmt.Errorf("%s embeds no page state", pageURL)
	}
	doc, _, found := strings.Cut(rest, "</script>")
	if !found {
		return nil, fmt.Errorf("%s embeds unterminated page state", pageURL)
	}
	var state struct {
		Props struct {
			PageProps struct {
				Settings struct {
					TileServerURL  string `json:"tileServerUrl"`
					MinZoom        string `json:"minZoom"`
					MaxZoom        string `json:"maxZoom"`
					MaxZoomPremium string `json:"maxZoomPremium"`
				} `json:"settings"`
				I18N struct {
					Store map[string]struct {
						Common struct {
							Category map[string]string `json:"category"`
							Type     map[string]string `json:"type"`
						} `json:"common"`
					} `json:"initialI18nStore"`
				} `json:"_nextI18Next"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(doc), &state); err != nil {
		return nil, fmt.Errorf("decode %s page state: %w", pageURL, err)
	}
	settings := state.Props.PageProps.Settings
	if settings.TileServerURL == "" {
		return nil, fmt.Errorf("%s describes no map", pageURL)
	}
	page := &pbPage{
		tileServer:     settings.TileServerURL,
		minZoom:        pbAtoi(settings.MinZoom, 0),
		maxZoom:        pbAtoi(settings.MaxZoom, 0),
		premiumMaxZoom: pbAtoi(settings.MaxZoomPremium, 0),
		categoryLabels: map[string]string{},
		typeLabels:     map[string]string{},
	}
	if page.maxZoom <= 0 {
		return nil, fmt.Errorf("%s declares no zoom range", pageURL)
	}
	// English labels when the page carries them, any language over none.
	store := state.Props.PageProps.I18N.Store
	languages := []string{"en"}
	for language := range store {
		languages = append(languages, language)
	}
	for _, language := range languages {
		bundle, ok := store[language]
		if !ok {
			continue
		}
		for key, label := range bundle.Common.Category {
			if _, taken := page.categoryLabels[key]; !taken {
				page.categoryLabels[key] = label
			}
		}
		for key, label := range bundle.Common.Type {
			if _, taken := page.typeLabels[key]; !taken {
				page.typeLabels[key] = label
			}
		}
	}
	return page, nil
}

// The two data requests every visitor's map makes, under the map's own path.
type pbAPICategory struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Position float64 `json:"position"`
	Types    []struct {
		ID       string  `json:"id"`
		Key      string  `json:"key"`
		Position float64 `json:"position"`
	} `json:"types"`
}

type pbAPIPin struct {
	ID       string `json:"id"`
	X        string `json:"x"`
	Y        string `json:"y"`
	Category struct {
		Key string `json:"key"`
	} `json:"category"`
	Type struct {
		Key string `json:"key"`
	} `json:"type"`
	Data []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"data"`
}

func fetchPBCategories(ctx context.Context, f *fetcher, pageURL string) ([]pbAPICategory, error) {
	var out []pbAPICategory
	if err := fetchPBResult(ctx, f, pageURL+"/api/trpc/category.getCategory", &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("the map declares no categories")
	}
	return out, nil
}

func fetchPBPins(ctx context.Context, f *fetcher, pageURL string) ([]pbAPIPin, error) {
	var out []pbAPIPin
	if err := fetchPBResult(ctx, f, pageURL+"/api/trpc/pin.getPins", &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("the map answered with no pins at all")
	}
	return out, nil
}

func fetchPBResult(ctx context.Context, f *fetcher, url string, into any) error {
	body, _, err := f.get(ctx, url)
	if err != nil {
		return err
	}
	var envelope struct {
		Result struct {
			Data struct {
				JSON json.RawMessage `json:"json"`
			} `json:"data"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	if len(envelope.Error) > 0 {
		return fmt.Errorf("%s answered: %s", url, envelope.Error)
	}
	if err := json.Unmarshal(envelope.Result.Data.JSON, into); err != nil {
		return fmt.Errorf("decode %s payload: %w", url, err)
	}
	return nil
}

func composePBCapture(
	gameSlug, mapSlug string,
	page *pbPage,
	categories []pbAPICategory,
	pins []pbAPIPin,
) *pbmap.Capture {
	capture := &pbmap.Capture{
		Source:   pbmap.Source,
		GameSlug: gameSlug,
		MapSlug:  mapSlug,
		// The page's language bundle names categories and types, never the
		// game or the map themselves; their slugs, spelled out, are all the
		// naming there is.
		GameTitle: mgdoc.SpellOut(gameSlug),
		MapTitle:  mgdoc.SpellOut(mapSlug),
	}
	capture.Map = pbmap.MapConfig{
		TileServer:     page.tileServer,
		MinZoom:        page.minZoom,
		MaxZoom:        page.maxZoom,
		PremiumMaxZoom: page.premiumMaxZoom,
		Transform:      pbTransform,
	}
	for _, category := range categories {
		entry := pbmap.Category{ID: category.ID, Key: category.Key, Position: category.Position}
		for _, t := range category.Types {
			entry.Types = append(entry.Types, pbmap.Type{ID: t.ID, Key: t.Key, Position: t.Position})
		}
		capture.Categories = append(capture.Categories, entry)
	}
	for _, pin := range pins {
		entry := pbmap.Pin{
			ID:          pin.ID,
			X:           pin.X,
			Y:           pin.Y,
			CategoryKey: pin.Category.Key,
			TypeKey:     pin.Type.Key,
		}
		// One entry is the rule; a pin stacking several keeps them all, the
		// extras folded into the description rather than dropped.
		if len(pin.Data) > 0 {
			entry.Name = pin.Data[0].Name
			entry.Description = pin.Data[0].Description
			for _, extra := range pin.Data[1:] {
				addition := strings.TrimSpace(extra.Name + " — " + extra.Description)
				entry.Description = strings.TrimSpace(entry.Description + "\n\n" + addition)
			}
		}
		capture.Pins = append(capture.Pins, entry)
	}
	// Labels for every key the data actually uses, from the page's bundle.
	seenCategories := map[string]bool{}
	seenTypes := map[string]bool{}
	for _, category := range capture.Categories {
		seenCategories[category.Key] = true
		for _, t := range category.Types {
			seenTypes[t.Key] = true
		}
	}
	for _, pin := range capture.Pins {
		seenCategories[pin.CategoryKey] = true
		seenTypes[pin.TypeKey] = true
	}
	for key := range seenCategories {
		if label := page.categoryLabels[key]; label != "" {
			capture.Labels.Categories = append(capture.Labels.Categories, pbmap.Label{Key: key, Label: label})
		}
	}
	for key := range seenTypes {
		if label := page.typeLabels[key]; label != "" {
			capture.Labels.Types = append(capture.Labels.Types, pbmap.Label{Key: key, Label: label})
		}
	}
	capture.Normalize()
	return capture
}

func pbDeepest(page *pbPage, o options) int {
	deepest := page.maxZoom
	if o.maxZoom > 0 {
		// An explicit ask may pass the free ceiling, up to what exists at all.
		limit := page.premiumMaxZoom
		if limit <= 0 {
			limit = page.maxZoom
		}
		deepest = min(o.maxZoom, limit)
	}
	return deepest
}

// capturePBTiles surveys and captures the pyramid in one walk. The first
// published level is asked for whole; every deeper level asks only after the
// children of tiles that exist, which is what bounds the walk over a mostly
// empty world square. What each level turns out to hold becomes the capture's
// account of the pyramid.
func capturePBTiles(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	page *pbPage,
	pageURL, scope, mapDir string,
	index *tileIndex,
) (captureStats, []pbmap.Level, error) {
	var stats captureStats
	var levels []pbmap.Level
	setID := index.tileSetID(scope)
	extension := pbmap.TileExtension(page.tileServer)
	deepest := pbDeepest(page, o)
	referer := pageURL + "/"
	fmt.Printf("   layer %q z%d-%d\n", scope, page.minZoom, deepest)

	var present map[[2]int]bool
	for zoom := page.minZoom; zoom <= deepest; zoom++ {
		across := 1 << zoom
		window := tileWindow{minX: 0, minY: 0, maxX: across - 1, maxY: across - 1}
		var wanted [][2]int
		if present == nil {
			wanted = window.tiles()
		} else {
			wanted = childrenOf(present, window)
		}
		if len(wanted) == 0 {
			break
		}
		results, err := fetchTemplateLevel(ctx, fetcher, o, page.tileServer, referer,
			setID, extension, mapDir, zoom, wanted, index)
		if err != nil {
			return stats, nil, err
		}
		stats.merge(results)

		present = make(map[[2]int]bool, len(results.hashes))
		level := pbmap.Level{Zoom: zoom, MinX: across, MinY: across, MaxX: -1, MaxY: -1}
		for coordinate := range results.hashes {
			present[coordinate] = true
			level.MinX = min(level.MinX, coordinate[0])
			level.MinY = min(level.MinY, coordinate[1])
			level.MaxX = max(level.MaxX, coordinate[0])
			level.MaxY = max(level.MaxY, coordinate[1])
		}
		fmt.Printf("     z%-2d %4d fetched · %4d held · %4d not published\n",
			zoom, results.fetched, results.skipped, results.absent)
		if level.MaxX < 0 {
			fmt.Printf("     z%-2d nothing published; the survey stops here\n", zoom)
			break
		}
		// The pipeline trusts a level to fill its declared window. Holes
		// inside the window are survivable -- the level rides along partially
		// with its coverage recorded -- but they deserve a loud word.
		expected := (level.MaxX - level.MinX + 1) * (level.MaxY - level.MinY + 1)
		if len(present) < expected {
			fmt.Printf("     WARNING: z%d holds %d of the %d tiles in its window\n",
				zoom, len(present), expected)
		}
		levels = append(levels, level)
	}
	if len(levels) == 0 {
		return stats, nil, errors.New("no tiles at all; is the CDN refusing the survey?")
	}
	return stats, levels, nil
}

// pbArchiveID numbers Piggyback games and maps in the archive's registers,
// one bit above IGN's so every source's ids stay apart; see ignArchiveID for
// why the range stays far below a float64's integers.
func pbArchiveID(seed string) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int64(hash.Sum32()&0x7fffffff) | 1<<33
}

func pbAtoi(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}
