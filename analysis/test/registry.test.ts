// The registry, and the cross-system carry.

import assert from "node:assert/strict";
import test from "node:test";

import {
  CellSystemRegistry,
  applicableSystems,
  cellSystems,
  equivalentCell,
} from "../cellsystems/registry.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { s2System } from "../cellsystems/s2.ts";
import { districtSystem } from "./districts.ts";
import { sphere, sphereWithoutMapping, square } from "./grounds.ts";

test("the shipped registry offers geohash first", () => {
  assert.deepEqual(cellSystems.systems.map((system) => system.slug), ["geohash", "s2"]);
  assert.equal(cellSystems.get("s2"), s2System);
  assert.equal(cellSystems.get("h3"), undefined);
  assert.throws(() => cellSystems.require("h3"), /no system named "h3"/);
});

test("only the systems willing to divide a ground are offered", () => {
  assert.deepEqual(applicableSystems(square).map((system) => system.slug), ["geohash"]);
  assert.deepEqual(applicableSystems(sphere).map((system) => system.slug), ["geohash", "s2"]);
  assert.deepEqual(applicableSystems(sphereWithoutMapping).map((system) => system.slug),
    ["geohash"], "a sphere with no flattening is a picture, not a globe");
});

test("a third system is a registry entry", () => {
  const districted = { ...square, world: { attrs: { "atlas.district.grid": "3x3" } } };
  const registry = cellSystems.with(districtSystem);
  assert.deepEqual(registry.systems.map((system) => system.slug), ["geohash", "s2", "districts"]);
  assert.deepEqual(applicableSystems(districted, registry).map((system) => system.slug),
    ["geohash", "districts"]);
  // And the shipped registry is untouched: composing one is not mutating one,
  // which is what keeps "which systems exist" from depending on import order.
  assert.deepEqual(cellSystems.systems.map((system) => system.slug), ["geohash", "s2"]);
});

test("two systems may not both answer to one slug", () => {
  assert.throws(() => new CellSystemRegistry([geohashSystem, geohashSystem]),
    /both call themselves "geohash"/);
});

test("a place carries across systems at like precision", () => {
  assert.equal(equivalentCell(sphere, geohashSystem, s2System, ""), "",
    "the root is the one place two hierarchies agree exactly");
  const carried = equivalentCell(sphere, geohashSystem, s2System, "m6");
  assert.equal(carried, "213");
  assert.equal(s2System.on(sphere).contains(carried, geohashSystem.on(sphere).center("m6")), true,
    "the new cell holds the old cell's centre");
  // A level-2 geohash cell is 256×128 of the 8192×4096 world; the S2 cell
  // holding its centre at the nearest area sits within a level either way.
  assert.ok(Math.abs(s2System.on(sphere).level(carried) - 4) <= 1);
});

test("the carry round-trips onto the same ground at the same depth", () => {
  const carried = equivalentCell(sphere, geohashSystem, s2System, "m6");
  const home = equivalentCell(sphere, s2System, geohashSystem, carried);
  assert.equal(home, "m6");
  assert.equal(geohashSystem.on(sphere).contains(home, s2System.on(sphere).center(carried)), true);
});

test("the carry never answers a cell below the new system's floor", () => {
  const on = s2System.on(sphere);
  for (const held of ["m", "m6", "m6s"]) {
    const carried = equivalentCell(sphere, geohashSystem, s2System, held);
    assert.ok(on.level(carried) <= s2System.maxLevel(sphere), `${held} carried too deep`);
  }
});
