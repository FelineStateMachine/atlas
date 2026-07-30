// Command stitch builds the z13 canvases embedded by the gamemap demo.
// It only emits maps with a snapshot and a complete tile set for every layer.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	zoom      = 13
	firstTile = 4064
	tileSize  = 256
	gridSize  = 8192
)

type archive struct {
	Games []archiveGame `json:"games"`
}

type archiveGame struct {
	Directory string `json:"directory"`
	Title     string `json:"title"`
}

type snapshotIndex struct {
	CapturedAt  string `json:"capturedAt"`
	ContentHash string `json:"contentHash"`
}

type rawMap struct {
	Title  string    `json:"title"`
	Slug   string    `json:"slug"`
	Config rawConfig `json:"config"`
	Game   rawGame   `json:"game"`
}

type rawGame struct {
	Slug string `json:"slug"`
}

type rawConfig struct {
	TileSets []rawTileSet `json:"tile_sets"`
}

type rawTileSet struct {
	Name   string                     `json:"name"`
	Path   string                     `json:"path"`
	Bounds map[string]rawTileSetBound `json:"bounds"`
}

type rawTileSetBound struct {
	X rawRange `json:"x"`
	Y rawRange `json:"y"`
}

type rawRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type tileRecord struct {
	ContentHash string `json:"contentHash"`
	Status      string `json:"status"`
	TileSetID   int64  `json:"tileSetId"`
	URL         string `json:"url"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Zoom        int    `json:"zoom"`
}

type readyMap struct {
	raw      rawMap
	mapDir   string
	byPath   map[string][]tileRecord
	tileSets []rawTileSet
}

func main() {
	source := flag.String("source", "", "FMG archive root")
	output := flag.String("output", "", "stitched PNG destination")
	flag.Parse()

	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "stitch: -source and -output are required")
		os.Exit(2)
	}
	if err := run(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, "stitch:", err)
		os.Exit(1)
	}
}

func run(source, output string) error {
	var index archive
	if err := readJSON(filepath.Join(source, "archive.json"), &index); err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	for _, game := range index.Games {
		mapDirs, err := filepath.Glob(filepath.Join(source, game.Directory, "maps", "*"))
		if err != nil {
			return err
		}
		sort.Strings(mapDirs)
		for _, mapDir := range mapDirs {
			info, err := os.Stat(mapDir)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				continue
			}
			ready, reason, err := inspectMap(mapDir)
			if err != nil {
				return err
			}
			if ready == nil {
				fmt.Printf("skip %s / %s: %s\n", game.Title, filepath.Base(mapDir), reason)
				continue
			}
			for _, set := range ready.tileSets {
				suffix := ""
				if len(ready.tileSets) > 1 {
					suffix = slugify(set.Name)
				}
				name := imageName(ready.raw.Game.Slug, ready.raw.Slug, suffix)
				destination := filepath.Join(output, name)
				if _, err := os.Stat(destination); err == nil {
					fmt.Printf("keep %s / %s / %s\n", game.Title, ready.raw.Title, set.Name)
					continue
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				fmt.Printf("stitch %s / %s / %s\n", game.Title, ready.raw.Title, set.Name)
				if err := stitch(destination, ready.mapDir, ready.byPath[set.Path]); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func inspectMap(mapDir string) (*readyMap, string, error) {
	var snapshots []snapshotIndex
	if err := readJSON(filepath.Join(mapDir, "snapshots", "index.json"), &snapshots); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "no snapshot index", nil
		}
		return nil, "", err
	}
	if len(snapshots) == 0 {
		return nil, "no captured snapshot", nil
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })

	var raw rawMap
	latest := snapshots[len(snapshots)-1]
	if err := readJSON(filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json"), &raw); err != nil {
		return nil, "", err
	}
	if len(raw.Config.TileSets) == 0 {
		return nil, "no configured tile sets", nil
	}

	var records []tileRecord
	if err := readJSON(filepath.Join(mapDir, "tiles", "index.json"), &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "no tile index", nil
		}
		return nil, "", err
	}
	byPath := make(map[string][]tileRecord)
	for _, record := range records {
		if record.Zoom != zoom || record.Status != "cached" || record.ContentHash == "" {
			continue
		}
		path := tileSetPath(record.URL)
		if path != "" {
			byPath[path] = append(byPath[path], record)
		}
	}

	for _, set := range raw.Config.TileSets {
		tiles := byPath[set.Path]
		if len(tiles) == 0 {
			return nil, fmt.Sprintf("layer %q has no z13 tiles", set.Name), nil
		}
		expected := requiredTileCount(set)
		if len(tiles) != expected {
			return nil, fmt.Sprintf("layer %q has %d/%d z13 tiles", set.Name, len(tiles), expected), nil
		}
		for _, tile := range tiles {
			if _, err := tilePath(mapDir, tile); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Sprintf("layer %q is missing tile %d/%d", set.Name, tile.X, tile.Y), nil
				}
				return nil, "", err
			}
		}
	}

	return &readyMap{
		raw:      raw,
		mapDir:   mapDir,
		byPath:   byPath,
		tileSets: raw.Config.TileSets,
	}, "", nil
}

func requiredTileCount(set rawTileSet) int {
	if bounds, ok := set.Bounds[strconv.Itoa(zoom)]; ok {
		return (bounds.X.Max - bounds.X.Min + 1) * (bounds.Y.Max - bounds.Y.Min + 1)
	}
	return (gridSize / tileSize) * (gridSize / tileSize)
}

func stitch(destination, mapDir string, records []tileRecord) error {
	canvas := image.NewRGBA(image.Rect(0, 0, gridSize, gridSize))
	for _, record := range records {
		x := (record.X - firstTile) * tileSize
		y := (record.Y - firstTile) * tileSize
		if x < 0 || y < 0 || x+tileSize > gridSize || y+tileSize > gridSize {
			return fmt.Errorf("tile %d/%d falls outside the z13 canvas", record.X, record.Y)
		}
		path, err := tilePath(mapDir, record)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		tile, _, decodeErr := image.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode %s: %w", path, decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		draw.Draw(canvas, image.Rect(x, y, x+tileSize, y+tileSize), tile, tile.Bounds().Min, draw.Src)
	}

	temp, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := png.Encode(temp, canvas); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, destination)
}

func tilePath(mapDir string, record tileRecord) (string, error) {
	pattern := filepath.Join(
		mapDir,
		"tiles",
		"set-"+strconv.FormatInt(record.TileSetID, 10),
		strconv.Itoa(zoom),
		strconv.Itoa(record.X),
		strconv.Itoa(record.Y)+".*",
	)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	return matches[0], nil
}

func tileSetPath(rawURL string) string {
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
