package authoring

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func deriveRasterFile(source Raster, capture Capture, cache *Cache) (vnext.RasterPyramid, []vnext.Blob, error) {
	data, err := os.ReadFile(cache.BlobPath(capture.Body))
	if err != nil {
		return vnext.RasterPyramid{}, nil, fmt.Errorf("read raster %s: %w", source.ID, err)
	}
	decoded, err := decodeRaster(data)
	if err != nil {
		return vnext.RasterPyramid{}, nil, fmt.Errorf("decode raster %s: %w", source.ID, err)
	}
	formats := make([]string, source.MaxZoom+1)
	for index := range formats {
		formats[index] = "png"
	}
	fullZoom := source.FullZoom
	if fullZoom == 0 {
		fullZoom = source.MaxZoom
	}
	pyramid := vnext.RasterPyramid{
		ID: "sample", Name: source.Name, Codec: "image/png", TileSize: source.TileSize,
		MinZoom: 0, MaxZoom: source.MaxZoom, FullZoom: fullZoom, SourceZoom: source.SourceZoom,
		Template: "tiles/" + source.ID + "/{z}/{x}/{y}.{format}", Formats: formats,
		Bounds: nativeRect(source.Bounds), Surface: nativeRect(source.Surface),
		Interpolate: source.Interpolate, Background: source.Background, Shard: source.Shard,
	}
	var blobs []vnext.Blob
	for zoom := int64(0); zoom <= source.MaxZoom; zoom++ {
		tiles := int64(1) << zoom
		for y := int64(0); y < tiles; y++ {
			for x := int64(0); x < tiles; x++ {
				data, err := renderRasterTile(decoded, source.TileSize, zoom, x, y)
				if err != nil {
					return vnext.RasterPyramid{}, nil, err
				}
				blobs = append(blobs, vnext.Blob{
					Name: fmt.Sprintf("tiles/%s/%d/%d/%d.png", source.ID, zoom, x, y), Data: data,
				})
			}
		}
	}
	return pyramid, blobs, nil
}

func decodeRaster(data []byte) (image.Image, error) {
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return decoded, nil
	}
	return decodeP3(data)
}

func decodeP3(data []byte) (image.Image, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	var magic string
	var width, height, maximum int
	if _, err := fmt.Fscan(reader, &magic, &width, &height, &maximum); err != nil {
		return nil, err
	}
	if magic != "P3" || width <= 0 || height <= 0 || maximum <= 0 || maximum > 65535 {
		return nil, fmt.Errorf("unsupported raster encoding")
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var red, green, blue int
			if _, err := fmt.Fscan(reader, &red, &green, &blue); err != nil {
				return nil, fmt.Errorf("read pixel %d,%d: %w", x, y, err)
			}
			if red < 0 || red > maximum || green < 0 || green > maximum || blue < 0 || blue > maximum {
				return nil, fmt.Errorf("pixel %d,%d exceeds raster range", x, y)
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(red * 255 / maximum), G: uint8(green * 255 / maximum), B: uint8(blue * 255 / maximum), A: 255,
			})
		}
	}
	return out, nil
}

func renderRasterTile(source image.Image, tileSize, zoom, tileX, tileY int64) ([]byte, error) {
	tile := image.NewNRGBA(image.Rect(0, 0, int(tileSize), int(tileSize)))
	bounds := source.Bounds()
	span := tileSize * (int64(1) << zoom)
	for y := int64(0); y < tileSize; y++ {
		for x := int64(0); x < tileSize; x++ {
			sourceX := bounds.Min.X + int((tileX*tileSize+x)*int64(bounds.Dx())/span)
			sourceY := bounds.Min.Y + int((tileY*tileSize+y)*int64(bounds.Dy())/span)
			if sourceX >= bounds.Max.X {
				sourceX = bounds.Max.X - 1
			}
			if sourceY >= bounds.Max.Y {
				sourceY = bounds.Max.Y - 1
			}
			tile.Set(int(x), int(y), source.At(sourceX, sourceY))
		}
	}
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, tile); err != nil {
		return nil, fmt.Errorf("encode canonical raster tile: %w", err)
	}
	return out.Bytes(), nil
}

func nativeRect(source *RasterRect) *vnext.RasterRect {
	if source == nil {
		return nil
	}
	return &vnext.RasterRect{X: source.X, Y: source.Y, Width: source.Width, Height: source.Height}
}
