package app_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// importRegions is the import region as each of one import's writes rendered
// it, in order: an import answers with the whole region once per state it
// reaches, and the last of them is what the reader is left looking at. Every
// one of them must be the region morphed onto itself -- an append is what let
// the states of a run pile up and outlive it.
func importRegions(t *testing.T, body string) []string {
	t.Helper()
	var regions []string
	for _, chunk := range strings.Split(body, "<hx-partial ")[1:] {
		head, rest, ok := strings.Cut(chunk, ">")
		if !ok {
			t.Fatalf("a partial with no end to its opening tag:\n%s", body)
		}
		if !strings.Contains(head, `hx-target="#atlas-import"`) ||
			!strings.Contains(head, `hx-swap="outerMorph"`) {
			t.Errorf("an import answered with <hx-partial %s>, which is not the region morphed onto itself", head)
		}
		region, _, ok := strings.Cut(rest, "</hx-partial>")
		if !ok {
			t.Fatalf("a partial that never closes:\n%s", body)
		}
		regions = append(regions, region)
	}
	if len(regions) == 0 {
		t.Fatalf("the import answered with no partials at all:\n%s", body)
	}
	return regions
}

// importRows counts the rows a rendering of the region holds. One import is
// one row, so this is 1 while a run is under way and 0 when a run left
// nothing behind.
func importRows(region string) int { return strings.Count(region, `data-state="`) }

var importRunID = regexp.MustCompile(`id="(atlas-import-run-\d+)"`)

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

	// One run is one row: every state is the same row rendered again, and the
	// row that ends the run is the good end, which takes itself away.
	regions := importRegions(t, string(rows))
	held := importRunID.FindStringSubmatch(regions[0])
	if held == nil {
		t.Fatalf("the first row has no identity to morph against:\n%s", regions[0])
	}
	for i, region := range regions {
		if importRows(region) != 1 {
			t.Errorf("write %d of an import rendered %d rows, want the one row the run is:\n%s",
				i, importRows(region), region)
		}
		if got := importRunID.FindStringSubmatch(region); got == nil || got[1] != held[1] {
			t.Errorf("write %d of an import renamed the row it was morphing:\n%s", i, region)
		}
	}
	last := regions[len(regions)-1]
	if !strings.Contains(last, `data-state="installed"`) || !strings.Contains(last, "Mars") {
		t.Errorf("the import's last word was not the volume it installed:\n%s", last)
	}
	if !strings.Contains(last, "import-row-fading") {
		t.Errorf("an import that ended well does not take itself off the screen:\n%s", last)
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

// A refusal is the one row that stays. It is the only account of what went
// wrong, so it neither fades nor is appended to: the next import morphs the
// region and the run that failed is replaced by the run that follows it.
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
	regions := importRegions(t, body)
	last := regions[len(regions)-1]
	if importRows(last) != 1 {
		t.Errorf("a refusal left %d rows on screen, want the one:\n%s", importRows(last), last)
	}
	if strings.Contains(last, "import-row-fading") {
		t.Errorf("a refusal took itself away before it could be read:\n%s", last)
	}
}

// A cancelled picker is silent. The reader closed a dialog they opened, and
// the region ends the exchange holding nothing -- which is also the state it
// was in before the button was pressed.
func TestImportCancelledIsSilent(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	host.pick = func(ctx context.Context) (io.ReadCloser, string, error) {
		return nil, "", hostenv.ErrNoSelection
	}
	got := post(t, handler, "/bundles/import", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("the import answered %d", got.Code)
	}
	body := got.Body.String()
	regions := importRegions(t, body)
	last := regions[len(regions)-1]
	if importRows(last) != 0 {
		t.Errorf("a cancelled picker left something on screen:\n%s", last)
	}
	if !strings.Contains(last, " hidden>") {
		t.Errorf("the region of a cancelled import did not put itself away:\n%s", last)
	}
	for _, unwanted := range []string{"nothing was chosen", `data-state="unchanged"`, `data-state="refused"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a cancelled picker was announced as %q:\n%s", unwanted, body)
		}
	}
}

// A page arrives with the region already standing and already put away: the
// morph an import answers with has to find an element of this id, and a
// reader who has imported nothing has nothing to be told about it.
func TestImportRegionStartsEmpty(t *testing.T) {
	handler, _ := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	body := get(t, handler, "/v/tunic/overworld", nil).Body.String()
	region, _, ok := strings.Cut(body, "</section>")
	if _, after, found := strings.Cut(region, `<section id="atlas-import"`); !ok || !found {
		t.Fatalf("a first paint carries no import region")
	} else if !strings.Contains(after, " hidden") {
		t.Errorf("the import region stands open on a page nobody has imported anything on:\n%s", after)
	}
	if importRunID.MatchString(body) {
		t.Errorf("a first paint carries an import row")
	}
}

// Two imports in a row are two rows over time and one row on screen. The
// second run's row is a different element from the first's, which is what
// makes it a row again rather than the finished one it replaced.
func TestImportsDoNotAccumulate(t *testing.T) {
	handler, host := newApp(t, volume("tunic", "TUNIC", tunicStamp))
	host.volumes.arriving = volume("mars", "Mars", marsStamp)
	host.pick = func(ctx context.Context) (io.ReadCloser, string, error) {
		return io.NopCloser(strings.NewReader("bundle bytes")), "mars-20260801-68e141f26b1a.atlas", nil
	}

	first := importRegions(t, post(t, handler, "/bundles/import", nil).Body.String())
	// The same build again: a successful import that copies nothing.
	host.volumes.already = true
	second := importRegions(t, post(t, handler, "/bundles/import", nil).Body.String())

	for _, regions := range [][]string{first, second} {
		for i, region := range regions {
			if importRows(region) != 1 {
				t.Errorf("write %d of an import rendered %d rows, want one:\n%s", i, importRows(region), region)
			}
		}
	}
	firstRun := importRunID.FindStringSubmatch(first[len(first)-1])
	secondRun := importRunID.FindStringSubmatch(second[len(second)-1])
	if firstRun == nil || secondRun == nil {
		t.Fatalf("an import's row has no identity:\n%s\n%s", first[len(first)-1], second[len(second)-1])
	}
	if firstRun[1] == secondRun[1] {
		t.Errorf("a second import reused %q, so its row is the first one's leftovers", firstRun[1])
	}

	last := second[len(second)-1]
	if !strings.Contains(last, `data-state="unchanged"`) {
		t.Errorf("installing a build the library already held did not say so:\n%s", last)
	}
	if !strings.Contains(last, "import-row-fading") {
		t.Errorf("an import that ended well does not take itself off the screen:\n%s", last)
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
