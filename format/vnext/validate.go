package vnext

import (
	"fmt"
	"math"
	"strings"
)

type identities map[string]string

func (ids identities) add(id, kind string) error {
	if id == "" {
		return fmt.Errorf("%s has no ID", kind)
	}
	key := kind + "\x00" + id
	if previous, held := ids[key]; held {
		return fmt.Errorf("ID %q is shared by %s and %s", id, previous, kind)
	}
	ids[key] = kind
	return nil
}

func validateVolume(volume Volume) error {
	if volume.ID == "" || volume.Title == "" || len(volume.Worlds) == 0 {
		return fmt.Errorf("volume requires an ID, title, and at least one world")
	}
	ids := make(identities)
	if err := ids.add(volume.ID, "volume"); err != nil {
		return err
	}
	for _, world := range volume.Worlds {
		if err := validateWorld(world, ids); err != nil {
			return err
		}
	}
	for _, asset := range volume.Assets {
		if err := ids.add(asset.ID, "asset"); err != nil {
			return err
		}
		if asset.MediaType == "" {
			return fmt.Errorf("asset %s has no media type", asset.ID)
		}
	}
	return nil
}

func validateWorld(world World, ids identities) error {
	if err := ids.add(world.ID, "world"); err != nil {
		return err
	}
	if world.Title == "" {
		return fmt.Errorf("world %s has no title", world.ID)
	}
	if err := ids.add(world.CoordinateSpace.ID, "coordinate space"); err != nil {
		return err
	}
	if world.CoordinateSpace.Kind == "" || world.CoordinateSpace.Unit == "" || world.CoordinateSpace.Definition == "" {
		return fmt.Errorf("world %s has an incomplete coordinate space", world.ID)
	}
	if world.CoordinateSpace.SourceZoom < 0 || world.CoordinateSpace.TileSize < 0 || world.CoordinateSpace.Size < 0 {
		return fmt.Errorf("world %s has invalid coordinate grid metadata", world.ID)
	}
	if err := validateClaims(world.Claims, "world"); err != nil {
		return err
	}
	sets := make(map[string]bool, len(world.FeatureSets))
	for _, set := range world.FeatureSets {
		if err := validateFeatureSet(set, ids); err != nil {
			return err
		}
		sets[set.ID] = true
	}
	for _, raster := range world.RasterPyramids {
		if err := ids.add(raster.ID, "raster pyramid"); err != nil {
			return err
		}
		if raster.Name == "" || raster.Codec == "" || raster.Template == "" || raster.TileSize <= 0 || raster.MinZoom < 0 || raster.MaxZoom < raster.MinZoom || raster.FullZoom < raster.MinZoom || raster.SourceZoom < 0 {
			return fmt.Errorf("raster pyramid %s is incomplete", raster.ID)
		}
		if len(raster.Formats) != int(raster.MaxZoom-raster.MinZoom+1) {
			return fmt.Errorf("raster pyramid %s needs one format per zoom", raster.ID)
		}
		if err := offlineString(raster.Template, "raster template"); err != nil {
			return err
		}
	}
	return validatePresentation(world.Presentation, sets, ids)
}

func validateFeatureSet(set FeatureSet, ids identities) error {
	if err := ids.add(set.ID, "feature set"); err != nil {
		return err
	}
	if set.SemanticType == "" {
		return fmt.Errorf("feature set %s has no semantic type", set.ID)
	}
	if err := validateClaims(set.Claims, "feature set"); err != nil {
		return err
	}
	fields := make(map[ID]bool, len(set.Properties))
	for _, field := range set.Properties {
		if field.ID.isZero() || field.Name == "" || !field.Kind.valid() || fields[field.ID] {
			return fmt.Errorf("feature set %s has an invalid property contract", set.ID)
		}
		fields[field.ID] = true
	}
	for _, feature := range set.Features {
		if err := ids.add(feature.ID, "feature"); err != nil {
			return err
		}
		if feature.Title == "" {
			return fmt.Errorf("feature %s has no title", feature.ID)
		}
		if _, err := encodeGeometry(feature.Geometry); err != nil {
			return fmt.Errorf("feature %s: %w", feature.ID, err)
		}
		seen := make(map[ID]bool, len(feature.Properties))
		for _, property := range feature.Properties {
			propertyID := property.fieldID()
			field, held := fieldByID(set.Properties, propertyID)
			if !held || seen[propertyID] || property.Value.Kind != field.Kind {
				return fmt.Errorf("feature %s violates its property contract", feature.ID)
			}
			seen[propertyID] = true
			if property.Value.Kind == KindString {
				if err := offlineString(property.Value.String, "feature property"); err != nil {
					return err
				}
			}
		}
		for _, field := range set.Properties {
			if !field.Optional && !seen[field.ID] {
				return fmt.Errorf("feature %s omits required property %s", feature.ID, field.ID)
			}
		}
		for _, relationship := range feature.Relationships {
			if err := offlineString(relationship.Target, "relationship target"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateClaims(claims []Property, owner string) error {
	seen := make(map[ID]bool, len(claims))
	for _, claim := range claims {
		id := claim.fieldID()
		if id.isZero() || claim.Field.ID != id || claim.Field.Name == "" || !claim.Field.Kind.valid() || claim.Value.Kind != claim.Field.Kind || seen[id] {
			return fmt.Errorf("%s has an invalid typed claim", owner)
		}
		seen[id] = true
		if claim.Value.Kind == KindString {
			if err := offlineString(claim.Value.String, owner+" claim"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGeometry(geometry Geometry) error {
	if geometry.Kind < GeometryPoint || geometry.Kind > GeometryPolygon || len(geometry.Parts) == 0 {
		return fmt.Errorf("geometry has an invalid kind or no parts")
	}
	if geometry.Kind == GeometryPoint && len(geometry.Parts) != 1 {
		return fmt.Errorf("point geometry must contain exactly one part")
	}
	for _, part := range geometry.Parts {
		if len(part.Rings) == 0 || (geometry.Kind != GeometryPolygon && len(part.Rings) != 1) {
			return fmt.Errorf("geometry part has an invalid ring count")
		}
		for _, ring := range part.Rings {
			minimum := 1
			if geometry.Kind == GeometryLineString {
				minimum = 2
			} else if geometry.Kind == GeometryPolygon {
				minimum = 4
			}
			if len(ring) < minimum {
				return fmt.Errorf("geometry ring has too few positions")
			}
			for _, position := range ring {
				if math.IsNaN(position[0]) || math.IsNaN(position[1]) || math.IsInf(position[0], 0) || math.IsInf(position[1], 0) {
					return fmt.Errorf("geometry contains a non-finite position")
				}
			}
		}
	}
	if geometry.Kind == GeometryPoint && len(geometry.Parts[0].Rings[0]) != 1 {
		return fmt.Errorf("point geometry must contain exactly one position")
	}
	return nil
}

func validatePresentation(presentation Presentation, sets map[string]bool, ids identities) error {
	if err := ids.add(presentation.ID, "presentation"); err != nil {
		return err
	}
	if presentation.Title == "" {
		return fmt.Errorf("presentation %s has no title", presentation.ID)
	}
	styles := make(map[string]bool, len(presentation.Styles))
	for _, style := range presentation.Styles {
		if err := ids.add(style.ID, "style"); err != nil {
			return err
		}
		styles[style.ID] = true
	}
	layers := make(map[string]bool, len(presentation.Layers))
	for _, layer := range presentation.Layers {
		if err := ids.add(layer.ID, "layer"); err != nil {
			return err
		}
		if !sets[layer.FeatureSet] || !styles[layer.Style] || layer.MaxZoom < layer.MinZoom {
			return fmt.Errorf("layer %s has an invalid feature set, style, or zoom range", layer.ID)
		}
		layers[layer.ID] = true
	}
	for _, entry := range presentation.Legend {
		if !layers[entry.Layer] || entry.Label == "" {
			return fmt.Errorf("legend has an invalid layer or label")
		}
	}
	return nil
}

func fieldByID(fields []Field, id ID) (Field, bool) {
	for _, field := range fields {
		if field.ID == id {
			return field, true
		}
	}
	return Field{}, false
}

func offlineString(value, context string) error {
	if strings.Contains(value, "http://") || strings.Contains(value, "https://") {
		return fmt.Errorf("%s carries a runtime URL", context)
	}
	return nil
}
