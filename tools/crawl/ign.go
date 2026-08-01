// The IGN mode fills the same archive from IGN's wikimaps. A wikimap page
// carries its whole configuration in the page itself; the markers come from
// one GraphQL request; the tiles are a small pyramid asked for level by level.
// What lands in the archive is a canonical capture -- the ignmap document --
// stored and versioned exactly the way MapGenie snapshots are.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/internal/ignmap"
)

const ignGraphQL = "https://mollusk.apis.ign.com/graphql"

// ignPageMap is the slice of the page's embedded state a capture is built
// from. Everything else in __NEXT_DATA__ -- build ids, trace baggage, session
// scaffolding -- is volatile and never stored.
type ignPageMap struct {
	Width           float64         `json:"width"`
	Height          float64         `json:"height"`
	MinZoom         int             `json:"minZoom"`
	MaxZoom         int             `json:"maxZoom"`
	InitialLat      float64         `json:"initialLat"`
	InitialLng      float64         `json:"initialLng"`
	BackgroundColor string          `json:"backgroundColor"`
	MapName         string          `json:"mapName"`
	MapType         string          `json:"mapType"`
	ObjectName      string          `json:"objectName"`
	MapGenieGameID  json.RawMessage `json:"mapGenieGameId"`
	Tilesets        []string        `json:"tilesets"`
	Types           []ignPageType   `json:"types"`
}

type ignPageType struct {
	TypeSlug       string `json:"typeSlug"`
	TypeName       string `json:"typeName"`
	ParentTypeSlug string `json:"parentTypeSlug"`
	MarkerIcon     struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"markerIcon"`
}

func runIGN(ctx context.Context, fetcher *fetcher, o options) error {
	objectSlug, mapSlug, ok := strings.Cut(o.ign, "/")
	if !ok || objectSlug == "" || mapSlug == "" {
		return errors.New("-ign wants objectSlug/mapSlug, as the address bar spells it")
	}
	pageURL := fmt.Sprintf("https://www.ign.com/maps/%s/%s", objectSlug, mapSlug)

	page, err := fetchIGNPage(ctx, fetcher, pageURL)
	if err != nil {
		return err
	}
	// A wikimap page can also front a MapGenie map. That data has its own
	// crawler, and capturing it here would archive the same source twice under
	// two names.
	if page.MapType != "ign" {
		return fmt.Errorf("%s is not a native IGN wikimap (map type %q)", pageURL, page.MapType)
	}
	if id := strings.TrimSpace(string(page.MapGenieGameID)); id != "" && id != "null" {
		return fmt.Errorf("%s proxies MapGenie game %s; crawl it with -game instead", pageURL, id)
	}
	if len(page.Tilesets) == 0 {
		return fmt.Errorf("%s publishes no tileset", pageURL)
	}
	if len(page.Tilesets) > 1 {
		fmt.Printf("   note: %d tilesets published; capturing the first\n", len(page.Tilesets))
	}
	template := page.Tilesets[0]
	scope := objectSlug + "/" + mapSlug
	if !strings.Contains(template, "/wikimaps/"+scope+"/") {
		return fmt.Errorf("tileset %q does not sit under /wikimaps/%s/, and the pipeline groups tiles by that path", template, scope)
	}

	markers, err := fetchIGNMarkers(ctx, fetcher, objectSlug, mapSlug, page.Types)
	if err != nil {
		return err
	}

	capture := composeIGNCapture(objectSlug, mapSlug, page, markers)
	raw, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	// A capture that does not translate would poison the archive for both
	// tools that read it, so it is refused here, while the problem is one
	// crawl old rather than buried in history.
	if _, err := ignmap.Translate(raw); err != nil {
		return fmt.Errorf("capture does not translate: %w", err)
	}

	gameID := ignArchiveID("ign:" + objectSlug)
	mapID := ignArchiveID("ign:" + scope)
	game := &apiGameFull{ID: gameID, Title: capture.GameTitle,
		Config: apiGameConfig{URL: pageURL}}
	full := &apiMapFull{ID: mapID, Title: capture.MapTitle, Slug: mapSlug}
	// The directory says where the capture came from even though the game
	// keeps its plain title: a MapGenie capture of the same game lives in its
	// own directory, and the two must never resolve to one.
	gameDirectory := resolveGameDirectory(o.archive,
		&apiGameFull{ID: gameID, Title: "IGN " + capture.GameTitle})
	mapDirectory := resolveMapDirectory(o.archive, gameDirectory, full)
	mapDir := filepath.Join(o.archive, mapDirectory)

	fmt.Printf("\n== IGN %s / %s (%d markers, %d types)\n",
		capture.GameTitle, capture.MapTitle, len(capture.Markers), len(capture.Types))

	if o.dryRun {
		fmt.Printf("   would write capture to %s\n", mapDir)
	} else if err := writeSnapshotRecord(mapDir, raw, ignmap.Kind, mapID, pageURL); err != nil {
		return err
	}

	if err := fetchIGNIcons(ctx, fetcher, o, filepath.Join(o.archive, gameDirectory), page.Types); err != nil {
		return err
	}

	index, err := readTileIndex(mapDir)
	if err != nil {
		return err
	}
	index.mapID = mapID
	stats, err := captureIGNTiles(ctx, fetcher, o, capture, template, scope, mapDir, index)
	if err != nil {
		return err
	}
	if o.dryRun {
		return nil
	}
	if err := writeTileIndex(mapDir, index); err != nil {
		return err
	}
	if err := writeMapMetadata(mapDir, game, full, index, len(capture.Markers)); err != nil {
		return err
	}
	if err := registerMap(o.archive, game, full, gameDirectory, mapDirectory, "ign"); err != nil {
		return err
	}
	fmt.Printf("   %d fetched · %d held · %d not published · %d failed · %.1f MB new\n",
		stats.fetched, stats.skipped, stats.absent, stats.failed, float64(stats.bytes)/1e6)
	return nil
}

// fetchIGNPage pulls the map's configuration out of the page. The page embeds
// its state as one JSON document in a script tag, which is the same data the
// page's own scripts boot from.
func fetchIGNPage(ctx context.Context, f *fetcher, pageURL string) (*ignPageMap, error) {
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
	var page struct {
		Props struct {
			PageProps struct {
				Page struct {
					Map ignPageMap `json:"map"`
				} `json:"page"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(doc), &page); err != nil {
		return nil, fmt.Errorf("decode %s page state: %w", pageURL, err)
	}
	m := page.Props.PageProps.Page.Map
	if m.Width <= 0 || m.Height <= 0 || len(m.Types) == 0 {
		return nil, fmt.Errorf("%s describes no wikimap", pageURL)
	}
	return &m, nil
}

// fetchIGNMarkers asks for every marker of every type in one request, which is
// how IGN's own page loads a map with everything switched on.
func fetchIGNMarkers(
	ctx context.Context,
	f *fetcher,
	objectSlug, mapSlug string,
	types []ignPageType,
) ([]ignmap.Marker, error) {
	typeSlugs := make([]string, 0, len(types))
	for _, t := range types {
		typeSlugs = append(typeSlugs, t.TypeSlug)
	}
	request, err := json.Marshal(map[string]any{
		"query": `query MapMarkers($objectSlug: String!, $mapSlug: String!, $typeSlugs: [String!]!) {
  markersMultiple(objectSlug: $objectSlug, mapSlug: $mapSlug, typeSlugs: $typeSlugs) {
    id lat lng markerName markerSlug typeSlug wikiPage iconSlug regionId checklistTaskId
  }
}`,
		"variables": map[string]any{
			"objectSlug": objectSlug,
			"mapSlug":    mapSlug,
			"typeSlugs":  typeSlugs,
		},
	})
	if err != nil {
		return nil, err
	}
	body, _, err := f.post(ctx, ignGraphQL, "application/json", request)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			MarkersMultiple []ignmap.Marker `json:"markersMultiple"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode markers: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("markers query: %s", response.Errors[0].Message)
	}
	if len(response.Data.MarkersMultiple) == 0 {
		return nil, errors.New("the map answered with no markers at all")
	}
	return response.Data.MarkersMultiple, nil
}

func composeIGNCapture(
	objectSlug, mapSlug string,
	page *ignPageMap,
	markers []ignmap.Marker,
) *ignmap.Capture {
	capture := &ignmap.Capture{
		Source:     ignmap.Source,
		ObjectSlug: objectSlug,
		MapSlug:    mapSlug,
		GameTitle:  page.ObjectName,
		MapTitle:   page.MapName,
		Map: ignmap.MapConfig{
			Width:           page.Width,
			Height:          page.Height,
			MinZoom:         page.MinZoom,
			MaxZoom:         page.MaxZoom,
			InitialLat:      page.InitialLat,
			InitialLng:      page.InitialLng,
			BackgroundColor: page.BackgroundColor,
			Tileset:         page.Tilesets[0],
		},
		Markers: markers,
	}
	if capture.GameTitle == "" {
		capture.GameTitle = objectSlug
	}
	for _, t := range page.Types {
		capture.Types = append(capture.Types, ignmap.Type{
			TypeSlug:       t.TypeSlug,
			TypeName:       t.TypeName,
			ParentTypeSlug: t.ParentTypeSlug,
			IconURL:        t.MarkerIcon.URL,
			IconWidth:      t.MarkerIcon.Width,
			IconHeight:     t.MarkerIcon.Height,
		})
	}
	capture.Normalize()
	return capture
}

// fetchIGNIcons stores each type's marker icon under the key tools/generate
// looks icons up by: the category's icon name, which for IGN is the type slug.
func fetchIGNIcons(
	ctx context.Context,
	f *fetcher,
	o options,
	gameDir string,
	types []ignPageType,
) error {
	fetched := 0
	for _, t := range types {
		if t.MarkerIcon.URL == "" {
			continue
		}
		extension := "png"
		if dot := strings.LastIndex(t.MarkerIcon.URL, "."); dot >= 0 && dot < len(t.MarkerIcon.URL)-1 {
			extension = strings.ToLower(t.MarkerIcon.URL[dot+1:])
		}
		path := filepath.Join(gameDir, "icons", t.TypeSlug+"."+extension)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if o.dryRun {
			fetched++
			continue
		}
		body, _, err := f.get(ctx, t.MarkerIcon.URL)
		if err != nil {
			if errors.Is(err, errAbsent) {
				fmt.Printf("   icon %s is not published\n", t.TypeSlug)
				continue
			}
			return fmt.Errorf("icon %s: %w", t.TypeSlug, err)
		}
		if err := writeFile(path, body); err != nil {
			return err
		}
		fetched++
	}
	if fetched > 0 {
		fmt.Printf("   %d icons\n", fetched)
	}
	return nil
}

// captureIGNTiles takes the whole pyramid level by level. There is no pruning
// to plan: the deepest level of a wikimap is at most a few thousand tiles, and
// the deepest level must be complete for the pyramid the bundle is built from,
// so every tile of every level is asked for once.
func captureIGNTiles(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	capture *ignmap.Capture,
	template, scope, mapDir string,
	index *tileIndex,
) (captureStats, error) {
	var stats captureStats
	setID := index.tileSetID(scope)
	extension := ignmap.TileExtension(template)
	deepest := capture.Map.MaxZoom
	if o.maxZoom > 0 && o.maxZoom < deepest {
		deepest = o.maxZoom
	}
	fmt.Printf("   layer %q z0-%d\n", scope, deepest)

	for zoom := 0; zoom <= deepest; zoom++ {
		maxX, maxY := ignmap.LevelExtent(capture.Map.Width, capture.Map.Height, zoom)
		window := tileWindow{minX: 0, minY: 0, maxX: maxX, maxY: maxY}
		results, err := fetchTemplateLevel(ctx, fetcher, o, template, "", "", setID, extension,
			mapDir, zoom, window.tiles(), index)
		if err != nil {
			return stats, err
		}
		stats.merge(results)
		fmt.Printf("     z%-2d %4d fetched · %4d held · %4d not published\n",
			zoom, results.fetched, results.skipped, results.absent)
		// The bundle is built from the deepest complete level. Holes there mean
		// the pyramid tops out shallower than the map is drawn, which deserves
		// more than a counter in the summary line.
		if zoom == deepest && results.absent > 0 {
			fmt.Printf("     WARNING: %d tiles of the deepest level are not published; "+
				"the bundle will be built from a shallower level\n", results.absent)
		}
	}
	return stats, nil
}

// fetchTemplateLevel takes one level of a pyramid whose URLs come from a
// {z}/{x}/{y} template, in whatever punctuation the source spells the tokens.
// The hashes of held tiles are recorded alongside fetched ones, so a resumed
// crawl sees the same picture of the level a fresh one would.
//
// A source that spells its addresses differently from the pipeline -- Trek
// names the row before the column, one zoom down -- passes a second template
// naming where the bytes actually live; the first template stays the address
// of record, the one resume checks and every reader of the archive derives
// the layer from. An empty fetchTemplate means the record address is the
// fetch address, which is every other source.
func fetchTemplateLevel(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	template, fetchTemplate, referer string,
	setID int64,
	extension, mapDir string,
	zoom int,
	wanted [][2]int,
	index *tileIndex,
) (levelResult, error) {
	result := levelResult{hashes: make(map[[2]int]string, len(wanted))}

	var mu sync.Mutex
	var group sync.WaitGroup
	var failure error
	gate := make(chan struct{}, o.concurrency)

	replacer := func(template string, x, y int) string {
		return strings.NewReplacer(
			"{z}", strconv.Itoa(zoom),
			"{x}", strconv.Itoa(x),
			"{y}", strconv.Itoa(y),
		).Replace(template)
	}
	if fetchTemplate == "" {
		fetchTemplate = template
	}

	for _, coordinate := range wanted {
		url := replacer(template, coordinate[0], coordinate[1])
		path := filepath.Join(mapDir, "tiles",
			"set-"+strconv.FormatInt(setID, 10),
			strconv.Itoa(zoom), strconv.Itoa(coordinate[0]),
			strconv.Itoa(coordinate[1])+"."+extension)

		if record, held := index.byURL[url]; held {
			if record.Status == "absent" {
				result.absent++
				continue
			}
			if record.Status == "cached" {
				if _, err := os.Stat(path); err == nil {
					result.skipped++
					result.hashes[coordinate] = record.ContentHash
					continue
				}
			}
		}
		if o.dryRun {
			result.fetched++
			continue
		}

		group.Add(1)
		gate <- struct{}{}
		go func(coordinate [2]int, url, fetchURL, path string) {
			defer group.Done()
			defer func() { <-gate }()

			body, contentType, err := fetcher.getReferred(ctx, fetchURL, referer)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status, message := "failed", err.Error()
				if errors.Is(err, errAbsent) {
					status, message = "absent", ""
					result.absent++
				} else {
					result.failed++
					if failure == nil {
						failure = err
					}
				}
				index.put(tileRecord{
					URL: url, Status: status, TileSetID: setID,
					Zoom: zoom, X: coordinate[0], Y: coordinate[1],
					ContentType: "application/octet-stream",
					Error:       message,
				})
				return
			}
			if err := writeFile(path, body); err != nil {
				failure = err
				return
			}
			hash := hashBytes(body)
			index.put(tileRecord{
				URL: url, Status: "cached", TileSetID: setID,
				Zoom: zoom, X: coordinate[0], Y: coordinate[1],
				ContentHash: hash, ContentType: contentType,
				ByteLength: len(body),
				CoverageKey: fmt.Sprintf("%d:%d:%d:%d:%d",
					index.mapID, setID, zoom, coordinate[0], coordinate[1]),
			})
			result.fetched++
			result.bytes += int64(len(body))
			result.hashes[coordinate] = hash
		}(coordinate, url, replacer(fetchTemplate, coordinate[0], coordinate[1]), path)
	}
	group.Wait()
	return result, failure
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ignArchiveID numbers IGN games and maps in the archive's registers. The high
// bit set above the int32 range keeps them apart from every MapGenie id while
// staying far below the point where a float64 -- which is how the register
// reads them back -- loses integers.
func ignArchiveID(seed string) int64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int64(hash.Sum32()&0x7fffffff) | 1<<32
}
