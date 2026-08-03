// `ATLAS:PIXELS`, judged against numbers worked out by hand.
//
// The projection is five facts: the banner, the window the camera fits, the
// resolution that fit implies, and the two depths — the lens's own and the
// one the view allows over it. Each one is derivable on paper from the
// documented arithmetic (`chart/projection.ts`'s own header), so each is
// derived on paper here and inlined, with the mars corpus volume as the one
// real-data anchor and invented lenses for the shapes the corpus does not
// carry.
//
// The window every derivation assumes: 1280 × 720 with the legend beside the
// map, which leaves the map 909 pixels wide — stated here rather than fitted
// backwards, exactly as the retired parity harness stated it.

import test from "node:test";
import { strict as assert } from "node:assert";
import {
  COORDINATE_SYSTEM, OVERZOOM_LEVELS, fitResolution, lensExtent, levelResolution, viewMaxZoom,
} from "../chart/projection.ts";
import { payloads, tileGrid } from "./fixtures.ts";
import { SQUARE, gamePlane, splitSheet } from "./models.ts";

const MAP_WIDTH = 909;
const MAP_HEIGHT = 659;

test("the banner is the constant every diagnostics snapshot carries", () => {
  // The sentence is the contract: it was recorded on every step of every
  // parity baseline, and it may never be reworded.
  assert.equal(COORDINATE_SYSTEM,
    "ATLAS:PIXELS; origin top-left; x increases right; y decreases downward");
});

test("a level's resolution is the world square over its tiles", () => {
  const grid = tileGrid("mars");
  assert.equal(levelResolution(grid, 0), 32, "the square in one tile");
  assert.equal(levelResolution(grid, 6), 0.5, "sixty-four tiles across");
  assert.equal(levelResolution(grid, 6), grid.size / 2 ** 6 / grid.tileSize);
});

test("Mars opens at the fit its own bounds imply", () => {
  const grid = tileGrid("mars");
  const payload = payloads("mars").get("global");
  assert.ok(payload, "the corpus carries the world");
  const lens = payload.lenses[0];
  assert.ok(lens, "the world has a lens");

  // The lens declares an 8192 × 4096 window anchored at the origin, and the
  // sign flips exactly once on the way into OL coordinates.
  const extent = lensExtent(lens, grid);
  assert.deepEqual(JSON.parse(JSON.stringify(extent)), [0, -4096, 8192, 0]);

  // The fit is whichever axis is tighter: 8192/909 ≈ 9.012 beats 4096/659 ≈
  // 6.215, so the width decides and the resolution is exactly 8192/909.
  const resolution = fitResolution(extent, MAP_WIDTH, MAP_HEIGHT);
  assert.equal(resolution, 8192 / 909, "the resolution the whole world fits at");

  // The view's zoom is the log of the fit against the level-zero resolution,
  // which is what makes zoom 0 the square in one tile on every volume:
  // log2(32 / (8192/909)) = log2(909/256) ≈ 1.8281364841941070.
  const zoom = Math.log2(levelResolution(grid, 0) / resolution);
  assert.ok(Math.abs(zoom - Math.log2(909 / 256)) < 1e-12, `${zoom} is the derived fit`);
  assert.ok(Math.abs(zoom - 1.828136484194107) < 1e-12, "and the paper number agrees");

  assert.equal(lens.maxZoom, 6, "the lens's own depth, as the corpus declares it");
  assert.equal(viewMaxZoom(lens), 8, "the depth the view allows over it");
  assert.equal(viewMaxZoom(lens) - lens.maxZoom, OVERZOOM_LEVELS, "two levels of overzoom");
  assert.equal(lens.interpolate, true, "a photograph is resampled smoothly");
});

test("the camera fits the window a lens drew, not the ground inside it", () => {
  // The split sheet's three lenses each window one band of the square, and
  // one of them declares a smaller surface too — the title in the margin.
  // The camera fits **bounds** every time: a reader opening a piece of a
  // split sheet is shown everything the lens drew, title included.
  const sheet = splitSheet();
  const lenses = sheet.worlds.get("aloft")?.lenses ?? [];
  assert.equal(lenses.length, 3, "the invented sheet is really split");
  for (const lens of lenses) {
    const rect = lens.bounds;
    assert.ok(rect, `${lens.name} declares a window`);
    assert.deepEqual(lensExtent(lens, sheet.tileGrid),
      [rect.x, -(rect.y + rect.height), rect.x + rect.width, -rect.y]);
  }
  // The bands, worked out by hand: y grows downward in a payload and the
  // flip pins each band between its negated bottom and top edges. Compared
  // through the serializer, because the top band's top edge is the seam's
  // one honest `-0` — a lens anchored at y = 0, negated.
  const bands = lenses.map((lens) => lensExtent(lens, sheet.tileGrid));
  assert.deepEqual(JSON.parse(JSON.stringify(bands)), [
    [0, -2048, 8192, 0],
    [0, -4096, 8192, -2048],
    [0, -6144, 8192, -4096],
  ]);
});

test("a lens with a surface and no bounds opens on the whole world square", () => {
  // The invented hand-drawn lens is the one that tells the two rectangles
  // apart: it declares a 5,066 × 4,191 surface and no bounds, and the camera
  // must fit the 8192 square — the surface belongs to whatever divides the
  // world, not to the fit.
  const city = gamePlane();
  const lens = city.worlds.get("old-quarter")?.lenses.find((held) => held.name === "Hand-Drawn");
  assert.ok(lens?.surface, "the lens declares a surface");
  assert.equal(lens.bounds, undefined, "and no bounds");
  assert.deepEqual(lensExtent(lens, SQUARE), [0, -8192, 8192, 0]);

  // In a 648-pixel-high map the square's height decides: 8192/648 ≈ 12.642
  // beats 8192/909, and the fit zoom is log2(32/(8192/648)) = log2(648/256).
  const resolution = fitResolution(lensExtent(lens, SQUARE), MAP_WIDTH, 648);
  assert.equal(resolution, 8192 / 648);
  assert.ok(Math.abs(
    Math.log2(levelResolution(SQUARE, 0) / resolution) - Math.log2(648 / 256)) < 1e-12);
});
