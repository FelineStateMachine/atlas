package authoring

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
)

type assembledFeature struct {
	feature vnext.Feature
	values  map[string]vnext.Property
}

func assemble(project Project, observations []observation) (vnext.Volume, error) {
	space := project.Target.CoordinateSpace
	world := vnext.World{
		ID: project.Target.World, Title: project.Target.Title,
		CoordinateSpace: vnext.CoordinateSpace{
			ID: space.ID, Kind: space.Kind, Unit: space.Unit, Definition: space.Definition,
			Extent: space.Extent, SourceZoom: space.SourceZoom, FirstTile: space.FirstTile,
			TileSize: space.TileSize, Size: space.Size,
		},
		Presentation: vnext.Presentation{ID: project.Presentation.ID, Title: project.Presentation.Title},
	}
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].FeatureSet != observations[j].FeatureSet {
			return observations[i].FeatureSet < observations[j].FeatureSet
		}
		if observations[i].NativeID != observations[j].NativeID {
			return observations[i].NativeID < observations[j].NativeID
		}
		return observations[i].SourceOrder < observations[j].SourceOrder
	})
	sets := make(map[string]int)
	families := make(map[string]string)
	features := make(map[string]map[string]*assembledFeature)
	for _, item := range observations {
		setIndex, exists := sets[item.FeatureSet]
		if !exists {
			world.FeatureSets = append(world.FeatureSets, vnext.FeatureSet{
				ID: setID(world.ID, item.FeatureSet), Title: item.SetTitle, SemanticType: item.Semantic,
				Claims: []vnext.Property{geometryClaim(item.Family)},
			})
			setIndex = len(world.FeatureSets) - 1
			sets[item.FeatureSet] = setIndex
			families[item.FeatureSet] = item.Family
			features[item.FeatureSet] = make(map[string]*assembledFeature)
		}
		set := &world.FeatureSets[setIndex]
		if set.Title != item.SetTitle || set.SemanticType != item.Semantic || families[item.FeatureSet] != item.Family || familyOf(item.Geometry.Kind) != item.Family {
			return vnext.Volume{}, fmt.Errorf("feature set %s has incompatible observations", item.FeatureSet)
		}
		for key, field := range item.Fields {
			if current, held := fieldByID(set.Properties, field.ID); held && (current.Kind != field.Kind || current.Name != field.Name || current.Optional != field.Optional) {
				return vnext.Volume{}, fmt.Errorf("feature set %s field %s changes type", item.FeatureSet, key)
			} else if !held {
				set.Properties = append(set.Properties, field)
			}
		}
		held := features[item.FeatureSet][item.NativeID]
		if held == nil {
			held = &assembledFeature{
				feature: vnext.Feature{
					ID: featureID(world.ID, item.FeatureSet, item.NativeID), Title: item.Title,
					Geometry: item.Geometry,
				},
				values: make(map[string]vnext.Property),
			}
			features[item.FeatureSet][item.NativeID] = held
		}
		held.feature.Provenance = appendProvenance(held.feature.Provenance, vnext.Provenance{
			Source: item.Source, NativeID: item.NativeID, CapturedAt: item.Evidence.CapturedAt,
		})
		for key, value := range item.Values {
			if _, exists := held.values[key]; !exists {
				field := item.Fields[key]
				held.values[key] = vnext.Property{FieldID: field.ID, Field: field, Value: value}
			}
		}
		for _, relation := range item.Relations {
			held.feature.Relationships = appendRelationship(held.feature.Relationships, vnext.Relationship{
				Predicate: relation.Predicate,
				Target:    featureID(world.ID, relation.FeatureSet, relation.NativeID),
			})
		}
	}
	for _, setName := range sortedSetNames(sets) {
		set := &world.FeatureSets[sets[setName]]
		sort.Slice(set.Properties, func(i, j int) bool { return set.Properties[i].ID.String() < set.Properties[j].ID.String() })
		for _, nativeID := range sortedFeatureNames(features[setName]) {
			held := features[setName][nativeID]
			for _, key := range sortedPropertyNames(held.values) {
				held.feature.Properties = append(held.feature.Properties, held.values[key])
			}
			sort.Slice(held.feature.Provenance, func(i, j int) bool {
				if held.feature.Provenance[i].Source != held.feature.Provenance[j].Source {
					return held.feature.Provenance[i].Source < held.feature.Provenance[j].Source
				}
				return held.feature.Provenance[i].CapturedAt < held.feature.Provenance[j].CapturedAt
			})
			sort.Slice(held.feature.Relationships, func(i, j int) bool {
				if held.feature.Relationships[i].Predicate != held.feature.Relationships[j].Predicate {
					return held.feature.Relationships[i].Predicate < held.feature.Relationships[j].Predicate
				}
				return held.feature.Relationships[i].Target < held.feature.Relationships[j].Target
			})
			set.Features = append(set.Features, held.feature)
		}
	}
	sort.Slice(world.FeatureSets, func(i, j int) bool { return world.FeatureSets[i].ID < world.FeatureSets[j].ID })
	for _, style := range project.Presentation.Styles {
		world.Presentation.Styles = append(world.Presentation.Styles, vnext.Style{
			ID: style.ID, Symbol: style.Symbol, Icon: style.Icon, RenderAs: style.RenderAs,
			IconAsset: style.IconAsset, IconPicture: style.IconPicture, Stroke: style.Stroke, Fill: style.Fill,
		})
	}
	for _, layer := range project.Presentation.Layers {
		maxZoom := layer.MaxZoom
		if maxZoom == 0 {
			maxZoom = 32
		}
		world.Presentation.Layers = append(world.Presentation.Layers, vnext.Layer{
			ID: layer.ID, FeatureSet: setID(world.ID, layer.FeatureSet), Style: layer.Style,
			Group: layer.Group, LabelPolicy: layer.LabelPolicy, Visible: layer.Visible,
			Order: layer.Order, MinZoom: layer.MinZoom, MaxZoom: maxZoom,
		})
		world.Presentation.Legend = append(world.Presentation.Legend, vnext.LegendEntry{Layer: layer.ID, Label: layer.Label, Order: layer.Order})
	}
	sort.Slice(world.Presentation.Styles, func(i, j int) bool { return world.Presentation.Styles[i].ID < world.Presentation.Styles[j].ID })
	sort.Slice(world.Presentation.Layers, func(i, j int) bool {
		if world.Presentation.Layers[i].Order != world.Presentation.Layers[j].Order {
			return world.Presentation.Layers[i].Order < world.Presentation.Layers[j].Order
		}
		return world.Presentation.Layers[i].ID < world.Presentation.Layers[j].ID
	})
	sort.Slice(world.Presentation.Legend, func(i, j int) bool {
		if world.Presentation.Legend[i].Order != world.Presentation.Legend[j].Order {
			return world.Presentation.Legend[i].Order < world.Presentation.Legend[j].Order
		}
		return world.Presentation.Legend[i].Layer < world.Presentation.Legend[j].Layer
	})
	if err := relationshipClosure(world); err != nil {
		return vnext.Volume{}, err
	}
	return vnext.Volume{ID: project.ID, Title: project.Title, Worlds: []vnext.World{world}}, nil
}

func geometryClaim(family string) vnext.Property {
	field := vnext.Field{
		ID:   vnext.IDFromName("dev.atlas.attribute.featureSet", semconv.KeyGeometryKind),
		Name: semconv.KeyGeometryKind, Kind: vnext.KindString,
	}
	return vnext.Property{FieldID: field.ID, Field: field, Value: vnext.StringValue(family)}
}

func appendProvenance(values []vnext.Provenance, item vnext.Provenance) []vnext.Provenance {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

func appendRelationship(values []vnext.Relationship, item vnext.Relationship) []vnext.Relationship {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

func fieldByID(fields []vnext.Field, id vnext.ID) (vnext.Field, bool) {
	for _, field := range fields {
		if field.ID == id {
			return field, true
		}
	}
	return vnext.Field{}, false
}

func relationshipClosure(world vnext.World) error {
	ids := make(map[string]bool)
	for _, set := range world.FeatureSets {
		for _, feature := range set.Features {
			ids[feature.ID] = true
		}
	}
	for _, set := range world.FeatureSets {
		for _, feature := range set.Features {
			for _, relation := range feature.Relationships {
				if !ids[relation.Target] {
					return fmt.Errorf("feature %s relationship %s targets missing %s", feature.ID, relation.Predicate, relation.Target)
				}
			}
		}
	}
	return nil
}

func setID(world, set string) string { return world + "/set/" + set }

func featureID(world, set, native string) string {
	digest := sha256.Sum256([]byte(native))
	return setID(world, set) + "/feature/" + hex.EncodeToString(digest[:12])
}

func sortedSetNames(values map[string]int) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedFeatureNames(values map[string]*assembledFeature) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedPropertyNames(values map[string]vnext.Property) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
