// Geohash: the halving, and what the port keeps of it.
//
// The numbers below are hand-derived from the halving rules rather than
// computed by the code under test, so a regression shows up as a number and
// not as a tautology. They are the same numbers the pre-rewrite suite carried,
// which is not an accident: the vectors gate compares this system's floats
// byte for byte, and these are the ones a reader can check by hand.

import assert from "node:assert/strict";
import test from "node:test";

import { geohashAlphabet, geohashCellAt, geohashMaxDepth, geohashSystem } from "../cellsystems/geohash.ts";
import { square, squareFromBounds } from "./grounds.ts";

const on = geohashSystem.on(square);

test("the alphabet drops the four characters a reader would mishear", () => {
  assert.equal(geohashAlphabet.length, 32);
  for (const character of "ailo") {
    assert.ok(!geohashAlphabet.includes(character), `${character} is not in the alphabet`);
  }
});

test("bbox halves the way geohash always has", () => {
  assert.deepEqual([...on.bbox("")], [0, -1024, 1024, -0]);
  // m = alphabet index 19 = 10011: x right, y bottom, x left, y up, x right.
  assert.deepEqual([...on.bbox("m")], [640, -768, 768, -512]);
  // 6 = 00110, continuing on the alternate axis.
  assert.deepEqual([...on.bbox("m6")], [672, -704, 704, -672]);
  assert.deepEqual([...on.bbox("m6s")], [688, -688, 692, -680]);
});

test("cellAt is bbox run backward", () => {
  const centre = [688, -688] as const;
  assert.equal(on.descendTarget("", centre), "m");
  assert.equal(on.descendTarget("m", centre), "m6");
  assert.equal(geohashCellAt(square, centre, 3), "m6s");
  assert.equal(geohashCellAt(square, [-1, -1], 3), "", "off the surface names nothing");
  assert.equal(geohashCellAt(square, centre), "m6s", "the default depth is the floor");
});

test("containment is inclusive on every edge", () => {
  assert.equal(on.contains("m6", [704, -672]), true, "the top-right corner is inside");
  assert.equal(on.contains("m6", [672, -704]), true, "the bottom-left corner is inside");
  assert.equal(on.contains("m6", [704.0001, -672]), false);
  assert.equal(on.contains("m6", [688, -672]), true, "the top edge is inside");
  // Cells sharing an edge all hold a point on it; nothing here breaks the tie,
  // because the caller asking is drawing, not partitioning. Four siblings meet
  // at m6's bottom-left corner and all four claim it.
  assert.deepEqual(
    on.children("m").filter((id) => on.contains(id, [672, -704])),
    ["m1", "m3", "m4", "m6"],
  );
  assert.deepEqual(
    on.children("m").filter((id) => on.contains(id, [688, -704])),
    ["m3", "m6"],
    "a point on a shared edge belongs to both cells",
  );
});

test("a hash that never leaves the alphabet keeps its own address", () => {
  assert.equal(on.normalizeInput("M6X!"), "m6x");
  assert.equal(on.normalizeInput("aM6iLo"), "m6", "a, i, l and o are dropped, not mapped");
  assert.equal(on.normalizeInput("m6w9"), "m6w", "the field stops at the floor");
  assert.equal(on.parseInput(""), "", "the root is a place");
  assert.equal(on.parseInput("m6"), "m6");
});

test("the label cuts the address into its context and its principal", () => {
  assert.deepEqual(on.label("m6s"), { context: "m6", principal: "s" });
  assert.deepEqual(on.label("m"), { context: "", principal: "m" });
  assert.deepEqual(on.label(""), { context: "", principal: "" });
});

test("the accent follows the last character, so neighbours differ", () => {
  assert.equal(on.colorKey("0"), 0);
  assert.equal(on.colorKey("m"), 9, "index 19, wrapped on a ten-colour wheel");
  assert.equal(on.colorKey("mz"), 1, "index 31");
  assert.equal(on.childIndex("m6"), 6);
});

test("the plan primitives stay where they were", () => {
  const children = on.children("m");
  assert.equal(children.length, 32);
  assert.equal(children[0], "m0");
  assert.equal(children[31], "mz");
  assert.equal(on.parent("m6"), "m");
  assert.equal(on.parent("m"), "");
  assert.equal(on.parent(""), "");
  assert.equal(on.level(""), 0);
  const ring = on.ring("m6");
  assert.equal(ring.length, 5);
  assert.deepEqual(ring[0], ring[4], "the ring closes");
  assert.deepEqual(on.locate([688, -688]), { label: "Geohash", value: "m6s" });
  assert.equal(on.locate([-1, -1]), null);
});

test("the ground is an argument: the same hash names different ground on a different lens", () => {
  const elsewhere = geohashSystem.on(squareFromBounds);
  assert.notDeepEqual([...elsewhere.bbox("m6")], [...on.bbox("m6")]);
  // Which is the whole point of de-globalizing: two lenses can be divided at
  // once, by two callers who never meet.
  assert.deepEqual([...on.bbox("m6")], [672, -704, 704, -672]);
});

test("geohash keeps halving past its own floor, and S2 does not", () => {
  // A frozen asymmetry, recorded rather than defended: `descendTarget` at
  // maxLevel answers a deeper cell in geohash and "" in S2. Nothing reaches
  // it — every caller stops at maxLevel first (cellPlan, equivalentCell) — but
  // a conformance suite that asserted one behaviour would be asserting a rule
  // the systems do not share. docs/analysis.md, "the floor asymmetry".
  assert.equal(geohashSystem.maxLevel(square), geohashMaxDepth);
  assert.equal(on.descendTarget("m6s", [688, -688]).length, 4,
    "geohash answers a level-4 cell when asked below its floor");
});
