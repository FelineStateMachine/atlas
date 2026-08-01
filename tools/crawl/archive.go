package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The archive layout is the one the FMG crawl dashboard produced, so tools/tiles
// and tools/generate read a crawled archive without knowing the difference.

type tileRecord struct {
	ByteLength  int    `json:"byteLength"`
	ContentHash string `json:"contentHash"`
	ContentType string `json:"contentType"`
	CoverageKey string `json:"coverageKey,omitempty"`
	Error       string `json:"error,omitempty"`
	Status      string `json:"status"`
	TileSetID   int64  `json:"tileSetId,omitempty"`
	URL         string `json:"url"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
	Zoom        int    `json:"zoom,omitempty"`
}

type snapshotRecord struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
	Kind        string `json:"kind"`
	SourceID    int64  `json:"sourceId"`
	SourceURL   string `json:"sourceUrl"`
}

// tileIndex is the working set of tiles/index.json: keyed by URL for resume
// checks, and remembering which directory each layer's tiles live under.
type tileIndex struct {
	mapID   int64
	byURL   map[string]tileRecord
	order   []string
	setIDs  map[string]int64
	usedIDs map[int64]bool
}

func newTileIndex(mapID int64) *tileIndex {
	return &tileIndex{
		mapID:   mapID,
		byURL:   make(map[string]tileRecord),
		setIDs:  make(map[string]int64),
		usedIDs: make(map[int64]bool),
	}
}

func (i *tileIndex) put(record tileRecord) {
	if _, seen := i.byURL[record.URL]; !seen {
		i.order = append(i.order, record.URL)
	}
	i.byURL[record.URL] = record
}

func (i *tileIndex) records() []tileRecord {
	out := make([]tileRecord, 0, len(i.order))
	for _, url := range i.order {
		out = append(out, i.byURL[url])
	}
	return out
}

// tileSetID keeps the directory a layer already uses, so re-crawling an archive
// captured by the extension does not orphan its files. New layers get a stable
// id derived from their path.
func (i *tileIndex) tileSetID(path string) int64 {
	if id, ok := i.setIDs[path]; ok {
		return id
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
	id := int64(hash.Sum32()%9000) + 1000
	for i.usedIDs[id] {
		id++
	}
	i.setIDs[path] = id
	i.usedIDs[id] = true
	return id
}

func readTileIndex(mapDir string) (*tileIndex, error) {
	index := newTileIndex(0)
	var records []tileRecord
	if err := readJSON(filepath.Join(mapDir, "tiles", "index.json"), &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return index, nil
		}
		return nil, err
	}
	for _, record := range records {
		index.put(record)
		if record.TileSetID == 0 || record.Zoom == 0 {
			continue
		}
		if path := tileSetPathOf(record.URL, record.Zoom); path != "" {
			index.setIDs[path] = record.TileSetID
			index.usedIDs[record.TileSetID] = true
		}
	}
	return index, nil
}

// tileSetPathOf recovers the layer path from a tile URL, matching how
// tools/tiles groups levels: everything between the source's marker segment
// and "/<zoom>/". MapGenie serves tiles under /games/, IGN under /wikimaps/,
// Piggyback under /tiles/.
func tileSetPathOf(url string, zoom int) string {
	for _, marker := range []string{"/games/", "/wikimaps/", "/tiles/"} {
		_, rest, found := strings.Cut(url, marker)
		if !found {
			continue
		}
		path, _, found := strings.Cut(rest, "/"+strconv.Itoa(zoom)+"/")
		if !found {
			continue
		}
		return path
	}
	return ""
}

func writeTileIndex(mapDir string, index *tileIndex) error {
	records := index.records()
	sort.SliceStable(records, func(a, b int) bool { return records[a].URL < records[b].URL })
	return writeJSON(filepath.Join(mapDir, "tiles", "index.json"), records)
}

// writeSnapshot stores the response exactly as received, addressed by its hash,
// and records it only if that content is new.
func writeSnapshot(mapDir string, full *apiMapFull, raw []byte) error {
	return writeSnapshotRecord(mapDir, raw, "map", full.ID,
		fmt.Sprintf("/api/v1/maps/%d/full", full.ID))
}

// writeSnapshotRecord is the shape every source's capture takes: the document
// addressed by its hash, and one index record per distinct content. A capture
// whose bytes are already archived leaves no trace of having been retaken.
func writeSnapshotRecord(mapDir string, raw []byte, kind string, sourceID int64, sourceURL string) error {
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	path := filepath.Join(mapDir, "snapshots", "map", hash+".json")
	if err := writeFile(path, raw); err != nil {
		return err
	}

	indexPath := filepath.Join(mapDir, "snapshots", "index.json")
	var snapshots []snapshotRecord
	if err := readJSON(indexPath, &snapshots); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, snapshot := range snapshots {
		if snapshot.ContentHash == hash {
			return nil
		}
	}
	snapshots = append(snapshots, snapshotRecord{
		CapturedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ContentHash: hash,
		Kind:        kind,
		SourceID:    sourceID,
		SourceURL:   sourceURL,
	})
	sort.Slice(snapshots, func(a, b int) bool {
		return snapshots[a].CapturedAt < snapshots[b].CapturedAt
	})
	return writeJSON(indexPath, snapshots)
}

func writeMapMetadata(
	mapDir string,
	game *apiGameFull,
	full *apiMapFull,
	index *tileIndex,
	pinCount int,
) error {
	cachedByZoom := map[string]int{}
	totalsByZoom := map[string]int{}
	var bytes int64
	var count int
	for _, record := range index.records() {
		if record.Zoom == 0 {
			continue
		}
		zoom := strconv.Itoa(record.Zoom)
		totalsByZoom[zoom]++
		if record.Status == "cached" {
			cachedByZoom[zoom]++
			bytes += int64(record.ByteLength)
			count++
		}
	}
	return writeJSON(filepath.Join(mapDir, "map.json"), map[string]any{
		"cachedTilesByZoom": cachedByZoom,
		"tileTotalsByZoom":  totalsByZoom,
		"gameId":            game.ID,
		"gameTitle":         game.Title,
		"gameUrl":           game.Config.URL,
		"mapId":             full.ID,
		"mapSlug":           full.Slug,
		"mapTitle":          full.Title,
		"phase":             "complete",
		"pinCount":          pinCount,
		"snapshotCount":     1,
		"tileBytes":         bytes,
		"tileCount":         count,
	})
}

// registerMap makes the map discoverable to tools/tiles and tools/generate by
// listing it in game.json and archive.json, leaving any existing entries alone.
// A source name, when there is one, rides along so a reader of the archive can
// tell which crawler a game came from; the tools never look at it.
func registerMap(
	archiveRoot string,
	game *apiGameFull,
	full *apiMapFull,
	gameDirectory, mapDirectory, source string,
) error {
	gamePath := filepath.Join(archiveRoot, gameDirectory, "game.json")
	entry := map[string]any{
		"directory": mapDirectory,
		"id":        full.ID,
		"slug":      full.Slug,
		"title":     full.Title,
	}
	var gameFile map[string]any
	if err := readJSON(gamePath, &gameFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		gameFile = map[string]any{}
	}
	gameFile["id"] = game.ID
	gameFile["title"] = game.Title
	gameFile["url"] = game.Config.URL
	gameFile["icons"] = filepath.ToSlash(filepath.Join(gameDirectory, "icons", "index.json"))
	if source != "" {
		gameFile["source"] = source
	}
	gameFile["maps"] = upsertByID(gameFile["maps"], entry, full.ID)
	if err := writeJSON(gamePath, gameFile); err != nil {
		return err
	}

	archivePath := filepath.Join(archiveRoot, "archive.json")
	var archiveFile map[string]any
	if err := readJSON(archivePath, &archiveFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		archiveFile = map[string]any{"format": "fmg-git-working-tree"}
	}
	gameEntry := map[string]any{
		"directory": gameDirectory,
		"id":        game.ID,
		"title":     game.Title,
		"url":       game.Config.URL,
	}
	if source != "" {
		gameEntry["source"] = source
	}
	archiveFile["games"] = upsertByID(archiveFile["games"], gameEntry, game.ID)
	return writeJSON(archivePath, archiveFile)
}

// upsertByID replaces the entry carrying id, or appends it, preserving whatever
// order and extra fields the archive already had.
func upsertByID(existing any, entry map[string]any, id int64) []any {
	list, _ := existing.([]any)
	for index, candidate := range list {
		record, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := record["id"].(float64); ok && int64(value) == id {
			for key, replacement := range entry {
				record[key] = replacement
			}
			list[index] = record
			return list
		}
	}
	return append(list, entry)
}

// resolveGameDirectory prefers the directory the archive already uses for this
// game. The existing names came from slugified titles with accents folded
// ("Pokémon Red/Blue/Yellow" became pokemon-red-blue-yellow), so reusing the
// recorded name is safer than trying to reproduce that transformation.
func resolveGameDirectory(archiveRoot string, game *apiGameFull) string {
	var archiveFile struct {
		Games []struct {
			Directory string `json:"directory"`
			ID        int64  `json:"id"`
		} `json:"games"`
	}
	if err := readJSON(filepath.Join(archiveRoot, "archive.json"), &archiveFile); err == nil {
		for _, candidate := range archiveFile.Games {
			if candidate.ID == game.ID && candidate.Directory != "" {
				return candidate.Directory
			}
		}
	}
	return filepath.ToSlash(filepath.Join("games", directoryName(game.Title, game.ID)))
}

func resolveMapDirectory(archiveRoot, gameDirectory string, full *apiMapFull) string {
	var gameFile struct {
		Maps []struct {
			Directory string `json:"directory"`
			ID        int64  `json:"id"`
		} `json:"maps"`
	}
	path := filepath.Join(archiveRoot, gameDirectory, "game.json")
	if err := readJSON(path, &gameFile); err == nil {
		for _, candidate := range gameFile.Maps {
			if candidate.ID == full.ID && candidate.Directory != "" {
				return candidate.Directory
			}
		}
	}
	return filepath.ToSlash(filepath.Join(gameDirectory, "maps", directoryName(full.Title, full.ID)))
}

// directoryName names a game or map the archive has never seen. Latin letters
// with diacritics fold to their base form so titles such as "Pokémon" keep a
// readable directory rather than losing the character to a separator.
func directoryName(title string, id int64) string {
	var builder strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(title) {
		if folded, ok := foldedLatin[r]; ok {
			r = folded
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			previousDash = false
		case !previousDash:
			builder.WriteRune('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-") + "-" + strconv.FormatInt(id, 10)
}

var foldedLatin = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ß': 's', 'ý': 'y',
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}
