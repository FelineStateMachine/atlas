# 11. One leveled event stream, and a lane of its own for it

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §9, §10 decision 14;
  [logging.md](../logging.md)

## Context

A pipeline that crawls, derives, composes, enriches and gates has a great deal
to say, and it has two audiences that want the same thing: a person watching a
run, and an agent diagnosing why it failed. Ad-hoc prints serve neither —
they cannot be filtered, correlated, or turned off, and they contaminate stdout
for anything that wants to parse a report.

## Decision

The system narrates itself through **one leveled, structured event stream**:
Go's standard `log/slog`, with no wrapper library. `internal/logging` is only
the level policy, the attribute vocabulary and the CLI wiring, written down in
one place.

- Levels with intent: `debug` mechanics; `info` one line per meaningful unit of
  work; `warn` something tolerated a human should eventually see; `error` an
  operation failed. Warn is not a softer Error.
- Text on **stderr** by default, so piped stdout stays clean for product
  output; `--log-json` for machines; `--log-level`, then `ATLAS_LOG`, then
  `info`. An unrecognised level is refused, not ignored.
- A small shared attribute vocabulary — `op`, `volume`, `world`, `lens`,
  `stamp`, `source`, `enricher`, `dur`, `path` — exported as a constructor per
  key, so a key is never a bare literal at a call site where it could be
  misspelled into invisibility. **Keys name domain facts, never code
  organization**: which component emitted an event is the logger's concern.
- `Setup` is called once, explicitly, from a command's `main`. There is no
  `init`.

`internal/logging` is a **lane of its own** in `golden/depcheck`, so who may
import it is answered by the import matrix rather than by where it happens to
sit: everybody may, except `format/`, which depends on the standard library
alone.

`analysis/` and `render/` mirror it with a thin console-backed `log` module, and
an ESLint rule forbids bare `console.*` outside it. The matching Go analyzer
forbids `fmt.Print*`/`log.Print*` outside a command's product-output path.

## Consequences

- The workbench's streamed pipeline output is this same stream rendered as
  rows. Nothing special is emitted for it.
- The headless parity runner captures the browser console, so a failing tour
  step ships its console context for free.
- The vocabulary is semi-standardized in the `semconv` spirit: a package with a
  fact none of the keys names may add its own, and a key that earns its way
  into several packages is promoted into the table.
