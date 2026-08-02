# 2. allons is removed, and nothing replaces it

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §1, §3.4, §10 decision 1

## Context

The pre-rewrite desktop application is built on `allons`, a framework of its
own carried as a module dependency. Before the rewrite was decided, what the
application actually consumes from its host was measured: **one event
subscription and one file dialog.** Everything else the framework offered was
either unused or a wrapper around something the standard library or Wails
already exposes.

## Decision

allons is removed entirely. The desktop host becomes roughly 150 lines in
`internal/app`: `wails.Run` with the application handler as the asset server, an
`fs.Sub` static mount for the seam bundle, and the native file dialog behind
`Hostenv.PickFile`. The event subscription becomes SSE on `GET /events` — a
server-sent stream of ready-to-swap partials — so no Wails runtime JavaScript
is needed in the page at all.

**No framework replaces it.** The gap it filled is now a named interface
(see [ADR 7](0007-hostenv-portability.md)) rather than a dependency.

## Consequences

- The host contract shrinks to three methods, which is what makes three
  different hosts plausible rather than aspirational.
- The page has no framework runtime of its own: what the browser loads is the
  vendored hypermedia runtime and, when present, the seam's one bundle.
- The Wails host is a later wave of this rewrite; until it lands, `atlas serve`
  is the only host and `PickFile` answers `ErrNotAvailable`. The application is
  fully functional under it, which is the point.
- `github.com/FelineStateMachine/allons` leaves `go.mod` when the old tree is
  archived, not before — the reference implementation still needs it to run as
  the oracle.
