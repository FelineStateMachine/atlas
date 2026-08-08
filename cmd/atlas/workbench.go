package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/FelineStateMachine/atlas/internal/app/assets"
	"github.com/FelineStateMachine/atlas/internal/logging"
	"github.com/FelineStateMachine/atlas/internal/workbench"
)

// `atlas workbench` is the workbench beside the reader: the measurement pages,
// the build-to-build diffs, the source registry, and the pipeline operations
// (issue #5 §5.6). It touches nothing on its own -- reading the library is free,
// and every operation that fetches or writes runs only when a person submits it
// from the page.
//
// This file is also the wiring the workbench's independence rests on. The
// handler may not import a pipeline lane, so three things it cannot reach are
// handed to it here, where a command is allowed to know every lane:
//
//   - the source registry entries, licence and attribution included, read from
//     the generate lane's own sources and crawlers;
//   - the tile register's file name, which is the generate lane's to know;
//   - the vendored hypermedia runtime, which is vendored once for the whole
//     program and lives with the application's assets.
//
// And one thing it reaches for itself: the binary operations invoke is this
// process's own executable, so the workbench runs the same build of the
// pipeline that is serving the page.

func workbenchCommand() command {
	return command{
		name:    "workbench",
		summary: "author, plan and build one .atlas-project beside the Atlas library",
		run:     runWorkbench,
	}
}

func runWorkbench(args []string) error {
	fs := flags("workbench", "[-addr HOST:PORT] [-bundles DIR] [-cache DIR] FILE.atlas-project")
	addr := fs.String("addr", "127.0.0.1:6180", "address the workbench listens on")
	bundleDir := fs.String("bundles", "",
		"registry of .atlas files to measure; default is the application's own library")
	cacheDir := fs.String("cache", "", "shared content-addressed source cache")
	var log logging.Options
	log.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := logging.Setup(log); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("name exactly one .atlas-project manifest")
	}
	project, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	registry := *bundleDir
	if registry == "" {
		if registry, err = defaultRegistryDir(); err != nil {
			return err
		}
	}
	cache := *cacheDir
	if cache == "" {
		if cache, err = defaultAuthoringCacheDir(); err != nil {
			return err
		}
	}
	binary, err := os.Executable()
	if err != nil {
		// A workbench that cannot name its own binary still measures; it just
		// cannot operate, and every operation card says so.
		slog.Warn("the workbench cannot find its own binary; operations are unavailable",
			logging.Op("workbench"), slog.Any("error", err))
		binary = ""
	}
	targets := workbench.Targets{
		Atlas:    binary,
		Registry: registry,
		Cache:    cache,
		Project:  project,
		Dir:      filepath.Dir(project),
	}

	handler, err := workbench.New(workbench.Options{
		Targets: targets,
		Runtime: assets.Runtime(),
		OpenArtifact: func(path string) error {
			if err := exec.Command("open", "-b", applicationID, path).Run(); err != nil {
				return fmt.Errorf("open in Atlas: %w", err)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	defer handler.Close()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	url := "http://" + listener.Addr().String()
	// The address is product output: a script starts the workbench and reads
	// where it landed. The narration goes to the event stream on stderr.
	fmt.Println(url)
	slog.Info("workbench serving", logging.Op("workbench"),
		slog.String("addr", listener.Addr().String()), logging.Path(registry))

	server := &http.Server{
		Handler: handler,
		// An operation is held open for as long as it runs, so there is no
		// write deadline to set. Reads are small and prompt.
		ReadHeaderTimeout: 10 * time.Second,
	}
	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopping.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Warn("shutting down", logging.Op("workbench"), slog.Any("error", err))
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("workbench stopped", logging.Op("workbench"))
	return nil
}
