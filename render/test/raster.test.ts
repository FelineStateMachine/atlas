// What the two raster layers are configured with, judged against the lens.
//
// The tile *inventory* tests next door ask what the pair requests; these ask
// what it draws, which is the other half of the same contract. Three of the
// layer options are the lens's own declarations and two were being dropped:
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
//   `interpolate` how an overzoomed tile is stretched: smooth for a
//                 photograph, nearest-neighbour for pixel art. The seam only
//                 ever repeats what the lens declares.
//
// The cases: a lens with a background and no bounds (bend-or's Basemap, on
// the whole world square), one with bounds and no background (mars, whose
// window is the top half of the square), one bounded well inside the square
// with margin on every side (the invented city's Streets), and one with a
// surface, no bounds and `interpolate: false` (its Hand-Drawn sheet) — the
// nearest-neighbour declaration no corpus lens carries.

import test from "node:test";
import { strict as assert } from "node:assert";
import type { Extent } from "ol/extent.js";
import { DataPlane } from "../data/plane.ts";
import type { Lens, TileGrid as GridSpec } from "../data/payload.ts";
import { atlasProjection, lensExtent } from "../chart/projection.ts";
import { TileCounter, buildRaster } from "../chart/raster.ts";
import { payloads, tileGrid, volumes } from "./fixtures.ts";
import { gamePlane, splitSheet } from "./models.ts";

function lensOf(slug: string, world: string, name: string): Lens {
  const found = payloads(slug).get(world)?.lenses.find((lens) => lens.name === name);
  assert.ok(found, `${slug}/${world} carries the ${name} lens`);
  return found;
}

function invented(volume: ReturnType<typeof gamePlane>, world: string, name: string): Lens {
  const found = volume.worlds.get(world)?.lenses.find((lens) => lens.name === name);
  assert.ok(found, `${volume.slug}/${world} carries the ${name} lens`);
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
  assert.equal(lens.background, "#14181d", "the corpus declares a backdrop");

  const pair = raster(lens, grid);
  assert.equal(backdrop(pair.base), "#14181d", "the map paints what the shelf paints");
  assert.equal(backdrop(pair.detail), undefined, "the patchy layer lets the whole one through");
});

test("a lens with no backdrop leaves the layer with none", () => {
  const grid = tileGrid("mars");
  const lens = lensOf("mars", "global", "Viking MDIM 2.1");
  assert.equal(lens.background, undefined, "the corpus declares no backdrop");

  const pair = raster(lens, grid);
  assert.equal(backdrop(pair.base), undefined, "and nothing is invented for it");
  assert.equal(backdrop(pair.detail), undefined);
});

test("both layers are clipped to the window a bounded lens fills", () => {
  // The invented Streets pyramid is a 4,159 × 4,378 rectangle anchored at
  // (2016, 2020) of an 8,192 square: a quarter of the world, with real
  // margin on every side for an unclipped layer to draw into. By hand:
  // [2016, −(2020 + 4378), 2016 + 4159, −2020].
  const city = gamePlane();
  const lens = invented(city, "old-quarter", "Streets");
  assert.ok(lens.bounds, "the lens declares a window");

  const window = [2016, -6398, 6175, -2020];
  assert.deepEqual([...lensExtent(lens, city.tileGrid)], window,
    "the camera's window and the layers' agree");

  const pair = raster(lens, city.tileGrid);
  assert.deepEqual(extent(pair.base.getExtent()), window);
  assert.deepEqual(extent(pair.detail.getExtent()), window);
  assert.notDeepEqual(window, [0, -city.tileGrid.size, city.tileGrid.size, 0],
    "and it is not the whole square");
});

test("a lens with no bounds is clipped to the whole world square", () => {
  const city = gamePlane();
  const drawn = invented(city, "old-quarter", "Hand-Drawn");
  assert.equal(drawn.bounds, undefined, "the lens declares a surface and no window");
  const pair = raster(drawn, city.tileGrid);
  assert.deepEqual(extent(pair.base.getExtent()), [0, -8192, 8192, 0]);
  assert.deepEqual(extent(pair.detail.getExtent()), [0, -8192, 8192, 0]);

  // And the real city agrees: bend-or's Basemap declares a surface — the
  // whole square, as it happens — and no bounds.
  const grid = tileGrid("bend-or");
  const basemap = raster(lensOf("bend-or", "2026-08-02", "Basemap"), grid);
  assert.deepEqual(extent(basemap.base.getExtent()), [0, -grid.size, grid.size, 0]);
});

test("the resampling a lens declares is the resampling both sources are built with", () => {
  // Nearest-neighbour for pixel art, smooth for photographs — the lens says
  // which, and the seam only ever repeats it. Every corpus lens declares
  // `interpolate: true`, so the nearest-neighbour branch is exercised by the
  // invented hand-drawn sheet and by nothing real; that hole is the corpus's,
  // named in docs/render-seam.md, not the seam's.
  const mars = raster(lensOf("mars", "global", "MOLA Elevation"), tileGrid("mars"));
  assert.equal(mars.base.getSource()?.getInterpolate(), true, "a photograph, smoothly");
  assert.equal(mars.detail.getSource()?.getInterpolate(), true);

  const city = gamePlane();
  const drawn = invented(city, "old-quarter", "Hand-Drawn");
  assert.equal(drawn.interpolate, false, "the invented sheet is pixel art");
  const pair = raster(drawn, city.tileGrid);
  assert.equal(pair.base.getSource()?.getInterpolate(), false,
    "magnified with bilinear smoothing, a drawing stops being a drawing");
  assert.equal(pair.detail.getSource()?.getInterpolate(), false);
});

test("every lens, corpus and invented, gets the extent its own camera fits, on both layers", () => {
  const cases: [string, Lens, GridSpec][] = [];
  for (const slug of volumes()) {
    const grid = tileGrid(slug);
    for (const payload of payloads(slug).values()) {
      for (const lens of payload.lenses) cases.push([`${slug}/${lens.name}`, lens, grid]);
    }
  }
  for (const volume of [splitSheet(), gamePlane()]) {
    for (const [world, payload] of volume.worlds) {
      for (const lens of payload.lenses) {
        cases.push([`${volume.slug}/${world}/${lens.name}`, lens, volume.tileGrid]);
      }
    }
  }
  for (const [name, lens, grid] of cases) {
    const pair = raster(lens, grid);
    const window = [...lensExtent(lens, grid)];
    assert.deepEqual(extent(pair.base.getExtent()), window, name);
    assert.deepEqual(extent(pair.detail.getExtent()), window, name);
    assert.equal(backdrop(pair.base), lens.background, name);
    assert.equal(backdrop(pair.detail), undefined, name);
  }
  assert.equal(cases.length, 8,
    "three corpus lenses and five invented ones: every lens this suite knows");
});
