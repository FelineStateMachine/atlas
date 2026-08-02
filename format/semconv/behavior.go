package semconv

import (
	"fmt"
	"strconv"
	"strings"
)

// RenderAs answers how a point collection draws, from its attributes first
// and a legacy display-type field when no attribute speaks. This is the one
// rule readers used to hold as a private string compare, spelled once for
// every consumer: text is text, and everything else is a pin.
//
// The legacy parameter survives only at ingestion, where a producer speaks
// this key for captures that predate the conventions; no payload carries a
// display type.
func RenderAs(attrs map[string]string, legacyDisplayType string) string {
	if value, declared := attrs[KeyRenderAs]; declared {
		return value
	}
	if legacyDisplayType == RenderAsText {
		return RenderAsText
	}
	return RenderAsPin
}

// LabelPolicy answers whether a collection's features wear their names on the
// map, in the producer's word alone: a reader's own override is the
// application's to lay over the top of this.
//
// The ladder, widest step first. A declared [KeyLabelPolicy] is the answer
// whatever the kind. Absent one, an area speaks -- [LabelAlways] -- because
// every map drew its region names before the key existed; a path waits; and a
// point collection speaks exactly when its curation drew it as text, since
// floating names are labels a producer pinned on rather than a different kind
// of thing to draw.
func LabelPolicy(kind string, attrs map[string]string) string {
	if value, declared := attrs[KeyLabelPolicy]; declared {
		return value
	}
	switch kind {
	case GeometryArea:
		return LabelAlways
	case GeometryPath:
		return LabelQuiet
	default:
		if RenderAs(attrs, "") == RenderAsText {
			return LabelAlways
		}
		return LabelQuiet
	}
}

// Equirect is a declared flattening of a sphere: the raster window it fills
// in world pixels, and the ground that window pictures in degrees. West may
// exceed east numerically -- a world centered on the antimeridian -- and the
// span still reads west to east.
type Equirect struct {
	X, Y, W, H               float64
	West, North, East, South float64
}

// ParseEquirect reads the two mapping attributes into one transform, refusing
// windows with no area and grounds with no extent.
func ParseEquirect(px, deg string) (Equirect, error) {
	pxParts, err := floats(px, 4)
	if err != nil {
		return Equirect{}, fmt.Errorf("equirect px: %w", err)
	}
	degParts, err := floats(deg, 4)
	if err != nil {
		return Equirect{}, fmt.Errorf("equirect deg: %w", err)
	}
	e := Equirect{
		X: pxParts[0], Y: pxParts[1], W: pxParts[2], H: pxParts[3],
		West: degParts[0], North: degParts[1], East: degParts[2], South: degParts[3],
	}
	if e.W <= 0 || e.H <= 0 {
		return Equirect{}, fmt.Errorf("equirect window %q has no area", px)
	}
	if e.North == e.South || e.West == e.East {
		return Equirect{}, fmt.Errorf("equirect ground %q has no extent", deg)
	}
	return e, nil
}

// EquirectOf reads a world's declared mapping out of its attributes. The
// false result means the world declares no equirectangular flattening, which
// for a plane is the ordinary case.
func EquirectOf(attrs map[string]string) (Equirect, bool, error) {
	px, hasPx := attrs[KeyGeometryEquirectPx]
	deg, hasDeg := attrs[KeyGeometryEquirectDeg]
	if !hasPx && !hasDeg {
		return Equirect{}, false, nil
	}
	mapping, err := ParseEquirect(px, deg)
	if err != nil {
		return Equirect{}, true, err
	}
	return mapping, true, nil
}

// Apply lands true coordinates on the raster: degrees in, world pixels out.
func (e Equirect) Apply(lat, lon float64) (x, y float64) {
	x = e.X + (lon-e.West)/(e.East-e.West)*e.W
	y = e.Y + (e.North-lat)/(e.North-e.South)*e.H
	return x, y
}

// Invert recovers true coordinates from the raster: world pixels in, degrees
// out. Every packed position runs back through this to stand on the sphere,
// so the mapping declared on the world is the whole story and no feature
// needs to carry its own.
func (e Equirect) Invert(x, y float64) (lat, lon float64) {
	lon = e.West + (x-e.X)/e.W*(e.East-e.West)
	lat = e.North - (y-e.Y)/e.H*(e.North-e.South)
	return lat, lon
}

func floats(value string, count int) ([]float64, error) {
	parts := strings.Split(value, ",")
	if len(parts) != count {
		return nil, fmt.Errorf("%q wants %d numbers", value, count)
	}
	out := make([]float64, count)
	for at, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", part)
		}
		out[at] = parsed
	}
	return out, nil
}
