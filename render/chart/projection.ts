// `ATLAS:PIXELS`: the coordinate system the whole seam draws in.
//
// A volume's worlds are cut from one square of world pixels — 8192 across,
// 256 to a tile, in every published bundle — and that square is the
// projection. It has no geodesy in it and wants none: the picture is the
// authority, and a world that also knows where it is on a sphere says so
// through `atlas.geometry.*`, which is the globe's business and not this
// projection's.
//
//   origin      top-left of the world square
//   x           increases right
//   y           **decreases** downward, so the square is [0, −size] … [size, 0]
//   resolution  world pixels per screen pixel; level z is size / (256 · 2^z)
//
// The banner in the diagnostics object says exactly this, on every report of
// every volume, which makes it the most pinned sentence in the seam.

import Projection from "ol/proj/Projection.js";
import TileGrid from "ol/tilegrid/TileGrid.js";
import type { Extent } from "ol/extent.js";
import type { Lens, TileGrid as GridSpec } from "../data/payload.ts";

/** The constant banner. Carried on every diagnostics report; never reworded. */
export const COORDINATE_SYSTEM =
  "ATLAS:PIXELS; origin top-left; x increases right; y decreases downward";

/** How far past a lens's own deepest level the view may be pushed. */
export const OVERZOOM_LEVELS = 2;

/** The raster cache each tile source keeps, in tiles. Reported as a constant. */
export const RASTER_CACHE_SIZE = 64;

/** The world square as an OL extent. */
export function worldExtent(grid: GridSpec): Extent {
  return [0, -grid.size, grid.size, 0];
}

/** The projection for one volume's world square. */
export function atlasProjection(grid: GridSpec): Projection {
  return new Projection({
    code: "ATLAS:PIXELS",
    units: "pixels",
    extent: worldExtent(grid),
  });
}

/** The resolution of pyramid level `z`: world pixels per screen pixel. */
export function levelResolution(grid: GridSpec, z: number): number {
  return grid.size / grid.tileSize / 2 ** z;
}

/**
 * A tile grid over the world square, from level 0 to `deepest`.
 *
 * The origin is the square's top-left and y decreases downward, which is what
 * makes a tile's `(x, y)` the same pair the archive names its file with. There
 * is no other convention in play anywhere in the format.
 */
export function tileGridFor(grid: GridSpec, deepest: number): TileGrid {
  const resolutions: number[] = [];
  for (let z = 0; z <= deepest; z++) resolutions.push(levelResolution(grid, z));
  return new TileGrid({
    extent: worldExtent(grid),
    origin: [0, 0],
    resolutions,
    tileSize: grid.tileSize,
  });
}

/**
 * The window a lens draws into, as an OL extent — what the camera fits.
 *
 * A lens may declare two rectangles and they are not interchangeable.
 * `bounds` is the raster window the pyramid actually fills; `surface` is the
 * ground that window pictures, which on a piece of a split sheet is *smaller*
 * than the window, because the window was grown to take in a title drawn
 * beside the map.
 *
 * The camera fits **bounds**, falling back to the whole world square: a
 * reader opening a split sheet is shown everything the lens drew, title
 * included. `surface` is what anything *dividing* the world measures, so no
 * cell lands on margin — and that reading belongs to the analysis lane's
 * Ground descriptor, which is handed both and prefers the other one. A lens
 * that declares a surface and no bounds is the case that tells them apart:
 * the camera opens on the whole square regardless.
 */
export function lensExtent(lens: Lens | null, grid: GridSpec): Extent {
  const rect = lens?.bounds;
  if (!rect) return worldExtent(grid);
  return [rect.x, -(rect.y + rect.height), rect.x + rect.width, -rect.y];
}

/** The resolution at which an extent just fits a viewport of this size. */
export function fitResolution(extent: Extent, width: number, height: number): number {
  const [minimumX = 0, minimumY = 0, maximumX = 0, maximumY = 0] = extent;
  if (width <= 0 || height <= 0) return 1;
  return Math.max((maximumX - minimumX) / width, (maximumY - minimumY) / height);
}

/** The deepest zoom the view allows over a lens: its own depth, plus overzoom. */
export function viewMaxZoom(lens: Lens | null): number {
  return (lens?.maxZoom ?? 0) + OVERZOOM_LEVELS;
}
