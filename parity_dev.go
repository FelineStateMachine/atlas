//go:build dev

package main

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// runHeadless serves the whole application over plain HTTP and never opens a
// window, when ATLAS_HEADLESS asks for it. The parity harness drives the app
// through a browser either way; a desktop window popping over the reader's
// work was the one part of the sweep that was never needed. The listener's
// address lands in inspector.url exactly where the framework would put it,
// so the harness finds a headless run and a windowed one the same way.
func runHeadless(assets fs.FS) bool {
	if os.Getenv("ATLAS_HEADLESS") == "" {
		return false
	}
	dir := os.Getenv("ATLAS_BUNDLES_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "atlas: headless without a config dir:", err)
			os.Exit(1)
		}
		dir = filepath.Join(base, "dev.felinestatemachine.atlas", "bundles")
	}
	registry := bundle.NewRegistry(dir)
	if err := registry.Rescan(); err != nil {
		slog.Warn("atlas: scanning bundles", "error", err)
	}
	handler := routes(assets, registry)
	// The framework serves the shell's own files under /static; headless
	// answers for them from the same embedded tree.
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	for name, kind := range map[string]string{"app.css": "text/css", "app.js": "text/javascript"} {
		mux.HandleFunc("GET /static/"+name, func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(assets, "assets/"+filepath.Base(r.URL.Path))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", kind+"; charset=utf-8")
			_, _ = w.Write(data)
		})
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "atlas: headless listener:", err)
		os.Exit(1)
	}
	base, err := os.UserConfigDir()
	if err == nil {
		address := "http://" + listener.Addr().String()
		path := filepath.Join(base, "dev.felinestatemachine.atlas", "inspector.url")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			_ = os.WriteFile(path, []byte(address+"\n"), 0o644)
		}
		fmt.Println("atlas: headless at", address)
	}
	if err := http.Serve(listener, mux); err != nil {
		fmt.Fprintln(os.Stderr, "atlas: headless server:", err)
		os.Exit(1)
	}
	return true
}

// parityDir returns the directory that receives behavior-parity tour results.
// It sits beside the app's own data so the harness can find results without
// threading an environment variable through `open`.
func parityDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "dev.felinestatemachine.atlas", "parity")
	return dir, os.MkdirAll(dir, 0o755)
}

// registerParityRoutes accepts tour results from the frontend's parity
// harness in development builds. The frontend runs the tour inside the real
// WKWebView and posts its snapshot log here; comparing two such logs across a
// refactor is the behavior-parity gate.
func registerParityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /parity/result", func(w http.ResponseWriter, r *http.Request) {
		dir, err := parityDir()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		path := filepath.Join(dir, fmt.Sprintf("tour-%d.json", time.Now().UnixMilli()))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, path)
	})
}
