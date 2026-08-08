package authoring

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestAssembleSeedsDeclaredFeatureSetContracts(t *testing.T) {
	project := contractTestProject()
	project.FeatureSets = append(project.FeatureSets, FeatureSetContract{
		ID: "areas", Title: "Sample Areas", SemanticType: "region", Geometry: "area",
	})
	volume, err := assemble(project, nil)
	if err != nil {
		t.Fatal(err)
	}
	world := volume.Worlds[0]
	if len(world.FeatureSets) != 2 {
		t.Fatalf("feature sets = %d, want two declared empty sets", len(world.FeatureSets))
	}
	places := world.FeatureSets[1]
	if places.ID != "sample-region/set/places" || len(places.Properties) != 1 || len(places.Features) != 0 {
		t.Fatalf("places contract = %+v", places)
	}
	if !hasGeometryClaim(places.Claims, "point") {
		t.Fatalf("places claims = %+v, want point contract", places.Claims)
	}
	wantFieldID := vnext.IDFromName(project.SchemaNamespace, "feature.places.kind")
	if places.Properties[0].ID != wantFieldID {
		t.Fatalf("field ID = %s, want source-independent %s", places.Properties[0].ID, wantFieldID)
	}
}

func TestAssembleChecksRequiredPropertiesAfterFusion(t *testing.T) {
	project := contractTestProject()
	geometry := vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{{1, 1}}}}}}
	first := observation{SourceOrder: 0, Source: "primary", NativeID: "one", FeatureSet: "places", Title: "Sample Place", Geometry: geometry, Values: map[string]vnext.Value{}}
	second := observation{SourceOrder: 1, Source: "secondary", NativeID: "one", FeatureSet: "places", Title: "Sample Place", Geometry: geometry, Values: map[string]vnext.Value{"kind": vnext.StringValue("sample")}}
	if _, err := assemble(project, []observation{first}); err == nil || !strings.Contains(err.Error(), "omits required property kind") {
		t.Fatalf("assemble = %v, want required-property refusal", err)
	}
	volume, err := assemble(project, []observation{first, second})
	if err != nil {
		t.Fatal(err)
	}
	feature := volume.Worlds[0].FeatureSets[0].Features[0]
	if len(feature.Properties) != 1 || feature.Properties[0].Value.String != "sample" || len(feature.Provenance) != 2 {
		t.Fatalf("fused feature = %+v", feature)
	}
}

func contractTestProject() Project {
	return Project{
		Schema: ProjectSchema, SchemaNamespace: "example.invalid/atlas/sample-region", ID: "sample-region", Title: "Sample Region",
		Target: Target{World: "sample-region", Title: "Sample Region", CoordinateSpace: CoordinateSpace{
			ID: "sample-space", Kind: "projected", Unit: "metre", Definition: "sample grid", Extent: [4]float64{0, 0, 10, 10},
		}},
		FeatureSets: []FeatureSetContract{{
			ID: "places", Title: "Sample Places", SemanticType: "place", Geometry: "point",
			Properties: []PropertyContract{{ID: "kind", Name: "Kind", Type: "string"}},
		}},
		Presentation: Presentation{ID: "default", Title: "Default"},
	}
}
