# The render seam

**Status: normative for the lane.** This document is written to one standard:
someone who has never seen `render/` should be able to delete it and write it
again from this document, `docs/format.md`, `docs/app.md` and
`docs/analysis.md`. That is what issue #5 §5.5 means by deletability — not a
CI job that removes a folder, but a lane whose contracts are written down well
enough that the code is replaceable.

The implementation is `render/`. Where this document and the code disagree,
take it as a defect in one of them and say so.

The standard holds for the contracts and not for the numbers: §10.1 is the
list of what a rewriter would still have to read out of `render/`.

---

## 1. What the seam is

The seam **pictures a volume**. It is a typed TypeScript application, built by
esbuild into one JavaScript file, loaded by one `<script type="module">` tag,
and it renders into two custom elements. It is the only client-side code in
the application.

It stands on **three published contracts and one duty**:

| | Where it is written | What it gives the seam |
|---|---|---|
| the `/data` plane | `docs/format.md`, `docs/app.md` §2.1 | worlds, packed locations, prose, tiles, icons |
| the scene description | `docs/app.md` §4, and §3 below | what to draw and in what arrangement |
| the analysis API | `docs/analysis.md`, `analysis/` | cell systems: plans, rings, style tokens |
| the diagnostics duty | §8 below | what the seam must publish about itself |

And on nothing else. It never imports the application, never reads a Go type,
never learns a route beyond the two it posts to, and **nothing imports it**
(`tools/depcheck` and the ESLint boundary rules both say so).

### 1.1 The rules that keep it that way

- **Data flows one way.** Server → scene description → seam. Two things flow
  back and no more: the `atlas:pick` DOM event and the debounced camera
  report (§5).
- **Fetch lives in one place.** `render/data/` owns every network call. An
  ESLint rule fails the build otherwise, naming this contract.
- **No bare `console.*`.** The `log` module is the browser end of the one
  event stream (`docs/logging.md`).
- **No UI framework.** The dependency surface is pinned: OpenLayers,
  globe.gl + three, and `@atlas/analysis` (which brings s2js). Growing it is
  a deliberate act, never a side effect of a feature.
- **`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, ESM,
  no `any`** outside a typed browser-API escape hatch.
- **~3,000 authored lines** is the guideline. `render/tools/lines.mjs` counts
  and warns; it never fails a build. Today: 3,024 code lines, 1,288 of prose,
  and 630 more of tests counted separately — 24 lines past the guideline, which
  the lane gate prints as a warning on every run. Read the number from the tool
  rather than from this sentence.

### 1.2 Progressive by construction

Until the bundle loads, `<atlas-viewport>` is an unknown element: it renders
nothing and breaks nothing. The application must serve, and every non-viewport
test must pass, with the seam's assets absent — `/static/app.js` then answers
`404` ([decision 13](decisions/0013-assets-and-static-are-two-mounts.md)).
This is the deletability principle standing up in the build order.

---

## 2. The surface

```html
<atlas-viewport state-src="#atlas-viewport-state" hx-morph-skip-children>
  <atlas-chart></atlas-chart>
  <atlas-globe hidden></atlas-globe>
</atlas-viewport>
<div id="atlas-viewport-state" hidden data-…>…</div>
```

Three elements, and the state node beside them. `<atlas-viewport>` is the host:
it reads the scene, loads payloads, computes the standing set, and hands both
panes the same object. `<atlas-chart>` is the plane; `<atlas-globe>` is the
sphere, and is present only on a world that declares one.

**The morph-skip requirement.** The application renders `hx-morph-skip-children`
on `<atlas-viewport>`, on the map pane and on the overview's canvas holder. A
swap may touch their attributes and must never reach their internals: tearing
down a WebGL context mid-gesture is not a re-render. What a swap *does* replace
is the inert state node beside them, whole (`outerMorph`), which is why the
seam re-resolves it by selector after every swap rather than holding the
element.

Two further pieces of chrome the application renders **for** the seam, and the
seam draws into:

| Element | Whose | What the seam does |
|---|---|---|
| `#overview-canvas`, `#overview-viewport` | rendered by `overview.tmpl` | draws the world once per lens; writes the camera rectangle in whole pixels |
| `#atlas-overview` | `overview.tmpl` | the shelf: marks it, and hides it when the camera has the whole world |
| `#globe-toggle` | rendered by `topbar.tmpl` | binds the click; flips the panes; writes `aria-pressed` |
| `#labels-hint` | rendered by `shell.tmpl` | writes the held-key hint text |
| `#visible-count` | `shell.tmpl`'s footer | writes the standing count — the seam's half of a number the server also renders |
| `#atlas-camera-*` | `shell.tmpl`'s hidden form | writes the five camera fields the form posts (§5) |

One more element sits in the same box and is **not** the seam's: `<div
id="map">`, rendered first by `viewport-surface`, morph-skipped, carrying the
page's backdrop, the keyboard focus and the loading scrim. In the reference
implementation it *was* the map. Here the panes are the custom elements and
`#map` is what is left, which is why paint order is load-bearing — rendered
after the panes its backdrop covers them, every count reads correct and the
page shows an empty rectangle.

Which pane is up is **seam-local state** (issue #5 §4.1): the same world, the
same filters, the same camera, seen from a different distance. The application
renders the toggle because only it knows whether the world declares a sphere;
the seam wires it because flipping moves no discrete state.

---

## 3. The scene description

`#atlas-viewport-state` is an inert node the server renders and the seam
observes. Every field is a string attribute or a `<data>` child, because the
node has to survive a morph and be readable without JavaScript running.

### 3.1 Attributes

| Attribute | Meaning |
|---|---|
| `data-volume` | volume slug |
| `data-base` | `/data/v/<slug>/<stamp12>` — the URL prefix of the serving build |
| `data-world` | world slug |
| `data-lens` | the lens's **name**, matched against the payload's `lenses[].name` |
| `data-lens-index` | its ordinal, used when the name does not match |
| `data-surface` | `plane` or `sphere` — what the world declares |
| `data-selected` | the selected feature's id, or empty |
| `data-search` | the search query, or empty |
| `data-grid-system` | the cell system's slug, or empty for "no grid" |
| `data-grid-cell` | the held cell's opaque id, or empty for the root |
| `data-subgrid` | `0` or a positive number: whether the subdivision shows |
| `data-camera` | `x,y,zoom,rotation`, absent until the seam has reported one |

### 3.2 Children

```html
<data class="hidden-collection"  value="12"></data>
<data class="highlighted-feature" value="3050"></data>
<data class="label-override"      value="39191589=quiet"></data>
```

- **`hidden-collection`** — collection ids the reader hid, of every kind.
- **`highlighted-feature`** — shape features the reader highlighted. They
  conjoin across collections and unite within one (§4.2).
- **`label-override`** — `<collectionId>=<always|quiet>`, the reader's own
  label policy where it differs from the curation.

### 3.3 How it is read

`readScene(node)` is a pure function of a node-shaped thing — it takes the
smallest structural interface a real `Element` satisfies, so it is tested over
a synthetic node with no DOM. It is **lenient**: a missing attribute is its
empty value, an unparsable camera is no camera, a malformed override is
dropped, unknown attributes are ignored.

`sceneChange(was, now)` names what moved — `volume`, `world`, `lens`,
`filters`, `selection`, `grid`, `camera` — and the seam acts on the names
rather than comparing attributes for itself. That is what makes a swap a
**reconcile**: a hidden collection restyles, a lens swap keeps the camera, a
world change rebuilds.

`SceneWatcher` keeps the two mechanisms a morph needs: a `MutationObserver`
for a node patched in place, and a re-resolve by selector for a node replaced
whole. `rescan()` is what the boot module calls after a swap.

---

## 4. What the seam computes

### 4.1 The world model

Built once per world, immutable afterwards. Points come from
`worlds/<slug>.bin` as zero-copy typed-array views (`docs/format.md` §7);
shapes come inline from the payload's collections.

**The projection nobody wrote down.** A payload spells positions as `lat`/`lng`
in the volume's own world space — which is the slippy-tile grid the capture
was cut from, not WGS 84 — and every renderer needs them as world pixels:

```
worldTiles = 2^grid.sourceZoom
xTile = (lng + 180) / 360 · worldTiles
yTile = (1 − asinh(tan(lat·π/180)) / π) / 2 · worldTiles
x =  (xTile − grid.firstTile) · grid.tileSize
y = −(yTile − grid.firstTile) · grid.tileSize
```

`grid` is the manifest's `tileGrid` with the world payload's own `grid`
override applied (`docs/format.md` §6.2) — every corpus volume overrides it,
so skipping the override puts every feature about 850,000 pixels off the map.
**This arithmetic belongs in `docs/format.md` and is not there.** It is
recorded here, and in `internal/app/world.go`, until it moves.

The sign flip is the one place the format's y-down pixels meet the y-negative
-down coordinates everything downstream speaks.

### 4.2 The standing set

One object per scene answers what stands, and the chart, the sphere, the picks
and the diagnostics all read it. The rules, in order:

1. **Shard.** A split world offers one layer at a time. A feature on another
   shard is *elsewhere in the world*, not filtered: not drawn, not counted,
   not picked.
2. **Filtered.** Its collection is hidden, or the search does not name it.
3. **Highlights.** Highlighted shapes **conjoin across collections and unite
   within one**: a pin stands only where every collection holding a highlight
   claims it. Containment is boundary-inclusive with a unit of grace; a path
   claims what lies within half its declared ground width.
4. **Cell.** A held grid cell narrows exactly the way a highlight does.

Rules 3 and 4 share one bypass: **the selected feature and anything the search
names are spared**. A card open about a feature must not lose it, and a
searched-for name is what the reader asked to see.

**Promotion** is a separate question from standing: selected, hovered,
searched, or inside the held cell. A promoted feature is drawn again in the
layer above everything and is exempt from declutter.

**Priority** is a pin's place in the crowd: `max(0, 1e6 − min(members,999)·1000)`
plus a stable hash of its id, mod 1000. Rarer collections first, ties broken
the same way on every run and in both panes.

---

## 5. What flows back

**`atlas:pick`** — a `CustomEvent` raised **on the window**, non-bubbling,
dispatched by the pane that resolved the hit:

```js
window.dispatchEvent(new CustomEvent("atlas:pick", { bubbles: false,
  detail: { feature: "1849", kind: "point" } }))   // kind: point | path | area
```

The seam resolves the hit and submits nothing: **the page turns it into an
ordinary `POST /session/select`**, and the new selection comes back as a new
scene. That is wired: the shell renders a hidden form,
`<form id="atlas-pick" hx-post="/session/select" hx-trigger="atlas:pick
from:window">`, whose one field the seam fills in before it raises the event
(`render/data/report.ts`, `internal/app/templates/shell.tmpl`). A pick off
either canvas therefore opens the card, exactly as a row in the dock does.

Two details are contract rather than taste. **The window, and no bubbling**:
there is one listener for the whole page and it is on the window, so the event
is raised where it is heard rather than left to travel — a swap can carry the
form across at any moment, and an event that depended on the path from a
canvas to it would depend on a tree the application is free to reshape.
**A miss does not post.** Clicking open water is not a request to close what
the reader is reading, so an empty feature raises nothing at all; the card is
put away by Escape and by its own button. A pick event that fired on a miss
would clear the selection on every stray click
(`render/test/pick.test.ts` holds this decision).

**The camera report** is `POST /session/view`, form-encoded
(`volume world x y zoom rotation`), debounced 400 ms after the camera settles,
answered `204` — but **the seam does not make the request**. It writes the
five values into the hidden inputs `#atlas-camera-world|-x|-y|-zoom|-rotation`
and dispatches `atlas:camera` on `window` (not bubbling); `shell.tmpl` carries
a hidden form with `hx-post="/session/view" hx-trigger="atlas:camera
from:window"` that does the posting. The seam therefore owns no request at
all, which is the stricter reading of "fetch lives in one place" and the reason
the seam never sees the answer. One report is unconditional at `show()`,
because a `view.fit()` with no animation raises no settle event and the opening
camera would otherwise never be told.

That is the whole outward surface: two DOM events and a set of hidden inputs.
No request the page did not make, and no write to the scene node, ever.

**Inbound, there is one.** After an htmx swap the seam must re-resolve the
scene node by selector, re-assert the chrome it wired, and rewrite the standing
count. `boot.ts` listens on `document.body` for `htmx:after:swap`,
`htmx:after:settle`, their htmx-2 camel-case spellings, and `atlas:rescan` —
the event a harness or a host raises to say "look again" without an htmx swap
behind it. All five call the same `rescan()`.

**Only the pane the reader is looking through reports.** A chart put away
behind the sphere still has a camera, and it is not a view of anything: the
pane has no window, so a fit into it lands wherever a fit into nothing lands,
and saving that would reopen the volume somewhere nobody ever stood. The same
rule governs the footer's count and the corner locator — a pane with no window
answers no question about what it can see. Coming back up is what makes its
camera the reader's again.

That is the whole outward surface. No other request, no other event, no
writes to the scene node, ever.

---

## 6. The chart

### 6.1 `ATLAS:PIXELS`

```
origin      top-left of the world square
x           increases right
y           decreases downward: the square is [0, −size] … [size, 0]
resolution  world pixels per screen pixel; level z is size / (tileSize · 2^z)
```

`size` is 8192 and `tileSize` is 256 in every published bundle, so level 0 is
the square in one tile and zoom 0 is resolution 32. The banner string

```
ATLAS:PIXELS; origin top-left; x increases right; y decreases downward
```

is part of the diagnostics contract (§8.2). Never reword it.

**The camera fits `bounds`**, falling back to the whole world square — never
`surface`. A lens may declare both, and they are not interchangeable: `bounds`
is the raster window the pyramid fills, `surface` is the ground that window
pictures, which on a split sheet is smaller because the window was grown to
take in a title drawn beside the map. The reader is shown everything the lens
drew; `surface` is what anything *dividing* the world measures, and that
reading belongs to the analysis lane's `Ground`.

### 6.2 The twelve layers

| z | layer | source | why it is there |
|---|---|---|---|
| 0 | `raster` | complete levels | the pyramid up to `fullZoom` |
| 1 | `rasterDetail` | patchy levels | rides on top so the complete pyramid shows through the gaps |
| 5 | `grid` | chosen cells | the held cell and its subdivision, **under** the pins |
| 6 | `zoneScrim` | one cut polygon | the dimming is seen *through*, not painted over |
| 10 | `zones` | paths and areas | the ground |
| 20 | `zoneTitles` | their names | at the curated policy |
| 40 | `pins` | markers | |
| 42 | `zonePins` | markers a highlight claimed | over the ground that claimed them |
| 44 | `zoneTitleDetail` | a revealed name | over the pins |
| 45 | `pinLabels` | names beside markers | decluttered |
| 48 | `gridContext` | dimmed neighbours | **over** the pins they dim |
| 50 | `priority` | promoted features | over everything, decluttered by nothing |

### 6.3 The raster, overzoom, and coverage

Two layers, because a pyramid is two different things. The base carries
`minZoom … fullZoom` and is complete. The detail carries everything above it
and renders **only below the complete level's resolution** — above that
threshold the base is whole, and asking the patchy pyramid for the same ground
doubles every request a fresh view makes.

**Coverage is consulted before every request** (`docs/format.md` §6.3.1): a
level with no bitset is fully covered *inside the lens's window*, and outside
that window there is nothing. A denied tile returns no URL, and the parent is
drawn larger instead. `render/test/pyramid.test.ts` reproduces every corpus
tile inventory from bounds, coverage and formats alone.

**Two overzoom levels** sit past the deepest tiles: the view's `maxZoom` is
`lens.maxZoom + 2`, and there the base layer's own tiles are drawn larger.
`lens.interpolate` decides how — smooth for a photograph, nearest-neighbour
for pixel art, because a hand-drawn map magnified with bilinear smoothing
stops being a drawing. *Every lens in the committed corpus declares `true`,
so the nearest-neighbour branch is pinned by no fixture.*

### 6.4 Scrims, labels, declutter

**Zone scrims** are one polygon over the world square with the highlighted
areas cut out of it, even-odd. Ring winding in a payload is nobody's promise,
so the outer ring and the holes are wound against each other before the fill
rule sees them.

**Label ladders** are decided by the application and drawn here. An area's
name speaks when its collection's policy is `always` — `atlas.label.policy`,
absent meaning `always` for areas — and the reader's override in the scene
wins over the curation. Holding **Z** reveals what is *optional* and never
revives what someone chose to quiet; that asymmetry is why the override is
consulted first.

**Declutter** runs on the label layer, ordered by priority: a name that has to
give way gives way to a rarer collection's, never to whichever was built
first. The promoted layer is not decluttered at all, which is the
selected/searched bypass.

### 6.5 Shard-crossing view carry

Swapping to a lens that draws another layer of the same split world **keeps
the camera exactly where it was**. The reader stepped between floors of one
building, not into another world, and a refit would throw away the place they
had found. A world change refits (or restores the reported camera); a lens
change does not.

### 6.6 The corner overview

The application renders the shelf and the two surfaces. The seam composites
the world **once per lens**, from the shallowest pyramid level whose picture
of the lens's own window is at least 168 pixels across — a lens filling a
quarter of the square goes two levels deeper for the same number of pixels —
and afterwards only the rectangle moves. The rectangle is written in whole
pixels, because sub-pixel drift between two runs of one tour means nothing.

While the sphere is up the chart has not moved, so the globe hands the locator
its own extent instead: the locator is the one place the globe's camera is
ever written down.

---

## 7. The globe

Offered only where the world declares `atlas.geometry.surface: sphere` **and**
the flattening inverts. Equirectangular is the one projection that drapes a
sphere without resampling, which is why the globe asks for it by name.

**Nothing is built until the sphere is entered.** A WebGL context, a texture
and two thousand meshes are not what a reader looking at a chart asked for,
and the `__atlasGlobe` global's absence until then is the observable (§8.3).

**Four budgets**, each a decision rather than a limit:

- **The base skin** — one 4096 × 2048 canvas texture, composited once per lens
  from the shallowest complete level worth showing.
- **The detail** — composited into that same texture, under the camera only,
  and only when the camera asks for more than the skin already has. A sphere
  seen whole shows the base skin and nothing else, which is what makes "past
  the base skin's depth, tiles actually arrive" checkable. A put-away globe
  keeps no pyramid tiles; an in-flight composite is cancelled rather than
  allowed to finish into a neighbourhood nobody is over.
- **Labels** — raised only while Z is held, at most 180, in priority order.
- **Sprites** — one per pin, built once, afterwards only shown or hidden, so a
  filter costs a boolean per pin.

**The camera round trip** is the pane's one contract with the chart. Position
is invertible arithmetic through the declared mapping; the distance is a
**calibrated power law, not a field-of-view calculation**:

```
lat, lng        = mapping.toLatLng(camera.x, −camera.y)      and toWorld back, y negated
altitude(zoom)  = clamp(2.5 / 2^(zoom − 2), 0.08, 4)
zoom(altitude)  = clamp(2 + log2(2.5 / max(altitude, 0.04)), 0, lens.maxZoom + 2)
rotation        = 0, always
```

Deriving the altitude from the viewport height, the resolution and the
equirect pixel span would be defensible arithmetic and is not what this
pairing is; the power law and its clamps are the contract (§10). The clamps
are load-bearing rather than hygiene.

A flip to the sphere and straight back hands the chart the camera it was given
— the **identical object**, when latitude, longitude and altitude have all
moved less than 1e-9, rather than numbers that round-trip to within a float of
them — and otherwise inverts honestly.

---

## 8. The diagnostics duty

The seam publishes what only a renderer can know, under stable names
(`render/diagnostics.ts` is their home). A snapshot is two halves: the
application's (the session island, `docs/app.md` §6) and the seam's. Neither
half guesses at the other's keys.

### 8.1 The globals

| Global | Shape | Who reads it |
|---|---|---|
| `__atlasSeam.snapshot()` | §8.2 | the canonical entry point |
| `__atlasDebug.snapshot()` | the same object | an alias for drivers that call this name |
| `render_game_to_text()` | JSON of `__atlasAppDiagnostics()` merged under the seam's snapshot | a headless driver |
| `advanceTime()` | forces a synchronous frame | a headless driver's settle loop |
| `__atlasGlobe` | §8.3 — **absent until the sphere is built** | the sphere's own diagnostics |

`__atlasAppDiagnostics` is the hook the application half plugs into. Nothing
in the seam defines it; if it is absent, `render_game_to_text()` is the seam's
half alone.

### 8.2 `__atlasSeam.snapshot()`

`coordinateSystem`, `world`, `lens`, `zoom`, `center`, `resolution`,
`nativeMaxZoom`, `maxZoom`, `interpolate`,
`tileStats{requested,loaded,errors,peakPending}`, `pins`,
`eligibleLocations`, `domNodes`, `canvases`, `rasterCacheSize` (64),
`labelsHeld`, `hoveredPin`, `selectedPin`, `fitZoom`,
`zones{visible,count,focused,highlighted,focusedPins}`,
`sync{drawn,listable}`,
`grid{enabled,system,prefix,maximumDepth,extent,cells,priorityPins}`.

`tileStats` counts tiles **since the lens was chosen**; `requested` and
`loaded` measure the route two runs took rather than the destination and are
not equality-checked. `grid.cells` is one object per plan cell —
`{hash, extent, role, count, contextDistance}` — read back out of the two
vector sources, chosen path first and dimmed context after, in a stable
order. Every planned cell appears whether or not it painted:
what the grid *holds* and what it *paints* are two questions.

### 8.3 `__atlasGlobe`

```ts
{ detail: { lens: string, tiles: Map<"z/x/y", true> },
  grid:   { group: Object3D | null, cell: string | null, fitKey: string },
  labels: { key: string, group: Object3D | null },
  sprites: Map<featureId, { visible: boolean }> }
```

Counts, never handles. `labels.key` is the sphere's own rounding of its
camera — `lat:lng:altitude:system:cell` — and is non-empty only while Z is
down.

---

## 9. Building and running

```sh
make seam            # esbuild → render/dist/app.js
make static          # → dist/static/app.js, the tree a -static mount wants
make serve-static    # the application with the seam mounted
make render-lane     # boundary rules + tsc + tests + line budget
npm run --silent watch --workspace @atlas/render     # atlas dev -seam-watch
```

The stylesheet is deliberately not in the static tree: the CSS is the
application's own asset, served from `/assets`, so deleting this lane costs a
page one script tag rather than its chrome.

---

## 10. What is not proven yet

Recorded here rather than papered over.

- **No browser test drives the panes.** The lane's own suite
  (`render/test/`) judges the models — projection, pyramids, visibility,
  picks, grid extents, the globe's decisions — over stated fixtures and the
  corpus; `tests/e2e` asserts arrangement, never pixels. What a real pointer
  over a real canvas does is exercised by hand, not by a gate.
- **The reconcile path is verified by unit tests and by hand, not by a
  browser tour.** Patching the state node in place and replacing it whole
  both land: a search narrows the standing set, a `<data>` child hides a
  collection, a replaced node plus one `atlas:rescan` opens the grid on the
  held cell, and the selected feature survives the cell's cull. What is
  still missing is a browser test that walks it every time.
- **The globe's altitude pairing is calibrated.** It is a power law anchored at
  one point, not a field-of-view calculation (§7):

  ```
  one zoom press     = altitude ÷ 2^±1, read off the camera, eased over 180 ms
  ```

  The whole disc at altitude 2.5 reads as the whole chart at zoom 2, and each
  halving of altitude reads as one more zoom. The clamps are load-bearing
  rather than hygiene: the farthest distance is what the whole-disc reading
  stands on.

  The locator's mark is calibrated with it, and it is a **dot, not a
  rectangle**: a fixed 22 × 22 box centred on the point the camera faces,
  written in the canvas's own pixels — the world square is 8,192 across, the
  locator composites it at 256 pixels, and a camera over the middle lands at
  `0.5 × 256 − 11 = 117`. The box never changes size however close the camera
  comes, because half a sphere is out of sight whatever the camera does and a
  true-to-scale rectangle would be saying something false.
- **Nearest-neighbour resampling is pinned by no fixture.** Every lens in the
  committed corpus declares `interpolate: true`.
- **Canvas hover is implemented and unverified.** A pointer that moves
  without pressing, and the styling that answers it, is held by no test;
  picks are (`render/test/pick.test.ts`).
- **The seam's bundle is 2.3 MB.** globe.gl brings three, d3 and turf. It is a
  desktop application served from its own binary, so this is a note rather
  than a problem — but it is the obvious thing to measure first if it ever
  becomes one.

## 10.1 What this document does not give you

§§1–9 are enough to build a seam that is *correct* — right contracts, right
flows, right arithmetic, right budgets. They are **not** enough to build one
that draws and reports exactly what this one does, because the code is full of
numbers this document does not carry. The gap is named here rather than left
for the next person to discover step by step.

What a rewriter would still have to take from `render/` itself:

- **Every drawn dimension.** Pin radii by state, rim stroke widths, icon pixel
  size and anchor, label fonts and halo widths, area fill opacities, sprite
  geometry and colours on the sphere. None of it is here, and almost nothing
  checks pixels, so a rewrite could differ in all of it and pass.
- **`atlas.icon.outset`.** §2 does not mention it; the seam reads the world's
  declaration and uses it for the pin rim. *It also uses it wrong* — the
  `light`/`dark` vocabulary is passed to the renderer as a literal colour
  string, and the colour table beside it is dead code. A rewriter following the
  semantics would draw different rims from the ones shipping today, and would
  be right to.
- **`atlas.render.as`.** The seam reads `pin` and `text` and nothing else, and
  today `text` only forces a collection's labels always-on — the "name and no
  marker" branch exists and is never reached. The registry's meaning and the
  seam's behaviour do not agree; the registry is the one to build to.
- **Search semantics.** Case-insensitive substring over a feature's `title`
  alone; a non-empty query is a hard filter on points and no filter at all on
  shapes, which nevertheless drop out of the *listable* count.
- **Hit testing.** Four CSS pixels of tolerance, resolved top-down through the
  z-order of §6.2 so a promoted pin beats a plain pin beats ground; a hidden
  feature does not swallow the hit. On the sphere it is nearest standing point
  within roughly two degrees. Hover shares the chart's rule and changes only
  the pin's radius — it deliberately does *not* rebuild the standing set.
- **Declutter is OpenLayers' own**, enabled on exactly one layer (`pinLabels`),
  ordered by the label style's `zIndex`. "Ordered by priority" in §6.4 is that
  zIndex, not a hand-written pass.
- **`members` in the priority formula** (§4.2) is the number of packed rows
  sharing a point's `owner` column — counted over the whole `.bin`, across
  every shard, before any filter. Not the standing count, not the legend's.
- **The overview's 168** (§6.6) is a *level-selection* threshold, not a canvas
  size: the canvas is the lens's window at the chosen level, which for a
  full-world lens on the standard grid is 256 px — the number §10's locator
  arithmetic quietly depends on.
- **Diagnostics that are not what their names suggest.** `fitZoom` is captured
  once, when a lens is opened fresh, and never recomputed. `zones.focused` is
  deliberately *not* emitted by the seam — the application's half supplies it
  and a seam that emitted `null` would clobber it. `grid.maximumDepth` reports
  the default system's depth even with the grid off. `sync.listable` counts
  only shapes that have a title.
- **The chart `View`'s explicit resolution ladder.** Enumerating
  `size/tileSize / 2^z` per level rather than deriving from a max and a factor
  is what makes two implementations' cameras *equal* rather than close.
- **The rest of the constants**: focus zoom 4 and the 220 ms flight, the
  "steered" thresholds that decide whether a closing card gives the camera
  back, the grid fit's 52 px padding, the 46 × 23 px test that decides whether
  a cell wears its label, the raster caches at 64, the renderer buffers, and
  the globe's texture size, detail budget and horizon arithmetic.

Two of those are contract-level rather than cosmetic and are the ones to write
down next, in this document: the outset vocabulary and `atlas.render.as`, both
of which are `format/semconv` keys whose *meaning* is normative even where the
current seam's reading of them is not.

## 11. Where each named behaviour lives

| Behaviour (issue #5 §5.5) | Module | Verified by |
|---|---|---|
| `ATLAS:PIXELS` projection | `chart/projection.ts` | `test/projection.test.ts` |
| 12-layer z-order | `chart/element.ts` | reading |
| overzoom + parent upsampling | `chart/raster.ts` | `test/raster.test.ts` |
| coverage bitsets honoured | `data/pyramid.ts` | `test/pyramid.test.ts` (the corpus inventories) |
| nearest vs smooth | `chart/raster.ts` | `test/raster.test.ts`; payload-faithful, no fixture pins the nearest branch |
| priority declutter + bypass | `chart/styles.ts`, `world/visibility.ts` | `test/visibility.test.ts` |
| zone scrims, even-odd | `chart/element.ts` | `test/element.test.ts` |
| shard-crossing view carry | `chart/element.ts` | `test/visibility.test.ts` (the shard half) |
| the corner overview | `chart/overview.ts` | reading |
| globe equirect compositing | `globe/texture.ts` | `test/texture.test.ts` |
| detail-tile + label budgets | `globe/element.ts` | `test/globe.test.ts` (the label budget); reading for the detail tiles |
| camera round trip | `globe/element.ts` | `test/globe.test.ts` |
| grid token vocabulary | `chart/grid.ts` | `test/grid.test.ts`, hand-derived extents |
