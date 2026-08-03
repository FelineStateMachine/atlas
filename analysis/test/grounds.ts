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
