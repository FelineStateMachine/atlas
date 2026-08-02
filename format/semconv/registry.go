package semconv

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Namespace prefixes every key the registry governs. An attribute outside it
// is not this registry's business and passes through untouched.
const Namespace = "atlas."

// The registered keys.
const (
	// KeyGeometryKind says what shape of thing a collection holds: points,
	// paths, or areas. Every collection declares one kind and every feature in
	// it is that kind; readers pick their rendering and UX by this key instead
	// of sniffing geometry types at draw time.
	KeyGeometryKind = "atlas.geometry.kind"

	// KeyLabelPolicy says whether an area collection's features wear their
	// names on the map always, or quietly -- only on highlight, selection, or
	// an explicit reveal. Area collections only: absent means always for
	// areas, and paths are always quiet.
	KeyLabelPolicy = "atlas.label.policy"

	// KeyRenderAs says how a point collection's features are drawn: as markers
	// or as floating text labels.
	KeyRenderAs = "atlas.render.as"

	// KeyIconStd names a standard-library icon for a collection that has no
	// artwork of its own, as set/name, e.g. "maki/mountain". A producer
	// resolves it to embedded bytes; a reader only ever sees the resolved
	// asset.
	KeyIconStd = "atlas.icon.std"

	// KeyIconKind says whether a collection's icon asset is a monochrome glyph
	// the reader tints, or a picture drawn as-is.
	KeyIconKind = "atlas.icon.kind"

	// KeyIconOutset says which rim a world's markers wear so they stay legible
	// against its art: the light rim of a dark raster or the dark rim of a
	// light one.
	KeyIconOutset = "atlas.icon.outset"

	// KeyGeometrySurface declares what a world's raster is a picture of: a
	// plane or a sphere. A world that says nothing is a plane.
	KeyGeometrySurface = "atlas.geometry.surface"

	// KeyGeometryProjection says how a spherical surface was flattened into
	// the raster. Equirectangular is the only vocabulary so far.
	KeyGeometryProjection = "atlas.geometry.projection"

	// KeyGeometryEquirectPx is the raster window the projection fills, in
	// world pixels, as "x,y,w,h".
	KeyGeometryEquirectPx = "atlas.geometry.equirect.px"

	// KeyGeometryEquirectDeg is the ground that window pictures, in degrees,
	// as "west,north,east,south".
	KeyGeometryEquirectDeg = "atlas.geometry.equirect.deg"

	// KeyGeometryMercatorPx is the raster window a Web-Mercator cut fills, in
	// world pixels, as "x,y,w,h". Where equirect declares y linear in degrees,
	// mercator declares y linear in the projected latitude -- asinh(tan
	// latitude) -- which is the flattening a real-world tile window actually
	// is.
	KeyGeometryMercatorPx = "atlas.geometry.mercator.px"

	// KeyGeometryMercatorDeg is the ground a Mercator window pictures, in
	// degrees at the window's edges, as "west,north,east,south".
	KeyGeometryMercatorDeg = "atlas.geometry.mercator.deg"

	// KeyGeometryBody names the body pictured, e.g. "mars".
	KeyGeometryBody = "atlas.geometry.body"

	// KeyGeometryRadiusKM is the body's mean radius in kilometers, as a
	// decimal string.
	KeyGeometryRadiusKM = "atlas.geometry.radius_km"

	// KeyGeoLat and KeyGeoLon carry a feature's true planetary coordinates as
	// the source published them: planetocentric degrees, east-positive
	// longitude. They are provenance and card material; rendering derives
	// positions from the world-level mapping instead.
	KeyGeoLat = "atlas.geo.lat"
	KeyGeoLon = "atlas.geo.lon"

	// KeyHydroHUC12 names the USGS twelve-digit hydrologic unit -- the
	// subwatershed -- a feature's ground lies wholly within. A feature
	// spanning subwatersheds carries no key rather than a misleading one.
	KeyHydroHUC12 = "atlas.hydro.huc12"

	// KeyStrokeWidthPx is the ground width of a path collection's features, in
	// world pixels: a trail is a line and a weight, and declaring the weight
	// lets a reader draw the path as one continuous stroke instead of an area
	// faked around it.
	KeyStrokeWidthPx = "atlas.stroke.width_px"

	// KeyCollectionKey is a collection's merge identity: the slug collections
	// from different sources meet under when they mean the same concept.
	// Absent, the icon key stands in.
	KeyCollectionKey = "atlas.collection.key"
)

// KeyNoteText never appears in a payload. It is the name a merge policy table
// and its ledger use for a feature's description, so "which description wins"
// is decided and recorded in the same vocabulary as everything else. It is
// deliberately unregistered: [Validate] refuses it on any entity.
const KeyNoteText = "atlas.note.text"

// The vocabularies the registered keys admit.
const (
	GeometryPoint = "point"
	GeometryPath  = "path"
	GeometryArea  = "area"

	LabelAlways = "always"
	LabelQuiet  = "quiet"

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

// definition is one registered key: where it attaches, how settled it is, and
// what values it admits.
type definition struct {
	entity    Entity
	stability Stability
	check     func(value string) error
}

// registry is the whole vocabulary. docs/semconv/REGISTRY.md is its prose
// twin, and TestRegistryAgreesWithItsDocument holds the two to the same list.
var registry = map[string]definition{
	KeyGeometryKind:        {EntityCollection, Stable, enum(GeometryPoint, GeometryPath, GeometryArea)},
	KeyLabelPolicy:         {EntityCollection, Experimental, enum(LabelAlways, LabelQuiet)},
	KeyRenderAs:            {EntityCollection, Stable, enum(RenderAsPin, RenderAsText)},
	KeyIconStd:             {EntityCollection, Stable, setName},
	KeyIconKind:            {EntityCollection, Stable, enum(IconKindGlyph, IconKindPicture)},
	KeyIconOutset:          {EntityWorld, Stable, enum(OutsetLight, OutsetDark)},
	KeyGeometrySurface:     {EntityWorld, Stable, enum(SurfacePlane, SurfaceSphere)},
	KeyGeometryProjection:  {EntityWorld, Stable, enum(ProjectionEquirect)},
	KeyGeometryEquirectPx:  {EntityWorld, Stable, numbers(4)},
	KeyGeometryEquirectDeg: {EntityWorld, Stable, numbers(4)},
	KeyGeometryMercatorPx:  {EntityWorld, Experimental, numbers(4)},
	KeyGeometryMercatorDeg: {EntityWorld, Experimental, numbers(4)},
	KeyGeometryBody:        {EntityWorld, Experimental, slug},
	KeyGeometryRadiusKM:    {EntityWorld, Experimental, decimal},
	KeyGeoLat:              {EntityFeature, Experimental, decimal},
	KeyGeoLon:              {EntityFeature, Experimental, decimal},
	KeyHydroHUC12:          {EntityFeature, Experimental, huc12},
	KeyStrokeWidthPx:       {EntityCollection, Experimental, positiveDecimal},
	KeyCollectionKey:       {EntityCollection, Experimental, slug},
}

// Keys lists every registered key in a stable order, for the tools that
// report on adoption and for the tests that hold the registry to its prose.
func Keys() []string {
	out := make([]string, 0, len(registry))
	for key := range registry {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// EntityOf reports where a key attaches. The false result is a reader's cue
// to ignore the attribute: an unregistered key is data from a vocabulary this
// build does not speak, never a reason to refuse a bundle.
func EntityOf(key string) (Entity, bool) {
	definition, known := registry[key]
	return definition.entity, known
}

// StabilityOf reports how settled a key is.
func StabilityOf(key string) (Stability, bool) {
	definition, known := registry[key]
	return definition.stability, known
}

// Check holds one attribute to the registry: registered, attached to this
// entity, and carrying a value its vocabulary admits. It is the single-key
// face of [Validate], for a producer building an attribute set one key at a
// time.
func Check(entity Entity, key, value string) error {
	definition, known := registry[key]
	if !known {
		return fmt.Errorf("attribute %q is not registered", key)
	}
	if definition.entity != entity {
		return fmt.Errorf("attribute %q attaches to a %s, not a %s", key, definition.entity, entity)
	}
	if err := definition.check(value); err != nil {
		return fmt.Errorf("attribute %q: %w", key, err)
	}
	return nil
}

// Validate is the producer-strict gate: every key of attrs in the atlas
// namespace must pass [Check] against entity. Keys outside the namespace are
// not this registry's business and pass through.
//
// Attributes are visited in sorted order, so a set with several faults always
// reports the same one.
func Validate(entity Entity, attrs map[string]string) error {
	for _, key := range sortedKeys(attrs) {
		if !strings.HasPrefix(key, Namespace) {
			continue
		}
		if err := Check(entity, key, attrs[key]); err != nil {
			return err
		}
	}
	return nil
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

func positiveDecimal(value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%q is not a positive number", value)
	}
	return nil
}

func huc12(value string) error {
	if len(value) != 12 {
		return fmt.Errorf("%q is not twelve digits", value)
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return fmt.Errorf("%q is not twelve digits", value)
		}
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

func sortedKeys(attrs map[string]string) []string {
	out := make([]string, 0, len(attrs))
	for key := range attrs {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
