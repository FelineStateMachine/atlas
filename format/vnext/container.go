package vnext

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	manifestName     = "atlas.json"
	containerFormat  = "atlas-schema-bundle"
	containerFraming = uint16(1)
	maxManifestSize  = 1 << 20
)

type schemaReference struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type tableReference struct {
	Name     string `json:"name"`
	RootType ID     `json:"rootType"`
	Hash     string `json:"hash"`
	Length   int64  `json:"length"`
}

type blobReference struct {
	Name   string `json:"name"`
	Hash   string `json:"hash"`
	Length int64  `json:"length"`
}

type bootstrap struct {
	Format  string           `json:"format"`
	Framing uint16           `json:"framing"`
	Volume  string           `json:"volume"`
	Schema  schemaReference  `json:"schema"`
	Tables  []tableReference `json:"tables"`
	Blobs   []blobReference  `json:"blobs,omitempty"`
}

type preparedEntry struct {
	name   string
	data   []byte
	method uint16
}

// Write produces one deterministic ZIP container with a small JSON bootstrap,
// an embedded canonical schema, stored typed blocks, and stored opaque blobs.
func Write(out io.Writer, bundle Bundle) error {
	if bundle.VolumeID == "" {
		return fmt.Errorf("bundle has no volume identity")
	}
	schemaData, err := bundle.Schema.Canonical()
	if err != nil {
		return fmt.Errorf("canonicalize schema: %w", err)
	}
	schemaHash := sha256.Sum256(schemaData)
	schemaName := "schema/" + hex.EncodeToString(schemaHash[:]) + ".json"
	manifest, entries, err := prepareBundle(bundle, schemaHash, schemaName, schemaData)
	if err != nil {
		return err
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal bootstrap: %w", err)
	}
	entries = append(entries, preparedEntry{name: manifestName, data: manifestData, method: zip.Deflate})
	slices.SortFunc(entries, func(left, right preparedEntry) int {
		if left.name == manifestName {
			return -1
		}
		if right.name == manifestName {
			return 1
		}
		return strings.Compare(left.name, right.name)
	})
	return writeArchive(out, entries)
}

func prepareBundle(bundle Bundle, schemaHash [32]byte, schemaName string, schemaData []byte) (bootstrap, []preparedEntry, error) {
	manifest := bootstrap{
		Format: containerFormat, Framing: containerFraming, Volume: bundle.VolumeID,
		Schema: schemaReference{Name: schemaName, Hash: hex.EncodeToString(schemaHash[:])},
	}
	entries := []preparedEntry{{name: schemaName, data: schemaData, method: zip.Deflate}}
	for _, named := range bundle.Tables {
		data, err := EncodeBlock(schemaHash, named.Table)
		if err != nil {
			return bootstrap{}, nil, fmt.Errorf("encode %s: %w", named.Name, err)
		}
		digest := sha256.Sum256(data)
		manifest.Tables = append(manifest.Tables, tableReference{
			Name: named.Name, RootType: named.Table.TypeID,
			Hash: hex.EncodeToString(digest[:]), Length: int64(len(data)),
		})
		entries = append(entries, preparedEntry{name: named.Name, data: data, method: zip.Store})
	}
	for _, blob := range bundle.Blobs {
		digest := sha256.Sum256(blob.Data)
		manifest.Blobs = append(manifest.Blobs, blobReference{
			Name: blob.Name, Hash: hex.EncodeToString(digest[:]), Length: int64(len(blob.Data)),
		})
		entries = append(entries, preparedEntry{name: blob.Name, data: blob.Data, method: zip.Store})
	}
	slices.SortFunc(manifest.Tables, func(left, right tableReference) int { return strings.Compare(left.Name, right.Name) })
	slices.SortFunc(manifest.Blobs, func(left, right blobReference) int { return strings.Compare(left.Name, right.Name) })
	return manifest, entries, nil
}

func writeArchive(out io.Writer, entries []preparedEntry) error {
	archive := zip.NewWriter(out)
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := validEntryName(entry.name); err != nil {
			return err
		}
		if seen[entry.name] {
			return fmt.Errorf("entry %s is written twice", entry.name)
		}
		seen[entry.name] = true
		header := &zip.FileHeader{Name: entry.name, Method: entry.method}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create %s: %w", entry.name, err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			return fmt.Errorf("write %s: %w", entry.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close Atlas archive: %w", err)
	}
	return nil
}

// Reader is an opened vNext bundle. It retains both the file schema and the
// application's merged runtime schema.
type Reader struct {
	VolumeID      string
	FileSchema    Schema
	RuntimeSchema Schema

	entries map[string]*zip.File
	tables  map[string]tableReference
	blobs   map[string]blobReference
}

// Open reads the bootstrap and embedded schema. Only container framing is a
// hard gate; schema differences are merged by identity.
func Open(source io.ReaderAt, size int64, applicationSchema Schema) (*Reader, error) {
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return nil, fmt.Errorf("open Atlas archive: %w", err)
	}
	entries, err := indexEntries(archive.File)
	if err != nil {
		return nil, err
	}
	manifest, err := readBootstrap(entries)
	if err != nil {
		return nil, err
	}
	fileSchema, err := readSchema(entries, manifest.Schema)
	if err != nil {
		return nil, err
	}
	runtimeSchema, err := UnionSchemas(applicationSchema, fileSchema)
	if err != nil {
		return nil, fmt.Errorf("merge runtime schema: %w", err)
	}
	reader := &Reader{
		VolumeID: manifest.Volume, FileSchema: fileSchema, RuntimeSchema: runtimeSchema,
		entries: entries, tables: make(map[string]tableReference, len(manifest.Tables)),
		blobs: make(map[string]blobReference, len(manifest.Blobs)),
	}
	if err := reader.indexReferences(manifest); err != nil {
		return nil, err
	}
	return reader, nil
}

func indexEntries(files []*zip.File) (map[string]*zip.File, error) {
	entries := make(map[string]*zip.File, len(files))
	for _, file := range files {
		if err := validEntryName(file.Name); err != nil {
			return nil, err
		}
		if _, held := entries[file.Name]; held {
			return nil, fmt.Errorf("archive carries %s twice", file.Name)
		}
		entries[file.Name] = file
	}
	return entries, nil
}

func readBootstrap(entries map[string]*zip.File) (bootstrap, error) {
	data, err := readLimitedEntry(entries, manifestName, maxManifestSize)
	if err != nil {
		return bootstrap{}, err
	}
	var manifest bootstrap
	if err := json.Unmarshal(data, &manifest); err != nil {
		return bootstrap{}, fmt.Errorf("decode bootstrap: %w", err)
	}
	if manifest.Format != containerFormat || manifest.Framing != containerFraming || manifest.Volume == "" {
		return bootstrap{}, fmt.Errorf("unsupported Atlas container identity or framing")
	}
	return manifest, nil
}

func readSchema(entries map[string]*zip.File, reference schemaReference) (Schema, error) {
	data, err := readLimitedEntry(entries, reference.Name, 16<<20)
	if err != nil {
		return Schema{}, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != reference.Hash {
		return Schema{}, fmt.Errorf("embedded schema hash does not match")
	}
	var schema Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return Schema{}, fmt.Errorf("decode embedded schema: %w", err)
	}
	canonical, err := schema.Canonical()
	if err != nil {
		return Schema{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Schema{}, fmt.Errorf("embedded schema is not canonical")
	}
	return schema, nil
}

func (reader *Reader) indexReferences(manifest bootstrap) error {
	for _, reference := range manifest.Tables {
		if _, held := reader.tables[reference.Name]; held || reader.entries[reference.Name] == nil {
			return fmt.Errorf("table reference %s is duplicate or missing", reference.Name)
		}
		reader.tables[reference.Name] = reference
	}
	for _, reference := range manifest.Blobs {
		if _, held := reader.blobs[reference.Name]; held || reader.entries[reference.Name] == nil {
			return fmt.Errorf("blob reference %s is duplicate or missing", reference.Name)
		}
		reader.blobs[reference.Name] = reference
	}
	return nil
}

// Table decodes and integrity-checks one named table.
func (reader *Reader) Table(name string) (Table, error) {
	reference, held := reader.tables[name]
	if !held {
		return Table{}, fmt.Errorf("bundle has no table %s", name)
	}
	data, err := readReferencedEntry(reader.entries[name], reference.Length, reference.Hash)
	if err != nil {
		return Table{}, err
	}
	block, err := DecodeBlock(data)
	if err != nil {
		return Table{}, fmt.Errorf("decode %s: %w", name, err)
	}
	schemaHash, err := reader.FileSchema.Hash()
	if err != nil {
		return Table{}, err
	}
	if block.SchemaHash != schemaHash || block.Table.TypeID != reference.RootType {
		return Table{}, fmt.Errorf("table %s disagrees with its schema or root type", name)
	}
	fileType, held := reader.FileSchema.Type(block.Table.TypeID)
	if !held {
		return Table{}, fmt.Errorf("schema has no root type for table %s", name)
	}
	for index := range block.Table.Columns {
		field, held := fileType.Field(block.Table.Columns[index].Field.ID)
		if !held || field.Kind != block.Table.Columns[index].Field.Kind {
			return Table{}, fmt.Errorf("schema has no matching field for table %s", name)
		}
		block.Table.Columns[index].Field = field
	}
	for _, field := range fileType.Fields {
		if field.Optional {
			continue
		}
		if !tableHasColumn(block.Table, field.ID) {
			return Table{}, fmt.Errorf("table %s omits required field %s", name, field.ID)
		}
	}
	return block.Table, nil
}

func tableHasColumn(table Table, fieldID ID) bool {
	for _, column := range table.Columns {
		if column.Field.ID == fieldID {
			return true
		}
	}
	return false
}

func (reader *Reader) blob(name string) ([]byte, error) {
	reference, held := reader.blobs[name]
	if !held {
		return nil, fmt.Errorf("bundle has no blob %s", name)
	}
	return readReferencedEntry(reader.entries[name], reference.Length, reference.Hash)
}

func readLimitedEntry(entries map[string]*zip.File, name string, maximum int64) ([]byte, error) {
	entry := entries[name]
	if entry == nil || int64(entry.UncompressedSize64) > maximum {
		return nil, fmt.Errorf("entry %s is missing or implausibly large", name)
	}
	return readEntry(entry)
}

func readReferencedEntry(entry *zip.File, length int64, hash string) ([]byte, error) {
	if entry == nil || int64(entry.UncompressedSize64) != length {
		return nil, fmt.Errorf("referenced entry length does not match")
	}
	data, err := readEntry(entry)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != hash {
		return nil, fmt.Errorf("referenced entry hash does not match")
	}
	return data, nil
}

func readEntry(entry *zip.File) ([]byte, error) {
	source, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer source.Close()
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Name, err)
	}
	return data, nil
}

func validEntryName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("entry name %q is not relative", name)
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("entry name %q climbs or stutters", name)
		}
	}
	return nil
}
