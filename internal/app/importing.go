package app

import (
	"errors"
	"log/slog"
	"net/http"

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

// ImportRow is one line of an import's progress, as the rows region renders it.
type ImportRow struct {
	// State is one of "picking", "installing", "installed", "unchanged",
	// "refused". It is a class name as much as a word: the CSS system styles
	// a row by it.
	State string

	// Detail is what to say: a file name, a volume title, an error.
	Detail string
}

// handleImport runs one import.
func (a *App) handleImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	control := http.NewResponseController(w)

	row := func(state, detail string) {
		a.writeRow(w, ImportRow{State: state, Detail: detail})
		_ = control.Flush()
	}

	row("picking", "choosing a bundle")
	chosen, name, err := a.env.PickFile(r.Context())
	switch {
	case errors.Is(err, hostenv.ErrNoSelection):
		row("unchanged", "nothing was chosen")
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

// writeRow renders one progress row into the response.
func (a *App) writeRow(w http.ResponseWriter, row ImportRow) {
	body, err := renderPartials([]string{"import"}, View{Rows: []ImportRow{row}})
	if err != nil {
		slog.Error("rendering an import row", logging.Op("render"), slog.Any("error", err))
		return
	}
	_, _ = w.Write(body)
}
