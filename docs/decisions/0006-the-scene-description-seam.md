# 6. The seam is driven by an inert scene description

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §5.5, §10 decision 12;
  [render-seam.md](../render-seam.md), [app.md](../app.md) §6

## Context

Something has to draw the map, and whatever draws it cannot be hypermedia. The
risk is that the drawing code slowly becomes a second application: it starts
holding discrete state, it starts fetching things the server already knows, and
the "thin seam" becomes the place where the real logic lives — which is where
the reference implementation ended up.

## Decision

The server renders an **inert `#atlas-viewport-state` node** — data attributes
and `<data>` children, readable with no JavaScript running — and the seam
observes and reconciles it. `readScene(node)` is a pure, lenient function of a
node-shaped thing; `sceneChange(was, now)` names what moved (`volume`, `world`,
`lens`, `filters`, `selection`, `grid`, `camera`) and the seam acts on the
names, which is what makes a swap a reconcile rather than a rebuild.

**Data flows one way** — server → scene description → seam. Exactly two things
flow back: the `atlas:pick` event and the debounced camera report.

The seam stands on three published documents (the `/data` plane, the scene
description, the analysis API) and one duty (diagnostics), and on nothing else.
Two custom elements are its whole surface.

**Deletability is a design principle, not a CI stunt.** It is upheld by
mechanisms: depcheck forbids inbound imports, the dependency surface is the
published contracts, custom elements are progressive by construction, and
`docs/render-seam.md` is held to the standard of a complete blind-rewrite
brief. No job deletes the folder to prove a point.

## Consequences

- The viewport is morph-skipped: a swap may touch its attributes, never its
  internals. That requirement is what lets the scene node live inside a page
  the server re-renders freely.
- Until the bundle loads, `<atlas-viewport>` is an unknown element that renders
  nothing and breaks nothing. The application serves a complete page with the
  seam's assets absent — asserted, not asserted-about, by the `seam-assets`
  waiver ([ADR 13](0013-assets-and-static-are-two-mounts.md)).
- The parity harness can compare server state and seam state under matching key
  names, because both publish diagnostics against one schema.
- A ~3,000 authored-line budget is tracked as a warning: a seam growing a
  second application inside it becomes visible in CI rather than later.
