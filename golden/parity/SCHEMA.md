# The parity snapshot, as a contract

A parity baseline is one file per fixture volume: `golden/parity/<slug>/tour.json`.
It is the record of one walk through the application's user-reachable
surface, taken from a fresh launch against a pinned library of bundles. The
current tree produced these files and is the golden reference; a candidate
build passes when its own walk agrees with them.

This document says what each field means and what "agrees" means. A reviewer
should be able to read it and know whether a candidate is correct without
reading the old implementation. The fixture volumes themselves — which
bundle, which stamp, why that one — are in `FIXTURES.json` beside this file.

---

## 1. The file

```jsonc
{
  "volume":   "mars",          // the volume the run was pinned to
  "viewport": [1280, 720],     // the browser window the walk happened in
  "problems": [],              // checks the tour ran on itself; a baseline has none
  "steps": [ { "name": "...", "snapshot": { ... } }, ... ]
}
```

`problems` is not a diff artifact. The tour checks a handful of invariants as
it goes — the map, the footer and the dock counting the same features; a
closed card giving back the view a jump borrowed; the sphere and the chart
losing the same pins to one filter — and a run that finds any of them broken
has *failed*, whatever it recorded. **A baseline with a non-empty `problems`
is not a baseline.** The invariants are listed in §4.

`steps` is ordered and compared positionally: `compare.mjs` first checks that
the two step-name lists are identical, then diffs snapshots pairwise by name.
A candidate that skips a step, adds one, or reorders is a difference before
any field is read.

---

## 2. Which steps exist

Step names are stable identifiers, not descriptions; renaming one is a
breaking change to the contract.

The tour is written so that a step is emitted only when the volume can reach
it. A volume with one lens has no `variant-second`; a volume with no shape
collections has no `zone-*`; only a sphere has `globe-entered`. So the step
list is a property of the *volume*, not of the build — which is exactly why
there is one baseline per volume and why the fixture set spans the format's
shapes. The step lists as captured:

| group | tunic | cyberpunk-2077 | fallout-new-vegas | zelda-totk | mars | bend-or |
|---|---|---|---|---|---|---|
| core (selection, search, filters, sections, grid, chrome, zoom, overview) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `variant-*` (lens switching) | – | ✓ | – | ✓ | ✓ | – |
| `map-*` (world switching) | – | – | ✓ | – | – | – |
| `zone-*`, `collection-*`, `filter-highlight-*`, `dock-*` (shape collections) | – | – | – | ✓ | – | ✓ |
| `and-highlighted`, `labels-held-highlighted`, `and-cleared` (two feature indexes) | – | – | – | – | – | ✓ |
| `labels-flipped/curated`, `label-*` ladder | – | ✓ | – | ✓ | – | ✓ |
| `point-labels-*` (a point collection with a label toggle) | – | ✓ | – | ✓ | – | – |
| `globe-*` | – | – | – | – | ✓ | – |
| `globe-offered`, `library-*`, `import-*`, `catalog-*`, `label-ladder-initial` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **total steps** | **37** | **49** | **39** | **67** | **59** | **66** |

The city is the only fixture whose ladder starts with anything silent:
`label-ladder-initial` records three collections quiet by curation there
(`atlas.label.policy=quiet` on Watersheds, Subwatersheds and Waterbodies)
against an empty `silent` on every other volume, so `label-override-set` and
`label-silenced-held` are read over a real curation rather than over a ladder
that only ever said yes. `point-labels-*` is nevertheless absent there: a
point collection is offered the toggle only where its curation drew it as
text (`atlas.render.as=text`), and the city's one point collection is drawn
as pins, so the row has no button to press.

Some steps are unconditional anchors even where the feature is absent —
`globe-offered` records `pane.globeOffered: false` on a plane, and
`label-ladder-initial` records an empty ladder on a volume with no label
toggles. They exist so that a build which *starts* offering a globe on a
plane shows up as a changed field rather than as a changed step list.

### Steps added in this milestone

Everything through `game-first` is the pre-existing tour, unchanged in name
and in order. The blind-spot groups are **appended** after it, so every
earlier step keeps its position and a pre-existing baseline can still be read
positionally against a prefix of a new one:

- **globe pane** — `globe-offered`, then on a sphere: `globe-entered`,
  `globe-zoomed-in`, `globe-zoomed-deep`, `globe-labels-held`,
  `globe-labels-released`, `globe-lens-second`, `globe-lens-first`,
  `globe-grid-open`, `globe-grid-descended`, `globe-grid-closed`,
  `globe-collection-shown`, `globe-collection-hidden`,
  `globe-collection-restored`, `globe-selected`, `globe-detail-closed`,
  `globe-zoomed-out`, `globe-left`, `globe-parked`, `globe-reentered`,
  `globe-returned`.
- **library: import and reconcile** — `library-initial`, `import-refused`,
  `catalog-reconciled`, `catalog-reconcile-filtered`,
  `catalog-reconciled-filtered`, `catalog-reconcile-cleared`.
- **label policy** — `label-ladder-initial`, `label-override-set`,
  `label-silenced-held`, `label-silenced-released`, `label-override-dropped`,
  `label-ladder-all-flipped`, `label-ladder-restored`.

One pre-existing step changed meaning slightly and is documented rather than
renamed: `game-second` used to switch to the second volume on the select and
now switches to the first volume that is not the one open. With no volume
pinned the two are the same volume, so the older behaviour is a special case
of the new one.

Which volume that lands on is a property of the library, not of the build.
The select sorts by slug, `bend-or` sorts first, so every fixture but the
city now switches to the city and the city switches to `cyberpunk-2077`. The
five game and planet baselines moved at exactly three places when the city
joined the library — `library.volumes` and `domNodes` on every step, and the
`game-second` step itself, which changed destination — and nowhere else.

---

## 3. The snapshot

A snapshot is two things merged: the application's own diagnostics object,
and what the harness saw for itself from outside. The split matters for a
rewrite. The first half is whatever the application chooses to publish about
its internals; the second half is read off the DOM, off `localStorage`, and
off the seams the application opens for the harness by name — so it survives
the internals being replaced, and it is the half a candidate build must
reproduce **key for key** even if its internals look nothing alike.

### 3.1 The application's diagnostics (`render_game_to_text()`)

| key | type | meaning |
|---|---|---|
| `coordinateSystem` | string | constant banner naming the pixel convention |
| `volume`, `world`, `lens` | string | titles of what is open |
| `zoom`, `center`, `resolution` | number, [x,y], number | the chart camera, unrounded |
| `nativeMaxZoom`, `maxZoom` | number | the lens's own depth, and the depth the view allows over it |
| `interpolate` | boolean | whether the raster is smoothed past its native depth |
| `tileStats` | `{requested, loaded, errors, peakPending}` | tiles since the lens was chosen — see §5 |
| `pins`, `eligibleLocations` | number | features in the registry; those the active shard admits |
| `domNodes`, `canvases` | number | element and canvas counts |
| `rasterCacheSize` | number | constant |
| `labelsHeld` | boolean | Z is down |
| `hoveredPin`, `selectedPin` | string\|null | titles |
| `fitZoom` | number | the zoom at which the whole map fits |
| `filters.hiddenCategories` | string[] sorted | collection ids the reader hid (all kinds, despite the legacy name) |
| `filters.collapsedSections` | string[] sorted | folded legend sections |
| `ui.*` | mixed | sidebar, detail card, search, solo chip, footer text, overview and dock flags, subgrid |
| `zones.*` | mixed | shape collections: whether any is visible, how many records, the focused one, the highlighted ones, and how many pins pass the zone filter |
| `sync.*` | mixed | the three-surface agreement: `drawn`/`listable` from the model, `footerText`/`dockText`/`dockFlag`/`dockRows` as rendered, `searching` |
| `grid.*` | mixed | cell system: `enabled`, `system`, `prefix`, `maximumDepth`, `extent`, the `cells` drawn, `priorityPins` |

### 3.2 What the harness observed (`pane`, `library`, `labels`, `session`, `route`)

**`pane`** — which pane is up and what the sphere has drawn. On a plane every
globe field is its empty value, and that is a fact worth pinning.

| key | type | meaning |
|---|---|---|
| `globeOffered` | boolean | the toggle is on screen: the world declares a sphere with an invertible mapping |
| `globeActive` | boolean | the toggle reads pressed |
| `chartHidden`, `globeHidden` | boolean | the two panes; exactly one is up whenever a globe exists |
| `globeBuilt` | boolean | the globe seam exists — i.e. the sphere has been entered at least once this session |
| `detailLens` | string | the pyramid the neighborhood under the camera is drawn from |
| `detailTiles` | number | tiles currently draped under the camera |
| `detailZoom` | number\|null | the pyramid level those tiles are from; `null` when the base skin alone is showing |
| `gridCells` | number | cell boundaries, fills and chips on the sphere |
| `gridCell`, `gridFitKey` | string\|null | the cell being held to, and the frame the camera was flown to hold it |
| `labelKey` | string | the sphere's own rounding of its camera — `lat:lng:altitude:system:cell` — non-empty only while Z is down |
| `labelSprites` | number | names raised over the sphere (budgeted at 180) |
| `pinSprites` | number | sprites the sphere has ever built |
| `visibleSprites` | number | sprites a filter is currently letting through — the globe's answer to the footer's count |
| `overviewShelfHidden` | boolean | the corner locator is put away |
| `reticle` | string | the locator's box in whole pixels, `"left top width height"`, **only while the sphere is up**; `""` otherwise — see §5 |

**`library`** — the chrome's own account of what can be opened: `volumes`,
`worlds`, `lenses` (the option values on each select, in order), the current
value of each, `lensFieldHidden`, `emptyStateHidden`, `addBundlesDisabled`,
`emptyOpenDisabled`.

`library.volumes` is the fixture library itself, so **adding or removing a
fixture volume changes every baseline** and is a re-capture, not an edit.

**`labels`** — the label-policy ladder as rendered: `speaking` and `silent`,
each a sorted array of collection ids taken from the `aria-pressed` state of
every label toggle in the legend. This is deliberately read off the buttons
rather than off the model: a build whose model flipped and whose button did
not is wrong, and this is where it shows.

**`session`** — the saved arrangement, read from `localStorage` under
`atlas.session.v3`. `{ last, entry }` where `entry` is the record for the
volume named by `last`, or `null`. The entry carries `volume`, `world`,
`lens` (index), `center` (rounded to whole world units), `zoom` (3 decimals),
`hidden`, `collapsed`, `expanded` (sorted id arrays), `labels` (the override
ledger, each `"<collectionId>=<always|quiet>"`, sorted), and
`overviewDocked` / `dockFolded` / `dockDismissed`.

For the rewrite this is the field to watch: the issue's §6 calls for the new
app to emit "server session state as a JSON island … matching golden key
names". *These* are the key names. Where the arrangement is stored is the new
app's business; what it contains is not.

**`route`** — `location.hash`, i.e. `#<volume>/<world>`.

---

## 4. What the tour asserts about itself

These run on every step or at the step named, and a failure fails the run.

Every step:
- the footer's text, the dock's text, the dock's rows and the model's
  `drawn`/`listable` counts all tell the same story (`checkSync`); the dock
  list is capped at 100 rows and the count above it never is.

Named steps:
- `dock-returned` — closing a card gives back the view the jump borrowed.
- `dock-kept-view` — unless the reader steered in the meantime, in which case
  the card closes on the view they steered to.
- `globe-entered` / `globe-left` — the toggle's state and the two panes agree.
- `globe-left` — a put-away globe keeps no pyramid tiles.
- `globe-zoomed-deep` — past the base skin's depth, tiles actually arrive.
- `globe-labels-released` — letting Z go drops every name.
- `globe-lens-second` — a lens swap changes which pyramid the sphere reads.
- `globe-collection-hidden` / `-restored` — one filter culls the sphere's
  pins and gives them back.
- `globe-returned` — a flip to the sphere and straight back lands the chart's
  camera exactly where it was. This is the round trip through the declared
  equirectangular mapping and the altitude/zoom pairing, in both directions.
- `import-refused` — a refused import changes nothing on offer and leaves its
  button enabled.
- `catalog-reconciled` — reconciling against an unchanged catalog moves
  neither the reader nor the camera.
- `catalog-reconciled-filtered` — nor does it spend a filter the reader set.
- `label-override-set` — a flip that disagrees with the curation is recorded
  in the session.
- `label-silenced-held` — holding Z does not revive a collection the reader
  silenced. Z reveals what is optional, never what someone chose to quiet.
- `label-override-dropped` — a flip back to the curated word drops the
  override rather than storing it, and the ladder returns to its start.
- `label-ladder-restored` — turning every toggle over and back restores the
  ladder exactly and leaves no overrides behind.

---

## 5. Comparing: what binds and what does not

Run `frontend/parity/compare.mjs baseline.json candidate.json`. Every leaf of
every snapshot binds, **except**:

```
--ignore tileStats.requested,tileStats.loaded
```

Two fields, both counting tiles fetched since the lens was chosen. They
measure the *route* two runs took to the same destination rather than the
destination: a browser that samples one fly-to a frame differently fetches a
tile the other never wanted, and no amount of settling undoes a request
already made. Across every paired run taken here — each of the six public
fixtures captured twice from two fresh launches, twice over — nothing else
ever moved. The fields stay recorded — a candidate that fetches four times as
many tiles is worth seeing — they are simply not equality-checked.
`tileStats.errors` and `tileStats.peakPending` bind as usual.

Two other things were unstable and were **fixed rather than ignored**, which
is the rule:

- Sub-pixel drift in the overview reticle: rounded to whole pixels, the same
  discipline the camera check already used.
- The chart's reticle being a stale leftover: the overview memoizes on the
  view's extent rounded to whole world units but writes its box from the
  unrounded extent, so two runs that finish a fly-to a fraction of a unit
  apart share the memo key and each keeps whichever box its own last frame
  wrote. The reticle is therefore reported only while the sphere is up, where
  it is recomputed on every camera event and is the only written form of the
  globe's camera. Over the chart the camera is in the snapshot proper and
  says the same thing without the staleness. *(This is a real, if minor,
  defect in the current tree, carried here as an observation rather than
  fixed — the golden reference is not edited.)*

Determinism is a checked property, not a claim: `capture.mjs --twice` takes
each baseline twice from two fresh launches and diffs the pair. All six
public baselines are identical across every step under the ignore list above.

---

## 5.1 Walking the tour against the rewrite

`golden/parity/compare.mjs` is the gate; `golden/HARNESS.md` says how to run
it. Two things about it belong here, because they are contract rather than
plumbing.

**The reading half was re-pointed; the values were not.** The page underneath
is a different page, so a handful of questions are asked of different
elements: the panes are `<atlas-chart>` and `<atlas-globe>` rather than `#map`
and `#globe`; the locator's shelf is `#atlas-overview`; the arrangement is
read from the server's JSON island rather than from `localStorage`; and the
empty-library card is a page of its own, so on an explorer page there is no
`#empty-state` element to ask — an absent card is recorded as a card that is
not shown, which is the same fact the reference recorded as `hidden`. Every
one of those is written down at the read in `golden/parity/tour.js`. No value
was redefined, and where a value genuinely cannot be equal it is a waiver in
`golden/waivers.json`, never an edited baseline.

**`refreshCatalog` is gone and needed no replacement.** The reference opened
`__atlasDebug.refreshCatalog()` for the harness. The rewritten page already
re-reads its own URL whenever the library moves under it (docs/app.md §5), so
the tour raises the event the application listens for instead of calling into
it — which drives the real reconcile through the real wiring rather than a
seam that existed only to be driven.

---

## 6. Reproducing a baseline

```sh
npm --prefix frontend ci && npm --prefix frontend run build   # assets/app.js carries the tour
node golden/parity/capture.mjs --twice --verify               # all six, each twice, hashes checked
node golden/parity/capture.mjs --only mars --twice            # one of them
```

`capture.mjs` links the exact fixture files named in `FIXTURES.json` into
`golden/parity/.bundles` and points the application at that directory, so the
registry's newest-wins fold has nothing to choose between and the library is
the same everywhere. Five of the six come from the installed library; the
city is built rather than installed, so its entry carries `builtInto` — the
repository-relative directory the pipeline wrote it to, `dist/bundles`, with
`ATLAS_GOLDEN_CITY_DIR` overriding it exactly as in
`golden/capture/capture.sh`. A missing city bundle stops the run and names
the four commands that build it rather than capturing five volumes and
calling it the set. Each tour is its own application process and its own
browser session — the fresh-launch rule holds by construction — and the tour
clears the saved session at both ends.

The tour is also still reachable by hand: F9 in a running development build
walks the same steps against whatever library is installed and posts the log
to `/parity/result`.

---

## 7. Gaps

Things the blind-spot work could not reach, recorded here rather than
papered over.

**The import picker is native and cannot be driven headlessly.** `POST
/data/bundles/import` raises a macOS file dialog through the window handle;
a headless run has no window, so the route answers `503` with
`{"problems": ["the application window is not ready"]}`. What the tour
captures is therefore the *refusal* and the reconcile the import ends with —
`import-refused`, then the `catalog-*` steps driving the same reconcile by
hand through the seam the application opens for exactly this. The choosing
and copying half of the flow is covered by `internal/bundle`'s own tests
(`registry_test.go`), not by a parity baseline. **If the rewrite gives import
an HTTP route that takes paths, this gap closes and these steps should be
extended rather than left as they are.**

**A catalog that actually changed cannot be reached headlessly.** The
reconcile has three paths: same stamp (captured), the open volume's bundle
replaced by a newer build, and the open volume's bundle gone. The last two
need the directory to change under a running application, and the headless
build does not run the directory watcher — nothing tells the page to look
again, and the registry does not rescan on its own. The two uncaptured paths
are where the interesting behaviour is (the arrangement carried across a
swap; landing somewhere that still exists after a departure), and they are
the first thing to capture if the new app can be told to rescan over HTTP.

**The empty library has no baseline.** `library.emptyStateHidden` and
`emptyOpenDisabled` are recorded on every step, but always from a library
with volumes in it; a tour against an empty directory cannot start, because
there is no volume to open. A candidate's empty state needs a test of its
own.

**Almost nothing checks pixels.** Every observation is a count, a flag, or a
string. A build that draws every pin in the wrong colour passes this tour. The
raster-level goldens (per-lens tile inventories with decoded-pixel digests,
issue §6.1) are a separate instrument and are not this one.

One thing was added after this hole was walked through rather than around.
The rewritten application painted its own backdrop over the pane, so the
renderer drew a whole world nobody could see — and every field in every
snapshot agreed that it had, because a count of what is drawn is not a
question about what is on screen. `checkCanvas` now asks two questions on the
first step of every run: whether the pane's own canvas is the thing at the
middle of the map, and whether it carries more than one flat colour. It is an
invariant the tour checks on itself, not a value compared against a baseline,
which is the only shape a pixel check can take here: there is no golden
screenshot and there should not be.

**One coverage hole belongs to the fixture set, not to the tour.** The globe
is reachable on exactly one fixture, Mars, because it is the only volume
declaring a sphere. Every `globe-*` assertion rests on that one volume's
data.

### The two gaps the rewrite could close

Both of the headline gaps above were written when the reference was the only
implementation. The rewritten application changes what is reachable, and the
change is recorded here rather than acted on, because extending a step is a
re-capture and a re-capture is not this milestone's to do.

**Import over HTTP.** `POST /bundles/import` still raises a native picker
through `hostenv.PickFile`, and `atlas serve` answers `ErrNotAvailable`, so
`import-refused` is still the refusal it always was. But the rewrite *can*
rescan: `VolumeStore.Rescan` exists, an import calls it, and it is the thing
the `catalog` event is raised by (docs/app.md §1.1, §5). A host route that
took a path — the shape issue #5 §6's gap note asks for — would close this
gap and the `import-*` steps should then be extended rather than left. Doing
it now would move six baselines to prove a route nobody has written.

**A catalog that actually changed.** Same answer, and closer than it was. The
reference could not be told to look again; the rewrite can, and the reconcile
the tour drives today goes through the application's own `catalog` wiring
rather than through a harness-only hook. The two uncaptured paths — the open
volume's build replaced, the open volume's bundle gone — need only a way to
put a file in the library and say so, which is the same missing route. They
remain the first thing to capture, and they are now a small thing to reach.

---

## What the rewrite has not reproduced yet

The gate is wired and walking. Every volume's step list matches its baseline
and the tour's own invariants hold; what remains is a residue of fields, and
`parity-compare` is declared unready until it is empty. These are behaviours
to build, never baselines to edit.

**Closed in this milestone**, recorded so the shape of the work is legible:
the camera reaching the island; the label ladder offered only where a producer
curated one; the reconcile leaving the reader and their filters where they
were; the panel coming out on a search; the camera flying to a feature reached
for by name and giving the view back when the card closes; a shard narrowing
the ground as well as the points; the held cell narrowing what is counted and
listed; the bulk unfold. Each of those was one defect, and most of them were
one defect standing in front of several others.

**Still open**, as the diff reads them:

- **The last bit of a float.** After a fit with easing the chart's `zoom` and
  `resolution` differ from the recording in their final unit of least
  precision — `1.687722075146357` against `1.6877220751463569`. It is the same
  arithmetic in a different order, not a different answer, and it moves the
  in-view half of the footer by one feature where a pin sits exactly on the
  window's edge. It wants either an arithmetic that lands identically or an
  argument, written down, for comparing a camera at the precision a camera is
  worth.
- **The sidebar's own refit.** Collapsing the sidebar widens the map, and the
  reference re-fitted; this build keeps the camera and only updates the size,
  so the centre stays where it was.
- **`fitZoom` after a volume is reopened.** The reference kept the zoom its
  last fit produced; this build recomputes the fit for the lens.
