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
			Title:     "Atlas — Game Map Explorer",
			Width:     1440,
			Height:    920,
			MinWidth:  900,
			MinHeight: 600,
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		os.Exit(1)
	}
}
