# 18. The divergences from the reference, recorded

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** here; the reference this measures against is
  the `golden-reference` tag (mirrored at `archive/golden-reference`)

## Context

The clean-room rewrite was judged against recorded behavior of the
reference implementation — HTTP exchanges, pipeline outputs, and browser
tours. Eight differences were found, examined, and accepted deliberately
rather than patched over, each with a written reason. The recordings and
the mechanism that held them are not part of this tree; the accepted
differences are design decisions, and design decisions get recorded.

## The eight divergences

**The shell page** (`GET /`). The reference served one hash-routed shell
document. This application serves a page per world at a real URL and sends
`/` to the volume the reader was last in (issue #5 §4.2). The recorded 8,864
bytes were the old frontend and are not reproducible by design.

**The hash route.** The same decision, seen from the address bar: the
reference recorded `location.hash` as `#<volume>/<world>` on every step; a
world here has a real URL and there is no fragment to read. What the field
stood for — which volume and world are open — never went unchecked; it is
carried by the page, its island and its selects.

**Lenses are addressed by name.** The reference addressed a lens by ordinal,
so its select's option values were `0`, `1`, `2`. A name outlives a build's
ordering, and `POST /session/lens` takes one (app.md §4.3), so the same
select carries `Viking MDIM 2.1` where the reference carried `1`. How many
lenses are offered, which is current and what it draws were never waived.

**Seam assets 404 without a mount.** The recorded `/static/app.js` and
`app.css` bodies were the reference's built frontend. The seam is a separate,
deletable lane (issue #5 §3.2): a host that mounted no static tree answers
404, and that refusal is the deletability principle doing its job.

**The enriched revision.** The reference merged inside composition and wrote
the plain policy revision; issue #5 §5.3 requires the enrich write to bump
the revision past the serving build's, which enrich.md §2 does by packing the
policy into the high field (9 becomes 109). The revision feeds the manifest
and the manifest feeds the stamp, so enriched stamps differ by construction
— a naming difference, never a content one.

**The DOM node count.** The reference recorded a count of every element on
the page as a runaway-render tripwire. This page is a different page —
server-rendered regions, two custom elements, an import region and a
shortcut block the reference never had — and reproducing the count would
mean reproducing the markup, which is the opposite of what a clean room is
for. The tripwire's job is done by the canvas and dock-row counts.

**The fit across an opening.** The reference fitted a newly opened world's
camera one layout frame early, before the browser had told the map its panel
had folded — so a ground reached from a folded panel and the same ground
reached from an open one recorded two different `fitZoom` values, one of
them measured against a window that no longer existed. Here a page arrives
with its panel already in its final state, so the fold and the fit are one
layout and the fit is measured against the window the reader actually gets.

**The chart camera under the sphere.** The reference wrote its whole
arrangement — the chart's camera included — on every interaction, even while
the globe stood in front of a chart with no window, which "fitted" the held
cell into a window of no size and saved the deepest zoom the lens allows.
Here the camera is reported by the pane the reader is looking through
(render-seam.md), so the saved camera is the one the reader actually left
the chart at.

## Consequences

Anyone re-deriving behavior from the reference tree — it is archived, with
its capture programs, on the `golden-reference` tag and the
`archive/golden-reference` branch — should expect exactly these eight
disagreements and no others as of the promotion. New divergences after the
promotion are ordinary design changes and get ordinary decision records.
