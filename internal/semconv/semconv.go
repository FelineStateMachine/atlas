// Package semconv is the registry of Atlas's semantic conventions: the
// attribute keys that carry display and geometry meaning through the
// pipeline as a shared language, the way OpenTelemetry's semantic
// conventions carry meaning between systems that never met.
//
// The conventions are Atlas's own. They exist precisely so the format is
// beholden to no upstream's habits -- not MapGenie's category grammar, not
// the games' assumptions, not the sciences' tiling -- and every rule that
// used to be an unspoken promotion inside one tool becomes a named key any
// producer can write and any reader can act on.
//
// The contract has two sides, deliberately asymmetric. Producers are strict:
// an attribute in the atlas namespace that the registry does not know, or a
// value outside its vocabulary, fails the build while the mistake is one
// change old. Readers are lenient: an unknown key is ignored, never refused,
// so a bundle written by a newer pipeline still opens in an older app. New
// keys arrive with experimental stability and earn stable; only a breaking
// change to an existing vocabulary would move Version.
package semconv

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Version names the vocabulary a bundle was written against. It rides the
// manifest as "conventions" and moves only when an existing key's meaning
// breaks -- additions ride on per-key stability instead.
const Version = 1

// Entity is what an attribute attaches to.
type Entity string

const (
	EntityBundle   Entity = "bundle"
	EntityWorld    Entity = "world"
	EntityCategory Entity = "category"
	EntityLocation Entity = "location"
)

// Stability says how settled a key is. Experimental keys may still change
// spelling or vocabulary; stable keys only break with a Version move.
type Stability string

const (
	Stable       Stability = "stable"
	Experimental Stability = "experimental"
)

// The registered keys.
const (
	// RenderAs says how a category's locations are drawn: as markers or as
	// floating text labels. It replaces the pipeline's single unspoken
	// display rule, the string compare on MapGenie's display_type.
	KeyRenderAs = "atlas.render.as"

	// IconStd names a standard-library icon for a category that has no
	// artwork of its own, as set/name, e.g. "maki/mountain". The ingestion
	// pipeline resolves it to embedded bytes; the app only ever sees the
	// resolved asset.
	KeyIconStd = "atlas.icon.std"

	// IconKind says whether a category's icon asset is a monochrome glyph
	// the viewer tints, or a picture drawn as-is. It names what used to be
	// inferred from the asset's file extension.
	KeyIconKind = "atlas.icon.kind"

	// IconOutset says which rim a map's markers wear so they stay legible
	// against its art: the light rim of a dark map or the dark rim of a
	// light one.
	KeyIconOutset = "atlas.icon.outset"

	// GeometrySurface declares what the map's raster is a picture of: a
	// plane, which every map was until a planet arrived, or a sphere. A map
	// that says nothing is a plane.
	KeyGeometrySurface = "atlas.geometry.surface"

	// GeometryProjection says how a spherical surface was flattened into
	// the raster. Equirectangular is the only vocabulary so far.
	KeyGeometryProjection = "atlas.geometry.projection"

	// GeometryEquirectPx is the raster window the projection fills, in
	// world pixels, as "x,y,w,h".
	KeyGeometryEquirectPx = "atlas.geometry.equirect.px"

	// GeometryEquirectDeg is the ground that window pictures, in degrees,
	// as "west,north,east,south".
	KeyGeometryEquirectDeg = "atlas.geometry.equirect.deg"

	// GeometryMercatorPx is the raster window a Web-Mercator cut fills, in
	// world pixels, as "x,y,w,h". Where equirect declares y linear in
	// degrees, mercator declares y linear in the projected latitude --
	// asinh(tan latitude) -- which is the flattening a real-world tile
	// window actually is.
	KeyGeometryMercatorPx = "atlas.geometry.mercator.px"

	// GeometryMercatorDeg is the ground that window pictures, in degrees at
	// the window's edges, as "west,north,east,south".
	KeyGeometryMercatorDeg = "atlas.geometry.mercator.deg"

	// GeometryBody names the body pictured, e.g. "mars".
	KeyGeometryBody = "atlas.geometry.body"

	// GeometryRadiusKM is the body's mean radius in kilometers, as a
	// decimal string.
	KeyGeometryRadiusKM = "atlas.geometry.radius_km"

	// GeoLat and GeoLon carry a location's true planetary coordinates as
	// the source published them: planetocentric degrees, east-positive
	// longitude. They are provenance and card material; rendering derives
	// positions from the map-level mapping instead.
	KeyGeoLat = "atlas.geo.lat"
	KeyGeoLon = "atlas.geo.lon"

	// CategoryKey is a category's merge identity: the slug categories from
	// different sources meet under when they mean the same concept. Absent,
	// the icon key stands in, which is today's behavior named.
	KeyCategoryKey = "atlas.category.key"

	// NoteText never appears in a payload: it is the name the merge policy
	// table and ledger use for a pin's description, so "which description
	// wins" is decided and recorded in the same vocabulary as everything
	// else.
	KeyNoteText = "atlas.note.text"
)

// Vocabulary values.
const (
	RenderAsPin  = "pin"
	RenderAsText = "text"

	IconKindGlyph   = "glyph"
	IconKindPicture = "picture"

	OutsetLight = "light"
	OutsetDark  = "dark"

	SurfacePlane  = "plane"
	SurfaceSphere = "sphere"

	ProjectionEquirect = "equirect"
)

// definition is one registered key: where it attaches, how settled it is,
// and what values it admits.
type definition struct {
	entity    Entity
	stability Stability
	check     func(value string) error
}

func enum(values ...string) func(string) error {
	return func(value string) error {
		if slices.Contains(values, value) {
			return nil
		}
		return fmt.Errorf("%q is not one of %s", value, strings.Join(values, "|"))
	}
}

func slug(value string) error {
	if value == "" {
		return fmt.Errorf("empty")
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("%q is not a slug", value)
	}
	return nil
}

func decimal(value string) error {
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return fmt.Errorf("%q is not a number", value)
	}
	return nil
}

func setName(value string) error {
	set, name, found := strings.Cut(value, "/")
	if !found || set == "" || name == "" {
		return fmt.Errorf("%q is not set/name", value)
	}
	if err := slug(set); err != nil {
		return err
	}
	return slug(name)
}

func numbers(count int) func(string) error {
	return func(value string) error {
		parts := strings.Split(value, ",")
		if len(parts) != count {
			return fmt.Errorf("%q wants %d comma-separated numbers", value, count)
		}
		for _, part := range parts {
			if _, err := strconv.ParseFloat(part, 64); err != nil {
				return fmt.Errorf("%q is not a number", part)
			}
		}
		return nil
	}
}

// registry is the whole vocabulary. REGISTRY.md is its prose twin, and a
// test holds the two to the same list of keys.
var registry = map[string]definition{
	KeyRenderAs:            {EntityCategory, Stable, enum(RenderAsPin, RenderAsText)},
	KeyIconStd:             {EntityCategory, Stable, setName},
	KeyIconKind:            {EntityCategory, Stable, enum(IconKindGlyph, IconKindPicture)},
	KeyIconOutset:          {EntityWorld, Stable, enum(OutsetLight, OutsetDark)},
	KeyGeometrySurface:     {EntityWorld, Stable, enum(SurfacePlane, SurfaceSphere)},
	KeyGeometryProjection:  {EntityWorld, Stable, enum(ProjectionEquirect)},
	KeyGeometryEquirectPx:  {EntityWorld, Stable, numbers(4)},
	KeyGeometryEquirectDeg: {EntityWorld, Stable, numbers(4)},
	KeyGeometryMercatorPx:  {EntityWorld, Experimental, numbers(4)},
	KeyGeometryMercatorDeg: {EntityWorld, Experimental, numbers(4)},
	KeyGeometryBody:        {EntityWorld, Experimental, slug},
	KeyGeometryRadiusKM:    {EntityWorld, Experimental, decimal},
	KeyGeoLat:              {EntityLocation, Experimental, decimal},
	KeyGeoLon:              {EntityLocation, Experimental, decimal},
	KeyCategoryKey:         {EntityCategory, Experimental, slug},
}

// Keys lists every registered key in a stable order, for the tests and the
// tools that report on adoption.
func Keys() []string {
	out := make([]string, 0, len(registry))
	for key := range registry {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// EntityOf reports where a key attaches, for tools walking a payload.
func EntityOf(key string) (Entity, bool) {
	definition, known := registry[key]
	return definition.entity, known
}

// StabilityOf reports how settled a key is.
func StabilityOf(key string) (Stability, bool) {
	definition, known := registry[key]
	return definition.stability, known
}

// Validate holds one entity's attributes to the registry: every key in the
// atlas namespace must be registered, attached to this entity, and carry a
// value its vocabulary admits. Keys outside the namespace are not this
// registry's business and pass through -- but nothing in the pipeline
// writes any today.
func Validate(entity Entity, attrs map[string]string) error {
	for _, key := range sortedKeys(attrs) {
		if !strings.HasPrefix(key, "atlas.") {
			continue
		}
		definition, known := registry[key]
		if !known {
			return fmt.Errorf("attribute %q is not registered", key)
		}
		if definition.entity != entity {
			return fmt.Errorf("attribute %q attaches to a %s, not a %s", key, definition.entity, entity)
		}
		if err := definition.check(attrs[key]); err != nil {
			return fmt.Errorf("attribute %q: %w", key, err)
		}
	}
	return nil
}

// RenderAs answers how a category draws, from its attributes first and the
// legacy display_type field when no attribute speaks. This is the one rule
// the viewer used to hold as a string compare, spelled once for every
// consumer: text is text, and everything else is a pin.
func RenderAs(attrs map[string]string, legacyDisplayType string) string {
	if value, declared := attrs[KeyRenderAs]; declared {
		return value
	}
	if legacyDisplayType == "text" {
		return RenderAsText
	}
	return RenderAsPin
}

// Equirect is a declared flattening of a sphere: the raster window it fills
// in world pixels, and the ground that window pictures in degrees. West may
// exceed east numerically (a map centered on the antimeridian); the span
// still reads west-to-east.
type Equirect struct {
	X, Y, W, H               float64
	West, North, East, South float64
}

// ParseEquirect reads the two mapping attributes into one transform,
// refusing windows with no area.
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

// Apply lands true coordinates on the raster: degrees in, world pixels out.
func (e Equirect) Apply(lat, lon float64) (x, y float64) {
	x = e.X + (lon-e.West)/(e.East-e.West)*e.W
	y = e.Y + (e.North-lat)/(e.North-e.South)*e.H
	return x, y
}

// Invert recovers true coordinates from the raster: world pixels in,
// degrees out. Every pin's packed position runs back through this to stand
// on the sphere, so the mapping declared on the map is the whole story and
// no pin needs to carry its own.
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

func sortedKeys(attrs map[string]string) []string {
	out := make([]string, 0, len(attrs))
	for key := range attrs {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
