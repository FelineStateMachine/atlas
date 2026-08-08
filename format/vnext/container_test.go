package vnext

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBundleIsDeterministicAndKeepsPackedDataStored(t *testing.T) {
	t.Parallel()

	bundle, err := Compile(minimalVolume())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	bundle.Release.CreatedAt = "2026-08-01T20:13:08Z"
	bundle.Release.Revision = 4
	var first, second bytes.Buffer
	if err := Write(&first, bundle); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := Write(&second, bundle); err != nil {
		t.Fatalf("write second: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("same semantic volume produced different archive bytes")
	}
	opened, err := Open(bytes.NewReader(first.Bytes()), int64(first.Len()), StandardSchema())
	if err != nil {
		t.Fatalf("open release: %v", err)
	}
	if opened.Release.Title != "Minimal" || opened.Release.CreatedAt != "2026-08-01T20:13:08Z" || opened.Release.Revision != 4 {
		t.Fatalf("release metadata = %#v", opened.Release)
	}
	if len(opened.Release.Stamp) != 64 {
		t.Fatalf("release stamp = %q, want sha256", opened.Release.Stamp)
	}

	archive, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if archive.File[0].Name != manifestName {
		t.Fatalf("first entry = %s, want %s", archive.File[0].Name, manifestName)
	}
	for _, entry := range archive.File {
		if (bytes.HasSuffix([]byte(entry.Name), []byte(".pack")) || bytes.HasPrefix([]byte(entry.Name), []byte("assets/"))) && entry.Method != zip.Store {
			t.Fatalf("range-readable entry %s uses compression method %d", entry.Name, entry.Method)
		}
	}
}

func TestNewerApplicationReadsOlderFileSchema(t *testing.T) {
	t.Parallel()

	bundle, err := Compile(minimalVolume())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	bundle.Release.CreatedAt = "2026-08-01T20:13:08Z"
	var archive bytes.Buffer
	if err := Write(&archive, bundle); err != nil {
		t.Fatalf("write: %v", err)
	}
	newField := Field{ID: IDFromName("com.example.future", "feature.score"), Name: "score", Kind: KindFloat64, Optional: true}
	newer, err := UnionSchemas(StandardSchema(), Schema{Types: []Type{{
		ID: CoreID("type.feature"), Name: "feature", Fields: []Field{newField},
	}}})
	if err != nil {
		t.Fatalf("make newer application schema: %v", err)
	}
	reader, err := Open(bytes.NewReader(archive.Bytes()), int64(archive.Len()), newer)
	if err != nil {
		t.Fatalf("newer application open older file: %v", err)
	}
	featureType, ok := reader.RuntimeSchema.Type(CoreID("type.feature"))
	if !ok {
		t.Fatal("runtime schema lost Feature")
	}
	if _, ok := featureType.Field(newField.ID); !ok {
		t.Fatal("runtime schema lost application-only field")
	}
	if _, err := reader.Volume(); err != nil {
		t.Fatalf("restore older volume: %v", err)
	}
}

func TestBlockRejectsCorruptColumn(t *testing.T) {
	t.Parallel()

	field := Field{ID: IDFromName("com.example", "name"), Name: "name", Kind: KindString}
	table := Table{TypeID: IDFromName("com.example", "type"), Rows: 1, Columns: []Column{{Field: field, Values: []Value{StringValue("atlas")}}}}
	data, err := EncodeBlock([32]byte{1}, table)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	data[len(data)-1] ^= 0xff
	if _, err := DecodeBlock(data); err == nil {
		t.Fatal("decoder accepted a corrupt column")
	}
}

func TestOpenWithLimitsRejectsImplausibleContainersBeforeReadingPayloads(t *testing.T) {
	t.Parallel()

	volume := minimalVolume()
	volume.Assets[0].Data = []byte("sample asset")
	data := nativeBytes(t, volume)
	for name, limits := range map[string]Limits{
		"entries": {MaxEntries: 1},
		"blob":    {MaxBlobBytes: 1},
		"total":   {MaxTotalBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := OpenWithLimits(bytes.NewReader(data), int64(len(data)), StandardSchema(), limits); err == nil {
				t.Fatal("container outside its explicit limit was accepted")
			}
		})
	}
}

func TestOpenRejectsUnreferencedAndCompressedPackedEntries(t *testing.T) {
	t.Parallel()

	data := nativeBytes(t, minimalVolume())
	compressed := rewriteAtlas(t, data, func(header *zip.FileHeader) {
		if strings.HasSuffix(header.Name, ".pack") {
			header.Method = zip.Deflate
		}
	}, false)
	if _, err := Open(bytes.NewReader(compressed), int64(len(compressed)), StandardSchema()); err == nil || !strings.Contains(err.Error(), "not stored") {
		t.Fatalf("compressed packed entry = %v", err)
	}
	orphaned := rewriteAtlas(t, data, nil, true)
	if _, err := Open(bytes.NewReader(orphaned), int64(len(orphaned)), StandardSchema()); err == nil || !strings.Contains(err.Error(), "unreferenced") {
		t.Fatalf("orphan entry = %v", err)
	}
}

func nativeBytes(t *testing.T, volume Volume) []byte {
	t.Helper()
	bundle, err := Compile(volume)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Release.CreatedAt = "2026-08-08T12:00:00Z"
	var data bytes.Buffer
	if err := Write(&data, bundle); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func rewriteAtlas(t *testing.T, source []byte, mutate func(*zip.FileHeader), orphan bool) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		header := entry.FileHeader
		if mutate != nil {
			mutate(&header)
		}
		destination, err := writer.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := destination.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if orphan {
		destination, err := writer.CreateHeader(&zip.FileHeader{Name: "orphan.bin", Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := destination.Write([]byte("orphan")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func minimalVolume() Volume {
	return Volume{
		ID: "minimal", Title: "Minimal",
		Worlds: []World{{
			ID: "world", Title: "World",
			CoordinateSpace: CoordinateSpace{ID: "space", Kind: "synthetic", Unit: "unit", Definition: "atlas:plane", Extent: [4]float64{0, 0, 1, 1}},
			Presentation:    Presentation{ID: "default", Title: "Default"},
		}},
		Assets: []Asset{{ID: "empty", MediaType: "application/octet-stream"}},
	}
}
