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
// # State of this package
//
// The files here are region stubs: the right containers under the right ids,
// with the data a region is given already flowing into them, and no chrome.
// The templates wave fills them. What is already load-bearing, and must not
// drift, is the *names*: the region names below, the element ids they render,
// and the partial envelope internal/app wraps them in. Those are the contract
// the handler, the CSS system and the seam all agree on.
package templates

import (
	"embed"
	"fmt"
	"html/template"
	"io"
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

// parsed is the whole template set, parsed once. Parsing at package
// initialisation would be init magic; this is a package-level value built by
// a function the tests can also call.
var parsed = template.Must(parse())

func parse() (*template.Template, error) {
	return template.New("atlas").ParseFS(files, "*.tmpl")
}

// Render writes one region.
func Render(w io.Writer, region string, data any) error {
	if err := parsed.ExecuteTemplate(w, region, data); err != nil {
		return fmt.Errorf("render %s: %w", region, err)
	}
	return nil
}

// Has reports whether a region has a template.
func Has(region string) bool { return parsed.Lookup(region) != nil }
