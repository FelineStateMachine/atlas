package workbench

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeSampleProject(t *testing.T, dir string) string {
	t.Helper()
	source := filepath.Join(dir, "sample-region.geojson")
	if err := os.WriteFile(source, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","id":"marker-1","properties":{"id":"marker-1","name":"Sample marker"},"geometry":{"type":"Point","coordinates":[1,1]}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "sample-region.atlas-project")
	manifest := `schema: atlas-project/v2
schema-namespace: example.invalid/atlas/sample-region/v1
id: sample-region
title: Sample Region
target:
  world: sample-region
  title: Sample Region
  coordinate-space:
    id: local
    kind: projected
    unit: metre
    definition: local sample grid
    extent: [0, 0, 100, 100]
sources:
  - id: sample-features
    adapter: geojson
    locator: sample-region.geojson
    media-type: application/geo+json
    license: Example data
    attribution: Sample Region contributors
    mapping:
      feature-set: markers
      title: Markers
      semantic-type: place
      identity: id
      feature-title: properties.name
      geometry:
        family: point
        source-space: local
        transform:
          kind: identity
presentation:
  id: default
  title: Default
  styles:
    - id: marker
      symbol: circle
  layers:
    - id: markers
      feature-set: markers
      style: marker
      label: Markers
      visible: true
      order: 1
release:
  revision: 1
`
	if err := os.WriteFile(project, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func projectTargets(t *testing.T) Targets {
	t.Helper()
	root := t.TempDir()
	return Targets{
		Atlas: "/opt/atlas", Dir: root, Project: writeSampleProject(t, root),
		Cache: filepath.Join(root, "cache"), Registry: filepath.Join(root, "library"),
	}
}

func TestBuildOperationUsesOnlyStartupAuthority(t *testing.T) {
	targets := projectTargets(t)
	op, err := buildOperation(targets, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt/atlas", "build", "--log-json", "-cache", targets.Cache, "-bundles", targets.Registry, targets.Project}
	if !reflect.DeepEqual(op.Argv, want) {
		t.Fatalf("argv %v, want %v", op.Argv, want)
	}
	offline, err := buildOperation(targets, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(offline.Argv, " "), "-offline") {
		t.Fatalf("offline argv = %v", offline.Argv)
	}
	missing := targets
	missing.Project = ""
	if _, err := buildOperation(missing, false); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("missing project refusal = %v", err)
	}
}

func TestProjectPageMakesTheNativeBuildGraphVisible(t *testing.T) {
	targets := projectTargets(t)
	held, err := New(Options{Targets: targets, Runtime: []byte("/* runtime */")})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	response, body := get(t, server, "/project")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("project answered %d: %s", response.StatusCode, body)
	}
	wants(t, "project", body,
		"Sample Region", "One manifest → one native volume", "sample-features", "geojson",
		"0 / 1 requests", "feature sources", "semantic sets", "presentation layers",
		"Build native Atlas", "Portable manifest", "Sample Region contributors")
	if response, _ := get(t, server, "/operations"); response.StatusCode != http.StatusNotFound {
		t.Errorf("retired operations route answered %d", response.StatusCode)
	}
	if response, _ := get(t, server, "/sources"); response.StatusCode != http.StatusNotFound {
		t.Errorf("retired sources route answered %d", response.StatusCode)
	}
}

func TestManifestSaveValidatesBeforeAtomicReplacement(t *testing.T) {
	targets := projectTargets(t)
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	original, err := os.ReadFile(targets.Project)
	if err != nil {
		t.Fatal(err)
	}
	response, _ := postForm(t, server.Client(), server.URL+"/project/save", url.Values{"manifest": {"schema: wrong\n"}})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid save answered %d", response.StatusCode)
	}
	after, _ := os.ReadFile(targets.Project)
	if string(after) != string(original) {
		t.Fatal("invalid candidate replaced the manifest")
	}
	updated := strings.Replace(string(original), "title: Sample Region", "title: Sample Region Updated", 1)
	response, _ = postForm(t, noRedirectClient(server.Client()), server.URL+"/project/save", url.Values{"manifest": {updated}})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid save answered %d", response.StatusCode)
	}
	installed, _ := os.ReadFile(targets.Project)
	if !strings.Contains(string(installed), "Sample Region Updated") {
		t.Fatal("valid candidate was not installed")
	}
}

func TestBuildSurvivesNavigationAndHandsOffTheArtifact(t *testing.T) {
	targets := projectTargets(t)
	if err := os.MkdirAll(targets.Registry, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := bundleSpec{slug: "sample-region", worlds: []worldSpec{{slug: "sample-region"}}}.write(t, targets.Registry)
	binary := filepath.Join(targets.Dir, "fake-atlas")
	script := "#!/bin/sh\nsleep 0.05\nprintf '%s\\n' '{\"time\":\"2026-01-01T00:00:00Z\",\"level\":\"INFO\",\"msg\":\"native build installed\",\"artifact\":\"" + artifact + "\"}' >&2\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	targets.Atlas = binary
	var opened string
	held, err := New(Options{Targets: targets, OpenArtifact: func(path string) error { opened = path; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	response, _ := postForm(t, noRedirectClient(server.Client()), server.URL+"/project/build", url.Values{})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("build answered %d", response.StatusCode)
	}
	if !held.supervisor.Snapshot().Running {
		t.Fatal("build was tied to the completed POST instead of continuing")
	}
	deadline := time.Now().Add(2 * time.Second)
	for held.supervisor.Snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	run := held.supervisor.Snapshot()
	if run.Running || run.Failed || run.Artifact != artifact {
		t.Fatalf("run = %+v", run)
	}
	_, runBody := get(t, server, "/project/run")
	wants(t, "completed run fragment", runBody, "Build complete", "Open in Atlas", "Drag this file onto an Atlas window", filepath.Base(artifact))
	if strings.Contains(runBody, `hx-get="/project/run"`) {
		t.Fatal("completed run fragment kept polling")
	}
	_, body := get(t, server, "/project")
	wants(t, "completed project", body, "Build complete", "Open in Atlas", "Drag this file onto an Atlas window", filepath.Base(artifact))
	response, _ = postForm(t, noRedirectClient(server.Client()), server.URL+"/project/open", url.Values{})
	if response.StatusCode != http.StatusSeeOther || opened != artifact {
		t.Fatalf("open answered %d and handed off %q", response.StatusCode, opened)
	}
	response, _ = get(t, server, "/project/artifact")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("drag artifact answered %d", response.StatusCode)
	}
}

func TestArtifactHandoffRefusesAnInvalidAtlasFile(t *testing.T) {
	targets := projectTargets(t)
	if err := os.MkdirAll(targets.Registry, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(targets.Registry, "sample-region-invalid.atlas")
	if err := os.WriteFile(artifact, []byte("not an Atlas file"), 0o644); err != nil {
		t.Fatal(err)
	}
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	held.supervisor.last = supervisedRun{Name: "build", Artifact: artifact}
	if _, err := held.currentArtifact(); err == nil || !strings.Contains(err.Error(), "open completed Atlas artifact") {
		t.Fatalf("currentArtifact = %v, want native validation failure", err)
	}
}

func TestProjectMutationsRefuseForeignOrigins(t *testing.T) {
	held, err := New(Options{Targets: projectTargets(t)})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/project/build", nil)
	request.Header.Set("Origin", "https://foreign.invalid")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign build answered %d", response.StatusCode)
	}
}

func postForm(t *testing.T, client *http.Client, at string, form url.Values) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, at, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

func noRedirectClient(base *http.Client) *http.Client {
	return &http.Client{Transport: base.Transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
