// Package oprunner runs one pipeline operation at a time and streams its voice
// back to whoever asked for it.
//
// The workbench does not reimplement the pipeline: an operation is the `atlas`
// binary invoked exactly as a person at a terminal would invoke it, and what
// comes back is that run's own event stream, one row at a time, flushed as it
// arrives (issue #5 §3.1, §5.6). This package is the whole of that mechanism
// and is deliberately not workbench-internal code: the safety properties below
// are the interesting part, they are testable without a page around them, and a
// second consumer of the pipeline gets them by importing rather than by copying.
//
// # The safety properties, carried verbatim
//
// They are carried verbatim from the reference workbench (readable on the
// golden-reference tag) and are named here because they are the contract, not
// an implementation detail:
//
//   - **Origin-checked.** A browser sends Origin on any cross-site POST.
//     [CheckOrigin] refuses a foreign one, so a stray page cannot operate the
//     workbench through the visitor's browser.
//   - **One operation at a time.** [Runner.Acquire] is the single slot; a
//     second submission is answered 409 rather than queued. The tools are safe
//     to interleave in principle, but two crawls sharing one pane help nobody.
//   - **Validated targets.** [ValidTarget] admits only what is safe as a
//     command argument and is actually a slug; a leading dash is refused so a
//     target can never be read as a flag.
//   - **It dies with the request.** The subprocess runs under the request's
//     context: a page abandoned mid-operation stops its operation, so nothing
//     crawls on with nobody watching.
//
// # The stream
//
// Rows are data, not markup. The runner parses the subprocess's event stream
// -- `atlas` writes slog records to stderr, JSON when asked (docs/logging.md)
// -- into [Row] values carrying level, message and the documented attribute
// vocabulary; anything that is not a record is carried as its own line.
// Rendering rows as HTML is the consumer's business, which is what keeps the
// framework vocabulary in templates (issue #5 §4.3).
package oprunner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// Operation is one subprocess to run: what it is called, where it runs, and the
// argv a person would have typed. Nothing here is derived from a request
// without passing through [ValidTarget] first.
type Operation struct {
	// Name is the operation as the page named it -- "crawl", "tiles",
	// "compose", "enrich", "measure". It is what a row is labelled with and
	// what a refusal talks about.
	Name string
	// Dir is the working directory. Empty means the runner's own, which is
	// what an absolute-path invocation wants.
	Dir string
	// Argv is the command, program first. It is passed to the operating
	// system as-is: no shell, no expansion, no interpolation.
	Argv []string
}

// Validate refuses an operation that is not a command. It is the last gate
// before a subprocess exists, and it exists so that a caller assembling argv
// from form values cannot accidentally assemble nothing.
func (o Operation) Validate() error {
	switch {
	case o.Name == "":
		return errors.New("an operation must be named")
	case len(o.Argv) == 0:
		return fmt.Errorf("operation %s has no command", o.Name)
	case strings.TrimSpace(o.Argv[0]) == "":
		return fmt.Errorf("operation %s names no program", o.Name)
	}
	for at, arg := range o.Argv {
		if strings.ContainsAny(arg, "\x00\n") {
			return fmt.Errorf("operation %s: argument %d carries a line break or a NUL", o.Name, at)
		}
	}
	return nil
}

// Command is the operation as one line, for the row that announces it.
func (o Operation) Command() string { return strings.Join(o.Argv, " ") }

// Kind says what a row is. The four kinds are the four things a run says: what
// was asked, what the event stream reported, what the program printed outside
// its event stream, and how it ended.
type Kind string

const (
	// KindCommand is the first row of every run: the command itself.
	KindCommand Kind = "command"
	// KindEvent is one parsed slog record from the subprocess's event stream.
	KindEvent Kind = "event"
	// KindOutput is a line the subprocess wrote that is not a record: product
	// output on stdout, or a message from something that does not speak slog.
	KindOutput Kind = "output"
	// KindResult is the last row: how the run ended.
	KindResult Kind = "result"
)

// Stream names which of a subprocess's two mouths a row came from.
const (
	StreamOut = "stdout"
	StreamErr = "stderr"
)

// Row is one line of an operation's voice.
//
// It is deliberately flat and stringly: a row exists to be rendered, and a
// consumer that has to reach into a nested value to write a table cell is
// deciding things a template should be reading.
type Row struct {
	// Seq numbers rows from one, in arrival order across both streams.
	Seq int
	// Kind is what this row is.
	Kind Kind
	// Stream is StreamOut or StreamErr for the rows that came from the
	// subprocess, empty for the rows the runner speaks itself.
	Stream string
	// Level is the event's level ("INFO", "WARN", "ERROR", "DEBUG") when the
	// row is a parsed record, empty otherwise.
	Level string
	// Time is the record's own timestamp, verbatim, when it carried one.
	Time string
	// Message is the record's message, the command line, the raw output line,
	// or the result -- whichever this row is.
	Message string
	// Attrs are the record's remaining attributes in a stable order: the
	// documented vocabulary of docs/logging.md first, in the order that
	// document lists it, then anything else alphabetically.
	Attrs []Attr
	// Failed marks a result row whose operation did not succeed, and an event
	// row at ERROR. It is a styling fact as much as a semantic one.
	Failed bool
}

// Attr is one attribute of an event.
type Attr struct{ Key, Value string }

// vocabulary is the documented attribute order of docs/logging.md. Attributes
// outside it sort after it, alphabetically: a row reads the same way every
// time, and the facts a person groups by come first.
var vocabulary = []string{"op", "volume", "world", "lens", "stamp", "source", "enricher", "dur", "path"}

// Runner owns the one operation slot. The zero value is ready to use.
type Runner struct {
	slot sync.Mutex
	// held is the operation running right now, for a refusal that can say
	// what it is waiting on.
	mu   sync.Mutex
	held string
}

// ErrBusy is what a second operation is told. It is the 409 of §5.6.
var ErrBusy = errors.New("an operation is already running")

// Busy reports the operation running right now, or the empty string.
func (r *Runner) Busy() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held
}

// Acquire takes the single operation slot, or reports that it is taken. The
// returned release is safe to call exactly once and must be called.
//
// It is exported because the refusal has to happen before a response is
// committed: a 409 cannot be sent after the first row has gone out.
func (r *Runner) Acquire(name string) (release func(), ok bool) {
	if !r.slot.TryLock() {
		return nil, false
	}
	r.mu.Lock()
	r.held = name
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.held = ""
		r.mu.Unlock()
		r.slot.Unlock()
	}, true
}

// Run streams one operation to emit, holding the slot for its whole life.
//
// The rows arrive in the order the subprocess produced them, interleaved across
// its two streams. An emit that fails ends the run: the consumer is the page,
// and a page that has gone away is the operation's reason to stop.
func (r *Runner) Run(ctx context.Context, op Operation, emit func(Row) error) error {
	if err := op.Validate(); err != nil {
		return err
	}
	release, ok := r.Acquire(op.Name)
	if !ok {
		return ErrBusy
	}
	defer release()
	return Stream(ctx, op, emit)
}

// Stream runs one operation and emits its rows, taking no slot.
//
// It is the mechanism without the mutex, exported for the consumer that has
// already acquired the slot itself -- [Runner.Serve] does exactly that, because
// it must answer 409 before it starts writing a body.
func Stream(ctx context.Context, op Operation, emit func(Row) error) error {
	if err := op.Validate(); err != nil {
		return err
	}
	// A run that ends early -- an emit that fails, a caller that gives up --
	// takes its subprocess with it.
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	seq := 0
	say := func(row Row) error {
		seq++
		row.Seq = seq
		return emit(row)
	}
	if err := say(Row{Kind: KindCommand, Message: op.Command(), Attrs: commandAttrs(op)}); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, op.Argv[0], op.Argv[1:]...)
	cmd.Dir = op.Dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		// A program that will not start is the operation's whole story, and
		// it is told through the stream rather than beside it.
		return say(Row{Kind: KindResult, Message: err.Error(), Failed: true})
	}

	lines := make(chan Row, 64)
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); scan(stdout, StreamOut, lines) }()
	go func() { defer readers.Done(); scan(stderr, StreamErr, lines) }()
	go func() { readers.Wait(); close(lines) }()

	var emitErr error
	for row := range lines {
		if emitErr != nil {
			continue // drain, so the readers finish and Wait can reap
		}
		if err := say(row); err != nil {
			emitErr = err
			stop()
		}
	}
	waitErr := cmd.Wait()
	if emitErr != nil {
		return emitErr
	}
	if waitErr != nil {
		return say(Row{Kind: KindResult, Message: waitErr.Error(), Failed: true})
	}
	return say(Row{Kind: KindResult, Message: "done"})
}

func commandAttrs(op Operation) []Attr {
	attrs := []Attr{{Key: "op", Value: op.Name}}
	if op.Dir != "" {
		attrs = append(attrs, Attr{Key: "path", Value: op.Dir})
	}
	return attrs
}

// scan reads one of the subprocess's streams line by line and turns each line
// into a row. A line that parses as a slog record becomes an event; anything
// else is carried verbatim, because a tool's own words are worth more than a
// runner's opinion of them.
func scan(from interface{ Read([]byte) (int, error) }, stream string, out chan<- Row) {
	reader := bufio.NewScanner(from)
	// A ledger line or a path can be long; a megabyte is past anything the
	// pipeline writes and short of anything that matters.
	reader.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for reader.Scan() {
		out <- rowOf(reader.Text(), stream)
	}
}

// rowOf reads one line as a row.
func rowOf(line, stream string) Row {
	if row, ok := eventOf(line); ok {
		row.Stream = stream
		return row
	}
	return Row{Kind: KindOutput, Stream: stream, Message: line}
}

// eventOf reads a JSON slog record. Anything missing its message is not one:
// the pipeline's product output is JSON too (`atlas measure -json`), and a
// score is not an event.
func eventOf(line string) (Row, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return Row{}, false
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return Row{}, false
	}
	message, ok := record["msg"].(string)
	if !ok {
		return Row{}, false
	}
	level, _ := record["level"].(string)
	when, _ := record["time"].(string)
	row := Row{
		Kind:    KindEvent,
		Level:   level,
		Time:    when,
		Message: message,
		Failed:  strings.EqualFold(level, "ERROR"),
	}
	row.Attrs = attrsOf(record)
	return row, true
}

// attrsOf is every attribute but the record's own three, in the documented
// order first and alphabetically after it.
func attrsOf(record map[string]any) []Attr {
	rest := make([]string, 0, len(record))
	for key := range record {
		switch key {
		case "msg", "level", "time", "source":
			continue
		}
		rest = append(rest, key)
	}
	sort.Slice(rest, func(a, b int) bool {
		rankA, rankB := rank(rest[a]), rank(rest[b])
		if rankA != rankB {
			return rankA < rankB
		}
		return rest[a] < rest[b]
	})
	attrs := make([]Attr, 0, len(rest))
	for _, key := range rest {
		attrs = append(attrs, Attr{Key: key, Value: valueOf(record[key])})
	}
	return attrs
}

func rank(key string) int {
	for at, known := range vocabulary {
		if key == known {
			return at
		}
	}
	return len(vocabulary)
}

// valueOf spells one attribute value. Numbers lose their JSON float shape:
// a count of 37 reads as 37, never as 3.7e+01.
func valueOf(value any) string {
	switch held := value.(type) {
	case string:
		return held
	case bool:
		if held {
			return "true"
		}
		return "false"
	case float64:
		if held == float64(int64(held)) {
			return fmt.Sprintf("%d", int64(held))
		}
		return fmt.Sprintf("%g", held)
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(held)
		if err != nil {
			return fmt.Sprint(held)
		}
		return string(encoded)
	}
}
