// Ring utilities: the one thing a renderer cannot work out for itself.
//
// A system's ring is continuous across the antimeridian (see the continuity
// rule in contract.ts), which means an honest ring may run off the surface's
// x-range by a whole world. Every renderer then faces the same question — how
// do I draw a loop that is partly somewhere else? — and the answer is the same
// for all of them: clip the ring as it lies, clip it again shifted a world
// each way, draw the pieces. The clip belongs here so no renderer owns a cell
// rule.

import type { Coordinate, Ground } from "./ground.ts";
import { surfaceExtent } from "./ground.ts";
import type { Ring } from "./contract.ts";
import type { PlanCell } from "./plan.ts";
import { worldSurface } from "../semconv/geometry.ts";

/**
 * Cut a closed ring against two vertical edges — the standard
 * Sutherland–Hodgman walk, one edge at a time.
 *
 * Returns a closed ring, or `[]` when nothing survives. The result may start
 * at a different vertex than the input: a clip has no opinion about where a
 * loop begins.
 */
export function clipRingX(ring: Ring, minimumX: number, maximumX: number): Coordinate[] {
  let points = openRing(ring);
  const edges: readonly [number, (point: Coordinate) => boolean][] = [
    [minimumX, (point) => point[0] >= minimumX],
    [maximumX, (point) => point[0] <= maximumX],
  ];
  for (const [edge, keep] of edges) {
    const output: Coordinate[] = [];
    for (let at = 0; at < points.length; at++) {
      const current = points[at];
      const previous = points[(at + points.length - 1) % points.length];
      if (!current || !previous) continue;
      const currentIn = keep(current);
      if (currentIn !== keep(previous)) {
        const t = (edge - previous[0]) / (current[0] - previous[0]);
        output.push([edge, previous[1] + t * (current[1] - previous[1])]);
      }
      if (currentIn) output.push(current);
    }
    points = output;
    if (points.length === 0) return [];
  }
  const first = points[0];
  return first ? [...points, first] : [];
}

/** The ring without its closing repeat, so the walk visits each vertex once. */
function openRing(ring: Ring): Coordinate[] {
  const first = ring[0];
  const last = ring[ring.length - 1];
  if (first && last && samePoint(first, last)) return ring.slice(0, -1);
  return [...ring];
}

/** Whether two points are the same point. */
export function samePoint(a: Coordinate, b: Coordinate): boolean {
  return a[0] === b[0] && a[1] === b[1];
}

/**
 * One plan cell as the rings a renderer actually fills, clipped to the ground.
 *
 * This is the whole of what a renderer would otherwise have to work out for
 * itself, and it is three rules in one place:
 *
 *   - Most rings lie within the surface and come back as a single piece,
 *     untouched — every geohash ring does.
 *   - A cell circling a pole has its loop closed along the picture's own top
 *     or bottom edge, closing point included: a pole cell's walk ends a world
 *     over from where it began, and that closure spans the last tessellation
 *     step. Dropping it leaves a sliver of ground the fill never covers, one
 *     step wide, at the walk's own longitude.
 *   - A ring that stayed continuous across the antimeridian is drawn twice —
 *     clipped as it lies and clipped shifted a world each way — so the one
 *     cell appears as its two pieces.
 *
 * The answer is a list of outer rings: one for a plain cell, more for a cell
 * the seam cut.
 */
export function cellRings(ground: Ground, cell: PlanCell): Coordinate[][] {
  const surface = surfaceExtent(ground);
  let ring: Coordinate[] = [...cell.ring];
  const first = ring[0];
  const last = ring[ring.length - 1];
  if (cell.pole && first && last) {
    const poleY = cell.pole === "north" ? surface[3] : surface[1];
    ring = [...ring, [last[0], poleY], [first[0], poleY], first];
  }
  // Only a sphere has a seam. A plane's cell that runs past the surface's
  // x-range — a real geohash overhanging bend-or's window, a world-square
  // cell beside a windowed lens — is honestly overhanging, not wrapping, and
  // cutting it into shifted pieces would tile ghost cells over the map.
  if (worldSurface(ground.world) !== "sphere") return [ring];
  if (ring.every(([x]) => x >= surface[0] && x <= surface[2])) return [ring];
  const width = surface[2] - surface[0];
  const pieces: Coordinate[][] = [];
  for (const shift of [0, -width, width]) {
    const clipped = clipRingX(
      ring.map(([x, y]): Coordinate => [x + shift, y]),
      surface[0],
      surface[2],
    );
    if (clipped.length >= 4) pieces.push(clipped);
  }
  return pieces;
}

/** Whether a ring's last point is exactly its first. */
export function ringIsClosed(ring: Ring): boolean {
  const first = ring[0];
  const last = ring[ring.length - 1];
  return ring.length >= 2 && first !== undefined && last !== undefined && samePoint(first, last);
}

/**
 * Whether a ring closes **on this ground**: its last point is its first, some
 * whole number of worlds over in x, on the same row.
 *
 * This — not exact equality — is the closure rule a continuous ring keeps. A
 * cell that circles a pole accumulates a whole world of longitude on the way
 * round, and its loop deliberately closes in the frame the walk ended in;
 * closing back to the opening frame would hand a renderer a segment sweeping
 * the entire planet. Consumers that need the loop as drawn should ask
 * {@link cellRings}, which resolves exactly this.
 */
export function ringClosesOn(ground: Ground, ring: Ring): boolean {
  const first = ring[0];
  const last = ring[ring.length - 1];
  if (ring.length < 4 || first === undefined || last === undefined) return false;
  if (last[1] !== first[1]) return false;
  const [minimumX, , maximumX] = surfaceExtent(ground);
  const width = maximumX - minimumX;
  if (width === 0) return samePoint(first, last);
  const worlds = (last[0] - first[0]) / width;
  return Math.abs(worlds - Math.round(worlds)) < 1e-9;
}
