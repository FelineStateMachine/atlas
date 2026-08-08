package vnext

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	rasterIndexName     = "indexes/rasters.v1.json"
	rasterShardPrefix   = "data/raster-shards/"
	maxRasterShardBytes = int64(64 << 20)
	maxRasterShardTiles = 4_096
)

type rasterTileIndex struct {
	Version uint16                 `json:"version"`
	Tiles   []rasterTileDescriptor `json:"tiles"`
}

type rasterTileDescriptor struct {
	Name      string `json:"name"`
	Shard     string `json:"shard"`
	Offset    int64  `json:"offset"`
	Length    int64  `json:"length"`
	Hash      string `json:"hash"`
	Format    string `json:"format"`
	MediaType string `json:"mediaType"`
}

type pendingRasterTile struct {
	name      string
	blob      Blob
	length    int64
	format    string
	mediaType string
}

type shardRange struct {
	offset int64
	length int64
}

// RasterTile is one verified logical tile read from a shard range. Format and
// MediaType are per tile, so a pyramid may legally mix PNG and JPEG.
type RasterTile struct {
	Name      string
	Format    string
	MediaType string
	Data      []byte
}

func packRasterBlobs(blobs []Blob) ([]Blob, error) {
	tiles := make([]pendingRasterTile, 0)
	kept := make([]Blob, 0, len(blobs))
	for _, blob := range blobs {
		format, mediaType, isTile := rasterTileType(blob.Name)
		if !isTile {
			kept = append(kept, blob)
			continue
		}
		length, err := blobLength(blob)
		if err != nil {
			return nil, fmt.Errorf("pack raster tile %s: %w", blob.Name, err)
		}
		if length > maxRasterShardBytes {
			return nil, fmt.Errorf("pack raster tile %s: tile exceeds shard limit", blob.Name)
		}
		tiles = append(tiles, pendingRasterTile{name: blob.Name, blob: blob, length: length, format: format, mediaType: mediaType})
	}
	if len(tiles) == 0 {
		return blobs, nil
	}
	slices.SortFunc(tiles, func(left, right pendingRasterTile) int { return strings.Compare(left.name, right.name) })
	index := rasterTileIndex{Version: 1}
	for start := 0; start < len(tiles); {
		end := start
		var length int64
		for end < len(tiles) && end-start < maxRasterShardTiles {
			next := tiles[end].length
			if end > start && length+next > maxRasterShardBytes {
				break
			}
			length += next
			end++
		}
		shard, descriptors, err := spoolRasterShard(tiles[start:end])
		if err != nil {
			cleanupTemporaryBlobs(kept)
			return nil, err
		}
		kept = append(kept, shard)
		index.Tiles = append(index.Tiles, descriptors...)
		start = end
	}
	data, err := json.Marshal(index)
	if err != nil {
		cleanupTemporaryBlobs(kept)
		return nil, fmt.Errorf("encode raster tile index: %w", err)
	}
	kept = append(kept, Blob{Name: rasterIndexName, Data: data})
	slices.SortFunc(kept, func(left, right Blob) int { return strings.Compare(left.Name, right.Name) })
	return kept, nil
}

func blobLength(blob Blob) (int64, error) {
	if blob.Path == "" {
		return int64(len(blob.Data)), nil
	}
	if len(blob.Data) != 0 {
		return 0, fmt.Errorf("blob has both data and a source path")
	}
	info, err := os.Stat(blob.Path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func spoolRasterShard(tiles []pendingRasterTile) (Blob, []rasterTileDescriptor, error) {
	file, err := os.CreateTemp("", "atlas-raster-shard-*.rpack")
	if err != nil {
		return Blob{}, nil, fmt.Errorf("stage raster shard: %w", err)
	}
	path := file.Name()
	failed := func(err error) (Blob, []rasterTileDescriptor, error) {
		file.Close()
		os.Remove(path)
		return Blob{}, nil, err
	}
	shardHash := sha256.New()
	descriptors := make([]rasterTileDescriptor, 0, len(tiles))
	var offset int64
	for _, tile := range tiles {
		tileHash := sha256.New()
		written, err := copyBlob(io.MultiWriter(file, shardHash, tileHash), tile.blob)
		if err != nil {
			return failed(fmt.Errorf("write raster tile %s: %w", tile.name, err))
		}
		if written != tile.length {
			return failed(fmt.Errorf("write raster tile %s: length changed", tile.name))
		}
		descriptors = append(descriptors, rasterTileDescriptor{
			Name: tile.name, Offset: offset, Length: written, Hash: hex.EncodeToString(tileHash.Sum(nil)),
			Format: tile.format, MediaType: tile.mediaType,
		})
		offset += written
	}
	if err := file.Sync(); err != nil {
		return failed(fmt.Errorf("sync raster shard: %w", err))
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return Blob{}, nil, fmt.Errorf("close raster shard: %w", err)
	}
	name := rasterShardPrefix + hex.EncodeToString(shardHash.Sum(nil)) + ".rpack"
	for index := range descriptors {
		descriptors[index].Shard = name
	}
	return Blob{Name: name, Path: path, temporary: true}, descriptors, nil
}

func copyBlob(destination io.Writer, blob Blob) (int64, error) {
	if blob.Path == "" {
		written, err := io.Copy(destination, bytes.NewReader(blob.Data))
		return written, err
	}
	source, err := os.Open(blob.Path)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(destination, source)
	closeErr := source.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func cleanupTemporaryBlobs(blobs []Blob) {
	for _, blob := range blobs {
		if blob.temporary {
			_ = os.Remove(blob.Path)
		}
	}
}

func rasterTileType(name string) (string, string, bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 5 || parts[0] != "tiles" || parts[1] == "" {
		return "", "", false
	}
	zoom, zoomErr := strconv.ParseUint(parts[2], 10, 6)
	x, xErr := strconv.ParseUint(parts[3], 10, 63)
	yText := strings.TrimSuffix(parts[4], filepath.Ext(parts[4]))
	y, yErr := strconv.ParseUint(yText, 10, 63)
	if zoomErr != nil || xErr != nil || yErr != nil || x >= uint64(1)<<zoom || y >= uint64(1)<<zoom {
		return "", "", false
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(parts[4])), ".")
	switch format {
	case "png":
		return format, "image/png", true
	case "jpg", "jpeg":
		return format, "image/jpeg", true
	case "webp":
		return format, "image/webp", true
	case "avif":
		return format, "image/avif", true
	default:
		return "", "", false
	}
}

func (reader *Reader) rasterIndex() (rasterTileIndex, error) {
	reference, held := reader.blobs[rasterIndexName]
	if !held {
		return rasterTileIndex{}, nil
	}
	data, err := readReferencedEntry(reader.entries[rasterIndexName], reference.Length, reference.Hash, reader.limits.MaxBlobBytes)
	if err != nil {
		return rasterTileIndex{}, err
	}
	var index rasterTileIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil || index.Version != 1 {
		return rasterTileIndex{}, fmt.Errorf("decode raster tile index")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return rasterTileIndex{}, fmt.Errorf("raster tile index carries trailing data")
	}
	canonical, err := json.Marshal(index)
	if err != nil || !bytes.Equal(canonical, data) {
		return rasterTileIndex{}, fmt.Errorf("raster tile index is not canonical")
	}
	previous := ""
	ranges := make(map[string][]shardRange)
	for _, tile := range index.Tiles {
		format, mediaType, validName := rasterTileType(tile.Name)
		shard, held := reader.blobs[tile.Shard]
		decodedHash, hashErr := hex.DecodeString(tile.Hash)
		if tile.Name <= previous || tile.Offset < 0 || tile.Length < 0 || tile.Length > reader.limits.MaxBlobBytes ||
			!validName || format != tile.Format || mediaType != tile.MediaType || !held ||
			tile.Offset > shard.Length || tile.Length > shard.Length-tile.Offset || hashErr != nil ||
			len(decodedHash) != sha256.Size || tile.Hash != strings.ToLower(tile.Hash) {
			return rasterTileIndex{}, fmt.Errorf("raster tile index is not canonical")
		}
		ranges[tile.Shard] = append(ranges[tile.Shard], shardRange{offset: tile.Offset, length: tile.Length})
		previous = tile.Name
	}
	for shardName, held := range ranges {
		slices.SortFunc(held, func(left, right shardRange) int {
			if left.offset < right.offset {
				return -1
			}
			if left.offset > right.offset {
				return 1
			}
			return 0
		})
		var end int64
		for _, part := range held {
			if part.offset != end {
				return rasterTileIndex{}, fmt.Errorf("raster shard %s has overlapping or uncovered ranges", shardName)
			}
			end += part.length
		}
		if end != reader.blobs[shardName].Length {
			return rasterTileIndex{}, fmt.Errorf("raster shard %s has unindexed bytes", shardName)
		}
	}
	for name := range reader.blobs {
		if strings.HasPrefix(name, rasterShardPrefix) && ranges[name] == nil {
			return rasterTileIndex{}, fmt.Errorf("raster shard %s is not indexed", name)
		}
	}
	return index, nil
}

// ReadRasterTile verifies one tile slice without reading the rest of its
// shard. The containing shard remains independently hash-verified by Validate.
func (reader *Reader) ReadRasterTile(name string) (RasterTile, error) {
	index, err := reader.rasterIndex()
	if err != nil {
		return RasterTile{}, err
	}
	at, held := slices.BinarySearchFunc(index.Tiles, name, func(tile rasterTileDescriptor, target string) int {
		return strings.Compare(tile.Name, target)
	})
	if !held {
		return RasterTile{}, fmt.Errorf("bundle has no raster tile %s", name)
	}
	tile := index.Tiles[at]
	data, err := reader.readStoredBlobRange(tile.Shard, tile.Offset, tile.Length)
	if err != nil {
		return RasterTile{}, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != tile.Hash {
		return RasterTile{}, fmt.Errorf("raster tile %s hash does not match", name)
	}
	return RasterTile{Name: name, Format: tile.Format, MediaType: tile.MediaType, Data: data}, nil
}

func (reader *Reader) readStoredBlobRange(name string, offset, length int64) ([]byte, error) {
	reference, held := reader.blobs[name]
	if !held || offset < 0 || length < 0 || offset > reference.Length || length > reference.Length-offset {
		return nil, fmt.Errorf("blob range is invalid")
	}
	entry := reader.entries[name]
	if entry == nil || entry.Method != 0 {
		return nil, fmt.Errorf("blob range is not stored")
	}
	dataOffset, err := entry.DataOffset()
	if err != nil {
		return nil, fmt.Errorf("locate %s: %w", name, err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(io.NewSectionReader(reader.source, dataOffset+offset, length), data); err != nil {
		return nil, fmt.Errorf("read %s range: %w", name, err)
	}
	return data, nil
}
