package main

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed ui
var uiFS embed.FS

// server is the dashboard: a handful of read-only views over the library.
type server struct {
	library *library
	pages   map[string]*template.Template
}

func newServer(lib *library) *server {
	pages := make(map[string]*template.Template)
	for _, name := range []string{"collection", "game", "diff"} {
		pages[name] = template.Must(template.ParseFS(uiFS, "ui/layout.tmpl", "ui/"+name+".tmpl"))
	}
	return &server{library: lib, pages: pages}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleCollection)
	mux.HandleFunc("GET /game/{slug}", s.handleGame)
	mux.HandleFunc("GET /game/{slug}/diff", s.handleDiff)
	mux.HandleFunc("GET /static/style.css", staticFile("ui/style.css", "text/css; charset=utf-8"))
	return mux
}

// render executes a page into a buffer first, so a template error becomes a
// clean 500 rather than half a page. Every page carries a policy that keeps
// it self-contained: nothing loads from anywhere but this server.
func (s *server) render(w http.ResponseWriter, page string, data any) {
	var body bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&body, "layout", data); err != nil {
		slog.Error("cartograph: render", "page", page, "error", err)
		http.Error(w, "rendering failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'")
	w.Write(body.Bytes())
}

func (s *server) handleCollection(w http.ResponseWriter, r *http.Request) {
	games, skipped, err := s.library.games()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "collection", struct {
		Dir     string
		Games   []*game
		Skipped []string
	}{Dir: s.library.dir, Games: games, Skipped: skipped})
}

func (s *server) handleGame(w http.ResponseWriter, r *http.Request) {
	held, err := s.library.gameBySlug(r.PathValue("slug"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if held == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "game", struct{ Game *game }{Game: held})
}

// handleDiff compares two builds of one game, named by file. The packed pins
// are read here, on demand, because only a comparison ever needs them whole.
func (s *server) handleDiff(w http.ResponseWriter, r *http.Request) {
	held, err := s.library.gameBySlug(r.PathValue("slug"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if held == nil {
		http.NotFound(w, r)
		return
	}
	from, to := held.Build(r.FormValue("a")), held.Build(r.FormValue("b"))
	if from == nil || to == nil {
		http.Error(w, "pick two builds of this game", http.StatusBadRequest)
		return
	}
	pinsA, err := loadPins(from.Path, from.MapSlugs)
	if err == nil {
		var pinsB map[string]map[int64]string
		if pinsB, err = loadPins(to.Path, to.MapSlugs); err == nil {
			s.render(w, "diff", struct {
				Game *game
				Diff *buildDiff
			}{Game: held, Diff: diffBuilds(from, to, pinsA, pinsB)})
			return
		}
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func staticFile(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := uiFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}
