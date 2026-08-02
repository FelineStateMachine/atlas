package oprunner

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The helper subprocess.
//
// A runner test needs a program whose exact behaviour it wrote: one that says a
// slog record, one that fails, one that runs until it is stopped. Re-running
// this test binary under a marker argument is that program, so the tests need
// nothing on PATH and nothing built beforehand.

const helperMarker = "-atlas-oprunner-helper"

func TestHelperProcess(t *testing.T) {
	args := flag.Args()
	if len(args) == 0 || args[0] != helperMarker {
		return // an ordinary run of the suite; this test is not one of the cases
	}
	switch args[1] {
	case "speaks":
		fmt.Fprintln(os.Stderr,
			`{"time":"2026-08-02T10:00:00Z","level":"INFO","msg":"crawl finished","op":"crawl","volume":"tunic","fetched":37}`)
		fmt.Fprintln(os.Stdout, "http://127.0.0.1:6180")
		fmt.Fprintln(os.Stderr, "a line nobody structured")
	case "fails":
		fmt.Fprintln(os.Stderr,
			`{"level":"ERROR","msg":"archive not found","op":"tiles","path":"/nowhere"}`)
		os.Exit(3)
	case "waits":
		fmt.Fprintln(os.Stderr, `{"level":"INFO","msg":"started","op":"crawl"}`)
		time.Sleep(30 * time.Second)
	case "dawdles":
		fmt.Fprintln(os.Stderr, `{"level":"INFO","msg":"started","op":"crawl"}`)
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprintln(os.Stderr, `{"level":"INFO","msg":"finished","op":"crawl"}`)
	}
	os.Exit(0)
}

func helper(name, mode string) Operation {
	return Operation{
		Name: name,
		Argv: []string{os.Args[0], "-test.run=TestHelperProcess", "--", helperMarker, mode},
	}
}

// collect runs an operation and gathers its rows.
func collect(t *testing.T, ctx context.Context, r *Runner, op Operation) ([]Row, error) {
	t.Helper()
	var rows []Row
	err := r.Run(ctx, op, func(row Row) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func TestRunCarriesTheSubprocessesOwnVoice(t *testing.T) {
	rows, err := collect(t, context.Background(), &Runner{}, helper("crawl", "speaks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 4 {
		t.Fatalf("a run that said three things produced %d rows: %+v", len(rows), rows)
	}
	if rows[0].Kind != KindCommand || !strings.Contains(rows[0].Message, helperMarker) {
		t.Errorf("the first row is not the command: %+v", rows[0])
	}
	if rows[0].Attrs[0] != (Attr{Key: "op", Value: "crawl"}) {
		t.Errorf("the command row does not name its operation: %+v", rows[0].Attrs)
	}

	var event, output, plain *Row
	for at := range rows {
		switch row := &rows[at]; {
		case row.Kind == KindEvent:
			event = row
		case row.Kind == KindOutput && row.Stream == StreamOut:
			output = row
		case row.Kind == KindOutput && row.Stream == StreamErr:
			plain = row
		}
	}
	if event == nil {
		t.Fatalf("no event row: %+v", rows)
	}
	if event.Level != "INFO" || event.Message != "crawl finished" || event.Time != "2026-08-02T10:00:00Z" {
		t.Errorf("the record was not read: %+v", event)
	}
	// The documented vocabulary comes first, whatever order the record was
	// written in, and a count reads as a count.
	want := []Attr{{"op", "crawl"}, {"volume", "tunic"}, {"fetched", "37"}}
	if len(event.Attrs) != len(want) {
		t.Fatalf("attrs %+v, want %+v", event.Attrs, want)
	}
	for at := range want {
		if event.Attrs[at] != want[at] {
			t.Errorf("attr %d is %+v, want %+v", at, event.Attrs[at], want[at])
		}
	}
	if output == nil || output.Message != "http://127.0.0.1:6180" {
		t.Errorf("product output did not arrive as its own row: %+v", output)
	}
	if plain == nil || plain.Message != "a line nobody structured" {
		t.Errorf("an unstructured line was not carried verbatim: %+v", plain)
	}
	last := rows[len(rows)-1]
	if last.Kind != KindResult || last.Failed || last.Message != "done" {
		t.Errorf("the run did not end with a plain result: %+v", last)
	}
	for at, row := range rows {
		if row.Seq != at+1 {
			t.Errorf("row %d carries seq %d", at, row.Seq)
		}
	}
}

func TestAFailedOperationSaysSoInItsLastRow(t *testing.T) {
	rows, err := collect(t, context.Background(), &Runner{}, helper("tiles", "fails"))
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Kind != KindResult || !last.Failed {
		t.Fatalf("a program that exited 3 ended with %+v", last)
	}
	if !strings.Contains(last.Message, "3") {
		t.Errorf("the result does not carry the exit status: %q", last.Message)
	}
	var errorRow *Row
	for at := range rows {
		if rows[at].Kind == KindEvent {
			errorRow = &rows[at]
		}
	}
	if errorRow == nil || !errorRow.Failed || errorRow.Message != "archive not found" {
		t.Errorf("the failure's own event did not arrive marked: %+v", errorRow)
	}
}

func TestAProgramThatWillNotStartIsToldThroughTheStream(t *testing.T) {
	rows, err := collect(t, context.Background(), &Runner{},
		Operation{Name: "crawl", Argv: []string{"/nowhere/atlas", "crawl"}})
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Kind != KindResult || !last.Failed {
		t.Fatalf("a missing program ended with %+v", last)
	}
}

func TestOnlyOneOperationRunsAtATime(t *testing.T) {
	runner := &Runner{}
	started := make(chan struct{})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var once sync.Once
		_ = runner.Run(ctx, helper("crawl", "waits"), func(row Row) error {
			if row.Kind == KindEvent {
				once.Do(func() { close(started) })
			}
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the first operation never spoke")
	}

	if held := runner.Busy(); held != "crawl" {
		t.Errorf("the runner says it is running %q", held)
	}
	if _, err := collect(t, context.Background(), runner, helper("tiles", "speaks")); !errors.Is(err, ErrBusy) {
		t.Errorf("a second operation was let in: %v", err)
	}

	stop()
	wg.Wait()
	if held := runner.Busy(); held != "" {
		t.Errorf("the slot was not released; it holds %q", held)
	}
	// And the slot really is free again.
	if _, err := collect(t, context.Background(), runner, helper("tiles", "speaks")); err != nil {
		t.Errorf("the runner stayed busy after its operation ended: %v", err)
	}
}

func TestAnAbandonedRunStopsItsSubprocess(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var once sync.Once
		_ = (&Runner{}).Run(ctx, helper("crawl", "waits"), func(row Row) error {
			if row.Kind == KindEvent {
				once.Do(func() { close(started) })
			}
			return nil
		})
	}()
	<-started
	stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the operation outlived the context that started it")
	}
}

func TestAnEmitThatFailsEndsTheRun(t *testing.T) {
	refuse := errors.New("the page went away")
	err := (&Runner{}).Run(context.Background(), helper("crawl", "waits"), func(row Row) error {
		if row.Kind == KindEvent {
			return refuse
		}
		return nil
	})
	if !errors.Is(err, refuse) {
		t.Fatalf("run ended with %v, want the emit's own error", err)
	}
}

func TestOperationValidate(t *testing.T) {
	cases := []struct {
		what string
		op   Operation
		ok   bool
	}{
		{"a command", Operation{Name: "crawl", Argv: []string{"atlas", "crawl"}}, true},
		{"no name", Operation{Argv: []string{"atlas"}}, false},
		{"no command", Operation{Name: "crawl"}, false},
		{"an empty program", Operation{Name: "crawl", Argv: []string{" "}}, false},
		{"a line break in an argument", Operation{Name: "crawl", Argv: []string{"atlas", "a\nb"}}, false},
	}
	for _, c := range cases {
		if err := c.op.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}

func TestValidTarget(t *testing.T) {
	cases := []struct {
		target string
		pair   bool
		ok     bool
	}{
		{"cyberpunk-2077", false, true},
		{"bend-or", false, true},
		{"mars", false, true},
		{"cyberpunk-2077/night-city", true, true},
		{"", false, false},
		{"-rf", false, false},
		{"not a target", false, false},
		{"Cyberpunk", false, false},
		{"a/b", false, false},
		{"a", true, false},
		{"a/b/c", true, false},
		{"/night-city", true, false},
		{"cyberpunk-2077/", true, false},
		{"a;rm", false, false},
	}
	for _, c := range cases {
		err := ValidTarget(c.target, c.pair)
		if (err == nil) != c.ok {
			t.Errorf("ValidTarget(%q, pair=%v) = %v", c.target, c.pair, err)
		}
	}
}

func TestCheckOrigin(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"", true}, // a plain same-origin form submission sends none
		{"http://127.0.0.1:6180", true},
		{"https://127.0.0.1:6180", true},
		{"http://evil.example", false},
		{"null", false},
		{"http://127.0.0.1:6181", false},
	}
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:6180/operations/run", nil)
		request.Host = "127.0.0.1:6180"
		if c.origin != "" {
			request.Header.Set("Origin", c.origin)
		}
		err := CheckOrigin(request)
		if (err == nil) != c.ok {
			t.Errorf("Origin %q: %v", c.origin, err)
		}
		if err != nil && !errors.Is(err, ErrForeignOrigin) {
			t.Errorf("Origin %q refused with an unrecognisable error: %v", c.origin, err)
		}
	}
}

// rows renders one row the way the workbench does, in miniature.
func rows(w io.Writer, row Row) error {
	_, err := fmt.Fprintf(w, "<p data-kind=%q data-seq=%q>%s</p>\n", row.Kind,
		fmt.Sprint(row.Seq), row.Message)
	return err
}

func site(t *testing.T, runner *Runner, op func(*http.Request) Operation) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runner.Serve(w, r, op(r), rows)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestServeStreamsRowsAsTheyArrive(t *testing.T) {
	server := site(t, &Runner{}, func(*http.Request) Operation { return helper("crawl", "dawdles") })

	started := time.Now()
	response, err := server.Client().Post(server.URL, "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the operation answered %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("rows arrived as %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("the stream is sniffable: %q", got)
	}

	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	// The helper waits a second and a half between its two events. A row that
	// arrives before then arrived because it was flushed, not because the
	// operation was over.
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("the first row took %s: the stream is being buffered", waited)
	}
	if !strings.Contains(first, `data-kind="command"`) {
		t.Errorf("the stream does not open with the command: %q", first)
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	body := first + string(rest)
	for _, want := range []string{"started", "finished", `data-kind="result"`, "done"} {
		if !strings.Contains(body, want) {
			t.Errorf("the stream misses %q: %s", want, body)
		}
	}
}

func TestServeRefusesAForeignOriginASecondOperationAndANonCommand(t *testing.T) {
	runner := &Runner{}
	var operation func(*http.Request) Operation = func(r *http.Request) Operation {
		if r.URL.Query().Get("empty") != "" {
			return Operation{Name: "crawl"}
		}
		return helper("crawl", "waits")
	}
	server := site(t, runner, operation)

	post := func(url string, header ...string) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		for at := 0; at+1 < len(header); at += 2 {
			request.Header.Set(header[at], header[at+1])
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}

	if got := post(server.URL, "Origin", "http://evil.example").StatusCode; got != http.StatusForbidden {
		t.Errorf("a cross-site POST answered %d", got)
	}
	if got := post(server.URL + "?empty=1").StatusCode; got != http.StatusBadRequest {
		t.Errorf("an operation with no command answered %d", got)
	}

	// A long operation holds the slot; the second submission is refused
	// outright rather than queued behind it.
	release, ok := runner.Acquire("crawl")
	if !ok {
		t.Fatal("the slot was already held")
	}
	response := post(server.URL)
	if response.StatusCode != http.StatusConflict {
		t.Errorf("a second operation answered %d, want 409", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "crawl") {
		t.Errorf("the refusal does not say what is running: %q", body)
	}
	release()
}
