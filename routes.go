package main

import (
	"io/fs"
	"net/http"
)

func routes(files fs.FS) http.Handler {
	mux := http.NewServeMux()
	registerParityRoutes(mux)
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
