# The render seam

**Status: normative for the lane.** This document is written to one standard:
someone who has never seen `render/` should be able to delete it and write it
again from this document, `docs/format.md`, `docs/app.md` and
`docs/analysis.md`. That is what issue #5 §5.5 means by deletability — not a
CI job that removes a folder, but a lane whose contracts are written down well
enough that the code is replaceable.

The implementation is `render/`. Where this document and the code disagree,
take it as a defect in one of them and say so.

---

## 1. What the seam is

The seam **pictures a volume**. It is a typed TypeScript application, built by
esbuild into one JavaScript file, loaded by one `<script type="module">` tag,
and it renders into two custom elements. It is the only client-side code in
the application.

It stands on **three published contracts and one duty**:

| | Where it is written | What it gives the seam |
|---|---|---|
| the `/data` plane | `docs/format.md`, `golden/fixtures/` | worlds, packed locations, prose, tiles, icons |
| the scene description | `docs/app.md` §4, and §3 below | what to draw and in what arrangement |
| the analysis API | `docs/analysis.md`, `analysis/` | cell systems: plans, rings, style tokens |
| the diagnostics duty | `golden/parity/SCHEMA.md`, and §8 below | what the seam must publish about itself |

And on nothing else. It never imports the application, never reads a Go type,
never learns a route beyond the two it posts to, and **nothing imports it**
(`golden/depcheck` and the ESLint boundary rules both say so).

### 1.1 The rules that keep it that way

- **Data flows one way.** Server → scene description → seam. Two things flow
  back and no more: the `atlas:pick` DOM event and the debounced camera
  report (§5).
- **Fetch lives in one place.** `render/data/` owns every network call. An
  ESLint rule fails the build otherwise, naming this contract.
- **No bare `console.*`.** The `log` module is the browser end of the one
  event stream (`docs/logging.md`), and the headless parity runner captures
  it.
- **No UI framework.** The dependency surface is pinned: OpenLayers,
  globe.gl + three, and `@atlas/analysis` (which brings s2js). It grows only
  behind a green parity tour.
- **`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, ESM,
  no `any`** outside a typed browser-API escape hatch.
- **~3,000 authored lines** is the guideline. `render/tools/lines.mjs` counts
  and warns; it never fails a build. Today: 2,800 code lines, 992 of prose,
  tests counted separately.

### 1.2 Progressive by construction

Until the bundle loads, `<atlas-viewport>` is an unknown element: it renders
nothing and breaks nothing. The application must serve, and every non-viewport
test must pass, with the seam's assets absent — `/static/app.js` then answers
`404`, which is what `golden/waivers.json`'s `seam-assets` entry asserts. This
is the deletability principle standing up in the build order.

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
| `#globe-toggle` | rendered by `topbar.tmpl` | binds the click; flips the panes; writes `aria-pressed` |
| `#labels-hint` | rendered by `viewport-surface` | writes the held-key hint text |

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
override applied (`docs/format.md` §6.2) — every fixture volume overrides it,
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

**`atlas:pick`** — a `CustomEvent`, bubbling and composed, dispatched from the
pane that resolved the hit:

```js
new CustomEvent("atlas:pick", { bubbles: true, composed: true,
  detail: { feature: "1849", kind: "point" } })   // kind: point | path | area
```

The seam resolves the hit and submits nothing. The page's one glue listener
turns it into an ordinary `POST /session/select`, and the new selection comes
back as a new scene. An empty `feature` is a click on nothing.

**The camera report** — `POST /session/view`, form-encoded
(`volume world x y zoom rotation`), debounced 400 ms after the camera settles,
answered `204`. It exists so a volume reopens where the reader left it. The
server stores it and hands it back in `data-camera`; it never reasons about
it.

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

is recorded on every step of every parity baseline. Never reword it.

**The camera fits `bounds`**, falling back to the whole world square — never
`surface`. A lens may declare both, and they are not interchangeable: `bounds`
is the raster window the pyramid fills, `surface` is the ground that window
pictures, which on a split sheet is smaller because the window was grown to
take in a title drawn beside the map. The reader is shown everything the lens
drew; `surface` is what anything *dividing* the world measures, and that
reading belongs to the analysis lane's `Ground`. Tunic is the volume that
tells them apart, and its baseline settles it.

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
doubles every request a fresh view makes, which is a number the baselines
record.

**Coverage is consulted before every request** (`docs/format.md` §6.3.1): a
level with no bitset is fully covered *inside the lens's window*, and outside
that window there is nothing. A denied tile returns no URL, and the parent is
drawn larger instead. `render/test/pyramid.test.ts` reproduces all twelve
golden tile inventories from bounds, coverage and formats alone.

**Two overzoom levels** sit past the deepest tiles: the view's `maxZoom` is
`lens.maxZoom + 2`, and there the base layer's own tiles are drawn larger.
`lens.interpolate` decides how — smooth for a photograph, nearest-neighbour
for pixel art, because a hand-drawn map magnified with bilinear smoothing
stops being a drawing. *Every lens in the public fixture set declares `true`,
so the nearest-neighbour branch is pinned by no golden.*

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
and `pane.globeBuilt` in the baselines is exactly the question "does the
`__atlasGlobe` global exist yet".

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

**The camera round trip** is the pane's one contract with the chart. The
pairing is invertible arithmetic through the declared mapping:

```
lat, lng          = mapping.toLatLng(camera.x, −camera.y)
degrees visible   = resolution · viewportHeight / equirect.px.h · |north − south|
altitude          = degrees / 180 · 2.5          (2.5 = globe.gl's own full view)
```

and back the other way. A flip to the sphere and straight back hands the chart
the camera it was given — the same numbers, not numbers that round-trip to
within a float of them — unless the reader actually moved the sphere, in which
case the inverse is taken honestly.

*The constant 2.5 and the locator rectangle the globe writes are **not**
calibrated against the reference implementation's own pairing; see §10.*

---

## 8. The diagnostics duty

The seam publishes what only a renderer can know, under the names
`golden/parity/SCHEMA.md` records. A snapshot is two halves: the application's
(the session island, `docs/app.md` §6) and the seam's. Neither half guesses at
the other's keys.

### 8.1 The globals

| Global | Shape | Who reads it |
|---|---|---|
| `__atlasSeam.snapshot()` | §8.2 | the canonical entry point |
| `__atlasDebug.snapshot()` | the same object | the recorded tours, which call this name |
| `render_game_to_text()` | JSON of `__atlasAppDiagnostics()` merged under the seam's snapshot | the recorded tours |
| `advanceTime()` | forces a synchronous frame | the tours' settle loop |
| `__atlasGlobe` | §8.3 — **absent until the sphere is built** | the tours' `pane` half |

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
vector sources, chosen path first and dimmed context after, which is the order
the baselines record. Every planned cell appears whether or not it painted:
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

- **The parity tour is wired and not yet green.** `golden/parity/tour.js` reads
  this lane through the ids above and walks every recorded step;
  `golden/parity/compare.mjs` is the gate. What it still shows is listed in
  `golden/parity/SCHEMA.md` §7, and most of it is the application's half
  rather than this one's.
- **The reconcile path is verified by hand, not by a tour.** Patching the state
  node in place and replacing it whole both land: a search narrows the
  standing set, a `<data>` child hides a collection, a replaced node plus one
  `atlas:rescan` opens the grid on the held cell with the extent the Mars
  baseline records, and the selected feature survives the cell's cull. What is
  *not* verified is the same journey through the application's own controls,
  because the legend's checkboxes currently post without a volume and answer
  `400` — an application-side defect, not the seam's.
- **The globe's altitude pairing is calibrated.** It is a power law anchored at
  one point, not a field-of-view calculation:

  ```
  altitude(zoom)     = clamp(2.5 / 2^(zoom − 2), 0.08, 4)
  zoom(altitude)     = clamp(2 + log2(2.5 / max(altitude, 0.04)), 0, lens.maxZoom + 2)
  one zoom press     = altitude ÷ 2^±1, read off the camera, eased over 180 ms
  ```

  The whole disc at altitude 2.5 reads as the whole chart at zoom 2, and each
  halving of altitude reads as one more zoom. The Mars baseline settles it
  twice over: `globe-left` records a chart zoom of 1.3219, which is exactly
  `2 + log2(2.5 / 4)` after the camera has been pushed out to the farthest
  distance, and `globe-labels-held` records an altitude of 0.68, which is
  `2.5 / 2^(1.8826 − 2)` halved twice — two halvings for three presses,
  because two of them land in one tick and both read the same standing
  altitude. The clamps are load-bearing rather than hygiene: the farthest is
  what `globe-left`'s recorded zoom is a reading of.

  The locator's mark is calibrated with it, and it is a **dot, not a
  rectangle**: a fixed 22 × 22 box centred on the point the camera faces,
  written in the canvas's own pixels. That is what makes `globe-entered`'s
  `"117 53 22 22"` fall out — the world square is 8,192 across, the locator
  composites it at 256 pixels, and a camera over the middle lands at
  `0.5 × 256 − 11 = 117`. The box never changes size however close the camera
  comes, because half a sphere is out of sight whatever the camera does and a
  true-to-scale rectangle would be saying something false.
- **Nearest-neighbour resampling is pinned by no fixture.** Every public lens
  declares `interpolate: true`.
- **Hover, picks and the detail card** are implemented and unverified against
  a baseline: they need the tour.
- **The seam's bundle is 2.3 MB.** globe.gl brings three, d3 and turf. It is a
  desktop application served from its own binary, so this is a note rather
  than a problem — but it is the obvious thing to measure first if it ever
  becomes one.

## 11. Where each named behaviour lives

| Behaviour (issue #5 §5.5) | Module | Verified by |
|---|---|---|
| `ATLAS:PIXELS` projection | `chart/projection.ts` | `test/projection.test.ts`, smoke |
| 12-layer z-order | `chart/element.ts` | reading; the tour |
| overzoom + parent upsampling | `chart/raster.ts` | `test/projection.test.ts`, smoke |
| coverage bitsets honoured | `data/pyramid.ts` | `test/pyramid.test.ts` (12 inventories) |
| nearest vs smooth | `chart/raster.ts` | payload-faithful; no fixture |
| priority declutter + bypass | `chart/styles.ts`, `world/visibility.ts` | `test/visibility.test.ts` |
| zone scrims, even-odd | `chart/element.ts` | reading; the tour |
| shard-crossing view carry | `chart/element.ts` | `test/visibility.test.ts` (the shard half) |
| the corner overview | `chart/overview.ts` | smoke |
| globe equirect compositing | `globe/texture.ts` | smoke |
| detail-tile + label budgets | `globe/element.ts` | smoke (0 on entry, 180 held, 0 on leave) |
| camera round trip | `globe/element.ts` | smoke (exact) |
| grid token vocabulary | `chart/grid.ts` | `test/grid.test.ts` against two recorded steps |
