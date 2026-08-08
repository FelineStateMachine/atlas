package authoring

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

func TestBuildSampleRegionIsNativeDeterministicAndFused(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "sample-region"+ProjectExtension)
	projectBody := strings.Replace(sampleProject, "sources:\n", `sources:
  - id: sample-features-secondary
    adapter: geojson
    locator: sample-region-secondary.geojson
    license: CC0-1.0
    attribution: Secondary sample publisher
    mapping:
      feature-set: places
      identity: properties.id
      feature-title: properties.name
      geometry:
        source-space: sample-region/space
        transform:
          kind: identity
      properties:
        - field: sample.kind
          source: properties.kind
`, 1)
	if err := os.WriteFile(projectPath, []byte(projectBody), 0o644); err != nil {
		t.Fatal(err)
	}
	primary := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":"one","name":"Sample One","kind":"primary"},"geometry":{"type":"Point","coordinates":[32,48]}}]}`
	secondary := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":"one","name":"Sample One","kind":"secondary"},"geometry":{"type":"Point","coordinates":[33,49]}}]}`
	if err := os.WriteFile(filepath.Join(root, "sample-region.geojson"), []byte(primary), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample-region-secondary.geojson"), []byte(secondary), 0o644); err != nil {
		t.Fatal(err)
	}
	cache, library := filepath.Join(root, "cache"), filepath.Join(root, "library")
	first, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cache, LibraryDir: library})
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), BuildOptions{ProjectPath: projectPath, CacheDir: cache, LibraryDir: library, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Present || first.Path != second.Path || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("rebuild differs: first=%+v second=%+v", first, second)
	}
	file, err := vnext.OpenFile(first.Path, vnext.StandardSchema())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	volume, err := file.Volume()
	if err != nil {
		t.Fatal(err)
	}
	if len(volume.Worlds) != 1 || len(volume.Worlds[0].FeatureSets) != 1 || len(volume.Worlds[0].FeatureSets[0].Features) != 1 {
		t.Fatalf("unexpected semantic volume: %+v", volume)
	}
	feature := volume.Worlds[0].FeatureSets[0].Features[0]
	if len(feature.Provenance) != 2 {
		t.Fatalf("fused feature has provenance %+v", feature.Provenance)
	}
	if got := feature.Properties[0].Value.String; got != "secondary" {
		// The manifest puts the secondary source first; source order is the
		// explicit deterministic conflict policy.
		t.Fatalf("fused property = %q, want secondary", got)
	}
}

func TestPlanOnlyDoesNotCreateCacheOrLibrary(t *testing.T) {
	root := t.TempDir()
	project := writeSampleProject(t, strings.Replace(sampleProject, "sample-region.geojson", filepath.Join(root, "missing.geojson"), 1))
	cache, library := filepath.Join(root, "cache"), filepath.Join(root, "library")
	result, err := Build(context.Background(), BuildOptions{ProjectPath: project, CacheDir: cache, LibraryDir: library, PlanOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plan.Requests) != 1 {
		t.Fatalf("plan has %d requests", len(result.Plan.Requests))
	}
	for _, path := range []string{cache, library} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("plan created %s", path)
		}
	}
}
