// Command atlas is the desktop shell: one window over the application handler.
//
// Everything in this file is host wiring (issue #5 §3.4). The application is
// one pure http.Handler that touches no filesystem and opens no dialog; what
// is here is the three things a desktop makes of it -- a window to draw it in,
// an operating system to keep the library and the session records on, and a
// native panel to choose a bundle with. Move those three and the same handler
// is `atlas serve`, which is exactly what cmd/atlas/serve.go does in fewer
// lines still.
//
// Two things this shell deliberately does not have. There is no framework: the
// application it hosts consumed exactly one event subscription and one file
// dialog from the one it used to have, and both are here in plain sight. And
// there is no Wails runtime JavaScript in the page -- nothing is bound, no
// bindings JSON is generated, and the page hears that the library moved over
// the application's own SSE stream (issue #5 §4.6). The one place Wails would
// inject its runtime is a 200 text/html answer at a path ending in "/", which
// this application only ever gives on an empty library: every explorer page is
// at /v/<volume>/<world> and is served exactly as the headless host serves it.
//
// The build is a plain `go build`, not `wails build`, which is why there is no
// wails.json: the Wails CLI's job is scaffolding a Vite frontend and this tree
// has none. See the Makefile's `desktop` target and .github/workflows/release.yml
// for the recipe, tags and all.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/wailshost"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The seam, embedded, so that the shipped application is one file. `make seam`
// writes render/dist/app.js and `make static` copies it here; a build that
// skipped both ships a static/ holding only its own README, /static/app.js
// answers 404, and the application works without the seam -- which is the
// deletability principle of issue #5 §3.2 standing up in the shipping binary
// rather than only in the test suite.
//
// The stylesheet system is not here. It is internal/app/assets, embedded by
// the application itself and served from /assets, because it is the
// application's own chrome and deleting the seam must not cost the page its
// appearance.
//
//go:embed static
var seam embed.FS

// The included Earth volume: a real, ordinary .atlas bundle -- NASA's Blue
// Marble base map, composed by `make included-earth` and committed at
// included/ -- embedded so a first launch opens onto a world instead of an
// empty library. It is embedded here and only here: the desktop shell is the
// host that owns a library, where `atlas serve` and the CLI use exactly the
// registry the operator gives them. included/README.md is the provenance and
// the regeneration recipe.
//
//go:embed included/*.atlas
var included embed.FS

// appIdentifier is the directory the application keeps its data under, on
// whichever configuration directory the machine calls its own. It is the
// identifier the pre-rewrite shell used, unchanged and on purpose: a reader's
// library is where it always was and this build finds it there.
//
// cmd/atlas/serve.go spells the same three constants for the headless host.
// They are duplicated rather than shared because they are each host's own
// account of where it looks, and a host that is not an operating system --
// the WASM one of §3.3 -- will have neither an environment nor a
// configuration directory to read.
const (
	appIdentifier = "dev.felinestatemachine.atlas"
	bundlesDirEnv = "ATLAS_BUNDLES_DIR"
	dataDirEnv    = "ATLAS_DATA_DIR"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := logging.Setup(logging.Options{}); err != nil {
		return err
	}

	data, err := dataDir()
	if err != nil {
		return err
	}
	library := os.Getenv(bundlesDirEnv)
	if library == "" {
		library = filepath.Join(data, "bundles")
	}
	// The library is created rather than merely looked for: a first launch
	// should show a reader an empty library with a path they can drop a
	// bundle into, not an error about a directory nobody has made yet.
	if err := os.MkdirAll(library, 0o755); err != nil {
		return fmt.Errorf("creating the library at %s: %w", library, err)
	}
	// The included Earth build lands before the host is constructed, so the
	// host's first scan of the library already sees it. Installation goes
	// through format/bundle's own path -- validated, versioned, staged,
	// idempotent -- so an already-installed build is a no-op and another Earth
	// build in the library is left exactly where it is, side by side, for the
	// registry fold to order. A built-in that cannot be installed is a startup
	// error rather than a silent absence: the application would be shipping a
	// broken asset, and that has to be heard about.
	if err := installIncluded(library); err != nil {
		return fmt.Errorf("installing the included Earth volume: %w", err)
	}

	// The window is declared before the host because the host holds its
	// picker and the window learns its context at startup, after the handler
	// is already serving. An import that races the window loses and is told
	// so (wailshost.Window.Pick).
	var window wailshost.Window
	host, err := oshost.New(oshost.Options{
		BundlesDir:  library,
		SessionsDir: filepath.Join(data, "sessions"),
		Pick:        window.Pick,
	})
	if err != nil {
		return err
	}

	static, err := fs.Sub(seam, "static")
	if err != nil {
		return err
	}
	handler := app.New(host, app.Options{Static: static})

	slog.Info("opening", logging.Op("desktop"), logging.Path(library),
		slog.Int("volumes", len(host.Volumes().Volumes())))

	return wails.Run(&options.App{
		Title: "Atlas — World Explorer",
		// The application *is* the asset server. Every request the page makes
		// -- pages, partials, the /data plane, the events stream -- is served
		// by the same handler the headless host serves, over the webview's own
		// scheme instead of a socket. The one thing that scheme cannot carry
		// is a redirect, so the host walks those itself (redirects.go).
		AssetServer: &assetserver.Options{Handler: followRedirects(handler)},
		OnStartup:   window.Opened,
		// A map wants every pixel it can have, so the window opens filling the
		// screen it lands on rather than at a size chosen here. Not fullscreen:
		// the menu bar and the dock stay, and the window is still a window.
		// Width and Height are what it returns to when unzoomed.
		WindowStartState: options.Maximised,
		Width:            1440,
		Height:           920,
		MinWidth:         900,
		MinHeight:        600,
	})
}

// installIncluded installs every embedded bundle into the library. After it
// returns, the embedded copies are done with: opening, rendering, analysis and
// export all read the installed files through the same paths any imported
// bundle is read through.
func installIncluded(library string) error {
	names, err := fs.Glob(included, "included/*.atlas")
	if err != nil {
		return err
	}
	for _, name := range names {
		data, err := included.ReadFile(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		installed, err := bundle.InstallBytes(library, data)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		slog.Info("included volume installed", logging.Op("desktop"),
			logging.Volume(installed.Slug), logging.Stamp(bundle.ShortStamp(installed.Stamp)),
			logging.Path(installed.Locator))
	}
	return nil
}

// dataDir is where the application keeps what is its own: the library of
// installed volumes and the session records that remember where a reader was.
// ATLAS_DATA_DIR points the whole of it somewhere else, which is how a
// development run reads and writes beside a checkout instead of in the
// reader's real library.
func dataDir() (string, error) {
	if fromEnv := os.Getenv(dataDirEnv); fromEnv != "" {
		return fromEnv, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no %s given and no configuration directory to fall back on: %w",
			dataDirEnv, err)
	}
	return filepath.Join(base, appIdentifier), nil
}
