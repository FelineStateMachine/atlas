package vnext

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCompilePartitionsFeaturesAndPagesThemWithoutMaterializingVolume(t *testing.T) {
	t.Parallel()

	volume := sampleRegionVolume(9_000)
	bundle, err := Compile(volume)
	if err != nil {
		t.Fatalf("compile Sample Region: %v", err)
	}
	partitionCount := 0
	for _, named := range bundle.Tables {
		if named.Name == featureTableName {
			t.Fatal("compiled bundle retained the monolithic feature table")
		}
		if strings.HasPrefix(named.Name, featurePartitionPrefix) {
			partitionCount++
		}
	}
	if partitionCount < 3 {
		t.Fatalf("feature partitions = %d, want at least 3", partitionCount)
	}

	bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	var encoded bytes.Buffer
	if err := Write(&encoded, bundle); err != nil {
		t.Fatalf("write Sample Region: %v", err)
	}
	reader, err := Open(bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), StandardSchema())
	if err != nil {
		t.Fatalf("open Sample Region: %v", err)
	}
	page, err := reader.FeaturePage(FeaturePageRequest{
		FeatureSet: "roads",
		Bounds:     &SpatialBounds{MinX: 100, MinY: -1, MaxX: 199, MaxY: 1},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("first feature page: %v", err)
	}
	if len(page.Features) != 10 || page.Next == "" {
		t.Fatalf("first page = %d features, next %q", len(page.Features), page.Next)
	}
	if page.PartitionsRead > 2 || page.BytesRead <= 0 {
		t.Fatalf("first page read %d partitions and %d bytes", page.PartitionsRead, page.BytesRead)
	}
	second, err := reader.FeaturePage(FeaturePageRequest{
		FeatureSet: "roads",
		Bounds:     &SpatialBounds{MinX: 100, MinY: -1, MaxX: 199, MaxY: 1},
		After:      page.Next,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("second feature page: %v", err)
	}
	if len(second.Features) != 10 || second.Features[0].ID == page.Features[0].ID {
		t.Fatalf("second page did not advance: %#v", second.Features)
	}

	restored, err := reader.Volume()
	if err != nil {
		t.Fatalf("compatibility materialization: %v", err)
	}
	if got := len(restored.Worlds[0].FeatureSets[0].Features); got != 9_000 {
		t.Fatalf("restored features = %d, want 9000", got)
	}
	if space := restored.Worlds[0].CoordinateSpace; space.OriginX != 10 || space.OriginY != 20 {
		t.Fatalf("restored tile origin = %d,%d, want 10,20", space.OriginX, space.OriginY)
	}
}

func TestOutlineDoesNotReadFeaturePartitions(t *testing.T) {
	t.Parallel()

	bundle, err := Compile(sampleRegionVolume(300))
	if err != nil {
		t.Fatal(err)
	}
	bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	var encoded bytes.Buffer
	if err := Write(&encoded, bundle); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), encoded.Bytes()...)
	archive, err := zip.NewReader(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if !strings.HasSuffix(entry.Name, ".features.pack") {
			continue
		}
		offset, err := entry.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		corrupt[offset+int64(entry.UncompressedSize64)-1] ^= 0xff
		break
	}
	reader, err := Open(bytes.NewReader(corrupt), int64(len(corrupt)), StandardSchema())
	if err != nil {
		t.Fatalf("open with unread corrupt partition: %v", err)
	}
	outline, err := reader.Outline()
	if err != nil {
		t.Fatalf("outline read a feature partition: %v", err)
	}
	if got := outline.Worlds[0].FeatureSets[0].Features; len(got) != 0 {
		t.Fatalf("outline materialized %d features", len(got))
	}
	if _, err := reader.FeaturePage(FeaturePageRequest{FeatureSet: "roads", Limit: 1}); err == nil {
		t.Fatal("feature page accepted the corrupt demanded partition")
	}
}

func TestPartitionAndShardGenerationIsCanonicalAcrossInputOrder(t *testing.T) {
	t.Parallel()

	left := sampleRegionVolume(520)
	right := sampleRegionVolume(520)
	features := right.Worlds[0].FeatureSets[0].Features
	for low, high := 0, len(features)-1; low < high; low, high = low+1, high-1 {
		features[low], features[high] = features[high], features[low]
	}
	encode := func(volume Volume, reverseTiles bool) []byte {
		bundle, err := Compile(volume)
		if err != nil {
			t.Fatal(err)
		}
		bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
		tiles := []Blob{
			{Name: "tiles/background/0/0/0.png", Data: []byte("zero")},
			{Name: "tiles/background/1/1/0.jpg", Data: []byte("one")},
		}
		if reverseTiles {
			tiles[0], tiles[1] = tiles[1], tiles[0]
		}
		bundle.Blobs = append(bundle.Blobs, tiles...)
		var output bytes.Buffer
		if err := Write(&output, bundle); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	if first, second := encode(left, false), encode(right, true); !bytes.Equal(first, second) {
		t.Fatal("stable feature IDs and tile names did not canonicalize input order")
	}
}

func TestValidateRejectsMalformedLogicalIndexes(t *testing.T) {
	t.Parallel()

	featureBundle, err := Compile(sampleRegionVolume(1))
	if err != nil {
		t.Fatal(err)
	}
	for index := range featureBundle.Blobs {
		if featureBundle.Blobs[index].Name == featureIndexName {
			featureBundle.Blobs[index].Data = append(featureBundle.Blobs[index].Data, '\n')
		}
	}
	featureBundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	var featureBytes bytes.Buffer
	if err := Write(&featureBytes, featureBundle); err != nil {
		t.Fatal(err)
	}
	featureReader, err := Open(bytes.NewReader(featureBytes.Bytes()), int64(featureBytes.Len()), StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	if err := featureReader.Validate(); err == nil || !strings.Contains(err.Error(), "feature index") {
		t.Fatalf("malformed feature index validation = %v", err)
	}

	rasterBundle, err := Compile(minimalVolume())
	if err != nil {
		t.Fatal(err)
	}
	rasterBundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	rasterBundle.Blobs = append(rasterBundle.Blobs,
		Blob{Name: rasterShardPrefix + "missing.rpack", Data: []byte("data")},
		Blob{Name: rasterIndexName, Data: []byte(`{"version":1,"tiles":[]}`)},
	)
	var rasterBytes bytes.Buffer
	if err := Write(&rasterBytes, rasterBundle); err != nil {
		t.Fatal(err)
	}
	rasterReader, err := Open(bytes.NewReader(rasterBytes.Bytes()), int64(rasterBytes.Len()), StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rasterReader.LogicalBlobNames(); err == nil || !strings.Contains(err.Error(), "not indexed") {
		t.Fatalf("orphan raster shard logical names = %v", err)
	}
}

func TestRasterTileRangeRejectsCorruptShardSlice(t *testing.T) {
	t.Parallel()

	bundle, err := Compile(minimalVolume())
	if err != nil {
		t.Fatal(err)
	}
	bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	bundle.Blobs = append(bundle.Blobs, Blob{Name: "tiles/background/0/0/0.png", Data: []byte("zero tile")})
	var encoded bytes.Buffer
	if err := Write(&encoded, bundle); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), encoded.Bytes()...)
	archive, err := zip.NewReader(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if !strings.HasPrefix(entry.Name, rasterShardPrefix) {
			continue
		}
		offset, err := entry.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		corrupt[offset] ^= 0xff
	}
	reader, err := Open(bytes.NewReader(corrupt), int64(len(corrupt)), StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadRasterTile("tiles/background/0/0/0.png"); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("corrupt tile read = %v", err)
	}
}

func TestRasterPackingSpoolsBoundedShardsWithoutRetainingTileCopies(t *testing.T) {
	t.Parallel()

	packed, err := packRasterBlobs([]Blob{
		{Name: "tiles/background/1/0/0.png", Data: bytes.Repeat([]byte{1}, 1<<20)},
		{Name: "tiles/background/1/1/0.png", Data: bytes.Repeat([]byte{2}, 1<<20)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var temporary []string
	for _, blob := range packed {
		if !strings.HasPrefix(blob.Name, rasterShardPrefix) {
			continue
		}
		if len(blob.Data) != 0 || blob.Path == "" || !blob.temporary {
			t.Fatalf("raster shard retained in memory: %#v", blob)
		}
		info, err := os.Stat(blob.Path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxRasterShardBytes {
			t.Fatalf("staged shard size = %d", info.Size())
		}
		temporary = append(temporary, blob.Path)
	}
	cleanupTemporaryBlobs(packed)
	for _, path := range temporary {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary shard remains at %s: %v", path, err)
		}
	}
}

func TestWritePacksRasterTilesAndReaderVerifiesLogicalTileSlices(t *testing.T) {
	t.Parallel()

	bundle, err := Compile(minimalVolume())
	if err != nil {
		t.Fatal(err)
	}
	bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	want := map[string][]byte{
		"tiles/background/0/0/0.png": []byte("zero tile"),
		"tiles/background/1/0/0.png": []byte("northwest"),
		"tiles/background/1/1/0.jpg": []byte("northeast"),
	}
	for name, data := range want {
		bundle.Blobs = append(bundle.Blobs, Blob{Name: name, Data: data})
	}
	var encoded bytes.Buffer
	if err := Write(&encoded, bundle); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(encoded.Bytes()), int64(encoded.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if strings.HasPrefix(entry.Name, "tiles/") {
			t.Fatalf("tile remained a ZIP entry: %s", entry.Name)
		}
	}

	reader, err := Open(bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range want {
		tile, err := reader.ReadRasterTile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(tile.Data, expected) {
			t.Fatalf("read %s = %q, want %q", name, tile.Data, expected)
		}
		if strings.HasSuffix(name, ".jpg") && (tile.Format != "jpg" || tile.MediaType != "image/jpeg") {
			t.Fatalf("mixed tile metadata = %#v", tile)
		}
	}
}

func sampleRegionVolume(features int) Volume {
	items := make([]Feature, features)
	for index := range items {
		items[index] = Feature{
			ID:    fmt.Sprintf("road-%04d", index),
			Title: fmt.Sprintf("Sample Road %04d", index),
			Geometry: Geometry{Kind: GeometryPoint, Parts: []GeometryPart{{Rings: [][]Position{{
				{float64(index), 0},
			}}}}},
			Relationships: []Relationship{{Predicate: "next", Target: fmt.Sprintf("road-%04d", (index+1)%max(features, 1))}},
			Provenance:    []Provenance{{Source: "sample", NativeID: fmt.Sprintf("%04d", index), CapturedAt: "2026-08-08T12:00:00Z"}},
		}
	}
	return Volume{
		ID: "sample-region", Title: "Sample Region",
		Worlds: []World{{
			ID: "sample-world", Title: "Sample Region",
			CoordinateSpace: CoordinateSpace{
				ID: "sample-space", Kind: "projected", Unit: "metre", Definition: "EPSG:3857",
				Extent: [4]float64{0, -1, float64(features), 1}, OriginX: 10, OriginY: 20,
			},
			FeatureSets: []FeatureSet{{ID: "roads", Title: "Roads", SemanticType: "transport.road", Features: items}},
			Presentation: Presentation{
				ID: "default", Title: "Default",
				Styles: []Style{{ID: "road-style"}},
				Layers: []Layer{{ID: "roads-layer", FeatureSet: "roads", Style: "road-style", Visible: true, MaxZoom: 22}},
			},
		}},
	}
}
