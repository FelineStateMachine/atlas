// The raster: two layers, because a pyramid is two different things.
//
// Up to `fullZoom` a pyramid is COMPLETE — every tile of every level exists,
// and the layer drawing them can always be stretched over a deeper view.
// Above it the capture only reached where it reached, and the coverage
// bitsets say where. Those patches ride on a second layer above the first, so
// the complete pyramid still shows through wherever the deep capture has a
// gap, and the reader never sees a hole where there is ground.
//
// Two overzoom levels sit past the deepest tiles either layer holds. There
// the base layer's own tiles are drawn larger — parent upsampling, which OL
// does for a tile grid that stops — and the lens's `interpolate` decides how:
// smooth for a photograph, nearest-neighbour for pixel art, because a
// hand-drawn map magnified with bilinear smoothing stops being a drawing.
//
// Nothing here asks for a tile the coverage denies. A wasted request is not
// free: it is a 404 the browser logs, a slot in the queue, and a reader
// watching a spinner for a tile that was never written.

import TileLayer from "ol/layer/Tile.js";
import XYZ from "ol/source/XYZ.js";
import type Projection from "ol/proj/Projection.js";
import { logger } from "../log.ts";
import type { DataPlane } from "../data/plane.ts";
import type { Lens, TileGrid as GridSpec } from "../data/payload.ts";
import { LensCoverage } from "../data/pyramid.ts";
import { RASTER_CACHE_SIZE, levelResolution, tileGridFor } from "./projection.ts";

const log = logger("raster");

/** Tiles since the lens was chosen. Two of the four are route, not destination. */
export interface TileStats {
  requested: number;
  loaded: number;
  errors: number;
  peakPending: number;
}

/** A counter the two sources share, so the pair reports as one pyramid. */
export class TileCounter {
  readonly stats: TileStats = { requested: 0, loaded: 0, errors: 0, peakPending: 0 };
  private pending = 0;

  reset(): void {
    this.stats.requested = 0;
    this.stats.loaded = 0;
    this.stats.errors = 0;
    this.stats.peakPending = 0;
    this.pending = 0;
  }

  started(): void {
    this.stats.requested++;
    this.pending++;
    if (this.pending > this.stats.peakPending) this.stats.peakPending = this.pending;
  }

  finished(): void {
    this.stats.loaded++;
    this.pending = Math.max(0, this.pending - 1);
  }

  failed(): void {
    this.stats.errors++;
    this.pending = Math.max(0, this.pending - 1);
  }
}

/** The two raster layers of one lens, and the counter they share. */
export interface Raster {
  readonly base: TileLayer<XYZ>;
  readonly detail: TileLayer<XYZ>;
}

function source(
  plane: DataPlane,
  base: string,
  lens: Lens,
  grid: GridSpec,
  projection: Projection,
  deepest: number,
  coverage: LensCoverage,
  counter: TileCounter,
): XYZ {
  const xyz = new XYZ({
    projection,
    tileGrid: tileGridFor(grid, deepest),
    interpolate: lens.interpolate,
    wrapX: false,
    cacheSize: RASTER_CACHE_SIZE,
    transition: 0,
    tileUrlFunction: (coordinate) => {
      const [z, x, y] = coordinate as [number, number, number];
      if (!coverage.has(grid, z, x, y)) return undefined;
      return plane.tileURL(base, lens, z, x, y) ?? undefined;
    },
  });
  xyz.on("tileloadstart", () => counter.started());
  xyz.on("tileloadend", () => counter.finished());
  xyz.on("tileloaderror", () => {
    counter.failed();
    log.warn("a tile the coverage admitted did not arrive", { op: "render", lens: lens.name });
  });
  return xyz;
}

/**
 * Build the pair for one lens.
 *
 * The base carries levels `minZoom … fullZoom`, the detail carries everything
 * above it. A pyramid that is complete to its own bottom gets an empty detail
 * layer rather than a special case: one shape of code, and one fewer thing
 * that can be wrong on the volumes where it matters.
 */
export function buildRaster(
  plane: DataPlane,
  base: string,
  lens: Lens,
  grid: GridSpec,
  projection: Projection,
  counter: TileCounter,
): Raster {
  const coverage = new LensCoverage(lens);
  const complete = Math.max(lens.minZoom, Math.min(lens.fullZoom, lens.maxZoom));
  const baseSource = source(plane, base, lens, grid, projection, complete, coverage, counter);
  const detailSource = source(plane, base, lens, grid, projection, lens.maxZoom, coverage, counter);
  log.info("a lens is open", {
    op: "render", lens: lens.name, path: lens.tiles,
    complete, deepest: lens.maxZoom, interpolate: lens.interpolate,
  });
  return {
    base: new TileLayer({ source: baseSource, zIndex: 0 }),
    // Levels above the complete one are only captured in patches. They ride
    // on top of the base so the fully-covered pyramid still shows through
    // wherever the deep capture has a gap.
    // The detail layer draws only where it has something the base does not:
    // strictly deeper than the complete level. Above that threshold the base
    // pyramid is whole and asking the patchy one for the same ground would
    // double every request a fresh view makes -- which is a number the parity
    // baselines record, so it is a correctness question and not a saving.
    // A pyramid complete to its own bottom says so with a layer that never
    // renders, rather than with a special case.
    detail: new TileLayer({
      source: detailSource,
      zIndex: 1,
      maxResolution: lens.maxZoom > complete ? levelResolution(grid, complete) : 0,
    }),
  };
}
