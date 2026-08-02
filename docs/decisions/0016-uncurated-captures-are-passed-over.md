# 16. A capture a reader cannot answer for is passed over, not refused

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** `internal/generate/sources` (`ErrNotReady`),
  `internal/generate/sources/arcgishub`; [generate.md](../generate.md)

## Context

The pipeline walks a whole archive: every volume, every source, in one run. Two
different things can go wrong with one entry, and the reference implementation
did not distinguish them — a malformed capture and a capture that is simply not
this reader's business both stopped the run.

The second case is real rather than theoretical. The city source is driven by a
curation table that pins each city's window; an operator may have crawled
**their own city** into the same archive, and the repository's privacy rule
keeps that city's name out of the public table. A hard refusal there takes
every other volume in the archive down with it.

## Decision

Two outcomes, kept distinct:

- **Refused** — the capture is wrong. Another source's kind; a world slug that
  is not a capture day; no basemap pyramid, no window, no datasets; a dataset
  captured twice; a point dataset whose rows are all untitled or mostly outside
  the declared window. A reader states its preconditions and refuses what does
  not meet them rather than guessing.
- **Passed over** — the capture is present but this reader cannot answer for
  it: an uncurated city, a half-finished crawl, a world with no capture. The
  reader wraps `ErrNotReady`, and the caller skips the volume with an `info`
  line naming the source and the reason.

`sources.ErrNotReady` aliases the archive's own signal, so a caller has one
thing to test with `errors.Is`.

## Consequences

- One unreadable volume never fails the other thirty. `atlas translate`,
  `tiles`, `compose` and `enrich` all skip on the same signal, so the behaviour
  is the pipeline's rather than one command's.
- The uncurated city is passed over rather than guessed at, because an
  unverified window would hang every pin on the wrong pixel — silence over
  plausibility, the same ethic the enrichers are held to.
- The public curation table can stay public. A private city lives in a
  gitignored table and the archive holding it still processes cleanly against
  the committed one.
- `ErrNotReady` is a *skip*, never a success: nothing downstream sees a partial
  volume, and the run's log says what it declined to read.
