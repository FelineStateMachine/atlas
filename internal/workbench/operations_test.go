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

	"github.com/FelineStateMachine/atlas/internal/authoring"
	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
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
feature-sets:
  - id: markers
    title: Markers
    semantic-type: place
    geometry: point
sources:
  - id: sample-features
    adapter: geojson
    locator: sample-region.geojson
    media-type: application/geo+json
    license: Example data
    attribution: Sample Region contributors
    mapping:
      feature-set: markers
      identity: id
      feature-title: properties.name
      geometry:
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
		"Build native Atlas", "Portable manifest", "Sample Region contributors",
		"Choose a U.S. area", "USGS Topo", "TIGER roads", "TIGER hydro", "TIGER counties",
		"Typed FeatureSets", "Compiled Atlas truth")
	if response, _ := get(t, server, "/operations"); response.StatusCode != http.StatusNotFound {
		t.Errorf("retired operations route answered %d", response.StatusCode)
	}
	if response, _ := get(t, server, "/sources"); response.StatusCode != http.StatusNotFound {
		t.Errorf("retired sources route answered %d", response.StatusCode)
	}
}

func TestUSAreaAuthoringReplacesTheSingleManifestWithTheSelectedDefaults(t *testing.T) {
	targets := projectTargets(t)
	held, err := New(Options{Targets: targets, Runtime: []byte("/* runtime */")})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	form := usAreaForm()
	form.Set("road-label", "Routes")
	form.Set("road-color", "#c05030")
	form.Set("road-order", "17")
	form.Del("road-visible")
	response, body := postForm(t, noRedirectClient(server.Client()), server.URL+"/project/us-area", form)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("U.S. area authoring answered %d: %s", response.StatusCode, body)
	}
	project, err := authoring.LoadProject(targets.Project)
	if err != nil {
		t.Fatalf("generated manifest: %v", err)
	}
	if project.Title != "Sample Region" || len(project.Rasters) != 1 || len(project.Sources) != 6 {
		t.Fatalf("generated project = %+v", project)
	}
	var roads authoring.Layer
	for _, layer := range project.Presentation.Layers {
		if layer.ID == "roads" {
			roads = layer
		}
	}
	if roads.Label != "Routes" || roads.Visible || roads.Order != 17 {
		t.Fatalf("presentation choices were not authored: %+v", roads)
	}
	if project.Target.CoordinateSpace.OriginX == 0 || project.Target.CoordinateSpace.OriginY == 0 {
		t.Fatalf("generated tile origins = %+v", project.Target.CoordinateSpace)
	}
	_, page := get(t, server, "/project?notice=Official+U.S.+profile+installed")
	wants(t, "generated authoring", page, "Official U.S. profile installed", "Capture requests", "Raster tiles", "Compiled Atlas truth")
}

func TestUSAreaAuthoringRefusesAnInvalidAreaBeforeReplacingTheManifest(t *testing.T) {
	targets := projectTargets(t)
	original, err := os.ReadFile(targets.Project)
	if err != nil {
		t.Fatal(err)
	}
	held, err := New(Options{Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	form := usAreaForm()
	form.Set("west", "2")
	form.Set("south", "86")
	form.Set("east", "3")
	form.Set("north", "89")
	response, body := postForm(t, server.Client(), server.URL+"/project/us-area", form)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Web Mercator world") {
		t.Fatalf("invalid area answered %d: %s", response.StatusCode, body)
	}
	after, err := os.ReadFile(targets.Project)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("invalid area replaced the manifest")
	}
}

func usAreaForm() url.Values {
	return url.Values{
		"west": {"-105.1"}, "south": {"39.6"}, "east": {"-104.9"}, "north": {"39.8"},
		"detail": {"12"}, "topo": {"true"}, "roads": {"true"}, "hydro": {"true"}, "counties": {"true"},
		"road-label": {"Roads"}, "hydro-label": {"Water"}, "county-label": {"Counties"},
		"road-color": {"#b45f3c"}, "hydro-color": {"#4b8db8"}, "hydro-fill": {"#b9d9ea"}, "county-color": {"#766b5b"},
		"road-visible": {"true"}, "hydro-visible": {"true"}, "county-visible": {"true"},
		"road-order": {"40"}, "hydro-order": {"20"}, "county-order": {"10"},
	}
}

func TestProjectPageShowsTheSourceToContractLaneWithoutOpeningEditors(t *testing.T) {
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
	wants(t, "contract graph", body,
		"Sources become semantic FeatureSets", "Source", "sample-region.geojson", "Adapter", "geojson",
		"Mapping", "point · identity", "id → properties.name", "FeatureSet contract", "Markers",
		"0 typed properties", "0 relationships", "Artifact", "native vNext · planned",
		"Build stages", "Edit the single configuration file")
	if strings.Contains(body, `<details class="card manifest-editor" open`) {
		t.Fatal("valid manifest editor was open by default")
	}
}

func TestGraphProjectGroupsSourcesIntoOneTypedContract(t *testing.T) {
	project := authoring.Project{
		Presentation: authoring.Presentation{Styles: []authoring.Style{{ID: "style"}}, Layers: []authoring.Layer{{ID: "layer"}}},
		FeatureSets: []authoring.FeatureSetContract{{
			ID: "places", Title: "Places", SemanticType: "place", Geometry: "point",
			Properties: []authoring.PropertyContract{
				{ID: "name", Name: "Name", Type: "string"},
				{ID: "rank", Name: "Rank", Type: "int64", Optional: true},
			},
			Relationships: []authoring.RelationshipContract{{ID: "area", Predicate: "within", FeatureSet: "areas"}},
		}},
		Sources: []authoring.Source{
			{ID: "primary", Adapter: "geojson", Locator: "primary.geojson", Mapping: authoring.Mapping{
				FeatureSet: "places", Identity: "id", FeatureTitle: "properties.name",
				Geometry:   authoring.GeometryMap{SourceSpace: "local", Transform: authoring.CoordinateTransform{Kind: "identity"}},
				Properties: []authoring.PropertyMap{{Field: "name", Source: "properties.name"}},
				Relations:  []authoring.RelationMap{{Relationship: "area", Target: "properties.area"}},
			}},
			{ID: "secondary", Adapter: "ogc-api-features", Locator: "https://example.invalid/features", Mapping: authoring.Mapping{
				FeatureSet: "places", Identity: "id", FeatureTitle: "properties.name",
				Geometry:   authoring.GeometryMap{SourceSpace: "local", Transform: authoring.CoordinateTransform{Kind: "identity"}},
				Properties: []authoring.PropertyMap{{Field: "rank", Source: "properties.rank"}},
			}},
		},
	}
	plan := authoring.Plan{Requests: []authoring.PlannedRequest{
		{Request: authoring.Request{Kind: authoring.RequestFeatures, Source: "primary"}, Cached: true},
		{Request: authoring.Request{Kind: authoring.RequestFeatures, Source: "secondary"}},
	}}
	graph := graphProject(project, plan)
	if len(graph.FeatureSets) != 1 || len(graph.FeatureSets[0].Sources) != 2 {
		t.Fatalf("feature set graph = %+v", graph.FeatureSets)
	}
	set := graph.FeatureSets[0]
	if len(set.Properties) != 2 || set.Properties[0].ID != "name" || set.Properties[1].ID != "rank" || !set.Properties[1].Optional {
		t.Fatalf("contract properties = %+v", set.Properties)
	}
	if len(set.Relations) != 1 || set.Relations[0].Predicate != "within" || set.Relations[0].Target != "areas" {
		t.Fatalf("contract relations = %+v", set.Relations)
	}
	if !set.Sources[0].Cached || set.Sources[1].Cached {
		t.Fatalf("source cache states = %+v", set.Sources)
	}
}

func TestProjectStagesDeriveProgressFromBuildEvents(t *testing.T) {
	run := supervisedRun{Name: "build", Running: true, Rows: []oprunner.Row{
		{Attrs: []oprunner.Attr{{Key: "stage", Value: "plan"}}},
		{Attrs: []oprunner.Attr{{Key: "stage", Value: "capture"}}},
	}}
	stages := projectStages(run)
	if stages[0].State != "done" || stages[1].State != "active" || stages[2].State != "pending" {
		t.Fatalf("running stages = %+v", stages)
	}
	run.Running, run.Failed = false, true
	run.Rows = append(run.Rows, oprunner.Row{Failed: true, Attrs: []oprunner.Attr{{Key: "stage", Value: "observe"}}})
	stages = projectStages(run)
	if stages[0].State != "done" || stages[1].State != "done" || stages[2].State != "failed" {
		t.Fatalf("failed stages = %+v", stages)
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
	wants(t, "completed run fragment", runBody, "Build complete", "Inspect packed data", "Open in Atlas", "Drag this file onto an Atlas window", filepath.Base(artifact))
	if strings.Contains(runBody, `hx-get="/project/run"`) {
		t.Fatal("completed run fragment kept polling")
	}
	_, body := get(t, server, "/project")
	wants(t, "completed project", body, "Build complete", "Inspect packed data", "Open in Atlas", "Drag this file onto an Atlas window", filepath.Base(artifact))
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
	held.supervisor.last = supervisedRun{Name: "build", Artifact: artifact, FinishedAt: time.Now()}
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
