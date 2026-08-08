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
	transformed := vnext.Geometry{Kind: geometry.Kind, Parts: make([]vnext.GeometryPart, len(geometry.Parts))}
	for partIndex, part := range geometry.Parts {
		transformed.Parts[partIndex].Rings = make([][]vnext.Position, len(part.Rings))
		for ringIndex, ring := range part.Rings {
			if geometry.Kind == vnext.GeometryPolygon && (len(ring) < 4 || ring[0] != ring[len(ring)-1]) {
				return vnext.Geometry{}, fmt.Errorf("polygon ring is not closed")
			}
			positions := make([]vnext.Position, len(ring))
			for positionIndex, position := range ring {
				positions[positionIndex] = transformPosition(position, mapping.Transform)
			}
			transformed.Parts[partIndex].Rings[ringIndex] = positions
		}
	}
	clipped := clipGeometry(transformed, target.Extent)
	if len(clipped.Parts) == 0 {
		return vnext.Geometry{}, fmt.Errorf("transformed geometry falls outside target extent")
	}
	return clampGeometry(clipped, target.Extent), nil
}

// clampGeometry removes floating-point dust left by affine projection and
// segment intersection at an exact target edge. Clipping has already decided
// topology; this only makes its closed-bound promise exact on disk.
func clampGeometry(geometry vnext.Geometry, extent [4]float64) vnext.Geometry {
	for partIndex := range geometry.Parts {
		for ringIndex := range geometry.Parts[partIndex].Rings {
			for positionIndex, position := range geometry.Parts[partIndex].Rings[ringIndex] {
				geometry.Parts[partIndex].Rings[ringIndex][positionIndex] = vnext.Position{
					math.Max(extent[0], math.Min(extent[2], position[0])),
					math.Max(extent[1], math.Min(extent[3], position[1])),
				}
			}
		}
	}
	return geometry
}

func clipGeometry(geometry vnext.Geometry, extent [4]float64) vnext.Geometry {
	out := vnext.Geometry{Kind: geometry.Kind}
	switch geometry.Kind {
	case vnext.GeometryPoint:
		for _, part := range geometry.Parts {
			if len(part.Rings) == 1 && len(part.Rings[0]) == 1 && inside(part.Rings[0][0], extent) {
				out.Parts = append(out.Parts, part)
			}
		}
	case vnext.GeometryLineString:
		for _, part := range geometry.Parts {
			if len(part.Rings) != 1 {
				continue
			}
			for _, line := range clipLine(part.Rings[0], extent) {
				out.Parts = append(out.Parts, vnext.GeometryPart{Rings: [][]vnext.Position{line}})
			}
		}
	case vnext.GeometryPolygon:
		for _, part := range geometry.Parts {
			if len(part.Rings) == 0 {
				continue
			}
			exterior := clipRing(part.Rings[0], extent)
			if len(exterior) == 0 {
				continue
			}
			rings := [][]vnext.Position{exterior}
			for _, hole := range part.Rings[1:] {
				if clipped := clipRing(hole, extent); len(clipped) > 0 {
					rings = append(rings, clipped)
				}
			}
			out.Parts = append(out.Parts, vnext.GeometryPart{Rings: rings})
		}
	}
	return out
}

func clipLine(line []vnext.Position, extent [4]float64) [][]vnext.Position {
	var out [][]vnext.Position
	var current []vnext.Position
	flush := func() {
		if len(current) >= 2 {
			out = append(out, current)
		}
		current = nil
	}
	for index := 1; index < len(line); index++ {
		start, end, held := clipSegment(line[index-1], line[index], extent)
		if !held {
			flush()
			continue
		}
		if len(current) == 0 || current[len(current)-1] != start {
			flush()
			current = []vnext.Position{start}
		}
		if current[len(current)-1] != end {
			current = append(current, end)
		}
	}
	flush()
	return out
}

func clipRing(ring []vnext.Position, extent [4]float64) []vnext.Position {
	if len(ring) < 4 || ring[0] != ring[len(ring)-1] {
		return nil
	}
	current := append([]vnext.Position(nil), ring[:len(ring)-1]...)
	insideEdge := [4]func(vnext.Position) bool{
		func(point vnext.Position) bool { return point[0] >= extent[0] },
		func(point vnext.Position) bool { return point[0] <= extent[2] },
		func(point vnext.Position) bool { return point[1] >= extent[1] },
		func(point vnext.Position) bool { return point[1] <= extent[3] },
	}
	crossing := [4]func(vnext.Position, vnext.Position) vnext.Position{
		func(from, to vnext.Position) vnext.Position { return crossX(from, to, extent[0]) },
		func(from, to vnext.Position) vnext.Position { return crossX(from, to, extent[2]) },
		func(from, to vnext.Position) vnext.Position { return crossY(from, to, extent[1]) },
		func(from, to vnext.Position) vnext.Position { return crossY(from, to, extent[3]) },
	}
	for edge := range insideEdge {
		if len(current) == 0 {
			return nil
		}
		next := make([]vnext.Position, 0, len(current)+4)
		for index, to := range current {
			from := current[(index+len(current)-1)%len(current)]
			fromInside, toInside := insideEdge[edge](from), insideEdge[edge](to)
			switch {
			case fromInside && toInside:
				next = append(next, to)
			case fromInside:
				next = append(next, crossing[edge](from, to))
			case toInside:
				next = append(next, crossing[edge](from, to), to)
			}
		}
		current = next
	}
	if len(current) < 3 {
		return nil
	}
	return append(current, current[0])
}

func clipSegment(from, to vnext.Position, extent [4]float64) (vnext.Position, vnext.Position, bool) {
	enter, leave := 0.0, 1.0
	dx, dy := to[0]-from[0], to[1]-from[1]
	narrow := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		at := q / p
		if p < 0 {
			if at > leave {
				return false
			}
			enter = math.Max(enter, at)
			return true
		}
		if at < enter {
			return false
		}
		leave = math.Min(leave, at)
		return true
	}
	if !narrow(-dx, from[0]-extent[0]) || !narrow(dx, extent[2]-from[0]) ||
		!narrow(-dy, from[1]-extent[1]) || !narrow(dy, extent[3]-from[1]) {
		return from, to, false
	}
	return vnext.Position{from[0] + enter*dx, from[1] + enter*dy},
		vnext.Position{from[0] + leave*dx, from[1] + leave*dy}, true
}

func crossX(from, to vnext.Position, x float64) vnext.Position {
	at := (x - from[0]) / (to[0] - from[0])
	return vnext.Position{x, from[1] + at*(to[1]-from[1])}
}

func crossY(from, to vnext.Position, y float64) vnext.Position {
	at := (y - from[1]) / (to[1] - from[1])
	return vnext.Position{from[0] + at*(to[0]-from[0]), y}
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
