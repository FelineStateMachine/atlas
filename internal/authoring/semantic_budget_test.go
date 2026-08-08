package authoring

import (
	"math"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestSemanticBudgetsAcceptExactEmittedBoundary(t *testing.T) {
	volume := semanticBudgetVolume(
		vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}}}}}},
		vnext.Geometry{Kind: vnext.GeometryLineString, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{0, 0}, {1, 1}, {2, 2}}}}}},
	)
	budgets := defaultBuildBudgets
	budgets.Features = 2
	budgets.GeometryPositions = 4

	if err := enforceSemanticBudgets(volume, budgets); err != nil {
		t.Fatalf("exact semantic budget boundary: %v", err)
	}
}

func TestSemanticBudgetsRejectWithoutTruncating(t *testing.T) {
	volume := semanticBudgetVolume(
		vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}}}}}},
		vnext.Geometry{Kind: vnext.GeometryLineString, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{0, 0}, {1, 1}, {2, 2}}}}}},
	)
	tests := []struct {
		name    string
		budget  BuildBudgets
		message string
	}{
		{name: "features", budget: BuildBudgets{Features: 1, GeometryPositions: 4}, message: "emits 2 features, budget is 1"},
		{name: "positions", budget: BuildBudgets{Features: 2, GeometryPositions: 3}, message: "emits 4 geometry positions, budget is 3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := enforceSemanticBudgets(volume, test.budget); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("semantic budget error = %v, want %q", err, test.message)
			}
		})
	}
	if got := len(volume.Worlds[0].FeatureSets[0].Features); got != 2 {
		t.Fatalf("refusal truncated volume to %d features", got)
	}
}

func TestSemanticBudgetsCountFusedOutputRatherThanObservations(t *testing.T) {
	project := contractTestProject()
	geometry := vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}}}}}}
	observations := []observation{
		{SourceOrder: 0, Source: "page-one", NativeID: "one", FeatureSet: "places", Title: "One", Geometry: geometry, Values: map[string]vnext.Value{"kind": vnext.StringValue("sample")}},
		{SourceOrder: 0, Source: "page-two", NativeID: "two", FeatureSet: "places", Title: "Two", Geometry: geometry, Values: map[string]vnext.Value{"kind": vnext.StringValue("sample")}},
		{SourceOrder: 1, Source: "secondary", NativeID: "one", FeatureSet: "places", Title: "One", Geometry: geometry, Values: map[string]vnext.Value{"kind": vnext.StringValue("alternate")}},
	}
	volume, err := assemble(project, observations)
	if err != nil {
		t.Fatal(err)
	}
	budgets := defaultBuildBudgets
	budgets.Features = 2
	budgets.GeometryPositions = 2
	if err := enforceSemanticBudgets(volume, budgets); err != nil {
		t.Fatalf("fused output at exact boundary: %v", err)
	}
	if got := len(volume.Worlds[0].FeatureSets[0].Features[0].Provenance); got != 2 {
		t.Fatalf("fused feature provenance = %d, want both sources", got)
	}
}

func TestSemanticBudgetArithmeticRefusesOverflow(t *testing.T) {
	if got, err := checkedSemanticAdd(math.MaxInt64-1, 1); err != nil || got != math.MaxInt64 {
		t.Fatalf("checked boundary = %d, %v", got, err)
	}
	if _, err := checkedSemanticAdd(math.MaxInt64-1, 2); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("checked overflow = %v", err)
	}
}

func semanticBudgetVolume(geometries ...vnext.Geometry) vnext.Volume {
	features := make([]vnext.Feature, 0, len(geometries))
	for index, geometry := range geometries {
		features = append(features, vnext.Feature{ID: string(rune('a' + index)), Geometry: geometry})
	}
	return vnext.Volume{Worlds: []vnext.World{{FeatureSets: []vnext.FeatureSet{{Features: features}}}}}
}
