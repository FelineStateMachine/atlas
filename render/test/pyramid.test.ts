// What a lens holds, judged against the corpus tile inventories — and
// against a split sheet small enough to inventory by hand.
//
// The corpus inventories are the record of every tile in every corpus
// pyramid, taken from the archives themselves. A reader that agrees with
// them agrees about three separate things at once: the bounds a lens fills,
// the coverage bitsets past `fullZoom` (and, as Mars shows, at every level a
// lens chooses to declare one), and the per-level format table. Disagree
// about any of them and this test names the tiles you invented and the ones
// you lost.
//
// Several lenses may name one pyramid — a split sheet keeps one archive and
// windows it three ways — so the derivation is compared as the union over
// the lenses that name it, which is what a pyramid on disk actually is. No
// corpus volume is split any more, so the invented sheet below carries that
// case, with every expected tile written out longhand.

import test from "node:test";
import { strict as assert } from "node:assert";
import { Coverage, inventoryNames, tileWindowAt } from "../data/pyramid.ts";
import { inventories, payloads, tileGrid, volumes } from "./fixtures.ts";
import { splitSheet } from "./models.ts";

test("every corpus pyramid is exactly the tiles the seam would ask for", () => {
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
  assert.equal(pyramids, 3, "bend-or's basemap and both mars captures were checked");
});

test("a split sheet's lenses share one pyramid, and the pyramid is their union", () => {
  // The whole inventory, worked out by hand from the three bands. Level 0 is
  // the square in one tile and every band touches it. At level 1 the 4096
  // tiles split Sky and Land across row 0 and Depths onto row 1; at level 2
  // each band owns its own row of four, and the square's bottom row belongs
  // to nobody. Level 3 is Depths alone, past its `fullZoom`, and its bitset
  // admits exactly three of the sixteen tiles its window touches.
  const expected = [
    "0/0/0.webp",
    "1/0/0.webp", "1/0/1.webp", "1/1/0.webp", "1/1/1.webp",
    "2/0/0.webp", "2/0/1.webp", "2/0/2.webp",
    "2/1/0.webp", "2/1/1.webp", "2/1/2.webp",
    "2/2/0.webp", "2/2/1.webp", "2/2/2.webp",
    "2/3/0.webp", "2/3/1.webp", "2/3/2.webp",
    "3/0/4.webp", "3/4/4.webp", "3/7/5.webp",
  ];
  const sheet = splitSheet();
  const lenses = sheet.worlds.get("aloft")?.lenses ?? [];
  assert.equal(new Set(lenses.map((lens) => lens.tiles)).size, 1, "one archive, three windows");

  const union = new Set<string>();
  for (const lens of lenses) {
    for (const name of inventoryNames(lens, sheet.tileGrid)) union.add(name);
  }
  assert.deepEqual([...union].sort(), expected, "the pyramid on disk, tile for tile");

  // And each band asks only for its own window: the level-0 tile is the one
  // tile every lens shares, so the union is smaller than the sum.
  const counts = lenses.map((lens) => inventoryNames(lens, sheet.tileGrid).length);
  assert.deepEqual(counts, [7, 7, 10], "Sky, Land, and Depths with its three deep tiles");
  assert.ok(counts.reduce((held, count) => held + count, 0) > union.size,
    "the shared root tile is derived by every lens and stored once");
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
