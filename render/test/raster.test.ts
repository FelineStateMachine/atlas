// What the two raster layers are configured with, judged against the lens.
//
// The tile *inventory* tests next door ask what the pair requests; these ask
// what it draws, which is the other half of the same contract. Two of the
// layer options are the lens's own declarations and were being dropped:
//
//   `background`  the colour the sheet was drawn on. The corner locator has
//                 always painted it under its thumbnail, so a map that
//                 ignored it disagreed with the shelf beside it about what
//                 the same lens looks like. It belongs to the base layer
//                 alone: on the detail layer it would paint over the complete
//                 pyramid wherever the deep capture has a gap.
//
//   `extent`      the window the pyramid fills. Coverage and `wrapX: false`
//                 refuse most requests outside it, but an unclipped layer
//                 still draws out there — a hole, or a neighbour's ground, in
//                 the margin of a sparse pyramid or a piece of a split sheet.
//
// The fixtures carry all three cases: a lens with a background and no bounds
// (bend-or's Basemap, on the whole world square), one with both (Fallout New
// Vegas's Pip-Boy over the Mojave, whose window is a rectangle well inside
// the square), and one with neither declaration (Cyberpunk's Default, which
// bounds but does not colour).

import test from "node:test";
import { strict as assert } from "node:assert";
import type { Extent } from "ol/extent.js";
import { DataPlane } from "../data/plane.ts";
import type { Lens, TileGrid as GridSpec } from "../data/payload.ts";
import { atlasProjection, lensExtent } from "../chart/projection.ts";
import { TileCounter, buildRaster } from "../chart/raster.ts";
import { payloads, tileGrid } from "./fixtures.ts";

function lensOf(slug: string, world: string, name: string): Lens {
  const found = payloads(slug).get(world)?.lenses.find((lens) => lens.name === name);
  assert.ok(found, `${slug}/${world} carries the ${name} lens`);
  return found;
}

function raster(lens: Lens, grid: GridSpec) {
  return buildRaster(
    new DataPlane(), "/data/v/x/000000000000", lens, grid, atlasProjection(grid),
    new TileCounter());
}

/** OL hands back its own array; compare the numbers. */
function extent(value: Extent | undefined): number[] | undefined {
  return value === undefined ? undefined : [...value];
}

/**
 * "No backdrop", as OL spells it.
 *
 * A layer that was never given one answers `undefined` — the declared return
 * type says `false`, which is only what an explicit *refusal* looks like. The
 * two are the same thing to a renderer and this test set does not care which
 * arrives, so both read as "nothing was painted".
 */
function backdrop(layer: { getBackground(): unknown }): unknown {
  const value = layer.getBackground();
  return value === false ? undefined : value;
}

test("the base layer carries the backdrop the lens declares, and the detail layer never does", () => {
  const grid = tileGrid("bend-or");
  const lens = lensOf("bend-or", "2026-08-02", "Basemap");
  assert.equal(lens.background, "#14181d", "the fixture declares a backdrop");

  const pair = raster(lens, grid);
  assert.equal(backdrop(pair.base), "#14181d", "the map paints what the shelf paints");
  assert.equal(backdrop(pair.detail), undefined, "the patchy layer lets the whole one through");
});

test("a lens with no backdrop leaves the layer with none", () => {
  const grid = tileGrid("cyberpunk-2077");
  const lens = lensOf("cyberpunk-2077", "night-city", "Default");
  assert.equal(lens.background, undefined, "the fixture declares no backdrop");

  const pair = raster(lens, grid);
  assert.equal(backdrop(pair.base), undefined, "and nothing is invented for it");
  assert.equal(backdrop(pair.detail), undefined);
});

test("both layers are clipped to the window a bounded lens fills", () => {
  // The Pip-Boy pyramid over the Mojave is a 4,159 x 4,378 rectangle anchored
  // at (2016, 2020) of an 8,192 square: a quarter of the world, with real
  // margin on every side for an unclipped layer to draw into.
  const grid = tileGrid("fallout-new-vegas");
  const lens = lensOf("fallout-new-vegas", "mojave-wasteland", "Pip-Boy");
  const rect = lens.bounds;
  assert.ok(rect, "the fixture declares a window");

  const window = [rect.x, -(rect.y + rect.height), rect.x + rect.width, -rect.y];
  assert.deepEqual([...lensExtent(lens, grid)], window, "the camera's window and the layers' agree");

  const pair = raster(lens, grid);
  assert.deepEqual(extent(pair.base.getExtent()), window);
  assert.deepEqual(extent(pair.detail.getExtent()), window);
  assert.notDeepEqual(window, [0, -grid.size, grid.size, 0], "and it is not the whole square");
});

test("a lens with no bounds is clipped to the whole world square", () => {
  const grid = tileGrid("tunic");
  const lens = lensOf("tunic", "world", "Default");
  assert.equal(lens.bounds, undefined, "the fixture declares a surface and no window");

  const pair = raster(lens, grid);
  assert.deepEqual(extent(pair.base.getExtent()), [0, -grid.size, grid.size, 0]);
  assert.deepEqual(extent(pair.detail.getExtent()), [0, -grid.size, grid.size, 0]);
});

test("every fixture lens gets the extent its own camera fits, on both layers", () => {
  let checked = 0;
  for (const slug of ["bend-or", "cyberpunk-2077", "fallout-new-vegas", "mars", "tunic",
    "zelda-tears-of-the-kingdom"]) {
    const grid = tileGrid(slug);
    for (const payload of payloads(slug).values()) {
      for (const lens of payload.lenses) {
        const pair = raster(lens, grid);
        const window = [...lensExtent(lens, grid)];
        assert.deepEqual(extent(pair.base.getExtent()), window, `${slug}/${lens.name}`);
        assert.deepEqual(extent(pair.detail.getExtent()), window, `${slug}/${lens.name}`);
        assert.equal(backdrop(pair.base), lens.background, `${slug}/${lens.name}`);
        assert.equal(backdrop(pair.detail), undefined, `${slug}/${lens.name}`);
        checked++;
      }
    }
  }
  assert.ok(checked >= 20, `every fixture lens was checked (${checked})`);
});
