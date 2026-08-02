// The highlight filter, run against real OpenLayers polygons but no map: the
// rule is AND across collections and OR within one, and the one-collection
// case must stay the union it has always been -- that equivalence is what
// lets the machinery land ahead of the wire that needs it.
import assert from "node:assert/strict";
import test from "node:test";

import Polygon from "ol/geom/Polygon.js";

import {
  collectionOf,
  geometryContainsCoordinate,
  groupByCollection,
  passesZoneFilters,
} from "../src/collections.js";

function square(x, y, size) {
  return new Polygon([[
    [x, y], [x + size, y], [x + size, y + size], [x, y + size], [x, y],
  ]]);
}

function record(id, geometry) {
  return { zone: { id }, geometries: [geometry] };
}

test("every v2 zone answers to the one implicit collection", () => {
  assert.equal(collectionOf({ id: 1 }), "zones");
  assert.equal(collectionOf({ id: 2 }), collectionOf({ id: 99 }));
});

test("one collection, two zones: inside either is enough", () => {
  const groups = groupByCollection([
    record(1, square(0, 0, 10)),
    record(2, square(100, 100, 10)),
  ]);
  assert.equal(groups.size, 1, "v2 zones share a single bucket");
  assert.equal(passesZoneFilters(groups, [5, 5]), true);
  assert.equal(passesZoneFilters(groups, [105, 105]), true);
  assert.equal(passesZoneFilters(groups, [50, 50]), false);
});

test("two collections: only the overlap survives", () => {
  // Grouped by hand, the way the v3 reader will: overlapping squares in
  // different collections, so only their shared corner answers both.
  const groups = new Map([
    ["districts", [record(1, square(0, 0, 10))]],
    ["watersheds", [record(2, square(5, 5, 10))]],
  ]);
  assert.equal(passesZoneFilters(groups, [7, 7]), true, "inside both");
  assert.equal(passesZoneFilters(groups, [2, 2]), false, "district only");
  assert.equal(passesZoneFilters(groups, [12, 12]), false, "watershed only");
  assert.equal(passesZoneFilters(groups, [50, 50]), false, "inside neither");
});

test("two collections, alternatives within one", () => {
  const groups = new Map([
    ["districts", [record(1, square(0, 0, 10)), record(2, square(20, 0, 10))]],
    ["watersheds", [record(3, square(0, 0, 40))]],
  ]);
  assert.equal(passesZoneFilters(groups, [5, 5]), true, "first district");
  assert.equal(passesZoneFilters(groups, [25, 5]), true, "second district");
  assert.equal(passesZoneFilters(groups, [15, 5]), false,
    "in the watershed but in neither district");
});

test("no highlights culls nothing", () => {
  assert.equal(passesZoneFilters(groupByCollection([]), [5, 5]), true);
});

test("containment keeps its pixel of grace at the border", () => {
  const geometry = square(0, 0, 10);
  assert.equal(geometryContainsCoordinate(geometry, [5, 5]), true);
  assert.equal(geometryContainsCoordinate(geometry, [10.9, 5]), true,
    "within a pixel of the edge still counts");
  assert.equal(geometryContainsCoordinate(geometry, [12, 5]), false);
});
