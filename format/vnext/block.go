package vnext

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
)

const (
	blockMagic         = "ATLASPK\x00"
	blockFraming       = uint16(1)
	blockHeaderSize    = 80
	directorySize      = 64
	columnFlagOptional = uint16(1)
	maxBlockRows       = 100_000_000
)

// Block is a decoded typed block. SchemaHash identifies the embedded schema
// used to write it but does not decide whether a reader may open it.
type Block struct {
	SchemaHash [32]byte
	Table      Table
}

type encodedColumn struct {
	field  Field
	nulls  []byte
	data   []byte
	crc    uint32
	nullAt uint64
	dataAt uint64
}

// EncodeBlock writes a deterministic, self-framed hybrid column block.
func EncodeBlock(schemaHash [32]byte, table Table) ([]byte, error) {
	table = canonicalTable(table)
	if table.Rows > maxBlockRows {
		return nil, fmt.Errorf("table has %d rows; maximum is %d", table.Rows, maxBlockRows)
	}
	if err := table.validate(); err != nil {
		return nil, err
	}
	columns, err := encodeColumns(table)
	if err != nil {
		return nil, err
	}
	payloadAt := uint64(blockHeaderSize + len(columns)*directorySize)
	assignOffsets(columns, payloadAt)
	out := make([]byte, payloadAt)
	writeBlockHeader(out, schemaHash, table, payloadAt)
	for index, column := range columns {
		writeDirectory(out[blockHeaderSize+index*directorySize:], column)
		out = append(out, column.nulls...)
		out = append(out, column.data...)
	}
	return out, nil
}

func encodeColumns(table Table) ([]encodedColumn, error) {
	columns := make([]encodedColumn, len(table.Columns))
	for index, column := range table.Columns {
		nulls := presenceBitmap(column.Values)
		data, err := encodeValues(column.Field.Kind, column.Values)
		if err != nil {
			return nil, fmt.Errorf("encode column %s: %w", column.Field.ID, err)
		}
		checksum := crc32.NewIEEE()
		_, _ = checksum.Write(nulls)
		_, _ = checksum.Write(data)
		columns[index] = encodedColumn{field: column.Field, nulls: nulls, data: data, crc: checksum.Sum32()}
	}
	return columns, nil
}

func assignOffsets(columns []encodedColumn, at uint64) {
	for index := range columns {
		columns[index].nullAt = at
		at += uint64(len(columns[index].nulls))
		columns[index].dataAt = at
		at += uint64(len(columns[index].data))
	}
}

func writeBlockHeader(out []byte, schemaHash [32]byte, table Table, payloadAt uint64) {
	copy(out[:8], blockMagic)
	binary.LittleEndian.PutUint16(out[8:10], blockFraming)
	binary.LittleEndian.PutUint32(out[12:16], uint32(table.Rows))
	binary.LittleEndian.PutUint32(out[16:20], uint32(len(table.Columns)))
	copy(out[20:52], schemaHash[:])
	copy(out[52:68], table.TypeID[:])
	binary.LittleEndian.PutUint32(out[68:72], directorySize)
	binary.LittleEndian.PutUint64(out[72:80], payloadAt)
}

func writeDirectory(out []byte, column encodedColumn) {
	copy(out[:16], column.field.ID[:])
	out[16] = byte(column.field.Kind)
	out[17] = byte(column.field.Kind)
	if column.field.Optional {
		binary.LittleEndian.PutUint16(out[18:20], columnFlagOptional)
	}
	binary.LittleEndian.PutUint64(out[20:28], column.nullAt)
	binary.LittleEndian.PutUint64(out[28:36], uint64(len(column.nulls)))
	binary.LittleEndian.PutUint64(out[36:44], column.dataAt)
	binary.LittleEndian.PutUint64(out[44:52], uint64(len(column.data)))
	binary.LittleEndian.PutUint32(out[52:56], column.crc)
}

func presenceBitmap(values []Value) []byte {
	bitmap := make([]byte, (len(values)+7)/8)
	for row, value := range values {
		if value.Kind != KindInvalid {
			bitmap[row/8] |= 1 << (row % 8)
		}
	}
	return bitmap
}

func encodeValues(kind Kind, values []Value) ([]byte, error) {
	switch kind {
	case KindBool:
		return encodeBools(values), nil
	case KindInt64:
		return encodeInt64s(values), nil
	case KindFloat64:
		return encodeFloat64s(values), nil
	case KindString, KindBytes:
		return encodeVariable(kind, values)
	case KindID:
		return encodeIDs(values), nil
	default:
		return nil, fmt.Errorf("unsupported wire kind %d", kind)
	}
}

func encodeBools(values []Value) []byte {
	out := make([]byte, len(values))
	for index, value := range values {
		if value.Kind != KindInvalid && value.Bool {
			out[index] = 1
		}
	}
	return out
}

func encodeInt64s(values []Value) []byte {
	out := make([]byte, len(values)*8)
	for index, value := range values {
		binary.LittleEndian.PutUint64(out[index*8:], uint64(value.Int64))
	}
	return out
}

func encodeFloat64s(values []Value) []byte {
	out := make([]byte, len(values)*8)
	for index, value := range values {
		binary.LittleEndian.PutUint64(out[index*8:], math.Float64bits(value.Float64))
	}
	return out
}

func encodeIDs(values []Value) []byte {
	out := make([]byte, len(values)*16)
	for index, value := range values {
		copy(out[index*16:], value.ID[:])
	}
	return out
}

func encodeVariable(kind Kind, values []Value) ([]byte, error) {
	var payload bytes.Buffer
	offsets := make([]uint32, len(values)+1)
	for index, value := range values {
		offsets[index] = uint32(payload.Len())
		if value.Kind == KindInvalid {
			continue
		}
		if kind == KindString {
			_, _ = payload.WriteString(value.String)
		} else {
			_, _ = payload.Write(value.Bytes)
		}
		if uint64(payload.Len()) > math.MaxUint32 {
			return nil, fmt.Errorf("variable column exceeds 4 GiB")
		}
	}
	offsets[len(values)] = uint32(payload.Len())
	out := make([]byte, len(offsets)*4+payload.Len())
	for index, offset := range offsets {
		binary.LittleEndian.PutUint32(out[index*4:], offset)
	}
	copy(out[len(offsets)*4:], payload.Bytes())
	return out, nil
}

// DecodeBlock validates and opens one block without consulting an application
// schema. Unknown fields therefore remain ordinary columns.
func DecodeBlock(data []byte) (Block, error) {
	header, err := readBlockHeader(data)
	if err != nil {
		return Block{}, err
	}
	table := Table{TypeID: header.typeID, Rows: header.rows, Columns: make([]Column, header.columns)}
	for index := 0; index < header.columns; index++ {
		at := blockHeaderSize + index*directorySize
		column, err := decodeColumn(data, data[at:at+directorySize], header.rows)
		if err != nil {
			return Block{}, fmt.Errorf("decode column %d: %w", index, err)
		}
		table.Columns[index] = column
	}
	if err := table.validate(); err != nil {
		return Block{}, err
	}
	return Block{SchemaHash: header.schemaHash, Table: canonicalTable(table)}, nil
}

type decodedHeader struct {
	rows       int
	columns    int
	schemaHash [32]byte
	typeID     ID
}

func readBlockHeader(data []byte) (decodedHeader, error) {
	if len(data) < blockHeaderSize || string(data[:8]) != blockMagic {
		return decodedHeader{}, fmt.Errorf("typed block has no Atlas magic")
	}
	if framing := binary.LittleEndian.Uint16(data[8:10]); framing != blockFraming {
		return decodedHeader{}, fmt.Errorf("typed block framing %d is not supported", framing)
	}
	columns := int(binary.LittleEndian.Uint32(data[16:20]))
	directoryEnd := blockHeaderSize + columns*directorySize
	if columns < 0 || directoryEnd < blockHeaderSize || directoryEnd > len(data) {
		return decodedHeader{}, fmt.Errorf("typed block directory is truncated")
	}
	if binary.LittleEndian.Uint32(data[68:72]) != directorySize || binary.LittleEndian.Uint64(data[72:80]) != uint64(directoryEnd) {
		return decodedHeader{}, fmt.Errorf("typed block directory framing is invalid")
	}
	header := decodedHeader{rows: int(binary.LittleEndian.Uint32(data[12:16])), columns: columns}
	if header.rows > maxBlockRows || header.columns == 0 && header.rows != 0 {
		return decodedHeader{}, fmt.Errorf("typed block row count is invalid")
	}
	copy(header.schemaHash[:], data[20:52])
	copy(header.typeID[:], data[52:68])
	return header, nil
}

func decodeColumn(data, directory []byte, rows int) (Column, error) {
	var fieldID ID
	copy(fieldID[:], directory[:16])
	kind := Kind(directory[16])
	if !kind.valid() || directory[17] != byte(kind) {
		return Column{}, fmt.Errorf("column has unsupported encoding")
	}
	optional := binary.LittleEndian.Uint16(directory[18:20])&columnFlagOptional != 0
	nulls, err := blockSlice(data, directory[20:28], directory[28:36])
	if err != nil || len(nulls) != (rows+7)/8 {
		return Column{}, fmt.Errorf("column presence bitmap is invalid")
	}
	payload, err := blockSlice(data, directory[36:44], directory[44:52])
	if err != nil {
		return Column{}, fmt.Errorf("column payload is invalid")
	}
	if checksum(nulls, payload) != binary.LittleEndian.Uint32(directory[52:56]) {
		return Column{}, fmt.Errorf("column checksum does not match")
	}
	values, err := decodeValues(kind, rows, nulls, payload)
	if err != nil {
		return Column{}, err
	}
	return Column{Field: Field{ID: fieldID, Kind: kind, Optional: optional}, Values: values}, nil
}

func blockSlice(data, offsetBytes, lengthBytes []byte) ([]byte, error) {
	offset := binary.LittleEndian.Uint64(offsetBytes)
	length := binary.LittleEndian.Uint64(lengthBytes)
	if offset > uint64(len(data)) || length > uint64(len(data))-offset {
		return nil, fmt.Errorf("range exceeds block")
	}
	return data[offset : offset+length], nil
}

func checksum(parts ...[]byte) uint32 {
	hash := crc32.NewIEEE()
	for _, part := range parts {
		_, _ = hash.Write(part)
	}
	return hash.Sum32()
}

func decodeValues(kind Kind, rows int, present, payload []byte) ([]Value, error) {
	values := make([]Value, rows)
	switch kind {
	case KindBool:
		return decodeBools(values, present, payload)
	case KindInt64:
		return decodeInt64s(values, present, payload)
	case KindFloat64:
		return decodeFloat64s(values, present, payload)
	case KindString, KindBytes:
		return decodeVariable(kind, values, present, payload)
	case KindID:
		return decodeIDs(values, present, payload)
	default:
		return nil, fmt.Errorf("unsupported wire kind %d", kind)
	}
}

func isPresent(bitmap []byte, row int) bool { return bitmap[row/8]&(1<<(row%8)) != 0 }

func decodeBools(values []Value, present, payload []byte) ([]Value, error) {
	if len(payload) != len(values) {
		return nil, fmt.Errorf("boolean column has wrong length")
	}
	for row := range values {
		if isPresent(present, row) {
			values[row] = BoolValue(payload[row] != 0)
		}
	}
	return values, nil
}

func decodeInt64s(values []Value, present, payload []byte) ([]Value, error) {
	if len(payload) != len(values)*8 {
		return nil, fmt.Errorf("int64 column has wrong length")
	}
	for row := range values {
		if isPresent(present, row) {
			values[row] = Int64Value(int64(binary.LittleEndian.Uint64(payload[row*8:])))
		}
	}
	return values, nil
}

func decodeFloat64s(values []Value, present, payload []byte) ([]Value, error) {
	if len(payload) != len(values)*8 {
		return nil, fmt.Errorf("float64 column has wrong length")
	}
	for row := range values {
		if isPresent(present, row) {
			values[row] = Float64Value(math.Float64frombits(binary.LittleEndian.Uint64(payload[row*8:])))
		}
	}
	return values, nil
}

func decodeIDs(values []Value, present, payload []byte) ([]Value, error) {
	if len(payload) != len(values)*16 {
		return nil, fmt.Errorf("ID column has wrong length")
	}
	for row := range values {
		if isPresent(present, row) {
			var id ID
			copy(id[:], payload[row*16:(row+1)*16])
			values[row] = IDValue(id)
		}
	}
	return values, nil
}

func decodeVariable(kind Kind, values []Value, present, payload []byte) ([]Value, error) {
	offsetBytes := (len(values) + 1) * 4
	if len(payload) < offsetBytes {
		return nil, fmt.Errorf("variable column offsets are truncated")
	}
	content := payload[offsetBytes:]
	previous := uint32(0)
	for row := range values {
		start := binary.LittleEndian.Uint32(payload[row*4:])
		end := binary.LittleEndian.Uint32(payload[(row+1)*4:])
		if start < previous || end < start || uint64(end) > uint64(len(content)) {
			return nil, fmt.Errorf("variable column offsets are invalid at row %d", row)
		}
		previous = end
		if !isPresent(present, row) {
			continue
		}
		if kind == KindString {
			values[row] = StringValue(string(content[start:end]))
		} else {
			values[row] = BytesValue(content[start:end])
		}
	}
	return values, nil
}
