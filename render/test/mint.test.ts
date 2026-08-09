import test from "node:test";
import { strict as assert } from "node:assert";
import { worldExtentToWGS84 } from "../mint.ts";

test("the whole Web Mercator world becomes the whole bounded Earth", () => {
  const bounds = worldExtentToWGS84([0, -256, 256, 0], {
    sourceZoom: 0, originX: 0, originY: 0, firstTile: 0, tileSize: 256, size: 256,
  });
  assert.deepEqual(bounds, [-180, -85.051128, 180, 85.051128]);
});

test("a local Atlas window keeps its independent source tile origins", () => {
  const bounds = worldExtentToWGS84([0, -256, 256, 0], {
    sourceZoom: 2, originX: 1, originY: 1, firstTile: 0, tileSize: 256, size: 256,
  });
  assert.deepEqual(bounds, [-90, 0, 0, 66.51326]);
});

test("a malformed or empty screen rectangle is not authored", () => {
  const grid = { sourceZoom: 2, originX: 1, originY: 1, firstTile: 0, tileSize: 256, size: 256 };
  assert.equal(worldExtentToWGS84([0, 0, 0, 0], grid), null);
  assert.equal(worldExtentToWGS84([NaN, -20, 20, 0], grid), null);
});
