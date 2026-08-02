// The grid, drawn from a plan.
//
// Everything about *which* cells there are, *where* they are and *what* they
// look like belongs to the analysis lane. This module is the adapter: it asks
// `cellPlan` for the cells in their frozen emission order, `cellRings` for the
// rings as they are actually drawn — the pole closure and the antimeridian cut
// are already resolved there, and no renderer should be working them out —
// and `gridCellVisual` for the tokens. It never sorts a plan, never dedupes
// one, and never invents a colour.
//
// Two sources, and the split is a z-order argument. The chosen path — the
// held cell and its subdivision — draws UNDER the pins, because the reader is
// looking at what stands inside it. The context — the neighbours, dimmed —
// draws OVER them, because dimming a neighbourhood has to dim what is in it.
// The promoted pins ride above both, which is how a searched-for name stays
// legible through a dimmed neighbour.

import Feature from "ol/Feature.js";
import Polygon from "ol/geom/Polygon.js";
import type VectorSource from "ol/source/Vector.js";
import {
  cellPlan, cellRings, gridCellVisual, surfaceExtent,
} from "@atlas/analysis";
import type { CellSystem, Coordinate as Cell, Extent, Ground, PlanCell } from "@atlas/analysis";
import type { PointRecord } from "../world/model.ts";

/** The smallest a cell may be drawn and still carry its address. */
const LABEL_FITS_PX = 46;

/** One cell as the diagnostics object reports it. */
export interface DrawnCell {
  readonly hash: string;
  readonly extent: Extent;
  readonly role: string;
  readonly count: number;
  readonly contextDistance: number;
}

/** What one draw of the grid produced. */
export interface GridDraw {
  readonly cells: DrawnCell[];
  readonly extent: Extent | null;
}

/**
 * Fill the two sources from one plan.
 *
 * `points` is the standing registry, and each cell is handed the number of
 * them it holds — the count the navigator and the diagnostics both read. It
 * is computed here rather than asked of the system twice, because a plan is
 * walked once and a containment test is the expensive half of drawing a grid.
 */
export function drawGrid(
  ground: Ground,
  system: CellSystem,
  cell: string,
  subgridVisible: boolean,
  resolution: number,
  points: readonly PointRecord[],
  chosen: VectorSource,
  context: VectorSource,
): GridDraw {
  const on = system.on(ground);
  const plan = cellPlan(ground, system, cell);
  const cells: DrawnCell[] = [];
  for (const planned of plan) {
    const [minX, minY, maxX, maxY] = planned.extent;
    const labelled = (maxX - minX) / resolution >= LABEL_FITS_PX &&
      (maxY - minY) / resolution >= LABEL_FITS_PX / 2;
    const visual = gridCellVisual(ground, system, planned, { subgridVisible, labelled });
    const count = points.reduce(
      (held, point) => held + (on.contains(planned.hash, point.coordinate) ? 1 : 0), 0);
    const feature = new Feature({
      geometry: new Polygon(cellRings(ground, planned).map((ring) => ring.map(pair))),
      gridCell: { hash: planned.hash, extent: planned.extent, role: planned.role, count,
        contextDistance: planned.contextDistance } satisfies DrawnCell,
      gridVisual: visual,
      priority: planned.role === "neighbor" ? planned.contextDistance : 100,
    });
    // Every planned cell becomes a feature, drawn or not: a cell whose label
    // does not fit answers `null` tokens and paints nothing, and the parity
    // baselines record it all the same. What the grid *holds* and what it
    // *paints* are two questions, and the plan is the answer to the first.
    (planned.role === "neighbor" ? context : chosen).addFeature(feature);
    cells.push({ hash: planned.hash, extent: planned.extent, role: planned.role, count,
      contextDistance: planned.contextDistance });
  }
  return { cells, extent: cell ? on.bbox(cell) : surfaceExtent(ground) };
}

/** Whether a point stands inside the held cell. */
export function cellTest(
  ground: Ground,
  system: CellSystem,
  cell: string,
): ((at: readonly [number, number]) => boolean) | null {
  if (!cell) return null;
  const on = system.on(ground);
  return (at) => on.contains(cell, at);
}

function pair(point: Cell): [number, number] {
  return [point[0], point[1]];
}

/** The cells a plan holds, without drawing them — for the globe and for tests. */
export function planCells(ground: Ground, system: CellSystem, cell: string): PlanCell[] {
  return cellPlan(ground, system, cell);
}
