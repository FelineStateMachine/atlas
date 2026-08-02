// Package islandgolden holds the clean-room application to the parity
// baselines' record of the arrangement (issue #5 §6, gate `island`).
//
// It lives under golden/ rather than beside internal/app for one mechanical
// reason and one honest one. The mechanical reason: these tests read fixture
// files off the disk, and the hostenv analyzer forbids internal/app -- test
// files included -- from importing os or path/filepath, which is exactly the
// rule that keeps the handler portable. The honest one: this is a gate against
// a golden, and gates against goldens live with the goldens.
//
// The application is driven through its own HTTP surface over an in-memory
// host. Nothing here reaches inside it.
package islandgolden_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// memoryHost is a host with no machine under it: a fixed library, sessions in
// a map, and no picker. It is what hostenv is for.
type memoryHost struct {
	volumes  *memoryVolumes
	sessions *memorySessions
}

func (h *memoryHost) Volumes() hostenv.VolumeStore   { return h.volumes }
func (h *memoryHost) Sessions() hostenv.SessionStore { return h.sessions }

func (h *memoryHost) PickFile(context.Context) (io.ReadCloser, string, error) {
	return nil, "", hostenv.ErrNotAvailable
}

type memoryVolumes struct{ volumes []hostenv.Volume }

func (s *memoryVolumes) Volumes() []hostenv.Volume { return s.volumes }
func (s *memoryVolumes) Location() string          { return "/library" }
func (s *memoryVolumes) Rescan() ([]string, error) { return nil, nil }

func (s *memoryVolumes) Install(string, io.Reader) (hostenv.Installed, error) {
	return hostenv.Installed{}, errors.New("this host installs nothing")
}

type memorySessions struct{ held map[string][]byte }

func (s *memorySessions) Load(name string) ([]byte, error) {
	if err := hostenv.ValidName(name); err != nil {
		return nil, err
	}
	body, ok := s.held[name]
	if !ok {
		return nil, hostenv.ErrNoSession
	}
	return body, nil
}

func (s *memorySessions) Save(name string, data []byte) error {
	if err := hostenv.ValidName(name); err != nil {
		return err
	}
	s.held[name] = append([]byte(nil), data...)
	return nil
}

func (s *memorySessions) Names() ([]string, error) {
	out := make([]string, 0, len(s.held))
	for name := range s.held {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// newApp mounts the application over a fresh in-memory host. No static tree is
// mounted, which is the shape the application is required to work in: the seam
// lands in M6 and nothing here waits for it (issue #5 §3.2).
func newApp(t *testing.T, volumes ...hostenv.Volume) (*app.App, *memoryHost) {
	t.Helper()
	host := &memoryHost{
		volumes:  &memoryVolumes{volumes: volumes},
		sessions: &memorySessions{held: map[string][]byte{}},
	}
	return app.New(host, app.Options{}), host
}

func get(t *testing.T, handler http.Handler, path string, _ map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
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
