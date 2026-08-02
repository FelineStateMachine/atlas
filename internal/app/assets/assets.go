// Package assets holds the application's own chrome: the stylesheet system
// and the hypermedia runtime, embedded in the binary.
//
// This is not the seam's tree. The seam's built bundle arrives from whatever
// host mounted it and is served under /static, which answers 404 when a host
// mounted nothing -- the application is required to build, serve and work
// without the seam present (issue #5 §3.2). What is here is the part of the
// page that is the application's own and must be there whatever else is not:
// the chrome a reader sees, and the runtime that swaps it.
//
// # The stylesheet system
//
// css/ is the token-first, one-file-per-region system carried over from the
// reference implementation as an asset (issue #5 §9). The files are the
// reference's own, unedited; where the new DOM demands a different selector
// the difference lives in chrome.css alone, so the delta between the carried
// system and this application's markup is one file long and readable in one
// sitting.
//
// The files are concatenated in the order below rather than chained by
// @import, because thirty-three imports is thirty-three round trips for a
// stylesheet that is forty kilobytes whole. The order is the cascade and is
// load-bearing: a handful of files deliberately override earlier ones.
//
// # The runtime
//
// htmx.min.js is htmx 4.0.0-beta6, vendored, and hx-sse.min.js is its
// server-sent-events extension, which is how the page hears that the library
// moved (§4.6). Both are vendored rather than linked because the offline
// invariant is not only about bundles: an atlas opens on a machine with no
// network, forever, and a page that fetches its own runtime from a CDN is a
// page that does not (issue #5 §2). The licence travels beside them as
// htmx.LICENSE.
package assets

import (
	"bytes"
	"embed"
	"io/fs"
	"sync"
)

//go:embed css/*.css htmx.min.js hx-sse.min.js htmx.LICENSE
var files embed.FS

// Files is the embedded tree, for a host that would rather serve it whole.
func Files() fs.FS { return files }

// Reloadable is what the development loop watches: the names, relative to a
// working copy of this directory, whose contents this package serves.
func Reloadable() []string { return append([]string(nil), stylesheets...) }

// stylesheets is the cascade, in order. One line per region, mirroring the
// one-file-per-region template set; chrome.css is last because its whole job
// is to have the last word.
var stylesheets = []string{
	"css/tokens.css",
	"css/base.css",
	"css/shell.css",
	"css/topbar.css",
	"css/brand.css",
	"css/map-pickers.css",
	"css/icon-button.css",
	"css/topbar-collapse.css",
	"css/sidebar.css",
	"css/search.css",
	"css/legend-toolbar.css",
	"css/tooltip.css",
	"css/layers.css",
	"css/layer-header.css",
	"css/solo-chip.css",
	"css/layer-switch.css",
	"css/text-symbol.css",
	"css/feature-index.css",
	"css/category-rows.css",
	"css/sidebar-footer.css",
	"css/map-panel.css",
	"css/globe.css",
	"css/map-loading.css",
	"css/map-corner.css",
	"css/overview.css",
	"css/zoom-controls.css",
	"css/grid-navigator.css",
	"css/map-hint.css",
	"css/dock.css",
	"css/pin-detail.css",
	"css/fatal-error.css",
	"css/empty-state.css",
	"css/responsive.css",
	"css/chrome.css",
}

var (
	mu     sync.RWMutex
	tree   fs.FS = files
	sheet  []byte
	loaded bool
)

// Reload points the assets at another tree, for the development loop: `atlas
// dev` watches the working copy so a stylesheet edit is visible on the next
// request. A shipped binary never calls it and serves what is embedded.
func Reload(other fs.FS) {
	mu.Lock()
	defer mu.Unlock()
	tree = other
	sheet, loaded = nil, false
	runtime, runtimeLoaded = nil, false
}

func read(name string) ([]byte, error) {
	mu.RLock()
	held := tree
	mu.RUnlock()
	return fs.ReadFile(held, name)
}

// Stylesheet is the whole cascade as one document. It is assembled on first
// ask from bytes that are already in the binary, and again after a Reload.
func Stylesheet() []byte {
	mu.RLock()
	if loaded {
		defer mu.RUnlock()
		return sheet
	}
	mu.RUnlock()

	var out bytes.Buffer
	for _, name := range stylesheets {
		body, err := read(name)
		if err != nil {
			// The list and the tree are both in this package and both in
			// the binary; a name that will not read is a build mistake,
			// and skipping it loses a region's styling rather than the
			// page.
			continue
		}
		out.WriteString("/* " + name + " */\n")
		out.Write(body)
		out.WriteString("\n")
	}
	mu.Lock()
	defer mu.Unlock()
	sheet, loaded = out.Bytes(), true
	return sheet
}

var (
	runtime       []byte
	runtimeLoaded bool
)

// Runtime is the hypermedia runtime: htmx and the one extension the
// application uses, in that order, as one script.
func Runtime() []byte {
	mu.RLock()
	if runtimeLoaded {
		defer mu.RUnlock()
		return runtime
	}
	mu.RUnlock()

	var out bytes.Buffer
	for _, name := range []string{"htmx.min.js", "hx-sse.min.js"} {
		body, err := read(name)
		if err != nil {
			continue
		}
		out.Write(body)
		out.WriteString("\n")
	}
	mu.Lock()
	defer mu.Unlock()
	runtime, runtimeLoaded = out.Bytes(), true
	return runtime
}
