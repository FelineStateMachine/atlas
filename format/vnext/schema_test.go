package vnext

import (
	"testing"
)

func TestSchemaUnionAcceptsIndependentExtensions(t *testing.T) {
	t.Parallel()

	core := StandardSchema()
	featureID := CoreID("type.feature")
	emergency := Field{
		ID:   IDFromName("com.example.emergency", "feature.level"),
		Name: "emergencyLevel",
		Kind: KindInt64,
	}
	capacity := Field{
		ID:   IDFromName("org.example.planning", "feature.capacity"),
		Name: "capacity",
		Kind: KindInt64,
	}

	branchA := Schema{Types: []Type{{ID: featureID, Name: "Feature", Fields: []Field{emergency}}}}
	branchB := Schema{Types: []Type{{ID: featureID, Name: "Feature", Fields: []Field{capacity}}}}
	merged, err := UnionSchemas(core, branchA, branchB)
	if err != nil {
		t.Fatalf("union independent schemas: %v", err)
	}

	feature, ok := merged.Type(featureID)
	if !ok {
		t.Fatal("merged schema lost Feature")
	}
	if _, ok := feature.Field(emergency.ID); !ok {
		t.Fatal("merged schema lost branch A field")
	}
	if _, ok := feature.Field(capacity.ID); !ok {
		t.Fatal("merged schema lost branch B field")
	}
}

func TestSchemaUnionRejectsIdentityReuseWithAnotherWireKind(t *testing.T) {
	t.Parallel()

	featureID := CoreID("type.feature")
	fieldID := IDFromName("com.example", "feature.rating")
	left := Schema{Types: []Type{{ID: featureID, Name: "Feature", Fields: []Field{{ID: fieldID, Name: "rating", Kind: KindInt64}}}}}
	right := Schema{Types: []Type{{ID: featureID, Name: "Feature", Fields: []Field{{ID: fieldID, Name: "rating", Kind: KindString}}}}}

	if _, err := UnionSchemas(left, right); err == nil {
		t.Fatal("union accepted one field identity with incompatible wire kinds")
	}
}

func TestCanonicalSchemaIgnoresDeclarationOrder(t *testing.T) {
	t.Parallel()

	a := Field{ID: IDFromName("com.example", "a"), Name: "a", Kind: KindString}
	b := Field{ID: IDFromName("com.example", "b"), Name: "b", Kind: KindFloat64}
	typeID := IDFromName("com.example", "type")
	left := Schema{Types: []Type{{ID: typeID, Name: "Example", Fields: []Field{a, b}}}}
	right := Schema{Types: []Type{{ID: typeID, Name: "Example", Fields: []Field{b, a}}}}

	leftHash, err := left.Hash()
	if err != nil {
		t.Fatalf("hash left schema: %v", err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		t.Fatalf("hash right schema: %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("declaration order changed schema identity: %x != %x", leftHash, rightHash)
	}
}
