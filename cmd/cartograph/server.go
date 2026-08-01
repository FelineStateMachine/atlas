package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed ui
var uiFS embed.FS

// server is the dashboard: read-only views over the library, and the
// operations panel that runs the pipeline's tools on request.
type server struct {
	library *library
	shop    *workshop
	pages   map[string]*template.Template
}

func newServer(lib *library, shop *workshop) *server {
	pages := make(map[string]*template.Template)
	for _, name := range []string{"collection", "game", "diff", "sources"} {
		pages[name] = template.Must(template.ParseFS(uiFS, "ui/layout.tmpl", "ui/"+name+".tmpl"))
	}
	return &server{library: lib, shop: shop, pages: pages}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleCollection)
	mux.HandleFunc("GET /game/{slug}", s.handleGame)
	mux.HandleFunc("GET /game/{slug}/diff", s.handleDiff)
	mux.HandleFunc("GET /sources", s.handleSources)
	mux.HandleFunc("POST /ops/run", s.handleOps)
	mux.HandleFunc("GET /static/style.css", staticFile("ui/style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /static/app.js", staticFile("ui/app.js", "text/javascript; charset=utf-8"))
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

func (s *server) handleSources(w http.ResponseWriter, r *http.Request) {
	s.render(w, "sources", struct {
		Sources []Source
		Repo    string
		Archive string
		Bundles string
	}{Sources: sources, Repo: s.shop.repo, Archive: s.shop.archive, Bundles: s.shop.bundles})
}

// handleOps runs one pipeline tool and streams its voice back as plain text.
// Every operation is explicit: it exists only because this request named it,
// and it dies with the request if the page is abandoned.
func (s *server) handleOps(w http.ResponseWriter, r *http.Request) {
	// A browser sends Origin on any cross-site POST; refusing a foreign one
	// keeps a stray web page from operating the workbench through the
	// visitor's browser.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		http.Error(w, "operations are submitted from the dashboard alone", http.StatusForbidden)
		return
	}
	if s.shop.repo == "" {
		http.Error(w, "no atlas repository found: start cartograph with -repo", http.StatusBadRequest)
		return
	}
	if s.shop.archive == "" {
		http.Error(w, "operations need an archive: start cartograph with -archive", http.StatusBadRequest)
		return
	}

	var argv []string
	var err error
	switch op := r.FormValue("op"); op {
	case "fetch":
		src, ok := sourceBySlug(r.FormValue("source"))
		if !ok {
			http.Error(w, "unknown source "+r.FormValue("source"), http.StatusBadRequest)
			return
		}
		argv, err = fetchArgv(src, r.FormValue("target"), s.shop.archive)
	case "tiles":
		argv = tilesArgv(s.shop.archive)
	case "generate":
		argv, err = generateArgv(s.shop.archive, s.shop.bundles)
	default:
		http.Error(w, "unknown operation "+op, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !s.shop.busy.TryLock() {
		http.Error(w, "an operation is already running", http.StatusConflict)
		return
	}
	defer s.shop.busy.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	out := flushWriter{w}
	fmt.Fprintf(out, "%s $ %s\n\n", s.shop.repo, strings.Join(argv, " "))
	if err := streamCommand(r.Context(), w, s.shop.repo, argv); err != nil {
		fmt.Fprintf(out, "\ncartograph: %v\n", err)
		return
	}
	fmt.Fprintf(out, "\ndone\n")
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
