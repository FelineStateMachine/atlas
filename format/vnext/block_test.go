package vnext

import (
	"bytes"
	"testing"
)

func TestPackedBlockRoundTripsEveryWireKind(t *testing.T) {
	t.Parallel()

	typeID := IDFromName("com.example", "type.observation")
	fields := []Field{
		{ID: IDFromName("com.example", "bool"), Name: "bool", Kind: KindBool, Optional: true},
		{ID: IDFromName("com.example", "int"), Name: "int", Kind: KindInt64, Optional: true},
		{ID: IDFromName("com.example", "float"), Name: "float", Kind: KindFloat64, Optional: true},
		{ID: IDFromName("com.example", "string"), Name: "string", Kind: KindString, Optional: true},
		{ID: IDFromName("com.example", "bytes"), Name: "bytes", Kind: KindBytes, Optional: true},
		{ID: IDFromName("com.example", "ref"), Name: "ref", Kind: KindID, Optional: true},
	}
	schema := Schema{Types: []Type{{ID: typeID, Name: "Observation", Fields: fields}}}
	hash, err := schema.Hash()
	if err != nil {
		t.Fatalf("hash schema: %v", err)
	}
	want := Table{
		TypeID: typeID,
		Rows:   2,
		Columns: []Column{
			{Field: fields[0], Values: []Value{BoolValue(true), NullValue()}},
			{Field: fields[1], Values: []Value{Int64Value(-42), Int64Value(9001)}},
			{Field: fields[2], Values: []Value{Float64Value(3.5), Float64Value(-0.25)}},
			{Field: fields[3], Values: []Value{StringValue("north"), StringValue("雪")}},
			{Field: fields[4], Values: []Value{BytesValue([]byte{0, 1, 2}), BytesValue(nil)}},
			{Field: fields[5], Values: []Value{IDValue(IDFromName("com.example", "one")), IDValue(IDFromName("com.example", "two"))}},
		},
	}

	data, err := EncodeBlock(hash, want)
	if err != nil {
		t.Fatalf("encode block: %v", err)
	}
	got, err := DecodeBlock(data)
	if err != nil {
		t.Fatalf("decode block: %v", err)
	}
	if diff := diffTable(want, got.Table); diff != "" {
		t.Fatal(diff)
	}
	reencoded, err := EncodeBlock(got.SchemaHash, got.Table)
	if err != nil {
		t.Fatalf("re-encode block: %v", err)
	}
	if !bytes.Equal(data, reencoded) {
		t.Fatal("canonical block changed after decode and re-encode")
	}
}

func TestOlderRuntimePreservesUnknownColumns(t *testing.T) {
	t.Parallel()

	typeID := CoreID("type.feature")
	known := Field{ID: CoreID("feature.title"), Name: "title", Kind: KindString}
	foreign := Field{ID: IDFromName("com.example.rescue", "feature.triage"), Name: "triage", Kind: KindInt64}
	readerSchema := Schema{Types: []Type{{ID: typeID, Name: "Feature", Fields: []Field{known}}}}
	fileSchema, err := UnionSchemas(readerSchema, Schema{Types: []Type{{ID: typeID, Name: "Feature", Fields: []Field{foreign}}}})
	if err != nil {
		t.Fatalf("make file schema: %v", err)
	}
	table := Table{TypeID: typeID, Rows: 1, Columns: []Column{
		{Field: known, Values: []Value{StringValue("Clinic")}},
		{Field: foreign, Values: []Value{Int64Value(4)}},
	}}
	hash, err := fileSchema.Hash()
	if err != nil {
		t.Fatalf("hash file schema: %v", err)
	}
	data, err := EncodeBlock(hash, table)
	if err != nil {
		t.Fatalf("encode file block: %v", err)
	}
	block, err := DecodeBlock(data)
	if err != nil {
		t.Fatalf("decode file block: %v", err)
	}
	view, err := ProjectTable(readerSchema, fileSchema, block.Table)
	if err != nil {
		t.Fatalf("project through old reader: %v", err)
	}
	if len(view.Known.Columns) != 1 || view.Known.Columns[0].Field.ID != known.ID {
		t.Fatalf("known projection = %#v", view.Known.Columns)
	}
	if len(view.Foreign.Columns) != 1 || view.Foreign.Columns[0].Field.ID != foreign.ID {
		t.Fatalf("foreign projection = %#v", view.Foreign.Columns)
	}

	reencoded, err := EncodeBlock(block.SchemaHash, view.Complete())
	if err != nil {
		t.Fatalf("re-encode projected block: %v", err)
	}
	if !bytes.Equal(data, reencoded) {
		t.Fatal("old reader did not preserve the foreign column byte-for-byte")
	}
}

func diffTable(want, got Table) string {
	if want.TypeID != got.TypeID || want.Rows != got.Rows || len(want.Columns) != len(got.Columns) {
		return "decoded table shape differs"
	}
	byID := make(map[ID]Column, len(got.Columns))
	for _, column := range got.Columns {
		byID[column.Field.ID] = column
	}
	for _, left := range want.Columns {
		right, held := byID[left.Field.ID]
		if !held || left.Field.Kind != right.Field.Kind || left.Field.Optional != right.Field.Optional || len(left.Values) != len(right.Values) {
			return "decoded column shape differs"
		}
		for row := range left.Values {
			if !left.Values[row].Equal(right.Values[row]) {
				return "decoded value differs"
			}
		}
	}
	return ""
}
