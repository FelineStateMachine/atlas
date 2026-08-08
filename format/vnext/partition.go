package vnext

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

const (
	featureIndexName       = "indexes/features.v1.json"
	featurePartitionPrefix = "data/feature-partitions/"
	featurePartitionRows   = 4_096
	maxFeaturePageRows     = 1_000
)

// SpatialBounds is a closed, axis-aligned query rectangle in a world's
// declared coordinate space.
type SpatialBounds struct {
	MinX float64 `json:"minX"`
	MinY float64 `json:"minY"`
	MaxX float64 `json:"maxX"`
	MaxY float64 `json:"maxY"`
}

type featurePartitionIndex struct {
	Version    uint16                       `json:"version"`
	Partitions []featurePartitionDescriptor `json:"partitions"`
}

type featurePartitionDescriptor struct {
	FeatureSet    string        `json:"featureSet"`
	Key           string        `json:"key"`
	Bounds        SpatialBounds `json:"bounds"`
	Rows          int           `json:"rows"`
	Features      string        `json:"features"`
	Relationships string        `json:"relationships"`
	Provenance    string        `json:"provenance"`
}

type orderedFeature struct {
	id     string
	key    uint64
	bounds SpatialBounds
}

// FeaturePageRequest selects a bounded page from one semantic FeatureSet.
// After is an opaque continuation returned by the preceding page.
type FeaturePageRequest struct {
	FeatureSet string
	Bounds     *SpatialBounds
	After      string
	Limit      int
}

// FeaturePageResult is a demand-read page and its physical read evidence.
type FeaturePageResult struct {
	Features       []Feature
	Next           string
	PartitionsRead int
	BytesRead      int64
}

// FeatureSetSummary is page-zero cardinality derived from the physical index;
// reading it never opens a feature partition.
type FeatureSetSummary struct {
	FeatureSet string
	Rows       int
}

// DemandAddressedFeatures reports whether feature rows are physically
// partitioned instead of carried in the legacy monolithic tables.
func (reader *Reader) DemandAddressedFeatures() bool {
	_, held := reader.blobs[featureIndexName]
	return held
}

// FeatureSetSummaries returns canonical per-set row counts from the physical
// partition index without materializing feature rows.
func (reader *Reader) FeatureSetSummaries() ([]FeatureSetSummary, error) {
	index, err := reader.featureIndex()
	if err != nil {
		return nil, err
	}
	if _, indexed := reader.blobs[featureIndexName]; !indexed {
		volume, err := reader.Volume()
		if err != nil {
			return nil, err
		}
		var out []FeatureSetSummary
		for _, world := range volume.Worlds {
			for _, set := range world.FeatureSets {
				out = append(out, FeatureSetSummary{FeatureSet: set.ID, Rows: len(set.Features)})
			}
		}
		slices.SortFunc(out, func(left, right FeatureSetSummary) int {
			return strings.Compare(left.FeatureSet, right.FeatureSet)
		})
		return out, nil
	}
	bySet := make(map[string]int)
	for _, partition := range index.Partitions {
		bySet[partition.FeatureSet] += partition.Rows
	}
	sets := make([]string, 0, len(bySet))
	for set := range bySet {
		sets = append(sets, set)
	}
	slices.Sort(sets)
	out := make([]FeatureSetSummary, 0, len(sets))
	for _, set := range sets {
		out = append(out, FeatureSetSummary{FeatureSet: set, Rows: bySet[set]})
	}
	return out, nil
}

func partitionFeatureTables(volume Volume, tables map[string]Table) ([]NamedTable, Blob, error) {
	features := tables[featureTableName]
	relationships := tables[relationshipTableName]
	provenance := tables[provenanceTableName]
	featureRows := rowsByString(features, "feature.id")
	index := featurePartitionIndex{Version: 1}
	var partitions []NamedTable
	for _, world := range volume.Worlds {
		for _, set := range world.FeatureSets {
			ordered, err := orderFeatures(set.Features, world.CoordinateSpace.Extent)
			if err != nil {
				return nil, Blob{}, fmt.Errorf("partition feature set %s: %w", set.ID, err)
			}
			for start := 0; start < len(ordered); start += featurePartitionRows {
				end := min(start+featurePartitionRows, len(ordered))
				chunk := ordered[start:end]
				descriptor, named, err := makeFeaturePartition(set.ID, chunk, featureRows, features, relationships, provenance)
				if err != nil {
					return nil, Blob{}, err
				}
				index.Partitions = append(index.Partitions, descriptor)
				partitions = append(partitions, named...)
			}
		}
	}
	slices.SortFunc(index.Partitions, func(left, right featurePartitionDescriptor) int {
		if bySet := strings.Compare(left.FeatureSet, right.FeatureSet); bySet != 0 {
			return bySet
		}
		return strings.Compare(left.Key, right.Key)
	})
	data, err := json.Marshal(index)
	if err != nil {
		return nil, Blob{}, fmt.Errorf("encode feature partition index: %w", err)
	}
	return partitions, Blob{Name: featureIndexName, Data: data}, nil
}

func orderFeatures(features []Feature, extent [4]float64) ([]orderedFeature, error) {
	ordered := make([]orderedFeature, len(features))
	for index, feature := range features {
		bounds, err := geometryBounds(feature.Geometry)
		if err != nil {
			return nil, fmt.Errorf("feature %s: %w", feature.ID, err)
		}
		ordered[index] = orderedFeature{id: feature.ID, key: spatialKey(bounds, extent), bounds: bounds}
	}
	slices.SortFunc(ordered, func(left, right orderedFeature) int {
		if left.key < right.key {
			return -1
		}
		if left.key > right.key {
			return 1
		}
		return strings.Compare(left.id, right.id)
	})
	return ordered, nil
}

func makeFeaturePartition(
	setID string,
	ordered []orderedFeature,
	featureRows map[string]int,
	features Table,
	relationships Table,
	provenance Table,
) (featurePartitionDescriptor, []NamedTable, error) {
	ids := make(map[string]bool, len(ordered))
	rows := make([]int, len(ordered))
	bounds := ordered[0].bounds
	for index, feature := range ordered {
		row, held := featureRows[feature.id]
		if !held {
			return featurePartitionDescriptor{}, nil, fmt.Errorf("feature table omits %s", feature.id)
		}
		ids[feature.id] = true
		rows[index] = row
		bounds = unionBounds(bounds, feature.bounds)
	}
	identity := fmt.Sprintf("%s\x00%016x\x00%s\x00%016x\x00%s", setID, ordered[0].key, ordered[0].id, ordered[len(ordered)-1].key, ordered[len(ordered)-1].id)
	digest := sha256.Sum256([]byte(identity))
	token := hex.EncodeToString(digest[:12])
	base := featurePartitionPrefix + token
	descriptor := featurePartitionDescriptor{
		FeatureSet: setID, Key: fmt.Sprintf("%016x/%s", ordered[0].key, ordered[0].id), Bounds: bounds, Rows: len(rows),
		Features: base + ".features.pack", Relationships: base + ".relationships.pack", Provenance: base + ".provenance.pack",
	}
	named := []NamedTable{
		{Name: descriptor.Features, Table: selectRows(features, rows)},
		{Name: descriptor.Relationships, Table: selectRowsForIDs(relationships, "relationship.feature", ids)},
		{Name: descriptor.Provenance, Table: selectRowsForIDs(provenance, "provenance.feature", ids)},
	}
	return descriptor, named, nil
}

func rowsByString(table Table, field string) map[string]int {
	rows := make(map[string]int, table.Rows)
	for row := 0; row < table.Rows; row++ {
		rows[requiredString(table, row, field)] = row
	}
	return rows
}

func selectRowsForIDs(table Table, field string, ids map[string]bool) Table {
	rows := make([]int, 0)
	for row := 0; row < table.Rows; row++ {
		if ids[requiredString(table, row, field)] {
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(left, right int) int {
		return bytes.Compare(stableRowKey(table, left), stableRowKey(table, right))
	})
	return selectRows(table, rows)
}

func stableRowKey(table Table, row int) []byte {
	values := make([]Value, len(table.Columns))
	for index, column := range table.Columns {
		values[index] = column.Values[row]
	}
	encoded, _ := json.Marshal(values)
	return encoded
}

func selectRows(table Table, rows []int) Table {
	out := Table{TypeID: table.TypeID, Rows: len(rows), Columns: make([]Column, len(table.Columns))}
	for columnIndex, column := range table.Columns {
		values := make([]Value, len(rows))
		for index, row := range rows {
			values[index] = column.Values[row]
		}
		out.Columns[columnIndex] = Column{Field: column.Field, Values: values}
	}
	return canonicalTable(out)
}

func geometryBounds(geometry Geometry) (SpatialBounds, error) {
	bounds := SpatialBounds{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
	for _, part := range geometry.Parts {
		for _, ring := range part.Rings {
			for _, position := range ring {
				bounds.MinX = math.Min(bounds.MinX, position[0])
				bounds.MinY = math.Min(bounds.MinY, position[1])
				bounds.MaxX = math.Max(bounds.MaxX, position[0])
				bounds.MaxY = math.Max(bounds.MaxY, position[1])
			}
		}
	}
	if math.IsInf(bounds.MinX, 0) {
		return SpatialBounds{}, fmt.Errorf("geometry has no positions")
	}
	return bounds, nil
}

func spatialKey(bounds SpatialBounds, extent [4]float64) uint64 {
	x := quantize((bounds.MinX+bounds.MaxX)/2, extent[0], extent[2])
	y := quantize((bounds.MinY+bounds.MaxY)/2, extent[1], extent[3])
	return morton32(x, y)
}

func quantize(value, minimum, maximum float64) uint32 {
	if maximum <= minimum || value <= minimum {
		return 0
	}
	if value >= maximum {
		return math.MaxUint32
	}
	return uint32((value - minimum) / (maximum - minimum) * float64(math.MaxUint32))
}

func morton32(x, y uint32) uint64 {
	var out uint64
	for bit := range 32 {
		out |= uint64((x>>bit)&1) << (bit * 2)
		out |= uint64((y>>bit)&1) << (bit*2 + 1)
	}
	return out
}

func unionBounds(left, right SpatialBounds) SpatialBounds {
	return SpatialBounds{
		MinX: math.Min(left.MinX, right.MinX), MinY: math.Min(left.MinY, right.MinY),
		MaxX: math.Max(left.MaxX, right.MaxX), MaxY: math.Max(left.MaxY, right.MaxY),
	}
}

func intersects(left, right SpatialBounds) bool {
	return left.MinX <= right.MaxX && left.MaxX >= right.MinX && left.MinY <= right.MaxY && left.MaxY >= right.MinY
}

func (reader *Reader) featureIndex() (featurePartitionIndex, error) {
	reference, held := reader.blobs[featureIndexName]
	if !held {
		return featurePartitionIndex{}, nil
	}
	data, err := readReferencedEntry(reader.entries[featureIndexName], reference.Length, reference.Hash, reader.limits.MaxBlobBytes)
	if err != nil {
		return featurePartitionIndex{}, err
	}
	var index featurePartitionIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil || index.Version != 1 {
		return featurePartitionIndex{}, fmt.Errorf("decode feature partition index")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return featurePartitionIndex{}, fmt.Errorf("feature partition index carries trailing data")
	}
	canonical, err := json.Marshal(index)
	if err != nil || !bytes.Equal(canonical, data) {
		return featurePartitionIndex{}, fmt.Errorf("feature partition index is not canonical")
	}
	if err := reader.validateFeatureIndex(index); err != nil {
		return featurePartitionIndex{}, err
	}
	return index, nil
}

func (reader *Reader) validateFeatureIndex(index featurePartitionIndex) error {
	seen := make(map[string]bool, len(index.Partitions)*3)
	previous := ""
	for _, partition := range index.Partitions {
		key := partition.FeatureSet + "\x00" + partition.Key
		finite := !math.IsNaN(partition.Bounds.MinX) && !math.IsNaN(partition.Bounds.MinY) &&
			!math.IsNaN(partition.Bounds.MaxX) && !math.IsNaN(partition.Bounds.MaxY) &&
			!math.IsInf(partition.Bounds.MinX, 0) && !math.IsInf(partition.Bounds.MinY, 0) &&
			!math.IsInf(partition.Bounds.MaxX, 0) && !math.IsInf(partition.Bounds.MaxY, 0)
		if partition.FeatureSet == "" || partition.Key == "" || key <= previous || partition.Rows <= 0 ||
			partition.Rows > featurePartitionRows || !finite || partition.Bounds.MinX > partition.Bounds.MaxX ||
			partition.Bounds.MinY > partition.Bounds.MaxY {
			return fmt.Errorf("feature partition index is not canonical")
		}
		for _, expected := range []struct {
			name   string
			typeID ID
		}{
			{partition.Features, CoreID("type.feature")},
			{partition.Relationships, CoreID("type.relationship")},
			{partition.Provenance, CoreID("type.provenance")},
		} {
			reference, held := reader.tables[expected.name]
			if !held || seen[expected.name] || !strings.HasPrefix(expected.name, featurePartitionPrefix) || reference.RootType != expected.typeID {
				return fmt.Errorf("feature partition %s has an invalid table role", partition.Key)
			}
			seen[expected.name] = true
		}
		previous = key
	}
	for name := range reader.tables {
		if strings.HasPrefix(name, featurePartitionPrefix) && !seen[name] {
			return fmt.Errorf("feature partition table %s is not indexed", name)
		}
	}
	return nil
}

func (reader *Reader) partitionedTable(name string) (Table, bool, error) {
	index, err := reader.featureIndex()
	if err != nil || len(index.Partitions) == 0 {
		return Table{}, false, err
	}
	var names []string
	for _, partition := range index.Partitions {
		switch name {
		case featureTableName:
			names = append(names, partition.Features)
		case relationshipTableName:
			names = append(names, partition.Relationships)
		case provenanceTableName:
			names = append(names, partition.Provenance)
		default:
			return Table{}, false, nil
		}
	}
	var combined Table
	for _, physical := range names {
		table, err := reader.Table(physical)
		if err != nil {
			return Table{}, true, err
		}
		combined, err = appendTable(combined, table)
		if err != nil {
			return Table{}, true, err
		}
	}
	return combined, true, nil
}

func appendTable(left, right Table) (Table, error) {
	if left.TypeID.isZero() {
		return right, nil
	}
	if left.TypeID != right.TypeID || len(left.Columns) != len(right.Columns) {
		return Table{}, fmt.Errorf("partition table shapes disagree")
	}
	out := Table{TypeID: left.TypeID, Rows: left.Rows + right.Rows, Columns: make([]Column, len(left.Columns))}
	for index := range left.Columns {
		if left.Columns[index].Field.ID != right.Columns[index].Field.ID {
			return Table{}, fmt.Errorf("partition columns disagree")
		}
		values := append([]Value(nil), left.Columns[index].Values...)
		values = append(values, right.Columns[index].Values...)
		out.Columns[index] = Column{Field: left.Columns[index].Field, Values: values}
	}
	return out, nil
}

// FeaturePage reads only intersecting partitions until the requested page is
// full. It never calls Volume and its physical counters make that observable.
func (reader *Reader) FeaturePage(request FeaturePageRequest) (FeaturePageResult, error) {
	if request.FeatureSet == "" || request.Limit < 0 || request.Limit > maxFeaturePageRows {
		return FeaturePageResult{}, fmt.Errorf("invalid feature page request")
	}
	if request.Limit == 0 {
		request.Limit = 100
	}
	after, err := decodeContinuation(request)
	if err != nil {
		return FeaturePageResult{}, err
	}
	index, err := reader.featureIndex()
	if err != nil {
		return FeaturePageResult{}, err
	}
	if _, indexed := reader.blobs[featureIndexName]; !indexed {
		return reader.legacyFeaturePage(request, after)
	}
	result := FeaturePageResult{}
	passedAfter := after == ""
	for _, partition := range index.Partitions {
		if partition.FeatureSet != request.FeatureSet || request.Bounds != nil && !intersects(partition.Bounds, *request.Bounds) {
			continue
		}
		features, bytesRead, err := reader.readFeaturePartition(partition)
		if err != nil {
			return FeaturePageResult{}, err
		}
		result.PartitionsRead++
		result.BytesRead += bytesRead
		for _, feature := range features {
			if !passedAfter {
				passedAfter = feature.ID == after
				continue
			}
			if request.Bounds != nil {
				bounds, err := geometryBounds(feature.Geometry)
				if err != nil || !intersects(bounds, *request.Bounds) {
					continue
				}
			}
			result.Features = append(result.Features, feature)
			if len(result.Features) == request.Limit {
				result.Next = encodeContinuation(request, feature.ID)
				return result, nil
			}
		}
	}
	if !passedAfter {
		return FeaturePageResult{}, fmt.Errorf("feature continuation is not present")
	}
	return result, nil
}

func (reader *Reader) legacyFeaturePage(request FeaturePageRequest, after string) (FeaturePageResult, error) {
	volume, err := reader.Volume()
	if err != nil {
		return FeaturePageResult{}, err
	}
	result := FeaturePageResult{PartitionsRead: 1}
	for _, name := range []string{featureTableName, relationshipTableName, provenanceTableName} {
		result.BytesRead += reader.tables[name].Length
	}
	passedAfter := after == ""
	foundSet := false
	for _, world := range volume.Worlds {
		for _, set := range world.FeatureSets {
			if set.ID != request.FeatureSet {
				continue
			}
			foundSet = true
			for _, feature := range set.Features {
				if !passedAfter {
					passedAfter = feature.ID == after
					continue
				}
				if request.Bounds != nil {
					bounds, err := geometryBounds(feature.Geometry)
					if err != nil || !intersects(bounds, *request.Bounds) {
						continue
					}
				}
				result.Features = append(result.Features, feature)
				if len(result.Features) == request.Limit {
					result.Next = encodeContinuation(request, feature.ID)
					return result, nil
				}
			}
		}
	}
	if !foundSet {
		return FeaturePageResult{}, fmt.Errorf("feature set is not present")
	}
	if !passedAfter {
		return FeaturePageResult{}, fmt.Errorf("feature continuation is not present")
	}
	return result, nil
}

func (reader *Reader) readFeaturePartition(partition featurePartitionDescriptor) ([]Feature, int64, error) {
	features, err := reader.Table(partition.Features)
	if err != nil {
		return nil, 0, err
	}
	relationships, err := reader.Table(partition.Relationships)
	if err != nil {
		return nil, 0, err
	}
	provenance, err := reader.Table(partition.Provenance)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Feature, features.Rows)
	byID := make(map[string]int, features.Rows)
	core, _ := StandardSchema().Type(CoreID("type.feature"))
	for row := 0; row < features.Rows; row++ {
		geometry, err := decodeGeometry(requiredBytes(features, row, "feature.geometry"))
		if err != nil {
			return nil, 0, err
		}
		feature := Feature{
			ID: requiredString(features, row, "feature.id"), Title: requiredString(features, row, "feature.title"),
			Subtitle: optionalStringAt(features, row, "feature.subtitle"), Description: optionalStringAt(features, row, "feature.description"),
			Shard: requiredInt64(features, row, "feature.shard"), Geometry: geometry,
		}
		if center := optionalBytesAt(features, row, "feature.center"); center != nil {
			position, err := decodePosition(center)
			if err != nil {
				return nil, 0, err
			}
			feature.Center = &position
		}
		for _, column := range features.Columns {
			if _, standard := core.Field(column.Field.ID); !standard && column.Values[row].Kind != KindInvalid {
				feature.Properties = append(feature.Properties, Property{FieldID: column.Field.ID, Field: column.Field, Value: column.Values[row]})
			}
		}
		out[row] = feature
		byID[feature.ID] = row
	}
	for row := 0; row < relationships.Rows; row++ {
		if at, held := byID[requiredString(relationships, row, "relationship.feature")]; held {
			decodeRelationship(&out[at], relationships, row)
		}
	}
	for row := 0; row < provenance.Rows; row++ {
		if at, held := byID[requiredString(provenance, row, "provenance.feature")]; held {
			decodeProvenance(&out[at], provenance, row)
		}
	}
	bytesRead := reader.tables[partition.Features].Length + reader.tables[partition.Relationships].Length + reader.tables[partition.Provenance].Length
	return out, bytesRead, nil
}

func continuationFingerprint(request FeaturePageRequest) [8]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(request.FeatureSet))
	if request.Bounds != nil {
		for _, value := range []float64{request.Bounds.MinX, request.Bounds.MinY, request.Bounds.MaxX, request.Bounds.MaxY} {
			var encoded [8]byte
			binary.LittleEndian.PutUint64(encoded[:], math.Float64bits(value))
			_, _ = hash.Write(encoded[:])
		}
	}
	var out [8]byte
	copy(out[:], hash.Sum(nil))
	return out
}

func encodeContinuation(request FeaturePageRequest, featureID string) string {
	fingerprint := continuationFingerprint(request)
	data := append(fingerprint[:], featureID...)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeContinuation(request FeaturePageRequest) (string, error) {
	if request.After == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(request.After)
	if err != nil || len(data) <= 8 {
		return "", fmt.Errorf("feature continuation is invalid")
	}
	fingerprint := continuationFingerprint(request)
	if !slices.Equal(data[:8], fingerprint[:]) {
		return "", fmt.Errorf("feature continuation belongs to another query")
	}
	return string(data[8:]), nil
}
