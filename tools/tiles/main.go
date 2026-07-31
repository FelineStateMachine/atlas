// Command tiles prepares bounded, multi-resolution raster tile pyramids from
// the FMG archive. Complete photographic source levels retain their encoded
// tiles; pixel-art levels are normalized to lossless PNG. Missing lower levels
// are derived from the next complete or generated level.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	referenceZoom  = 13
	baseSourceZoom = 8
	firstTile      = 4064
	tileSize       = 256
	worldSize      = 8192
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
	Extension string                     `json:"extension"`
	MaxZoom   int                        `json:"max_zoom"`
	MinZoom   int                        `json:"min_zoom"`
	Name      string                     `json:"name"`
	Path      string                     `json:"path"`
	Bounds    map[string]rawTileSetBound `json:"bounds"`
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

type tileFile struct {
	Record tileRecord
	Path   string
	Format string
}

type tilePlan struct {
	AssetPath string
	Bounds    *contentBounds
	// Levels holds every usable source level. Levels at or below MaxFullZoom
	// cover the map completely; deeper ones may be partially captured and are
	// carried as extra detail on top of the complete pyramid.
	Levels          map[int][]tileFile
	Interpolate     bool
	MapDir          string
	MaxFullZoom     int
	MaxSourceZoom   int
	SourcePath      string
	PreferredFormat string
}

type manifest struct {
	TileSize int               `json:"tileSize"`
	Size     int               `json:"size"`
	Variants []variantManifest `json:"variants"`
}

type variantManifest struct {
	SourcePath string `json:"sourcePath"`
	AssetPath  string `json:"assetPath"`
	MinZoom    int    `json:"minZoom"`
	MaxZoom    int    `json:"maxZoom"`
	// FullZoom is the deepest level with complete coverage. Levels beyond it
	// exist only where Coverage says so; the viewer falls back to the parent
	// tile elsewhere.
	FullZoom    int                       `json:"fullZoom"`
	SourceZoom  int                       `json:"sourceZoom"`
	Formats     []string                  `json:"formats"`
	Bounds      *contentBounds            `json:"bounds,omitempty"`
	Interpolate bool                      `json:"interpolate"`
	Background  string                    `json:"background,omitempty"`
	Coverage    map[string]*levelCoverage `json:"coverage,omitempty"`
}

// levelCoverage is a row-major bitset over the [X,Y,W,H] tile window of one
// level. A set bit means the tile was written.
type levelCoverage struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Bits string `json:"bits"`
}

// coverageBuilder accumulates the tiles actually written for one level and
// encodes them as a bitset once the level is finished.
type coverageBuilder struct {
	present map[[2]int]bool
	total   int
}

func (b *coverageBuilder) mark(x, y int) {
	if b.present == nil {
		b.present = make(map[[2]int]bool)
	}
	b.present[[2]int{x, y}] = true
}

// build returns nil when the level is fully covered, so complete levels cost
// nothing in the manifest.
func (b *coverageBuilder) build() *levelCoverage {
	if len(b.present) == 0 || len(b.present) == b.total {
		return nil
	}
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	maxX, maxY := -1, -1
	for key := range b.present {
		minX, minY = min(minX, key[0]), min(minY, key[1])
		maxX, maxY = max(maxX, key[0]), max(maxY, key[1])
	}
	width, height := maxX-minX+1, maxY-minY+1
	bits := make([]byte, (width*height+7)/8)
	for key := range b.present {
		index := (key[1]-minY)*width + (key[0] - minX)
		bits[index/8] |= 1 << (index % 8)
	}
	return &levelCoverage{
		X:    minX,
		Y:    minY,
		W:    width,
		H:    height,
		Bits: base64.StdEncoding.EncodeToString(bits),
	}
}

type contentBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func main() {
	source := flag.String("source", "", "FMG archive root")
	output := flag.String("output", "", "embedded tile destination")
	flag.Parse()

	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "tiles: -source and -output are required")
		os.Exit(2)
	}
	if err := run(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, "tiles:", err)
		os.Exit(1)
	}
}

func run(source, output string) error {
	var index archive
	if err := readJSON(filepath.Join(source, "archive.json"), &index); err != nil {
		return err
	}

	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	temp, err := os.MkdirTemp(parent, "."+filepath.Base(output)+"-")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	defer os.RemoveAll(temp)

	out := manifest{TileSize: tileSize, Size: worldSize}
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
			plans, title, reason, err := inspectMap(mapDir)
			if err != nil {
				return err
			}
			if len(plans) == 0 {
				fmt.Printf("skip %s / %s: %s\n", game.Title, filepath.Base(mapDir), reason)
				continue
			}
			for _, plan := range plans {
				fmt.Printf("tile %s / %s / %s\n", game.Title, title, plan.SourcePath)
				entry, err := buildPyramid(temp, plan)
				if err != nil {
					return fmt.Errorf("%s / %s: %w", title, plan.SourcePath, err)
				}
				out.Variants = append(out.Variants, entry)
			}
		}
	}
	sort.Slice(out.Variants, func(i, j int) bool {
		return out.Variants[i].SourcePath < out.Variants[j].SourcePath
	})
	if err := writeJSON(filepath.Join(temp, "index.json"), out); err != nil {
		return err
	}
	if err := replaceDirectory(temp, output); err != nil {
		return err
	}
	return nil
}

func inspectMap(mapDir string) ([]tilePlan, string, string, error) {
	var snapshots []snapshotIndex
	if err := readJSON(filepath.Join(mapDir, "snapshots", "index.json"), &snapshots); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", "no snapshot index", nil
		}
		return nil, "", "", err
	}
	if len(snapshots) == 0 {
		return nil, "", "no captured snapshot", nil
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt < snapshots[j].CapturedAt })

	var raw rawMap
	latest := snapshots[len(snapshots)-1]
	if err := readJSON(filepath.Join(mapDir, "snapshots", "map", latest.ContentHash+".json"), &raw); err != nil {
		return nil, "", "", err
	}
	if len(raw.Config.TileSets) == 0 {
		return nil, raw.Title, "no configured tile sets", nil
	}

	var records []tileRecord
	if err := readJSON(filepath.Join(mapDir, "tiles", "index.json"), &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, raw.Title, "no tile index", nil
		}
		return nil, "", "", err
	}
	byPath := make(map[string]map[int][]tileRecord)
	for _, record := range records {
		if record.Status != "cached" || record.ContentHash == "" {
			continue
		}
		path := tileSetPath(record.URL, record.Zoom)
		if path == "" {
			continue
		}
		if byPath[path] == nil {
			byPath[path] = make(map[int][]tileRecord)
		}
		byPath[path][record.Zoom] = append(byPath[path][record.Zoom], record)
	}

	plans := make([]tilePlan, 0, len(raw.Config.TileSets))
	for _, set := range raw.Config.TileSets {
		levels := make(map[int][]tileFile)
		maxFullZoom, maxSourceZoom := -1, -1
		for zoom, level := range byPath[set.Path] {
			if zoom < baseSourceZoom || zoom > set.MaxZoom {
				continue
			}
			files, full, err := readLevel(mapDir, set, zoom, level)
			if err != nil {
				return nil, "", "", err
			}
			if len(files) == 0 {
				continue
			}
			levels[zoom] = files
			maxSourceZoom = max(maxSourceZoom, zoom)
			if full {
				maxFullZoom = max(maxFullZoom, zoom)
			}
		}
		if maxFullZoom < baseSourceZoom {
			return nil, raw.Title, fmt.Sprintf("layer %q has no complete source level", set.Name), nil
		}
		// A partial level is only usable if every level beneath it exists;
		// otherwise the viewer would jump across a missing resolution.
		for zoom := maxFullZoom + 1; zoom <= maxSourceZoom; zoom++ {
			if len(levels[zoom]) == 0 {
				maxSourceZoom = zoom - 1
				break
			}
		}
		assetPath := raw.Game.Slug + "__" + raw.Slug
		if len(raw.Config.TileSets) > 1 {
			assetPath += "__" + slugify(set.Name)
		}
		plans = append(plans, tilePlan{
			AssetPath:       assetPath,
			Bounds:          contentBoundsFor(levels[maxFullZoom], maxFullZoom),
			Levels:          levels,
			Interpolate:     !isPixelArt(raw.Game.Slug),
			MapDir:          mapDir,
			MaxFullZoom:     maxFullZoom,
			MaxSourceZoom:   maxSourceZoom,
			PreferredFormat: normalizeFormat(set.Extension),
			SourcePath:      set.Path,
		})
	}
	return plans, raw.Title, "", nil
}

// readLevel collects every cached tile of one source level. It reports whether
// the level covers its expected window completely; partial levels are still
// returned so they can be carried as extra detail above the complete pyramid.
func readLevel(mapDir string, set rawTileSet, zoom int, records []tileRecord) ([]tileFile, bool, error) {
	bounds := expectedBounds(set, zoom)
	expected := (bounds.X.Max - bounds.X.Min + 1) * (bounds.Y.Max - bounds.Y.Min + 1)

	seen := make(map[[2]int]bool, len(records))
	files := make([]tileFile, 0, len(records))
	for _, record := range records {
		key := [2]int{record.X, record.Y}
		if seen[key] || record.X < bounds.X.Min || record.X > bounds.X.Max ||
			record.Y < bounds.Y.Min || record.Y > bounds.Y.Max {
			continue
		}
		path, err := sourceTilePath(mapDir, record)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		seen[key] = true
		files = append(files, tileFile{
			Record: record,
			Path:   path,
			Format: normalizeFormat(strings.TrimPrefix(filepath.Ext(path), ".")),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Record.Y != files[j].Record.Y {
			return files[i].Record.Y < files[j].Record.Y
		}
		return files[i].Record.X < files[j].Record.X
	})
	return files, expected > 0 && len(files) == expected, nil
}

func expectedBounds(set rawTileSet, zoom int) rawTileSetBound {
	if bounds, ok := set.Bounds[strconv.Itoa(zoom)]; ok {
		return bounds
	}
	minimum, maximum := worldTileRange(zoom)
	return rawTileSetBound{
		X: rawRange{Min: minimum, Max: maximum},
		Y: rawRange{Min: minimum, Max: maximum},
	}
}

func worldTileRange(zoom int) (int, int) {
	const worldTilesAtReference = worldSize / tileSize
	if zoom <= referenceZoom {
		shift := referenceZoom - zoom
		return firstTile >> shift, ((firstTile + worldTilesAtReference) >> shift) - 1
	}
	shift := zoom - referenceZoom
	return firstTile << shift, ((firstTile + worldTilesAtReference) << shift) - 1
}

func buildPyramid(root string, plan tilePlan) (variantManifest, error) {
	maxZoom := plan.MaxSourceZoom - baseSourceZoom
	fullZoom := plan.MaxFullZoom - baseSourceZoom
	formats := make([]string, maxZoom+1)
	coverage := make(map[string]*levelCoverage)

	// Resolve the background tile once, from the level we trust. Reusing one
	// hash across every level keeps a sparse deep level from having its handful
	// of real tiles voted "background" by its own majority, and skipping the
	// omission entirely when no flat colour could be sampled means we never
	// leave a hole we cannot paint over.
	full := plan.Levels[plan.MaxFullZoom]
	placeholder := placeholderHash(full)
	backgroundHex, err := placeholderColor(full, placeholder)
	if err != nil {
		return variantManifest{}, err
	}
	if backgroundHex == "" {
		placeholder = ""
	}
	background := parseHexColor(backgroundHex)

	// The deepest complete level is the one we trust. Everything below it is
	// folded down from it rather than taken from whatever intermediate levels
	// the capture happens to hold: those are separately-encoded JPEGs of
	// varying completeness, and a box filter over the level we already have is
	// both more consistent and never leaves a level empty.
	format, mask, err := copyLevel(root, plan, plan.MaxFullZoom, fullZoom, full, placeholder)
	if err != nil {
		return variantManifest{}, err
	}
	formats[fullZoom] = format
	if mask != nil {
		coverage[strconv.Itoa(fullZoom)] = mask
	}
	for localZoom := fullZoom - 1; localZoom >= 0; localZoom-- {
		derivedFormat := plan.PreferredFormat
		if !plan.Interpolate || derivedFormat == "png" {
			derivedFormat = "png"
		}
		mask, err := deriveLevel(root, plan.AssetPath, localZoom, formats[localZoom+1], derivedFormat, !plan.Interpolate, background)
		if err != nil {
			return variantManifest{}, err
		}
		formats[localZoom] = derivedFormat
		if mask != nil {
			coverage[strconv.Itoa(localZoom)] = mask
		}
	}

	// Partially captured levels above it. These have gaps by definition, so the
	// viewer falls back to the parent tile wherever coverage says nothing. A
	// level that survives with nothing in it ends the pyramid: advertising a
	// zoom with no tiles would only produce misses.
	for localZoom := fullZoom + 1; localZoom <= maxZoom; localZoom++ {
		sourceZoom := localZoom + baseSourceZoom
		format, mask, err := copyLevel(root, plan, sourceZoom, localZoom, plan.Levels[sourceZoom], placeholder)
		if err != nil {
			return variantManifest{}, err
		}
		if mask == nil {
			maxZoom = localZoom - 1
			formats = formats[:localZoom]
			break
		}
		formats[localZoom] = format
		coverage[strconv.Itoa(localZoom)] = mask
	}

	if len(coverage) == 0 {
		coverage = nil
	}
	return variantManifest{
		SourcePath:  plan.SourcePath,
		AssetPath:   plan.AssetPath,
		MinZoom:     0,
		MaxZoom:     maxZoom,
		FullZoom:    fullZoom,
		SourceZoom:  plan.MaxSourceZoom,
		Formats:     formats,
		Bounds:      plan.Bounds,
		Interpolate: plan.Interpolate,
		Background:  backgroundHex,
		Coverage:    coverage,
	}, nil
}

// parseHexColor turns "#rrggbb" into a colour, defaulting to opaque black when
// the level had no flat background to sample.
func parseHexColor(value string) color.Color {
	if len(value) != 7 || value[0] != '#' {
		return color.NRGBA{A: 0xff}
	}
	component, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return color.NRGBA{A: 0xff}
	}
	return color.NRGBA{
		R: uint8(component >> 16),
		G: uint8(component >> 8),
		B: uint8(component),
		A: 0xff,
	}
}

// copyLevel writes one source level, omitting tiles that are byte-identical
// copies of the level's background placeholder. Source bytes are passed through
// untouched: re-encoding cannot add detail the capture never had, and for the
// JPEG-sourced pixel-art maps it only stores compression noise at a larger size.
func copyLevel(
	root string,
	plan tilePlan,
	sourceZoom int,
	localZoom int,
	files []tileFile,
	placeholder string,
) (string, *levelCoverage, error) {
	if len(files) == 0 {
		return "", nil, fmt.Errorf("source level %d has no tiles", sourceZoom)
	}
	outputFormat := files[0].Format
	origin, _ := worldTileRange(sourceZoom)
	span := 1 << localZoom
	builder := &coverageBuilder{total: span * span}

	for _, file := range files {
		if file.Format != outputFormat {
			return "", nil, fmt.Errorf("source level %d mixes %s and %s", sourceZoom, outputFormat, file.Format)
		}
		if placeholder != "" && file.Record.ContentHash == placeholder {
			continue
		}
		x := file.Record.X - origin
		y := file.Record.Y - origin
		if err := copyFile(file.Path, tilePath(root, plan.AssetPath, localZoom, x, y, outputFormat)); err != nil {
			return "", nil, err
		}
		builder.mark(x, y)
	}
	return outputFormat, builder.build(), nil
}

// placeholderHash returns the content hash of the level's background tile, or
// "" when no single tile holds a majority. The majority rule matches
// contentBoundsFor: only an unambiguous filler tile is treated as empty space,
// so a map whose real content happens to repeat is never punched full of holes.
func placeholderHash(files []tileFile) string {
	counts := make(map[string]int, len(files))
	var dominant string
	for _, file := range files {
		hash := file.Record.ContentHash
		counts[hash]++
		if counts[hash] > counts[dominant] {
			dominant = hash
		}
	}
	if dominant == "" || counts[dominant] <= len(files)/2 {
		return ""
	}
	return dominant
}

// placeholderColor reports the flat colour of the level's background tile so
// the viewer can paint it behind the raster layer. Tiles we omit then look
// exactly as they did when they were shipped as files. It returns "" when the
// tile is not flat enough for a single colour to stand in for it.
func placeholderColor(files []tileFile, hash string) (string, error) {
	if hash == "" {
		return "", nil
	}
	var sample string
	for _, file := range files {
		if file.Record.ContentHash == hash {
			sample = file.Path
			break
		}
	}
	if sample == "" {
		return "", nil
	}
	value, err := decodeImage(sample)
	if err != nil {
		return "", err
	}
	bounds := value.Bounds()
	var sums [3]int64
	var count int64
	minimum := [3]uint32{^uint32(0), ^uint32(0), ^uint32(0)}
	maximum := [3]uint32{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 4 {
			r, g, b, _ := value.At(x, y).RGBA()
			for channel, component := range [3]uint32{r >> 8, g >> 8, b >> 8} {
				sums[channel] += int64(component)
				minimum[channel] = min(minimum[channel], component)
				maximum[channel] = max(maximum[channel], component)
			}
			count++
		}
	}
	if count == 0 {
		return "", nil
	}
	for channel := range minimum {
		if maximum[channel]-minimum[channel] > 12 {
			return "", nil
		}
	}
	return fmt.Sprintf("#%02x%02x%02x",
		sums[0]/count, sums[1]/count, sums[2]/count), nil
}

// deriveLevel folds the level above down by two. Parents with no surviving
// children are skipped, so omitted background propagates down the pyramid.
func deriveLevel(
	root, assetPath string,
	zoom int,
	childFormat, outputFormat string,
	nearest bool,
	background color.Color,
) (*levelCoverage, error) {
	childRoot := filepath.Join(root, assetPath, strconv.Itoa(zoom+1))
	parents := make(map[[2]int]bool)
	err := filepath.WalkDir(childRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || normalizeFormat(strings.TrimPrefix(filepath.Ext(path), ".")) != childFormat {
			return nil
		}
		y, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if err != nil {
			return fmt.Errorf("decode child y from %s: %w", path, err)
		}
		x, err := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		if err != nil {
			return fmt.Errorf("decode child x from %s: %w", path, err)
		}
		parents[[2]int{x / 2, y / 2}] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	keys := make([][2]int, 0, len(parents))
	for key := range parents {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})
	span := 1 << zoom
	builder := &coverageBuilder{total: span * span}
	for _, parent := range keys {
		composite := image.NewNRGBA(image.Rect(0, 0, tileSize*2, tileSize*2))
		// Quadrants whose child was omitted as background must still carry the
		// background colour: a derived JPEG has no alpha to fall back on.
		draw.Draw(composite, composite.Bounds(), &image.Uniform{background}, image.Point{}, draw.Src)
		for offsetY := 0; offsetY < 2; offsetY++ {
			for offsetX := 0; offsetX < 2; offsetX++ {
				childX := parent[0]*2 + offsetX
				childY := parent[1]*2 + offsetY
				path := tilePath(root, assetPath, zoom+1, childX, childY, childFormat)
				child, err := decodeImage(path)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, err
				}
				target := image.Rect(offsetX*tileSize, offsetY*tileSize, (offsetX+1)*tileSize, (offsetY+1)*tileSize)
				draw.Draw(composite, target, child, child.Bounds().Min, draw.Src)
			}
		}
		parentImage := downsample(composite, nearest)
		destination := tilePath(root, assetPath, zoom, parent[0], parent[1], outputFormat)
		if err := encodeImage(destination, parentImage, outputFormat); err != nil {
			return nil, err
		}
		builder.mark(parent[0], parent[1])
	}
	return builder.build(), nil
}

func downsample(source *image.NRGBA, nearest bool) *image.NRGBA {
	target := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			destination := target.PixOffset(x, y)
			sourceX, sourceY := x*2, y*2
			if nearest {
				copy(target.Pix[destination:destination+4], source.Pix[source.PixOffset(sourceX, sourceY):][:4])
				continue
			}
			offsets := [4]int{
				source.PixOffset(sourceX, sourceY),
				source.PixOffset(sourceX+1, sourceY),
				source.PixOffset(sourceX, sourceY+1),
				source.PixOffset(sourceX+1, sourceY+1),
			}
			for channel := 0; channel < 4; channel++ {
				sum := 0
				for _, offset := range offsets {
					sum += int(source.Pix[offset+channel])
				}
				target.Pix[destination+channel] = uint8((sum + 2) / 4)
			}
		}
	}
	return target
}

func contentBoundsFor(files []tileFile, zoom int) *contentBounds {
	counts := make(map[string]int)
	var placeholder string
	for _, file := range files {
		hash := file.Record.ContentHash
		counts[hash]++
		if counts[hash] > counts[placeholder] {
			placeholder = hash
		}
	}
	excludePlaceholder := counts[placeholder] > len(files)/2
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	maxX, maxY := -1, -1
	for _, file := range files {
		if excludePlaceholder && file.Record.ContentHash == placeholder {
			continue
		}
		minX = min(minX, file.Record.X)
		minY = min(minY, file.Record.Y)
		maxX = max(maxX, file.Record.X)
		maxY = max(maxY, file.Record.Y)
	}
	if maxX < minX || maxY < minY {
		return nil
	}
	// Bounds are reported in the shared world space, which is sized at the
	// reference zoom. A shallower level covers more world per tile and a deeper
	// one covers less, so the tile pitch is scaled both ways.
	origin, _ := worldTileRange(zoom)
	pitch, divisor := tileSize, 1
	if zoom < referenceZoom {
		pitch = tileSize << (referenceZoom - zoom)
	} else if zoom > referenceZoom {
		divisor = 1 << (zoom - referenceZoom)
	}
	bounds := &contentBounds{
		X:      (minX - origin) * pitch / divisor,
		Y:      (minY - origin) * pitch / divisor,
		Width:  (maxX - minX + 1) * pitch / divisor,
		Height: (maxY - minY + 1) * pitch / divisor,
	}
	if bounds.X == 0 && bounds.Y == 0 && bounds.Width == worldSize && bounds.Height == worldSize {
		return nil
	}
	return bounds
}

func sourceTilePath(mapDir string, record tileRecord) (string, error) {
	pattern := filepath.Join(
		mapDir,
		"tiles",
		"set-"+strconv.FormatInt(record.TileSetID, 10),
		strconv.Itoa(record.Zoom),
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
	sort.Strings(matches)
	return matches[0], nil
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

func tilePath(root, assetPath string, zoom, x, y int, format string) string {
	return filepath.Join(root, assetPath, strconv.Itoa(zoom), strconv.Itoa(x), strconv.Itoa(y)+"."+format)
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func decodeImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	value, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return value, nil
}

func encodeImage(path string, value image.Image, format string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	var encodeErr error
	switch format {
	case "png":
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		encodeErr = encoder.Encode(file, value)
	default:
		encodeErr = jpeg.Encode(file, value, &jpeg.Options{Quality: 90})
	}
	if encodeErr != nil {
		file.Close()
		return fmt.Errorf("encode %s: %w", path, encodeErr)
	}
	return file.Close()
}

func replaceDirectory(temp, output string) error {
	backup := output + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if _, err := os.Stat(output); err == nil {
		if err := os.Rename(output, backup); err != nil {
			return fmt.Errorf("backup old tile output: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temp, output); err != nil {
		_ = os.Rename(backup, output)
		return fmt.Errorf("install tile output: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove old tile output: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, destination any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func normalizeFormat(value string) string {
	value = strings.ToLower(strings.TrimPrefix(value, "."))
	if value == "jpeg" {
		return "jpg"
	}
	if value != "png" {
		return "jpg"
	}
	return value
}

// isPixelArt marks maps drawn on a pixel grid. They are folded down with
// nearest-neighbour sampling and rendered without interpolation, so the grid
// survives instead of being blurred into it.
func isPixelArt(gameSlug string) bool {
	if strings.HasPrefix(gameSlug, "pokemon-") {
		return true
	}
	switch gameSlug {
	case "old-school-runescape":
		return true
	}
	return false
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
