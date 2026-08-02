// `ATLAS:PIXELS`, judged against the numbers a recorded tour opens with.
//
// The Mars baseline's first step carries the camera the chart arrives at
// before anyone touches it: the banner, the fit, the resolution that fit
// implies, and the two depths — the lens's own and the one the view allows
// over it. Those five numbers are the whole projection, so this test derives
// them from the fixture's payload and holds them to the tour.
//
// The one thing the baseline knows and a fixture does not is the window the
// tour ran in: 1280 × 720 with the legend beside the map, which leaves the
// map 909 pixels wide. That is where the recorded resolution comes from, and
// it is stated here rather than fitted backwards.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { strict as assert } from "node:assert";
import {
  COORDINATE_SYSTEM, OVERZOOM_LEVELS, fitResolution, lensExtent, levelResolution, viewMaxZoom,
} from "../chart/projection.ts";
import { FIXTURES, payloads, tileGrid } from "./fixtures.ts";

const MAP_WIDTH = 909;
const MAP_HEIGHT = 659;

interface Snapshot {
  coordinateSystem: string;
  zoom: number;
  center: [number, number];
  resolution: number;
  nativeMaxZoom: number;
  maxZoom: number;
  interpolate: boolean;
  fitZoom: number;
}

function initial(slug: string): Snapshot {
  const tour = JSON.parse(readFileSync(
    join(FIXTURES, "..", "..", "parity", slug, "tour.json"), "utf8")) as {
      steps: { name: string; snapshot: Snapshot }[];
    };
  const found = tour.steps.find((step) => step.name === "initial");
  assert.ok(found, `${slug} opens somewhere`);
  return found.snapshot;
}

test("the banner is the constant every baseline carries", () => {
  for (const slug of ["mars", "tunic", "bend-or"]) {
    assert.equal(initial(slug).coordinateSystem, COORDINATE_SYSTEM);
  }
});

test("a level's resolution is the world square over its tiles", () => {
  const grid = tileGrid("mars");
  assert.equal(levelResolution(grid, 0), 32, "the square in one tile");
  assert.equal(levelResolution(grid, 6), 0.5, "sixty-four tiles across");
  assert.equal(levelResolution(grid, 6), grid.size / 2 ** 6 / grid.tileSize);
});

test("Mars opens at the fit the tour recorded", () => {
  const recorded = initial("mars");
  const grid = tileGrid("mars");
  const payload = payloads("mars").get("global");
  assert.ok(payload, "the fixture carries the world");
  const lens = payload.lenses[0];
  assert.ok(lens, "the world has a lens");

  const extent = lensExtent(lens, grid);
  assert.deepEqual(JSON.parse(JSON.stringify(extent)), [0, -4096, 8192, 0]);

  const resolution = fitResolution(extent, MAP_WIDTH, MAP_HEIGHT);
  assert.equal(resolution, recorded.resolution, "the resolution the whole world fits at");

  // The view's zoom is the log of the fit against the level-zero resolution,
  // which is what makes zoom 0 the square in one tile on every volume.
  const zoom = Math.log2(levelResolution(grid, 0) / resolution);
  assert.ok(Math.abs(zoom - recorded.fitZoom) < 1e-12, `${zoom} is the recorded fit`);
  assert.equal(recorded.zoom, recorded.fitZoom, "a volume opens at its fit");

  assert.equal(lens.maxZoom, recorded.nativeMaxZoom, "the lens's own depth");
  assert.equal(viewMaxZoom(lens), recorded.maxZoom, "the depth the view allows over it");
  assert.equal(viewMaxZoom(lens) - lens.maxZoom, OVERZOOM_LEVELS, "two levels of overzoom");
  assert.equal(lens.interpolate, recorded.interpolate, "a photograph is resampled smoothly");
});

test("the resampling a lens declares is the resampling the tour recorded", () => {
  // Nearest-neighbour for pixel art, smooth for photographs — the lens says
  // which, and the seam only ever repeats it. Worth recording: **every** lens
  // in the public fixture set declares `interpolate: true`, so the
  // nearest-neighbour branch is exercised by no golden. It is pinned here as
  // "whatever the payload says", which is the part that can be checked, and
  // named in docs/render-seam.md as a hole in the fixture set rather than in
  // the seam.
  for (const slug of ["mars", "tunic", "bend-or", "zelda-tears-of-the-kingdom"]) {
    const first = [...payloads(slug).values()][0]?.lenses[0];
    assert.ok(first, `${slug} has a lens`);
    assert.equal(initial(slug).interpolate, first.interpolate, slug);
  }
});

test("a lens fits its declared surface, not the window it was cut into", () => {
  const grid = tileGrid("zelda-tears-of-the-kingdom");
  const lenses = payloads("zelda-tears-of-the-kingdom").get("hyrule")?.lenses ?? [];
  for (const lens of lenses) {
    const extent = lensExtent(lens, grid);
    const rect = lens.surface ?? lens.bounds;
    assert.ok(rect, `${lens.name} declares a window`);
    assert.deepEqual(extent, [rect.x, -(rect.y + rect.height), rect.x + rect.width, -rect.y]);
  }
});
