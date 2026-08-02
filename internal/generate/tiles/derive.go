package tiles

import (
	"encoding/base64"
	"errors"
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

	// WebP sources decode through image.Decode like any other. Go encodes none,
	// so levels folded down from one are written as JPEG or PNG.
	_ "golang.org/x/image/webp"
)

// Deriving a pyramid, once a plan has decided what it is.
//
// The shape of the work is one rule and its consequences. The deepest complete
// captured level is the one to trust: it is copied through byte for byte,
// because re-encoding cannot add detail a capture never had and for the
// JPEG-sourced game maps it only stores compression noise at a larger size.
// Every shallower level is folded down from the level above it -- not taken from
// whatever intermediate levels the capture happens to hold, which are separately
// encoded pictures of varying completeness. Partial levels above the complete one
// are copied while they stay contiguous, and a reader falls back to the parent
// tile wherever coverage says nothing.
//
// A level's background tile is found once, on the level we trust, and omitted
// everywhere. Reusing one hash across every level keeps a sparse deep level from
// having its handful of real tiles voted "background" by its own majority, and
// skipping the omission entirely when no flat colour could be sampled means a
// hole is never left that nothing can paint over.

// Derive builds one pyramid under root and reports it as a register entry. The
// entry carries no stamp: stamping is the caller's, because a caller that
// carried a pyramid over rather than deriving it stamps the same way.
func Derive(root string, plan Plan) (Pyramid, error) {
	if plan.Warp != nil {
		return Pyramid{}, fmt.Errorf(
			"pyramid %s is a warped variant, which this deriver does not build yet "+
				"(see docs/generate.md, the tile deriver)", plan.Name)
	}
	maxZoom := plan.MaxSourceZoom - plan.Frame.BaseZoom
	fullZoom := plan.MaxFullZoom - plan.Frame.BaseZoom
	formats := make([]string, maxZoom+1)
	coverage := make(map[string]*Coverage)

	full := plan.Levels[plan.MaxFullZoom]
	filler := placeholder(full)
	backgroundHex, err := placeholderColor(full, filler)
	if err != nil {
		return Pyramid{}, err
	}
	if backgroundHex == "" {
		// Nothing flat enough to stand in for the omitted tiles, so nothing is
		// omitted: a hole nobody can paint over is worse than a duplicated tile.
		filler = ""
	}
	background := parseHex(backgroundHex)

	format, mask, err := copyLevel(root, plan, plan.MaxFullZoom, fullZoom, full, filler)
	if err != nil {
		return Pyramid{}, err
	}
	formats[fullZoom] = format
	if mask != nil {
		coverage[strconv.Itoa(fullZoom)] = mask
	}

	for localZoom := fullZoom - 1; localZoom >= 0; localZoom-- {
		derived := plan.Format
		if !plan.Interpolate || derived == "png" {
			derived = "png"
		}
		// A WebP source level copies through as it came, but nothing here
		// encodes WebP, so the levels folded down from it take JPEG.
		if derived == "webp" {
			derived = "jpg"
		}
		mask, err := deriveLevel(root, plan.Name, localZoom,
			formats[localZoom+1], derived, !plan.Interpolate, background)
		if err != nil {
			return Pyramid{}, err
		}
		formats[localZoom] = derived
		if mask != nil {
			coverage[strconv.Itoa(localZoom)] = mask
		}
	}

	// Partially captured levels above the complete one. These have gaps by
	// definition. A level that survives with nothing in it ends the pyramid:
	// advertising a zoom with no tiles would only produce misses.
	for localZoom := fullZoom + 1; localZoom <= maxZoom; localZoom++ {
		sourceZoom := localZoom + plan.Frame.BaseZoom
		format, mask, err := copyLevel(root, plan, sourceZoom, localZoom, plan.Levels[sourceZoom], filler)
		if err != nil {
			return Pyramid{}, err
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
	sourceZoom, firstTile := plan.Frame.Window()
	return Pyramid{
		TileSet:     plan.TileSet,
		Name:        plan.Name,
		MinZoom:     0,
		MaxZoom:     maxZoom,
		FullZoom:    fullZoom,
		SourceZoom:  plan.MaxSourceZoom,
		Window:      Window{SourceZoom: sourceZoom, FirstTile: firstTile},
		Formats:     formats,
		Bounds:      plan.Bounds,
		Interpolate: plan.Interpolate,
		Background:  backgroundHex,
		Coverage:    coverage,
		LensName:    plan.LensName,
		AlignedWith: plan.AlignedWith,
	}, nil
}

// copyLevel writes one captured level through, omitting tiles that are
// byte-identical copies of the level's background.
func copyLevel(
	root string,
	plan Plan,
	sourceZoom, localZoom int,
	tiles []Tile,
	filler string,
) (string, *Coverage, error) {
	if len(tiles) == 0 {
		return "", nil, fmt.Errorf("captured level %d holds no tile", sourceZoom)
	}
	format := tiles[0].Format
	origin := plan.Frame.Origin(sourceZoom)
	span := 1 << localZoom
	mask := &coverageMask{total: span * span}

	for _, tile := range tiles {
		if tile.Format != format {
			return "", nil, fmt.Errorf("captured level %d mixes %s and %s", sourceZoom, format, tile.Format)
		}
		if filler != "" && tile.Ref.ContentHash == filler {
			continue
		}
		x, y := tile.Ref.X-origin, tile.Ref.Y-origin
		if err := copyFile(tile.Path, tilePath(root, plan.Name, localZoom, x, y, format)); err != nil {
			return "", nil, err
		}
		mask.mark(x, y)
	}
	return format, mask.build(), nil
}

// deriveLevel folds the level above down by two. Parents with no surviving
// children are skipped, so omitted background propagates down the pyramid.
func deriveLevel(
	root, name string,
	zoom int,
	childFormat, format string,
	nearest bool,
	background color.Color,
) (*Coverage, error) {
	childRoot := filepath.Join(root, name, strconv.Itoa(zoom+1))
	parents := make(map[[2]int]bool)
	err := filepath.WalkDir(childRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || normalizeExt(filepath.Ext(path)) != childFormat {
			return nil
		}
		y, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if err != nil {
			return fmt.Errorf("read child row from %s: %w", path, err)
		}
		x, err := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		if err != nil {
			return fmt.Errorf("read child column from %s: %w", path, err)
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
	mask := &coverageMask{total: span * span}
	for _, parent := range keys {
		composite := image.NewNRGBA(image.Rect(0, 0, TileSize*2, TileSize*2))
		// A quadrant whose child was omitted as background must still carry the
		// background colour: a derived JPEG has no alpha to fall back on.
		draw.Draw(composite, composite.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
		for offsetY := range 2 {
			for offsetX := range 2 {
				path := tilePath(root, name, zoom+1, parent[0]*2+offsetX, parent[1]*2+offsetY, childFormat)
				child, err := decode(path)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, err
				}
				target := image.Rect(offsetX*TileSize, offsetY*TileSize,
					(offsetX+1)*TileSize, (offsetY+1)*TileSize)
				draw.Draw(composite, target, child, child.Bounds().Min, draw.Src)
			}
		}
		if err := encode(tilePath(root, name, zoom, parent[0], parent[1], format),
			downsample(composite, nearest), format); err != nil {
			return nil, err
		}
		mask.mark(parent[0], parent[1])
	}
	return mask.build(), nil
}

// downsample halves a 2x2 composite into one tile. Pixel art is sampled
// nearest-neighbour so the grid survives instead of being blurred into itself;
// everything else is a box filter over the four pixels that become one.
func downsample(source *image.NRGBA, nearest bool) *image.NRGBA {
	target := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for y := range TileSize {
		for x := range TileSize {
			at := target.PixOffset(x, y)
			sourceX, sourceY := x*2, y*2
			if nearest {
				copy(target.Pix[at:at+4], source.Pix[source.PixOffset(sourceX, sourceY):][:4])
				continue
			}
			offsets := [4]int{
				source.PixOffset(sourceX, sourceY),
				source.PixOffset(sourceX+1, sourceY),
				source.PixOffset(sourceX, sourceY+1),
				source.PixOffset(sourceX+1, sourceY+1),
			}
			for channel := range 4 {
				sum := 0
				for _, offset := range offsets {
					sum += int(source.Pix[offset+channel])
				}
				target.Pix[at+channel] = uint8((sum + 2) / 4)
			}
		}
	}
	return target
}

// placeholderColor reports the flat colour of a level's background tile, so a
// reader can paint it behind the raster and an omitted tile looks exactly as it
// did when it was shipped as a file. It answers "" when the tile is not flat
// enough for one colour to stand in for it.
func placeholderColor(tiles []Tile, hash string) (string, error) {
	if hash == "" {
		return "", nil
	}
	var sample string
	for _, tile := range tiles {
		if tile.Ref.ContentHash == hash {
			sample = tile.Path
			break
		}
	}
	if sample == "" {
		return "", nil
	}
	value, err := decode(sample)
	if err != nil {
		return "", err
	}
	bounds := value.Bounds()
	var sums [3]int64
	var count int64
	lowest := [3]uint32{^uint32(0), ^uint32(0), ^uint32(0)}
	highest := [3]uint32{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 4 {
			r, g, b, _ := value.At(x, y).RGBA()
			for channel, component := range [3]uint32{r >> 8, g >> 8, b >> 8} {
				sums[channel] += int64(component)
				lowest[channel] = min(lowest[channel], component)
				highest[channel] = max(highest[channel], component)
			}
			count++
		}
	}
	if count == 0 {
		return "", nil
	}
	for channel := range lowest {
		if highest[channel]-lowest[channel] > 12 {
			return "", nil
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", sums[0]/count, sums[1]/count, sums[2]/count), nil
}

// parseHex turns "#rrggbb" into a colour, defaulting to opaque black when the
// level had no flat background to sample.
func parseHex(value string) color.Color {
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

// coverageMask accumulates the tiles actually written for one level and encodes
// them as a bitset once the level is finished.
type coverageMask struct {
	present map[[2]int]bool
	total   int
}

func (m *coverageMask) mark(x, y int) {
	if m.present == nil {
		m.present = make(map[[2]int]bool)
	}
	m.present[[2]int{x, y}] = true
}

// build answers nil when the level is empty or exactly full, so a complete level
// costs nothing in the register and an absent entry reads as "all of it".
func (m *coverageMask) build() *Coverage {
	if len(m.present) == 0 || len(m.present) == m.total {
		return nil
	}
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	maxX, maxY := -1, -1
	for key := range m.present {
		minX, minY = min(minX, key[0]), min(minY, key[1])
		maxX, maxY = max(maxX, key[0]), max(maxY, key[1])
	}
	width, height := maxX-minX+1, maxY-minY+1
	bits := make([]byte, (width*height+7)/8)
	for key := range m.present {
		index := (key[1]-minY)*width + (key[0] - minX)
		bits[index/8] |= 1 << (index % 8)
	}
	return &Coverage{
		X: minX, Y: minY, W: width, H: height,
		Bits: base64.StdEncoding.EncodeToString(bits),
	}
}

func tilePath(root, name string, zoom, x, y int, format string) string {
	return filepath.Join(root, name, strconv.Itoa(zoom), strconv.Itoa(x), strconv.Itoa(y)+"."+format)
}

func normalizeExt(ext string) string {
	value := strings.ToLower(strings.TrimPrefix(ext, "."))
	switch value {
	case "jpeg":
		return "jpg"
	case "png", "webp":
		return value
	default:
		return "jpg"
	}
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func decode(path string) (image.Image, error) {
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

func encode(path string, value image.Image, format string) error {
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
