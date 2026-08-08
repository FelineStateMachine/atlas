package vnext

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
)

// Kind is a stable primitive wire kind. New encodings may be added for a kind
// without changing its semantic identity.
type Kind uint8

const (
	KindInvalid Kind = iota
	KindBool
	KindInt64
	KindFloat64
	KindString
	KindBytes
	KindID
)

func (kind Kind) valid() bool { return kind >= KindBool && kind <= KindID }

// Field describes one independently evolvable claim on a type.
type Field struct {
	ID       ID     `json:"id"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Optional bool   `json:"optional,omitempty"`
}

// Type is a globally identified record whose fields are also globally
// identified. Declaration order carries no meaning.
type Type struct {
	ID     ID      `json:"id"`
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

// Field finds a field by identity.
func (typ Type) Field(id ID) (Field, bool) {
	for _, field := range typ.Fields {
		if field.ID == id {
			return field, true
		}
	}
	return Field{}, false
}

// Schema is the embedded vocabulary required to interpret a bundle.
type Schema struct {
	Types []Type `json:"types"`
}

// Type finds a record type by identity.
func (schema Schema) Type(id ID) (Type, bool) {
	for _, typ := range schema.Types {
		if typ.ID == id {
			return typ, true
		}
	}
	return Type{}, false
}

// Canonical returns the one order-independent JSON encoding of a schema.
func (schema Schema) Canonical() ([]byte, error) {
	normalized := Schema{Types: append([]Type(nil), schema.Types...)}
	for index := range normalized.Types {
		normalized.Types[index].Fields = append([]Field(nil), normalized.Types[index].Fields...)
		slices.SortFunc(normalized.Types[index].Fields, func(left, right Field) int {
			return slices.Compare(left.ID[:], right.ID[:])
		})
	}
	slices.SortFunc(normalized.Types, func(left, right Type) int {
		return slices.Compare(left.ID[:], right.ID[:])
	})
	if err := normalized.validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical schema: %w", err)
	}
	return data, nil
}

// Hash is the cache identity of the canonical schema, never a compatibility
// gate. Readers merge the embedded schema with the schema they know.
func (schema Schema) Hash() ([32]byte, error) {
	data, err := schema.Canonical()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// UnionSchemas merges schemas produced independently. A shared ID must retain
// its wire kind; names are descriptive and converge deterministically.
func UnionSchemas(schemas ...Schema) (Schema, error) {
	types := make(map[ID]Type)
	for _, schema := range schemas {
		if err := mergeSchema(types, schema); err != nil {
			return Schema{}, err
		}
	}
	out := Schema{Types: make([]Type, 0, len(types))}
	for _, typ := range types {
		out.Types = append(out.Types, typ)
	}
	if _, err := out.Canonical(); err != nil {
		return Schema{}, err
	}
	return canonicalSchema(out), nil
}

func mergeSchema(types map[ID]Type, schema Schema) error {
	for _, incoming := range schema.Types {
		if incoming.ID.isZero() || incoming.Name == "" {
			return fmt.Errorf("schema carries an unnamed or zero-ID type")
		}
		current, held := types[incoming.ID]
		if !held {
			current = Type{ID: incoming.ID, Name: incoming.Name}
		} else if incoming.Name < current.Name {
			current.Name = incoming.Name
		}
		fields := make(map[ID]Field, len(current.Fields)+len(incoming.Fields))
		for _, field := range current.Fields {
			fields[field.ID] = field
		}
		for _, field := range incoming.Fields {
			if err := mergeField(fields, field, incoming.ID); err != nil {
				return err
			}
		}
		current.Fields = current.Fields[:0]
		for _, field := range fields {
			current.Fields = append(current.Fields, field)
		}
		types[incoming.ID] = current
	}
	return nil
}

func mergeField(fields map[ID]Field, incoming Field, typeID ID) error {
	if incoming.ID.isZero() || incoming.Name == "" || !incoming.Kind.valid() {
		return fmt.Errorf("type %s carries an invalid field", typeID)
	}
	current, held := fields[incoming.ID]
	if !held {
		fields[incoming.ID] = incoming
		return nil
	}
	if current.Kind != incoming.Kind {
		return fmt.Errorf("field %s is both wire kind %d and %d", incoming.ID, current.Kind, incoming.Kind)
	}
	if incoming.Name < current.Name {
		current.Name = incoming.Name
	}
	current.Optional = current.Optional || incoming.Optional
	fields[incoming.ID] = current
	return nil
}

func (schema Schema) validate() error {
	types := make(map[ID]bool, len(schema.Types))
	for _, typ := range schema.Types {
		if typ.ID.isZero() || typ.Name == "" || types[typ.ID] {
			return fmt.Errorf("schema carries a duplicate, unnamed, or zero-ID type")
		}
		types[typ.ID] = true
		fields := make(map[ID]bool, len(typ.Fields))
		for _, field := range typ.Fields {
			if field.ID.isZero() || field.Name == "" || !field.Kind.valid() || fields[field.ID] {
				return fmt.Errorf("type %s carries a duplicate or invalid field", typ.ID)
			}
			fields[field.ID] = true
		}
	}
	return nil
}

func canonicalSchema(schema Schema) Schema {
	for index := range schema.Types {
		slices.SortFunc(schema.Types[index].Fields, func(left, right Field) int {
			return slices.Compare(left.ID[:], right.ID[:])
		})
	}
	slices.SortFunc(schema.Types, func(left, right Type) int {
		return slices.Compare(left.ID[:], right.ID[:])
	})
	return schema
}
