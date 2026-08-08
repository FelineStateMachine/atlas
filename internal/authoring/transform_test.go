package authoring

import (
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestTransformGeometryAppliesAffineToEveryNestedPosition(t *testing.T) {
	geometry := vnext.Geometry{Kind: vnext.GeometryPolygon, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{
		{{1, 1}, {3, 1}, {3, 2}, {1, 1}},
	}}}}
	mapping := GeometryMap{
		Family: "area", SourceSpace: "source",
		Transform: CoordinateTransform{Kind: "affine", Matrix: [6]float64{10, 0, 5, 0, 20, 7}},
	}
	target := CoordinateSpace{Extent: [4]float64{0, 0, 100, 100}}
	got, err := transformGeometry(geometry, mapping, target)
	if err != nil {
		t.Fatal(err)
	}
	want := []vnext.Position{{15, 27}, {35, 27}, {35, 47}, {15, 27}}
	for index, position := range got.Parts[0].Rings[0] {
		if position != want[index] {
			t.Fatalf("position %d = %v, want %v", index, position, want[index])
		}
	}
}

func TestTransformGeometryRefusesFamilyExtentAndRingViolations(t *testing.T) {
	target := CoordinateSpace{Extent: [4]float64{0, 0, 10, 10}}
	tests := []struct {
		name     string
		geometry vnext.Geometry
		mapping  GeometryMap
	}{
		{
			name:     "family mismatch",
			geometry: vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}}}}}},
			mapping:  GeometryMap{Family: "path", Transform: CoordinateTransform{Kind: "identity"}},
		},
		{
			name:     "outside extent",
			geometry: vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{11, 1}}}}}},
			mapping:  GeometryMap{Family: "point", Transform: CoordinateTransform{Kind: "identity"}},
		},
		{
			name:     "open polygon",
			geometry: vnext.Geometry{Kind: vnext.GeometryPolygon, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}, {2, 1}, {2, 2}, {1, 2}}}}}},
			mapping:  GeometryMap{Family: "area", Transform: CoordinateTransform{Kind: "identity"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := transformGeometry(test.geometry, test.mapping, target); err == nil {
				t.Fatal("transform accepted invalid geometry")
			}
		})
	}
}
