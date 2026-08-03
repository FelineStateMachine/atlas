// analysis/cellsystems — the founding family of the analysis lane.
//
// A cell system divides a ground into an exact hierarchy a reader can
// telescope through. This package is the whole of that: one contract
// (contract.ts), two systems keeping it (geohash.ts, s2.ts), a registry, and
// the two contractual outputs — the plan and the style tokens.
//
// The lane's rules, from issue #5 §3.2 and §5.4:
//
//   - Pure. Every function is a function of its declared inputs. A Ground
//     descriptor goes in; plans and tokens come out. No DOM, no fetch, no
//     application imports, no shared client state.
//   - Renderer-neutral. Outputs are plans and pure style tokens. Renderers
//     adapt them; no renderer owns a cell rule.
//   - Session-free. The active system, the held cell and whether the
//     subdivision shows are session state, and they arrive as arguments.
//
// The contract is written down in `docs/analysis.md`, including the
// walkthrough for adding a third system. `analysis/testdata/cells/` is the
// judge: the shared contract vectors both this lane and the server's Go twin
// (internal/app/cells, via tests/cells) answer, so the two copies of the
// arithmetic are held to one set of numbers.

export type {
  CellID,
  CellLabel,
  CellSystem,
  GroundedCellSystem,
  LocatedCell,
  Pole,
  Ring,
} from "./contract.ts";

export type { Coordinate, Extent, Ground, Lens, Rect } from "./ground.ts";
export { extentArea, surfaceExtent, surfaceWidth } from "./ground.ts";

export type { GeoMapping, ProjectedWindow, World, WorldAttrs } from "../semconv/geometry.ts";
export {
  equirectMapping,
  geoMapping,
  mercatorMapping,
  mercatorWindow,
  worldSurface,
} from "../semconv/geometry.ts";

export { CellSystemRegistry, applicableSystems, cellSystems, equivalentCell } from "./registry.ts";

export {
  geohashAlphabet,
  geohashBaseDepth,
  geohashCellAt,
  geohashDepth,
  geohashExtent,
  geohashMaxDepth,
  geohashSystem,
} from "./geohash.ts";
export { s2System } from "./s2.ts";

export type { PlanCell } from "./plan.ts";
export { ancestorChain, cellPlan } from "./plan.ts";

export { cellRings, clipRingX, ringClosesOn, ringIsClosed, samePoint } from "./ring.ts";

export type {
  CellVisual,
  FillTokens,
  LabelTokens,
  LineTokens,
  VisualOptions,
} from "./visual.ts";
export { gridCellVisual } from "./visual.ts";

export type { CellRole, GridTheme } from "./tokens.ts";
export { gridTheme, palette, paletteColor } from "./tokens.ts";
