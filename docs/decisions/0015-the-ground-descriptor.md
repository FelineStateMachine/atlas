# 15. A cell system is handed a `Ground`, not an application

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §5.4, §10 decision 17;
  [analysis.md](../analysis.md) §3

## Context

The pre-rewrite cell systems reach into the running application's shared client
state to find out what they are dividing: the world square, the open lens, the
world's geometry attributes. That makes each system a function of a global, so
it cannot be tested headlessly, cannot be reused by a renderer nobody has
written, and cannot divide two grounds at once.

The analysis lane's charter is broader than cell systems — future analyses may
declare inputs beyond the volume (user markup, a live feed, a hook into a
running game) — and a lane whose founding family reads globals cannot absorb
those without being refactored first.

## Decision

Every system receives a **`Ground` descriptor**: exactly the implicit state the
old systems read, and nothing else.

```ts
interface Ground {
  tileGridSize: number | null;   // the volume's world square; null with no volume open
  lens: Lens | null;             // null with no lens open
  world: World;                  // the semconv geometry keys
}
```

`surfaceExtent(ground)` resolves it in three branches, in order: the lens's
declared surface, else the lens's raster bounds, else the whole world square.

**Session state is deliberately not in it.** The active system, the held cell
and whether the subgrid shows arrive as arguments — `cellPlan(ground, system,
cellID)` — which is what lets one ground be divided two ways at once by two
callers who never meet.

## Consequences

- Every transformation is a clean function of its declared inputs, which is
  what lets the charter's future analyses register without refactoring the
  lane.
- A third system (H3, MGRS, a game's district grid) is a registry entry plus a
  green run of the conformance suite.
- The vectors record nine grounds as language-neutral JSON, and the gate
  compares JSON *text* on both sides — a lens anchored at `y = 0` produces a
  `maxY` of `-0`, which is a real double that JSON cannot carry, and loosening
  the comparison would hide a class of difference the harness exists to see.
- `analysis/semconv/geometry.ts` is the lane's own reader for the four geometry
  keys it may be asked about. It is deliberately a *reader*: it never writes a
  key and never validates a bundle. Its key constants come from
  `spec/registry.yaml`, the one machine-readable source both ends generate
  from.
