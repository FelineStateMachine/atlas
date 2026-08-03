// The plan, and the order that is its contract.
//
// The order is compared positionally wherever a plan is checked — here, and
// in the property suite (properties.test.ts) that reconstructs it from the
// rule alone. These tests document what that order *is*, so a reader can
// learn the rule without decoding a reconstruction.

import assert from "node:assert/strict";
import test from "node:test";

import { ancestorChain, cellPlan } from "../cellsystems/plan.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { s2System } from "../cellsystems/s2.ts";
import { sphere, square } from "./grounds.ts";

const roles = (held: string) =>
  cellPlan(square, geohashSystem, held).map((cell) => `${cell.role}:${cell.hash}`);

test("the root plan is the root's children and nothing else", () => {
  const plan = cellPlan(square, geohashSystem, "");
  assert.equal(plan.length, 32);
  assert.ok(plan.every((cell) => cell.role === "child"));
  assert.deepEqual(plan.map((cell) => cell.hash), geohashSystem.on(square).children(""));
});

test("a held cell emits its ancestors' neighbours, then itself, then its children", () => {
  const plan = roles("m6");
  // 31 neighbours at the root, 31 more inside m, then the scope and 32 children.
  assert.equal(plan.length, 31 + 31 + 1 + 32);
  assert.equal(plan[0], "neighbor:0");
  assert.equal(plan[30], "neighbor:z", "the root's neighbours come first, in child order");
  assert.equal(plan[31], "neighbor:m0");
  assert.equal(plan[62], "scope:m6", "the held cell sits between its context and its subdivision");
  assert.equal(plan[63], "child:m60");
  assert.equal(plan[94], "child:m6z");
  assert.ok(!plan.includes("neighbor:m"), "the cell on the path is never its own neighbour");
  assert.ok(!plan.includes("neighbor:m6"));
});

test("context distance counts levels up from the held cell", () => {
  const plan = cellPlan(square, geohashSystem, "m6");
  const distances = plan
    .filter((cell) => cell.role === "neighbor")
    .map((cell) => cell.contextDistance);
  assert.deepEqual([...new Set(distances)], [2, 1],
    "the root's neighbours are two levels out; m's siblings are one");
  assert.ok(plan.filter((cell) => cell.role !== "neighbor")
    .every((cell) => cell.contextDistance === 0));
});

test("a cell at the floor is a leaf, and nothing follows it", () => {
  const plan = cellPlan(square, geohashSystem, "m6s");
  const last = plan[plan.length - 1];
  assert.equal(last?.role, "leaf");
  assert.equal(last?.hash, "m6s");
  assert.equal(plan.filter((cell) => cell.role === "child").length, 0,
    "a leaf has nothing to divide");
  assert.equal(plan.filter((cell) => cell.role === "scope").length, 0,
    "and it is not also a scope");
  assert.equal(plan.length, 31 * 3 + 1);
});

test("the order is the same order for a system shaped nothing like geohash", () => {
  const plan = cellPlan(sphere, s2System, "5");
  assert.deepEqual(plan.map((cell) => `${cell.role}:${cell.hash}`), [
    "neighbor:1", "neighbor:3", "neighbor:7", "neighbor:9", "neighbor:b",
    "scope:5",
    "child:44", "child:4c", "child:54", "child:5c",
  ]);
});

test("an S2 cell at the floor is a leaf too", () => {
  const plan = cellPlan(sphere, s2System, "47a1cb");
  const last = plan[plan.length - 1];
  assert.equal(last?.role, "leaf");
  assert.equal(last?.hash, "47a1cb");
  // Five neighbours at the root, three at each of ten levels, then the leaf.
  assert.equal(plan.length, 5 + 3 * 10 + 1);
});

test("every plan cell carries the five things a renderer reads", () => {
  for (const cell of cellPlan(sphere, s2System, "5")) {
    assert.equal(typeof cell.hash, "string");
    assert.equal(cell.extent.length, 4);
    assert.ok(cell.ring.length >= 4);
    assert.ok(cell.pole === null || cell.pole === "north" || cell.pole === "south");
    assert.ok(Number.isInteger(cell.childIndex));
  }
  // The cells of that plan that circle a pole say so, and they are the ones a
  // renderer has to close along the picture's own top or bottom edge: the
  // opposite face over in the context, the held face, and the one child of it
  // the pole actually falls in.
  const polar = cellPlan(sphere, s2System, "5").filter((cell) => cell.pole !== null);
  assert.deepEqual(polar.map((cell) => `${cell.hash}:${cell.pole}`),
    ["b:south", "5:north", "54:north"]);
});

test("the ancestor chain walks root first", () => {
  assert.deepEqual(ancestorChain(geohashSystem.on(square), "m6s"), ["", "m", "m6", "m6s"]);
  assert.deepEqual(ancestorChain(geohashSystem.on(square), ""), [""]);
  assert.deepEqual(ancestorChain(s2System.on(sphere), "44"), ["", "5", "44"]);
});

test("session state is an argument, so one ground plans two ways at once", () => {
  const geohash = cellPlan(sphere, geohashSystem, "m");
  const s2 = cellPlan(sphere, s2System, "5");
  assert.notEqual(geohash.length, s2.length);
  // Neither call disturbed the other, because neither wrote anything down.
  assert.deepEqual(cellPlan(sphere, geohashSystem, "m"), geohash);
});
