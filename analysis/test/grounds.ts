// The grounds the tests stand on.
//
// Written here rather than read from `analysis/testdata/cells/grounds.json`
// on purpose: the shared vectors judge this lane (vectors.test.ts) and must
// stay something the property tests cannot quietly become. These are the
// clean design's own fixtures, and they say what a Ground is by being one.

import type { Ground } from "../cellsystems/ground.ts";

/** A 1024 world declaring the whole of itself as its lens surface. */
export const square: Ground = {
  tileGridSize: 1024,
  lens: { surface: { x: 0, y: 0, width: 1024, height: 1024 }, bounds: null },
  world: { attrs: {} },
};

/** The same world with a lens that declares only a raster window. */
export const squareFromBounds: Ground = {
  tileGridSize: 1024,
  lens: { surface: null, bounds: { x: 128, y: 256, width: 512, height: 256 } },
  world: { attrs: {} },
};

/** The same world with no lens open at all: the last fallback. */
export const squareNoLens: Ground = {
  tileGridSize: 1024,
  lens: null,
  world: { attrs: {} },
};

/** No volume, no lens: an empty ground, which surfaceExtent still answers for. */
export const nothing: Ground = {
  tileGridSize: null,
  lens: null,
  world: { attrs: {} },
};

/** The top half of an 8192 square declared as a whole equirectangular sphere. */
export const sphere: Ground = {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 8192, height: 4096 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
  },
};

/** A Web-Mercator city window on a sphere: the other flattening, exercised. */
export const mercatorCity: Ground = {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 4096, height: 4096 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "mercator",
      "atlas.geometry.mercator.px": "0,0,4096,4096",
      "atlas.geometry.mercator.deg": "-121.4,44.15,-121.2,43.99",
    },
  },
};

/** A sphere that declares no flattening: S2 must refuse it. */
export const sphereWithoutMapping: Ground = {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 8192, height: 4096 }, bounds: null },
  world: { attrs: { "atlas.geometry.surface": "sphere" } },
};

/**
 * An earth-anchored plane: a city map that declares an earth Mercator window,
 * so its geohash is REAL — genuine WGS84 addresses of the pictured ground.
 * Invented numbers, none of them the corpus's: a 4096 square over a
 * 0.8° × 0.5° window, whose base depth is 4 because the depth-4 spans
 * (0.3515625° × 0.17578125°) both fit and the depth-3 spans (1.40625° both
 * ways) fit neither. The window's north edge, 45°, lies exactly on a depth-4
 * cell boundary (768 · 0.17578125° above −90°), so the edge-touching row is
 * deliberately exercised: a cell that only touches pictures nothing and is
 * not enumerated. Districted, so the carry has a second system to cross to.
 */
export const anchored: Ground = {
  tileGridSize: 4096,
  lens: { surface: { x: 0, y: 0, width: 4096, height: 4096 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "plane",
      "atlas.geometry.body": "earth",
      "atlas.geometry.mercator.px": "0,0,4096,4096",
      "atlas.geometry.mercator.deg": "-122,45,-121.2,44.5",
      "atlas.district.grid": "3x3",
    },
  },
};

/**
 * The corpus city's own declaration, transcribed from
 * `testdata/corpus/bundles/bend-or/worlds/2026-08-02.payload.json` — the
 * ground the shared bend-or vectors are read on, spelled here so the intent
 * tests can hold the derived depths (base 4, floor 6) to the real window.
 */
export const bendOr: Ground = {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 8192, height: 8192 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "plane",
      "atlas.geometry.body": "earth",
      "atlas.geometry.mercator.px": "0,0,8192,8192",
      "atlas.geometry.mercator.deg":
        "-121.48204667177453,44.17572950664809,-121.1529533282255,43.93922950850393",
    },
  },
};
