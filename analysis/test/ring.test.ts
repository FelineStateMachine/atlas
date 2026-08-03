// The ring utilities: what a renderer would otherwise have to work out.

import assert from "node:assert/strict";
import test from "node:test";

import type { Coordinate } from "../cellsystems/ground.ts";
import { cellRings, clipRingX, ringClosesOn, ringIsClosed } from "../cellsystems/ring.ts";
import { cellPlan } from "../cellsystems/plan.ts";
import { s2System } from "../cellsystems/s2.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { sphere, square } from "./grounds.ts";

/** A clip may start its ring at any vertex; the shape is what matters. */
const canonical = (ring: readonly Coordinate[]) => {
  const open = ring.slice(0, -1).map(([x, y]) => `${x},${y}`);
  const start = open.indexOf([...open].sort()[0] ?? "");
  return [...open.slice(start), ...open.slice(0, start)].join(" ");
};

test("clipRingX cuts a wrapped ring against the surface edges", () => {
  const straddling: Coordinate[] = [[80, -10], [80, -30], [120, -30], [120, -10], [80, -10]];
  assert.equal(
    canonical(clipRingX(straddling, 0, 100)),
    canonical([[80, -10], [80, -30], [100, -30], [100, -10], [80, -10]]),
    "the piece as it lies",
  );
  const shifted = straddling.map(([x, y]): Coordinate => [x - 100, y]);
  assert.equal(
    canonical(clipRingX(shifted, 0, 100)),
    canonical([[0, -10], [0, -30], [20, -30], [20, -10], [0, -10]]),
    "and the other piece, a world over",
  );
  assert.deepEqual(clipRingX(shifted.map(([x, y]): Coordinate => [x - 200, y]), 0, 100), [],
    "a ring entirely outside survives as nothing");
  const inside: Coordinate[] = [[10, -10], [10, -20], [20, -20], [20, -10], [10, -10]];
  assert.deepEqual(clipRingX(inside, 0, 100), inside,
    "a ring entirely inside comes back whole, closed, in its own order");
});

test("clipRingX answers a closed ring, or none at all", () => {
  const straddling: Coordinate[] = [[80, -10], [80, -30], [120, -30], [120, -10], [80, -10]];
  assert.equal(ringIsClosed(clipRingX(straddling, 0, 100)), true);
});

test("closure on a ground allows the pole cell's whole world of drift", () => {
  const on = s2System.on(sphere);
  assert.equal(ringIsClosed(on.ring("47a1cb")), true, "an ordinary cell closes exactly");
  assert.equal(ringIsClosed(on.ring("5")), false, "a pole face does not, and must not");
  assert.equal(ringClosesOn(sphere, on.ring("5")), true, "but it closes on the ground");
  assert.equal(ringClosesOn(sphere, on.ring("47a1cb")), true);
  assert.equal(ringClosesOn(sphere, [[0, 0], [1, 0], [1, -1], [0, -5]]), false,
    "an open loop is open");
});

test("a plain cell draws as one piece, untouched", () => {
  const [cell] = cellPlan(square, geohashSystem, "m6");
  assert.ok(cell);
  const pieces = cellRings(square, cell);
  assert.equal(pieces.length, 1);
  assert.deepEqual(pieces[0], [...cell.ring], "every geohash ring is already a polygon");
});

test("a pole cell closes along the picture's own edge", () => {
  const plan = cellPlan(sphere, s2System, "5");
  const face = plan.find((cell) => cell.hash === "5");
  assert.ok(face);
  const pieces = cellRings(sphere, face);
  assert.ok(pieces.length >= 2, "and it left the surface, so it arrives cut");
  for (const piece of pieces) {
    assert.ok(piece.every(([x]) => x >= 0 && x <= 8192), "every piece is on the picture");
    assert.equal(ringIsClosed(piece), true);
    assert.ok(piece.some(([, y]) => y === 0),
      "the loop is closed along the top edge, where the north pole is drawn");
  }
});

test("a cell that only crosses the seam arrives as its two pieces", () => {
  const plan = cellPlan(sphere, s2System, "");
  const crossing = plan.filter((cell) => {
    const xs = cell.ring.map(([x]) => x);
    return Math.min(...xs) < 0 || Math.max(...xs) > 8192;
  });
  assert.ok(crossing.length > 0, "the six faces of a cube do not all fit inside one rectangle");
  for (const cell of crossing) {
    const pieces = cellRings(sphere, cell);
    assert.ok(pieces.length >= 2, `${cell.hash} should arrive as more than one piece`);
    for (const piece of pieces) {
      assert.ok(piece.every(([x]) => x >= 0 && x <= 8192));
    }
  }
});
