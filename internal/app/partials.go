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
//     open <details> survive a re-render (issue #5 §4.3). They are *outer*
//     morphs, and that is not a style choice: a region's template renders the
//     region's own container (§4.1), so the answer to a swap always carries an
//     element with the target's own id. Asking htmx to morph that into the
//     target's *children* asks it to make an element a descendant of itself,
//     which it declines -- silently, leaving the page as it was, which is a
//     legend that never recounts and a dock that never reloses a row. Morphing
//     the element onto itself is the reading that matches what the templates
//     actually render, and it keeps the first-paint bytes and the swap bytes
//     identical, which is the property §4.1 wanted in the first place;
//   - the viewport's state node is swapped whole and the viewport's own
//     internals are never touched -- the seam owns what is inside it, and a
//     swap that reached in would tear down a WebGL context mid-gesture. The
//     custom elements carry hx-morph-skip-children, which is htmx 4's own
//     spelling of that rule, so the guarantee is the runtime's rather than a
//     convention every template has to remember.
//
// The envelope spells its target and swap as hx-target and hx-swap rather
// than as bare target and swap attributes: htmx 4 reads an <hx-partial>
// through the same attribute vocabulary it reads everything else through, and
// an unprefixed target is silently no target at all. The region names and the
// element ids either side of it are unchanged.

// A partialTarget is where one region's HTML goes and how it lands.
type partialTarget struct {
	target string
	swap   string
}

// partialTargets is the region map. A region absent from this table cannot be
// swapped, which is the point: the set of things an interaction may move is
// declared, not discovered.
var partialTargets = map[string]partialTarget{
	// shell and empty-state render a whole document rather than a region's
	// container, so their inside is what lands inside #atlas-shell.
	"shell":          {"#atlas-shell", "innerMorph"},
	"topbar":         {"#atlas-topbar", "innerMorph"},
	"legend":         {"#atlas-legend", "outerMorph"},
	"dock":           {"#atlas-dock", "outerMorph"},
	"detail":         {"#atlas-detail", "outerMorph"},
	"grid-navigator": {"#atlas-grid-navigator", "outerMorph"},
	"overview":       {"#atlas-overview", "outerMorph"},
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
		fmt.Fprintf(&out, "<hx-partial hx-target=%q hx-swap=%q>", where.target, where.swap)
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
