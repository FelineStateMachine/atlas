package app

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// Live updates (issue #5 §4.6). The registry is scanned at launch and rescanned
// on import; what a rescan changed is announced here as ready-to-swap
// partials, so a page that is open when a bundle arrives updates itself
// without a poll and without the reader doing anything.
//
// The envelope, which is contract and is stable ahead of the templates:
//
//	event: catalog
//	data: <hx-partial hx-target="#atlas-topbar" hx-swap="innerMorph">…</hx-partial>
//
//	event: refresh
//	data: <hx-partial hx-target="#atlas-shell" hx-swap="innerMorph" hx-get="/v/tunic/world"></hx-partial>
//
// Two event names and no more. `catalog` says the library's composition moved
// and carries the regions that list it. `refresh` is the one directive: the
// volume this connection is watching now serves a different build, every URL
// under its old stamp is gone, and the page has to be re-fetched whole rather
// than patched. A connection says which volume it is watching as a query
// parameter, because the server cannot otherwise know which of several open
// pages a stamp move concerns.
//
// There is no event for "a file appeared in the library directory": nothing
// watches the directory, by decision (issue #5 §2). A drop from outside
// appears at the next launch.

// eventNames are the two events the stream carries.
const (
	eventCatalog = "catalog"
	eventRefresh = "refresh"
)

// message is one server-sent event.
type message struct {
	name string
	body []byte

	// volume, when set, aims the message at connections watching one volume.
	// The refresh directive is the only message that uses it.
	volume string
}

// hub fans messages out to the open event streams. It is small on purpose:
// one process, a handful of connections, no history, no replay. A subscriber
// that cannot keep up is dropped rather than allowed to hold the publisher --
// what it missed is a catalog change, and the next one carries the whole
// state anyway.
type hub struct {
	mu   sync.Mutex
	subs map[chan message]struct{}
}

func newHub() *hub { return &hub{subs: map[chan message]struct{}{}} }

func (h *hub) subscribe() chan message {
	stream := make(chan message, 8)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[stream] = struct{}{}
	return stream
}

func (h *hub) unsubscribe(stream chan message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, stream)
	close(stream)
}

func (h *hub) publish(m message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for stream := range h.subs {
		select {
		case stream <- m:
		default:
			slog.Warn("an event stream fell behind and lost a message",
				logging.Op("events"), slog.String("event", m.name))
		}
	}
}

// subscribers reports how many streams are open. It is here for the tests and
// for a diagnostics readout; nothing in the application branches on it.
func (h *hub) subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// announce turns what a rescan changed into the messages the stream carries.
// It is called by the import flow and by anything else that moves the library.
func (a *App) announce(changed []string) {
	if len(changed) == 0 {
		return
	}
	held := a.library()
	// The topbar carries the volume selector. An emptied library is not a
	// selector with nothing in it, it is a different page, so the shell goes
	// with the event.
	regions := []string{"topbar"}
	if len(held.order) == 0 {
		regions = append(regions, "empty-state")
	}
	body, err := renderPartials(regions, a.view(held, nil, Session{Schema: SessionSchema}))
	if err != nil {
		slog.Error("rendering a catalog event", logging.Op("events"), slog.Any("error", err))
		return
	}
	a.events.publish(message{name: eventCatalog, body: body})

	// A volume whose serving build moved cannot be patched: every URL under
	// the old stamp is gone. The page is told to re-fetch itself.
	for _, slug := range changed {
		volume, serving := held.bySlug[slug]
		if !serving {
			continue
		}
		manifest := volume.Manifest()
		world := a.session(slug).World
		if _, ok := worldEntry(manifest, world); !ok {
			world = manifest.Worlds[0].Slug
		}
		where := partialTargets["shell"]
		directive := []byte("<hx-partial hx-target=\"" + where.target + "\" hx-swap=\"" + where.swap +
			"\" hx-get=\"/v/" + slug + "/" + world + "\"></hx-partial>")
		a.events.publish(message{name: eventRefresh, body: directive, volume: slug})
	}
	slog.Info("library changed", logging.Op("events"),
		slog.Int("volumes", len(changed)), slog.Any("changed", changed))
}

// handleEvents holds one SSE connection open.
//
// The connection names the volume it is watching, so a stamp move is announced
// only to the pages it concerns; a connection that names nothing hears the
// catalog events and no refresh directives.
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	watching := r.URL.Query().Get("volume")
	if watching != "" && bundle.ValidSlug(watching) != nil {
		http.Error(w, "that is not a volume", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	control := http.NewResponseController(w)

	stream := a.events.subscribe()
	defer a.events.unsubscribe(stream)

	// A comment as soon as the subscription exists. It carries nothing, and
	// it is the only way a client -- or a test -- can know the connection is
	// live rather than merely accepted.
	if _, err := io.WriteString(w, ": atlas\n\n"); err != nil {
		return
	}
	if err := control.Flush(); err != nil {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-stream:
			if m.volume != "" && m.volume != watching {
				continue
			}
			if _, err := w.Write(encodeEvent(m)); err != nil {
				return
			}
			if err := control.Flush(); err != nil {
				return
			}
		}
	}
}

// encodeEvent writes one message in the wire format: a name, then the body one
// data line per line of HTML, then a blank line. HTML is written as it is --
// the line splitting is the protocol's, not an escape.
func encodeEvent(m message) []byte {
	var out bytes.Buffer
	out.WriteString("event: ")
	out.WriteString(m.name)
	out.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(string(m.body), "\n"), "\n") {
		out.WriteString("data: ")
		out.WriteString(line)
		out.WriteString("\n")
	}
	out.WriteString("\n")
	return out.Bytes()
}
