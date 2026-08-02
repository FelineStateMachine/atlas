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

## 2.1 The extended half: three step kinds

Everything above was written against a tour that could not, in principle, see
three classes of defect. Not "did not" — *could not*: there was no pointer
event anywhere in it, so a click on the canvas going nowhere was invisible; it
synthesized four keys and never asked where the focus ended up, so a shortcut
that fired while the reader was typing was invisible; and every observation
was a count, a flag or a string, so a sphere rendered black with the right
number of sprites on it was invisible. Three kinds of step close those, and
they are appended after `label-ladder-restored`, in this order: picks, keys,
pictures.

They obey the same rule as every other step: a step is emitted only where the
volume can reach it, so a volume with no shape collections has no
`pick-a-shape` and a plane has no `screen-globe`. Walked against the current
tree that is around three dozen steps a volume: 33 on tunic (37 → 70), 36 on
mars (59 → 95), 37 on the city (66 → 103). Every one of them was walked
against this tree before it was written down.

**How they are switched on, and why not with a flag.** `run.mjs --extended`
walks them; the gate asks the *baseline* whether to. `compare.mjs` looks for a
step whose name begins `pick-` in the committed baseline: while the six
baselines hold none, the gate walks exactly the tour they were captured from
and nothing about this milestone can turn it red. The moment the capture wave
of §6.1 commits baselines that hold them, every run of the gate walks all
three kinds on every volume, and there is no lever beside it to turn them off
again. `--extended` on the command line forces the walk without the baselines,
which is how a fix is checked before the wave.

### 2.1.1 Picks — a pointer, at a pixel, on the canvas

The aim is worked out through the pane's own OpenLayers map, and *only* the
aim: the click goes in as `pointerdown`/`pointerup` on the map's viewport and
travels through OpenLayers' hit detection, the seam's `singleclick` handler,
the pick form the page renders for it, `POST /session/select`, and the swap
that comes back. A target is chosen as the first feature, by id, that the pane
resolves *as itself* at its own pixel — pins overlap, and a pin clicked where
another is drawn on top is a correct pick of the wrong feature, which would
make the recorded value a property of the camera rather than of the build.

Pick steps carry one extra snapshot section, `pick`:

| key | meaning |
|---|---|
| `pick.at` | the id aimed at — a feature id, a cell hash, or `null` for a deliberate miss |
| `pick.title`, `pick.kind` | the aimed feature's title and kind (`point`, `path`, `area`, `cell`) |
| `pick.under` | what the pane says is at that pixel *before* the click — the aim's own witness. The pin, by construction; nothing, for a miss; and for a cell, whatever feature the cell happens to hold, which is the question that step asks |

What each step drives and what it records:

| step | drives | records | today |
|---|---|---|---|
| `pick-ready` | two zoom-outs, so the aim has features on screen | the camera it aims from | passes |
| `pick-a-pin` | a real click at a pin's pixel | `pick`, `ui.detailOpen`, `ui.detailTitle`, `selectedPin` | **passes** |
| `pick-missed` | a click on empty ground with the card open | the card is *still* open | **passes** |
| `pick-cleared` | the card closed | the selection put down | passes |
| `pick-a-shape` | a click inside a district or on a line | `pick`, the card on the ground's title | **passes** (volumes with shapes) |
| `pick-shape-cleared` | the card closed | — | passes |
| `pick-grid-open` | `G` | `grid.enabled`, the cells drawn | passes |
| `pick-in-grid` | a click inside a drawn cell | `grid.prefix` descends to that cell, no card opens | **awaiting fix** |
| `pick-grid-closed` | two Escapes | the grid put away | passes |

`pick-missed` is the one that pins a *decision* rather than a behaviour: a
click on nothing is not a pick and therefore is not a deselection either. A
build that closes the card on every stray click has changed what a click
means, and no count in the log would have noticed.

Aiming a miss is the only aim that has to be *found* rather than looked up,
and on the city it cannot be found at the view a walk opens on: eight thousand
features, a river system and a road network leave no gap on a lattice fine
enough to be worth scanning. So the step goes in two zoom levels first, which
spreads the same features over four times the pixels — what a reader does when
they want to click between things — and only says it could not aim when even
that fails. A step that quietly did not happen is the failure mode this whole
exercise is about.

### 2.1.2 Keys — including where the focus lands, and who else hears them

Keyboard steps carry a `focus` section: `focus.active` is the id of
`document.activeElement` (or its tag name where it has no id), and
`focus.selected` says whether the field's whole value is selected — a reader
reaching for search a second time means to replace what is in it.

| step | drives | records | today |
|---|---|---|---|
| `key-search-primed` | a word typed into `#pin-search` | the search state ⌘K will be asked to reclaim | passes |
| `key-search-focus` | ⌘K at the window | `focus.active` = `pin-search`, `focus.selected` | **awaiting fix** |
| `key-search-cleared` | the field emptied | — | passes |
| `key-grid-open` | `G` at the window | `grid.enabled` **and** `focus.active` = `grid-input` | grid **passes**, focus **awaiting fix** |
| `key-grid-descended` | a character typed into `#grid-input` | `grid.prefix` | passes |
| `key-escape-once` | Escape with the focus in the grid field | `focus.active` = `map`, and the cell **not** ascended | **awaiting fix** |
| `key-escape-twice` | a second Escape | the grid ascends | passes |
| `key-cell-system-before` / `-cycled` | ⌘G at the window | `grid.system` changes and `grid.prefix` carries across | **awaiting fix** |
| `key-grid-closed` | two Escapes | the grid put away | passes |
| `key-labels-held` / `-released` | Z down, Z up | `labelsHeld`, the ladder | **passes** |
| `key-typing-before` | — | the grid before a reader types | passes |
| `key-typing-not-a-shortcut` | `g` dispatched **at `#pin-search`** | the grid is **unchanged**, the focus stays in the field | **awaiting fix** |

The last row is the one that needed a new kind of dispatch. Every other key in
this file is raised on the window, which is where a keystroke with no focused
control is heard and where the application's shortcuts listen
(`internal/app/templates/shell.tmpl`). A reader's own typing starts at the
field and *bubbles* to the window, so a shortcut that never asks where the key
came from hears it. The window-dispatched steps must keep passing whatever
guard the fix adds — an event raised at the window has the window as its
target, which is by design and is what makes the tour's other shortcuts still
work — and this step is the only one that asks the other question.

### 2.1.3 Pictures — the driver's screenshot, compared perceptually

A screenshot step publishes what it wants shot and waits; the runner, which is
already watching the page to see where the walk has got to, takes it with the
browser's own screenshot and says so. It is taken outside the page on purpose:
a WebGL sphere has nothing to read back through a 2D context, which is exactly
why `checkCanvas` falls silent on the one pane where a blank picture has
already happened.

Pictures land in `golden/parity/screens/<volume>/<step>.png`. The step records
`screen.file` and `screen.element` in its snapshot — which picture belongs to
it, and what was photographed — and the *measurements* of the picture go in
`log.screens`, outside `steps`, recorded and not compared.

| step | element | what it would catch |
|---|---|---|
| `screen-chart` | `atlas-chart` | the pane painted over, the world drawn nowhere |
| `screen-dock-open` / `-folded` | `#atlas-dock` | the panel beside the map, out and away |
| `screen-labels-held` | `atlas-chart` | a label ladder whose sprites count right and draw nothing |
| `screen-raster-deep` | `atlas-chart` | a pixel-art lens smoothed past its native depth (`interpolate`) |
| `screen-outside-bounds` | `atlas-chart` | the background outside the lens's own bounds |
| `screen-ground` | `atlas-chart` | a multipart district with one part missing |
| `screen-globe` | `atlas-globe` | the sphere's base skin — the black-planet case |
| `screen-globe-labels` | `atlas-globe` | names raised over a sphere, facing the reader |

**The threshold, and the argument for it** (`golden/parity/pixels.mjs`). Two
numbers rather than one, because the two ways a picture moves are different in
kind and one figure loose enough for the harmless one is blind to the other.
`mean` is the average absolute difference per colour channel over every pixel;
antialiasing, a tile decoded a version apart, an easing that finished a frame
earlier all move very many pixels very little, and a sphere drawn black or a
raster smoothed moves the average by whole numbers. `differing` is the
fraction of pixels that moved more than 12/255 in any channel; a label drawn a
pixel over moves a fraction of a percent of the picture and a district that
changed colour does not. A picture passes at `mean ≤ 0.5` **and** `differing ≤
0.2%`.

Those two numbers are measured rather than guessed, in both directions. Two
fresh-launch walks of tunic taken one after the other produced six pairs of
**identical** pictures — the tour settles every step before recording it, and
a settled page draws the same frame twice — so the noise to tolerate on the
machine that takes the baselines is none, and the room left is for the machine
that did not take them. Against deliberate damage: a 60×60 patch recoloured,
which is what a district drawn in the wrong colour or a part not drawn looks
like, moves 0.76% of the picture and fails; a whole picture three shades
darker, which is what a lost texture looks like, moves the mean to 2.33 and
fails; a thousandth of the pixels moved thirty apiece, which is what edges and
a label a pixel over look like, passes. The numbers live in one exported
constant so that a run failing on noise is answered by moving a number in the
open rather than by not looking.

**The cheap check, and what it cannot see.** Every picture is also described
without any baseline at all: `distinct` counts the colours in its middle half,
coarsened to four bits a channel, and a picture that answers 1 fails the walk
outright. That is `checkCanvas`'s question asked of a photograph, it is
available today, and it is a **floor and not a resemblance** — the black
sphere this milestone found answers 64, because its pins are on it. Only the
committed picture catches that one, which is why §6.1 exists.

### 2.1.4 The awaiting-fix table

These steps are written against behaviour that is not in the tree yet. They
are listed so the capture wave can tell new coverage from a regression: a step
below that is still red when the wave runs means its fix did not land, and the
wave must stop rather than baseline the defect.

| step | what it asserts | why it fails today | whose fix |
|---|---|---|---|
| `pick-in-grid` | a click inside a drawn cell telescopes into the cell | the pane resolves features and never cells, so the click selects whatever pin is under it | grid telescope |
| `key-search-focus` | ⌘K focuses and selects `#pin-search` | there is no ⌘K shortcut; the field advertises one with a `<kbd>` | keyboard |
| `key-grid-open` (focus half) | `G` leaves the focus in `#grid-input` | the route opens the grid and nothing moves the focus | keyboard |
| `key-escape-once` | the first Escape leaves the field, not the level | Escape is bound straight to `ascend` on the window | keyboard |
| `key-cell-system-cycled` | ⌘G cycles the system and carries the cell | `⌘G` is excluded by the `g` trigger's guard and bound to nothing | keyboard / S2 |
| `key-typing-not-a-shortcut` | typing `g` in a text field does not toggle the grid | the shortcuts listen on the window and a reader's key bubbles to it | keyboard |
| `screen-globe` | the sphere's base skin is the lens, not black | the base texture never reaches the sphere | globe sprites |
| `screen-ground` | a multipart district draws every part | a second part is dropped | multipart |

The two picture rows are a different kind of awaiting from the six above them,
and the difference is the whole reason this table exists. A step that asserts
fails *today*, out loud, and the walk is red until its fix lands. A picture
with no committed twin is **not captured**, which is neither a pass nor a
failure — so those two rows cannot go red on their own, and the wave is the
only thing standing between a defect and a golden of it. They are here because
the pictures were looked at while this was written: mars's `screen-globe`
photographed a **black sphere with its pins on it** — 142 colours in the
middle half, every count in the snapshot correct, the overview beside it
showing the Mars texture the sphere was not wearing. That is the exact defect
the picture step exists for, and committing it as a baseline would be the
worst outcome available.

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

Run `golden/parity/compare.mjs baseline.json candidate.json`. Every leaf of
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
# re-capture runs from the golden-reference tag; see golden/capture/README.md
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

## 6.1 Capturing the extended half

```sh
make static                                                    # the seam the tour walks over
node golden/parity/run.mjs --volume mars --bundles <farm> \
  --extended --shots /tmp/shots --out /tmp/mars.json           # one walk, by hand
node golden/parity/capture.mjs --capture-extended              # the wave: all six
node golden/parity/capture.mjs --capture-extended --only mars  # one of them
```

This is the one capture in this directory that runs against the **rewrite**
rather than against the golden reference, and it needs an argument for that.
The reference never had a pick step, a focus reading or a screenshot; there is
nothing of its behaviour to reproduce, so the finished rewrite is where the
new steps' first recording has to come from. What keeps that from laundering
everything else is the shape of the mode, and it is not a convention — it is
what the code does:

1. **Nothing already captured is re-taken.** The committed snapshots are
   copied through byte for byte and the new steps are appended after them.
2. **The walk is held to the baseline before a byte is written.** The shared
   prefix is diffed against the committed steps under the same waivers the
   gate reads, and a volume that differs is refused with its differences
   printed. A build that does not reproduce the reference is not a build to
   take new baselines from.
3. **A red walk is not a baseline.** The new steps' own checks — §2.1.4's
   table — are tour problems, and `runTour` throws on a red walk. So every
   fix in that table must have landed before this mode can write anything. If
   one has not, the wave stops on it by name, which is the difference between
   "new coverage" and "we baselined the defect".
4. **The pictures are written into `golden/parity/screens/<volume>/`** and are
   ignored by git until that directory's `.gitignore` says otherwise. The wave
   deletes the `*.png` line and commits them; from then on `compare.mjs`
   compares every picture it has a twin for and reports the rest as *not
   captured*, which is not a pass.

What the wave must do, in order: build the seam; run
`capture.mjs --capture-extended` over all six volumes; read the refusals if
any and stop rather than force; drop the ignore line; commit baselines and
pictures together in one commit, because a baseline that names a picture that
is not there is a broken gate.

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

**Almost nothing checks pixels.** *Answered, in §2.1.3, and the answer is
narrower than the hole.* Every observation used to be a count, a flag or a
string, and a build that drew every pin in the wrong colour passed. Up to
eight screenshot steps a volume — six on a plane, eight where there is a
sphere — now photograph the pane and compare it against a committed picture,
which is the part of the hole that can be closed with a golden. Two things are still true: a picture is compared *perceptually*, so a
change under the threshold is a change nobody sees here; and until the capture
wave of §6.1 runs there are no committed pictures, so the steps take pictures
and report them as not captured. The raster-level goldens (per-lens tile
inventories with decoded-pixel digests, issue §6.1) remain a separate
instrument and are still not this one.

**Nothing drove a pointer, and nothing asked where the focus was.** *Answered,
in §2.1.1 and §2.1.2.* Both were structural rather than accidental: a tour
made entirely of `.click()` on controls cannot see a canvas that ignores
clicks, and a tour that dispatches every key at the window cannot see a
shortcut firing while the reader types. The steps that close them are written
against fixes that were still landing when they were written, and §2.1.4 says
which is which.

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

## What the rewrite reproduces, and the two things it does not

All six volumes agree with their baselines, step for step and field for field,
under the declared waivers and the two advisory `tileStats` counters. The tour
finds no problems on any of them: the map, the footer and the dock tell one
story on every step of every walk, a closed card gives back the view a jump
borrowed, and the sphere and the chart lose the same pins to one filter.

| volume | steps | tour's own checks | fields differing |
|---|---|---|---|
| tunic | 37 / 37 | clean | **0** |
| cyberpunk-2077 | 49 / 49 | clean | **0** |
| fallout-new-vegas | 39 / 39 | clean | **0** |
| zelda-tears-of-the-kingdom | 67 / 67 | clean | **0** |
| mars | 59 / 59 | clean | **0** |
| bend-or | 66 / 66 | clean | **0** |

Two differences are declared rather than reproduced, and both are the same
kind of thing: a number the reference read off a window that was about to stop
existing.

- **`fit-across-an-opening`** — `fitZoom` at `game-second` and `map-second`,
  the two steps at which a walk opens a ground it was not standing on. The
  reference folded the panel beside the map and then fitted the camera on the
  next animation frame, before the browser had told OpenLayers the map had
  grown, so a ground reached from somewhere with the panel out was fitted to
  the narrower window it was leaving. The fixture set records both answers for
  the same volume — 1.3399 where the panel was already folded, 1.2621 where it
  was not — which is what makes it a race rather than a rule. The rewrite's
  page arrives with its panel in the state it will be in, so there is no frame
  to catch.
- **`chart-camera-under-the-sphere`** — `session.entry.zoom` at five of mars's
  globe steps. The reference wrote its whole arrangement, the chart's camera
  included, on every interaction; a filter pressed while the sphere was up
  therefore saved a chart camera that was a fit into a window of no size. The
  rewrite reports a camera only from the pane the reader is looking through
  (docs/render-seam.md §5), so the record keeps the camera the reader actually
  left. The chart's *live* camera is bound and agrees at those same five steps;
  only what was saved differs.

Both are in `golden/waivers.json` with the argument written out, both name the
steps they cover, and both leave the field bound everywhere else. Nothing else
is waived and no tolerance was declared: every other difference this milestone
found was a defect, and the defects are listed in the commit history rather
than here.

### What the five diagnosed defects turned out to be

Kept because a diagnosis that was half right is worth the next reader knowing
about.

- **A feature index row highlighted where it should have jumped.** True, and
  larger than it looked: the row's verb was wrong (`/session/highlight` where
  the reference selected, opened the card and flew the camera), highlighting had
  no contextmenu to answer at all, a jump to *ground* has to fit the shape's
  extent rather than fly at its middle, the index listed every layer of a split
  sheet instead of the one the lens draws, and the mark a jump leaves on the
  index outlives the card it opened. Five changes, one wrong verb.
- **Titles sorted by bytes, not by collation.** True. `internal/app/collate.go`
  is the root collation the browser sorted with, in two strengths, reproducing
  the browser's order over all 7,879 titles in the fixture set.
- **The panel did not fold on a world change.** True, and it folds in the
  handler that actually serves a world change: the world select navigates, so
  the fold belongs to `handleExplorer` and not only to `applyWorld`.
- **The held cell divided different ground in the two halves.** *Not what it
  was.* The two halves divide the same ground and always did — every fixture's
  lens declares a surface, and both sides start from it. What differed was what
  a cell *narrows*: the server was culling ground by the held cell, and the
  reference never did. A cell narrows what is standing on the ground, not the
  ground.
- **The globe did not swap its panes under the tour.** The toggle was wired
  once per swap and the mark that said so was a `data-` attribute — which is
  the one place a mark cannot be kept, because a morph rewrites attributes from
  markup that has never heard of it. The listeners accumulated, one press
  flipped the panes once per listener, and whether the sphere came up was
  decided by the parity of a count. By hand it was odd; at the end of a tour it
  was even.
