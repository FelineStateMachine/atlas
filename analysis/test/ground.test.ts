// The Ground descriptor and the vocabulary it reads.
//
// These are the clean design's own intent tests (issue #5 §6, "goldens prove
// equivalence, then fresh unit tests document the clean design's intent").
// What they document: a ground is data, the fallback ladder is three branches
// deep, and a malformed declaration is answered with silence rather than a
// plausible number.

import assert from "node:assert/strict";
import test from "node:test";

import { extentArea, surfaceExtent, surfaceWidth } from "../cellsystems/ground.ts";
import { equirectMapping, geoMapping, mercatorMapping, worldSurface } from "../semconv/geometry.ts";
import { mercatorCity, nothing, sphere, square, squareFromBounds, squareNoLens } from "./grounds.ts";

test("the surface extent has three branches, in order", () => {
  assert.deepEqual([...surfaceExtent(square)], [0, -1024, 1024, -0]);
  assert.deepEqual([...surfaceExtent(squareFromBounds)], [128, -512, 640, -256]);
  assert.deepEqual([...surfaceExtent(squareNoLens)], [0, -1024, 1024, -0]);
});

test("a ground with nothing on it is still a ground", () => {
  // No volume and no lens: an empty extent rather than a thrown error, because
  // the app asks this question before it has opened anything.
  assert.deepEqual([...surfaceExtent(nothing)], [0, -0, 0, -0]);
  assert.equal(surfaceWidth(nothing), 0);
});

test("a lens's declared surface wins over the window it fills", () => {
  const both = {
    tileGridSize: 8192,
    lens: {
      surface: { x: 100, y: 200, width: 300, height: 400 },
      bounds: { x: 0, y: 0, width: 8192, height: 8192 },
    },
    world: { attrs: {} },
  };
  assert.deepEqual([...surfaceExtent(both)], [100, -600, 400, -200],
    "the surface is the ground; the bounds are only where the raster reaches");
});

test("width and area read the extent the same way", () => {
  assert.equal(surfaceWidth(square), 1024);
  assert.equal(extentArea(surfaceExtent(square)), 1024 * 1024);
});

test("a world that says nothing is a plane", () => {
  assert.equal(worldSurface(square.world), "plane");
  assert.equal(worldSurface(sphere.world), "sphere");
  assert.equal(worldSurface(null), "plane");
  assert.equal(worldSurface({ attrs: { "atlas.geometry.surface": "" } }), "plane");
});

test("the equirect mapping inverts", () => {
  const mapping = equirectMapping(sphere.world);
  assert.ok(mapping);
  assert.deepEqual([...mapping.toWorld(90, -180)], [0, 0], "the north-west corner is the origin");
  assert.deepEqual([...mapping.toWorld(0, 0)], [4096, 2048], "the origin is the centre");
  const [lat, lng] = mapping.toLatLng(4096, 2048);
  assert.equal(lat, 0);
  assert.equal(lng, 0);
});

test("the mercator mapping inverts, non-linearly in latitude", () => {
  const mapping = mercatorMapping(mercatorCity.world);
  assert.ok(mapping);
  const [x, y] = mapping.toWorld(44.05, -121.3);
  const [lat, lng] = mapping.toLatLng(x, y);
  assert.ok(Math.abs(lat - 44.05) < 1e-9, `latitude round-trips: ${lat}`);
  assert.ok(Math.abs(lng - -121.3) < 1e-9, `longitude round-trips: ${lng}`);
  // Linear in longitude, not in latitude: the halfway parallel is not halfway
  // down the picture.
  const [middleX] = mapping.toWorld(44.05, -121.3);
  assert.ok(Math.abs(middleX - 2048) < 1e-9, "longitude is linear");
  const [, middleY] = mapping.toWorld((44.15 + 43.99) / 2, -121.3);
  assert.notEqual(middleY, 2048);
});

test("geoMapping prefers equirect, and falls through to mercator", () => {
  assert.ok(geoMapping(sphere.world));
  assert.ok(geoMapping(mercatorCity.world));
  assert.equal(geoMapping(square.world), null, "a plane declares no flattening");
});

test("a malformed declaration is answered with silence, not a plausible number", () => {
  const cases: Record<string, string>[] = [
    { "atlas.geometry.projection": "equirect" },
    { "atlas.geometry.projection": "equirect", "atlas.geometry.equirect.px": "0,0,8192" },
    {
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,180",
    },
    {
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,x,-90",
    },
    {
      // A window with no height would invert by dividing by zero.
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,0",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
    {
      // Degenerate degrees: north equals south.
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,0,180,0",
    },
  ];
  for (const attrs of cases) {
    assert.equal(geoMapping({ attrs }), null, `${JSON.stringify(attrs)} is not a mapping`);
  }
});
