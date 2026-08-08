package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProject = `schema: atlas-project/v1
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
