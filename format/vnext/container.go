package vnext

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
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
	Release Release          `json:"release"`
	Schema  schemaReference  `json:"schema"`
	Tables  []tableReference `json:"tables"`
	Blobs   []blobReference  `json:"blobs,omitempty"`
}

type preparedEntry struct {
	name       string
	data       []byte
	sourcePath string
	method     uint16
}

// Limits bounds the native container before any referenced entry is
// materialized. Zero fields take the stable application defaults; negative
// fields are invalid. Manifests may describe less, never silently more.
type Limits struct {
	MaxEntries    int
	MaxTables     int
	MaxBlobs      int
	MaxTableBytes int64
	MaxBlobBytes  int64
	MaxTotalBytes int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxEntries: 100_000, MaxTables: 4_096, MaxBlobs: 90_000,
		MaxTableBytes: 512 << 20, MaxBlobBytes: 512 << 20, MaxTotalBytes: 1 << 40,
	}
}

func (limits Limits) resolved() (Limits, error) {
	defaults := DefaultLimits()
	values := []*int{&limits.MaxEntries, &limits.MaxTables, &limits.MaxBlobs}
	defaultInts := []int{defaults.MaxEntries, defaults.MaxTables, defaults.MaxBlobs}
	for index, value := range values {
		if *value < 0 {
			return Limits{}, fmt.Errorf("native container limit is negative")
		}
		if *value == 0 {
			*value = defaultInts[index]
		}
	}
	byteValues := []*int64{&limits.MaxTableBytes, &limits.MaxBlobBytes, &limits.MaxTotalBytes}
	defaultBytes := []int64{defaults.MaxTableBytes, defaults.MaxBlobBytes, defaults.MaxTotalBytes}
	for index, value := range byteValues {
		if *value < 0 {
			return Limits{}, fmt.Errorf("native container byte limit is negative")
		}
		if *value == 0 {
			*value = defaultBytes[index]
		}
	}
	return limits, nil
}

// Write produces one deterministic ZIP container with a small JSON bootstrap,
// an embedded canonical schema, stored typed blocks, and stored opaque blobs.
func Write(out io.Writer, bundle Bundle) error {
	if err := ValidSlug(bundle.VolumeID); err != nil {
		return fmt.Errorf("volume identity: %w", err)
	}
	if err := bundle.Release.validate(false); err != nil {
		return fmt.Errorf("release: %w", err)
	}
	// The stamp is always derived here; callers cannot smuggle a stale identity
	// across a changed schema or blob set.
	bundle.Release.Stamp = ""
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
	stampInput, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal stamp input: %w", err)
	}
	stamp := sha256.Sum256(stampInput)
	manifest.Release.Stamp = hex.EncodeToString(stamp[:])
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
		Release: bundle.Release,
		Schema:  schemaReference{Name: schemaName, Hash: hex.EncodeToString(schemaHash[:])},
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
		hash, length, err := blobDigest(blob)
		if err != nil {
			return bootstrap{}, nil, fmt.Errorf("prepare %s: %w", blob.Name, err)
		}
		manifest.Blobs = append(manifest.Blobs, blobReference{
			Name: blob.Name, Hash: hash, Length: length,
		})
		entries = append(entries, preparedEntry{name: blob.Name, data: blob.Data, sourcePath: blob.Path, method: zip.Store})
	}
	slices.SortFunc(manifest.Tables, func(left, right tableReference) int { return strings.Compare(left.Name, right.Name) })
	slices.SortFunc(manifest.Blobs, func(left, right blobReference) int { return strings.Compare(left.Name, right.Name) })
	return manifest, entries, nil
}

func blobDigest(blob Blob) (string, int64, error) {
	if blob.Path == "" {
		digest := sha256.Sum256(blob.Data)
		return hex.EncodeToString(digest[:]), int64(len(blob.Data)), nil
	}
	if len(blob.Data) != 0 {
		return "", 0, fmt.Errorf("blob has both data and a source path")
	}
	file, err := os.Open(blob.Path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	length, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), length, nil
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
		if entry.sourcePath != "" {
			source, err := os.Open(entry.sourcePath)
			if err != nil {
				return fmt.Errorf("open %s: %w", entry.name, err)
			}
			_, copyErr := io.Copy(writer, source)
			closeErr := source.Close()
			if copyErr != nil {
				return fmt.Errorf("write %s: %w", entry.name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close %s: %w", entry.name, closeErr)
			}
			continue
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
	Release       Release
	FileSchema    Schema
	RuntimeSchema Schema

	entries map[string]*zip.File
	tables  map[string]tableReference
	blobs   map[string]blobReference
	limits  Limits
}

// Open reads the bootstrap and embedded schema. Only container framing is a
// hard gate; schema differences are merged by identity.
func Open(source io.ReaderAt, size int64, applicationSchema Schema) (*Reader, error) {
	return OpenWithLimits(source, size, applicationSchema, DefaultLimits())
}

// OpenWithLimits opens a native Atlas under an explicit reproducible resource
// policy. Open is the compatibility facade using DefaultLimits.
func OpenWithLimits(source io.ReaderAt, size int64, applicationSchema Schema, limits Limits) (*Reader, error) {
	limits, err := limits.resolved()
	if err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return nil, fmt.Errorf("open Atlas archive: %w", err)
	}
	entries, err := indexEntries(archive.File, limits)
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
		VolumeID: manifest.Volume, Release: manifest.Release, FileSchema: fileSchema, RuntimeSchema: runtimeSchema,
		entries: entries, tables: make(map[string]tableReference, len(manifest.Tables)),
		blobs: make(map[string]blobReference, len(manifest.Blobs)), limits: limits,
	}
	if err := reader.indexReferences(manifest); err != nil {
		return nil, err
	}
	return reader, nil
}

func indexEntries(files []*zip.File, limits Limits) (map[string]*zip.File, error) {
	if len(files) > limits.MaxEntries {
		return nil, fmt.Errorf("archive carries %d entries, limit is %d", len(files), limits.MaxEntries)
	}
	entries := make(map[string]*zip.File, len(files))
	var total uint64
	for _, file := range files {
		if err := validEntryName(file.Name); err != nil {
			return nil, err
		}
		if _, held := entries[file.Name]; held {
			return nil, fmt.Errorf("archive carries %s twice", file.Name)
		}
		if file.UncompressedSize64 > math.MaxInt64 || total > math.MaxUint64-file.UncompressedSize64 {
			return nil, fmt.Errorf("archive entry sizes overflow")
		}
		total += file.UncompressedSize64
		if total > uint64(limits.MaxTotalBytes) {
			return nil, fmt.Errorf("archive expands beyond %d bytes", limits.MaxTotalBytes)
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return bootstrap{}, fmt.Errorf("decode bootstrap: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return bootstrap{}, fmt.Errorf("bootstrap carries trailing data")
	}
	if manifest.Format != containerFormat || manifest.Framing != containerFraming {
		return bootstrap{}, fmt.Errorf("unsupported Atlas container identity or framing")
	}
	if err := ValidSlug(manifest.Volume); err != nil {
		return bootstrap{}, fmt.Errorf("volume identity: %w", err)
	}
	if err := manifest.Release.Validate(); err != nil {
		return bootstrap{}, fmt.Errorf("release: %w", err)
	}
	stamp := manifest.Release.Stamp
	manifest.Release.Stamp = ""
	stampInput, err := json.Marshal(manifest)
	if err != nil {
		return bootstrap{}, fmt.Errorf("marshal stamp input: %w", err)
	}
	digest := sha256.Sum256(stampInput)
	if hex.EncodeToString(digest[:]) != stamp {
		return bootstrap{}, fmt.Errorf("release stamp does not match bundle contents")
	}
	manifest.Release.Stamp = stamp
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
	if len(manifest.Tables) > reader.limits.MaxTables || len(manifest.Blobs) > reader.limits.MaxBlobs {
		return fmt.Errorf("native container reference count exceeds policy")
	}
	allowed := map[string]bool{manifestName: true, manifest.Schema.Name: true}
	for _, reference := range manifest.Tables {
		entry := reader.entries[reference.Name]
		if allowed[reference.Name] || entry == nil {
			return fmt.Errorf("table reference %s is duplicate or missing", reference.Name)
		}
		if err := validateReference(reference.Name, reference.Hash, reference.Length, reader.limits.MaxTableBytes, entry); err != nil {
			return fmt.Errorf("table reference %s: %w", reference.Name, err)
		}
		reader.tables[reference.Name] = reference
		allowed[reference.Name] = true
	}
	for _, reference := range manifest.Blobs {
		entry := reader.entries[reference.Name]
		if allowed[reference.Name] || entry == nil {
			return fmt.Errorf("blob reference %s is duplicate or missing", reference.Name)
		}
		if err := validateReference(reference.Name, reference.Hash, reference.Length, reader.limits.MaxBlobBytes, entry); err != nil {
			return fmt.Errorf("blob reference %s: %w", reference.Name, err)
		}
		reader.blobs[reference.Name] = reference
		allowed[reference.Name] = true
	}
	for name := range reader.entries {
		if !allowed[name] {
			return fmt.Errorf("archive carries unreferenced entry %s", name)
		}
	}
	return nil
}

func validateReference(name, hash string, length, maximum int64, entry *zip.File) error {
	if name == manifestName || length < 0 || length > maximum || int64(entry.UncompressedSize64) != length {
		return fmt.Errorf("length is invalid or exceeds %d bytes", maximum)
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size || hash != strings.ToLower(hash) {
		return fmt.Errorf("hash is not lowercase SHA-256")
	}
	if entry.Method != zip.Store {
		return fmt.Errorf("referenced packed data is not stored")
	}
	return nil
}

// Table decodes and integrity-checks one named table.
func (reader *Reader) Table(name string) (Table, error) {
	reference, held := reader.tables[name]
	if !held {
		return Table{}, fmt.Errorf("bundle has no table %s", name)
	}
	data, err := readReferencedEntry(reader.entries[name], reference.Length, reference.Hash, reader.limits.MaxTableBytes)
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

// TableBlock returns the verified physical bytes for browser or range-based
// consumers that decode columns themselves.
func (reader *Reader) TableBlock(name string) ([]byte, error) {
	reference, held := reader.tables[name]
	if !held {
		return nil, fmt.Errorf("bundle has no table %s", name)
	}
	return readReferencedEntry(reader.entries[name], reference.Length, reference.Hash, reader.limits.MaxTableBytes)
}

func tableHasColumn(table Table, fieldID ID) bool {
	for _, column := range table.Columns {
		if column.Field.ID == fieldID {
			return true
		}
	}
	return false
}

func (reader *Reader) Blob(name string) ([]byte, error) {
	reference, held := reader.blobs[name]
	if !held {
		return nil, fmt.Errorf("bundle has no blob %s", name)
	}
	return readReferencedEntry(reader.entries[name], reference.Length, reference.Hash, reader.limits.MaxBlobBytes)
}

func (reader *Reader) TableNames() []string {
	names := make([]string, 0, len(reader.tables))
	for name := range reader.tables {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (reader *Reader) BlobNames() []string {
	names := make([]string, 0, len(reader.blobs))
	for name := range reader.blobs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Validate reads and verifies every typed table and opaque blob, then restores
// the semantic root. Installers call it before and after committing a file.
func (reader *Reader) Validate() error {
	for _, name := range reader.TableNames() {
		if _, err := reader.Table(name); err != nil {
			return fmt.Errorf("validate %s: %w", name, err)
		}
	}
	for _, name := range reader.BlobNames() {
		reference := reader.blobs[name]
		if err := validateReferencedEntry(reader.entries[name], reference.Length, reference.Hash, reader.limits.MaxBlobBytes); err != nil {
			return fmt.Errorf("validate %s: %w", name, err)
		}
	}
	if _, err := reader.Volume(); err != nil {
		return fmt.Errorf("restore volume: %w", err)
	}
	return nil
}

func readLimitedEntry(entries map[string]*zip.File, name string, maximum int64) ([]byte, error) {
	entry := entries[name]
	if entry == nil || int64(entry.UncompressedSize64) > maximum {
		return nil, fmt.Errorf("entry %s is missing or implausibly large", name)
	}
	return readEntryLimited(entry, maximum)
}

func readReferencedEntry(entry *zip.File, length int64, hash string, maximum int64) ([]byte, error) {
	if entry == nil || length < 0 || length > maximum || int64(entry.UncompressedSize64) != length {
		return nil, fmt.Errorf("referenced entry length does not match")
	}
	data, err := readEntryLimited(entry, maximum)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != hash {
		return nil, fmt.Errorf("referenced entry hash does not match")
	}
	return data, nil
}

func validateReferencedEntry(entry *zip.File, length int64, hash string, maximum int64) error {
	if entry == nil || length < 0 || length > maximum || int64(entry.UncompressedSize64) != length {
		return fmt.Errorf("referenced entry length does not match")
	}
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer source.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(source, plusOne(maximum)))
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.Name, err)
	}
	if written != length || hex.EncodeToString(digest.Sum(nil)) != hash {
		return fmt.Errorf("referenced entry length or hash does not match")
	}
	return nil
}

func readEntry(entry *zip.File) ([]byte, error) {
	return readEntryLimited(entry, math.MaxInt64-1)
}

func readEntryLimited(entry *zip.File, maximum int64) ([]byte, error) {
	source, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", entry.Name, err)
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, plusOne(maximum)))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Name, err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("entry %s expands beyond %d bytes", entry.Name, maximum)
	}
	return data, nil
}

func plusOne(value int64) int64 {
	if value == math.MaxInt64 {
		return value
	}
	return value + 1
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
