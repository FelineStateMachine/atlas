// Invented volumes, built through the lane's own types.
//
// The corpus (`testdata/corpus/bundles`) keeps two real volumes: bend-or, a
// plane city, and mars, a sphere. Two shapes of volume that used to be
// captured commercially are not in it any more, and the tests that proved
// them now prove them here instead, on volumes invented for the purpose:
//
//   A SPLIT SHEET. One world pictured as horizontal bands, several lenses
//   naming the same pyramid, each windowing its own band with `bounds` — and
//   one of them declaring a `surface` smaller than its window, because the
//   sheet drew a title in the margin. Every number is small enough to check
//   by hand, which is the whole point of inventing it.
//
//   A GAME PLANE. A city world whose district owns two separate pieces of
//   ground (one with a hole in it) and whose canal is recorded as three
//   runs — the multipart shapes — plus a lens bounded well inside the world
//   square with margin on every side, and a hand-drawn lens that declares
//   `interpolate: false`, the nearest-neighbour case no corpus lens carries.
//
// These are factories over `data/payload.ts` types and nothing else: a test
// that consumes one is exercising the same reader a real payload meets.

import type { Collection, Lens, TileGrid, WorldPayload } from "../data/payload.ts";

/** The world square every published bundle cuts: 8192 pixels, 256 to a tile. */
export const SQUARE: TileGrid = { sourceZoom: 13, firstTile: 4064, tileSize: 256, size: 8192 };

/** One invented volume: its grid, and its worlds by slug. */
export interface SyntheticVolume {
  readonly slug: string;
  readonly tileGrid: TileGrid;
  readonly worlds: ReadonlyMap<string, WorldPayload>;
}

const sheetLevels = (count: number) => Array.from({ length: count }, () => "webp");

/**
 * The split sheet: three lenses, one pyramid, one band of the square each.
 *
 * The bands: Sky owns rows 0–2048, Land 2048–4096, Depths 4096–6144; the
 * bottom quarter of the square was never captured by anyone. Depths alone
 * goes a level past `fullZoom`, and its level-3 coverage admits exactly three
 * tiles — (0,4), (4,4) and (7,5) of the 8×2 tile band its window touches,
 * which is bits 0, 4 and 15 of a row-major bitset, low bit first: bytes
 * 0b00010001 and 0b10000000, base64 "EYA=".
 */
export function splitSheet(): SyntheticVolume {
  const shared = {
    tiles: "aerie-sheet",
    minZoom: 0,
    maxZoom: 2,
    fullZoom: 2,
    sourceZoom: 13,
    formats: sheetLevels(3),
    interpolate: true,
  };
  const lenses: Lens[] = [
    {
      ...shared,
      name: "Sky",
      bounds: { x: 0, y: 0, width: 8192, height: 2048 },
      // The window took in a title drawn beside the map; the ground is less.
      surface: { x: 512, y: 256, width: 7168, height: 1536 },
    },
    {
      ...shared,
      name: "Land",
      bounds: { x: 0, y: 2048, width: 8192, height: 2048 },
    },
    {
      ...shared,
      name: "Depths",
      maxZoom: 3,
      formats: sheetLevels(4),
      bounds: { x: 0, y: 4096, width: 8192, height: 2048 },
      coverage: { "3": { x: 0, y: 4, w: 8, h: 2, bits: "EYA=" } },
    },
  ];
  const world: WorldPayload = {
    lenses,
    collections: [],
    attrs: { "atlas.geometry.surface": "plane" },
  };
  return { slug: "aerie", tileGrid: SQUARE, worlds: new Map([["aloft", world]]) };
}

/** A closed ring in the volume's own `[lng, lat]` space, as payloads spell it. */
function ring(lng: number, lat: number, span: number): number[][] {
  return [
    [lng, lat], [lng + span, lat], [lng + span, lat - span], [lng, lat - span], [lng, lat],
  ];
}

/**
 * The game plane: one city world with the two multipart shapes that matter.
 *
 * "Twin Wards" is a MultiPolygon of two parts — the first carries a hole of
 * its own — so a builder that flattens rings onto one polygon turns the
 * second ward into a hole punched out of the first. "Old Canal" is a
 * MultiLineString of three runs, so a builder that keeps only the first run
 * silently drops two thirds of the water.
 */
export function gamePlane(): SyntheticVolume {
  const districts: Collection = {
    id: 1,
    title: "Districts",
    kind: "area",
    visible: true,
    features: [
      {
        id: 101,
        title: "Twin Wards",
        geometry: [{
          type: "MultiPolygon",
          coordinates: [
            [ring(-3, 44, 0.4), ring(-2.9, 43.9, 0.2)],
            [ring(-2, 44, 0.4)],
          ],
        }],
      },
      {
        id: 102,
        title: "Single Ward",
        geometry: [{ type: "Polygon", coordinates: [ring(-1, 44, 0.3)] }],
      },
    ],
  };
  const canals: Collection = {
    id: 2,
    title: "Canals",
    kind: "path",
    visible: true,
    features: [{
      id: 201,
      title: "Old Canal",
      geometry: [{
        type: "MultiLineString",
        coordinates: [
          [[-3, 44], [-2.8, 43.9], [-2.6, 44]],
          [[-2.5, 44], [-2.4, 43.95]],
          [[-2.3, 44], [-2.2, 43.9], [-2.1, 44], [-2, 43.95]],
        ],
      }],
    }],
  };
  const cityLevels = (count: number) => Array.from({ length: count }, () => "jpg");
  const lenses: Lens[] = [
    {
      // A pyramid filling a window well inside the square: real margin on
      // every side for an unclipped layer to draw into.
      name: "Streets",
      tiles: "old-quarter",
      minZoom: 0,
      maxZoom: 5,
      fullZoom: 5,
      sourceZoom: 13,
      formats: cityLevels(6),
      bounds: { x: 2016, y: 2020, width: 4159, height: 4378 },
      interpolate: true,
    },
    {
      // A surface and no bounds: the ground it pictures is declared, the
      // window is not, and the camera must fit the whole square. Pixel art,
      // so it resamples nearest-neighbour.
      name: "Hand-Drawn",
      tiles: "old-quarter-drawn",
      minZoom: 0,
      maxZoom: 4,
      fullZoom: 4,
      sourceZoom: 13,
      formats: cityLevels(5),
      surface: { x: 1563, y: 2000, width: 5066, height: 4191 },
      interpolate: false,
    },
  ];
  const world: WorldPayload = {
    lenses,
    collections: [districts, canals],
    attrs: { "atlas.geometry.surface": "plane" },
  };
  return { slug: "neon-harbor", tileGrid: SQUARE, worlds: new Map([["old-quarter", world]]) };
}
