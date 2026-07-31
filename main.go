// Command atlas is a self-contained interactive map explorer built with
// Allons. Its FMG catalog, raster tile pyramids, category icons, and frontend
// are embedded in the executable, so the application needs no network
// connection or sidecar data.
package main

import (
	"embed"
	"fmt"
	"net/http"
	"os"

	"github.com/FelineStateMachine/allons/local"
	"github.com/FelineStateMachine/allons/wailsapp"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// Archive capture is deliberately not a generate step: it reaches out to
// MapGenie and takes as long as the chosen depth demands. Run it by hand for
// the game or map you want, then regenerate.
//
//	go run ./tools/crawl -game skyrim -map solstheim -max-zoom 15
//	node tools/icons/render-icons.mjs --game skyrim

//go:generate go run ./tools/tiles -source ../gamemap/fmg-archive -output assets/tiles
//go:generate go run ./tools/generate -source ../gamemap -tiles assets/tiles/index.json -output assets/catalog.json
//go:generate npm --prefix frontend ci
//go:generate npm --prefix frontend run build

//go:embed all:assets
var assets embed.FS

func main() {
	err := wailsapp.Run(
		local.Config{
			AppID:     "dev.felinestatemachine.atlas",
			Schema:    local.Schema{AppSchemaVersion: 1, AppSchemaMinReadable: 1},
			Assets:    assets,
			Bootstrap: local.CustomBootstrap("/"),
			Sync:      local.SyncConfig{Autostart: local.AutostartOff},
			Routes: func(*local.App) http.Handler {
				return routes(assets)
			},
		},
		&options.App{
			Title: "Atlas — Game Map Explorer",
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
