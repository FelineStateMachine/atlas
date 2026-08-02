package app

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/FelineStateMachine/atlas/internal/app/templates"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The partial envelope. Every interaction answers with an <hx-partial> set
// covering exactly the regions it touched (issue #5 §4.2) -- a legend row, a
// solo chip, a dock readout and the viewport's state node in one response,
// rather than a page-wide refresh standing in for knowing what moved.
//
// The envelope is what the handler, the templates and the seam agree on, and
// it is stable ahead of the templates that will fill it: a region has one
// element id and one swap, named here and nowhere else. Two swaps are
// deliberate and load-bearing:
//
//   - morph swaps for legend, dock and detail, so scroll position, focus and
//     open <details> survive a re-render (issue #5 §4.3);
//   - the viewport's state node is swapped whole and the viewport's own
//     internals are never touched -- the seam owns what is inside it, and a
//     swap that reached in would tear down a WebGL context mid-gesture.

// A partialTarget is where one region's HTML goes and how it lands.
type partialTarget struct {
	target string
	swap   string
}

// partialTargets is the region map. A region absent from this table cannot be
// swapped, which is the point: the set of things an interaction may move is
// declared, not discovered.
var partialTargets = map[string]partialTarget{
	"shell":          {"#atlas-shell", "innerMorph"},
	"topbar":         {"#atlas-topbar", "innerMorph"},
	"legend":         {"#atlas-legend", "innerMorph"},
	"dock":           {"#atlas-dock", "innerMorph"},
	"detail":         {"#atlas-detail", "innerMorph"},
	"grid-navigator": {"#atlas-grid-navigator", "innerMorph"},
	"overview":       {"#atlas-overview", "innerMorph"},
	"viewport":       {"#atlas-viewport-state", "outerMorph"},
	"empty-state":    {"#atlas-shell", "innerMorph"},
	"import":         {"#atlas-import", "beforeend"},
}

// renderPartials builds the <hx-partial> set for a list of regions.
func renderPartials(regions []string, data any) ([]byte, error) {
	var out bytes.Buffer
	for _, region := range regions {
		where, known := partialTargets[region]
		if !known {
			return nil, fmt.Errorf("region %q has no partial target", region)
		}
		fmt.Fprintf(&out, "<hx-partial target=%q swap=%q>", where.target, where.swap)
		if err := templates.Render(&out, region, data); err != nil {
			return nil, err
		}
		out.WriteString("</hx-partial>\n")
	}
	return out.Bytes(), nil
}

// writePartials answers a request with a partial set. A template that fails to
// render is a programming error the reader cannot act on, so it is logged and
// answered as one rather than pasted into the page.
func (a *App) writePartials(w http.ResponseWriter, regions []string, data any) {
	body, err := renderPartials(regions, data)
	if err != nil {
		slog.Error("rendering partials", logging.Op("render"), slog.Any("error", err))
		http.Error(w, "the response could not be rendered", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// writePage answers with a whole page.
func (a *App) writePage(w http.ResponseWriter, region string, data any) {
	var out bytes.Buffer
	if err := templates.Render(&out, region, data); err != nil {
		slog.Error("rendering a page", logging.Op("render"),
			slog.String("region", region), slog.Any("error", err))
		http.Error(w, "the page could not be rendered", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(out.Bytes())
}
