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
	feature       vnext.Feature
	values        map[string]vnext.Property
	relationships map[string]bool
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
	contracts := make(map[string]FeatureSetContract)
	fields := make(map[string]map[string]vnext.Field)
	features := make(map[string]map[string]*assembledFeature)
	for _, contract := range project.FeatureSets {
		set := vnext.FeatureSet{
			ID: setID(world.ID, contract.ID), Title: contract.Title, SemanticType: contract.SemanticType,
			Claims: []vnext.Property{geometryClaim(contract.Geometry)},
		}
		fields[contract.ID] = make(map[string]vnext.Field, len(contract.Properties))
		for _, property := range contract.Properties {
			kind, err := kindOf(property.Type)
			if err != nil {
				return vnext.Volume{}, fmt.Errorf("feature set %s property %s: %w", contract.ID, property.ID, err)
			}
			field := vnext.Field{
				ID:   vnext.IDFromName(project.SchemaNamespace, "feature."+contract.ID+"."+property.ID),
				Name: property.Name, Kind: kind, Optional: property.Optional,
			}
			set.Properties = append(set.Properties, field)
			fields[contract.ID][property.ID] = field
		}
		sort.Slice(set.Properties, func(i, j int) bool { return set.Properties[i].ID.String() < set.Properties[j].ID.String() })
		world.FeatureSets = append(world.FeatureSets, set)
		sets[contract.ID] = len(world.FeatureSets) - 1
		contracts[contract.ID] = contract
		features[contract.ID] = make(map[string]*assembledFeature)
	}
	for _, item := range observations {
		_, exists := sets[item.FeatureSet]
		if !exists {
			return vnext.Volume{}, fmt.Errorf("observation targets undeclared feature set %s", item.FeatureSet)
		}
		contract := contracts[item.FeatureSet]
		if familyOf(item.Geometry.Kind) != contract.Geometry {
			return vnext.Volume{}, fmt.Errorf("feature set %s has incompatible observations", item.FeatureSet)
		}
		held := features[item.FeatureSet][item.NativeID]
		if held == nil {
			held = &assembledFeature{
				feature: vnext.Feature{
					ID: featureID(world.ID, item.FeatureSet, item.NativeID), Title: item.Title,
					Geometry: item.Geometry,
				},
				values: make(map[string]vnext.Property), relationships: make(map[string]bool),
			}
			features[item.FeatureSet][item.NativeID] = held
		}
		held.feature.Provenance = appendProvenance(held.feature.Provenance, vnext.Provenance{
			Source: item.Source, NativeID: item.NativeID, CapturedAt: item.Evidence.CapturedAt,
		})
		for key, value := range item.Values {
			if _, exists := held.values[key]; !exists {
				field, exists := fields[item.FeatureSet][key]
				if !exists {
					return vnext.Volume{}, fmt.Errorf("feature set %s has no property %s", item.FeatureSet, key)
				}
				held.values[key] = vnext.Property{FieldID: field.ID, Field: field, Value: value}
			}
		}
		for _, relation := range item.Relations {
			held.feature.Relationships = appendRelationship(held.feature.Relationships, vnext.Relationship{
				Predicate: relation.Predicate,
				Target:    featureID(world.ID, relation.FeatureSet, relation.NativeID),
			})
			held.relationships[relation.Contract] = true
		}
	}
	for _, setName := range sortedSetNames(sets) {
		set := &world.FeatureSets[sets[setName]]
		contract := contracts[setName]
		for _, nativeID := range sortedFeatureNames(features[setName]) {
			held := features[setName][nativeID]
			for _, property := range contract.Properties {
				if _, exists := held.values[property.ID]; !property.Optional && !exists {
					return vnext.Volume{}, fmt.Errorf("feature %s omits required property %s", held.feature.ID, property.ID)
				}
			}
			for _, relation := range contract.Relationships {
				if !relation.Optional && !held.relationships[relation.ID] {
					return vnext.Volume{}, fmt.Errorf("feature %s omits required relationship %s", held.feature.ID, relation.ID)
				}
			}
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
