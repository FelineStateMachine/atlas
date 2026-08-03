// The camera's boundary discipline: resilience at every door.
//
// THE DEFECT, in a reader's words: "we could end up too zoomed in that we
// enter an invalid state that is corruptive across the 2d/3d transition. it
// rendered blank due to being unable to place the camera correctly." Their
// Mars pane was entirely blank — geohash `3qd` held, the MOLA lens, the chart
// pane up — with no way back but throwing the page away.
//
// WHAT MAKES IT CORRUPTIVE RATHER THAN MERELY WRONG, confirmed in a real
// browser against the Mars fixture before a line of this was written:
//
//   1. The sphere is placed at a point of view that is not a place. three's
//      camera is `NaN` from that frame on, so the planet renders nothing —
//      "unable to place the camera correctly", exactly.
//   2. The reader flips down. The write-back inverts that camera honestly and
//      hands the chart `[NaN, NaN]`. OpenLayers keeps it: every constraint
//      passes it through, and the chart draws nothing either.
//   3. Nothing gets it back. Zoom in, zoom out, ascend the grid, swap the
//      lens: all four move the zoom and leave the centre `NaN`, because a pan
//      adds to a centre and a zoom does not touch one. Recorded readings, in
//      order: `zoom 2 centre [NaN, NaN]`, `zoom 3 centre [NaN, NaN]`,
//      `zoom 2 …`, `zoom 5.994 …`, `zoom 5.994 …`.
//
// The reference had exactly one guard against this — both coordinates checked
// for being numbers before the map was told (`frontend/src/globe.js`, "a
// broken number here once blacked out both panes with no way back") — and the
// rewrite had lost it. But finiteness alone is not the whole of the class,
// and the second half was measured in the same session: behind the sphere the
// chart's map reports a size of `[0, 0]`, and the fit a cell descent makes
// there subtracts its own padding from that, gets a negative window, and pins
// the resolution to the deepest level the lens has. So the camera the chart is
// left standing on while nobody is looking is `zoom 8` — the ceiling — over
// the cell's middle, chosen by a window that does not exist. That is right
// while the pane is away and it is the reader's view the moment it fronts.
//
// So: one sanitizer, applied wherever a camera crosses a pane boundary or a
// pane fronts, and a fallback to the world's own opening view for a camera
// that cannot be repaired. Everything below holds that.

import test from "node:test";
import { strict as assert } from "node:assert";
import { setLevel } from "../log.ts";
import type { Lens, TileGrid } from "../data/payload.ts";
import type { WorldContext } from "../context.ts";

setLevel("error");

// The seam's elements extend `HTMLElement` at definition time, and `globe.gl`
// reads the document as it loads. Both modules arrive after the page does.
const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class {};
host.window = globalThis;
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
const node = () => ({
  style: {}, classList: { add() {} }, getContext: () => null,
  appendChild() {}, insertBefore() {}, setAttribute() {}, addEventListener() {},
});
host.document = {
  createElement: node,
  createElementNS: node,
  createTextNode: node,
  getElementsByTagName: () => [node()],
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
  head: node(),
  body: node(),
};

const { AtlasChart, saneCamera, sameCamera } = await import("../chart/element.ts");
const { AtlasGlobe, altitudeForZoom } = await import("../globe/element.ts");
type Camera = import("../chart/element.ts").Camera;

// ---- the world these tests are about ---------------------------------
//
// Mars, as `testdata/corpus/bundles/mars` declares it: one 8192-pixel square,
// two lenses of six native levels each, so the view's ceiling is 8.

const GRID: TileGrid = { sourceZoom: 13, firstTile: 4064, tileSize: 256, size: 8192 };

const MOLA: Lens = {
  name: "MOLA Elevation", tiles: "global__mola-elevation", minZoom: 0, maxZoom: 6,
  fullZoom: 0, sourceZoom: 13, formats: ["png"], interpolate: false,
  bounds: { x: 0, y: 0, width: 8192, height: 4096 },
};

/** The lens ceiling: six native levels and two of overzoom. */
const CEILING = 8;

/** The world square, in the seam's own coordinates: y decreases downward. */
const WORLD = { minX: 0, minY: -4096, maxX: 8192, maxY: 0 };

/** The reader's camera in the reproduction: the `3qd` cell, at the ceiling. */
const HELD: Camera = { x: 1360, y: -2224, zoom: 8, rotation: 0 };

// ---- the sanitizer's table -------------------------------------------

test("a healthy camera comes back out of the sanitizer as itself", () => {
  const healthy: Camera[] = [
    HELD,
    { x: 4096, y: -2048, zoom: 0, rotation: 0 },
    { x: 0, y: -4096, zoom: CEILING, rotation: 0 },
    { x: 3605.162673611111, y: -1713.4931195970112, zoom: 4.394578925885768, rotation: 0 },
    // `globe-left` in the recorded Mars tour: the camera the sphere hands back
    // from its farthest distance. Every bound globe step's camera goes through
    // this function now, so "unmoved" has to mean unmoved to the last bit.
    { x: 4096, y: -2048, zoom: 1.3219280948873622, rotation: 0 },
  ];
  for (const camera of healthy) {
    const sane = saneCamera(camera, MOLA, GRID);
    assert.ok(sane, `${JSON.stringify(camera)} is a place`);
    assert.ok(sameCamera(sane, camera), `and is answered bit for bit: ${JSON.stringify(sane)}`);
  }
});

test("a camera that is not a number is refused rather than repaired", () => {
  // Refused, not clamped: there is no number `NaN` was nearly, and a guess
  // put on a view is a reader looking at somewhere nobody chose. The caller's
  // answer to a refusal is the world's own fit.
  const broken: Camera[] = [
    { x: Number.NaN, y: -2224, zoom: 8, rotation: 0 },
    { x: 1360, y: Number.NaN, zoom: 8, rotation: 0 },
    { x: Number.NaN, y: Number.NaN, zoom: 2, rotation: 0 },
    { x: 1360, y: -2224, zoom: Number.NaN, rotation: 0 },
    { x: 1360, y: -2224, zoom: 8, rotation: Number.NaN },
    { x: Number.POSITIVE_INFINITY, y: -2224, zoom: 8, rotation: 0 },
    { x: 1360, y: Number.NEGATIVE_INFINITY, zoom: 8, rotation: 0 },
    { x: 1360, y: -2224, zoom: Number.POSITIVE_INFINITY, rotation: 0 },
  ];
  for (const camera of broken) {
    assert.equal(saneCamera(camera, MOLA, GRID), null, JSON.stringify(camera));
  }
  assert.equal(saneCamera(null, MOLA, GRID), null, "and nothing at all is not a place either");
  assert.equal(saneCamera(undefined, MOLA, GRID), null);
});

test("a zoom past the lens's own depth is held to it", () => {
  // Finite and useless: the pyramid has no level there, so the raster serves
  // nothing however long the reader waits.
  const deep = saneCamera({ ...HELD, zoom: 99 }, MOLA, GRID);
  assert.equal(deep?.zoom, CEILING, "the ceiling is the lens's, plus its overzoom");
  const shallow = saneCamera({ ...HELD, zoom: -3 }, MOLA, GRID);
  assert.equal(shallow?.zoom, 0, "and the floor is the whole world");
  assert.equal(deep?.x, HELD.x, "clamping the depth moves nothing else");
  assert.equal(deep?.y, HELD.y);
});

test("a centre outside the world square is brought back onto it", () => {
  const east = saneCamera({ ...HELD, x: 99_999 }, MOLA, GRID);
  assert.equal(east?.x, WORLD.maxX, "east of everything drawn is the eastern edge");
  const west = saneCamera({ ...HELD, x: -5 }, MOLA, GRID);
  assert.equal(west?.x, WORLD.minX);
  const under = saneCamera({ ...HELD, y: -99_999 }, MOLA, GRID);
  assert.equal(under?.y, WORLD.minY, "y decreases downward, so the floor is negative");
  const over = saneCamera({ ...HELD, y: 5 }, MOLA, GRID);
  // `===` rather than `assert.equal`, which is `Object.is`: negating the
  // lens's own top edge spells `-0`, and `-0` is the top edge.
  assert.ok(over?.y === WORLD.maxY, `${over?.y}`);
});

test("a lens that draws only part of the square keeps the camera inside its part", () => {
  // The extent is the *lens's*, not the world's: a split sheet's layer draws
  // one box out of one pyramid, and a camera outside that box is over ground
  // this lens never drew.
  const corner: Lens = { ...MOLA, bounds: { x: 1024, y: 512, width: 2048, height: 1024 } };
  const sane = saneCamera({ x: 8000, y: -4000, zoom: 3, rotation: 0 }, corner, GRID);
  assert.deepEqual([sane?.x, sane?.y], [1024 + 2048, -(512 + 1024)],
    "the far corner of what this lens drew");
});

// ---- every boundary applies it ---------------------------------------
//
// The chart is asked as an object rather than as an element: nothing below
// wants a page, a WebGL context or a tile server, and what is under test is
// which numbers reach the view.

/** A view that records what it was told, and answers what it was last told. */
function stubView(camera: Camera = HELD) {
  return {
    centre: [camera.x, camera.y] as number[] | undefined,
    zoom: camera.zoom as number | undefined,
    rotation: camera.rotation,
    fits: [] as { extent: number[]; size: number[] }[],
    getCenter() { return this.centre; },
    getZoom() { return this.zoom; },
    getRotation() { return this.rotation; },
    getResolution() { return this.zoom === undefined ? undefined : 32 / 2 ** this.zoom; },
    setCenter(at: number[]) { this.centre = at; },
    setZoom(to: number) { this.zoom = to; },
    setRotation(to: number) { this.rotation = to; },
    fit(extent: number[], options: { size: number[] }) {
      const [minX = 0, minY = 0, maxX = 0, maxY = 0] = extent;
      this.fits.push({ extent, size: options.size });
      this.centre = [(minX + maxX) / 2, (minY + maxY) / 2];
      this.zoom = 0;
    },
    animate() {},
  };
}

function context(lens: Lens | null = MOLA): WorldContext {
  return {
    lens, grid: GRID, model: { slug: "global" },
  } as unknown as WorldContext;
}

/**
 * The host's own two lines when the sphere goes down, in the host's own order
 * (`AtlasViewport.flipPane`): the write-back if there is one, then the check
 * that the pane coming up is standing somewhere.
 */
function flipDown(chart: InstanceType<typeof AtlasChart>, handed: Camera | null): void {
  if (handed) chart.goTo(handed.x, handed.y, handed.zoom, handed.rotation);
  chart.front();
}

/** A chart with a view under it and nothing else: no page, no map, no tiles. */
function chartOver(
  view: ReturnType<typeof stubView>, { size = [909, 648] as number[] | undefined } = {},
): InstanceType<typeof AtlasChart> {
  const chart = Object.create(AtlasChart.prototype) as Record<string, unknown>;
  chart.view = view;
  chart.context = context();
  chart.map = { getSize: () => size };
  chart.clientWidth = 909;
  chart.clientHeight = 648;
  return chart as unknown as InstanceType<typeof AtlasChart>;
}

test("the one door: a camera that is not a place never reaches the view", () => {
  const view = stubView();
  const chart = chartOver(view);
  chart.goTo(Number.NaN, Number.NaN, 4);
  assert.deepEqual(view.centre, [4096, -2048], "the world's own middle, from the fit");
  assert.equal(view.fits.length, 1, "and it got there by fitting the world rather than by guessing");
  // `-0` is what negating the lens's own top edge answers, and it is the same
  // place as `0`: compared as numbers rather than as JSON.
  assert.ok(view.fits[0]?.extent.every((edge, at) => edge === [0, -4096, 8192, 0][at]),
    JSON.stringify(view.fits[0]?.extent));
});

test("the one door holds a camera deeper than the lens to the lens", () => {
  const view = stubView();
  chartOver(view).goTo(1360, -2224, 99);
  assert.equal(view.zoom, CEILING);
  assert.deepEqual(view.centre, [1360, -2224]);
  assert.equal(view.fits.length, 0, "a camera that can be repaired is repaired, not thrown away");
});

test("a healthy camera through the door is put on the view untouched", () => {
  const view = stubView({ x: 0, y: 0, zoom: 0, rotation: 0 });
  chartOver(view).goTo(HELD.x, HELD.y, HELD.zoom, HELD.rotation);
  assert.deepEqual(view.centre, [HELD.x, HELD.y]);
  assert.equal(view.zoom, HELD.zoom);
});

test("fronting leaves a healthy standing camera completely alone", () => {
  // Not "leaves it equal": leaves it *untouched*. A `setCenter` here would
  // raise a move on every pane swap, and a move is a report to the session, a
  // recount in the footer and a redrawn corner locator — three things the
  // recorded globe steps pin and none of which a pane coming up is.
  const view = stubView(HELD);
  const centre = view.centre;
  chartOver(view).front();
  assert.equal(view.centre, centre, "the very same array, never written again");
  assert.equal(view.fits.length, 0);
});

test("fronting over a camera that is not a place opens the world instead", () => {
  const view = stubView({ x: Number.NaN, y: Number.NaN, zoom: 2, rotation: 0 });
  const chart = chartOver(view);
  chart.front();
  assert.equal(view.fits.length, 1, "the world's own opening view, which is the way back");
  assert.deepEqual(view.centre, [4096, -2048]);
});

test("fronting a camera deeper than the lens brings it back to the ceiling", () => {
  // The camera behind the sphere is fitted into a window of no size and lands
  // at whatever the deepest level is. Under a lens that goes shallower than
  // the one it was fitted under, that is past the ceiling — and fronting is
  // where it is asked.
  const view = stubView({ ...HELD, zoom: 12 });
  chartOver(view).front();
  assert.equal(view.zoom, CEILING);
});

test("the fit a broken camera falls back to is measured in a real window", () => {
  // A pane coming up out from behind the sphere still reports `[0, 0]` until
  // the size observer catches up, and fitting into no window is the very
  // arithmetic that makes a camera suspect. The element's own measurement is
  // the fallback.
  const view = stubView({ x: Number.NaN, y: 0, zoom: 2, rotation: 0 });
  chartOver(view, { size: [0, 0] }).front();
  assert.deepEqual(view.fits[0]?.size, [909, 648], "the pane's, not the map's zero");
  const none = stubView({ x: Number.NaN, y: 0, zoom: 2, rotation: 0 });
  chartOver(none, { size: undefined }).front();
  assert.deepEqual(none.fits[0]?.size, [909, 648]);
});

test("a zoom press over a camera standing nowhere puts the world back", () => {
  // `?? 0` catches a view with no zoom and not a view whose zoom is not a
  // number, and `NaN + 1` is `NaN`: this is why the reader's every press did
  // nothing once the pane had gone. A press is now the way out.
  const view = stubView({ x: 1360, y: -2224, zoom: Number.NaN, rotation: 0 });
  const chart = chartOver(view);
  chart.nudgeZoom(1);
  assert.equal(view.fits.length, 1, "one press is enough to be looking at the world again");
  assert.equal(view.zoom, 0);
});

// ---- the sphere's two ends -------------------------------------------

/** A globe with the Mars mapping under it and a stub sphere in place of one. */
function globeOver(pov: { lat: number; lng: number; altitude: number }) {
  const placed: { pov: unknown; ms: number }[] = [];
  const sphere = {
    at: { ...pov },
    pointOfView(want?: { lat: number; lng: number; altitude: number }, ms?: number) {
      if (!want) return this.at;
      this.at = { ...want };
      placed.push({ pov: { ...want }, ms: ms ?? 0 });
      return this.at;
    },
    width() {}, height() {},
  };
  const globe = Object.create(AtlasGlobe.prototype) as Record<string, unknown>;
  // The three passes a sphere makes over its own scene graph, stood down: what
  // is under test is which numbers reach the camera, and none of them wants a
  // pin, a name or a tile.
  globe.update = () => {};
  globe.refreshDetail = () => {};
  globe.clearDetail = () => {};
  globe.globe = sphere;
  globe.context = context();
  globe.equirect = {
    px: [0, 0, 8192, 4096],
    deg: [-180, 90, 180, -90],
    mapping: {
      toLatLng: (x: number, y: number): [number, number] =>
        [90 - (y / 4096) * 180, -180 + (x / 8192) * 360],
      toWorld: (lat: number, lng: number): [number, number] =>
        [((lng + 180) / 360) * 8192, ((90 - lat) / 180) * 4096],
    },
  };
  globe.clientWidth = 909;
  globe.clientHeight = 648;
  globe.hidden = true;
  globe.given = null;
  globe.handed = null;
  return { globe: globe as unknown as InstanceType<typeof AtlasGlobe>, sphere, placed };
}

test("the sphere is never placed at a point of view that is not a place", () => {
  // The one-way half of the corruption. A map given `NaN` can still be told a
  // good centre afterwards; three's camera cannot be told anything at all —
  // every reading off it is `NaN` from that frame on, which is what turns one
  // broken number into two blank panes.
  const { globe, sphere, placed } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  globe.enter({ x: Number.NaN, y: -2224, zoom: 8, rotation: 0 }, { width: 909, height: 648 });
  assert.equal(placed.length, 0, "nothing was placed");
  assert.deepEqual(sphere.at, { lat: 0, lng: 0, altitude: 2.5 }, "and the sphere did not move");
});

test("entering with a camera deeper than the lens still enters, at the lens's depth", () => {
  const { globe, placed } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  globe.enter({ ...HELD, zoom: 99 }, { width: 909, height: 648 });
  assert.equal(placed.length, 1, "a camera that can be repaired is not a refusal");
  const at = placed[0]?.pov as { altitude: number };
  assert.equal(at.altitude, altitudeForZoom(CEILING), "the distance the ceiling reads as");
});

test("the write-back refuses a camera it cannot invert into a place", () => {
  // The reference's own guard, at the reference's own place: both
  // coordinates, checked before the map is told (`globe.js:182`).
  const { globe } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  globe.enter({ x: 4096, y: -2048, zoom: 2, rotation: 0 }, { width: 909, height: 648 });
  const sphere = (globe as unknown as { globe: { at: unknown } }).globe;
  sphere.at = { lat: Number.NaN, lng: Number.NaN, altitude: 0.5 };
  assert.equal(globe.leave(), null, "nothing is handed back rather than a place that is not one");
  assert.equal(globe.cameraOf({ lat: Number.NaN, lng: 0, altitude: 1 }), null);
  assert.equal(globe.cameraOf({ lat: 0, lng: Number.NaN, altitude: 1 }), null);
});

test("the write-back holds what it does hand back to the lens it will be drawn under", () => {
  const { globe } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  globe.enter({ x: 4096, y: -2048, zoom: 2, rotation: 0 }, { width: 909, height: 648 });
  const sphere = (globe as unknown as { globe: { at: unknown } }).globe;
  // Pressed hard against the sphere's nearest distance, which reads deeper
  // than the whole-disc anchor and is exactly where a reader who kept zooming
  // ends up.
  sphere.at = { lat: -7.734375, lng: -120.234375, altitude: 0.0005 };
  const back = globe.leave();
  assert.ok(back);
  assert.ok(back.zoom <= CEILING, `the lens's ceiling, not the pairing's: ${back.zoom}`);
  assert.ok(back.x >= WORLD.minX && back.x <= WORLD.maxX);
  assert.ok(back.y >= WORLD.minY && back.y <= WORLD.maxY);
  assert.ok(sameCamera(back, saneCamera(back, MOLA, GRID)), "and it is already sane");
});

test("an unmoved sphere still hands back the exact camera it was given", () => {
  // The pairing is lossy in both directions on purpose, so a reader who never
  // touched the sphere must get their own numbers back rather than numbers
  // that round-trip to within a float of them. Sanitizing is exact, so this
  // survives it — `globe-returned` in the Mars tour is the recorded reading.
  const { globe } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  const given: Camera = { x: 3605.162673611111, y: -1713.4931195970112, zoom: 4.394578925885768,
    rotation: 0 };
  globe.enter(given, { width: 909, height: 648 });
  assert.ok(sameCamera(globe.leave(), given), "bit for bit");
});

// ---- the reproduction, at the unit level -----------------------------

test("the reader's sequence: 3qd held, MOLA, into the sphere and back", () => {
  // The browser run, step for step, against the Mars fixture:
  //
  //   1. `g`, `3qd` typed, the MOLA lens chosen: the chart stands at zoom 8
  //      over [1360, -2224], the deepest the lens allows.
  //   2. The sphere comes up. The chart's map now reports `[0, 0]`.
  //   3. Something places the sphere at a point of view that is not a place —
  //      a `NaN` out of a degenerate camera, a cell whose middle could not be
  //      computed, a pane with no window. three's camera is `NaN` afterwards
  //      and the planet is blank.
  //   4. The reader flips back down.
  //
  // Before the guard, step 4 handed the chart `[NaN, NaN]` and the second
  // pane went blank with it. Now the sphere hands back nothing, and the chart
  // fronts on the camera it was standing on — which is checked as it comes up.
  const { globe } = globeOver({ lat: 0, lng: 0, altitude: 2.5 });
  globe.enter(HELD, { width: 909, height: 648 });
  const sphere = (globe as unknown as { globe: { at: unknown } }).globe;
  sphere.at = { lat: Number.NaN, lng: Number.NaN, altitude: Number.NaN };

  const handed: Camera | null = globe.leave();
  assert.ok(handed === null, "the sphere refuses to hand a blank pane on");

  const view = stubView(HELD);
  const chart = chartOver(view, { size: [0, 0] });
  flipDown(chart, handed);
  assert.deepEqual(view.centre, [1360, -2224], "the reader is back over the cell they held");
  assert.equal(view.zoom, 8, "at the depth they were reading it at");
  assert.equal(view.fits.length, 0, "and nothing was thrown away to get there");
});

test("and the same sequence under a lens that cannot go that deep", () => {
  // The other half of the class, and the reason finiteness is not enough. The
  // camera the chart is left standing on behind the sphere is a fit into a
  // window of no size, which lands at the deepest level *the lens it was
  // fitted under* has. Front it under a shallower one and it is a camera the
  // pyramid has no tile for: finite, and blank.
  const shallow = context({ ...MOLA, maxZoom: 2 });
  const view = stubView({ ...HELD, zoom: 8 });
  const chart = chartOver(view);
  (chart as unknown as { context: WorldContext }).context = shallow;
  chart.front();
  assert.equal(view.zoom, 4, "two native levels and two of overzoom");
  assert.deepEqual(view.centre, [1360, -2224], "the place is kept; only the depth is given up");
});

test("a session record from another build cannot open a world nowhere", () => {
  // The record is written by whatever build the reader last had, over
  // whatever lens they were on, and it is applied through the same one door.
  // The server refuses one it can see is wrong; this is the half the server
  // cannot see, and it is a clamp on apply rather than a change to the record.
  const view = stubView({ x: 0, y: 0, zoom: 0, rotation: 0 });
  const chart = chartOver(view);
  chart.goTo(20_000, 500, 14, 0);
  assert.ok(view.centre?.[0] === WORLD.maxX);
  assert.ok(view.centre?.[1] === WORLD.maxY, "the top edge, which negation spells `-0`");
  assert.equal(view.zoom, CEILING);
  assert.equal(view.fits.length, 0);
});
