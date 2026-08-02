// Package httpgolden replays the recorded data plane against the clean-room
// application (issue #5 §6, gate `http-replay`).
//
// golden/fixtures/http/transcript.json is every request the reference
// implementation was asked and every answer it gave, headers included. This
// test asks the same questions of internal/app and holds the answers to the
// recording: status, the recorded header set, and the body by length and
// SHA-256 -- after the same two normalizations the capture made, the Date
// header and the library directory the catalog reports.
//
// # Two modes, and why
//
// Most of the transcript is content out of real .atlas archives, and those
// archives are not in the repository -- a fixture bundle is a canonicalized
// extraction, not the file. So the gate runs at whatever depth the machine
// allows:
//
//   - **synthesized** (always). A volume store built from the committed
//     fixture manifests. It can answer the catalog and every refusal, which
//     means composition -- field names, ordering, the base URL, bundlesDir --
//     is checked byte for byte on any machine, CI included. Exchanges that
//     need archive content are counted and reported, not silently passed.
//   - **registry** (when ATLAS_REGISTRY_DIR names a bundles directory). A
//     real library holding exactly the fixture builds, served through the real
//     OS host. Every exchange replays, bodies included.
//
// # The waived exchanges
//
// Three exchanges are the application's own page and its assets -- the shell
// HTML and the seam bundle the reference implementation served. The rewrite
// replaces that page by design (§4.2, real URLs for hash routing) and the
// seam's build lands in M6, so those bodies cannot match and are not made to:
// they are entries in golden/waivers.json, and this test holds them to what
// still has to be true -- the status and the content type.
package httpgolden

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv/oshost"
)

const (
	fixturesDir = "../fixtures"
	registryEnv = "ATLAS_REGISTRY_DIR"

	// cityDirEnv and cityDirDefault are golden/capture/capture.sh's own
	// convention for the one fixture volume that is not installed anywhere:
	// the public proof city, built for the fixture and read from where it was
	// built.
	cityDirEnv     = "ATLAS_GOLDEN_CITY_DIR"
	cityDirDefault = "../../dist/bundles"

	datePlace    = "<date>"
	bundlesPlace = "<bundles-dir>"
)

// waiver is what is left to check on an exchange golden/waivers.json accepts a
// difference on. The reviewed decision lives in the waiver file; this is the
// reduced assertion that decision leaves behind, spelled out so a waiver never
// means "unchecked".
type waiver struct {
	id     string
	assert func(t *testing.T, status int, headers map[string]string, want exchange)
}

var waived = map[string]waiver{
	// The reference implementation served one hash-routed shell document. The
	// rewrite serves a page per world and sends / to the last one, which a
	// client follows to a different document of the same kind: the status and
	// the content type are still the recorded ones, and the bytes are not.
	"/": {id: "app-shell-page", assert: func(t *testing.T, status int, headers map[string]string, want exchange) {
		t.Helper()
		if status != want.Status {
			t.Errorf("answered %d, recorded %d", status, want.Status)
		}
		if got := headers["Content-Type"]; got != want.Headers["Content-Type"] {
			t.Errorf("Content-Type = %q, recorded %q", got, want.Headers["Content-Type"])
		}
	}},

	// The seam's built bundle and stylesheet are M6's, and this replay mounts
	// no static tree, so the honest answer today is 404 -- which is also the
	// answer that proves the application serves without the seam present.
	"/static/app.css": {id: "seam-assets", assert: assertNoSeam},
	"/static/app.js":  {id: "seam-assets", assert: assertNoSeam},
}

func assertNoSeam(t *testing.T, status int, _ map[string]string, want exchange) {
	t.Helper()
	if status != http.StatusNotFound {
		t.Errorf("answered %d with no seam mounted, want 404: the application must serve without it", status)
	}
}

// exchange is one recorded request and its answer, in the capture's own shape
// (golden/capture/http/main.go).
type exchange struct {
	Note           string            `json:"note"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`
	Status         int               `json:"status"`
	Headers        map[string]string `json:"headers"`
	BodyBytes      int               `json:"bodyBytes"`
	BodySHA256     string            `json:"bodySha256"`
	BodyFile       string            `json:"bodyFile,omitempty"`
	BodyText       string            `json:"bodyText,omitempty"`
}

type transcript struct {
	Note      string     `json:"note"`
	Exchanges int        `json:"exchanges"`
	Recorded  []exchange `json:"transcript"`
}

// fixtureSet is the part of FIXTURES.json this test needs: which file is the
// serving build of each fixture volume, and whether that file is one the
// library holds at all. A volume carrying builtFor was built for the fixture
// rather than found installed, and is read from where it was built.
type fixtureSet struct {
	Volumes []struct {
		Slug     string          `json:"slug"`
		File     string          `json:"file"`
		Stamp    string          `json:"stamp"`
		BuiltFor json.RawMessage `json:"builtFor,omitempty"`
	} `json:"volumes"`
}

func TestReplayTranscript(t *testing.T) {
	recorded := readTranscript(t)
	if len(recorded.Recorded) != recorded.Exchanges {
		t.Fatalf("the transcript announces %d exchanges and carries %d",
			recorded.Exchanges, len(recorded.Recorded))
	}

	t.Run("synthesized", func(t *testing.T) {
		store := synthesizedStore(t)
		replay(t, recorded, store, false)
	})

	t.Run("registry", func(t *testing.T) {
		store := registryStore(t)
		replay(t, recorded, store, true)
	})
}

// replay drives every recorded exchange against a server over one store.
// whole says whether the store can answer for archive content; where it
// cannot, content exchanges are reported rather than skipped quietly.
func replay(t *testing.T, recorded transcript, store hostenv.VolumeStore, whole bool) {
	t.Helper()
	server := httptest.NewServer(app.New(&host{volumes: store, sessions: hostenv.NewMemorySessions()}, app.Options{}))
	defer server.Close()

	var checked, reduced, deferred int
	for _, want := range recorded.Recorded {
		name := want.Method + " " + want.Path
		if want.RequestHeaders != nil {
			name += " (" + headerList(want.RequestHeaders) + ")"
		}
		t.Run(name, func(t *testing.T) {
			if !whole && needsArchive(want) {
				deferred++
				t.Skipf("needs the real archive: set %s to replay this exchange", registryEnv)
			}
			status, headers, body := ask(t, server.URL, want)
			body = scrub(body, store.Location())

			if accepted, held := waived[want.Path]; held {
				accepted.assert(t, status, headers, want)
				reduced++
				t.Logf("body waived by %s (golden/waivers.json)", accepted.id)
				return
			}
			if status != want.Status {
				t.Fatalf("answered %d, recorded %d\n%s", status, want.Status, string(body))
			}
			compareHeaders(t, headers, want.Headers)
			if got := len(body); got != want.BodyBytes {
				t.Errorf("body is %d bytes, recorded %d", got, want.BodyBytes)
			}
			if got := hashOf(body); got != want.BodySHA256 {
				t.Errorf("body hashes to %s, recorded %s", got, want.BodySHA256)
			}
			if want.BodyText != "" && string(body) != want.BodyText {
				t.Errorf("body = %q, recorded %q", body, want.BodyText)
			}
			if want.BodyFile != "" {
				held, err := os.ReadFile(filepath.Join(fixturesDir, "http", want.BodyFile))
				if err != nil {
					t.Fatal(err)
				}
				// The fixture files are written newline-terminated; the body
				// they hold is what was served, which is not.
				held = bytes.TrimSuffix(held, []byte("\n"))
				if string(body) != string(held) {
					t.Errorf("the body differs from %s: %s", want.BodyFile, firstDifference(body, held))
				}
			}
			checked++
		})
	}
	t.Logf("%d exchanges replayed whole, %d reduced by a waiver, %d awaiting a real library",
		checked, reduced, deferred)
}

// needsArchive reports whether an exchange can only be answered by a store
// that holds the actual .atlas files: a served entry out of a volume.
func needsArchive(want exchange) bool {
	return want.Status == http.StatusOK && strings.HasPrefix(want.Path, "/data/v/")
}

func ask(t *testing.T, base string, want exchange) (int, map[string]string, []byte) {
	t.Helper()
	request, err := http.NewRequest(want.Method, base+want.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range want.RequestHeaders {
		request.Header.Set(name, value)
	}
	// The capture used an ordinary client, so a redirect it followed is part
	// of what was recorded -- the path that climbs out of a volume is
	// answered by the mux's own cleaning, and the 404 recorded is the answer
	// at the end of that redirect.
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	headers := make(map[string]string, len(response.Header))
	for name := range response.Header {
		value := strings.Join(response.Header.Values(name), ", ")
		if name == "Date" {
			value = datePlace
		}
		headers[name] = value
	}
	return response.StatusCode, headers, body
}

// compareHeaders holds the answer to the recorded header set exactly, in both
// directions: a header the reference did not send is as much a difference as
// one it did.
func compareHeaders(t *testing.T, got, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if held, sent := got[name]; !sent {
			t.Errorf("no %s header; recorded %q", name, value)
		} else if held != value {
			t.Errorf("%s = %q, recorded %q", name, held, value)
		}
	}
	for name, value := range got {
		if _, recorded := want[name]; !recorded {
			t.Errorf("%s = %q was sent, and the reference sent no such header", name, value)
		}
	}
}

func scrub(body []byte, location string) []byte {
	if location == "" {
		return body
	}
	return []byte(strings.ReplaceAll(string(body), location, bundlesPlace))
}

func hashOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func firstDifference(got, want []byte) string {
	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			from := max(0, i-40)
			to := min(len(got), i+40)
			return fmt.Sprintf("%q\n     %q (at byte %d)", got[from:to], want[from:min(len(want), i+40)], i)
		}
	}
	return fmt.Sprintf("%d bytes against %d", len(got), len(want))
}

func headerList(headers map[string]string) string {
	names := make([]string, 0, len(headers))
	for name, value := range headers {
		names = append(names, name+": "+value)
	}
	sort.Strings(names)
	return strings.Join(names, "; ")
}

func readTranscript(t *testing.T) transcript {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, "http", "transcript.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded transcript
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	return recorded
}

// synthesizedStore is a library built out of the committed fixture manifests:
// every serving volume the transcript names, with nothing behind them. It is
// what makes catalog composition checkable on a machine with no bundles.
func synthesizedStore(t *testing.T) hostenv.VolumeStore {
	t.Helper()
	dirs, err := os.ReadDir(filepath.Join(fixturesDir, "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	store := &manifestStore{location: "/fixtures/registry"}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fixturesDir, "bundles", dir.Name(), "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest bundle.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("%s: %v", dir.Name(), err)
		}
		if err := manifest.Validate(); err != nil {
			t.Fatalf("%s: %v", dir.Name(), err)
		}
		store.volumes = append(store.volumes, manifestVolume{manifest: manifest})
	}
	if len(store.volumes) == 0 {
		t.Fatal("no fixture manifests to build a library from")
	}
	return store
}

// registryStore builds a library holding exactly the fixture builds, the way
// golden/capture/capture.sh builds the registry it records from: the fixture
// files linked into a directory of their own, so the fold answers for the
// fixtures and for nothing else.
//
// The files come from two places, for the same reason the capture script reads
// two: the installed library holds the games and the planet, and the public
// city was built for the fixture and lives where it was built.
func registryStore(t *testing.T) hostenv.VolumeStore {
	t.Helper()
	dir := os.Getenv(registryEnv)
	if dir == "" {
		t.Skipf("set %s to a bundles directory to replay the recorded bodies", registryEnv)
	}
	cityDir := os.Getenv(cityDirEnv)
	if cityDir == "" {
		cityDir = cityDirDefault
	}

	data, err := os.ReadFile(filepath.Join(fixturesDir, "FIXTURES.json"))
	if err != nil {
		t.Fatal(err)
	}
	var set fixtureSet
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}

	library := t.TempDir()
	for _, volume := range set.Volumes {
		if volume.File == "" {
			continue
		}
		from, convention := dir, registryEnv
		if len(volume.BuiltFor) > 0 {
			from, convention = cityDir, cityDirEnv
		}
		source, err := filepath.Abs(filepath.Join(from, volume.File))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(source); err != nil {
			t.Skipf("no %s in %s: point %s at the directory holding it, or re-capture the fixture set",
				volume.File, from, convention)
		}
		if err := os.Symlink(source, filepath.Join(library, volume.File)); err != nil {
			t.Fatal(err)
		}
	}

	store, err := oshost.NewVolumes(library)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// manifestStore is a VolumeStore with manifests and no archives.
type manifestStore struct {
	volumes  []hostenv.Volume
	location string
}

func (s *manifestStore) Volumes() []hostenv.Volume { return s.volumes }
func (s *manifestStore) Location() string          { return s.location }
func (s *manifestStore) Rescan() ([]string, error) { return nil, nil }

func (s *manifestStore) Install(string, io.Reader) (hostenv.Installed, error) {
	return hostenv.Installed{}, fmt.Errorf("the synthesized fixture library takes no imports")
}

type manifestVolume struct{ manifest bundle.Manifest }

func (v manifestVolume) Manifest() bundle.Manifest { return v.manifest }

func (v manifestVolume) Open(entry string) (io.ReadCloser, int64, error) {
	return nil, 0, fmt.Errorf("the fixture manifests carry no %s", entry)
}

// host is the hostenv the replay mounts: a library, sessions that live as long
// as the test, and no picker.
type host struct {
	volumes  hostenv.VolumeStore
	sessions hostenv.SessionStore
}

func (h *host) Volumes() hostenv.VolumeStore   { return h.volumes }
func (h *host) Sessions() hostenv.SessionStore { return h.sessions }

func (h *host) PickFile(ctx context.Context) (io.ReadCloser, string, error) {
	return nil, "", hostenv.ErrNotAvailable
}
