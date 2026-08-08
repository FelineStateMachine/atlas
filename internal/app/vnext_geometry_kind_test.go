package app

import (
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestFeatureSetGeometryKindUsesStableClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		claim        string
		semanticType string
		geometry     vnext.GeometryKind
	}{
		{name: "point", claim: semconv.GeometryPoint, semanticType: "transport.station", geometry: vnext.GeometryPolygon},
		{name: "path", claim: semconv.GeometryPath, semanticType: "place.landmark", geometry: vnext.GeometryPoint},
		{name: "area", claim: semconv.GeometryArea, semanticType: "network.route", geometry: vnext.GeometryLineString},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			set := vnext.FeatureSet{
				SemanticType: test.semanticType,
				Claims: []vnext.Property{{
					Field: vnext.Field{
						Name: semconv.KeyGeometryKind,
						Kind: vnext.KindString,
					},
					Value: vnext.StringValue(test.claim),
				}},
				Features: []vnext.Feature{{Geometry: vnext.Geometry{Kind: test.geometry}}},
			}

			if got := featureSetGeometryKind(set); got != test.claim {
				t.Fatalf("feature set geometry kind = %q, want stable claim %q", got, test.claim)
			}
		})
	}
}

func TestFeatureSetGeometryKindSniffsGeometryWithoutClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		geometry vnext.GeometryKind
		want     string
	}{
		{name: "point", geometry: vnext.GeometryPoint, want: semconv.GeometryPoint},
		{name: "path", geometry: vnext.GeometryLineString, want: semconv.GeometryPath},
		{name: "area", geometry: vnext.GeometryPolygon, want: semconv.GeometryArea},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			set := vnext.FeatureSet{
				SemanticType: "geometry.unrelated",
				Features:     []vnext.Feature{{Geometry: vnext.Geometry{Kind: test.geometry}}},
			}
			if got := featureSetGeometryKind(set); got != test.want {
				t.Fatalf("feature set geometry kind = %q, want sniffed %q", got, test.want)
			}
		})
	}
}
