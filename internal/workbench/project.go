package workbench

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/authoring"
	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
)

const maxManifestBytes = 2 << 20

type projectPage struct {
	Targets      Targets
	Manifest     string
	Project      authoring.Project
	Plan         authoring.Plan
	Error        string
	Notice       string
	Run          supervisedRun
	FeatureSets  int
	SourceCount  int
	RasterCount  int
	LayerCount   int
	CachedCount  int
	RequestCount int
	ArtifactName string
	Graph        projectGraph
	Stages       []projectBuildStage
}

// projectGraph is the manifest and resolved plan shaped for the page. It is a
// view only: adapters still acquire bytes, mappings still assign meaning, and
// the compiler remains the sole authority for the artifact.
type projectGraph struct {
	FeatureSets []projectFeatureSet
	Rasters     []projectSideInput
	Assets      []projectSideInput
	Styles      int
	Layers      int
}

type projectFeatureSet struct {
	ID           string
	Title        string
	SemanticType string
	Geometry     string
	Sources      []projectSourceLane
	Properties   []projectProperty
	Relations    []projectRelation
}

type projectSourceLane struct {
	ID            string
	Locator       string
	Adapter       string
	Cache         string
	Cached        bool
	Identity      string
	FeatureTitle  string
	Geometry      string
	SourceSpace   string
	Transform     string
	PropertyCount int
	RelationCount int
}

type projectProperty struct {
	ID       string
	Name     string
	Type     string
	Optional bool
}

type projectRelation struct {
	Predicate string
	Target    string
	Optional  bool
}

type projectSideInput struct {
	ID      string
	Adapter string
	Cache   string
	Cached  bool
}

type projectBuildStage struct {
	ID    string
	Label string
	State string
}

func (w *Workbench) projectPage(notice string) projectPage {
	page := projectPage{Targets: w.targets, Notice: notice, Run: w.supervisor.Snapshot()}
	page.ArtifactName = filepath.Base(page.Run.Artifact)
	page.Stages = projectStages(page.Run)
	data, err := os.ReadFile(w.targets.Project)
	if err != nil {
		page.Error = err.Error()
		return page
	}
	if len(data) > maxManifestBytes {
		page.Error = "manifest is larger than the editor limit"
		return page
	}
	page.Manifest = string(data)
	project, err := authoring.LoadProject(w.targets.Project)
	if err != nil {
		page.Error = err.Error()
		return page
	}
	page.Project = project
	plan, err := authoring.PlanProject(project, authoring.OpenCache(w.targets.Cache), w.targets.Registry)
	if err != nil {
		page.Error = err.Error()
		return page
	}
	page.Plan = plan
	page.Graph = graphProject(project, plan)
	page.SourceCount = len(project.Sources)
	page.RasterCount = len(project.Rasters)
	page.LayerCount = len(project.Presentation.Layers)
	page.CachedCount = plan.Cached
	page.RequestCount = len(plan.Requests)
	page.FeatureSets = len(project.FeatureSets)
	return page
}

func graphProject(project authoring.Project, plan authoring.Plan) projectGraph {
	graph := projectGraph{Styles: len(project.Presentation.Styles), Layers: len(project.Presentation.Layers)}
	requestCache := make(map[string]bool, len(plan.Requests))
	for _, request := range plan.Requests {
		requestCache[string(request.Kind)+"\x00"+request.Source] = request.Cached
	}
	setIndex := make(map[string]int)
	for _, contract := range project.FeatureSets {
		setIndex[contract.ID] = len(graph.FeatureSets)
		set := projectFeatureSet{
			ID: contract.ID, Title: contract.Title, SemanticType: contract.SemanticType, Geometry: contract.Geometry,
		}
		for _, property := range contract.Properties {
			set.Properties = append(set.Properties, projectProperty{
				ID: property.ID, Name: property.Name, Type: property.Type, Optional: property.Optional,
			})
		}
		for _, relation := range contract.Relationships {
			set.Relations = append(set.Relations, projectRelation{
				Predicate: relation.Predicate, Target: relation.FeatureSet, Optional: relation.Optional,
			})
		}
		sort.Slice(set.Properties, func(i, j int) bool { return set.Properties[i].ID < set.Properties[j].ID })
		sort.Slice(set.Relations, func(i, j int) bool {
			if set.Relations[i].Predicate != set.Relations[j].Predicate {
				return set.Relations[i].Predicate < set.Relations[j].Predicate
			}
			return set.Relations[i].Target < set.Relations[j].Target
		})
		graph.FeatureSets = append(graph.FeatureSets, set)
	}
	for _, source := range project.Sources {
		setID := source.Mapping.FeatureSet
		at, held := setIndex[setID]
		if !held {
			continue
		}
		set := &graph.FeatureSets[at]
		cached := requestCache[string(authoring.RequestFeatures)+"\x00"+source.ID]
		cache := "fetch"
		if cached {
			cache = "ready"
		}
		set.Sources = append(set.Sources, projectSourceLane{
			ID: source.ID, Locator: source.Locator, Adapter: source.Adapter, Cache: cache, Cached: cached,
			Identity: source.Mapping.Identity, FeatureTitle: source.Mapping.FeatureTitle,
			Geometry: set.Geometry, SourceSpace: source.Mapping.Geometry.SourceSpace,
			Transform:     source.Mapping.Geometry.Transform.Kind,
			PropertyCount: len(source.Mapping.Properties), RelationCount: len(source.Mapping.Relations),
		})
	}
	for _, raster := range project.Rasters {
		ready := true
		found := false
		for _, request := range plan.Requests {
			if request.Kind != authoring.RequestRaster || request.Source != raster.ID {
				continue
			}
			found = true
			ready = ready && request.Cached
		}
		graph.Rasters = append(graph.Rasters, projectSideInput{
			ID: raster.ID, Adapter: raster.Adapter, Cache: cacheWord(found && ready), Cached: found && ready,
		})
	}
	for _, asset := range project.Assets {
		ready := requestCache[string(authoring.RequestAsset)+"\x00"+asset.ID]
		graph.Assets = append(graph.Assets, projectSideInput{
			ID: asset.ID, Adapter: "asset-file", Cache: cacheWord(ready), Cached: ready,
		})
	}
	return graph
}

func cacheWord(cached bool) string {
	if cached {
		return "ready"
	}
	return "fetch"
}

func projectStages(run supervisedRun) []projectBuildStage {
	stages := []projectBuildStage{
		{ID: "plan", Label: "Plan", State: "pending"},
		{ID: "capture", Label: "Capture", State: "pending"},
		{ID: "observe", Label: "Observe", State: "pending"},
		{ID: "assemble", Label: "Assemble", State: "pending"},
		{ID: "publish", Label: "Publish", State: "pending"},
	}
	if run.Name == "" {
		return stages
	}
	current := 0
	for _, row := range run.Rows {
		for _, attr := range row.Attrs {
			if attr.Key != "stage" {
				continue
			}
			for index := range stages {
				if stages[index].ID == attr.Value && index > current {
					current = index
				}
			}
		}
	}
	for index := range stages {
		if index < current || !run.Running && !run.Failed && index <= current {
			stages[index].State = "done"
		} else if index == current && run.Running {
			stages[index].State = "active"
		}
	}
	if run.Failed {
		stages[current].State = "failed"
	}
	return stages
}

func (w *Workbench) handleProject(rw http.ResponseWriter, r *http.Request) {
	w.render(rw, "project", w.projectPage(r.URL.Query().Get("notice")))
}

func (w *Workbench) handleProjectSave(rw http.ResponseWriter, r *http.Request) {
	if err := oprunner.CheckOrigin(r); err != nil {
		http.Error(rw, err.Error(), http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(rw, r.Body, maxManifestBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	manifest := r.FormValue("manifest")
	if strings.TrimSpace(manifest) == "" {
		http.Error(rw, "manifest is empty", http.StatusBadRequest)
		return
	}
	if err := validateAndInstallManifest(w.targets.Project, []byte(manifest)); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(rw, r, "/project?notice=Manifest+saved", http.StatusSeeOther)
}

func validateAndInstallManifest(path string, data []byte) error {
	dir := filepath.Dir(path)
	stage, err := os.CreateTemp(dir, ".atlas-project-*.atlas-project")
	if err != nil {
		return fmt.Errorf("stage manifest: %w", err)
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return fmt.Errorf("write staged manifest: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("sync staged manifest: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged manifest: %w", err)
	}
	if _, err := authoring.LoadProject(stagePath); err != nil {
		return err
	}
	if err := os.Chmod(stagePath, 0o644); err != nil {
		return fmt.Errorf("prepare staged manifest: %w", err)
	}
	if err := os.Rename(stagePath, path); err != nil {
		return fmt.Errorf("install manifest: %w", err)
	}
	return nil
}

func (w *Workbench) handleProjectBuild(rw http.ResponseWriter, r *http.Request) {
	if err := oprunner.CheckOrigin(r); err != nil {
		http.Error(rw, err.Error(), http.StatusForbidden)
		return
	}
	if _, err := authoring.LoadProject(w.targets.Project); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	op, err := buildOperation(w.targets, r.FormValue("offline") == "true")
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := w.supervisor.Start(op); err != nil {
		status := http.StatusInternalServerError
		if err == oprunner.ErrBusy {
			status = http.StatusConflict
		}
		http.Error(rw, err.Error(), status)
		return
	}
	http.Redirect(rw, r, "/project?notice=Build+started", http.StatusSeeOther)
}

func (w *Workbench) handleProjectRun(rw http.ResponseWriter, _ *http.Request) {
	run := w.supervisor.Snapshot()
	page := projectPage{Run: run, ArtifactName: filepath.Base(run.Artifact), Stages: projectStages(run)}
	var body bytes.Buffer
	if err := w.pages["project"].ExecuteTemplate(&body, "project-run", page); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Write(body.Bytes())
}

func (w *Workbench) handleProjectOpen(rw http.ResponseWriter, r *http.Request) {
	if err := oprunner.CheckOrigin(r); err != nil {
		http.Error(rw, err.Error(), http.StatusForbidden)
		return
	}
	artifact, err := w.currentArtifact()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if w.openArtifact == nil {
		http.Error(rw, "native Atlas handoff is unavailable", http.StatusNotImplemented)
		return
	}
	if err := w.openArtifact(artifact); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(rw, r, "/project?notice=Opened+in+Atlas", http.StatusSeeOther)
}

func (w *Workbench) handleProjectArtifact(rw http.ResponseWriter, r *http.Request) {
	artifact, err := w.openCompletedArtifact()
	if err != nil {
		http.NotFound(rw, r)
		return
	}
	defer artifact.Close()
	if _, err := artifact.source.Seek(0, io.SeekStart); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": artifact.name})
	rw.Header().Set("Content-Disposition", disposition)
	info, err := artifact.source.Stat()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(rw, r, artifact.name, info.ModTime(), artifact.source)
}

func (w *Workbench) currentArtifact() (string, error) {
	artifact, err := w.openCompletedArtifact()
	if err != nil {
		return "", err
	}
	defer artifact.Close()
	return artifact.path, nil
}
