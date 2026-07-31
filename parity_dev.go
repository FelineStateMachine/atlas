//go:build dev

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

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
