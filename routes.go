package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/bundle"
)

// dataTypes names the content types the bundle layout can hold. The framework
// sends nosniff, so every response says what it is or the browser drops it.
var dataTypes = map[string]string{
	".json": "application/json",
	".text": "application/json",
	".bin":  "application/octet-stream",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

func routes(files fs.FS, registry *bundle.Registry) http.Handler {
	mux := http.NewServeMux()
	registerParityRoutes(mux)

	// The catalog is composed from whatever bundles are installed right now,
	// so it is the one response that must never be cached.
	mux.HandleFunc("GET /data/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(registry.Snapshot().Catalog)
	})

	// Game content lives under a stamp taken from the bundle's own version,
	// so every URL names exactly one build of one game: cacheable forever,
	// and gone -- 404, and the frontend refetches the catalog -- the moment
	// a newer bundle takes the slug over.
	mux.HandleFunc("GET /data/g/{slug}/{stamp}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		held, ok := registry.Snapshot().Games[r.PathValue("slug")]
		if !ok || r.PathValue("stamp") != bundle.ShortStamp(held.Manifest.Version.Stamp) {
			http.NotFound(w, r)
			return
		}
		rest := r.PathValue("rest")
		if !strings.HasPrefix(rest, "maps/") && !strings.HasPrefix(rest, "tiles/") &&
			!strings.HasPrefix(rest, "icons/") {
			http.NotFound(w, r)
			return
		}
		dot := strings.LastIndexByte(rest, '.')
		if dot < 0 {
			http.NotFound(w, r)
			return
		}
		kind, ok := dataTypes[rest[dot:]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		entry, size, err := held.OpenEntry(rest)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer entry.Close()
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		_, _ = io.Copy(w, entry)
	})

	// Adding a game from inside the application: a native picker, then the
	// chosen files are validated and copied into the bundles directory, which
	// is the same road a hand-dropped file takes.
	mux.HandleFunc("POST /data/bundles/import", func(w http.ResponseWriter, r *http.Request) {
		paths, ready := pickBundleFiles()
		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"problems": []string{"the application window is not ready"},
			})
			return
		}
		installed, refused := registry.Install(paths)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"imported": installed,
			"problems": refused,
		})
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(files, "assets/index.html")
		if err != nil {
			http.Error(w, "embedded application shell is unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	return mux
}
