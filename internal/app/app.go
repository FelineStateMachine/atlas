// Package app is the hypermedia application: one http.Handler that serves the
// data plane the seam reads, the server-rendered pages a reader navigates, and
// the partials every interaction answers with.
//
// The handler is pure. It touches no filesystem, builds no path, opens no
// dialog: the library of volumes, the session records, and the file picker all
// arrive through internal/app/hostenv (issue #5 §3.3). That is what lets a
// Wails webview, the headless `atlas serve`, and -- later, at near-zero cost --
// a WASM service worker mount the same code.
//
// Two planes share the mux and answer to different rules.
//
// The **data plane** (/data) is byte-compatible with the implementation this
// one replaces (docs/app.md §2.1; the differences accepted against the
// reference are decision 18). The catalog is composed from whatever is
// installed right now and is never cached; volume content sits under a URL
// carrying the build's stamp, so it may be cached forever and a new build
// arrives at new URLs. app_test.go is the gate on the plane's shape.
//
// The **app plane** is hypermedia: real URLs for volume and world, one POST
// per interaction concern, an SSE stream that says when the library moved, and
// partial responses that name exactly the regions an interaction touched. The
// state-placement rule -- discrete state on the server and in URLs, continuous
// interaction state in the seam -- is documented in docs/app.md and enforced
// by nothing but review.
package app

import (
	"io/fs"
	"net/http"
	"sort"
	"sync"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// App is the application. It is an http.Handler and holds no state of its own
// beyond the wiring: everything a request needs it reads through hostenv, so
// two Apps over one host are the same application seen twice.
type App struct {
	env    hostenv.Hostenv
	static fs.FS
	events *hub
	mux    *http.ServeMux

	// worlds and texts hold what a world costs to stand up: a payload
	// decoded, every location unpacked, every ring projected. That is work
	// worth doing once per build rather than once per keystroke of a
	// search, and the keys carry the build's stamp, so a new build is a new
	// entry and nothing is ever stale. They are memory, not state: two Apps
	// over one host still answer the same, only slower.
	worlds *worldCache
	texts  *textCache

	// writing serializes the read-modify-write of one volume's record. Two
	// interactions can be in flight at once -- a reader holds a key down, a
	// debounced search overlaps a click, the seam reports a camera while a
	// filter is being answered -- and each one reads the record, changes one
	// field and writes the whole thing back. Without this the later write
	// silently discards whatever the earlier one had just decided, which
	// shows up as a control that answered and then un-answered itself.
	writing sync.Mutex
}

// Options is what a host tells the application about itself.
type Options struct {
	// Static is the tree served under /static: the seam's built bundle and
	// the stylesheet system. It is an fs.FS rather than a directory because
	// the desktop host embeds it and a dev host reads it from disk, and the
	// handler must not be able to tell which. A nil tree serves no assets,
	// which is what a build without the seam looks like -- the application
	// is required to work without it (issue #5 §3.2).
	Static fs.FS
}

// New wires the application over a host.
func New(env hostenv.Hostenv, opts Options) *App {
	a := &App{
		env: env, static: opts.Static, events: newHub(), mux: http.NewServeMux(),
		worlds: newWorldCache(), texts: newTextCache(),
	}
	a.routes()
	return a
}

// ServeHTTP answers one request.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

// routes is the whole URL surface, in the order docs/app.md lists it. Every
// route is spelled here; nothing registers itself.
func (a *App) routes() {
	// The data plane, byte-compatible with the reference implementation.
	a.mux.HandleFunc("GET /data/catalog.json", a.handleCatalog)
	a.mux.HandleFunc("GET /data/v/{slug}/{stamp}/{rest...}", a.handleContent)

	// The app plane.
	a.mux.HandleFunc("GET /{$}", a.handleHome)
	a.mux.HandleFunc("GET /open", a.handleOpen)
	a.mux.HandleFunc("GET /v/{volume}/{world}", a.handleExplorer)
	a.mux.HandleFunc("GET /fragments/detail/{id}", a.handleDetail)
	a.mux.HandleFunc("POST /session/{concern}", a.handleSession)
	a.mux.HandleFunc("POST /bundles/import", a.handleImport)
	a.mux.HandleFunc("GET /events", a.handleEvents)
	a.mux.HandleFunc("GET /static/{path...}", a.handleStatic)

	// The application's own chrome: the stylesheet system and the
	// hypermedia runtime, embedded in the binary. It is a separate mount
	// from /static on purpose -- /static is whatever tree a host handed
	// over, and the application is required to work when a host handed over
	// nothing (issue #5 §3.2), which it cannot do if its own chrome lives
	// there.
	a.mux.HandleFunc("GET /assets/{path...}", a.handleAsset)
}

// library is the volumes serving right now, as one request sees them. It is
// taken once per request: the store swaps whole snapshots, so a request that
// looked up a volume and then opened an entry out of it is reading one
// consistent library even if an import landed in between.
//
// The order is the catalog's -- by title -- because that is the order a reader
// meets volumes in, in the selector and in the catalog alike, and "the first
// volume" should mean the same thing everywhere it is said.
type library struct {
	order  []hostenv.Volume
	bySlug map[string]hostenv.Volume
}

func (a *App) library() library {
	volumes := a.env.Volumes().Volumes()
	// The order is this request's to arrange, so it is arranged over a copy:
	// what a store hands out is the store's.
	held := library{
		order:  append(make([]hostenv.Volume, 0, len(volumes)), volumes...),
		bySlug: make(map[string]hostenv.Volume, len(volumes)),
	}
	sort.SliceStable(held.order, func(i, j int) bool {
		return held.order[i].Manifest().Volume.Title < held.order[j].Manifest().Volume.Title
	})
	for _, volume := range volumes {
		held.bySlug[volume.Manifest().Volume.Slug] = volume
	}
	return held
}
