# The event stream

Atlas narrates itself through one leveled, structured event stream. There is
no wrapper library: Go's standard `log/slog` is the whole mechanism, and
`internal/logging` is only the level policy, the attribute vocabulary, and the
CLI wiring written down in one place.

The stream has two audiences that want the same thing — a person watching a
pipeline run, and an agent diagnosing why it failed. Both are served by one
line per meaningful unit of work, carrying domain facts under documented keys
rather than prose.

## Levels, used with intent

| Level | What belongs here | Default |
|---|---|---|
| `debug` | Internal mechanics: a cache consulted, a plan derived, a retry. Anything whose volume scales with the data. | off |
| `info` | One line per **meaningful unit of work**: a capture skipped, a pyramid derived, a bundle installed, a build gated. If a person would say it aloud when asked "what happened?", it is Info. | on |
| `warn` | Something tolerated that a human should eventually see: a waiver hit, a held merge pin, a bundle skipped during a scan, an under-claiming enricher that stayed silent. Never used for "this failed". | on |
| `error` | An operation failed. The error value rides as an attribute; the message says what was being attempted. | on |

Two rules about volume. Info is per *unit of work*, not per item: "packed
2,048 locations" is one Info line, not 2,048. And Warn is not a softer Error —
if the run continues correctly, it is Warn; if something a caller asked for
did not happen, it is Error.

## Where the stream goes

Every CLI run writes the event stream to the terminal:

- **stderr, human-readable text, by default.** Piped stdout stays clean for
  product output — reports, JSON, anything a caller parses. A command that
  prints a report to stdout and its narration to stderr composes; one that
  mixes them does not.
- **`--log-json`** switches to a machine-readable stream, for the workbench,
  for CI, and for anything that wants to filter by attribute.
- **`--log-level`** opens up `debug` (or closes down to `warn`/`error`).
  Absent the flag, **`ATLAS_LOG`** is consulted; absent both, the level is
  `info`. A flag always beats the environment: an explicit instruction beats
  an ambient one. An unrecognised level name is refused, not silently
  ignored.

The workbench's streamed pipeline output is this same stream, rendered as
rows. Nothing special is emitted for it.

`fmt.Print*` and `log.Print*` outside a command's product-output path are lint
violations (§9 of the rewrite issue). Narration goes through the stream.

## The shared attribute vocabulary

Log events speak about the system the way bundles speak about themselves. The
key set is small, documented, and used consistently, so grep, the workbench,
and an agent can correlate events without per-package archaeology.

| Key | Type | Meaning |
|---|---|---|
| `op` | string | The unit of work: `crawl`, `translate`, `tiles`, `compose`, `enrich`, `measure`, `install`, `scan`. The first thing to group by. |
| `volume` | string (slug) | Which `.atlas` volume the event is about. |
| `world` | string (slug) | Which ground within that volume. |
| `lens` | string | Which picture of that ground. |
| `stamp` | string | The content fingerprint of the build in question, short form (12 hex). What ties an event to a file on disk. |
| `source` | string (slug) | The capture source a generate-lane event is about. Only the generate lane and merge ledgers speak it. |
| `enricher` | string | The enricher an enrich-lane event is about. |
| `dur` | duration | How long the unit of work took. |
| `path` | string | A filesystem path the event is about. The one key that names the machine rather than the domain; it is here because a person reading a failure needs to know which file. |

**Keys name domain facts about the work, never code organization.** Which
component emitted an event is the logger's name or source, not a vocabulary
entry — `slog.Logger.With` and `AddSource` already answer that question.

The vocabulary is semi-standardized, in the `semconv` spirit: a package with a
fact none of these keys names may add its own, and a key that earns its way
into several packages is promoted here. `internal/logging` exports a
constructor per key (`logging.Volume(slug)`, `logging.Dur(d)`) so a key is
never a bare string literal at a call site, where it could be misspelled into
invisibility.

## Usage

```go
opts := logging.Options{}
opts.Bind(flag.CommandLine)
flag.Parse()

logger, err := logging.Setup(opts)   // stderr, and slog's process default
if err != nil {
    // an unusable --log-level is a usage error, not a warning
}

logger.Info("bundle installed",
    logging.Op("install"),
    logging.Volume("bend-or"),
    logging.Stamp("ec3fe8c21cfe"),
    logging.Dur(elapsed))
```

`Setup` is called once, explicitly, from a command's `main`. There is no
`init`: a library importing this package gets nothing it did not ask for, and
`format/` does not import it at all.

## The browser half

`render/log.ts` mirrors this: a thin leveled module, console-backed,
structured fields as an object payload, the level gate read once from
`?atlas-log=` or `localStorage`. Reading it once is deliberate — a level that
changed mid-run would make two captures of one tour disagree. The browser
console becomes the same kind of stream, which the headless parity runner
already captures, so a failing tour step ships its console context for free. An
ESLint rule forbids bare `console.*` outside that module and names this
contract when it fires. The level names and the attribute vocabulary above are
shared; the analysis lane has no logger of its own, because a pure
transformation of its declared inputs has nothing to narrate.
