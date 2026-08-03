package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// The handler is tested through a host that is entirely in memory, which is
// the point of hostenv: nothing here needs a directory, a bundle file, or a
// window, so what these tests exercise is the handler and not the machine.

type fakeVolume struct {
	manifest bundle.Manifest
	entries  map[string][]byte
}

func (v *fakeVolume) Manifest() bundle.Manifest { return v.manifest }

func (v *fakeVolume) Open(name string) (io.ReadCloser, int64, error) {
	held, ok := v.entries[name]
	if !ok {
		return nil, 0, errors.New("no such entry")
	}
	return io.NopCloser(bytes.NewReader(held)), int64(len(held)), nil
}

type fakeVolumes struct {
	volumes  []hostenv.Volume
	location string

	// arriving is what the next Install adds, and installs is what it was
	// asked to take in. already makes it the successful import that copied
	// nothing, because the library was holding this exact build already.
	arriving hostenv.Volume
	installs [][]byte
	refuse   error
	already  bool
}

func (s *fakeVolumes) Volumes() []hostenv.Volume { return s.volumes }
func (s *fakeVolumes) Location() string          { return s.location }

func (s *fakeVolumes) Rescan() ([]string, error) { return nil, nil }

func (s *fakeVolumes) Install(name string, content io.Reader) (hostenv.Installed, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return hostenv.Installed{}, err
	}
	s.installs = append(s.installs, data)
	if s.refuse != nil {
		return hostenv.Installed{}, s.refuse
	}
	if s.arriving == nil {
		return hostenv.Installed{}, errors.New("nothing to install")
	}
	manifest := s.arriving.Manifest()
	s.volumes = append(s.volumes, s.arriving)
	return hostenv.Installed{
		Slug:    manifest.Volume.Slug,
		Title:   manifest.Volume.Title,
		Stamp:   manifest.Version.Stamp,
		Already: s.already,
		Changed: []string{manifest.Volume.Slug},
	}, nil
}

type fakeHost struct {
	volumes  *fakeVolumes
	sessions hostenv.SessionStore
	pick     func(ctx context.Context) (io.ReadCloser, string, error)
}

func (h *fakeHost) Volumes() hostenv.VolumeStore   { return h.volumes }
func (h *fakeHost) Sessions() hostenv.SessionStore { return h.sessions }

func (h *fakeHost) PickFile(ctx context.Context) (io.ReadCloser, string, error) {
	if h.pick == nil {
		return nil, "", hostenv.ErrNotAvailable
	}
	return h.pick(ctx)
}

// volume builds one serving volume with the entries a data-plane test asks
// for.
func volume(slug, title, stamp string, worlds ...bundle.WorldEntry) *fakeVolume {
	if len(worlds) == 0 {
		worlds = []bundle.WorldEntry{{Slug: "overworld", Title: "Overworld", Points: 3, UpdatedAt: "2026-01-01T00:00:00Z"}}
	}
	return &fakeVolume{
		manifest: bundle.Manifest{
			Format:        bundle.Format,
			FormatVersion: bundle.FormatVersion,
			Conventions:   2,
			Volume:        bundle.Volume{Slug: slug, Title: title},
			Version:       bundle.Version{Stamp: stamp, CreatedAt: "2026-01-01T00:00:00Z"},
			TileGrid:      bundle.TileGrid{SourceZoom: 13, FirstTile: 4064, TileSize: 256, Size: 8192},
			Worlds:        worlds,
		},
		entries: map[string][]byte{
			"worlds/overworld.json":     []byte(`{"lenses":[],"collections":[]}`),
			"worlds/overworld.bin":      []byte("ATLASLOC"),
			"worlds/overworld.text":     []byte(`{}`),
			"icons/marker.svg":          []byte("<svg/>"),
			"tiles/overworld/0/0/0.jpg": []byte("raster"),
		},
	}
}

const (
	tunicStamp = "13d5657ed9038808e5fe12ef44e769b40297e9d720e53376f430f224128f2dfc"
	marsStamp  = "68e141f26b1a8808e5fe12ef44e769b40297e9d720e53376f430f224128f2dfc"
)

func newApp(t *testing.T, volumes ...hostenv.Volume) (*app.App, *fakeHost) {
	t.Helper()
	host := &fakeHost{
		volumes:  &fakeVolumes{volumes: volumes, location: "/library"},
		sessions: hostenv.NewMemorySessions(),
	}
	return app.New(host, app.Options{}), host
}

func get(t *testing.T, handler http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func post(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCatalogComposition(t *testing.T) {
	handler, _ := newApp(t,
		volume("tunic", "TUNIC", tunicStamp),
		volume("mars", "Mars", marsStamp))

	got := get(t, handler, "/data/catalog.json", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("catalog answered %d", got.Code)
	}
	if cache := got.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("catalog Cache-Control = %q, want no-store: it is the one response whose job is to be current", cache)
	}
	if kind := got.Header().Get("Content-Type"); kind != "application/json" {
		t.Errorf("catalog Content-Type = %q", kind)
	}

	var catalog struct {
		Volumes []struct {
			Slug   string `json:"slug"`
			Title  string `json:"title"`
			Stamp  string `json:"stamp"`
			Base   string `json:"base"`
			Worlds []struct {
				Slug string `json:"slug"`
			} `json:"worlds"`
		} `json:"volumes"`
		BundlesDir string `json:"bundlesDir"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Volumes) != 2 {
		t.Fatalf("catalog lists %d volumes", len(catalog.Volumes))
	}
	// Volumes are listed by title, which is the order a reader meets them in.
	if catalog.Volumes[0].Slug != "mars" || catalog.Volumes[1].Slug != "tunic" {
		t.Errorf("catalog order is %s, %s; want by title",
			catalog.Volumes[0].Slug, catalog.Volumes[1].Slug)
	}
	if want := "/data/v/tunic/" + bundle.ShortStamp(tunicStamp); catalog.Volumes[1].Base != want {
		t.Errorf("base = %q, want %q", catalog.Volumes[1].Base, want)
	}
	if catalog.BundlesDir != "/library" {
		t.Errorf("bundlesDir = %q, want the store's own location", catalog.BundlesDir)
	}
}

func TestContentPlane(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	base := "/data/v/tunic/" + bundle.ShortStamp(tunicStamp)

	served := []struct {
		name string
		path string
		kind string
		size int
	}{
		{"a world payload", base + "/worlds/overworld.json", "application/json", 30},
		{"the packed locations", base + "/worlds/overworld.bin", "application/octet-stream", 8},
		{"the deferred prose", base + "/worlds/overworld.text", "application/json", 2},
		{"an icon", base + "/icons/marker.svg", "image/svg+xml", 6},
		{"a tile", base + "/tiles/overworld/0/0/0.jpg", "image/jpeg", 6},
	}
	for _, tt := range served {
		t.Run(tt.name, func(t *testing.T) {
			got := get(t, handler, tt.path, nil)
			if got.Code != http.StatusOK {
				t.Fatalf("%s answered %d", tt.path, got.Code)
			}
			if kind := got.Header().Get("Content-Type"); kind != tt.kind {
				t.Errorf("Content-Type = %q, want %q", kind, tt.kind)
			}
			if got.Body.Len() != tt.size {
				t.Errorf("body is %d bytes, want %d", got.Body.Len(), tt.size)
			}
			if cache := got.Header().Get("Cache-Control"); cache != "private, max-age=31536000, immutable" {
				t.Errorf("Cache-Control = %q: a stamped URL names one build forever", cache)
			}
		})
	}

	refused := []struct {
		name string
		path string
	}{
		{"a stamp that is not the serving build", "/data/v/tunic/000000000000/worlds/overworld.json"},
		{"a volume that is not installed", "/data/v/not-a-volume/" + bundle.ShortStamp(tunicStamp) + "/worlds/overworld.json"},
		{"an entry outside worlds, tiles and icons", base + "/atlas.json"},
		{"an extension the plane names no type for", base + "/worlds/overworld.txt"},
		{"a world the bundle does not hold", base + "/worlds/not-a-world.json"},
		{"a path with no extension at all", base + "/worlds/overworld"},
		{"a path under the shell that is not a page", "/not-a-page"},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			got := get(t, handler, tt.path, nil)
			if got.Code != http.StatusNotFound {
				t.Errorf("%s answered %d, want 404", tt.path, got.Code)
			}
		})
	}
}

// The reference implementation sets a length and copies: a Range request is
// answered whole. Tiles are stored uncompressed so that ranges *could* be
// served, and the golden transcript records that they are not; this test is
// what would go red if somebody implemented them without a waiver.
func TestContentDoesNotServeRanges(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	path := "/data/v/tunic/" + bundle.ShortStamp(tunicStamp) + "/tiles/overworld/0/0/0.jpg"

	got := get(t, handler, path, map[string]string{"Range": "bytes=0-2"})
	if got.Code != http.StatusOK {
		t.Fatalf("a range request answered %d, want 200 with the whole body", got.Code)
	}
	if got.Body.String() != "raster" {
		t.Errorf("body = %q, want the whole entry", got.Body.String())
	}
	if accept := got.Header().Get("Accept-Ranges"); accept != "" {
		t.Errorf("Accept-Ranges = %q, want the plane to make no such offer", accept)
	}
}

func TestHomeSendsAReaderBackWhereTheyWere(t *testing.T) {
	handler, host := newApp(t,
		volume("tunic", "TUNIC", tunicStamp),
		volume("mars", "Mars", marsStamp))

	// Nobody has been anywhere yet: the first volume in catalog order stands
	// in for "where you were".
	got := get(t, handler, "/", nil)
	if got.Code != http.StatusFound {
		t.Fatalf("/ answered %d, want a redirect to a world", got.Code)
	}
	if where := got.Header().Get("Location"); where != "/v/mars/overworld" {
		t.Errorf("/ sent the reader to %q", where)
	}

	// Visiting a world remembers it, and / follows.
	if page := get(t, handler, "/v/tunic/overworld", nil); page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	got = get(t, handler, "/", nil)
	if where := got.Header().Get("Location"); where != "/v/tunic/overworld" {
		t.Errorf("after visiting tunic, / sent the reader to %q", where)
	}
	if _, err := host.sessions.Load("volume.tunic.json"); err != nil {
		t.Errorf("visiting a world wrote no session record: %v", err)
	}
}

func TestHomeWithNothingInstalled(t *testing.T) {
	handler, _ := newApp(t)
	got := get(t, handler, "/", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("an empty library answered %d, want the library card", got.Code)
	}
	if !strings.Contains(got.Body.String(), "/library") {
		t.Error("the empty state does not say where a bundle goes")
	}
}

func TestExplorerRefusesWhatIsNotThere(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	for _, path := range []string{"/v/not-a-volume/overworld", "/v/tunic/not-a-world"} {
		if got := get(t, handler, path, nil); got.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, got.Code)
		}
	}
}

// One route per concern, each answering with the regions it touched. The
// table is the contract: a concern that starts moving a different set of
// regions has changed the page's behavior and should say so here first.
func TestSessionConcerns(t *testing.T) {
	tests := []struct {
		concern string
		form    url.Values
		targets []string
		check   func(t *testing.T, s app.Session)
	}{
		{
			concern: "world",
			form:    url.Values{"world": {"overworld"}},
			targets: []string{"#atlas-topbar", "#atlas-legend", "#atlas-dock", "#atlas-overview", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if s.World != "overworld" {
					t.Errorf("world = %q", s.World)
				}
			},
		},
		{
			concern: "lens",
			form:    url.Values{"lens": {"satellite"}},
			// The legend and the dock are in the set because a lens can be a
			// different *layer* of a split sheet, and the ground under the
			// reader changes with it: which shapes the index lists and which
			// features the panel can name are the new layer's answers.
			targets: []string{
				"#atlas-topbar", "#atlas-legend", "#atlas-dock", "#atlas-overview",
				"#atlas-viewport-state",
			},
			check: func(t *testing.T, s app.Session) {
				if s.Lens != "satellite" {
					t.Errorf("lens = %q", s.Lens)
				}
			},
		},
		{
			concern: "collections",
			form:    url.Values{"collection": {"7"}, "visible": {"0"}},
			targets: []string{"#atlas-legend", "#atlas-dock", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if len(s.Hidden) != 1 || s.Hidden[0] != "7" {
					t.Errorf("hidden = %v", s.Hidden)
				}
			},
		},
		{
			concern: "sections",
			form:    url.Values{"section": {"quests"}, "open": {"0"}},
			targets: []string{"#atlas-legend"},
			check: func(t *testing.T, s app.Session) {
				// A fresh record is arranged before the concern runs, and
				// the arrangement folds the viewer's own Zones section, so
				// what this asserts is that the named section joined it.
				if !contains(s.Collapsed, "quests") {
					t.Errorf("collapsed = %v", s.Collapsed)
				}
			},
		},
		{
			concern: "labels",
			form:    url.Values{"collection": {"7"}, "policy": {"always"}},
			targets: []string{"#atlas-legend", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if s.Labels["7"] != "always" {
					t.Errorf("labels = %v", s.Labels)
				}
			},
		},
		{
			// Isolating is a move on the hide set rather than a state of
			// its own, and the chip is derived from what is hidden. With
			// nothing named, the chip's own way out -- show everything --
			// is what the route means.
			concern: "solo",
			form:    url.Values{},
			targets: []string{"#atlas-legend", "#atlas-dock", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if len(s.Hidden) != 0 {
					t.Errorf("hidden = %v", s.Hidden)
				}
			},
		},
		{
			concern: "expand",
			form:    url.Values{"collection": {"7"}, "open": {"1"}},
			targets: []string{"#atlas-legend"},
			check: func(t *testing.T, s app.Session) {
				if len(s.Expanded) != 1 || s.Expanded[0] != "7" {
					t.Errorf("expanded = %v", s.Expanded)
				}
			},
		},
		{
			concern: "highlight",
			form:    url.Values{"feature": {"31"}},
			targets: []string{"#atlas-legend", "#atlas-dock", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if len(s.Highlighted) != 1 || s.Highlighted[0] != "31" {
					t.Errorf("highlighted = %v", s.Highlighted)
				}
			},
		},
		{
			concern: "overview",
			form:    url.Values{"docked": {"1"}},
			targets: []string{"#atlas-overview"},
			check: func(t *testing.T, s app.Session) {
				if !s.Overview.Docked {
					t.Error("the overview did not dock")
				}
			},
		},
		{
			concern: "search",
			form:    url.Values{"q": {"shrine"}},
			targets: []string{"#atlas-legend", "#atlas-dock", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if s.Search != "shrine" {
					t.Errorf("search = %q", s.Search)
				}
			},
		},
		{
			concern: "dock",
			form:    url.Values{"open": {"1"}, "section": {"counts"}},
			targets: []string{"#atlas-dock"},
			check: func(t *testing.T, s app.Session) {
				if !s.Dock.Open || s.Dock.Section != "counts" {
					t.Errorf("dock = %+v", s.Dock)
				}
			},
		},
		{
			concern: "select",
			form:    url.Values{"feature": {"1849"}},
			targets: []string{"#atlas-legend", "#atlas-dock", "#atlas-detail", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if s.Selected != "1849" || !s.Detail.Open {
					t.Errorf("selection = %q, detail = %+v", s.Selected, s.Detail)
				}
			},
		},
		{
			concern: "grid",
			// The field posts what the reader typed and the record holds an
			// address: four characters is past the geohash telescope's floor,
			// so what lands is the three-character cell the navigator would
			// have shown (internal/app/grid.go, normalizeCell).
			form:    url.Values{"system": {"geohash"}, "cell": {"9q5c"}, "subgrid": {"2"}},
			targets: []string{"#atlas-grid-navigator", "#atlas-dock", "#atlas-viewport-state"},
			check: func(t *testing.T, s app.Session) {
				if s.Grid.System != "geohash" || s.Grid.Cell != "9q5" || s.Grid.Subgrid != 2 {
					t.Errorf("grid = %+v", s.Grid)
				}
			},
		},
		{
			concern: "sidebar",
			form:    url.Values{"open": {"1"}},
			targets: []string{"#atlas-shell"},
			check: func(t *testing.T, s app.Session) {
				if s.Sidebar.Collapsed {
					t.Error("the sidebar did not open")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.concern, func(t *testing.T) {
			handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
			form := url.Values{"volume": {"tunic"}}
			for name, values := range tt.form {
				form[name] = values
			}
			got := post(t, handler, "/session/"+tt.concern, form)
			if got.Code != http.StatusOK {
				t.Fatalf("/session/%s answered %d: %s", tt.concern, got.Code, got.Body)
			}
			if kind := got.Header().Get("Content-Type"); kind != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", kind)
			}
			body := got.Body.String()
			for _, target := range tt.targets {
				if !strings.Contains(body, `target="`+target+`"`) {
					t.Errorf("the answer carries no partial for %s:\n%s", target, body)
				}
			}
			// The regions the concern touches, and the island. Every answer
			// carries the island because every answer has just written the
			// record it publishes; it is inert and it moves nothing on
			// screen, so it is not one of the regions an interaction is
			// judged by, and it is not in the table above.
			if !strings.Contains(body, `target="#atlas-session-island"`) {
				t.Errorf("the answer carries no state island:\n%s", body)
			}
			if swaps := strings.Count(body, "<hx-partial"); swaps != len(tt.targets)+1 {
				t.Errorf("the answer carries %d partials, want the %d regions the concern touches and the island",
					swaps, len(tt.targets))
			}

			held, err := host.sessions.Load("volume.tunic.json")
			if err != nil {
				t.Fatalf("no session was written: %v", err)
			}
			var session app.Session
			if err := json.Unmarshal(held, &session); err != nil {
				t.Fatal(err)
			}
			if session.Schema != app.SessionSchema {
				t.Errorf("record schema = %d", session.Schema)
			}
			tt.check(t, session)
		})
	}
}

// The blunt reset, which is ⌘R's whole job: this volume's record is deleted
// and the reader is put back into the volume with nothing remembered about it.
//
// It exists because a record that has gone wrong is not a thing anybody wants
// to diagnose one field at a time -- highlights culled at every pin, a filter
// with no row to press -- and the state a volume opens with is synthesized
// from the world itself, so throwing the record away *is* the way back.
//
// Two things this test holds that are easy to lose. The answer is a full round
// trip rather than a partial set, because after a reset there is no record for
// a region to be rendered from; and the last-volume pointer survives, because
// the reader is not leaving the volume, they are standing in it with a clean
// record.
func TestSessionResetForgetsTheVolumeAndComesBack(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))

	// A reader who has been somewhere: the page opened, a collection put away,
	// a feature highlighted, a search typed, a grid opened and a camera
	// settled. Every one of these is a field the reset has to take with it.
	if page := get(t, handler, "/v/tunic/overworld", nil); page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	for _, step := range []struct {
		concern string
		form    url.Values
	}{
		{"collections", url.Values{"collection": {"7"}, "visible": {"0"}}},
		{"highlight", url.Values{"feature": {"1849"}}},
		{"search", url.Values{"q": {"mill"}}},
		{"grid", url.Values{"system": {"toggle"}}},
		{"view", url.Values{"world": {"overworld"}, "x": {"120.5"}, "y": {"-40"}, "zoom": {"6.25"}}},
	} {
		form := url.Values{"volume": {"tunic"}}
		for name, values := range step.form {
			form[name] = values
		}
		if got := post(t, handler, "/session/"+step.concern, form); got.Code != http.StatusOK {
			t.Fatalf("/session/%s answered %d: %s", step.concern, got.Code, got.Body)
		}
	}
	before := sessionRecord(t, host, "volume.tunic.json")
	if len(before.Hidden) == 0 || len(before.Highlighted) == 0 || before.Search == "" ||
		before.Grid.System == "" || len(before.Cameras) == 0 {
		t.Fatalf("the record under test was never arranged: %+v", before)
	}

	got := post(t, handler, "/session/reset", url.Values{"volume": {"tunic"}})
	if got.Code != http.StatusNoContent {
		t.Fatalf("the reset answered %d: %s", got.Code, got.Body)
	}
	// The full refresh, as the header htmx acts on itself. A partial set would
	// be rendered out of a record that no longer exists, and a 303 would be
	// followed by fetch and swapped into a page that asked for no swap.
	if back := got.Header().Get("HX-Redirect"); back != "/v/tunic/overworld" {
		t.Errorf("the reset answers HX-Redirect %q, want the volume's own world back", back)
	}
	if got.Body.Len() != 0 {
		t.Errorf("the reset answers with a body:\n%s", got.Body)
	}
	if strings.Contains(got.Body.String(), "<hx-partial") {
		t.Errorf("the reset answers with partials over a record it just deleted:\n%s", got.Body)
	}

	// The record is gone from the store, which is the whole of the reset.
	if _, err := host.sessions.Load("volume.tunic.json"); !errors.Is(err, hostenv.ErrNoSession) {
		t.Errorf("the volume's record survived the reset: %v", err)
	}
	// And the pointer did not go with it: the reader is still in this volume.
	if _, err := host.sessions.Load("app.json"); err != nil {
		t.Errorf("the reset forgot which volume the reader is in: %v", err)
	}

	// What the redirect lands on: a page synthesized out of the world's own
	// arrangement. The island is the server's account of it, and it says the
	// three things the reader lost their hour to -- nothing hidden, nothing
	// highlighted, and a camera nobody has reported, which is the chart
	// fitting the world again.
	page := get(t, handler, "/v/tunic/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the page the reset comes back to answered %d", page.Code)
	}
	for _, want := range []string{`"hidden":[]`, `"center":null`, `"zoom":null`, `"dockFolded":true`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the island after a reset does not say %s:\n%s", want, page.Body)
		}
	}
	after := sessionRecord(t, host, "volume.tunic.json")
	if len(after.Hidden) != 0 || len(after.Highlighted) != 0 || after.Search != "" ||
		after.Grid.System != "" || after.Grid.Cell != "" || len(after.Cameras) != 0 ||
		after.Selected != "" || after.Detail.Open || after.Dock.Open || after.Overview.Docked ||
		after.Sidebar.Collapsed || len(after.Labels) != 0 {
		t.Errorf("the record the reset came back to is not fresh: %+v", after)
	}
	// The subdivision is the one field a fresh record does not leave at its
	// zero value: a grid a reader opens is a grid with its next level drawn.
	if after.Grid.Subgrid != 1 {
		t.Errorf("the fresh record's subgrid = %d, want the arrangement's own 1", after.Grid.Subgrid)
	}
}

// A reset with nothing to reset. Pressing ⌘R twice, or on a volume opened and
// never arranged, is the same request against a store that holds no record for
// it -- and deleting what is not there is what the caller asked for, so it
// answers exactly as the first press did and writes nothing behind it.
func TestSessionResetWithNoRecordIsANoOp(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))

	got := post(t, handler, "/session/reset", url.Values{"volume": {"tunic"}})
	if got.Code != http.StatusNoContent {
		t.Fatalf("a reset with nothing to reset answered %d: %s", got.Code, got.Body)
	}
	// The world came from the manifest rather than from the record, because
	// there was no record to read it out of.
	if back := got.Header().Get("HX-Redirect"); back != "/v/tunic/overworld" {
		t.Errorf("HX-Redirect = %q, want the volume's first world", back)
	}
	names, err := host.sessions.Names()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name == "volume.tunic.json" {
			t.Errorf("a reset wrote the record it was asked to delete: %v", names)
		}
	}
}

// sessionRecord reads one volume's record out of the store under test.
func sessionRecord(t *testing.T, host *fakeHost, name string) app.Session {
	t.Helper()
	held, err := host.sessions.Load(name)
	if err != nil {
		t.Fatalf("no session record %s: %v", name, err)
	}
	var session app.Session
	if err := json.Unmarshal(held, &session); err != nil {
		t.Fatal(err)
	}
	return session
}

// The camera is the one continuous thing the server keeps, and it answers with
// nothing a reader can see: a swap of any of the chrome in response to a
// settling camera would fight the reader's own hand. What it does answer with
// is the state island, which is an inert script node -- no focus to lose, no
// scroll to reset -- because otherwise the camera it just wrote would be
// readable only after the next unrelated request, and the parity baselines
// record it on their very first step.
func TestCameraReportIsQuiet(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	got := post(t, handler, "/session/view", url.Values{
		"volume": {"tunic"}, "world": {"overworld"},
		"x": {"120.5"}, "y": {"-40"}, "zoom": {"6.25"}, "rotation": {"0.5"},
	})
	if got.Code != http.StatusOK {
		t.Fatalf("a camera report answered %d, want 200", got.Code)
	}
	body := got.Body.String()
	if strings.Count(body, "<hx-partial") != 1 ||
		!strings.Contains(body, `target="#atlas-session-island"`) {
		t.Errorf("a camera report answered with more than the island:\n%s", body)
	}
	if !strings.Contains(body, `"zoom":6.25`) {
		t.Errorf("the island does not carry the camera just reported:\n%s", body)
	}

	held, err := host.sessions.Load("volume.tunic.json")
	if err != nil {
		t.Fatal(err)
	}
	var session app.Session
	if err := json.Unmarshal(held, &session); err != nil {
		t.Fatal(err)
	}
	camera, kept := session.Cameras["overworld"]
	if !kept || camera.X != 120.5 || camera.Zoom != 6.25 {
		t.Errorf("camera = %+v, kept = %v", camera, kept)
	}
	if camera.At == "" {
		t.Error("the camera report carries no time")
	}
}

// A pick off the canvas, end to end: the page's half and the handler's.
//
// The seam resolves a hit and can do nothing with it on its own -- it fills a
// form this page renders and says so, exactly as the camera does. So the
// first half of this is that the form is there and wired, which is the whole
// of the defect it was written for: both panes reported picks for a milestone
// and nothing was listening, so clicking a feature moved nothing.
//
// The second half is the field the form deliberately does not carry. `focus`
// is what a row reached for from a list means -- take me there, and mark it
// in the index while I stand on it -- and a pick is not that: the reader who
// clicked the ground was already on it. Selecting and going are two facts,
// and only one of them is a canvas click's to state.
func TestCanvasPickSelectsWithoutFocusing(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))

	page := get(t, handler, "/v/tunic/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	shell := page.Body.String()
	opens := strings.Index(shell, `<form hidden id="atlas-pick"`)
	if opens < 0 {
		t.Fatalf("the page renders no pick form, so a canvas pick posts nothing:\n%s", shell)
	}
	closes := strings.Index(shell[opens:], "</form>")
	if closes < 0 {
		t.Fatalf("the pick form is never closed:\n%s", shell[opens:])
	}
	pickForm := shell[opens : opens+closes]
	for _, want := range []string{
		`hx-post="/session/select"`,
		`hx-trigger="atlas:pick from:window"`,
		`name="feature" id="atlas-pick-feature"`,
	} {
		if !strings.Contains(pickForm, want) {
			t.Errorf("the pick form is missing %s:\n%s", want, pickForm)
		}
	}
	// The volume is inherited from the shell like every other interaction's,
	// and the focus flag is not here at all.
	if strings.Contains(pickForm, "focus") {
		t.Errorf("the pick form posts a focus field, so a pick would jump:\n%s", pickForm)
	}

	got := post(t, handler, "/session/select", url.Values{
		"volume": {"tunic"}, "feature": {"1849"},
	})
	if got.Code != http.StatusOK {
		t.Fatalf("a canvas pick answered %d: %s", got.Code, got.Body)
	}
	if body := got.Body.String(); !strings.Contains(body, `target="#atlas-detail"`) {
		t.Errorf("a pick answered without the card it opened:\n%s", body)
	}

	held, err := host.sessions.Load("volume.tunic.json")
	if err != nil {
		t.Fatalf("a pick wrote no session record: %v", err)
	}
	var session app.Session
	if err := json.Unmarshal(held, &session); err != nil {
		t.Fatal(err)
	}
	if session.Selected != "1849" || !session.Detail.Open {
		t.Errorf("selection = %q, detail = %+v", session.Selected, session.Detail)
	}
	if session.Focused != "" {
		t.Errorf("a canvas pick moved the index mark to %q; only a list row does that",
			session.Focused)
	}
}

// A cell chosen off a surface, end to end: the page's half and the handler's.
//
// It is the pick's arrangement exactly, for the other thing a surface can be
// pointed at. A reader telescopes three ways -- typing an address, clicking a
// cell the chart drew, pressing a point on the sphere -- and only the first
// has a control of its own. The other two are pixels, and a pixel is the
// seam's to resolve; what it resolves to has to land somewhere, and this form
// is where.
//
// The field it does NOT carry is the interesting half. Which system is
// dividing the map is already in the record -- the address was resolved by
// that system -- so a seam posting one alongside would be the seam holding an
// opinion about state it does not own, and a stale one at that.
func TestGridPickFormCarriesTheAddressAndNothingElse(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))

	page := get(t, handler, "/v/tunic/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	shell := page.Body.String()
	opens := strings.Index(shell, `<form hidden id="atlas-grid-pick"`)
	if opens < 0 {
		t.Fatalf("the page renders no grid-pick form, so a cell clicked posts nothing:\n%s", shell)
	}
	closes := strings.Index(shell[opens:], "</form>")
	if closes < 0 {
		t.Fatalf("the grid-pick form is never closed:\n%s", shell[opens:])
	}
	pickForm := shell[opens : opens+closes]
	for _, want := range []string{
		`hx-post="/session/grid"`,
		`hx-trigger="atlas:grid-pick from:window"`,
		`name="cell" id="atlas-grid-pick-cell"`,
	} {
		if !strings.Contains(pickForm, want) {
			t.Errorf("the grid-pick form is missing %s:\n%s", want, pickForm)
		}
	}
	// The address is the one field. The volume is inherited from the shell,
	// like every other interaction's on this page.
	if strings.Contains(pickForm, `name="system"`) {
		t.Errorf("the grid-pick form posts a system, which is already in the record:\n%s", pickForm)
	}
	if strings.Contains(pickForm, `name="volume"`) {
		t.Errorf("the grid-pick form posts its own volume rather than inheriting one:\n%s", pickForm)
	}
	// It posts to the route the navigator's own field posts to, with the same
	// field name, so one address is normalized and validated for all three
	// ways into a cell.
	if !strings.Contains(shell, `id="grid-input" name="cell"`) {
		t.Errorf("the navigator's field and the grid-pick form no longer name one field:\n%s", shell)
	}
	// And it posts every keystroke, with no memory of the last one. `changed`
	// remembers the value an event carried and refuses one that matches, which
	// is right for a field nobody else writes to and wrong for this one: every
	// swap re-renders it, so ascending out of "m" leaves an empty field beside
	// a remembered "m", and a reader typing that hash again is answered with
	// silence. `input` already fires only when the value moved.
	if !strings.Contains(shell, `hx-post="/session/grid" hx-trigger="input delay:150ms"`) {
		t.Errorf("the navigator's field does not post an ordinary debounced input:\n%s", shell)
	}

	got := post(t, handler, "/session/grid", url.Values{
		"volume": {"tunic"}, "cell": {"9q"},
	})
	if got.Code != http.StatusOK {
		t.Fatalf("a cell chosen off a surface answered %d: %s", got.Code, got.Body)
	}

	held, err := host.sessions.Load("volume.tunic.json")
	if err != nil {
		t.Fatalf("a cell wrote no session record: %v", err)
	}
	var session app.Session
	if err := json.Unmarshal(held, &session); err != nil {
		t.Fatal(err)
	}
	if session.Grid.Cell != "9q" {
		t.Errorf("cell = %q, want the one the surface named", session.Grid.Cell)
	}
}

func TestSessionRefusals(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	cases := []struct {
		name string
		path string
		form url.Values
		want int
	}{
		{"a concern nobody declared", "/session/telepathy", url.Values{"volume": {"tunic"}}, http.StatusNotFound},
		{"no volume named", "/session/dock", url.Values{}, http.StatusBadRequest},
		{"a volume that is not installed", "/session/dock", url.Values{"volume": {"mars"}}, http.StatusNotFound},
		{"a world that is not a slug", "/session/world", url.Values{"volume": {"tunic"}, "world": {"../etc"}}, http.StatusBadRequest},
		{"a camera report with no camera", "/session/view", url.Values{"volume": {"tunic"}, "world": {"overworld"}}, http.StatusBadRequest},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := post(t, handler, tt.path, tt.form); got.Code != tt.want {
				t.Errorf("%s answered %d, want %d", tt.path, got.Code, tt.want)
			}
		})
	}
}

// The keyboard the shell declares, read back out of the page.
//
// These are string assertions on markup, which is usually a smell, and here it
// is the whole point: an hx-trigger filter is executable code the server only
// ever writes and never runs, so the one place a missing guard can be caught
// before a reader finds it is the bytes. Each shortcut is checked for the
// three things every one of them has to say -- whose keystroke it is, that the
// key is answered, and that a held key is not a hundred keys -- and for the
// two exceptions that are deliberate.
func TestKeyboardShortcutsAreHardened(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	page := get(t, handler, "/v/tunic/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	shell := page.Body.String()
	opens := strings.Index(shell, `<div hidden id="atlas-shortcuts">`)
	if opens < 0 {
		t.Fatalf("the page declares no shortcuts:\n%s", shell)
	}
	closes := strings.Index(shell[opens:], "</div>")
	if closes < 0 {
		t.Fatalf("the shortcuts block is never closed:\n%s", shell[opens:])
	}
	block := shell[opens : opens+closes]

	// The editable-target guard, spelled the one way every filter spells it. A
	// select is in the list because the reference puts it there, and the
	// `instanceof Element` is what lets a key dispatched straight at the
	// window -- the parity tour's way of pressing one -- through as nobody's
	// typing.
	const guard = `!(event.target instanceof Element&&` +
		`(/^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName)||event.target.isContentEditable))`
	if !strings.Contains(block, guard) {
		t.Fatalf("no filter carries the editable-target guard:\n%s", block)
	}

	for _, want := range []struct {
		name   string
		filter string
		guard  bool
	}{
		// ⌘G is one of the two shortcuts deliberately above the guard:
		// cycling the cell system is most wanted from inside the token field.
		{"⌘G cycles the cell system",
			`keydown[(metaKey||ctrlKey)&&key.toLowerCase()=='g'&&!event.repeat] from:window prevent`, false},
		// ⌘R is the other, and for the reference's own reason: a reader whose
		// page is misbehaving reaches for reload from wherever the cursor is,
		// and a reload that answers everywhere except the field they happen to
		// be typing in is a reload half taken away.
		{"⌘R resets this volume's session",
			`keydown[(metaKey||ctrlKey)&&key.toLowerCase()=='r'&&!event.repeat] from:window prevent`, false},
		// ⌘⌥B by physical key, because Option rewrites event.key on a Mac. Two
		// spans, each asking the fold button which way it is pointing, so
		// neither can go stale while the shell is not re-rendered with it.
		{"⌘⌥B unfolds the panel",
			`keydown[event.code=='KeyB'&&(metaKey||ctrlKey)&&altKey&&!event.repeat&&` + guard +
				`&&document.querySelector('#dock-fold')?.getAttribute('aria-expanded')=='false'] from:window prevent`, true},
		{"⌘⌥B folds the panel",
			`keydown[event.code=='KeyB'&&(metaKey||ctrlKey)&&altKey&&!event.repeat&&` + guard +
				`&&document.querySelector('#dock-fold')?.getAttribute('aria-expanded')=='true'] from:window prevent`, true},
		// ⌘B refuses altKey, so the two B chords can never both answer.
		{"⌘B puts the index away",
			`keydown[(metaKey||ctrlKey)&&!altKey&&key.toLowerCase()=='b'&&!event.repeat&&` + guard +
				`] from:window prevent`, true},
		{"G opens the grid",
			`keydown[key.toLowerCase()=='g'&&!metaKey&&!ctrlKey&&!altKey&&!event.repeat&&` + guard +
				`] from:window prevent`, true},
		{"Space divides the chosen cell",
			`keydown[key==' '&&!event.repeat&&` + guard +
				`&&document.querySelector('#atlas-grid-navigator')?.hidden===false` +
				`&&event.target!==document.querySelector('#subgrid-toggle')] from:window prevent`, true},
		// The field's own space bar, which the guard above shuts out by
		// design: it is heard on the field, so it needs no guard of its own.
		{"Space divides it from the field too",
			`keydown[key==' '&&!event.repeat] from:#grid-input prevent`, false},
		{"Escape closes the card",
			`keydown[key=='Escape'&&!event.repeat&&` + guard + `] from:#map prevent`, true},
		{"Escape telescopes the grid out",
			`keydown[key=='Escape'&&!event.repeat&&` + guard +
				`&&document.querySelector('#atlas-grid-navigator')?.hidden===false] from:window prevent`, true},
	} {
		if !strings.Contains(block, `hx-trigger="`+want.filter+`"`) {
			t.Errorf("%s is missing or unhardened; wanted the trigger\n  %s\nin\n%s",
				want.name, want.filter, block)
			continue
		}
		if want.guard && !strings.Contains(want.filter, guard) {
			t.Errorf("%s does not carry the editable-target guard", want.name)
		}
	}

	// The routes the shortcuts stand on, and the fold's two directions.
	for _, want := range []string{
		`hx-post="/session/grid" hx-vals:append='{"system":"cycle"}'`,
		`hx-post="/session/grid" hx-vals:append='{"system":"toggle"}'`,
		`hx-post="/session/grid" hx-vals:append='{"subgrid":"flip"}'`,
		`hx-post="/session/grid" hx-vals:append='{"ascend":"1"}'`,
		`hx-post="/session/dock" hx-vals:append='{"open":"1","byHand":"1"}'`,
		`hx-post="/session/dock" hx-vals:append='{"open":"0","byHand":"1"}'`,
		`hx-post="/session/select" hx-vals:append='{"feature":""}'`,
		`hx-post="/session/sidebar"`,
		`hx-post="/session/reset"`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the shortcuts block posts no %s:\n%s", want, block)
		}
	}

	// Escape means two things at once and both happen, so the two requests it
	// raises are queued rather than raced: the card closes, and then the grid
	// ascends against a record the first answer has already written.
	if got := strings.Count(block, `hx-sync="#atlas-shell:queue all"`); got != 2 {
		t.Errorf("%d of Escape's two routes queue on the shell; a press would race itself:\n%s",
			got, block)
	}

	// ⌘R takes the browser's reload and hands it back. The keystroke the page
	// swallows is answered with the volume's own address again, over a record
	// that has just been deleted -- so the shortcut has to stand above the
	// editable guard, where the reference put it, and the span that claims it
	// must carry no guard at all.
	reset := strings.Index(block, `hx-post="/session/reset"`)
	if reset < 0 {
		t.Fatalf("nothing claims ⌘R; the blunt reset has no key:\n%s", block)
	}
	span := block[reset:]
	if closes := strings.Index(span, "</span>"); closes >= 0 {
		span = span[:closes]
	}
	if strings.Contains(span, guard) {
		t.Errorf("⌘R carries the editable-target guard, so a page a reader is "+
			"typing in cannot be reset:\n%s", span)
	}
	if !strings.Contains(span, ` prevent"`) {
		t.Errorf("⌘R does not swallow the browser's own reload, which would "+
			"race the redirect:\n%s", span)
	}

	// Every filter says the key was pressed rather than held. A key held down
	// on a route that writes a file is sixty writes a second.
	spans := strings.Count(block, "hx-trigger=")
	if repeats := strings.Count(block, "!event.repeat"); repeats != spans {
		t.Errorf("%d of %d shortcuts guard the autorepeat", repeats, spans)
	}
	// And every one of them answers the key it acts on, which is what stops
	// the machine sounding its rejection tone at every press.
	if prevented := strings.Count(block, ` prevent"`); prevented != spans {
		t.Errorf("%d of %d shortcuts swallow the keystroke they answer", prevented, spans)
	}
	// The seam's half is not here, and must not be: none of it moves discrete
	// state, and one of them cannot be a request at all.
	for _, absent := range []string{"'z'", "'`'", "'k'", "contextmenu"} {
		if strings.Contains(block, absent) {
			t.Errorf("the shell declares %s, which belongs to the seam:\n%s", absent, block)
		}
	}
}

func TestDetailFragment(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	got := get(t, handler, "/fragments/detail/1849?volume=tunic", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("the detail fragment answered %d", got.Code)
	}
	// What the fragment must always carry is its own container, because the
	// region is swapped into it by id.
	if !strings.Contains(got.Body.String(), `id="atlas-detail"`) {
		t.Errorf("the fragment carries no card container:\n%s", got.Body)
	}
	// This volume's payload holds no features, so nothing resolves and the
	// card comes back closed -- which it says with the `hidden` the carried
	// assets/css/pin-detail.css reads, not by being empty. The states a reader
	// walks through are held in golden/island; this is the one the fragment
	// route reaches on its own.
	if !strings.Contains(got.Body.String(), `class="pin-detail" hidden>`) {
		t.Errorf("a card with nothing to show does not say it is closed:\n%s", got.Body)
	}
	if got := get(t, handler, "/fragments/detail/1849", nil); got.Code != http.StatusBadRequest {
		t.Errorf("a fragment naming no volume answered %d", got.Code)
	}
}

// zonedVolume is a volume whose world is two shape collections with named
// features, which is what a feature index needs to have rows: a shape is
// indexed only once it has ground to draw (internal/app/world.go, Drawn).
func zonedVolume() *fakeVolume {
	held := volume("tunic", "TUNIC", tunicStamp)
	ring := func(west, east string) string {
		return `[{"type":"Polygon","coordinates":[[[` + west + `,44.00],[` + east +
			`,44.00],[` + east + `,44.05],[` + west + `,44.05],[` + west + `,44.00]]]}]`
	}
	held.entries["worlds/overworld.json"] = []byte(`{"lenses":[],"collections":[
		{"id":900,"title":"Zoning","kind":"area","attrs":{"atlas.geometry.kind":"area"},"features":[
			{"id":91,"title":"R1","geometry":` + ring("-121.30", "-121.25") + `},
			{"id":92,"title":"R2","geometry":` + ring("-121.20", "-121.15") + `},
			{"id":93,"title":"R3","geometry":` + ring("-121.10", "-121.05") + `}]},
		{"id":901,"title":"Watersheds","kind":"area","attrs":{"atlas.geometry.kind":"area"},"features":[
			{"id":94,"title":"Tumalo","geometry":` + ring("-121.00", "-120.95") + `}]}
	]}`)
	return held
}

// zoneOnlyButton is one zone row's exclusive control, from the attribute that
// names the zone to the end of the button that carries it.
func zoneOnlyButton(page, zone string) string {
	at := strings.Index(page, `data-zone-only="`+zone+`"`)
	if at < 0 {
		return ""
	}
	rest := page[at:]
	if end := strings.Index(rest, "</button>"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// The exclusive control on a zone row: this ground, and no other.
//
// It is a post-parity addition rather than a port. The reference implementation
// gave the only-button to collections and to sections alone, and its zone
// highlights only ever accumulated (frontend/src/areas.js, toggleZoneHighlight)
// -- so a reader who wanted one zone had to clear the set by hand, or isolate
// the collection, which answers the different and heavier question of putting
// every other collection away.
//
// What this holds is the whole of the move: an accumulated set is replaced
// rather than added to, the press that made a zone exclusive is the press that
// gives the highlights back, and a highlight still brings its own collection
// out of hiding on the way -- asking to look at a piece of ground and keeping
// it put away cannot both be meant, however the asking was spelled.
func TestOneZoneCanBeAskedForExclusively(t *testing.T) {
	handler, host := newApp(t, zonedVolume())
	session := func(t *testing.T) app.Session {
		t.Helper()
		held, err := host.sessions.Load("volume.tunic.json")
		if err != nil {
			t.Fatalf("no session was written: %v", err)
		}
		var out app.Session
		if err := json.Unmarshal(held, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	highlight := func(t *testing.T, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		form["volume"] = []string{"tunic"}
		got := post(t, handler, "/session/highlight", form)
		if got.Code != http.StatusOK {
			t.Fatalf("/session/highlight answered %d: %s", got.Code, got.Body)
		}
		return got
	}

	// The accumulating form, which is what the row's right button asks and
	// what this control is not: two zones highlighted is two zones.
	highlight(t, url.Values{"feature": {"91"}})
	highlight(t, url.Values{"feature": {"92"}})
	if held := session(t).Highlighted; len(held) != 2 {
		t.Fatalf("highlighting twice did not accumulate: %v", held)
	}
	// The collection the exclusive zone belongs to is away, so the ride-along
	// has something to do.
	if got := post(t, handler, "/session/collections",
		url.Values{"volume": {"tunic"}, "collection": {"901"}, "visible": {"0"}}); got.Code != http.StatusOK {
		t.Fatalf("hiding a collection answered %d", got.Code)
	}

	// The exclusive form replaces the set rather than joining it.
	answer := highlight(t, url.Values{"feature": {"94"}, "only": {"1"}})
	held := session(t)
	if len(held.Highlighted) != 1 || held.Highlighted[0] != "94" {
		t.Errorf("the exclusive press did not replace the set: %v", held.Highlighted)
	}
	if contains(held.Hidden, "901") {
		t.Errorf("the exclusive press left its own collection hidden: %v", held.Hidden)
	}
	// It moves what a highlight moves, and nothing else: the same three
	// regions the accumulating form declares (docs/app.md §4.3).
	for _, target := range []string{"#atlas-legend", "#atlas-dock", "#atlas-viewport-state"} {
		if !strings.Contains(answer.Body.String(), `target="`+target+`"`) {
			t.Errorf("the answer carries no partial for %s", target)
		}
	}

	// Pressing it again on the zone that is already alone is the way out. It
	// is the isolate chip's own toggle, one row further in: a control that set
	// a filter is the control that lifts it.
	highlight(t, url.Values{"feature": {"94"}, "only": {"1"}})
	if held := session(t).Highlighted; len(held) != 0 {
		t.Errorf("pressing the exclusive control again did not clear the highlights: %v", held)
	}

	// And a zone that is alone by another route -- highlighted one at a time
	// until one was left -- is exclusive too, because the state is derived
	// from the set rather than from which button reached it.
	highlight(t, url.Values{"feature": {"93"}})
	highlight(t, url.Values{"feature": {"93"}, "only": {"1"}})
	if held := session(t).Highlighted; len(held) != 0 {
		t.Errorf("a lone highlight was not treated as an exclusive one: %v", held)
	}
}

// The control on the page: every zone row wears one, it says what it would do,
// and the zone that is the whole of the highlight is the one that reads as
// pressed.
func TestAZoneRowWearsItsExclusiveControl(t *testing.T) {
	handler, _ := newApp(t, zonedVolume())
	if got := post(t, handler, "/session/highlight",
		url.Values{"volume": {"tunic"}, "feature": {"93"}, "only": {"1"}}); got.Code != http.StatusOK {
		t.Fatalf("/session/highlight answered %d: %s", got.Code, got.Body)
	}
	page := get(t, handler, "/v/tunic/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	shell := page.Body.String()

	// One control per zone, and the request it carries is the exclusive form
	// of the highlight concern -- not the isolate route the collection rows
	// use, which would put every other collection away.
	for _, zone := range []string{"91", "92", "93", "94"} {
		markup := zoneOnlyButton(shell, zone)
		if markup == "" {
			t.Fatalf("the zone %s has no exclusive control:\n%s", zone, legendOf(t, shell))
		}
		if !strings.Contains(markup, `hx-post="/session/highlight"`) {
			t.Errorf("the zone %s control does not ask the highlight concern:\n%s", zone, markup)
		}
		if !strings.Contains(markup, `"only":"1"`) {
			t.Errorf("the zone %s control does not ask exclusively:\n%s", zone, markup)
		}
		if !strings.Contains(markup, `"feature":"`+zone+`"`) {
			t.Errorf("the zone %s control names another feature:\n%s", zone, markup)
		}
	}
	// It is the same control the collection rows wear, so it reads and draws
	// as one: the carried `.only-button` rule is what reveals it.
	if !strings.Contains(zoneOnlyButton(shell, "91"), `aria-label="Exclusively R1"`) {
		t.Errorf("the control does not say what it does:\n%s", zoneOnlyButton(shell, "91"))
	}
	// And the pressed state is the state, not the press: R3 is the whole of
	// what is highlighted, so its control is the one that reads pressed.
	if !strings.Contains(zoneOnlyButton(shell, "93"), `aria-pressed="true"`) {
		t.Errorf("the exclusive zone's control does not read pressed:\n%s", zoneOnlyButton(shell, "93"))
	}
	for _, zone := range []string{"91", "92", "94"} {
		if !strings.Contains(zoneOnlyButton(shell, zone), `aria-pressed="false"`) {
			t.Errorf("the zone %s reads exclusive while %s is:\n%s", zone, "93", zoneOnlyButton(shell, zone))
		}
	}
	// The control is a sibling of the row's own button and not a child of it,
	// which is the whole of how one click cannot be the other: an event
	// reaches its ancestors and never its siblings.
	row := shell[strings.Index(shell, `data-zone="93"`):]
	row = row[:strings.Index(row, `data-zone-only="93"`)]
	if strings.Count(row, "</button>") != 1 {
		t.Errorf("the exclusive control is nested inside the row's own button:\n%s", row)
	}
}

// A legend row wears the collection's artwork, and only a collection with no
// artwork wears its initials.
//
// The reference set `--pin-icon` and the `has-source-icon` class on the row's
// icon cell and emptied its text, or wrote the initials into it
// (frontend/src/theme.js, applyCategoryGlyph); the stylesheet that draws one
// from the other came over verbatim, so the whole of the port is what the
// template emits. A city is the volume that showed it missing -- its
// collections are enriched rather than curated, so they carry a standard glyph
// and no colour of their own -- and it is the shape the payload below has.
func TestALegendRowWearsTheCollectionsArtwork(t *testing.T) {
	held := volume("bend-or", "Bend, Oregon", tunicStamp)
	held.entries["worlds/overworld.json"] = []byte(`{"lenses":[],"collections":[
		{"id":1496244488,"title":"Historic Resources","kind":"point","group":"Heritage",
		 "icon":"historic-resources","iconAsset":"std--maki-monument.svg",
		 "attrs":{"atlas.geometry.kind":"point","atlas.icon.kind":"glyph"}},
		{"id":42,"title":"Fire Stations","kind":"point","group":"Heritage",
		 "attrs":{"atlas.geometry.kind":"point"}}
	]}`)
	handler, _ := newApp(t, held)

	page := get(t, handler, "/v/bend-or/overworld", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d", page.Code)
	}
	shell := page.Body.String()
	base := "/data/v/bend-or/" + bundle.ShortStamp(tunicStamp)

	// The collection that carries a glyph: the artwork, named the way the
	// seam names it, and no initials to draw over it.
	wearing := `<span class="category-icon has-source-icon" style="--pin-icon: ` +
		`url('` + base + `/icons/std--maki-monument.svg')" title="historic-resources"></span>`
	if !strings.Contains(shell, wearing) {
		t.Errorf("the Historic Resources row draws no artwork; want\n\t%s\nin\n%s", wearing, legendOf(t, shell))
	}
	// The collection that carries none: its initials, and nothing that would
	// make the stylesheet look for a picture.
	if want := `<span class="category-icon" title="Fire Stations">FS</span>`; !strings.Contains(shell, want) {
		t.Errorf("a collection with no artwork lost its initials; want\n\t%s\nin\n%s", want, legendOf(t, shell))
	}

	// And the colour: first in the payload is first on the wheel, which is
	// the colour the seam draws the same collection's pins in
	// (render/chart/styles.ts, collectionColor). The two used to disagree
	// -- the legend hashed the collection's id -- and a city, whose
	// collections declare no colour of their own, is where it showed.
	if want := `class="category-row" style="--pin-color: #4fb3d5"`; !strings.Contains(shell, want) {
		t.Errorf("the first collection is not wearing the first colour of the wheel:\n%s", legendOf(t, shell))
	}
}

// legendOf is the sidebar alone, for a failure message that is readable.
func legendOf(t *testing.T, shell string) string {
	t.Helper()
	opens := strings.Index(shell, `<aside id="atlas-legend"`)
	if opens < 0 {
		return shell
	}
	closes := strings.Index(shell[opens:], "</aside>")
	if closes < 0 {
		return shell[opens:]
	}
	return shell[opens : opens+closes]
}

// contains is the membership question a handful of checks above ask of a
// sorted set.
func contains(set []string, member string) bool {
	for _, held := range set {
		if held == member {
			return true
		}
	}
	return false
}

func TestStaticWithoutASeam(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	if got := get(t, handler, "/static/app.js", nil); got.Code != http.StatusNotFound {
		t.Errorf("a host that mounted no seam answered %d, want 404: the application works without it",
			got.Code)
	}
}
