// Geohash: the halving, and what the port keeps of it.
//
// The numbers below are hand-derived from the halving rules rather than
// computed by the code under test, so a regression shows up as a number and
// not as a tautology. They are the same numbers the pre-rewrite suite carried,
// which is not an accident: the vectors gate compares this system's floats
// byte for byte, and these are the ones a reader can check by hand.

import assert from "node:assert/strict";
import test from "node:test";

import {
  geohashAlphabet,
  geohashBaseDepth,
  geohashCellAt,
  geohashDepth,
  geohashExtent,
  geohashMaxDepth,
  geohashSystem,
} from "../cellsystems/geohash.ts";
import { bendOr, sphere, square, squareFromBounds, squareNoLens } from "./grounds.ts";

const on = geohashSystem.on(square);

test("the alphabet drops the four characters a reader would mishear", () => {
  assert.equal(geohashAlphabet.length, 32);
  for (const character of "ailo") {
    assert.ok(!geohashAlphabet.includes(character), `${character} is not in the alphabet`);
  }
});

test("bbox halves the way geohash always has", () => {
  // The root is the world square — a plane's own [0, -size, size, 0], whose
  // top edge is a plain 0 rather than the surface ladder's -0.
  assert.deepEqual([...on.bbox("")], [0, -1024, 1024, 0]);
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

test("the ground is an argument: the same hash names different ground on a different world", () => {
  const elsewhere = geohashSystem.on(sphere);
  assert.notDeepEqual([...elsewhere.bbox("m6")], [...on.bbox("m6")]);
  // Which is the whole point of de-globalizing: two grounds can be divided at
  // once, by two callers who never meet.
  assert.deepEqual([...on.bbox("m6")], [672, -704, 704, -672]);
});

test("a plane's grid divides the world square and stays put across lenses", () => {
  // The maintainer's ruling: a game map's grid covers the FULL map area,
  // whatever window the open lens declares, so switching lenses moves no
  // hash. The lens-window ground and the whole-square ground answer the same
  // boxes, and the bounds-only lens does too.
  assert.deepEqual([...geohashExtent(square)], [0, -1024, 1024, 0]);
  assert.deepEqual([...geohashExtent(squareFromBounds)], [0, -1024, 1024, 0]);
  assert.deepEqual([...geohashExtent(squareNoLens)], [0, -1024, 1024, 0]);
  const windowed = geohashSystem.on(squareFromBounds);
  assert.deepEqual([...windowed.bbox("m6")], [...on.bbox("m6")]);
  assert.equal(windowed.descendTarget("m", [688, -688]), "m6");
  // A sphere keeps the surface ladder: its picture IS the ground.
  assert.deepEqual([...geohashExtent(sphere)], [0, -4096, 8192, -0]);
});

// ---- the real mode: an earth-anchored plane -------------------------------
//
// bend-or declares a plane, an earth body and a Mercator window of
// -121.48204667177453 … -121.1529533282255 east-west (0.32909° of longitude)
// and 44.17572950664809 … 43.93922950850393 north-south (0.23650° of
// latitude), over the whole 8192 square. Its geohashes are the real ones.

const real = geohashSystem.on(bendOr);

test("bend-or's window comes out base depth 4, floor 6 — exactly", () => {
  // Cell spans halve from 360°/180° per the 5-bit alternation: depth 3 is
  // 1.40625° both ways (too coarse for either axis), depth 4 is 0.3515625°
  // of longitude (misses 0.32909°) by 0.17578125° of latitude (fits
  // 0.23650°). The OR fires at 4, and the three layers end at 6.
  assert.equal(geohashBaseDepth(bendOr), 4);
  assert.equal(geohashDepth(bendOr), 6);
  assert.equal(geohashSystem.inputLength(bendOr), 6);
  // Three layers of precision on every ground: maxLevel does not move.
  assert.equal(geohashSystem.maxLevel(bendOr), 3);
  // And the synthetic grounds keep their 3.
  assert.equal(geohashBaseDepth(square), 1);
  assert.equal(geohashDepth(square), geohashMaxDepth);
});

test("bend-or's cell ids are genuine WGS84 geohashes", () => {
  // Derived independently with the textbook algorithm (python) before the
  // implementation existed: the raster centre (4096, 4096 y-down) is
  // lat 44.05759758…, lng -121.3175 — geohash 9rcdxk; (1024, 2048) is
  // 9rcdux; and (9000, 4096), EAST of the declared window, still has a real
  // address: 9rcfeu. The ground is real earth, so no point is refused.
  assert.equal(geohashCellAt(bendOr, [4096, -4096]), "9rcdxk");
  assert.equal(geohashCellAt(bendOr, [1024, -2048]), "9rcdux");
  assert.equal(geohashCellAt(bendOr, [9000, -4096]), "9rcfeu");
  assert.equal(geohashCellAt(bendOr, [4096, -4096], 4), "9rcd");
  assert.deepEqual(real.locate([4096, -4096]), { label: "Geohash", value: "9rcdxk" });
});

test("the root plan is the six depth-4 cells the window intersects, reading order", () => {
  // Depth-4 cells are 0.3515625° × 0.17578125° from (-180, -90). The window
  // spans columns 166–167 and rows 761–763 of that grid — two columns, three
  // rows, north to south then west to east — and every one of the six
  // intersects with real area (the bottom row by 0.0061°, the top by
  // 0.0546°).
  assert.deepEqual(real.children(""), ["9rce", "9rcg", "9rcd", "9rcf", "9rc9", "9rcc"]);
  // The sibling ordinal is the position among the window's own cells.
  assert.equal(real.childIndex("9rcd"), 2);
  assert.equal(real.childIndex("9rce"), 0);
});

test("real cells are drawn at their full extents, overhang and all", () => {
  // Hand-derived through the window's own formulas (x linear in longitude,
  // y linear in asinh(tan lat)), cross-checked in python to 1e-10:
  // 9rcd is [-121.640625, 43.9453125] … [-121.2890625, 44.12109375], which
  // lands west of the raster (x -3947.43…) — honest overhang, not clipped.
  assert.deepEqual([...real.bbox("9rcd")],
    [-3947.432211218598, -7981.703605508047, 4803.884266171338, -1895.4089537724801]);
  // 9rcdx, one layer down: [-121.3330078125, 44.033203125] …
  // [-121.2890625, 44.0771484375], fully inside the raster.
  assert.deepEqual([...real.bbox("9rcdx")],
    [3709.969706497596, -4940.812877376354, 4803.884266171338, -3418.6763501123382]);
  const ring = real.ring("9rcd");
  assert.equal(ring.length, 5);
  assert.deepEqual(ring[0], ring[4], "the ring closes");
});

test("level counts layers on real ground, so the telescope is still three deep", () => {
  assert.equal(real.level(""), 0);
  assert.equal(real.level("9rcd"), 1);
  assert.equal(real.level("9rcdx"), 2);
  assert.equal(real.level("9rcdxk"), 3);
  assert.equal(real.parent("9rcdx"), "9rcd");
  assert.equal(real.parent("9rcd"), "", "a base cell telescopes out to the root");
  assert.equal(real.descendTarget("", [4096, -4096]), "9rcd",
    "descending from the root lands on a base cell, not a hemisphere");
  assert.equal(real.descendTarget("9rcd", [4096, -4096]), "9rcdx");
});

test("a real address is whole at base depth, and a shorter one is a draft", () => {
  assert.equal(real.normalizeInput("9RCDXK!"), "9rcdxk");
  assert.equal(real.normalizeInput("9rcdxkpb"), "9rcdxk", "the field stops at the floor");
  assert.equal(real.parseInput(""), "", "the root is a place");
  assert.equal(real.parseInput("9rc"), null, "a hemisphere-to-city prefix is a draft");
  assert.equal(real.parseInput("9rcd"), "9rcd");
});

test("real containment is the cell's own degree rectangle, boundaries inclusive", () => {
  assert.equal(real.contains("9rcd", [4096, -4096]), true);
  assert.equal(real.contains("9rcf", [4096, -4096]), false);
  assert.equal(real.contains("9rcf", [9000, -4096]), true,
    "a point outside the window is still on real ground");
  assert.equal(real.contains("", [9000, -4096]), true, "the root is the whole earth");
  const children = real.children("9rcd");
  assert.ok(children.length > 0 && children.length <= 32);
  for (const child of children) {
    assert.equal(real.parent(child), "9rcd");
  }
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
