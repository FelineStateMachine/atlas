// Command atlas is an offline interactive map explorer built with Allons.
// The executable carries only the application shell; each game arrives as a
// self-contained .atlas bundle dropped into the bundles directory, and the
// application needs no network connection either way.
package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/FelineStateMachine/allons/local"
	"github.com/FelineStateMachine/allons/local/events"
	"github.com/FelineStateMachine/allons/wailsapp"
	"github.com/FelineStateMachine/atlas/internal/bundle"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// catalogChangedTopic is the event the frontend refreshes its catalog on: a
// bundle arrived, was replaced, or left, and the games on offer moved with it.
const catalogChangedTopic = "atlas:catalog-changed"

// Archive capture is deliberately not a generate step: it reaches out to
// MapGenie and takes as long as the chosen depth demands. Run it by hand for
// the game or map you want, then regenerate.
//
//	go run ./tools/crawl -game skyrim -map solstheim -max-zoom 15
//	node tools/icons/render-icons.mjs --game skyrim

//go:generate go run ./tools/tiles -source ../gamemap/fmg-archive -output build/tiles
//go:generate go run ./tools/generate -source ../gamemap -tiles build/tiles/index.json -bundles dist/bundles
//go:generate npm --prefix frontend ci
//go:generate npm --prefix frontend run build

//go:embed assets/index.html assets/app.css assets/app.js
var assets embed.FS

func main() {
	// Created inside Routes, which the framework calls while opening the
	// application, and used again at startup below; Open completes before the
	// window exists, so the order holds.
	var registry *bundle.Registry
	err := wailsapp.Run(
		local.Config{
			AppID:     "dev.felinestatemachine.atlas",
			Schema:    local.Schema{AppSchemaVersion: 1, AppSchemaMinReadable: 1},
			Assets:    assets,
			Bootstrap: local.CustomBootstrap("/"),
			Sync:      local.SyncConfig{Autostart: local.AutostartOff},
			Routes: func(app *local.App) http.Handler {
				registry = bundle.NewRegistry(bundlesDir(app))
				if err := registry.Rescan(); err != nil {
					slog.Warn("atlas: scanning bundles", "error", err)
				}
				// Published through the bus so the frontend hears about the
				// library changing the same way it would hear about anything
				// else: the emitter is bound at startup, and events published
				// before a window exists have no one to reach anyway.
				registry.SetOnChange(func(changed []string) {
					app.Events().Publish(catalogChangedTopic, events.Detail{Entities: changed})
				})
				return routes(assets, registry)
			},
		},
		&options.App{
			Title: "Atlas — Game Map Explorer",
			OnStartup: func(wctx context.Context) {
				captureWailsContext(wctx)
				if err := registry.Watch(wctx); err != nil {
					slog.Warn("atlas: watching bundles", "error", err)
				}
			},
			// A map wants every pixel it can have, so the window opens filling
			// the screen it lands on rather than at a size chosen here. Not
			// fullscreen: the menu bar and dock stay, and the window is still a
			// window. Width and Height are what it returns to when unzoomed.
			WindowStartState: options.Maximised,
			Width:            1440,
			Height:           920,
			MinWidth:         900,
			MinHeight:        600,
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}

// bundlesDir is where installed games live: a directory of .atlas files under
// the application's own data directory. ATLAS_BUNDLES_DIR points elsewhere for
// development, so a freshly generated dist/bundles serves without being copied
// into the running application's library.
func bundlesDir(app *local.App) string {
	dir := os.Getenv("ATLAS_BUNDLES_DIR")
	if dir == "" {
		dir = filepath.Join(app.DataDir(), "bundles")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("atlas: creating bundles directory", "path", dir, "error", err)
	}
	return dir
}
