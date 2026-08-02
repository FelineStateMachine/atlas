# 13. `/assets` and `/static` are two different mounts

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** [app.md](../app.md) §2.2; issue #5 §3.2, §5.5

## Context

The page needs two kinds of static file: the application's own chrome (the
stylesheet system and the hypermedia runtime) and the seam's built bundle. It
would be simpler to serve both from one static tree. It would also make the
deletability principle unfalsifiable, because a build without the seam would be
a build without a stylesheet.

## Decision

Two mounts, and the difference **is** the deletability principle in the URL
space.

| Route | What it is |
|---|---|
| `GET /assets/{app.css,htmx.js}` | the application's own chrome, compiled into the binary (`internal/app/assets`) |
| `GET /static/{path...}` | whatever static tree the host mounted; `404` when it mounted none |

A build with no seam serves a complete, styled, interactive page.

Both are **vendored rather than linked**. The offline invariant is not only
about bundles: an atlas opens on a machine with no network, forever, and a page
that fetches its runtime from a CDN is a page that does not.

## Consequences

- `golden/waivers.json`'s `seam-assets` entry asserts the `404`, which turns
  the waiver into a check of the deletability principle rather than an
  unchecked difference from the reference implementation.
- The `make static` target copies only `render/dist/app.js` into the tree a
  host mounts. The stylesheet is deliberately not there: deleting the seam
  costs the page one script tag, not its chrome.
- The dev loop can reload the two independently — `templates.Reload(fs.FS)` and
  `assets.Reload(fs.FS)` for the application's own files, esbuild `--watch` for
  the seam.
- A host that hands over no static tree is a legitimate, tested configuration,
  which is what `atlas serve` is on a machine that never built the seam.
