// Command crawl fills the FMG archive directly from the MapGenie API.
//
// A run names a game, optionally a single map, and captures everything that
// target needs in one pass: the map metadata snapshot and every tile of its
// pyramid. There are no separate phases to run in a particular order, and
// re-running is cheap because anything already archived is skipped.
//
// MapGenie serves this data to every visitor of a map page, but a crawl asks
// for far more of it at once than a person browsing would. Requests are
// therefore spaced out, limited in number, backed off when the server pushes
// back, and never repeated for something already on disk.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiBase = "https://mapgenie.io/api/v1"
	// MapGenie's CDN answers 403 to requests without a browser user agent.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	// Levels holding at most this many tiles are captured whole. Deeper levels
	// follow only the parents that held content, which is what keeps a z15
	// capture affordable: empty space stays empty at every zoom below it.
	wholeLevelBudget = 1024
)

func main() {
	options := parseOptions()
	if err := run(options); err != nil {
		fmt.Fprintln(os.Stderr, "crawl:", err)
		os.Exit(1)
	}
}

type options struct {
	archive     string
	game        string
	mapTarget   string
	maxZoom     int
	list        bool
	dryRun      bool
	concurrency int
	interval    time.Duration
}

func parseOptions() options {
	var o options
	flag.StringVar(&o.archive, "archive", "../gamemap/fmg-archive", "FMG archive root")
	flag.StringVar(&o.game, "game", "", "game slug or id")
	flag.StringVar(&o.mapTarget, "map", "", "map slug or id (default: every map of the game)")
	flag.IntVar(&o.maxZoom, "max-zoom", 0, "deepest zoom to capture (default: the layer's own maximum)")
	flag.BoolVar(&o.list, "list", false, "list games, or the maps of -game, and exit")
	flag.BoolVar(&o.dryRun, "dry-run", false, "report what would be fetched without writing anything")
	flag.IntVar(&o.concurrency, "concurrency", 4, "concurrent requests")
	flag.DurationVar(&o.interval, "interval", 150*time.Millisecond, "minimum spacing between requests")
	flag.Parse()
	return o
}

func run(o options) error {
	if o.concurrency < 1 {
		return errors.New("-concurrency must be at least 1")
	}
	fetcher := newFetcher(o.concurrency, o.interval)
	ctx := context.Background()

	games, err := listGames(ctx, fetcher)
	if err != nil {
		return err
	}
	if o.game == "" {
		if !o.list {
			return errors.New("-game is required; use -list to see what is available")
		}
		for _, game := range games {
			fmt.Printf("%-38s id=%d\n", game.Slug, game.ID)
		}
		return nil
	}

	game, ok := findGame(games, o.game)
	if !ok {
		return fmt.Errorf("no game matches %q; use -list to see what is available", o.game)
	}
	targets, err := selectMaps(game, o.mapTarget)
	if err != nil {
		return err
	}
	if o.list {
		for _, target := range targets {
			fmt.Printf("%-38s id=%d  %s\n", target.Slug, target.ID, target.Title)
		}
		return nil
	}

	for _, target := range targets {
		if err := crawlMap(ctx, fetcher, o, game, target); err != nil {
			return fmt.Errorf("%s / %s: %w", game.Title, target.Title, err)
		}
	}
	return nil
}

// crawlMap captures one map completely: its metadata snapshot, then its tiles.
// Both are idempotent, so an interrupted run resumes by simply being repeated.
func crawlMap(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	game *apiGameFull,
	target apiMapRef,
) error {
	fmt.Printf("\n== %s / %s (map %d)\n", game.Title, target.Title, target.ID)

	full, raw, err := fetchMap(ctx, fetcher, target.ID)
	if err != nil {
		return err
	}
	gameDirectory := resolveGameDirectory(o.archive, game)
	mapDirectory := resolveMapDirectory(o.archive, gameDirectory, full)
	mapDir := filepath.Join(o.archive, mapDirectory)

	if o.dryRun {
		fmt.Printf("   would write snapshot to %s\n", mapDir)
	} else if err := writeSnapshot(mapDir, full, raw); err != nil {
		return err
	}

	index, err := readTileIndex(mapDir)
	if err != nil {
		return err
	}
	index.mapID = full.ID
	stats, err := captureTiles(ctx, fetcher, o, game, full, mapDir, index)
	if err != nil {
		return err
	}
	if o.dryRun {
		return nil
	}
	if err := writeTileIndex(mapDir, index); err != nil {
		return err
	}
	if err := writeMapMetadata(mapDir, game, full, index); err != nil {
		return err
	}
	if err := registerMap(o.archive, game, full, gameDirectory, mapDirectory); err != nil {
		return err
	}
	fmt.Printf("   %d fetched · %d held · %d not published · %d failed · %.1f MB new\n",
		stats.fetched, stats.skipped, stats.absent, stats.failed, float64(stats.bytes)/1e6)
	return nil
}

// captureTiles walks each layer from its shallowest zoom down. Levels small
// enough to take whole are taken whole; past that only the children of tiles
// that held content are requested, so blank regions cost nothing at depth.
func captureTiles(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	game *apiGameFull,
	full *apiMapFull,
	mapDir string,
	index *tileIndex,
) (captureStats, error) {
	var stats captureStats
	base := game.Config.TilesBaseURL
	if base == "" {
		base = "https://tiles.mapgenie.io"
	}

	for _, set := range full.Config.TileSets {
		setID := index.tileSetID(set.Path)
		deepest := set.MaxZoom
		if o.maxZoom > 0 && o.maxZoom < deepest {
			deepest = o.maxZoom
		}
		fmt.Printf("   layer %q (%s) z%d-%d\n", set.Name, set.Path, set.MinZoom, deepest)

		var live map[[2]int]bool
		var background string
		for zoom := set.MinZoom; zoom <= deepest; zoom++ {
			window, ok := set.window(zoom)
			if !ok {
				fmt.Printf("     z%-2d skipped: no bounds published\n", zoom)
				live = nil
				continue
			}
			wanted := window.tiles()
			whole := len(wanted) <= wholeLevelBudget || live == nil
			if !whole {
				wanted = childrenOf(live, window)
			}
			if len(wanted) == 0 {
				fmt.Printf("     z%-2d nothing left to follow\n", zoom)
				break
			}

			results, err := fetchLevel(ctx, fetcher, o, set, setID, base, mapDir, zoom, wanted, index)
			if err != nil {
				return stats, err
			}
			stats.merge(results)

			// Each level re-encodes its own background tile, so the hash is
			// re-read from every level captured whole. It is never taken from a
			// pruned level, whose handful of real tiles could otherwise vote
			// themselves away, and a level that yields no answer keeps the last
			// one rather than losing it.
			if whole {
				if found := dominantHash(results.hashes); found != "" {
					background = found
				}
			}
			live = liveTiles(results.hashes, background)
			fmt.Printf("     z%-2d %4d fetched · %4d held · %4d not published · %4d carry content\n",
				zoom, results.fetched, results.skipped, results.absent, len(live))
			if background != "" && len(live) == 0 {
				break
			}
		}
	}
	return stats, nil
}

func fetchLevel(
	ctx context.Context,
	fetcher *fetcher,
	o options,
	set apiTileSet,
	setID int64,
	base, mapDir string,
	zoom int,
	wanted [][2]int,
	index *tileIndex,
) (levelResult, error) {
	result := levelResult{hashes: make(map[[2]int]string, len(wanted))}
	extension := strings.TrimPrefix(strings.ToLower(set.Extension), ".")
	if extension == "" {
		extension = "jpg"
	}

	var mu sync.Mutex
	var group sync.WaitGroup
	var failure error
	gate := make(chan struct{}, o.concurrency)

	for _, coordinate := range wanted {
		url := fmt.Sprintf("%s/games/%s/%d/%d/%d.%s",
			strings.TrimSuffix(base, "/"), set.Path, zoom, coordinate[0], coordinate[1], extension)
		path := filepath.Join(mapDir, "tiles",
			"set-"+strconv.FormatInt(setID, 10),
			strconv.Itoa(zoom), strconv.Itoa(coordinate[0]),
			strconv.Itoa(coordinate[1])+"."+extension)

		// Resume: a tile already recorded and present on disk is never asked
		// for a second time.
		if record, held := index.byURL[url]; held {
			if record.Status == "absent" {
				continue
			}
			if record.Status == "cached" {
				if _, err := os.Stat(path); err == nil {
					mu.Lock()
					result.skipped++
					result.hashes[coordinate] = record.ContentHash
					mu.Unlock()
					continue
				}
			}
		}
		if o.dryRun {
			mu.Lock()
			result.fetched++
			mu.Unlock()
			continue
		}

		group.Add(1)
		gate <- struct{}{}
		go func(coordinate [2]int, url, path string) {
			defer group.Done()
			defer func() { <-gate }()

			body, contentType, err := fetcher.get(ctx, url)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status, message := "failed", err.Error()
				if errors.Is(err, errAbsent) {
					// Recorded so a later run does not ask again.
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
			sum := sha256.Sum256(body)
			hash := hex.EncodeToString(sum[:])
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
		}(coordinate, url, path)
	}
	group.Wait()
	return result, failure
}

type levelResult struct {
	fetched int
	skipped int
	absent  int
	failed  int
	bytes   int64
	hashes  map[[2]int]string
}

type captureStats struct {
	fetched int
	skipped int
	absent  int
	failed  int
	bytes   int64
}

func (s *captureStats) merge(r levelResult) {
	s.fetched += r.fetched
	s.skipped += r.skipped
	s.absent += r.absent
	s.failed += r.failed
	s.bytes += r.bytes
}

// backgroundSampleFloor is the smallest level worth judging. The top of a
// pyramid is a handful of tiles showing the whole map, where "repeated over
// half the level" means nothing.
const backgroundSampleFloor = 16

// dominantHash reports the tile a level uses for empty space. Two tiles of real
// map are never byte-identical, so any tile repeated across a level is the
// background, however small a share of the level it occupies -- a map whose
// content fills most of its bounds still has a background in the corners.
//
// A level whose tiles are *all* identical says nothing: there is no content to
// contrast against, so the question is left to a deeper level.
func dominantHash(hashes map[[2]int]string) string {
	if len(hashes) < backgroundSampleFloor {
		return ""
	}
	counts := make(map[string]int, len(hashes))
	var best string
	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		counts[hash]++
		if counts[hash] > counts[best] {
			best = hash
		}
	}
	repeats := max(8, len(hashes)/20)
	if best == "" || counts[best] < repeats || counts[best] == len(hashes) {
		return ""
	}
	return best
}

func liveTiles(hashes map[[2]int]string, background string) map[[2]int]bool {
	live := make(map[[2]int]bool, len(hashes))
	for coordinate, hash := range hashes {
		if hash == "" || (background != "" && hash == background) {
			continue
		}
		live[coordinate] = true
	}
	return live
}

func childrenOf(live map[[2]int]bool, window tileWindow) [][2]int {
	children := make([][2]int, 0, len(live)*4)
	for parent := range live {
		for dy := 0; dy < 2; dy++ {
			for dx := 0; dx < 2; dx++ {
				child := [2]int{parent[0]*2 + dx, parent[1]*2 + dy}
				if window.holds(child) {
					children = append(children, child)
				}
			}
		}
	}
	sortCoordinates(children)
	return children
}

func sortCoordinates(coordinates [][2]int) {
	sort.Slice(coordinates, func(i, j int) bool {
		if coordinates[i][1] != coordinates[j][1] {
			return coordinates[i][1] < coordinates[j][1]
		}
		return coordinates[i][0] < coordinates[j][0]
	})
}

type tileWindow struct{ minX, minY, maxX, maxY int }

func (w tileWindow) holds(c [2]int) bool {
	return c[0] >= w.minX && c[0] <= w.maxX && c[1] >= w.minY && c[1] <= w.maxY
}

func (w tileWindow) tiles() [][2]int {
	out := make([][2]int, 0, (w.maxX-w.minX+1)*(w.maxY-w.minY+1))
	for y := w.minY; y <= w.maxY; y++ {
		for x := w.minX; x <= w.maxX; x++ {
			out = append(out, [2]int{x, y})
		}
	}
	return out
}

func (s apiTileSet) window(zoom int) (tileWindow, bool) {
	bounds, ok := s.Bounds[strconv.Itoa(zoom)]
	if !ok {
		return tileWindow{}, false
	}
	return tileWindow{
		minX: bounds.X.Min, minY: bounds.Y.Min,
		maxX: bounds.X.Max, maxY: bounds.Y.Max,
	}, true
}
