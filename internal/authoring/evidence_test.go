package authoring

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReplaysExactEvidenceFromEarlierAtlas(t *testing.T) {
	projectDir := t.TempDir()
	for _, name := range []string{
		"sample-region.atlas-project",
		"sample-region.geojson",
		"sample-region-routes.geojson",
		"sample-region-areas.geojson",
		"sample-region-raster.ppm",
		"sample-region-symbol.svg",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projectPath := filepath.Join(projectDir, "sample-region.atlas-project")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	libraryDir := filepath.Join(t.TempDir(), "library")
	first, err := Build(context.Background(), BuildOptions{
		ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: libraryDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}

	featurePath := filepath.Join(projectDir, "sample-region.geojson")
	changed, err := os.ReadFile(featurePath)
	if err != nil {
		t.Fatal(err)
	}
	changed = []byte(strings.Replace(string(changed), "Sample Marker", "Updated Marker", 1))
	if err := os.WriteFile(featurePath, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), BuildOptions{
		ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: libraryDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Path == first.Path {
		t.Fatal("changed evidence did not produce a distinct Atlas build")
	}

	replayed, err := Build(context.Background(), BuildOptions{
		ProjectPath: projectPath, CacheDir: cacheDir, LibraryDir: libraryDir,
		ReplayPath: first.Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayedBytes, err := os.ReadFile(replayed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Path != first.Path || !replayed.Present || !bytes.Equal(replayedBytes, firstBytes) {
		t.Fatalf("exact replay differs: first=%s replayed=%s present=%v", first.Path, replayed.Path, replayed.Present)
	}
}

func TestReplayAllowsMappingOnlyProjectChangeButRejectsAcquisitionDrift(t *testing.T) {
	request := PlannedRequest{Request: Request{
		ID: strings.Repeat("a", 64), Kind: RequestFeatures, Source: "sample-places",
	}}
	plan := Plan{Project: "sample-region", ProjectDigest: "new-policy", Requests: []PlannedRequest{request}}
	selection := EvidenceSelection{
		Format: BuildReceiptFormat, Project: "sample-region", ProjectDigest: "old-policy",
		Captures: []SelectedCapture{{
			Request: request.ID, Acquisition: request.ID, Source: request.Source, SHA256: strings.Repeat("b", 64),
			Length: 1, MediaType: "application/geo+json", CapturedAt: "2026-08-08T12:00:00Z",
		}},
	}
	data, _, err := buildReceipt(plan, map[string]Capture{request.ID: {
		RequestHash: request.ID, Source: request.Source, CapturedAt: selection.Captures[0].CapturedAt,
		Body: BlobRef{SHA256: selection.Captures[0].SHA256, Length: 1, MediaType: selection.Captures[0].MediaType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEvidenceSelection(data)
	if err != nil || decoded.ProjectDigest != "new-policy" {
		t.Fatalf("current policy receipt = %+v, %v", decoded, err)
	}
	selection.Captures[0].Request = strings.Repeat("c", 64)
	if _, err := replayCaptures(plan, selection, OpenCache(t.TempDir())); err == nil || !strings.Contains(err.Error(), "does not select") {
		t.Fatalf("replay acquisition drift = %v", err)
	}
}
