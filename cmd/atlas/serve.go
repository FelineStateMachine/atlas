package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// `atlas serve` is the headless host: the same application the desktop window
// shows, over plain HTTP, with no window at all. It is the dev loop, the CI
// host, and what the e2e suite drives in a real browser (tests/e2e).
//
// It is also the proof that the handler is portable: everything OS-shaped here
// is three lines of hostenv wiring, and the handler below them cannot tell
// which host it is under.

// serveDefaults are the environment variables the flags fall back to, which is
// how a dev loop points the host at a freshly generated library without
// spelling it out every time.
const (
	bundlesDirEnv = "ATLAS_BUNDLES_DIR"
	dataDirEnv    = "ATLAS_DATA_DIR"

	// appIdentifier is the directory the application keeps its data under, on
	// whichever config directory the machine calls its own.
	appIdentifier = "dev.felinestatemachine.atlas"
)

func serveCommand() command {
	return command{
		name:    "serve",
		summary: "serve the application over HTTP, with no window",
		run:     runServe,
	}
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:0",
		"address to listen on; port 0 takes whatever is free, so a running application is never in the way")
	bundles := flags.String("bundles", "",
		"the library of .atlas files (default $"+bundlesDirEnv+", else the application's own data directory)")
	data := flags.String("data", "",
		"where session records are written (default $"+dataDirEnv+"; empty means sessions live only as long as the process)")
	static := flags.String("static", "",
		"a directory served under /static: the seam's built bundle and the stylesheets")
	var logs logging.Options
	logs.Bind(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := logging.Setup(logs); err != nil {
		return err
	}

	library, err := libraryDir(*bundles)
	if err != nil {
		return err
	}
	sessions := *data
	if sessions == "" {
		sessions = os.Getenv(dataDirEnv)
	}
	if sessions != "" {
		sessions = filepath.Join(sessions, "sessions")
	}

	host, err := oshost.New(oshost.Options{BundlesDir: library, SessionsDir: sessions})
	if err != nil {
		return err
	}
	var assets fs.FS
	if *static != "" {
		assets = os.DirFS(*static)
	}
	handler := app.New(host, app.Options{Static: assets})

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	url := "http://" + listener.Addr().String()

	// The address is product output: a script starts the host and reads where
	// it landed. The narration of the same fact goes to the event stream,
	// which is on stderr, so a pipe stays clean (docs/logging.md).
	fmt.Println(url)
	slog.Info("serving", logging.Op("serve"),
		slog.String("addr", listener.Addr().String()), logging.Path(library),
		slog.Int("volumes", len(host.Volumes().Volumes())))

	server := &http.Server{
		Handler: handler,
		// The events stream is held open for as long as a page is open, so
		// there is no write deadline to set. Reads are small and prompt.
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Interrupt is a reader at a terminal; SIGTERM is a script that started
	// the host and is finished with it. Both mean stop.
	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

// libraryDir is where the .atlas files are: the flag, the environment, or the
// application's own data directory, in that order.
func libraryDir(flagged string) (string, error) {
	if flagged != "" {
		return flagged, nil
	}
	if fromEnv := os.Getenv(bundlesDirEnv); fromEnv != "" {
		return fromEnv, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no -bundles given and no config directory to fall back on: %w", err)
	}
	return filepath.Join(base, appIdentifier, "bundles"), nil
}
