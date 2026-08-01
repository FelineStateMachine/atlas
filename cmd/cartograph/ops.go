package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
)

// The workbench runs the pipeline's own tools rather than reimplementing
// them: a fetch is tools/crawl, pyramids are tools/tiles, composition is
// tools/generate, each invoked exactly as a person at a terminal would and
// each streamed back to the page as it speaks. Nothing runs on a schedule or
// at startup; an operation exists only between a submitted form and the
// subprocess's exit.

// workshop holds where operations run: the repository whose tools are
// invoked, the archive captures land in, and the registry composed bundles
// install into. The lock keeps operations serial -- the tools are safe to
// interleave in principle, but two crawls sharing one progress pane help
// nobody.
type workshop struct {
	repo    string
	archive string
	bundles string
	busy    sync.Mutex
}

// fetchArgv is the tools/crawl invocation for one source and target,
// verbatim what a person would type in the repository.
func fetchArgv(src Source, target, archive string) ([]string, error) {
	selected, err := src.FetchArgs(target)
	if err != nil {
		return nil, err
	}
	argv := []string{"go", "run", "./tools/crawl", "-archive", archive}
	return append(argv, selected...), nil
}

// tilesArgv derives every pyramid the archive holds into build/tiles, which
// sits under the repository so the per-pyramid stamps persist and rebuilds
// stay incremental.
func tilesArgv(archive string) []string {
	return []string{"go", "run", "./tools/tiles", "-source", archive, "-output", "build/tiles"}
}

// generateArgv composes bundles from the archive and the derived tiles into
// the given registry directory. tools/generate is addressed by the directory
// containing fmg-archive rather than the archive itself, so the archive path
// must actually be one.
func generateArgv(archive, bundles string) ([]string, error) {
	if filepath.Base(archive) != "fmg-archive" {
		return nil, fmt.Errorf(
			"the archive %s is not an fmg-archive directory, and tools/generate finds captures under that name", archive)
	}
	return []string{
		"go", "run", "./tools/generate",
		"-source", filepath.Dir(archive),
		"-tiles", "build/tiles/index.json",
		"-bundles", bundles,
	}, nil
}

// streamCommand runs argv in dir and writes everything it says, both
// streams interleaved, to w as it arrives. The context is the request's: a
// page abandoned mid-operation stops its subprocess, so nothing crawls on
// with nobody watching.
func streamCommand(ctx context.Context, w io.Writer, dir string, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out := flushWriter{w}
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// flushWriter pushes every write through to the client immediately, so a
// long crawl reads as progress rather than silence. Writers that cannot
// flush -- a buffer in a test -- just write.
type flushWriter struct {
	w io.Writer
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if flusher, ok := f.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}
