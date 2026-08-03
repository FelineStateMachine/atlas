package app

import (
	"encoding/json"
	"html/template"
	"math"
	"sort"
	"strconv"
)

// The state island.
//
// The explorer page carries the session it was rendered from as an inert JSON
// script node. It exists for one reason: the application's own account of the
// arrangement and the seam's report of the same arrangement must diff cleanly
// against each other -- the joint diagnostics issue #5 §6 asks for, documented
// in docs/app.md §6. These are the session schema's published key names.
//
// Where the arrangement is stored is this application's business: it is a
// file per volume behind hostenv, not a browser's local storage. What the
// island contains is a published contract, which is why the shape below is
// spelled out by hand rather than derived from the Session struct. When the
// two disagree, this file is the one that is wrong. tests/island drives the
// application through its routes over the corpus and holds the island to this
// shape; the e2e suite reads the same island out of a real browser.
//
// Two of the twelve keys cannot be produced here and are carried as the
// server's echo of what the seam last said: center and zoom are the chart's
// camera, arrived at by fitting a raster to a window, and no amount of session
// state computes them. They are null until the seam has reported one
// (docs/app.md §6).

// islandDoc is the whole island: which volume the reader was last in, and the
// record for that volume.
type islandDoc struct {
	Last  string       `json:"last"`
	Entry *islandEntry `json:"entry"`
}

// islandEntry is one volume's arrangement, in the session schema's published
// key names and key order (docs/app.md §6).
type islandEntry struct {
	Volume         string    `json:"volume"`
	World          string    `json:"world"`
	Lens           int       `json:"lens"`
	Center         []float64 `json:"center"`
	Zoom           *float64  `json:"zoom"`
	Hidden         []any     `json:"hidden"`
	Collapsed      []string  `json:"collapsed"`
	Expanded       []any     `json:"expanded"`
	Labels         []string  `json:"labels"`
	OverviewDocked bool      `json:"overviewDocked"`
	DockFolded     bool      `json:"dockFolded"`
	DockDismissed  bool      `json:"dockDismissed"`
}

// island renders the state island's JSON body.
func island(last string, model *worldModel, shown *VolumeView, session Session) template.JS {
	doc := islandDoc{Last: last}
	if shown != nil {
		doc.Entry = islandOf(model, shown, session)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		// Nothing above can fail to marshal. An empty island is a saner
		// face than a panic, and the page still works without it.
		return template.JS(`{"last":"","entry":null}`)
	}
	return template.JS(body)
}

func islandOf(model *worldModel, shown *VolumeView, session Session) *islandEntry {
	entry := &islandEntry{
		Volume:         shown.Slug,
		World:          shown.World.Slug,
		Lens:           shown.LensIndex,
		Hidden:         identifiers(session.Hidden),
		Collapsed:      stringsOf(session.Collapsed),
		Expanded:       identifiers(session.Expanded),
		Labels:         overrideLedger(session.Labels),
		OverviewDocked: session.Overview.Docked,
		DockFolded:     !session.Dock.Open,
		DockDismissed:  session.Dock.Dismissed,
	}
	_ = model
	if camera, held := session.Cameras[shown.World.Slug]; held {
		// Rounded exactly as the seam rounds what it reports, so the
		// island and the seam's report diff without a normalizer in
		// between: whole world units for the centre, three decimals for
		// the zoom (docs/app.md §6).
		entry.Center = []float64{halfUp(camera.X), halfUp(camera.Y)}
		zoom := math.Round(camera.Zoom*1000) / 1000
		entry.Zoom = &zoom
	}
	return entry
}

// halfUp rounds the way JavaScript's Math.round does, because that is how the
// seam rounds its half of the diff: a half goes up, toward positive infinity,
// rather than away from zero. On the y axis -- where every world coordinate is
// negative -- the two conventions disagree on exactly the values a cell
// boundary lands on, which is a whole world unit of difference in a field the
// island and the seam's report must agree on exactly.
func halfUp(value float64) float64 { return math.Floor(value + 0.5) }

// identifiers writes a set of collection ids the way the session schema
// publishes them:
// sorted as the strings they ride the DOM as, and emitted as the numbers the
// payload declares them as. A world whose ids are not numeric keeps its
// strings, because a reader is never refused over the shape of an id.
func identifiers(values []string) []any {
	sorted := sortedIDs(values)
	out := make([]any, 0, len(sorted))
	for _, value := range sorted {
		if number, err := strconv.ParseInt(value, 10, 64); err == nil {
			out = append(out, number)
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringsOf(values []string) []string {
	out := sortedIDs(values)
	if out == nil {
		return []string{}
	}
	return out
}

// overrideLedger is the label-override ledger as the session schema spells
// it: one
// "<collection>=<policy>" per override, sorted, so a record is stable to diff.
func overrideLedger(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for collection, policy := range labels {
		if policy == "" {
			continue
		}
		out = append(out, collection+"="+policy)
	}
	sort.Strings(out)
	return out
}
