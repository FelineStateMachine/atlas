# The analysis vectors

Item 6 of issue #5 §6.1: *the hand-derived geohash/S2 goldens plus recorded
cell plans for every grid-touching tour step, captured as language-neutral
JSON fixtures.*

This directory is what "the cell systems are correct" means, written down
without reference to any implementation. A Go or TypeScript build reads the
same files, runs the same eight calls, and compares the same way.

```sh
node golden/analysis/run.mjs              # the gate (also `make golden`)
node golden/analysis/run.mjs --verbose     # name every case as it passes
node golden/analysis/capture.mjs --check   # re-record in memory, write nothing
```

**The gate now judges the clean lane.** M6 landed `analysis/cellsystems` and
threw the switch in `run.mjs`: the eight functions come out of
`engine/cleanroom.mjs`, and every fixture in this directory passed on the first
run against an implementation written from the contract rather than from the
old files. The gate needs `npm ci` at the repository root (the lane's one
dependency, s2js) and a node that strips types — 22.18+, or 24+.

`engine/current.mjs` stays where it is: it documents the oracle these fixtures
were recorded from, it still runs, and `ATLAS_ANALYSIS_ENGINE=current` re-points
the gate at the old tree for a side-by-side. That path needs the old module's
dependencies — `npm --prefix frontend ci` — because it drives s2js *and*
OpenLayers through the current tree. No browser, no bundler, no test framework,
either way.

## What is here

| file | what it pins |
| --- | --- |
| `vectors/grounds.json` | the ground descriptors every case names, and what each derives |
| `vectors/surface.json` | the surface extent's three fallback branches; which systems will divide a ground |
| `vectors/identity.json` | level, parent, sibling ordinal, label cuts, palette index — one id at a time |
| `vectors/hierarchy.json` | `children()` in its stable order, and the parent link back |
| `vectors/containment.json` | boundary-inclusive containment, descent agreeing with it, `locate` |
| `vectors/geometry.json` | bbox, centre, ring, and the antimeridian clip |
| `vectors/continuity.json` | rings that stay continuous across the seam; the poles a cell circles |
| `vectors/input.json` | what the navigator keeps of a keystroke, and whole-or-nothing parsing |
| `vectors/carry.json` | `equivalentCell` — a place carried across systems at like precision |
| `plans/<volume>.json` | the plan at every grid-touching step of that volume's parity tour |
| `plans/contract.json` | the plans the tour cannot reach: the leaf role, deeper levels, all of S2 |

178 vectors over 9 grounds; 28 plans over 1,391 cells.

## The eight calls

Every fixture case is one call. This is the entire dispatch table a consumer
needs — five functions and the 18-method contract behind `invoke`:

```
surfaceExtent(ground)                     -> [minX, minY, maxX, maxY]
applicableSystems(ground)                 -> slugs, in the navigator's order
invoke(ground, system, method, args)      -> any contract method
geohashCellAt(ground, coordinate, depth)  -> the pin card's fixed-depth address
equivalentCell(ground, from, to, id)      -> the cross-system carry
clipRingX(ring, minX, maxX)               -> the antimeridian clip
cellPlan(ground, system, cellID)          -> the plan, in emission order
cellVisual(ground, system, cell, options) -> the pure style tokens
```

Three contract methods take the world rather than an id — `appliesTo`,
`maxLevel`, `inputLength` — and are handed the ground's world, so a case
never spells the world twice. Coordinates are OL world coordinates: x east,
y **negative-down**, the space `pin.coordinate` lives in.

## The ground descriptor (what M6's `Ground` must carry)

Issue #5 §5.4 de-globalizes the systems: they receive a `Ground` descriptor
instead of reaching into the app's shared client state. These fixtures are
that descriptor a milestone early. Each ground records **exactly what the
current engine consumed as implicit global state**, and nothing else — the
list below is the whole of it, read out of `frontend/src/cellsystems/`:

| field | the global it was read from | who reads it |
| --- | --- | --- |
| `tileGridSize` | `state.volume.tileGrid.size` | `surfaceExtent`, as the last fallback |
| `lens.surface` | `state.lens.surface` | `surfaceExtent`, preferred when declared |
| `lens.bounds` | `state.lens.bounds` | `surfaceExtent`, when no surface is declared |
| `world.attrs` | `state.world.attrs` | `worldSurface` (`atlas.geometry.surface`) and `geoMapping` (`atlas.geometry.projection` + the projection's `px`/`deg` pair) |

`lens` is `null` when the app had no lens open; `surface` and `bounds` are
independently nullable, and the fallback ladder is surface → bounds → the
world square, which `vectors/surface.json` pins in all three branches.

Two derived fields are recorded on every ground and checked by the gate:

- **`surfaceExtent`** — `[minX, minY, maxX, maxY]`, the ground every system
  divides. A candidate free to carry the ground differently still has to
  land on these numbers.
- **`systems`** — which systems will divide it. S2 requires
  `atlas.geometry.surface: sphere` *and* an invertible mapping; geohash
  divides anything, because it never asks what the picture is of.

**What is deliberately not in the ground:** the active system, the held cell,
and whether the subgrid is showing. Those are session state (§4.1), and they
arrive as arguments — `cellPlan(ground, system, cellID)` — which is why the
plan fixtures record them per step rather than folding them into the ground.

Nine grounds: four hand-derived ones from the frontend suite (`test/…`) and
five read out of the bundle fixtures, one per parity volume, named
`<volume>/<world>/<lensIndex>` with the volume, world, lens name and index in
`provenance`.

## The positional guarantee

**A plan is compared positionally.** `cellPlan` emits, in this order:

1. for each ancestor from the root down, that ancestor's children except the
   one on the path, as `neighbor`s carrying their distance from the held cell;
2. then either the held cell as a `leaf` — if it sits at `maxLevel`, and then
   nothing follows — or the held cell as a `scope` followed by every one of
   its children.

The gate checks length first, then cell for cell, and reports the index of
the first difference: *"cell 12 of 32, emission order is the contract"*. A
reordering is a failure, not a detail. Within a cell the key order is
`hash, extent, ring, pole, childIndex, role, contextDistance`; the JSON
carries it, but only the array order is contractual.

`visuals` runs parallel to `plan`, index for index: for each cell, the
`gridCellVisual` tokens under both answers to the renderer's own question
(does the label fit?), at the step's `subgridVisible`. `null` means the cell
draws nothing at all — a child cell too small to label, with the subdivision
put away. The tokens are pure: colours, opacities, widths in px, the label's
two cuts and its chip. No renderer owns a cell rule; the chart adapts these
into `ol/style` and the globe into materials and sprites.

## Provenance, and the tie to the parity baselines

Each `plans/<volume>.json` step names the tour step it was recorded from.
The inputs were recovered from the parity baseline's own grid state
(`grid.system`, `grid.prefix`, `ui.subgridVisible`, and the world and lens
titles the snapshot records), and the ground from the matching bundle fixture
under `golden/fixtures/bundles/`.

The gate re-checks that tie on every run: for every recorded step, the cells
the plan emits must be the cells the parity baseline drew, with the same
extents, roles and context distances. The baseline's own *order* is
OpenLayers' spatial index and means nothing, so that half is a set check —
the ordering claim belongs to the plan fixture. The baseline's `count` field
is a pin count and is not a plan property at all.

The tour holds geohash at depths 0 and 1 on five volumes, puts the subgrid
away once, and never cycles to S2. Everything it leaves unexercised —
the `leaf` role at either system's floor, a deeper geohash plan, S2 at the
root, S2 on a polar face, S2's own leaf — is in `plans/contract.json`, on the
same Mars ground as the tour steps, so a difference there is a difference in
the systems and not in the ground.

## Hand-derived, not self-recorded

The geohash numbers were derived from the halving rules and the S2 ones from
the Go library's own test vectors, in `frontend/test/cellsystems.test.mjs`,
before any of this existed. `capture.mjs` carries those literals as a
checklist: every case the frontend suite pins is recorded from the engine
*and* asserted against the hand-derived literal on the way past, and a
disagreement aborts the capture. Those cases are marked `handDerived: true`
in the fixtures. The rest are recordings of an implementation that works,
which is what a golden is.

## The two engines

`run.mjs` imports one module and calls the eight functions above. Two modules
provide them, and the fixtures know about neither.

`engine/cleanroom.mjs` — **what the gate judges today** — adapts
`analysis/cellsystems`. It is short, and every line it does not have is the
de-globalization: a ground is an argument, the held cell is an argument, and
three of the eighteen contract methods live on the system while the other
fifteen are reached through `system.on(ground)`.

`engine/current.mjs` is the adapter for the current tree, and it does one thing
the clean lane does not need: `applyGround` writes the recorded ground
descriptor *back into the globals* the old systems read it from. Its two
neighbours — `engine/hooks.mjs` and `engine/stubs.mjs` — exist for the same
reason: `frontend/src/grid.js` holds the plan and its style tokens in the same
file as a DOM controller, so the hook swaps four application-shaped imports
(`dom`, `detail`, `navigation`, `features`) for stubs that throw if anything
ever calls them. The cell math itself is loaded from its real source files,
unbundled and unedited. All three stay because the oracle is worth being able
to re-run; none of them is on the gate's default path.

## Re-capturing

```sh
node golden/analysis/capture.mjs
```

Rewrites every file here from the current implementation, re-checking the
hand-derived literals and the parity tie as it goes. It is the recording
instrument, and running it is not a way to make a red gate green: a golden is
never edited to match a candidate (`golden/HARNESS.md`). Re-capture belongs
to a change in the *fixtures* — a new fixture volume, a new tour step that
touches the grid — and lands as its own reviewed commit against the current
tree, never against the rewrite.
