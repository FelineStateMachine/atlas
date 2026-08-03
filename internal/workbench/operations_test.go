package workbench

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testTargets() Targets {
	return Targets{
		Atlas:     "/opt/atlas",
		Dir:       "/depot",
		Registry:  "/library/bundles",
		Archive:   "/depot/archive",
		TileSet:   "/depot/tiles",
		TileIndex: "/depot/tiles/index.json",
	}
}

func TestPlanIsTheCommandAPersonWouldHaveTyped(t *testing.T) {
	targets := testTargets()
	sources := testSources()
	cases := []struct {
		what string
		ask  request
		want []string
	}{
		{
			what: "a crawl of one source and target",
			ask:  request{Operation: "crawl", Source: "ign", Target: "cyberpunk-2077/night-city"},
			want: []string{"/opt/atlas", "crawl", "--log-json", "-archive", "/depot/archive",
				"-source", "ign", "cyberpunk-2077/night-city"},
		},
		{
			what: "every pyramid the archive holds",
			ask:  request{Operation: "tiles"},
			want: []string{"/opt/atlas", "tiles", "--log-json",
				"-archive", "/depot/archive", "-output", "/depot/tiles"},
		},
		{
			what: "composition into the registry the pages read",
			ask:  request{Operation: "compose"},
			want: []string{"/opt/atlas", "compose", "--log-json", "-archive", "/depot/archive",
				"-tiles", "/depot/tiles/index.json", "-bundles", "/library/bundles"},
		},
		{
			what: "enrichment of one volume",
			ask:  request{Operation: "enrich", Volume: "cyberpunk-2077"},
			want: []string{"/opt/atlas", "enrich", "--log-json", "-archive", "/depot/archive",
				"-tiles", "/depot/tiles/index.json", "-bundles", "/library/bundles", "cyberpunk-2077"},
		},
		{
			what: "a measurement of the registry",
			ask:  request{Operation: "measure"},
			want: []string{"/opt/atlas", "measure", "--log-json", "-bundles", "/library/bundles"},
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			op, err := plan(targets, sources, c.ask)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(op.Argv, c.want) {
				t.Errorf("argv %v, want %v", op.Argv, c.want)
			}
			if op.Name != c.ask.Operation {
				t.Errorf("the operation is called %q", op.Name)
			}
			if op.Dir != targets.Dir {
				t.Errorf("the operation runs in %q", op.Dir)
			}
		})
	}
}

func TestPlanRefusesWhatItCannotRun(t *testing.T) {
	targets := testTargets()
	sources := testSources()
	cases := []struct {
		what    string
		targets Targets
		ask     request
		says    string
	}{
		{"an operation nobody wrote", targets, request{Operation: "levitate"}, "unknown operation"},
		{"a source nobody registered", targets, request{Operation: "crawl", Source: "nowhere", Target: "x"}, "unknown source"},
		{
			"a source with no crawler", targets,
			request{Operation: "crawl", Source: "mapgenie", Target: "cyberpunk-2077"},
			"no crawler registered",
		},
		{
			"a target of the wrong shape", targets,
			request{Operation: "crawl", Source: "ign", Target: "night-city"},
			"needs exactly one slash",
		},
		{
			"a target that would read as a flag", targets,
			request{Operation: "crawl", Source: "nasa-trek", Target: "-rf"},
			"starts with a dash",
		},
		{
			"a volume slug that is not one", targets,
			request{Operation: "measure", Volume: "../etc"}, "volume",
		},
		{
			"a volume on an operation that takes none", targets,
			request{Operation: "tiles", Volume: "tunic"}, "does not take a volume",
		},
		{
			"a crawl with no archive configured",
			Targets{Atlas: "/opt/atlas", Registry: "/library/bundles"},
			request{Operation: "crawl", Source: "ign", Target: "a/b"},
			"no archive configured",
		},
		{
			"anything at all with no binary",
			Targets{Registry: "/library/bundles"},
			request{Operation: "measure"},
			"atlas binary",
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			op, err := plan(c.targets, sources, c.ask)
			if err == nil {
				t.Fatalf("the plan was accepted: %v", op.Argv)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the refusal is %q, and it should name %q", err, c.says)
			}
		})
	}
}

func TestTheOperationsPageSaysWhatItCanAndCannotRun(t *testing.T) {
	// A workbench pointed at a registry alone measures and nothing else, and
	// the page says which operations are unavailable rather than letting a
	// person find out by pressing a button.
	partial, err := New(Options{
		Targets: Targets{Atlas: "/opt/atlas", Registry: t.TempDir()},
		Sources: testSources(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, site(t, partial), "/operations")
	wants(t, "operations", body,
		"crawl", "tiles", "compose", "enrich", "measure",
		"no archive configured", "/operations/run", "op-log")

	whole, err := New(Options{Targets: testTargets(), Sources: testSources()})
	if err != nil {
		t.Fatal(err)
	}
	_, ready := get(t, site(t, whole), "/operations")
	wants(t, "operations", ready,
		"/depot/archive", "/depot/tiles", "/library/bundles",
		// one form per crawlable source, each with its own target hint
		"IGN Wiki", "NASA Trek", "cyberpunk-2077/night-city", "a body, e.g. mars")
	if strings.Contains(ready, "not configured") {
		t.Error("a fully configured workbench reports something missing")
	}
	// A source with no crawler is not offered a fetch form.
	if strings.Contains(ready, `value="mapgenie"`) {
		t.Error("a source with no crawler was offered a fetch")
	}
}

// post submits an operation form the way the page does.
func post(t *testing.T, server *http.Client, at string, form url.Values, header ...string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, at, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for pair := 0; pair+1 < len(header); pair += 2 {
		request.Header.Set(header[pair], header[pair+1])
	}
	response, err := server.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

func TestAnOperationIsRefusedBeforeItIsStarted(t *testing.T) {
	held, err := New(Options{Targets: testTargets(), Sources: testSources()})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)
	at := server.URL + "/operations/run"

	// A cross-site POST is turned away before anything is even planned.
	response, _ := post(t, server.Client(), at, url.Values{"op": {"measure"}},
		"Origin", "http://evil.example")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site submission answered %d", response.StatusCode)
	}
	// A same-origin one is not.
	for _, c := range []struct {
		what string
		form url.Values
		want int
	}{
		{"nothing at all", url.Values{}, http.StatusBadRequest},
		{"an operation nobody wrote", url.Values{"op": {"levitate"}}, http.StatusBadRequest},
		{"a bad target", url.Values{"op": {"crawl"}, "source": {"ign"}, "target": {"-rf"}}, http.StatusBadRequest},
	} {
		response, _ := post(t, server.Client(), at, c.form)
		if response.StatusCode != c.want {
			t.Errorf("%s answered %d, want %d", c.what, response.StatusCode, c.want)
		}
	}

	// One operation at a time: while the slot is held, a submission is
	// refused outright rather than queued behind it.
	release, ok := held.runner.Acquire("compose")
	if !ok {
		t.Fatal("the slot was already held")
	}
	response, body := post(t, server.Client(), at, url.Values{"op": {"measure"}})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("a second operation answered %d, want 409", response.StatusCode)
	}
	if !strings.Contains(body, "compose") {
		t.Errorf("the refusal does not say what is running: %q", body)
	}
	release()
}

func TestAnOperationStreamsItsVoiceBackAsRows(t *testing.T) {
	// The binary is a path with nothing at the end of it, so the operation
	// fails at once -- which is the point: its refusal has to arrive through
	// the page as rows rather than as a status code, and that wiring is what
	// is under test here. What a working subprocess sounds like, and that its
	// lines are flushed as they arrive, is oprunner's own suite.
	targets := testTargets()
	targets.Atlas = filepath.Join(t.TempDir(), "atlas")
	targets.Dir = t.TempDir()

	held, err := New(Options{Targets: targets, Sources: testSources()})
	if err != nil {
		t.Fatal(err)
	}
	server := site(t, held)

	response, body := post(t, server.Client(), server.URL+"/operations/run", url.Values{"op": {"measure"}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the operation answered %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("the rows arrived as %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("the stream is sniffable: %q", got)
	}
	wants(t, "operation stream", body,
		`class="op-row"`, `data-kind="command"`, "measure", "--log-json",
		"/library/bundles", `data-kind="result"`, `data-failed="true"`)

	// And the slot is free again the moment the operation ends.
	if busy := held.runner.Busy(); busy != "" {
		t.Errorf("the runner still holds %q", busy)
	}
}
