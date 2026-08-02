// What a lens holds, judged against the golden tile inventories.
//
// The inventories are the record of every tile in every fixture pyramid,
// taken from the archives themselves. A reader that agrees with them agrees
// about three separate things at once: the bounds a lens fills, the coverage
// bitsets past `fullZoom` (and, as Mars shows, at every level a lens chooses
// to declare one), and the per-level format table. Disagree about any of
// them and this test names the tiles you invented and the ones you lost.
//
// Several lenses may name one pyramid — Tears of the Kingdom has three, one
// per layer of a split sheet — so the derivation is compared as the union
// over the lenses that name it, which is what a pyramid on disk actually is.

import test from "node:test";
import { strict as assert } from "node:assert";
import { Coverage, inventoryNames, tileWindowAt } from "../data/pyramid.ts";
import { inventories, payloads, tileGrid, volumes } from "./fixtures.ts";

test("every golden pyramid is exactly the tiles the seam would ask for", () => {
  let pyramids = 0;
  for (const slug of volumes()) {
    const grid = tileGrid(slug);
    const list = [...payloads(slug).values()].flatMap((payload) => payload.lenses);
    const byPyramid = new Map<string, (typeof list)[number][]>();
    for (const lens of list) {
      byPyramid.set(lens.tiles, [...(byPyramid.get(lens.tiles) ?? []), lens]);
    }
    for (const inventory of inventories(slug)) {
      const derived = new Set<string>();
      for (const lens of byPyramid.get(inventory.pyramid) ?? []) {
        for (const name of inventoryNames(lens, grid)) derived.add(name);
      }
      const recorded = new Set(inventory.tiles.map((tile) => tile.name));
      const missing = [...recorded].filter((name) => !derived.has(name));
      const invented = [...derived].filter((name) => !recorded.has(name));
      assert.deepEqual(
        { missing: missing.slice(0, 5), invented: invented.slice(0, 5) },
        { missing: [], invented: [] },
        `${slug}/${inventory.pyramid}`);
      assert.equal(derived.size, inventory.count, `${slug}/${inventory.pyramid}: count`);
      pyramids++;
    }
  }
  assert.ok(pyramids >= 12, "every fixture pyramid was checked");
});

test("a coverage bitset is read least significant bit first, row major", () => {
  // Three tiles of a 4×2 window anchored at (1, 0): the second and fourth of
  // the top row and the third of the bottom one, which are indices 1, 3 and 6
  // row-major. Low bit first inside the byte makes that 0b01001010 — the
  // whole window fits in one byte, and a reader that walked the bits the
  // other way round would find the mirror of this set.
  const coverage = new Coverage({
    x: 1, y: 0, w: 4, h: 2, bits: Buffer.from([0b01001010]).toString("base64"),
  });
  assert.equal(coverage.has(2, 0), true);
  assert.equal(coverage.has(4, 0), true);
  assert.equal(coverage.has(1, 0), false);
  assert.equal(coverage.has(3, 1), true);
  assert.equal(coverage.has(1, 1), false);
  // Outside the declared window is absent, not an exception.
  assert.equal(coverage.has(9, 9), false);
  assert.equal(coverage.has(-4, 0), false);
});

test("a lens fills its own window and no more", () => {
  const grid = { sourceZoom: 13, firstTile: 4064, tileSize: 256, size: 8192 };
  const whole = {
    name: "w", tiles: "w", minZoom: 0, maxZoom: 6, fullZoom: 6, sourceZoom: 13,
    formats: ["jpg", "jpg", "jpg", "jpg", "jpg", "jpg", "jpg"], interpolate: true,
  };
  // A world square in one tile at level 0, sixty-four across at level 6.
  assert.deepEqual(tileWindowAt(whole, grid, 0), { x0: 0, x1: 1, y0: 0, y1: 1 });
  assert.deepEqual(tileWindowAt(whole, grid, 6), { x0: 0, x1: 64, y0: 0, y1: 64 });
  // Half the square in height is half the rows: the Mars case.
  const half = { ...whole, bounds: { x: 0, y: 0, width: 8192, height: 4096 } };
  assert.deepEqual(tileWindowAt(half, grid, 6), { x0: 0, x1: 64, y0: 0, y1: 32 });
});
