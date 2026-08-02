// S2: the system that asks the ground a question first.
//
// The vectors below are the Go library's own — the leaf under
// (49.703498679, 11.770681595) is 0x47a1cbd595522b39 and its level-10 parent
// is token 47a1cb — read through a declared equirectangular flattening. Nothing
// here re-derives spherical geometry; what these tests document is the bridge
// between s2js and world pixels, and the contract's own edges.

import assert from "node:assert/strict";
import test from "node:test";

import { s2System } from "../cellsystems/s2.ts";
import { surfaceExtent } from "../cellsystems/ground.ts";
import { sphere, sphereWithoutMapping, square } from "./grounds.ts";

const on = s2System.on(sphere);

/** The library's own test point, in the sphere ground's world pixels. */
const known = [
  ((11.770681595 + 180) / 360) * 8192,
  -(((90 - 49.703498679) / 180) * 4096),
] as const;

test("S2 divides only a ground that says what its picture is of", () => {
  assert.equal(s2System.appliesTo(sphere), true);
  assert.equal(s2System.appliesTo(square), false, "a plane declares no sphere");
  assert.equal(s2System.appliesTo(sphereWithoutMapping), false,
    "a sphere with no invertible flattening cannot be divided geodesically");
});

test("tokens carry the library's vectors, one port level down", () => {
  assert.deepEqual(on.locate(known), { label: "S2", value: "47a1cb" });
  assert.equal(on.level("47a1cb"), 11, "port level = S2 level + 1");
  assert.equal(s2System.maxLevel(sphere), 11);
  assert.equal(s2System.inputLength(sphere), 6, "a level-10 token is six characters");
});

test("ids do not refine by appending characters", () => {
  const children = on.children("47a1cb");
  assert.deepEqual(children, ["47a1ca4", "47a1cac", "47a1cb4", "47a1cbc"]);
  for (const child of children) {
    assert.equal(on.parent(child), "47a1cb", `${child} nests`);
  }
  assert.deepEqual(children.map((child) => on.childIndex(child)), [0, 1, 2, 3]);
  // The proof the contract holds nothing geohash-shaped: the first child's
  // token is not its parent's token plus a character.
  assert.ok(!children[0]?.startsWith("47a1cb"));
});

test("the six faces are the root's children, in face order", () => {
  assert.deepEqual(on.children(""), ["1", "3", "5", "7", "9", "b"]);
  assert.deepEqual(on.children("").map((id) => on.childIndex(id)), [0, 1, 2, 3, 4, 5]);
  assert.equal(on.parent("1"), "", "a face's parent is the root");
});

test("containment agrees with descent, all the way to the floor", () => {
  let id = "";
  for (let hop = 0; hop < 11; hop++) {
    id = on.descendTarget(id, known);
    assert.ok(id, `descent step ${hop} names a cell`);
    assert.equal(on.contains(id, known), true, `${id} holds its own point`);
  }
  assert.equal(id, "47a1cb", "eleven hops land on the level-10 cell");
  assert.equal(on.descendTarget(id, known), "", "the telescope has a floor");
});

test("input parses whole or not at all", () => {
  assert.equal(on.parseInput(""), "");
  assert.equal(on.parseInput("47a1cb"), "47a1cb");
  assert.equal(on.parseInput("xyz"), null, "not hex, not a place");
  assert.equal(on.parseInput("47a1cbd595522b39"), "47a1cb",
    "a deeper token clamps to the floor, re-tokenized so the spelling is canonical");
  assert.equal(on.normalizeInput("47A1 CB!"), "47a1cb");
  assert.equal(on.normalizeInput("47a1cbd595522b39"), "47a1cb", "the field stops at six");
});

test("rings close, stay continuous, and know the poles", () => {
  const ring = on.ring("47a1cb");
  assert.ok(ring.length > 8, "edges arrive tessellated");
  assert.deepEqual(ring[0], ring[ring.length - 1], "a cell away from a pole closes exactly");
  assert.equal(on.poleContained("5"), "north", "face 2 holds the north pole");
  assert.equal(on.poleContained("b"), "south", "face 5 holds the south pole");
  assert.equal(on.poleContained("47a1cb"), null);
  // A pole cell's walk accumulates a whole world of longitude, and its closure
  // joins in the frame the walk ended in — a world over from where it began.
  const polar = on.ring("5");
  const first = polar[0];
  const last = polar[polar.length - 1];
  assert.ok(first && last);
  assert.equal(last[1], first[1], "same row");
  assert.equal(Math.abs(last[0] - first[0]), 8192, "one whole world over");
});

test("a ring may run off the picture rather than wrap", () => {
  const polar = on.ring("5");
  const [, , maximumX] = surfaceExtent(sphere);
  assert.ok(polar.some((point) => point[0] > maximumX),
    "continuity beats staying inside the frame; the adapters cut it");
});

test("the root is the ground, in every method that can be handed it", () => {
  // The oracle answered these out of a token vocabulary that has no spelling
  // for the root, which produced numbers rather than errors. Nothing reaches
  // them — the root is never a plan cell — and the contract says "" is the
  // root of every system, so the clean lane keeps the contract instead.
  assert.equal(on.level(""), 0);
  assert.equal(on.parent(""), "");
  assert.equal(on.childIndex(""), 0);
  assert.equal(on.poleContained(""), null, "the whole sphere circles both poles; neither answer is honest");
  assert.deepEqual([...on.bbox("")], [...surfaceExtent(sphere)]);
  assert.deepEqual([...on.center("")], [4096, -2048]);
  assert.equal(on.contains("", known), true, "the root holds every point on the sphere");
  assert.equal(on.ring("").length, 5, "the root's ring is the picture's own rectangle");
});

test("a ground S2 refuses is a ground S2 will not measure", () => {
  const refused = s2System.on(sphereWithoutMapping);
  assert.throws(() => refused.center("47a1cb"), /appliesTo/,
    "the failure names the question the caller should have asked");
  assert.equal(refused.contains("47a1cb", known), false, "with no mapping there is no leaf");
  assert.equal(refused.locate(known), null, "silence over plausibility");
});
