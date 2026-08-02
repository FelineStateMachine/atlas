# 1. HTMX 4 owns the discrete state

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §1, §4, §10 decision 2; [app.md](../app.md)

## Context

The reference implementation is a client application: hash routing, discrete
state held in browser JavaScript, and the display logic that decides what a
legend shows — legend algebra, AND-across/OR-within filtering, label ladders,
reading the semantic conventions — living in the frontend beside the code that
draws pixels. Every one of those decisions had to be re-derived on the client
because the server had never made it.

## Decision

The application is server-driven hypermedia over HTMX 4.0, and the state
placement rule is the axis everything else hangs on: **discrete application
state lives on the server and in URLs; continuous interaction state lives in
the seam.** One per-volume session record holds volume/world/lens, hidden
collections, label overrides, search, dock, selection, grid. Real URLs replace
hash routing. Every interaction that changes discrete state is a `POST
/session/{concern}` answering with the partial set for exactly the regions that
concern touches.

Four usage rules make it stay honest: morph swaps so scroll, focus and open
`<details>` survive a re-render; the viewport morph-skipped; explicit
`:inherited` rather than a spooky cascade; and no `hx-on` logic in templates —
the glue is one after-swap rescan hook in the seam's boot module.

Two clauses bound the commitment. The **pragmatism clause**: an interaction
that proves laggy as a round trip is re-classified in the issue's interaction
inventory, never patched with ad-hoc client code. And **vocabulary
containment**: `hx-*` attributes live in templates only, so the published
contracts — routes, region names, the scene description, the diagnostics
protocol — stay framework-neutral.

## Consequences

- All display logic runs once, in Go, through `format/semconv`. Templates
  render; they do not decide.
- A world can be bookmarked, reloaded and linked to. A `GET` of an explorer
  page writes the session, so the session follows the address bar rather than
  the other way round.
- The concern table (app.md §4.3) becomes a real artifact: each concern names
  the regions it touches, and a partial set that under-covers an interaction is
  a bug with a name.
- Replacing HTMX later is a template concern, not an architecture event.
- The seam gets smaller than the frontend it replaces, because most of what the
  frontend did was decide.
