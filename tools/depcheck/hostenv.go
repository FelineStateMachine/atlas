package main

import (
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// hostenvPurity keeps the application one pure http.Handler. Everything
// OS-shaped sits behind the small interfaces of app/hostenv, which is what
// keeps a third host (a WASM Service Worker PWA) reachable at near-zero cost
// later (issue #5 §3.3).
var hostenvPurity = &analysis.Analyzer{
	Name: "hostenv",
	Doc: `forbid OS and dialog access outside app/hostenv (issue #5 §3.3)

The handler never touches os, file paths, or dialogs directly: it reaches them
through Hostenv's VolumeStore, SessionStore and PickFile. Implementations of
those interfaces — including the Wails host — live under internal/app/hostenv.

The rule is about the handler, so it is scoped to the app lane. The two host
entries, cmd/atlas and the desktop shell at the module root, are where the OS
is supposed to be reached: theirs is the wiring that decides which directories
and which window the handler is mounted over, and they are exempt by being
outside the lane the rule is written about.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runImportRule(hostenvEdge),
}

// hostenvImplementations is the one place OS calls are in contract.
const hostenvImplementations = "internal/app/hostenv"

// osShaped names the standard-library packages that put the handler on a
// particular machine. io/fs is deliberately absent: an fs.FS is the portable
// shape the stores hand out.
var osShaped = map[string]bool{
	"os":            true,
	"os/exec":       true,
	"os/signal":     true,
	"os/user":       true,
	"path/filepath": true,
	"syscall":       true,
}

func hostenvEdge(from Lane, fromRel, importPath string) string {
	if from != LaneApp || under(fromRel, hostenvImplementations) {
		return ""
	}
	switch {
	case osShaped[importPath]:
		return contractf(
			"the application handler must not import "+quote(importPath)+" outside "+hostenvImplementations,
			"3.3",
			"the app is one pure http.Handler: volumes, sessions and the import dialog arrive through the Hostenv interfaces, which is what lets a Wails webview, a headless atlas serve, and a future WASM service worker all mount the same code",
		)
	case dialogShaped(importPath):
		return contractf(
			"the application handler must not import the host toolkit "+quote(importPath)+" outside "+hostenvImplementations,
			"3.3",
			"a native dialog is a Hostenv.PickFile concern; the handler that renders hypermedia must not know which window system it is running under",
		)
	}
	return ""
}

func dialogShaped(importPath string) bool {
	if isStdlib(importPath) {
		return false
	}
	for _, marker := range []string{"dialog", "zenity", "wails", "webview", "systray"} {
		if strings.Contains(strings.ToLower(importPath), marker) {
			return true
		}
	}
	return false
}
