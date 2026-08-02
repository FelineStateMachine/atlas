package crawl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The write path.
//
// Everything a crawl produces lands through this file, and it lands the way
// docs/generate.md §3 describes the archive: a volume register at the root, a
// world register per volume, a capture index and its bodies per world, and a
// tile index beside the rasters. The layout is the historical one and is kept
// verbatim; what is new is that only these functions write it.
//
// Two habits are load-bearing and are worth reading twice.
//
// A register is updated **in place, by identity**. An entry is found by its id,
// its known fields are merged over whatever was there, and its position in the
// list never changes. That is what keeps a directory named once named forever,
// and it is why a field this package has never heard of survives a run.
//
// A capture is deduplicated **by content hash alone**. The body is written --
// idempotently, to a path that is its own hash -- and the index is appended to
// only if that hash has never been seen. So `capturedAt` is first-seen rather
// than last-verified, and a re-crawl of unchanged data leaves the archive byte
// for byte as it was. Everything downstream stands on that: the translators
// replay, the stamps stand still, and a rebuild writes the file that is already
// there.

// Archive is an archive open for writing.
type Archive struct{ root string }

// Open readies an archive for writing. The directory need not exist.
func Open(root string) (*Archive, error) {
	if root == "" {
		return nil, errors.New("an archive needs a root")
	}
	return &Archive{root: root}, nil
}

// Root is where the archive sits.
func (a *Archive) Root() string { return a.root }

// Volume is a volume as a crawl registers it.
type Volume struct {
	// ID is the volume's identity in the crawling source's id space. It is what
	// the register is keyed by, so it must be stable across runs.
	ID int64
	// Title is the volume's name as the publisher gives it -- the plain one, so
	// two sources' captures of one game answer for one library entry.
	Title string
	// Locator is where the publisher publishes it. Provenance only.
	Locator string
	// Source is the crawler's registry name.
	Source string
	// DirectoryTitle is the title the volume's directory is named from on first
	// sight, and only then. It carries the source's own prefix -- "IGN
	// Cyberpunk 2077" -- so that two publishers describing one game do not
	// fight over one directory while both still register the plain title.
	DirectoryTitle string
}

// World is a world as a crawl registers it.
type World struct {
	ID    int64
	Slug  string
	Title string
}

// RegisterVolume finds or opens a volume's directory and records it in the
// archive's register. It answers the directory, absolute.
func (a *Archive) RegisterVolume(v Volume) (string, error) {
	register, err := readList(filepath.Join(a.root, "archive.json"), "games")
	if err != nil {
		return "", err
	}
	directory := findDirectory(register, v.ID)
	if directory == "" {
		title := v.DirectoryTitle
		if title == "" {
			title = v.Title
		}
		directory = "games/" + DirectoryName(title, v.ID)
	}
	entry := map[string]any{
		"directory": directory,
		"id":        v.ID,
		"title":     v.Title,
		"url":       v.Locator,
	}
	if v.Source != "" {
		entry["source"] = v.Source
	}
	register = upsert(register, v.ID, entry)
	if err := writeJSON(filepath.Join(a.root, "archive.json"), map[string]any{
		"format": "fmg-git-working-tree",
		"games":  register,
	}); err != nil {
		return "", err
	}
	return filepath.Join(a.root, filepath.FromSlash(directory)), nil
}

// RegisterWorld finds or opens a world's directory under a volume and records
// it in the volume's register. It answers the directory, absolute.
func (a *Archive) RegisterWorld(v Volume, volumeDir string, w World) (string, error) {
	path := filepath.Join(volumeDir, "game.json")
	worlds, err := readList(path, "maps")
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(a.root, volumeDir)
	if err != nil {
		return "", err
	}
	directory := findDirectory(worlds, w.ID)
	if directory == "" {
		directory = filepath.ToSlash(filepath.Join(relative, "maps", DirectoryName(w.Title, w.ID)))
	}
	worlds = upsert(worlds, w.ID, map[string]any{
		"directory": directory,
		"id":        w.ID,
		"slug":      w.Slug,
		"title":     w.Title,
	})
	volume := map[string]any{
		"id":    v.ID,
		"title": v.Title,
		"url":   v.Locator,
		"icons": filepath.ToSlash(filepath.Join(relative, "icons", "index.json")),
		"maps":  worlds,
	}
	if v.Source != "" {
		volume["source"] = v.Source
	}
	if err := writeJSON(path, volume); err != nil {
		return "", err
	}
	return filepath.Join(a.root, filepath.FromSlash(directory)), nil
}

// Capture is one archived document: what the source called it, where it came
// from, and the bytes themselves.
type Capture struct {
	Kind      string
	SourceID  int64
	SourceURL string
	Body      []byte
}

// WriteCapture archives one capture and reports whether anything moved.
//
// The body is written unconditionally -- to a path that is its own content hash,
// so writing it twice writes the same bytes -- and the index is appended to only
// when that hash is new. A re-crawl of unchanged data therefore leaves the
// working tree untouched, which is the property every replayable thing
// downstream stands on.
func (a *Archive) WriteCapture(worldDir string, c Capture) (hash string, fresh bool, err error) {
	hash = Hash(c.Body)
	if err := writeFile(filepath.Join(worldDir, "snapshots", "map", hash+".json"), c.Body); err != nil {
		return "", false, err
	}
	path := filepath.Join(worldDir, "snapshots", "index.json")
	var index []snapshot
	if err := readJSON(path, &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	for _, held := range index {
		if held.ContentHash == hash {
			return hash, false, nil
		}
	}
	index = append(index, snapshot{
		// First-seen, and never refreshed. A volume's creation time is derived
		// from this, so a re-verified capture must not move it.
		CapturedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ContentHash: hash,
		Kind:        c.Kind,
		SourceID:    c.SourceID,
		SourceURL:   c.SourceURL,
	})
	sort.Slice(index, func(i, j int) bool { return index[i].CapturedAt < index[j].CapturedAt })
	if err := writeJSON(path, index); err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// snapshot is a capture index record. Its field order is its wire order.
type snapshot struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
	Kind        string `json:"kind"`
	SourceID    int64  `json:"sourceId"`
	SourceURL   string `json:"sourceUrl"`
}

// TileRecord is one tile as the index records it. Its field order is its wire
// order, and the omissions are the layout's own.
type TileRecord struct {
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

// TileIndex is a world's tile records, open for writing. It is keyed by URL,
// because the URL is what a resumed run looks a tile up by.
type TileIndex struct {
	byURL map[string]TileRecord
	// worldID and setIDs are what a coverage key is spelled from.
	worldID int64
	setIDs  map[string]int64
	usedIDs map[int64]bool
}

// OpenTileIndex reads what a previous run recorded, so this one can resume: a
// tile already cached is not fetched again, and a tile the origin said it never
// published is not asked for again.
func (a *Archive) OpenTileIndex(worldDir string, worldID int64) (*TileIndex, error) {
	var records []TileRecord
	if err := readJSON(filepath.Join(worldDir, "tiles", "index.json"), &records); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	index := &TileIndex{
		byURL:   make(map[string]TileRecord, len(records)),
		worldID: worldID,
		setIDs:  make(map[string]int64),
		usedIDs: make(map[int64]bool),
	}
	for _, record := range records {
		index.byURL[record.URL] = record
		// The numbered directories a previous run assigned are recovered rather
		// than reassigned: a fresh number would orphan every tile already on
		// disk under the old one.
		if record.TileSetID != 0 && record.Zoom != 0 {
			if set := tileSetOf(record.URL, record.Zoom); set != "" {
				index.setIDs[set] = record.TileSetID
				index.usedIDs[record.TileSetID] = true
			}
		}
	}
	return index, nil
}

// Held is what a previous run recorded for a URL, if anything.
func (i *TileIndex) Held(url string) (TileRecord, bool) {
	record, ok := i.byURL[url]
	return record, ok
}

// Put records one tile.
func (i *TileIndex) Put(record TileRecord) { i.byURL[record.URL] = record }

// SetID is the numbered directory a publisher's pyramid path is stored under,
// assigned once and remembered. The number is derived from the path so a fresh
// archive is deterministic, and probed forward on collision so two paths never
// share a directory.
func (i *TileIndex) SetID(path string) int64 {
	if id, known := i.setIDs[path]; known {
		return id
	}
	id := int64(fnv32a(path)%9000) + 1000
	for i.usedIDs[id] {
		id++
	}
	i.setIDs[path] = id
	i.usedIDs[id] = true
	return id
}

// CoverageKey is what a tile record carries so a reader of the index can tell
// two worlds' tiles apart without parsing a URL.
func (i *TileIndex) CoverageKey(setID int64, zoom, x, y int) string {
	return strconv.FormatInt(i.worldID, 10) + ":" + strconv.FormatInt(setID, 10) + ":" +
		strconv.Itoa(zoom) + ":" + strconv.Itoa(x) + ":" + strconv.Itoa(y)
}

// TilePath is where a tile's bytes sit.
func TilePath(worldDir string, setID int64, zoom, x, y int, format string) string {
	return filepath.Join(worldDir, "tiles", "set-"+strconv.FormatInt(setID, 10),
		strconv.Itoa(zoom), strconv.Itoa(x), strconv.Itoa(y)+"."+format)
}

// WriteTile puts a tile's bytes on disk.
func WriteTile(path string, data []byte) error { return writeFile(path, data) }

// Close writes the index. Records are sorted by URL, so a diff of two runs reads
// as what changed rather than as what was fetched in what order.
func (i *TileIndex) Close(worldDir string) error {
	records := make([]TileRecord, 0, len(i.byURL))
	for _, record := range i.byURL {
		records = append(records, record)
	}
	sort.SliceStable(records, func(a, b int) bool { return records[a].URL < records[b].URL })
	return writeJSON(filepath.Join(worldDir, "tiles", "index.json"), records)
}

// WriteArtwork stores one piece of a volume's collection artwork.
func WriteArtwork(volumeDir, key, extension string, data []byte) error {
	return writeFile(filepath.Join(volumeDir, "icons", key+extension), data)
}

// upsert merges an entry over whatever the register already held for that
// identity, keeping its position. Anything the register carried that this
// package has never heard of survives, which is what lets the layout outlive the
// code that writes it.
func upsert(list []map[string]any, id int64, entry map[string]any) []map[string]any {
	for index, held := range list {
		if identity(held) != id {
			continue
		}
		for key, value := range entry {
			held[key] = value
		}
		list[index] = held
		return list
	}
	return append(list, entry)
}

func findDirectory(list []map[string]any, id int64) string {
	for _, held := range list {
		if identity(held) != id {
			continue
		}
		if directory, ok := held["directory"].(string); ok {
			return directory
		}
	}
	return ""
}

// identity reads a register entry's id back. It arrives as a float64 because
// that is what JSON numbers decode to, and every id in the archive is held under
// 2^53 so the round trip is exact.
func identity(entry map[string]any) int64 {
	value, ok := entry["id"].(float64)
	if !ok {
		return 0
	}
	return int64(value)
}

func readList(path, key string) ([]map[string]any, error) {
	var held map[string]any
	if err := readJSON(path, &held); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	raw, ok := held[key].([]any)
	if !ok {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if record, ok := entry.(map[string]any); ok {
			out = append(out, record)
		}
	}
	return out, nil
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// writeJSON writes an index the way the layout spells one: two-space indent and
// a trailing newline, so a git diff of an archive reads line by line.
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// fnv32a is FNV-1a, the same derivation every stable-name-to-number in the lane
// uses.
func fnv32a(value string) uint32 {
	const offset, prime = 2166136261, 16777619
	hash := uint32(offset)
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= prime
	}
	return hash
}

// tileSetOf recovers a publisher's pyramid path from a tile URL, the way the
// archive's reader does. The two must agree: a record this cannot parse is a
// record the deriver will not see.
func tileSetOf(rawURL string, zoom int) string {
	for _, marker := range []string{"/games/", "/wikimaps/", "/tiles/"} {
		_, rest, found := strings.Cut(rawURL, marker)
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
