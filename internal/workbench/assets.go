package workbench

import (
	"bytes"
	"embed"
	"io/fs"
	"sync"
)

// The workbench's own chrome.
//
// Two files, embedded: the identity's token system and one stylesheet for the
// pages here. tokens.css is the reference implementation's own file, carried as
// an asset rather than as code (issue #5 §9) and identical to the copy the
// application serves -- the workbench is a different surface of one program and
// must not be a different colour. Everything below the tokens is this
// surface's: dense tables, score headlines, and a console pane, which the
// application has no use for.
//
// The hypermedia runtime is deliberately not here. htmx is vendored once, in
// the application's assets, and reaches this handler as bytes through
// [Options.Runtime]: one vendored copy, one licence file, and no import edge
// between two lanes that must not depend on each other. `atlas workbench` --
// which may import both -- is what hands it over.

//go:embed templates/*.tmpl assets/*.css
var files embed.FS

// stylesheets is the cascade, in order: the tokens first, because everything
// after them is spelled in their terms.
var stylesheets = []string{"assets/tokens.css", "assets/workbench.css"}

var (
	sheetOnce sync.Once
	sheet     []byte
)

// stylesheet is the whole cascade as one document, assembled once from bytes
// that are already in the binary.
func stylesheet() []byte {
	sheetOnce.Do(func() {
		var out bytes.Buffer
		for _, name := range stylesheets {
			body, err := fs.ReadFile(files, name)
			if err != nil {
				// Both files are in this package and in the binary; a name
				// that will not read is a build mistake, and skipping it
				// loses styling rather than the page.
				continue
			}
			out.WriteString("/* " + name + " */\n")
			out.Write(body)
			out.WriteString("\n")
		}
		sheet = out.Bytes()
	})
	return sheet
}
