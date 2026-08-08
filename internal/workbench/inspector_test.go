package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FelineStateMachine/atlas/internal/authoring"
)

func representativeInspector(t *testing.T) (*Workbench, authoring.BuildResult) {
	t.Helper()
	root := t.TempDir()
	project, err := filepath.Abs(filepath.Join("..", "..", "examples", "sample-region"+authoring.ProjectExtension))
	if err != nil {
		t.Fatal(err)
	}
	targets := Targets{
		Atlas: "/opt/atlas", Dir: root, Project: project,
		Cache: filepath.Join(root, "cache"), Registry: filepath.Join(root, "library"),
	}
	result, err := authoring.Build(context.Background(), authoring.BuildOptions{
		ProjectPath: project, CacheDir: targets.Cache, LibraryDir: targets.Registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	held.supervisor.last = supervisedRun{Name: "build", Artifact: result.Path, FinishedAt: time.Now()}
	return held, result
}

func TestInspectorDescribesTheCompiledRepresentativeArtifact(t *testing.T) {
	held, _ := representativeInspector(t)
	artifact, err := held.openCompletedArtifact()
	if err != nil {
		t.Fatal(err)
	}
	page, err := inspectArtifact(artifact, "", "", 0)
	artifact.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Worlds) != 1 || len(page.Worlds[0].FeatureSets) != 3 || page.Worlds[0].Features != 4 {
		t.Fatalf("compiled world summary = %+v", page.Worlds)
	}
	var places artifactFeatureSet
	for _, set := range page.Worlds[0].FeatureSets {
		if strings.HasSuffix(set.ID, "/set/places") {
			places = set
		}
	}
	if places.Geometry != "point" || places.FeatureCount != 2 || len(places.Properties) != 6 {
		t.Fatalf("compiled places contract = %+v", places)
	}
	wantKinds := map[string]bool{"bool": false, "int64": false, "float64": false, "string": false, "bytes": false, "id": false}
	for _, field := range places.Properties {
		wantKinds[field.Kind] = true
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("compiled places contract omits %s", kind)
		}
	}
	if page.Receipt == nil || len(page.Receipt.Captures) != 5 || page.ReceiptErr != "" {
		t.Fatalf("compiled receipt = %+v, error %q", page.Receipt, page.ReceiptErr)
	}

	server := site(t, held)
	response, body := get(t, server, "/project/inspect")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("inspector answered %d: %s", response.StatusCode, body)
	}
	wants(t, "compiled inspector", body,
		"Compiled artifact", "native semantic truth", "validated", "runtime union",
		"Sample Region", "CoordinateSpace", "Sample Places", "point", "Sample Routes", "path",
		"Sample Areas", "area", "Sample Background", "Presentation", "Executable replay receipt")

	selected := places.Features[0]
	query := url.Values{"set": {places.ID}, "feature": {selected.ID}}
	response, body = get(t, server, "/project/inspect?"+query.Encode())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("selected feature answered %d: %s", response.StatusCode, body)
	}
	wants(t, "compiled feature facts", body, "Feature facts", selected.Title, "typed values", "observed relationships", "within", "provenance records")
}

func TestInspectorPaginatesFeatureSummaries(t *testing.T) {
	targets := projectTargets(t)
	if err := os.MkdirAll(targets.Registry, 0o755); err != nil {
		t.Fatal(err)
	}
	features := make([]featureSpec, 31)
	for index := range features {
		features[index] = featureSpec{id: int64(index + 1), title: fmt.Sprintf("Sample feature %02d", index+1)}
	}
	path := bundleSpec{
		slug: "sample-region", title: "Sample Region",
		worlds: []worldSpec{{slug: "sample-region", title: "Sample Region", features: features}},
	}.write(t, targets.Registry)
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	held.supervisor.last = supervisedRun{Name: "build", Artifact: path, FinishedAt: time.Now()}
	server := site(t, held)
	response, body := get(t, server, "/project/inspect?set=sample-region%2Fset%2Fmarkers")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("first page answered %d: %s", response.StatusCode, body)
	}
	wants(t, "first feature page", body, "Features (0–25 of 31)", "Sample feature 01", "Sample feature 25", "Next")
	if strings.Contains(body, "Sample feature 26") {
		t.Fatal("first feature page rendered beyond its 25-item budget")
	}
	response, body = get(t, server, "/project/inspect?set=sample-region%2Fset%2Fmarkers&offset=25")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second page answered %d: %s", response.StatusCode, body)
	}
	wants(t, "second feature page", body, "Features (25–31 of 31)", "Sample feature 26", "Sample feature 31", "Previous")
}

func TestCompletedArtifactBoundaryRequiresSuccessAndRejectsLinks(t *testing.T) {
	targets := projectTargets(t)
	if err := os.MkdirAll(targets.Registry, 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]supervisedRun{
		"running":    {Name: "build", Running: true, Artifact: filepath.Join(targets.Registry, "running.atlas")},
		"failed":     {Name: "build", Failed: true, FinishedAt: time.Now(), Artifact: filepath.Join(targets.Registry, "failed.atlas")},
		"unfinished": {Name: "build", Artifact: filepath.Join(targets.Registry, "unfinished.atlas")},
		"other run":  {Name: "diff", FinishedAt: time.Now(), Artifact: filepath.Join(targets.Registry, "other.atlas")},
	} {
		t.Run(name, func(t *testing.T) {
			held.supervisor.last = run
			if _, err := held.openCompletedArtifact(); err == nil || !strings.Contains(err.Error(), "no successful completed") {
				t.Fatalf("boundary error = %v", err)
			}
		})
	}

	outside := filepath.Join(targets.Dir, "outside.atlas")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(targets.Registry, "link.atlas")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	held.supervisor.last = supervisedRun{Name: "build", FinishedAt: time.Now(), Artifact: link}
	if _, err := held.openCompletedArtifact(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink error = %v", err)
	}

	directory := filepath.Join(targets.Registry, "directory.atlas")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	held.supervisor.last = supervisedRun{Name: "build", FinishedAt: time.Now(), Artifact: directory}
	if _, err := held.openCompletedArtifact(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}

	held.supervisor.last = supervisedRun{Name: "build", FinishedAt: time.Now(), Artifact: outside}
	if _, err := held.openCompletedArtifact(); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside-library error = %v", err)
	}
}

func TestCompletedArtifactKeepsTheValidatedFileOpen(t *testing.T) {
	held, result := representativeInspector(t)
	want, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := held.openCompletedArtifact()
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	if err := os.Rename(result.Path, result.Path+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.Path, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := artifact.source.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(artifact.source)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("validated handle followed the replaced pathname")
	}
}

func TestInspectorReceiptDecoderIsStrictAndContained(t *testing.T) {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	selection := authoring.EvidenceSelection{
		Format: authoring.BuildReceiptFormat, Project: "sample-region", ProjectDigest: hashA,
		Captures: []authoring.SelectedCapture{{
			Request: hashA, Acquisition: hashB, Source: "sample-source", SHA256: hashA,
			Length: 4, MediaType: "application/geo+json", CapturedAt: "2026-08-08T12:00:00Z",
		}},
	}
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspectEvidenceSelection(data); err != nil {
		t.Fatalf("valid receipt refused: %v", err)
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"unknown field": func(data []byte) []byte { return append(data[:len(data)-1], []byte(`,"surprise":true}`)...) },
		"trailing data": func(data []byte) []byte { return append(data, []byte(` {}`)...) },
		"uppercase hash": func(data []byte) []byte {
			return []byte(strings.Replace(string(data), hashA, strings.ToUpper(hashA), 1))
		},
		"bad time": func(data []byte) []byte {
			return []byte(strings.Replace(string(data), "2026-08-08T12:00:00Z", "sometime", 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := inspectEvidenceSelection(mutate(append([]byte(nil), data...))); err == nil {
				t.Fatal("invalid receipt was accepted")
			}
		})
	}
}
