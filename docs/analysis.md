# The analysis lane

**Status: normative for `analysis/cellsystems`.** This document specifies the
cell-system contract — the eighteen methods, the coordinate and continuity
rules, the `Ground` descriptor, and the two contractual outputs — and it is
written to be sufficient on its own. An implementer who has never seen Atlas
should be able to write a conforming system, register it, and pass the
conformance suite from this document and nothing else.

The reference implementation is `analysis/cellsystems`. The judge is the
executable conformance suite in `analysis/test/conformance.ts`, plus the 178
shared contract vectors in `analysis/testdata/cells/` — compared as JSON text,
positionally, and read by the Go twin (`tests/cells`) as well, so the two
copies of the arithmetic answer to one set of numbers. Where this document and
the implementation disagree, take it as a defect in one of them and say so.

---

## 1. What the lane is

Issue #5 §5.4: **an analysis is a client-side, render-time transformation
applied to a volume — the `.atlas` file is blind to it and is never mutated by
it.** The volume is one input; an analysis may declare others (user markup, a
live external feed, a hook into a running game), and how those are supplied is
the consumer's adapter concern, never the lane's.

Three rules follow, and they are enforced rather than encouraged:

- **Pure.** Every function is a function of its declared inputs. No DOM, no
  fetch, no application imports, no shared client state. The repository-root
  `eslint.config.mjs` refuses the imports and the globals; `tsc` with
  `lib: ["es2023"]` refuses the DOM types before a lint rule gets the chance.
- **Renderer-neutral.** Outputs are plans and pure style tokens. Renderers
  adapt them. No renderer owns a cell rule.
- **Session-free.** Which system is active, which cell is held, and whether the
  subdivision is showing are session state (§4.1). They arrive as arguments.

**Cell systems are the founding family** and set the conformance bar: a second
family joins by exporting from `analysis/index.ts` beside them, and by bringing
its own property suite.

---

## 2. Coordinates

Two coordinate conventions meet in this lane, and confusing them is the one
mistake that produces plausible wrong answers.

| Space | Used by | Sense |
|---|---|---|
| **OL world coordinates** | everything in `analysis/cellsystems` | x east, y **negative-down**: the top edge of the world square is `y = 0` (or `-0`), the bottom is `y = -height`. This is the space a pin's `coordinate` lives in. |
| **y-down world pixels** | a `Rect` in a `Ground`, and `atlas.geometry.*.px` | x east, y **positive-down**, from the world square's top-left. This is the space a bundle declares itself in. |

The conversion happens in exactly two places — `surfaceExtent`, which flips the
sign of a declared `Rect`, and the `GeoMapping` calls in `s2.ts`, which flip it
again on the way to degrees. Nothing else in the lane converts, and nothing
outside it should have to.

A degree pair is always `[lat, lng]`, latitude first, degrees, north and east
positive.

---

## 3. The `Ground` descriptor

A system is handed a `Ground` instead of reaching into a running application.
The descriptor is exactly the implicit state the pre-rewrite systems read, and
nothing else:

```ts
interface Rect   { x: number; y: number; width: number; height: number }  // y-down pixels
interface Lens   { surface?: Rect | null; bounds?: Rect | null }
interface World  { attrs: Readonly<Record<string, string>> }

interface Ground {
  tileGridSize: number | null;   // the volume's world square; null with no volume open
  lens: Lens | null;             // null with no lens open
  world: World;                  // the semconv geometry keys
}
```

`analysis/testdata/cells/grounds.json` records six of these, each with its two
derived values — `surfaceExtent` and the systems that divide it — checked by
both lanes' vector suites.

### 3.1 `surfaceExtent(ground): [minX, minY, maxX, maxY]`

The ground every system divides. Three branches, in order:

1. the lens's declared **surface**, when it has one;
2. failing that, the lens's raster **bounds**;
3. failing that, the whole **world square** (`tileGridSize`, or `0`).

Each branch flips the sign: `[x, -(y + height), x + width, -y]`. A lens
anchored at `y = 0` therefore produces a `maxY` of `-0`, which is a real
double. JSON cannot carry it, which is why the shared vectors are compared as
JSON text on both sides — the serialization forces the negative zero to be
normalized away rather than the comparison loosened.

### 3.2 What is deliberately not in it

The active system, the held cell, and whether the subgrid is showing. Those are
session state, and they arrive as arguments — `cellPlan(ground, system,
cellID)`. That is what lets one ground be divided two ways at once, by two
callers who never meet, and it is what the plan tests exercise by passing the
session values per call rather than folding them into the ground.

### 3.3 The attribute vocabulary it reads

`analysis/semconv/geometry.ts` is the lane's reader for four keys:

| Key | Read by | Meaning |
|---|---|---|
| `atlas.geometry.surface` | `worldSurface` | `sphere`, or a plane when absent |
| `atlas.geometry.projection` | `geoMapping` | `equirect` or `mercator` |
| `atlas.geometry.<p>.px` | `geoMapping` | `x,y,w,h` — the raster window, y-down pixels |
| `atlas.geometry.<p>.deg` | `geoMapping` | `west,north,east,south` — what it pictures |

A declaration that cannot be read — wrong arity, a non-number, a zero-sized
window, a degenerate degree range — produces `null`, never a plausible NaN.

The reader is the lane's own; the **keys it reads are not**. They come from
`analysis/semconv/keys.ts`, generated from `spec/registry.yaml` — the one
machine-readable source `format/semconv` and
[`docs/semconv/REGISTRY.md`](semconv/REGISTRY.md) are cut from too (issue #5
§8). The seam imports the same module, as `@atlas/analysis/semconv/keys`, so
no lane spells a convention key for itself.

---

## 4. The eighteen methods

A cell system divides a ground into an exact hierarchy a reader can telescope
through. The contract splits in two, and the split *is* the de-globalization.

**Three** answer about a ground before any cell is named, and live on the
system:

```ts
appliesTo(ground): boolean      // will this system divide this ground at all
maxLevel(ground): number        // how deep the telescope goes
inputLength(ground): number     // how many characters the navigator's field takes
```

**Fifteen** answer about a cell on a ground, and are reached through
`system.on(ground)`, which binds the ground once:

```ts
level(id): number               // depth; the root "" is level 0
parent(id): CellID              // one level up; "" from a level-1 cell and from the root
children(id): CellID[]          // next level down, in a STABLE order
childIndex(id): number          // ordinal among siblings
contains(id, at): boolean       // boundaries INCLUSIVE
descendTarget(id, at): CellID   // the child of id under the point, or ""
bbox(id): Extent                // [minX, minY, maxX, maxY], OL world coordinates
center(id): Coordinate          // a point inside the cell
ring(id): Ring                  // closed, pre-tessellated, CONTINUOUS across the seam
poleContained(id): Pole | null  // "north" | "south" | null, for ring closure
label(id): CellLabel            // { context, principal } — the chip's two cuts
colorKey(id): number            // palette index; siblings must differ
normalizeInput(text): string    // what the navigator's field keeps of a keystroke
parseInput(text): CellID | null // a canonical id, or null while it is not yet a place
locate(at): LocatedCell | null  // { label, value } for the pin card
```

Plus three identity fields: `slug` (stable, persisted in sessions), `name` (how
the navigator names it), `short` (the mark its cycle button wears).

### 4.1 The rules

**Opaque ids.** A cell id is an opaque string and `""` is the root of every
system. Nothing outside a system may take one apart. Geohash refines by
appending a character; S2 does not — the children of `47a1cb` are `47a1ca4`
and its siblings — and a third system may do neither.

**Stable child order.** `children(id)` answers in one order, forever. The plan
emits in it, the palette indexes into it, and the vectors gate compares
positionally. `childIndex(children(id)[n]) === n`.

**Boundary-inclusive containment.** `contains` is true on the boundary. A point
on a shared edge is in both cells, and a corner is in all four; nothing in the
lane breaks the tie, because the caller asking is drawing, not partitioning.

**Descent agrees with containment.** `descendTarget(id, at)` names a child that
`contains` the point.

**Whole-or-nothing parsing.** `parseInput` answers a canonical id or `null`. A
draft is not a place: the field holds it and the session stays put. Every id a
system mints at or above `maxLevel` must survive `normalizeInput` and
`parseInput` unchanged.

**Ring closure.** `ring(id)` is a loop whose last point is its first — *some
whole number of worlds over in x, on the same row*. See continuity.

**Antimeridian continuity.** A ring never jumps the seam: consecutive points
stay within half a world of each other in x, **even where that runs the loop
off the surface**. An honest ring may sit at x = 13312 on an 8192-wide picture.
The system does not wrap or split; `cellRings(ground, cell)` does, and it is
where every renderer should go. Half a world exactly is a bound rather than a
strict one: a cell touching a pole sweeps exactly that much longitude in one
tessellation step, and that is ground, not a seam.

**Poles.** `poleContained(id)` says which pole a cell circles, because a loop
around a pole closes along the picture's own top or bottom edge and only the
system knows it needs to. A cell that circles a pole has a parent that circles
the same one.

### 4.2 What the contract deliberately does not promise

Three silences, each of which a property suite would otherwise be tempted to
assert falsely.

**Winding.** A ring's signed area may be either sign. The antimeridian unwrap
reverses it for some loops that circle a pole, and nothing downstream reads the
direction. Non-degeneracy is a rule; orientation is not.

**The floor asymmetry.** `descendTarget` at `maxLevel` answers `""` in S2 and
keeps halving in geohash. Nothing reaches it — every caller checks `maxLevel`
first, in `cellPlan` and in `equivalentCell` — and both behaviours are pinned
by the shared vectors, so the contract asserts only that descent *terminates*: the telescope
reaches `maxLevel` in exactly `maxLevel` hops, one level per hop.

**The whole-world assumption.** The seam unwrap treats the ground's width as
one whole world. That is true of the equirectangular whole-planet lenses S2 is
offered on in practice, and false of, say, a Web-Mercator city window — where
`appliesTo` still answers yes and the coarse cells' rings are nonsense.
`analysis/test/s2-limits.test.ts` pins the limit rather than papering over it;
narrowing `appliesTo` is a curation decision for the app or a future `Ground`
field, not this lane's to make unilaterally.

---

## 5. The registry, and carrying a place between systems

```ts
const cellSystems: CellSystemRegistry;                        // geohash, then s2
applicableSystems(ground, registry?): readonly CellSystem[];  // those that will divide it
equivalentCell(ground, from, to, id): CellID;                 // the cross-system carry
```

A registry is **immutable**: `registry.with(system)` answers a new one. A
mutable module-level registry would be the same shared client state §5.4 just
removed from the systems, and would make "which systems exist" depend on import
order. Two systems may not share a slug.

`equivalentCell` carries a place across systems: the cell in the new hierarchy
holding the old cell's centre, taken to the depth whose cells cover closest to
the same ground. The two hierarchies share no boundaries, so what survives is
the point and the precision — area compared on a **log scale**, since levels
grow geometrically and "closest" should mean closest in kind, not in pixels.
The root carries to the root, which is the only place two hierarchies agree
exactly.

---

## 6. The plan, and its frozen order

```ts
cellPlan(ground, system, cellID): PlanCell[]

interface PlanCell {
  hash: CellID;          // the id; the field keeps this name whatever minted it
  extent: Extent;
  ring: Ring;
  pole: Pole | null;
  childIndex: number;
  role: "neighbor" | "scope" | "child" | "leaf";
  contextDistance: number;
}
```

**Emission order is the contract.** Every check of a plan — the conformance
suite, the property suite — compares it positionally, cell for cell. A
reordering is a failure, not a detail. The order is:

1. For each ancestor from the root down, that ancestor's children **except the
   one on the path**, as `neighbor`s carrying their `contextDistance` — how many
   levels above the held cell that ancestor's children sit. The root's
   neighbours come first, so `contextDistance` descends across the run.
2. Then either:
   - the held cell as a **`leaf`**, if it sits at `maxLevel` — and **nothing
     follows**, because a leaf has nothing to divide; or
   - the held cell as a **`scope`**, followed by every one of its children as
     `child`s in the system's stable child order.

The root plan — no held cell — is the second case with no ancestors and no
scope: the root's children, and nothing else.

`contextDistance` is `0` for every role but `neighbor`.

---

## 7. The style tokens

```ts
gridCellVisual(ground, system, cell, { subgridVisible, labelled }): CellVisual | null
```

Pure tokens: colours, opacities, widths in px, the label's two cuts and its
chip. The chart adapts them into `ol/style` and the globe into materials and
sprites; neither holds a colour or a width of its own, which is why the two
projections read as one instrument.

Two questions come from the renderer's side rather than the plan's:
`subgridVisible` (session state) and `labelled` (does the label fit where it
would be drawn — a measurement only a renderer can make).

- A **`child`** with `labelled: false` or `subgridVisible: false` draws
  **nothing at all**: `null`. The subdivision appears at the size it can be
  read, on both panes at the same visual moment.
- A **`scope`** with the subdivision showing is **bare**: an outline at
  `widths.scopeBare`, no fill, no label. Put the subdivision away and it
  thickens to `widths.scope` and gets its label back.
- A **`leaf`** is filled in its own accent; a **`neighbor`** is dimmed by
  `dimBase + contextDistance × dimStep`, capped at `dimCap`.

`palette` and `gridTheme` are exported: a renderer reads them, it does not
restate them.

---

## 8. Rings on the picture

```ts
cellRings(ground, cell): Coordinate[][]
clipRingX(ring, minX, maxX): Coordinate[]
ringClosesOn(ground, ring): boolean
```

`cellRings` is the whole of what a renderer would otherwise have to work out,
and it is three rules in one place:

- Most rings lie within the surface and come back as a **single piece,
  untouched** — every geohash ring does.
- A cell circling a **pole** has its loop closed along the picture's own top or
  bottom edge, *closing point included*: a pole cell's walk ends a world over
  from where it began, and that closure spans the last tessellation step.
  Dropping it leaves a sliver of ground the fill never covers, one step wide,
  at the walk's own longitude.
- A ring that stayed continuous across the **antimeridian** is clipped as it
  lies and clipped again shifted a world each way, so the one cell arrives as
  its two pieces.

`clipRingX` is the Sutherland–Hodgman walk that does the cutting; it may start
its result at a different vertex than the input, because a clip has no opinion
about where a loop begins.

---

## 9. Adding a third system

A third system — H3, MGRS, a game's district grid — is **a registry entry plus
a green conformance run**. `analysis/test/districts.ts` is a worked example
that exists only to prove it: dash-joined ids, nine-way subdivision, a label
that is not "the last character and everything before it", and a `parseInput`
with something to refuse.

The checklist:

1. **Write the system.** One file exporting a `CellSystem`: the three identity
   fields, the three ground questions, and `on(ground)` returning the fifteen.
   Read §4.1 first — every rule there is a property the suite checks.
2. **Decide what ground you divide.** `appliesTo` is where a system says no.
   Geohash says yes to everything; S2 asks for a declared sphere and an
   invertible flattening; a district grid asks for a curation attribute. Say no
   loudly rather than dividing a ground you do not understand.
3. **Pick the floor.** `maxLevel` is a reading decision, not a mathematical
   one: deep enough for the pins on the map, shallow enough that every level
   stays a place rather than a coordinate.
4. **Register it.** `cellSystems.with(yourSystem)` — or add it to the shipped
   list in `registry.ts` if it ships. The registry order is the navigator's
   order.
5. **Add a conformance subject.** In `analysis/test/conformance.test.ts`:

   ```ts
   describeConformance("your system on its ground", {
     system: yourSystem,
     ground: aGroundItApplies,
     probe: [x, y],            // a point well inside, used to drive descent
     boundaryProbes: [ ... ],  // [cellID, point] pairs your geometry puts exactly
                               // on a boundary; declare [] and say why if your
                               // containment is decided on a finer lattice
   });
   ```

6. **Run the gates.** `make analysis-lane` for the boundary rules, the type
   checker and the conformance suite; `make test` for everything, including
   the shared vectors, which your system does not appear in and must not
   disturb.
7. **Do not touch the shared vectors.** `analysis/testdata/cells/` describes
   geohash and S2. A new system adds nothing to it; if adding one turns a
   vector red, the new system reached something shared that it should not
   have.

---

## 10. The root cases

`""` is the root of every system, and S2's token vocabulary has no spelling
for it — `fromToken("")` answers a cell id that is neither the sphere nor an
error — so the root's answers are defined by this contract rather than
borrowed from the token library. The root is never a plan cell, so nothing
downstream reaches these answers through a plan; they exist so the fifteen
methods stay total. The contract: the root is the whole ground, its bbox and
ring are the picture's own rectangle, its centre is the picture's centre, it
contains every point the ground can name, its ordinal is 0, and it circles no
single pole because it circles both.

---

## 11. Running the lane

```sh
make analysis-lane        # boundary rules + tsc --strict + the lane's suite
make test                 # the whole required surface, this lane included
npm run --silent test     # the lane's suite alone, shared vectors included
```

The lane needs a node that strips types (22.18+, or 24+) and one `npm ci` at
the repository root, which installs its single dependency: `s2js`, the Go S2
library ported, test vectors and all. Nothing else is bundled, mocked, or
built.
