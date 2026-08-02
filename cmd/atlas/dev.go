package main

import (
	"context"
	"errors"
	"flag"
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

	"github.com/fsnotify/fsnotify"

	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/assets"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
	"github.com/FelineStateMachine/atlas/internal/app/templates"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// `atlas dev` is the development loop: one command that serves the application
// and re-reads its own chrome when it changes.
//
// It is `atlas serve` with two additions and no third. Templates and
// stylesheets are read from the working copy instead of the binary and
// re-parsed the moment a file is written, so a template edit is one refresh
// away rather than one rebuild away. And -seam-watch runs the seam's own
// watcher beside it, for when there is a seam to watch.
//
// The watching lives here, in cmd/, and nowhere else. internal/app is one pure
// http.Handler that touches no filesystem (issue #5 §3.3), and a development
// loop is exactly the kind of convenience that would erode that rule if it
// were allowed inside: what the application exposes is a Reload that takes an
// fs.FS, and building an fs.FS out of a directory is this file's business.

const (
	// devTemplates and devAssets are where the chrome lives in a working
	// copy. They are relative to the module root, which is where a developer
	// runs this from.
	devTemplates = "internal/app/templates"
	devAssets    = "internal/app/assets"

	// devSeam is the seam's own directory. It does not exist yet.
	devSeam = "render"
)

func devCommand() command {
	return command{
		name:    "dev",
		summary: "serve the application from the working copy, re-reading it as it changes",
		run:     runDev,
	}
}

func runDev(args []string) error {
	flags := flag.NewFlagSet("dev", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:7433",
		"address to listen on; a fixed port by default, because a development loop is worth bookmarking")
	bundles := flags.String("bundles", "",
		"the library of .atlas files (default $"+bundlesDirEnv+", else the application's own data directory)")
	data := flags.String("data", "",
		"where session records are written (empty means sessions live only as long as the process)")
	root := flags.String("root", ".",
		"the working copy the chrome is read from")
	static := flags.String("static", "",
		"a directory served under /static: the seam's built bundle")
	seamWatch := flags.Bool("seam-watch", false,
		"also run the seam's bundler in watch mode; a no-op with a message while the seam does not exist")
	var logs logging.Options
	logs.Bind(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := logging.Setup(logs); err != nil {
		return err
	}

	tree, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("working copy: %w", err)
	}

	// Read the chrome from the working copy before serving anything, so a
	// loop that starts against a broken template says so immediately rather
	// than at the first request.
	if err := reloadChrome(tree); err != nil {
		return err
	}

	library, err := libraryDir(*bundles)
	if err != nil {
		return err
	}
	sessions := *data
	if sessions != "" {
		sessions = filepath.Join(sessions, "sessions")
	}
	host, err := oshost.New(oshost.Options{BundlesDir: library, SessionsDir: sessions})
	if err != nil {
		return err
	}
	options := app.Options{}
	if *static != "" {
		options.Static = os.DirFS(*static)
	}
	handler := app.New(host, options)

	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watcher, err := watchChrome(stopping, tree)
	if err != nil {
		return err
	}
	defer watcher()

	if *seamWatch {
		if err := watchSeam(stopping, tree); err != nil {
			return err
		}
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	url := "http://" + listener.Addr().String()
	fmt.Println(url)
	slog.Info("developing", logging.Op("serve"),
		slog.String("addr", listener.Addr().String()), logging.Path(library),
		slog.Int("volumes", len(host.Volumes().Volumes())))

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-stopping.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Warn("shutting down", logging.Op("serve"), slog.Any("error", err))
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("stopped", logging.Op("serve"))
	return nil
}

// reloadChrome points the templates and the stylesheet system at the working
// copy. A tree that will not parse is reported and the set in hand is kept:
// an unbalanced action halfway through an edit is a normal thing to type.
func reloadChrome(root string) error {
	if err := templates.Reload(os.DirFS(filepath.Join(root, devTemplates))); err != nil {
		return err
	}
	assets.Reload(os.DirFS(filepath.Join(root, devAssets)))
	return nil
}

// watchChrome re-reads the chrome whenever a file under it is written.
//
// Editors write a file two or three times in a few milliseconds -- a temporary
// file, a rename, a permissions fixup -- so writes are coalesced over a short
// window rather than answered one for one, which turns a save into one reparse
// instead of four.
func watchChrome(ctx context.Context, root string) (func(), error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch the working copy: %w", err)
	}
	for _, dir := range []string{
		filepath.Join(root, devTemplates),
		filepath.Join(root, devAssets),
		filepath.Join(root, devAssets, "css"),
	} {
		if err := watcher.Add(dir); err != nil {
			slog.Warn("nothing to watch", logging.Op("serve"),
				logging.Path(dir), slog.Any("error", err))
		}
	}

	go func() {
		defer watcher.Close()
		var settling <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-watcher.Events:
				if !open {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
					continue
				}
				settling = time.After(60 * time.Millisecond)
			case <-settling:
				settling = nil
				if err := reloadChrome(root); err != nil {
					// A broken template is the ordinary state of a file
					// being edited. Say what is wrong and keep serving
					// the last set that parsed.
					slog.Warn("the chrome did not reload", logging.Op("render"),
						slog.Any("error", err))
					continue
				}
				slog.Info("chrome reloaded", logging.Op("render"))
			case err, open := <-watcher.Errors:
				if !open {
					return
				}
				slog.Warn("watching the working copy", logging.Op("serve"), slog.Any("error", err))
			}
		}
	}()
	return func() {}, nil
}

// watchSeam runs the seam's bundler beside the server.
//
// The seam is M6 and does not exist yet, so this is deliberately a stub that
// says so rather than a failure: `atlas dev -seam-watch` is the command a
// developer will want the day the seam lands, and it should not need writing
// then. When render/ exists with a package.json, this runs its watch script
// and streams its output into the same event stream everything else uses.
func watchSeam(ctx context.Context, root string) error {
	seam := filepath.Join(root, devSeam)
	if _, err := os.Stat(filepath.Join(seam, "package.json")); err != nil {
		slog.Info("no seam to watch", logging.Op("serve"), logging.Path(seam),
			slog.String("note", "render/ lands in M6; -seam-watch will run its bundler then"))
		return nil
	}
	cmd := exec.CommandContext(ctx, "npm", "run", "watch")
	cmd.Dir = seam
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the seam's bundler: %w", err)
	}
	slog.Info("watching the seam", logging.Op("serve"), logging.Path(seam))
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Warn("the seam's bundler stopped", logging.Op("serve"), slog.Any("error", err))
		}
	}()
	return nil
}
