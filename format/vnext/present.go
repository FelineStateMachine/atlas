package vnext

import (
	"fmt"
	"slices"
)

// PresentedFeature is the scene-facing projection of one semantic feature.
type PresentedFeature struct {
	FeatureID  string
	LayerID    string
	StyleID    string
	Legend     string
	Symbol     string
	Stroke     string
	Fill       string
	Geometry   Geometry
	Properties []Property
}

// Present applies replaceable layer, style, and legend intent to a world's
// semantic feature graph.
func Present(world World) ([]PresentedFeature, error) {
	sets := make(map[string]FeatureSet, len(world.FeatureSets))
	for _, set := range world.FeatureSets {
		sets[set.ID] = set
	}
	styles := make(map[string]Style, len(world.Presentation.Styles))
	for _, style := range world.Presentation.Styles {
		styles[style.ID] = style
	}
	legend := make(map[string]string, len(world.Presentation.Legend))
	for _, entry := range world.Presentation.Legend {
		legend[entry.Layer] = entry.Label
	}
	layers := append([]Layer(nil), world.Presentation.Layers...)
	slices.SortStableFunc(layers, func(left, right Layer) int {
		if left.Order < right.Order {
			return -1
		}
		if left.Order > right.Order {
			return 1
		}
		return 0
	})
	var presented []PresentedFeature
	for _, layer := range layers {
		set, held := sets[layer.FeatureSet]
		if !held {
			return nil, fmt.Errorf("layer %s refers to unknown feature set %s", layer.ID, layer.FeatureSet)
		}
		style, held := styles[layer.Style]
		if !held {
			return nil, fmt.Errorf("layer %s refers to unknown style %s", layer.ID, layer.Style)
		}
		for _, feature := range set.Features {
			presented = append(presented, PresentedFeature{
				FeatureID: feature.ID, LayerID: layer.ID, StyleID: style.ID, Legend: legend[layer.ID],
				Symbol: style.Symbol, Stroke: style.Stroke, Fill: style.Fill,
				Geometry: feature.Geometry, Properties: append([]Property(nil), feature.Properties...),
			})
		}
	}
	return presented, nil
}
