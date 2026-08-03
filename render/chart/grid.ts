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
import { cellPlan, cellRings, surfaceExtent } from "@atlas/analysis";
import type { CellSystem, Coordinate as Cell, Extent, Ground, PlanCell } from "@atlas/analysis";
import type { PointRecord } from "../world/model.ts";

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
 *
 * WHAT A CELL LOOKS LIKE IS NOT DECIDED HERE, and that is the whole shape of
 * this module. `gridCellVisual` needs to know whether the address fits inside
 * the cell *as the camera currently sees it*, which is a question about a
 * resolution — and the resolution at the moment a grid is built is the one the
 * camera is leaving, not the one it is flying to. Baked in here, a descent
 * drew its children against the parent's zoom, decided none of them could
 * carry a label, and drew nothing at all; the camera then landed and nothing
 * asked again. So the plan cell rides on the feature and the style function
 * answers, once per render, against the resolution it is handed.
 */
export function drawGrid(
  ground: Ground,
  system: CellSystem,
  cell: string,
  points: readonly PointRecord[],
  chosen: VectorSource,
  context: VectorSource,
): GridDraw {
  const on = system.on(ground);
  const plan = cellPlan(ground, system, cell);
  const cells: DrawnCell[] = [];
  for (const planned of plan) {
    const count = points.reduce(
      (held, point) => held + (on.contains(planned.hash, point.coordinate) ? 1 : 0), 0);
    const feature = new Feature({
      geometry: new Polygon(cellRings(ground, planned).map((ring) => ring.map(pair))),
      gridCell: { hash: planned.hash, extent: planned.extent, role: planned.role, count,
        contextDistance: planned.contextDistance } satisfies DrawnCell,
      gridPlan: planned,
      // The paint order within one layer. A dimmed neighbour is drawn under
      // the neighbours nearer the held cell — the dimming deepens inward, and
      // a farther cell's darker sheet painted last would flatten the ladder —
      // and a cell's own children are drawn in the system's child order so two
      // adjacent chips overlap the same way every time.
      priority: planned.role === "neighbor"
        ? -planned.contextDistance * 100 + planned.childIndex
        : planned.childIndex,
    });
    // Every planned cell becomes a feature, drawn or not: a cell whose label
    // does not fit answers `null` tokens and paints nothing, and the
    // diagnostics report carries it all the same. What the grid *holds* and
    // what it *paints* are two questions, and the plan answers the first.
    (planned.role === "neighbor" ? context : chosen).addFeature(feature);
    cells.push({ hash: planned.hash, extent: planned.extent, role: planned.role, count,
      contextDistance: planned.contextDistance });
  }
  return { cells, extent: cell ? on.bbox(cell) : surfaceExtent(ground) };
}

/**
 * Whether the camera owes this grid a move.
 *
 * Descending holds a cell and holding it moves the camera: the chart fits the
 * ground the address names, and the sphere turns to face it. Both panes decide
 * that here, because it is one rule and they must not answer it differently.
 *
 * A CELL MOVES WITHIN A SYSTEM. Cycling the cell system carries the held cell
 * across to whichever cell of the next system covers the same ground
 * (`equivalentCell`), so geohash's "m" becomes S2's "24" and the reader has
 * not gone anywhere — the string moved, the place did not. Compared as strings
 * alone that reads as a descent, and both panes flew the camera to ground it
 * was already over, which is the one thing a reader pressing a cycle key is
 * not asking for.
 *
 * Nothing held yet is not a move either: opening a grid over the ground
 * already on screen is not a request to go anywhere.
 */
export function cellMoved(
  was: { system: string; cell: string },
  now: { system: string; cell: string },
): boolean {
  return was.system === now.system && was.cell !== now.cell;
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
