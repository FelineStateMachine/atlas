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

allons is removed entirely. The desktop host becomes roughly 150 lines at the
module root: `wails.Run` with the application handler as the asset server, an
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
- The Wails host was a later wave of this rewrite; until it landed at close-out,
  `atlas serve` was the only host and `PickFile` answered `ErrNotAvailable`.
  The application was fully functional under it, which was the point.
- `github.com/FelineStateMachine/allons` left `go.mod` when the old tree was
  archived, not before — the reference implementation needed it to run as the
  oracle. It went with the tree, along with the transitive SQLite and BLAKE3
  dependencies it brought.

## What it actually cost

The host came in at 162 lines of `main.go`, 122 of `redirects.go` and 83 of
`hostenv/wailshost`. `redirects.go` is the estimate's one surprise and none of
it is framework: a webview scheme task cannot express a redirect, so the host
walks the application's own doorways itself (`docs/app.md` §1.4).
