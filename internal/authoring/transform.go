package authoring

import (
	"fmt"
	"math"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func transformGeometry(geometry vnext.Geometry, mapping GeometryMap, target CoordinateSpace) (vnext.Geometry, error) {
	if familyOf(geometry.Kind) != mapping.Family {
		return vnext.Geometry{}, fmt.Errorf("geometry is %s, feature set requires %s", familyOf(geometry.Kind), mapping.Family)
	}
	out := vnext.Geometry{Kind: geometry.Kind, Parts: make([]vnext.GeometryPart, len(geometry.Parts))}
	for partIndex, part := range geometry.Parts {
		out.Parts[partIndex].Rings = make([][]vnext.Position, len(part.Rings))
		for ringIndex, ring := range part.Rings {
			positions := make([]vnext.Position, len(ring))
			for positionIndex, position := range ring {
				transformed := transformPosition(position, mapping.Transform)
				if !inside(transformed, target.Extent) {
					return vnext.Geometry{}, fmt.Errorf("transformed position %v falls outside target extent", transformed)
				}
				positions[positionIndex] = transformed
			}
			if geometry.Kind == vnext.GeometryPolygon && (len(positions) < 4 || positions[0] != positions[len(positions)-1]) {
				return vnext.Geometry{}, fmt.Errorf("polygon ring is not closed")
			}
			out.Parts[partIndex].Rings[ringIndex] = positions
		}
	}
	return out, nil
}

func sourceExtent(mapping GeometryMap, target CoordinateSpace) ([4]float64, error) {
	if mapping.Transform.Kind == "identity" {
		return target.Extent, nil
	}
	matrix := mapping.Transform.Matrix
	determinant := matrix[0]*matrix[4] - matrix[1]*matrix[3]
	if determinant == 0 {
		return [4]float64{}, fmt.Errorf("affine transform is singular")
	}
	inverse := CoordinateTransform{Kind: "affine", Matrix: [6]float64{
		matrix[4] / determinant,
		-matrix[1] / determinant,
		(matrix[1]*matrix[5] - matrix[4]*matrix[2]) / determinant,
		-matrix[3] / determinant,
		matrix[0] / determinant,
		(matrix[3]*matrix[2] - matrix[0]*matrix[5]) / determinant,
	}}
	corners := []vnext.Position{
		{target.Extent[0], target.Extent[1]}, {target.Extent[0], target.Extent[3]},
		{target.Extent[2], target.Extent[1]}, {target.Extent[2], target.Extent[3]},
	}
	extent := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, corner := range corners {
		position := transformPosition(corner, inverse)
		extent[0], extent[1] = math.Min(extent[0], position[0]), math.Min(extent[1], position[1])
		extent[2], extent[3] = math.Max(extent[2], position[0]), math.Max(extent[3], position[1])
	}
	return extent, nil
}

func transformPosition(position vnext.Position, transform CoordinateTransform) vnext.Position {
	if transform.Kind == "identity" {
		return position
	}
	matrix := transform.Matrix
	return vnext.Position{
		matrix[0]*position[0] + matrix[1]*position[1] + matrix[2],
		matrix[3]*position[0] + matrix[4]*position[1] + matrix[5],
	}
}

func inside(position vnext.Position, extent [4]float64) bool {
	return finite(position[0]) && finite(position[1]) &&
		position[0] >= extent[0] && position[0] <= extent[2] &&
		position[1] >= extent[1] && position[1] <= extent[3]
}

func familyOf(kind vnext.GeometryKind) string {
	switch kind {
	case vnext.GeometryPoint:
		return "point"
	case vnext.GeometryLineString:
		return "path"
	case vnext.GeometryPolygon:
		return "area"
	default:
		return "unknown"
	}
}
