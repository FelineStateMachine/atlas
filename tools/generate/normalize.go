package main

import (
	"fmt"

	"github.com/FelineStateMachine/atlas/internal/semconv"
)

// A map's contents used to mirror the MapGenie shape all the way through the
// generator: pins under groups and categories, regions standing apart as
// zones, and every pass over a map written twice because of it. The catalog
// now holds one model instead -- collections of features, each feature a
// geometry with attributes -- and the v2 wire keeps its exact bytes by being
// reconstructed from that model at emission time. The wire can only move once
// everything it says provably fits the model, and the byte-identical rebuild
// is that proof.

// A collection's kind is the dimensionality of its features. Nothing
// classifies geometry yet -- paths still travel as pretend areas -- so the
// vocabulary is declared in full but only points and areas are spoken.
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
// pins, or the world's regions gathered under the implicit key.
type worldCollection struct {
	ID int64
	// Key is the collection's curation and merge identity; legacy categories
	// carry none and are still met by their icon key instead.
	Key   string
	Title string
	// Group is the legend section this collection files under -- today's
	// group title -- and GroupID is the number that lets the v2 groups be
	// re-emitted byte-identically. The implicit region collection has neither.
	Group   string
	GroupID int64
	// Kind is "point", "path" or "area". The implicit region collection says
	// "area" nominally; its features keep whatever geometry the source drew.
	Kind             string
	Icon, IconAsset  string
	IconPicture      bool
	Color, IconColor string
	// DisplayType is the legacy render field, kept only because the v2 wire
	// still spells it.
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
	// HasText stays unset until the wire carries features directly: the v2
	// emission derives its marker from Description at packing time.
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
				GroupID:     rawGroup.ID,
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
			Key:   decl.Key,
			Title: decl.Title,
			Kind:  kind,
			Attrs: decl.Attrs,
		})
	}
	implicit := worldCollection{
		Key:   regionsCollectionKey,
		Title: "Regions",
		Kind:  kindArea,
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
	for _, collection := range declared {
		if len(collection.Features) > 0 {
			collections = append(collections, collection)
		}
	}
	if len(implicit.Features) > 0 {
		collections = append(collections, implicit)
	}
	return collections, nil
}

// v2Groups rebuilds the wire's group tree from the collections. Categories
// within a group were contiguous in every capture and every pass keeps them
// so, which is why regrouping by first-seen GroupID reproduces the original
// structure byte for byte.
func (m catalogWorld) v2Groups() []catalogGroup {
	var groups []catalogGroup
	at := make(map[int64]int)
	for _, collection := range m.Collections {
		if collection.Kind != kindPoint {
			continue
		}
		index, seen := at[collection.GroupID]
		if !seen {
			index = len(groups)
			at[collection.GroupID] = index
			groups = append(groups, catalogGroup{ID: collection.GroupID, Title: collection.Group})
		}
		category := catalogCategory{
			ID:          collection.ID,
			Title:       collection.Title,
			Icon:        collection.Icon,
			IconAsset:   collection.IconAsset,
			IconPicture: collection.IconPicture,
			Color:       collection.Color,
			IconColor:   collection.IconColor,
			DisplayType: collection.DisplayType,
			Visible:     collection.Visible,
			Attrs:       collection.Attrs,
		}
		for _, f := range collection.Features {
			category.Locations = append(category.Locations, catalogLocation{
				ID:          f.ID,
				Title:       f.Title,
				Description: f.Description,
				Latitude:    f.Lat,
				Longitude:   f.Lng,
				RegionID:    f.Member,
				Shard:       f.Shard,
				Links:       f.Links,
				Attrs:       f.Attrs,
			})
		}
		groups[index].Categories = append(groups[index].Categories, category)
	}
	return groups
}

// v2Zones rebuilds the wire's zone list from the shape collections, each
// collection's features in the order the capture told them and the
// collections in declaration order. That concatenation is exactly the raw
// region order: the one producer that declares collections emits its regions
// grouped by dataset in the same curated order it declares them, and the
// implicit collection -- last here -- is the whole list everywhere else.
func (m catalogWorld) v2Zones() []zone {
	var zones []zone
	for _, collection := range m.Collections {
		if collection.Kind == kindPoint {
			continue
		}
		for _, f := range collection.Features {
			zones = append(zones, zone{
				ID:             f.ID,
				Title:          f.Title,
				Subtitle:       f.Subtitle,
				Description:    f.Description,
				ParentRegionID: f.Parent,
				Center:         f.Center,
				Shard:          f.Shard,
				Features:       f.Geometry,
				Attrs:          f.Attrs,
			})
		}
	}
	return zones
}

// pinCount is how many point features the map holds, which is what the
// manifest has always reported for it.
func (m catalogWorld) pinCount() int {
	total := 0
	for _, collection := range m.Collections {
		if collection.Kind == kindPoint {
			total += len(collection.Features)
		}
	}
	return total
}
