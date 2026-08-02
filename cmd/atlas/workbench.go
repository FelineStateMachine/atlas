package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FelineStateMachine/atlas/internal/app/assets"
	"github.com/FelineStateMachine/atlas/internal/generate/crawl"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
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
		summary: "serve the workbench: scores, build diffs, sources and pipeline operations",
		run:     runWorkbench,
	}
}

func runWorkbench(args []string) error {
	fs := flags("workbench", "[-addr HOST:PORT] [-bundles DIR] [-archive DIR] [-tiles DIR]")
	addr := fs.String("addr", "127.0.0.1:6180", "address the workbench listens on")
	bundleDir := fs.String("bundles", "",
		"registry of .atlas files to measure; default is the application's own library")
	archiveDir := fs.String("archive", "",
		"capture archive root operations read and write (operations that need one are refused without it)")
	tileSet := fs.String("tiles", "",
		"derived tile set directory operations write and read")
	var log logging.Options
	log.Bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := logging.Setup(log); err != nil {
		return err
	}

	registry := *bundleDir
	var err error
	if registry == "" {
		if registry, err = defaultRegistryDir(); err != nil {
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
		Archive:  *archiveDir,
		TileSet:  *tileSet,
	}
	if *tileSet != "" {
		targets.TileIndex = filepath.Join(*tileSet, tiles.IndexName)
	}

	handler, err := workbench.New(workbench.Options{
		Targets: targets,
		Sources: sourceCards(),
		Runtime: assets.Runtime(),
	})
	if err != nil {
		return err
	}

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

// sourceCards is the generate lane's source registry, as the workbench's cards.
//
// Every fact is the lane's own: the identity and the terms come from the
// source's Describe(), and whether a fetch may be offered at all comes from
// whether a crawler is registered under the same name. Nothing is restated
// here, so a card cannot disagree with the documents its source emits.
func sourceCards() []workbench.Source {
	cards := make([]workbench.Source, 0, len(sources.All()))
	for _, source := range sources.All() {
		about := source.Describe()
		card := workbench.Source{
			Name:        about.Name,
			Label:       about.Label,
			License:     about.License,
			Attribution: about.Attribution,
			IDSpace:     about.IDSpace,
		}
		if crawler, err := crawl.For(about.Name); err == nil {
			card.Crawlable = true
			card.TargetHint = crawler.Usage()
			// A crawler's usage line shows the shape of its target, and a
			// target addressed as two slugs shows it as a slash. That is the
			// one thing target validation has to be told, and the crawler is
			// the only thing that knows it.
			card.Pair = strings.Contains(crawler.Usage(), "/")
		}
		cards = append(cards, card)
	}
	return cards
}
