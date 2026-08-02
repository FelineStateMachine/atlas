// Package logging sets up the one event stream the system narrates itself
// through: stdlib log/slog, no wrapper library, a shared attribute
// vocabulary, and the CLI wiring that decides where the stream goes.
//
// The stream has two audiences that want the same thing: a person watching a
// pipeline run, and an agent diagnosing why it failed. Both are served by one
// line per meaningful unit of work, carrying domain facts under documented
// keys rather than prose.
//
// The conventions, in short:
//
//   - Levels are used with intent. Debug is internal mechanics, off by
//     default. Info is one line per meaningful unit of work. Warn is
//     something tolerated that a human should eventually see. Error is an
//     operation that failed.
//   - The stream goes to stderr as human-readable text, so piped stdout stays
//     clean for product output -- reports, JSON, anything a caller parses.
//     --log-json switches to machine-readable; --log-level, or ATLAS_LOG,
//     opens up Debug.
//   - Attribute keys come from the vocabulary below. They name domain facts
//     about the work, never code organization: which component emitted an
//     event is the logger's name, not a vocabulary entry.
//
// The vocabulary and the level policy are documented for humans in
// docs/logging.md, which is this package's prose twin.
//
// This package deliberately sits in internal/: format/ stays log-free, and
// CLI flag wiring is not the format's concern.
package logging

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// The shared attribute vocabulary. Every event that has one of these facts to
// state states it under this key, so grep, the workbench, and an agent can
// correlate events across packages without per-package archaeology.
const (
	// KeyOp names the unit of work: "crawl", "compose", "install",
	// "enrich", "measure". It is the first thing to group by.
	KeyOp = "op"
	// KeyVolume, KeyWorld, and KeyLens are the subject, in the format's own
	// words: which .atlas file, which ground within it, which picture of that
	// ground. All three are slugs.
	KeyVolume = "volume"
	KeyWorld  = "world"
	KeyLens   = "lens"
	// KeyStamp is the content fingerprint of the build being spoken about,
	// short form. It is what ties an event to a file on disk.
	KeyStamp = "stamp"
	// KeySource names the capture source a generate-lane event is about.
	// Only merge ledgers and the generate lane ever speak it.
	KeySource = "source"
	// KeyEnricher names the enricher an enrich-lane event is about.
	KeyEnricher = "enricher"
	// KeyDur is how long a unit of work took, as a time.Duration.
	KeyDur = "dur"
	// KeyPath is a filesystem path an event is about. It is the one key that
	// names the machine rather than the domain, and it is here because a
	// person reading a failure needs to know which file.
	KeyPath = "path"
)

// Vocabulary lists the documented attribute keys, for the test that holds
// this package and docs/logging.md to the same list.
func Vocabulary() []string {
	return []string{KeyOp, KeyVolume, KeyWorld, KeyLens, KeyStamp, KeySource, KeyEnricher, KeyDur, KeyPath}
}

// The vocabulary as attribute constructors. They exist so a caller writes
// logging.Volume(slug) rather than a bare string literal, which is what keeps
// a key from being misspelled into invisibility.
func Op(name string) slog.Attr       { return slog.String(KeyOp, name) }
func Volume(slug string) slog.Attr   { return slog.String(KeyVolume, slug) }
func World(slug string) slog.Attr    { return slog.String(KeyWorld, slug) }
func Lens(name string) slog.Attr     { return slog.String(KeyLens, name) }
func Stamp(stamp string) slog.Attr   { return slog.String(KeyStamp, stamp) }
func Source(slug string) slog.Attr   { return slog.String(KeySource, slug) }
func Enricher(name string) slog.Attr { return slog.String(KeyEnricher, name) }
func Dur(d time.Duration) slog.Attr  { return slog.Duration(KeyDur, d) }
func Path(path string) slog.Attr     { return slog.String(KeyPath, path) }

// LevelEnv is the environment variable that opens up Debug when no flag says
// otherwise. A flag always wins: an explicit instruction beats an ambient one.
const LevelEnv = "ATLAS_LOG"

// Options is how a command chooses where its event stream goes. The zero
// value is the default every CLI run gets: human-readable text on stderr at
// Info.
type Options struct {
	// JSON writes machine-readable events instead of text.
	JSON bool
	// Level is the lowest level that is written: "debug", "info", "warn",
	// "error", or empty to fall back to LevelEnv and then to Info.
	Level string
	// Source adds the emitting file and line to every event. It is off by
	// default because it is noise to a person and a build detail to an agent.
	Source bool
}

// Bind registers --log-json and --log-level on a flag set. A command calls it
// once, parses, and hands the options to [Setup].
func (o *Options) Bind(fs *flag.FlagSet) {
	fs.BoolVar(&o.JSON, "log-json", o.JSON, "write the event stream as JSON instead of text")
	fs.StringVar(&o.Level, "log-level", o.Level,
		"lowest event level to write: debug, info, warn, error (default from "+LevelEnv+", else info)")
}

// Level resolves the level this run writes at: the flag if it was given, then
// the environment, then Info.
func (o Options) level() (slog.Level, error) {
	if o.Level != "" {
		return ParseLevel(o.Level)
	}
	if fromEnv := os.Getenv(LevelEnv); fromEnv != "" {
		level, err := ParseLevel(fromEnv)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", LevelEnv, err)
		}
		return level, nil
	}
	return slog.LevelInfo, nil
}

// ParseLevel reads a level name, case-insensitively. It admits the four names
// the level policy uses and nothing else: a typo should be a refusal, not a
// silent fall back to Info.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("%q is not one of debug, info, warn, error", name)
}

// New builds a logger writing to w. It is the testable form: [Setup] is this
// with stderr and the process default wired up.
func New(w io.Writer, o Options) (*slog.Logger, error) {
	level, err := o.level()
	if err != nil {
		return nil, err
	}
	options := &slog.HandlerOptions{Level: level, AddSource: o.Source}
	var handler slog.Handler
	if o.JSON {
		handler = slog.NewJSONHandler(w, options)
	} else {
		handler = slog.NewTextHandler(w, options)
	}
	return slog.New(handler), nil
}

// Setup builds the run's logger on stderr and makes it the process default,
// so a package that reaches for slog's top-level functions writes to the same
// stream as one that carries a logger.
//
// It is called once, explicitly, from a command's main -- never from an init
// -- so a library importing this package gets nothing it did not ask for.
func Setup(o Options) (*slog.Logger, error) {
	logger, err := New(os.Stderr, o)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return logger, nil
}
