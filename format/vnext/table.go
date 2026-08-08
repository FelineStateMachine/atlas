package vnext

import (
	"bytes"
	"fmt"
	"math"
	"slices"
)

// Value is one scalar in a typed column. KindInvalid is the null value.
type Value struct {
	Kind    Kind
	Bool    bool
	Int64   int64
	Float64 float64
	String  string
	Bytes   []byte
	ID      ID
}

func NullValue() Value                 { return Value{} }
func BoolValue(value bool) Value       { return Value{Kind: KindBool, Bool: value} }
func Int64Value(value int64) Value     { return Value{Kind: KindInt64, Int64: value} }
func Float64Value(value float64) Value { return Value{Kind: KindFloat64, Float64: value} }
func StringValue(value string) Value   { return Value{Kind: KindString, String: value} }
func IDValue(value ID) Value           { return Value{Kind: KindID, ID: value} }

func BytesValue(value []byte) Value {
	return Value{Kind: KindBytes, Bytes: append([]byte(nil), value...)}
}

// Equal compares values while preserving float bit patterns and byte content.
func (value Value) Equal(other Value) bool {
	return value.Kind == other.Kind &&
		value.Bool == other.Bool &&
		value.Int64 == other.Int64 &&
		math.Float64bits(value.Float64) == math.Float64bits(other.Float64) &&
		value.String == other.String &&
		bytes.Equal(value.Bytes, other.Bytes) &&
		value.ID == other.ID
}

// Column is one field laid out for every row in a table.
type Column struct {
	Field  Field
	Values []Value
}

// Table is one root type encoded as hybrid columns.
type Table struct {
	TypeID  ID
	Rows    int
	Columns []Column
}

// Value returns one field value from one row.
func (table Table) Value(fieldID ID, row int) (Value, bool) {
	for _, column := range table.Columns {
		if column.Field.ID == fieldID {
			return column.Values[row], true
		}
	}
	return Value{}, false
}

func (table Table) validate() error {
	if table.TypeID.isZero() || table.Rows < 0 {
		return fmt.Errorf("table has invalid type or row count")
	}
	seen := make(map[ID]bool, len(table.Columns))
	for _, column := range table.Columns {
		if column.Field.ID.isZero() || !column.Field.Kind.valid() || seen[column.Field.ID] {
			return fmt.Errorf("table %s carries a duplicate or invalid column", table.TypeID)
		}
		seen[column.Field.ID] = true
		if len(column.Values) != table.Rows {
			return fmt.Errorf("column %s has %d values for %d rows", column.Field.ID, len(column.Values), table.Rows)
		}
		for row, value := range column.Values {
			if value.Kind == KindInvalid && !column.Field.Optional {
				return fmt.Errorf("required column %s is null at row %d", column.Field.ID, row)
			}
			if value.Kind != KindInvalid && value.Kind != column.Field.Kind {
				return fmt.Errorf("column %s expects kind %d, got %d at row %d", column.Field.ID, column.Field.Kind, value.Kind, row)
			}
		}
	}
	return nil
}

func canonicalTable(table Table) Table {
	out := Table{TypeID: table.TypeID, Rows: table.Rows, Columns: append([]Column(nil), table.Columns...)}
	slices.SortFunc(out.Columns, func(left, right Column) int {
		return slices.Compare(left.Field.ID[:], right.Field.ID[:])
	})
	return out
}

// TableProjection separates columns understood by an application from newer
// file columns while retaining both for lossless pass-through.
type TableProjection struct {
	Known   Table
	Foreign Table
}

// Complete reunites known and foreign columns in canonical order.
func (projection TableProjection) Complete() Table {
	columns := append([]Column(nil), projection.Known.Columns...)
	columns = append(columns, projection.Foreign.Columns...)
	return canonicalTable(Table{TypeID: projection.Known.TypeID, Rows: projection.Known.Rows, Columns: columns})
}

// ProjectTable classifies a file table using an application's schema. The
// embedded file schema remains authoritative for field metadata.
func ProjectTable(application, file Schema, table Table) (TableProjection, error) {
	if _, err := UnionSchemas(application, file); err != nil {
		return TableProjection{}, fmt.Errorf("merge application and file schemas: %w", err)
	}
	fileType, ok := file.Type(table.TypeID)
	if !ok {
		return TableProjection{}, fmt.Errorf("file schema does not describe table type %s", table.TypeID)
	}
	applicationType, applicationKnowsType := application.Type(table.TypeID)
	projection := TableProjection{
		Known:   Table{TypeID: table.TypeID, Rows: table.Rows},
		Foreign: Table{TypeID: table.TypeID, Rows: table.Rows},
	}
	for _, column := range table.Columns {
		field, held := fileType.Field(column.Field.ID)
		if !held || field.Kind != column.Field.Kind {
			return TableProjection{}, fmt.Errorf("file schema disagrees with column %s", column.Field.ID)
		}
		column.Field = field
		if _, known := applicationType.Field(field.ID); applicationKnowsType && known {
			projection.Known.Columns = append(projection.Known.Columns, column)
		} else {
			projection.Foreign.Columns = append(projection.Foreign.Columns, column)
		}
	}
	projection.Known = canonicalTable(projection.Known)
	projection.Foreign = canonicalTable(projection.Foreign)
	return projection, nil
}
