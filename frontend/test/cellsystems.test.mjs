// Golden tests for the cell systems: pure math, no DOM, no OpenLayers.
// The geohash values are hand-derived from the halving rules -- not
// computed by the code under test -- so a regression in the extraction
// shows up as a number, not a tautology.
import assert from "node:assert/strict";
import test from "node:test";

import { state } from "../src/state.js";
import { clipRingX, surfaceExtent } from "../src/cellsystems/index.js";
import { geohashSystem, geohashCellAt } from "../src/cellsystems/geohash.js";

function onSquareSurface() {
  state.game = { tileGrid: { size: 1024 } };
  state.variant = { surface: { x: 0, y: 0, width: 1024, height: 1024 } };
}

test("surfaceExtent prefers the surface and falls back to bounds", () => {
  onSquareSurface();
  assert.deepEqual(surfaceExtent(), [0, -1024, 1024, -0]);
  state.variant = { bounds: { x: 128, y: 256, width: 512, height: 256 } };
  assert.deepEqual(surfaceExtent(), [128, -512, 640, -256]);
  state.variant = null;
  assert.deepEqual(surfaceExtent(), [0, -1024, 1024, -0]);
});

test("bbox halves the way geohash always has", () => {
  onSquareSurface();
  assert.deepEqual(geohashSystem.bbox(""), [0, -1024, 1024, -0]);
  // m = alphabet index 19 = 10011: x right, y bottom, x left, y up, x right.
  assert.deepEqual(geohashSystem.bbox("m"), [640, -768, 768, -512]);
  // 6 = 00110, continuing on the alternate axis.
  assert.deepEqual(geohashSystem.bbox("m6"), [672, -704, 704, -672]);
});

test("cellAt is bbox run backward", () => {
  onSquareSurface();
  const center = [688, -688];
  assert.equal(geohashSystem.descendTarget("", center), "m");
  assert.equal(geohashSystem.descendTarget("m", center), "m6");
  assert.equal(geohashCellAt(center, 3), "m6s");
  assert.equal(geohashCellAt([-1, -1], 3), "", "off the surface names nothing");
});

test("contains is boundary-inclusive", () => {
  onSquareSurface();
  assert.equal(geohashSystem.contains("m6", [704, -672]), true);
  assert.equal(geohashSystem.contains("m6", [672, -704]), true);
  assert.equal(geohashSystem.contains("m6", [704.0001, -672]), false);
});

test("normalizeInput keeps what the alphabet keeps", () => {
  onSquareSurface();
  assert.equal(geohashSystem.normalizeInput("M6X!"), "m6x");
  assert.equal(geohashSystem.normalizeInput("aM6iLo"), "m6");
  assert.equal(geohashSystem.normalizeInput("m6w9"), "m6w");
  assert.equal(geohashSystem.parseInput(""), "", "the root is a place");
});

test("clipRingX cuts a wrapped ring against the surface edges", () => {
  // The clip may start its ring at any vertex; the shape is what matters.
  const canonical = (ring) => {
    const open = ring.slice(0, -1).map(([x, y]) => `${x},${y}`);
    const start = open.indexOf([...open].sort()[0]);
    return [...open.slice(start), ...open.slice(0, start)].join(" ");
  };
  // A square straddling the right edge at x=100: the piece as it lies.
  const straddling = [[80, -10], [80, -30], [120, -30], [120, -10], [80, -10]];
  assert.equal(
    canonical(clipRingX(straddling, 0, 100)),
    canonical([[80, -10], [80, -30], [100, -30], [100, -10], [80, -10]]),
  );
  // Shifted a world (100) left, the other piece appears at the left edge.
  const shifted = straddling.map(([x, y]) => [x - 100, y]);
  assert.equal(
    canonical(clipRingX(shifted, 0, 100)),
    canonical([[0, -10], [0, -30], [20, -30], [20, -10], [0, -10]]),
  );
  // A ring entirely outside survives as nothing.
  assert.deepEqual(clipRingX(shifted.map(([x, y]) => [x - 200, y]), 0, 100), []);
  // A ring entirely inside comes back whole, closed, in its own order.
  const inside = [[10, -10], [10, -20], [20, -20], [20, -10], [10, -10]];
  assert.deepEqual(clipRingX(inside, 0, 100), inside);
});

test("plan primitives stay stable", () => {
  onSquareSurface();
  const children = geohashSystem.children("m");
  assert.equal(children.length, 32);
  assert.equal(children[0], "m0");
  assert.equal(children[31], "mz");
  assert.equal(geohashSystem.childIndex("m6"), 6);
  assert.equal(geohashSystem.parent("m6"), "m");
  assert.equal(geohashSystem.level(""), 0);
  assert.deepEqual(geohashSystem.label("m6s"), { context: "m6", principal: "s" });
  const ring = geohashSystem.ring("m6");
  assert.equal(ring.length, 5);
  assert.deepEqual(ring[0], ring[4], "the ring closes");
  assert.deepEqual(geohashSystem.locate([688, -688]), { label: "Geohash", value: "m6s" });
  assert.equal(geohashSystem.locate([-1, -1]), null);
});
