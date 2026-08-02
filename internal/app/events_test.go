package app_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FelineStateMachine/atlas/internal/app"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

// The import flow and the events stream are one story: an import rescans, and
// what the rescan moved is announced to every page that is open.

// stream opens an SSE connection and reads it in the background, returning a
// reader for the events and a stop function. It waits for the connection's
// opening comment, so a test that publishes afterwards cannot race the
// subscription.
func openStream(t *testing.T, base, query string) (*bufio.Reader, func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events"+query, nil)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request) //nolint:bodyclose // closed by the stop below
	if err != nil {
		stop()
		t.Fatal(err)
	}
	if kind := response.Header.Get("Content-Type"); kind != "text/event-stream" {
		stop()
		t.Fatalf("the events stream answered %q", kind)
	}
	reader := bufio.NewReader(response.Body)
	hello, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(hello, ":") {
		stop()
		t.Fatalf("the stream opened with %q, %v; want the connection comment", hello, err)
	}
	return reader, func() {
		stop()
		response.Body.Close()
	}
}

// readEvent reads one whole event: its name and its data lines joined.
func readEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var name string
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading an event: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "" && name != "":
			return name, strings.Join(data, "\n")
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		}
	}
}

func TestImportAnnouncesWhatArrived(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	host.volumes.arriving = volume("mars", "Mars", marsStamp)
	host.pick = func(ctx context.Context) (io.ReadCloser, string, error) {
		return io.NopCloser(strings.NewReader("bundle bytes")), "mars-20260801-68e141f26b1a.atlas", nil
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	watchingMars, stopMars := openStream(t, server.URL, "?volume=mars")
	defer stopMars()
	watchingNothing, stopNothing := openStream(t, server.URL, "")
	defer stopNothing()

	response, err := http.Post(server.URL+"/bundles/import", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the import answered %d", response.StatusCode)
	}
	for _, want := range []string{`data-state="picking"`, `data-state="installing"`, `data-state="installed"`, "Mars"} {
		if !bytes.Contains(rows, []byte(want)) {
			t.Errorf("the import rows do not carry %q:\n%s", want, rows)
		}
	}
	if len(host.volumes.installs) != 1 || string(host.volumes.installs[0]) != "bundle bytes" {
		t.Fatalf("the store was handed %q", host.volumes.installs)
	}

	// Every open page hears that the library moved.
	name, data := readEvent(t, watchingNothing)
	if name != "catalog" {
		t.Fatalf("the first event was %q, want catalog", name)
	}
	if !strings.Contains(data, `target="#atlas-topbar"`) || !strings.Contains(data, "Mars") {
		t.Errorf("the catalog event carries no volume selector:\n%s", data)
	}

	// Only the page watching the volume whose build moved is told to refetch.
	if name, _ := readEvent(t, watchingMars); name != "catalog" {
		t.Fatalf("the mars stream's first event was %q", name)
	}
	name, data = readEvent(t, watchingMars)
	if name != "refresh" {
		t.Fatalf("the mars stream's second event was %q, want refresh", name)
	}
	if !strings.Contains(data, `hx-get="/v/mars/overworld"`) {
		t.Errorf("the refresh directive does not say where to refetch from:\n%s", data)
	}

	// The connection watching nothing hears no directive. Read with a
	// deadline: nothing arriving is the assertion.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = watchingNothing.ReadString('\n')
	}()
	select {
	case <-done:
		t.Error("a connection watching no volume was sent a refresh directive")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestImportWithoutAPicker(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	got := post(t, handler, "/bundles/import", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("the import answered %d", got.Code)
	}
	body := got.Body.String()
	if !strings.Contains(body, `data-state="refused"`) || !strings.Contains(body, "picker") {
		t.Errorf("a host with no picker did not say so:\n%s", body)
	}
}

func TestImportRefused(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	host.pick = func(ctx context.Context) (io.ReadCloser, string, error) {
		return nil, "", hostenv.ErrNoSelection
	}
	got := post(t, handler, "/bundles/import", nil)
	if !strings.Contains(got.Body.String(), "nothing was chosen") {
		t.Errorf("a cancelled picker read as something else:\n%s", got.Body)
	}
}

func TestEventsRefuseANonVolume(t *testing.T) {
	handler, _ := newApp(t)
	if got := get(t, handler, "/events?volume=../etc", nil); got.Code != http.StatusBadRequest {
		t.Errorf("watching %q answered %d", "../etc", got.Code)
	}
}

// The record schema is versioned, and a record from a schema this build does
// not know is passed over rather than half-read.
func TestSessionSchemaGate(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	if err := host.sessions.Save("volume.tunic.json",
		[]byte(`{"schema":`+strconv.Itoa(app.SessionSchema+1)+`,"world":"nowhere"}`)); err != nil {
		t.Fatal(err)
	}
	got := get(t, handler, "/v/tunic/overworld", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("the explorer answered %d over a record it cannot read", got.Code)
	}
	held, err := host.sessions.Load("volume.tunic.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(held, []byte(`"world":"overworld"`)) {
		t.Errorf("the unreadable record was not replaced: %s", held)
	}
}
