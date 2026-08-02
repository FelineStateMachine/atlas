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
