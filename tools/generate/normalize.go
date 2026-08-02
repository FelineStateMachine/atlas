package main

import (
	"fmt"
	"hash/fnv"

	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// A map's contents used to mirror the MapGenie shape all the way through the
// generator: pins under groups and categories, regions standing apart as
// zones, and every pass over a map written twice because of it. The catalog
// holds one model instead -- collections of features, each feature a
// geometry with attributes -- and since format v3 the wire says the same
// thing the model does, so what this file normalizes is what a payload
// carries.

// A collection's kind is the dimensionality of its features.
const (
	kindPoint = "point"
	kindPath  = "path"
	kindArea  = "area"
)

// regionsCollectionKey names the implicit collection every world's regions
// fold into. MapGenie never declared its regions as a collection, so the
// generator declares one for them.
const regionsCollectionKey = "regions"

// worldCollection is one legend entry's worth of features: a category of
// pins, or a curated set of shapes, or the world's undeclared regions
// gathered under the implicit key.
type worldCollection struct {
	ID int64
	// Key is the collection's curation and merge identity; legacy categories
	// carry none and are still met by their icon key instead.
	Key   string
	Title string
	// Group is the legend section this collection files under. Shape
	// collections usually carry none and gather in the viewer's own section.
	Group string
	// Kind is "point", "path" or "area".
	Kind             string
	Icon, IconAsset  string
	IconPicture      bool
	Color, IconColor string
	// DisplayType is MapGenie's legacy render field, never emitted: it exists
	// only so speakConventions can derive atlas.render.as for captures from
	// before the conventions.
	DisplayType string
	Visible     bool
	Attrs       map[string]string
	Features    []feature
}

// feature is one thing on the map, whatever its dimensionality: a point
// carries Lat and Lng, a shape carries Geometry, and everything else --
// prose, attributes, shard, nesting -- reads the same for both.
type feature struct {
	ID          int64
	Title       string
	Subtitle    string
	Description string
	Lat, Lng    float64
	Geometry    []geometry
	// Member is the area feature containing this point -- the region a
	// MapGenie pin names -- where Parent nests one shape inside another.
	Member *int64
	Parent *int64
	Center *coordinate
	Shard  int64
	// HasText is set at packing time, when the Description defers into the
	// text payload and this marker is what the wire keeps of it.
	HasText bool
	Links   []catalogLink
	Attrs   map[string]string
}

// normalizeWorld folds a decoded capture into the uniform model: each
// category becomes a point collection carrying its resolved colours, and
// every region becomes a shape feature of the collection it claims -- a
// declared one when the region names it, the implicit region collection when
// it names nothing. Declared collections follow the point collections in
// declaration order, the implicit collection last, and a region naming a
// collection nobody declared fails the build rather than guessing. A region
// whose geometry all came through empty is dropped here, exactly as it always
// was, because a shape with nothing to draw is not a shape.
func normalizeWorld(raw rawMap) ([]worldCollection, error) {
	var collections []worldCollection
	for _, rawGroup := range raw.Groups {
		for _, rawCategory := range rawGroup.Categories {
			collection := worldCollection{
				ID:          rawCategory.ID,
				Title:       rawCategory.Title,
				Group:       rawGroup.Title,
				Kind:        kindPoint,
				Icon:        rawCategory.Icon,
				Color:       resolvedCategoryColor(rawGroup, rawCategory),
				IconColor:   resolvedIconColor(rawGroup, rawCategory),
				DisplayType: rawCategory.DisplayType,
				Visible:     rawCategory.Visible,
				Attrs:       rawCategory.Attrs,
			}
			for _, rawLocation := range rawCategory.Locations {
				lat, err := number(rawLocation.Latitude)
				if err != nil {
					return nil, fmt.Errorf("location %d latitude: %w", rawLocation.ID, err)
				}
				lng, err := number(rawLocation.Longitude)
				if err != nil {
					return nil, fmt.Errorf("location %d longitude: %w", rawLocation.ID, err)
				}
				collection.Features = append(collection.Features, feature{
					ID:          rawLocation.ID,
					Title:       rawLocation.Title,
					Description: rawLocation.Description,
					Lat:         lat,
					Lng:         lng,
					Member:      rawLocation.RegionID,
					Attrs:       rawLocation.Attrs,
				})
			}
			collections = append(collections, collection)
		}
	}
	declared := make([]worldCollection, 0, len(raw.Collections))
	declaredAt := make(map[string]int, len(raw.Collections))
	for _, decl := range raw.Collections {
		if decl.Key == "" || decl.Key == regionsCollectionKey {
			return nil, fmt.Errorf("collection %q declares key %q, which the implicit collection reserves",
				decl.Title, decl.Key)
		}
		if _, doubled := declaredAt[decl.Key]; doubled {
			return nil, fmt.Errorf("collection %q is declared twice", decl.Key)
		}
		kind := decl.Attrs[semconv.KeyGeometryKind]
		if kind != kindPath && kind != kindArea {
			return nil, fmt.Errorf("collection %q declares geometry kind %q; a declared collection must say %q or %q",
				decl.Key, kind, kindPath, kindArea)
		}
		if _, stroked := decl.Attrs[semconv.KeyStrokeWidthPx]; kind == kindPath && !stroked {
			return nil, fmt.Errorf("path collection %q declares no %s; a path is a line and a width",
				decl.Key, semconv.KeyStrokeWidthPx)
		}
		declaredAt[decl.Key] = len(declared)
		declared = append(declared, worldCollection{
			Key:     decl.Key,
			Title:   decl.Title,
			Kind:    kind,
			Visible: true,
			Attrs:   decl.Attrs,
		})
	}
	implicit := worldCollection{
		Key:     regionsCollectionKey,
		Title:   "Regions",
		Kind:    kindArea,
		Visible: true,
	}
	for _, rawRegion := range raw.Regions {
		shape := feature{
			ID:          rawRegion.ID,
			Title:       rawRegion.Title,
			Subtitle:    rawRegion.Subtitle,
			Description: rawRegion.Description,
			Parent:      rawRegion.ParentRegionID,
			Attrs:       rawRegion.Attrs,
		}
		centerX, hasX, err := optionalNumber(rawRegion.CenterX)
		if err != nil {
			return nil, fmt.Errorf("region %d center_x: %w", rawRegion.ID, err)
		}
		centerY, hasY, err := optionalNumber(rawRegion.CenterY)
		if err != nil {
			return nil, fmt.Errorf("region %d center_y: %w", rawRegion.ID, err)
		}
		if hasX && hasY {
			shape.Center = &coordinate{Latitude: centerY, Longitude: centerX}
		}
		home := &implicit
		if rawRegion.Collection != "" {
			at, known := declaredAt[rawRegion.Collection]
			if !known {
				return nil, fmt.Errorf("region %q claims collection %q, which the map never declares",
					rawRegion.Title, rawRegion.Collection)
			}
			home = &declared[at]
		}
		for _, part := range rawRegion.Features {
			if part.Geometry.Type == "" || len(part.Geometry.Coordinates) == 0 {
				continue
			}
			// The geometry sniff is dead: a line belongs to a declared path
			// collection, which says so up front, and never to the implicit
			// collection, whose regions are ground.
			if home == &implicit && part.Geometry.Type == "MultiLineString" {
				return nil, fmt.Errorf(
					"region %q draws lines in the implicit region collection; lines must be declared as a path collection",
					rawRegion.Title)
			}
			shape.Geometry = append(shape.Geometry, part.Geometry)
		}
		if len(shape.Geometry) > 0 {
			home.Features = append(home.Features, shape)
		}
	}
	// Shape collections need numeric identities of their own on the wire --
	// the viewer's hide and unfold sets are keyed by them -- and a source
	// declares keys, not numbers, so the numbers are derived from the same
	// stable names the way every other id in the pipeline is.
	used := make(map[int64]string, len(collections))
	for _, collection := range collections {
		used[collection.ID] = collection.Title
	}
	claim := func(key string) (int64, error) {
		hash := fnv.New32a()
		fmt.Fprintf(hash, "%d:collection:%s", raw.ID, key)
		id := int64(hash.Sum32() & 0x7fffffff)
		if id == 0 {
			id = 1
		}
		if holder, taken := used[id]; taken {
			return 0, fmt.Errorf("collection %q collides with %q on id %d", key, holder, id)
		}
		used[id] = key
		return id, nil
	}
	for _, collection := range declared {
		if len(collection.Features) == 0 {
			continue
		}
		id, err := claim(collection.Key)
		if err != nil {
			return nil, err
		}
		collection.ID = id
		collections = append(collections, collection)
	}
	if len(implicit.Features) > 0 {
		id, err := claim(implicit.Key)
		if err != nil {
			return nil, err
		}
		implicit.ID = id
		collections = append(collections, implicit)
	}
	return collections, nil
}

// featureTally counts the map's features by kind: the origin account's view
// of what the map holds, the manifest's per-kind counts, and the yardstick
// every merge audit measures the composed map against.
func (m catalogWorld) featureTally() featureCounts {
	var counts featureCounts
	for _, collection := range m.Collections {
		switch collection.Kind {
		case kindPoint:
			counts.Point += len(collection.Features)
		case kindPath:
			counts.Path += len(collection.Features)
		default:
			counts.Area += len(collection.Features)
		}
	}
	return counts
}
