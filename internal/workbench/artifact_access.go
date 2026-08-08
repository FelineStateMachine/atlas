package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

// completedArtifact keeps the exact regular file that passed the native
// integrity gate open. Inspector and download consumers therefore read the
// validated inode rather than reopening a replaceable pathname.
type completedArtifact struct {
	path       string
	name       string
	source     *os.File
	reader     *vnext.Reader
	descriptor vnext.Descriptor
}

func (artifact *completedArtifact) Close() error { return artifact.source.Close() }

func (w *Workbench) openCompletedArtifact() (*completedArtifact, error) {
	run := w.supervisor.Snapshot()
	if run.Name != "build" || run.Running || run.Failed || run.FinishedAt.IsZero() || run.Artifact == "" {
		return nil, fmt.Errorf("no successful completed Atlas build is available")
	}
	absLibrary, err := filepath.Abs(w.targets.Registry)
	if err != nil {
		return nil, err
	}
	absArtifact, err := filepath.Abs(run.Artifact)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(absLibrary, absArtifact)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Ext(relative) != ".atlas" {
		return nil, fmt.Errorf("build artifact is outside the Atlas library")
	}
	root, err := os.OpenRoot(absLibrary)
	if err != nil {
		return nil, fmt.Errorf("open Atlas library: %w", err)
	}
	defer root.Close()
	linkInfo, err := root.Lstat(relative)
	if err != nil {
		return nil, fmt.Errorf("stat completed Atlas artifact: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("completed Atlas artifact is a symbolic link")
	}
	source, err := root.Open(relative)
	if err != nil {
		return nil, fmt.Errorf("open completed Atlas artifact: %w", err)
	}
	info, err := source.Stat()
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("stat opened Atlas artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		source.Close()
		return nil, fmt.Errorf("completed Atlas artifact is not a regular file")
	}
	reader, err := vnext.Open(source, info.Size(), vnext.StandardSchema())
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("open completed Atlas artifact: %w", err)
	}
	if err := reader.Validate(); err != nil {
		source.Close()
		return nil, fmt.Errorf("validate completed Atlas artifact: %w", err)
	}
	return &completedArtifact{
		path: absArtifact, name: filepath.Base(absArtifact), source: source, reader: reader,
		descriptor: vnext.DescriptorOf(absArtifact, reader.VolumeID, reader.Release, info.Size()),
	}, nil
}
