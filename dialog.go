package main

import (
	"context"
	"sync/atomic"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The Wails context exists only between startup and shutdown, and native
// dialogs can only be asked for through it. It is captured at startup and
// read wherever a request handler needs to put a window in front of the
// user; a request that arrives before there is a window is refused rather
// than queued.
var wailsContext atomic.Value

func captureWailsContext(ctx context.Context) {
	wailsContext.Store(ctx)
}

// pickBundleFiles opens the native file dialog filtered to .atlas files and
// returns the chosen paths, or nothing if there is no window yet.
func pickBundleFiles() ([]string, bool) {
	ctx, ok := wailsContext.Load().(context.Context)
	if !ok {
		return nil, false
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(ctx, wailsruntime.OpenDialogOptions{
		Title: "Add game bundles",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Atlas bundles (*.atlas)", Pattern: "*.atlas"},
		},
	})
	if err != nil {
		wailsruntime.LogErrorf(ctx, "atlas: open dialog: %v", err)
		return nil, true
	}
	return paths, true
}
