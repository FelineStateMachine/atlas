// Package workbench is the workbench beside the reader: one http.Handler that
// answers for the collection itself.
//
// Where the application serves the build of every volume a reader should have
// and asks no questions, the workbench answers the questions -- what each build
// is worth, what moved between two of them, what the collection owes the people
// whose work it carries, and what the pipeline should do next. It is pure
// HTMX: server-rendered pages, no seam, no client-side state (issue #5 §5.6).
//
// # What it is made of
//
// It reads two things and links three packages. It reads the registry
// directory, scoring every build it finds with internal/enrich/maturity, and it
// reads nothing else: the source registry arrives as data from whoever mounted
// the handler (sources.go), and pipeline work is done by shelling out to the
// `atlas` CLI through internal/workbench/oprunner, never by linking a lane. The
// import matrix of issue #5 §3.2 -- format plus enrich/maturity, and nothing
// else -- is therefore a property of the design rather than a rule the code has
// to be reminded of.
//
// # The pages
//
//	GET  /                        the library: every volume, headlined by its serving score
//	GET  /volume/{slug}           measurement: the score, its breakdown, the axes, the ledger
//	GET  /volume/{slug}/diff      two builds side by side, headlined by the score delta
//	GET  /sources                 the source registry: licence, attribution, id space
//	GET  /operations              the pipeline, and what it may be pointed at
//	POST /operations/run          one operation, streamed back as rows
//	GET  /assets/{path...}        the stylesheet, and the hypermedia runtime
//
// Every page is measurement first: a score is the headline and everything else
// is diagnostic, because the score is the only number anything gates on
// (issue #5 §5.3, docs/enrich.md).
//
// # Where the operating system is
//
// The handler is not held to the application's hostenv rule -- it is a
// developer's tool on a developer's machine, and it exists to run subprocesses
// -- but the OS still lives at the edges: library.go stats and opens files,
// oprunner starts processes, and everything between them works on values.
package workbench

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
	"github.com/FelineStateMachine/atlas/internal/logging"
	"github.com/FelineStateMachine/atlas/internal/workbench/oprunner"
)

// Options is what a host tells the workbench about itself.
type Options struct {
	// Targets is where the workbench reads and where operations run. Its
	// Registry is the library every measurement page is about.
	Targets Targets
	// Sources is the capture source registry, as data (see sources.go).
	Sources []Source
	// Runtime is the vendored hypermedia runtime served at /assets/htmx.js.
	// It arrives as bytes from the wiring rather than by importing the
	// application, so one vendored copy serves both surfaces and neither
	// lane depends on the other. A nil runtime is a supported configuration:
	// every page here works as plain HTML, and operations still stream --
	// the browser renders the rows itself instead of a swap appending them.
	Runtime []byte
	// Table is the point table scores are read under. The zero value means
	// the embedded one, which is what everything but a test wants.
	Table maturity.Table
}

// Workbench is the handler. It holds the registry it reads, the registry of
// sources it was told about, and the one operation slot.
type Workbench struct {
	library *library
	sources []Source
	targets Targets
	runtime []byte
	runner  *oprunner.Runner

	pages map[string]*template.Template
	rows  *template.Template
	mux   *http.ServeMux
}

// New wires a workbench.
func New(opts Options) (*Workbench, error) {
	table := opts.Table
	if table.Version == 0 {
		held, err := maturity.Points()
		if err != nil {
			return nil, err
		}
		table = held
	}
	pages, rows, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	w := &Workbench{
		library: &library{dir: opts.Targets.Registry, table: table},
		sources: opts.Sources,
		targets: opts.Targets,
		runtime: opts.Runtime,
		runner:  &oprunner.Runner{},
		pages:   pages,
		rows:    rows,
		mux:     http.NewServeMux(),
	}
	w.routes()
	return w, nil
}

// ServeHTTP answers one request.
func (w *Workbench) ServeHTTP(rw http.ResponseWriter, r *http.Request) { w.mux.ServeHTTP(rw, r) }

// routes is the whole URL surface, spelled in one place; nothing registers
// itself.
func (w *Workbench) routes() {
	w.mux.HandleFunc("GET /{$}", w.handleLibrary)
	w.mux.HandleFunc("GET /volume/{slug}", w.handleVolume)
	w.mux.HandleFunc("GET /volume/{slug}/diff", w.handleDiff)
	w.mux.HandleFunc("GET /sources", w.handleSources)
	w.mux.HandleFunc("GET /operations", w.handleOperations)
	w.mux.HandleFunc("POST /operations/run", w.handleRun)
	w.mux.HandleFunc("GET /assets/{path...}", w.handleAsset)
}

// The content security policy every page carries, and it is the strict one:
// nothing loads from anywhere but this server, nothing but this server's own
// forms may be submitted to, and no page may be framed. A workbench that runs
// pipeline operations is exactly the page that must not be reachable through
// somebody else's document.
const contentSecurityPolicy = "default-src 'none'; style-src 'self'; script-src 'self'; " +
	"img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// render executes a page into a buffer first, so a template error becomes a
// clean 500 rather than half a page.
func (w *Workbench) render(rw http.ResponseWriter, page string, data any) {
	held, known := w.pages[page]
	if !known {
		http.Error(rw, "no such page: "+page, http.StatusInternalServerError)
		return
	}
	var body bytes.Buffer
	if err := held.ExecuteTemplate(&body, "layout", data); err != nil {
		slog.Error("rendering a workbench page", logging.Op("workbench"),
			slog.String("page", page), slog.Any("error", err))
		http.Error(rw, "rendering failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	// The library is read whole on every ask, so a page is only ever true of
	// the moment it was asked for.
	rw.Header().Set("Cache-Control", "no-store")
	rw.Write(body.Bytes())
}

// libraryPage is the collection, every volume headlined by what its serving
// build is worth.
type libraryPage struct {
	Dir     string
	Table   int
	Volumes []*volume
	Skipped []string
}

func (w *Workbench) handleLibrary(rw http.ResponseWriter, r *http.Request) {
	volumes, skipped, err := w.library.volumes()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	w.render(rw, "library", libraryPage{
		Dir:     w.library.dir,
		Table:   w.library.table.Version,
		Volumes: volumes,
		Skipped: skipped,
	})
}

// volumePage is one volume measured: the serving build's score and how it moved,
// then every build in full -- its worlds, the five axes as diagnostics, and the
// whole of every ledger its payloads carry.
type volumePage struct {
	Volume   *volume
	Table    int
	Movement *maturity.Comparison
}

func (w *Workbench) handleVolume(rw http.ResponseWriter, r *http.Request) {
	held, _, err := w.library.volumeBySlug(r.PathValue("slug"))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	if held == nil {
		http.NotFound(rw, r)
		return
	}
	w.render(rw, "volume", volumePage{
		Volume:   held,
		Table:    w.library.table.Version,
		Movement: held.Movement(),
	})
}

// diffPage is two builds of one volume, headlined by the score delta.
type diffPage struct {
	Volume *volume
	Diff   *buildDiff
}

// handleDiff compares two builds of one volume, named by file. The packed
// features are read here, on demand, because only a comparison ever needs them
// whole.
func (w *Workbench) handleDiff(rw http.ResponseWriter, r *http.Request) {
	held, _, err := w.library.volumeBySlug(r.PathValue("slug"))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	if held == nil {
		http.NotFound(rw, r)
		return
	}
	from, to := held.Build(r.FormValue("a")), held.Build(r.FormValue("b"))
	if from == nil || to == nil {
		http.Error(rw, "name two builds of this volume", http.StatusBadRequest)
		return
	}
	featuresA, err := features(from)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	featuresB, err := features(to)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	w.render(rw, "diff", diffPage{Volume: held, Diff: diffBuilds(from, to, featuresA, featuresB)})
}

// sourcesPage is the card wall.
type sourcesPage struct {
	Sources []Source
}

func (w *Workbench) handleSources(rw http.ResponseWriter, r *http.Request) {
	w.render(rw, "sources", sourcesPage{Sources: w.sources})
}

// operationsPage is the pipeline: what may be run, what each one needs, and
// what the workbench was pointed at.
type operationsPage struct {
	Targets    Targets
	Sources    []Source
	Operations []operationCard
	Busy       string
}

// operationCard is one operation with its readiness resolved, so the template
// renders a decision rather than making one.
type operationCard struct {
	operation
	Ready   bool
	Missing string
}

func (w *Workbench) handleOperations(rw http.ResponseWriter, r *http.Request) {
	cards := make([]operationCard, 0, len(operations))
	for _, held := range operations {
		ready, missing := held.Ready(w.targets)
		cards = append(cards, operationCard{operation: held, Ready: ready, Missing: missing})
	}
	crawlable := make([]Source, 0, len(w.sources))
	for _, source := range w.sources {
		if source.Crawlable {
			crawlable = append(crawlable, source)
		}
	}
	w.render(rw, "operations", operationsPage{
		Targets:    w.targets,
		Sources:    crawlable,
		Operations: cards,
		Busy:       w.runner.Busy(),
	})
}

// handleRun runs one operation and streams its voice back as rows.
//
// The safety properties are the runner's: a foreign Origin is refused, a second
// operation is refused, and the subprocess dies with the request. What is
// decided here is only what to run -- and every refusal of that is a 400 that
// starts nothing.
func (w *Workbench) handleRun(rw http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	asked := request{
		Operation: r.FormValue("op"),
		Source:    r.FormValue("source"),
		Target:    r.FormValue("target"),
		Volume:    r.FormValue("volume"),
	}
	if asked.Operation == "" {
		http.Error(rw, errNoOperation.Error(), http.StatusBadRequest)
		return
	}
	// The origin check happens before anything is planned as well as inside
	// the runner: a cross-site POST should not even be told which operations
	// exist.
	if err := oprunner.CheckOrigin(r); err != nil {
		http.Error(rw, err.Error(), http.StatusForbidden)
		return
	}
	op, err := plan(w.targets, w.sources, asked)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	slog.Info("running an operation", logging.Op("workbench"),
		slog.String("operation", op.Name), slog.String("command", op.Command()))
	w.runner.Serve(rw, r, op, w.writeRow)
}

// writeRow renders one row of an operation's output. The rows region swaps
// beforeend, so one row is one fragment and the page grows by a line at a time
// -- the same shape the application's import stream takes (issue #5 §4.3).
func (w *Workbench) writeRow(to io.Writer, row oprunner.Row) error {
	return w.rows.ExecuteTemplate(to, "op-row", row)
}

// assetEpoch is the modification time every asset reports: the bytes are
// compiled in (or handed over at startup), so they change exactly when the
// binary does and never while it is running.
var assetEpoch = time.Unix(0, 0).UTC()

// handleAsset serves the workbench's own chrome: the stylesheet, and the
// hypermedia runtime the wiring handed over.
func (w *Workbench) handleAsset(rw http.ResponseWriter, r *http.Request) {
	var body []byte
	var kind string
	switch r.PathValue("path") {
	case "workbench.css":
		body, kind = stylesheet(), "text/css; charset=utf-8"
	case "htmx.js":
		if len(w.runtime) == 0 {
			// A workbench mounted without a runtime is a working workbench;
			// saying so plainly is better than serving an empty script.
			http.NotFound(rw, r)
			return
		}
		body, kind = w.runtime, "text/javascript; charset=utf-8"
	default:
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", kind)
	rw.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(rw, r, r.PathValue("path"), assetEpoch, bytes.NewReader(body))
}

// parseTemplates builds the page set. Each page is the layout plus its own
// file, in its own template namespace, so two pages may both define "content"
// and each stays one file long.
func parseTemplates() (map[string]*template.Template, *template.Template, error) {
	funcs := template.FuncMap{
		// The one display function templates get: a stamp is 64 characters
		// and a page wants the twelve that identify it (format/bundle).
		"short": bundle.ShortStamp,
	}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		held, err := template.New("layout").Funcs(funcs).ParseFS(
			files, "templates/layout.tmpl", "templates/"+name+".tmpl")
		if err != nil {
			return nil, nil, fmt.Errorf("workbench templates: %w", err)
		}
		pages[name] = held
	}
	rows, err := template.New("op-row").Funcs(funcs).ParseFS(files, "templates/op-row.tmpl")
	if err != nil {
		return nil, nil, fmt.Errorf("workbench templates: %w", err)
	}
	return pages, rows, nil
}

// pageNames are the pages, one template file each, named for the page.
var pageNames = []string{"library", "volume", "diff", "sources", "operations"}
