package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProject = `schema: atlas-project/v2
schema-namespace: example.invalid/atlas/sample-region
id: sample-region
title: Sample Region
target:
  world: sample-region
  title: Sample Region
  coordinate-space:
    id: sample-region/space
    kind: projected
    unit: pixel
    definition: atlas:tile-plane
    extent: [0, 0, 256, 256]
    tile-size: 256
    size: 256
sources:
  - id: sample-features
    adapter: geojson
    locator: sample-region.geojson
    license: CC0-1.0
    attribution: Sample publisher
    mapping:
      feature-set: places
      title: Places
      semantic-type: sample.place
      identity: properties.id
      feature-title: properties.name
      geometry:
        family: point
        source-space: sample-region/space
        transform:
          kind: identity
      fields:
        - id: sample.kind
          source: properties.kind
          type: string
presentation:
  id: sample-region/presentation
  title: Sample Region
  styles:
    - id: place-style
      symbol: circle
      fill: '#5588AA'
  layers:
    - id: places-layer
      feature-set: places
      style: place-style
      label: Places
      visible: true
      order: 0
      max-zoom: 32
`

func writeSampleProject(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample-region"+ProjectExtension)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadProject(t *testing.T) {
	path := writeSampleProject(t, sampleProject)
	project, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "sample-region" || project.Target.World != "sample-region" || len(project.Sources) != 1 {
		t.Fatalf("unexpected project: %+v", project)
	}
	want := filepath.Join(filepath.Dir(path), "sample-region.geojson")
	if got := project.ResolveLocator(project.Sources[0].Locator); got != want {
		t.Fatalf("resolved locator %q, want %q", got, want)
	}
}

func TestLoadProjectRefusesUnknownFields(t *testing.T) {
	path := writeSampleProject(t, strings.Replace(sampleProject, "title: Sample Region\n", "title: Sample Region\nunknown: true\n", 1))
	if _, err := LoadProject(path); err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("LoadProject = %v, want unknown-field refusal", err)
	}
}

func TestProjectRequiresManifestExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample-region.yaml")
	if err := os.WriteFile(path, []byte(sampleProject), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(path); err == nil || !strings.Contains(err.Error(), ProjectExtension) {
		t.Fatalf("LoadProject = %v, want extension refusal", err)
	}
}

func TestProjectRejectsConflictingFeatureSets(t *testing.T) {
	project, err := LoadProject(writeSampleProject(t, sampleProject))
	if err != nil {
		t.Fatal(err)
	}
	second := project.Sources[0]
	second.ID = "sample-features-two"
	second.Mapping.Title = "Different title"
	project.Sources = append(project.Sources, second)
	if err := project.Validate(); err == nil || !strings.Contains(err.Error(), "conflicting declarations") {
		t.Fatalf("Validate = %v, want feature-set conflict", err)
	}
}

func TestProjectRejectsGeometryAndRelationshipContractErrors(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Project)
		mention string
	}{
		{
			name: "unknown geometry family",
			change: func(project *Project) {
				project.Sources[0].Mapping.Geometry.Family = "mesh"
			},
			mention: "unknown geometry family",
		},
		{
			name: "identity crosses spaces",
			change: func(project *Project) {
				project.Sources[0].Mapping.Geometry.SourceSpace = "another-space"
			},
			mention: "identity transform source space",
		},
		{
			name: "singular affine",
			change: func(project *Project) {
				project.Sources[0].Mapping.Geometry.SourceSpace = "source-space"
				project.Sources[0].Mapping.Geometry.Transform = CoordinateTransform{Kind: "affine"}
			},
			mention: "singular",
		},
		{
			name: "unknown relationship feature set",
			change: func(project *Project) {
				project.Sources[0].Mapping.Relations = []RelationMap{{Predicate: "within", FeatureSet: "missing", Target: "properties.parent"}}
			},
			mention: "unknown feature set",
		},
		{
			name: "unknown style asset",
			change: func(project *Project) {
				project.Presentation.Styles[0].IconAsset = "missing"
			},
			mention: "unknown asset",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project, err := LoadProject(writeSampleProject(t, sampleProject))
			if err != nil {
				t.Fatal(err)
			}
			test.change(&project)
			if err := project.Validate(); err == nil || !strings.Contains(err.Error(), test.mention) {
				t.Fatalf("Validate = %v, want %q", err, test.mention)
			}
		})
	}
}

func TestProjectV1IsAHardBreak(t *testing.T) {
	legacy := strings.Replace(sampleProject, ProjectSchema, "atlas-project/v1", 1)
	if _, err := LoadProject(writeSampleProject(t, legacy)); err == nil || !strings.Contains(err.Error(), ProjectSchema) {
		t.Fatalf("LoadProject = %v, want v2 hard break", err)
	}
}
