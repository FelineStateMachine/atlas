package app

import (
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The app plane's pages. Real URLs replace the reference implementation's hash
// routing: a volume and a world are in the address bar, so a page can be
// bookmarked, reloaded, and linked to (issue #5 §4.2).

// handleHome sends a reader to where they were, or explains an empty library.
//
// A redirect rather than a rendered page is the point: the explorer lives at
// exactly one URL per world, and / is a doorway, not a second name for it.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	held := a.library()
	if len(held.order) == 0 {
		a.writePage(w, "empty-state", a.view(held, nil, Session{Schema: SessionSchema}))
		return
	}

	volume, serving := held.bySlug[a.lastVolume()]
	if !serving {
		volume = held.order[0]
	}
	manifest := volume.Manifest()
	session := a.session(manifest.Volume.Slug)
	world := session.World
	if _, ok := worldEntry(manifest, world); !ok {
		world = manifest.Worlds[0].Slug
	}
	http.Redirect(w, r, "/v/"+manifest.Volume.Slug+"/"+world, http.StatusFound)
}

// handleExplorer serves one world of one volume: the whole page, server
// rendered, with every region already in its remembered state.
func (a *App) handleExplorer(w http.ResponseWriter, r *http.Request) {
	slug, world := r.PathValue("volume"), r.PathValue("world")
	if bundle.ValidSlug(slug) != nil || bundle.ValidSlug(world) != nil {
		http.NotFound(w, r)
		return
	}
	held := a.library()
	volume, serving := held.bySlug[slug]
	if !serving {
		http.NotFound(w, r)
		return
	}
	manifest := volume.Manifest()
	if _, ok := worldEntry(manifest, world); !ok {
		http.NotFound(w, r)
		return
	}

	// Arriving at a URL is a choice, whether it was clicked, typed or
	// restored, so the session follows the address bar rather than the other
	// way round. A store that refuses the write costs the reader their place
	// on the next launch and nothing else, so the page is still served.
	session := a.session(slug)
	session.World = world
	session.Stamp = bundle.ShortStamp(manifest.Version.Stamp)
	if err := a.saveSession(&session); err != nil {
		slog.Warn("the session could not be written", logging.Op("session"),
			logging.Volume(slug), slog.Any("error", err))
	}
	if err := a.rememberVolume(slug); err != nil {
		slog.Warn("the last volume could not be remembered", logging.Op("session"),
			logging.Volume(slug), slog.Any("error", err))
	}

	a.writePage(w, "shell", a.view(held, volume, session))
}

// handleDetail answers with one feature's card. The card is a fragment rather
// than a partial set: it is fetched for a target that asked for it, not pushed
// at regions an interaction moved.
func (a *App) handleDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("volume")
	if bundle.ValidSlug(slug) != nil {
		http.Error(w, "the fragment names no volume", http.StatusBadRequest)
		return
	}
	held := a.library()
	volume, serving := held.bySlug[slug]
	if !serving {
		http.NotFound(w, r)
		return
	}
	view := a.view(held, volume, a.session(slug))
	view.Feature = r.PathValue("id")
	a.writePage(w, "detail", view)
}

// handleStatic serves the seam's built bundle and the stylesheet system out of
// whatever tree the host mounted. A host that mounted nothing answers 404: the
// application is required to work without the seam present, which is the
// deletability principle standing up in the build order (issue #5 §3.2).
func (a *App) handleStatic(w http.ResponseWriter, r *http.Request) {
	if a.static == nil {
		http.NotFound(w, r)
		return
	}
	name := path.Clean(r.PathValue("path"))
	if name == "." || name == "/" || strings.HasPrefix(name, "..") {
		http.NotFound(w, r)
		return
	}
	file, err := a.static.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	kind := mime.TypeByExtension(path.Ext(name))
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	if seeker, ok := file.(io.ReadSeeker); ok {
		// http.ServeContent handles conditional requests and ranges for the
		// static tree, which is a plain asset server and carries none of the
		// data plane's byte-compatibility duty.
		http.ServeContent(w, r, name, info.ModTime(), seeker)
		return
	}
	_, _ = io.Copy(w, file)
}
