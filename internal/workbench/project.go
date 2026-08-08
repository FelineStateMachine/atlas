package workbench

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
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
}

func (w *Workbench) projectPage(notice string) projectPage {
	page := projectPage{Targets: w.targets, Notice: notice, Run: w.supervisor.Snapshot()}
	page.ArtifactName = filepath.Base(page.Run.Artifact)
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
	page.SourceCount = len(project.Sources)
	page.RasterCount = len(project.Rasters)
	page.LayerCount = len(project.Presentation.Layers)
	page.CachedCount = plan.Cached
	page.RequestCount = len(plan.Requests)
	sets := make(map[string]bool)
	for _, source := range project.Sources {
		sets[source.Mapping.FeatureSet] = true
	}
	page.FeatureSets = len(sets)
	return page
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
	page := projectPage{Run: run, ArtifactName: filepath.Base(run.Artifact)}
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
	artifact, err := w.currentArtifact()
	if err != nil {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(artifact)))
	http.ServeFile(rw, r, artifact)
}

func (w *Workbench) currentArtifact() (string, error) {
	artifact := w.supervisor.Snapshot().Artifact
	if artifact == "" {
		return "", fmt.Errorf("no completed Atlas artifact is available")
	}
	absArtifact, err := filepath.Abs(artifact)
	if err != nil {
		return "", err
	}
	absLibrary, err := filepath.Abs(w.targets.Registry)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(absLibrary, absArtifact)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Ext(absArtifact) != ".atlas" {
		return "", fmt.Errorf("build artifact is outside the Atlas library")
	}
	file, err := vnext.OpenFile(absArtifact, vnext.StandardSchema())
	if err != nil {
		return "", fmt.Errorf("open completed Atlas artifact: %w", err)
	}
	defer file.Close()
	if err := file.Validate(); err != nil {
		return "", fmt.Errorf("validate completed Atlas artifact: %w", err)
	}
	return absArtifact, nil
}
