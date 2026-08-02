// Package templates holds the application's server-rendered HTML, one file per
// region, mirroring the one-CSS-file-per-region system carried over from the
// reference implementation (issue #5 §4.5).
//
// Templates render; they do not decide. Every display decision -- legend
// algebra, AND-across/OR-within filtering, label ladders, reading the
// conventions -- happens in Go before a template is given anything, so a
// template that needs an `if` about policy is a signal that the policy is in
// the wrong place.
//
// # The rules these files are held to
//
// One file per region, named for the region, mirroring the one-file-per-region
// stylesheet system carried into internal/app/assets/css. A region's template
// renders that region's own container, so a first paint and a swap produce the
// same bytes and there is no third spelling of the page.
//
// No hx-on. Every interaction is an hx-* attribute naming a route, and the
// only glue on the page is the seam's boot module (issue #5 §4.3).
//
// Inheritance is explicit. hx-vals:inherited and hx-swap:inherited are
// declared once, on the shell, and nothing else inherits: an attribute that
// travels without ":inherited" spelled out is spooky action, and the point of
// declaring it at the top is that a reader of one template can see everything
// acting on it.
//
// The hx-* vocabulary lives here and nowhere else (issue #5 §4.3). The routes,
// the region names, the partial envelope and the viewport state node are all
// framework-neutral contracts, so replacing HTMX would be an afternoon in this
// directory rather than an architecture event.
//
// What must not drift is the names: the region names below, the element ids
// they render, and the partial envelope internal/app wraps them in. Those are
// the contract the handler, the stylesheet system and the seam all agree on.
package templates

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"sync"
)

//go:embed *.tmpl
var files embed.FS

// Regions are the names a template may be rendered under, in the order the
// page lays them out. One file per region, named for the region.
var Regions = []string{
	"shell",
	"topbar",
	"legend",
	"dock",
	"detail",
	"grid-navigator",
	"overview",
	"viewport",
	"empty-state",
	"import",
}

// parsed is the whole template set. Parsing at package initialisation would
// be init magic; this is a package-level value built by a function the tests
// can also call, and swapped whole by Reload.
var (
	mu     sync.RWMutex
	parsed = template.Must(parse(files))
)

func parse(tree fs.FS) (*template.Template, error) {
	return template.New("atlas").ParseFS(tree, "*.tmpl")
}

// Reload re-parses the set from another tree, for the development loop:
// `atlas dev` watches the working copy and calls this when a file changes, so
// a template edit is visible on the next request rather than the next build.
//
// A tree that will not parse is refused whole and the set in hand is kept. An
// unbalanced {{if}} halfway through an edit is a normal thing to type, and
// taking the running page down over it would make the loop worse than a
// rebuild.
//
// The embedded tree is what a shipped binary serves and nothing calls this
// there; the fs.FS argument is how the rule that the application never touches
// a filesystem survives having a development loop (issue #5 §3.3).
func Reload(tree fs.FS) error {
	fresh, err := parse(tree)
	if err != nil {
		return fmt.Errorf("reload templates: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	parsed = fresh
	return nil
}

func held() *template.Template {
	mu.RLock()
	defer mu.RUnlock()
	return parsed
}

// Render writes one region.
func Render(w io.Writer, region string, data any) error {
	if err := held().ExecuteTemplate(w, region, data); err != nil {
		return fmt.Errorf("render %s: %w", region, err)
	}
	return nil
}

// Has reports whether a region has a template.
func Has(region string) bool { return held().Lookup(region) != nil }
