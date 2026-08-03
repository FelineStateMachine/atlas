package app

import (
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// Adding a volume from inside the application: the host puts a picker in front
// of the reader, what they chose is validated and copied into the library, and
// the library is rescanned -- the same road a hand-dropped file takes, except
// that a drop is only noticed at the next launch (issue #5 §2, §4.6).
//
// The response is a stream of rows rather than one answer at the end, because
// a hundred-megabyte bundle takes long enough that silence reads as failure.
// Each row is flushed as it happens.
//
// ONE RUN IS ONE ROW. The stream is a stream of *states of the same row*, not
// a stream of rows: every write in this file renders the whole region with the
// one row that is true right now, and the region is morphed onto itself
// (internal/app/partials.go). An append would make each state a line of its
// own and leave every line on screen for the life of the page, which is what
// this used to do -- a cancelled picker alone left two of them.
//
// The row carries the run's number as its element id, so the morph has an
// identity to work against: within a run the id is the same and the row is
// updated in place, and a *second* run's row is a different element, which is
// what makes a fresh dismissal animation start rather than the finished one
// being inherited. The number is a name on the page and nothing else -- it is
// not application state and nobody remembers it -- so a counter for the
// process is all it takes.
var importRuns atomic.Uint64

// ImportRow is one line of an import's progress, as the rows region renders it.
type ImportRow struct {
	// Run is which import this row belongs to. It is the row's identity on
	// the page: one run's row is one element, morphed from state to state.
	Run uint64

	// State is one of "picking", "installing", "installed", "unchanged",
	// "refused". It is a class name as much as a word: the CSS system styles
	// a row by it.
	State string

	// Detail is what to say: a file name, a volume title, an error.
	Detail string
}

// Fading says whether a row takes itself off the screen. An import that
// finished well is news for a moment and clutter after that, so the good ends
// are marked here and faded out by the stylesheet; a refusal is the opposite,
// and stays until the next import replaces it (it is the only thing left to
// read about what went wrong).
func (r ImportRow) Fading() bool {
	return r.State == "installed" || r.State == "unchanged"
}

// handleImport runs one import.
func (a *App) handleImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	control := http.NewResponseController(w)

	run := importRuns.Add(1)
	row := func(state, detail string) {
		a.writeRegion(w, []ImportRow{{Run: run, State: state, Detail: detail}})
		_ = control.Flush()
	}
	// quiet takes the row away again: the region is rendered with nothing in
	// it, which is how a run that turned out to be nothing at all leaves the
	// screen exactly as it found it.
	quiet := func() {
		a.writeRegion(w, nil)
		_ = control.Flush()
	}

	row("picking", "choosing a bundle")
	chosen, name, err := a.env.PickFile(r.Context())
	switch {
	case errors.Is(err, hostenv.ErrNoSelection):
		// A cancelled picker is silent. The reader closed a dialog they
		// opened; they know what they did, and being told about it is the
		// application talking to itself. The reference implementation said
		// nothing here either.
		quiet()
		return
	case errors.Is(err, hostenv.ErrNotAvailable):
		// The status is set before anything is written for a host that
		// cannot pick at all, which is the one case where the answer is
		// known before the stream starts.
		slog.Warn("import asked of a host with no picker", logging.Op("install"))
		row("refused", "this host has no file picker; drop the bundle into the library instead")
		return
	case err != nil:
		row("refused", err.Error())
		return
	}
	defer chosen.Close()

	row("installing", name)
	installed, err := a.env.Volumes().Install(name, chosen)
	if err != nil {
		slog.Warn("import refused", logging.Op("install"),
			logging.Path(name), slog.Any("error", err))
		row("refused", err.Error())
		return
	}
	if installed.Already {
		row("unchanged", installed.Title+" is already installed at this build")
	} else {
		row("installed", installed.Title)
	}

	// The rescan happened inside the store; what it moved is announced to
	// every open page, which is how a second window learns about an import
	// performed in the first.
	a.announce(installed.Changed)
}

// writeRegion renders the import region, holding the one row that is true now
// -- or nothing, which is a region that is not on screen at all.
func (a *App) writeRegion(w http.ResponseWriter, rows []ImportRow) {
	body, err := renderPartials([]string{"import"}, View{Rows: rows})
	if err != nil {
		slog.Error("rendering an import row", logging.Op("render"), slog.Any("error", err))
		return
	}
	_, _ = w.Write(body)
}
