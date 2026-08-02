// The Ground descriptor: what a cell system is handed instead of reaching
// into a running application (issue #5 §5.4).
//
// The pre-rewrite systems read three globals — the volume's tile grid, the
// open lens, the current world — which is what made them untestable outside a
// browser holding an app. A Ground is exactly those reads, written down, and
// nothing else. `golden/analysis/vectors/grounds.json` records the same shape
// a milestone early and the vectors gate holds this module to its numbers.
//
// What is deliberately NOT here: the active system, the held cell, whether
// the subdivision is showing. Those are session state (§4.1) and they arrive
// as arguments — `cellPlan(ground, system, cellID)` — which is what lets one
// ground be divided two ways at once by two callers who never meet.

import type { World } from "../semconv/geometry.ts";

/** A rectangle in **y-down world pixels**, the space a lens declares itself in. */
export interface Rect {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/**
 * One raster picture of a ground. `surface` is the ground the picture covers
 * when the lens declares one; `bounds` is the raster window it actually fills.
 * Both are independently absent, and the fallback ladder below is why.
 */
export interface Lens {
  readonly surface?: Rect | null;
  readonly bounds?: Rect | null;
}

/**
 * Everything a cell system may know about where it is dividing.
 *
 * `tileGridSize` is the volume's world square — the last fallback, and null
 * when no volume is open at all. `lens` is null when no lens is open.
 */
export interface Ground {
  readonly tileGridSize: number | null;
  readonly lens: Lens | null;
  readonly world: World;
}

/** `[minX, minY, maxX, maxY]` in OL world coordinates: x east, y negative-down. */
export type Extent = readonly [number, number, number, number];

/** `[x, y]` in OL world coordinates: x east, y negative-down. */
export type Coordinate = readonly [number, number];

/**
 * The ground every cell system divides, as one extent.
 *
 * Three branches, in order: the lens's declared surface; failing that its
 * raster window; failing that the whole world square. The sign flip is the
 * conversion from the y-down pixels a bundle declares to the y-negative-down
 * coordinates every other function here speaks — the top edge of a lens
 * anchored at y = 0 comes out as -0, which is a real double and a real part
 * of the recorded goldens.
 */
export function surfaceExtent(ground: Ground): Extent {
  const surface = ground.lens?.surface;
  if (surface) {
    return [
      surface.x,
      -(surface.y + surface.height),
      surface.x + surface.width,
      -surface.y,
    ];
  }
  const size = ground.tileGridSize ?? 0;
  const bounds = ground.lens?.bounds ?? { x: 0, y: 0, width: size, height: size };
  return [bounds.x, -(bounds.y + bounds.height), bounds.x + bounds.width, -bounds.y];
}

/** The width of the ground: one whole world of x, and the seam's period. */
export function surfaceWidth(ground: Ground): number {
  const [minimumX, , maximumX] = surfaceExtent(ground);
  return maximumX - minimumX;
}

/** The area of an extent, used to compare precision across systems. */
export function extentArea(extent: Extent): number {
  const [minimumX, minimumY, maximumX, maximumY] = extent;
  return (maximumX - minimumX) * (maximumY - minimumY);
}
