// The geohash cell system, in its two modes.
//
// SYNTHETIC. On a ground that is only a picture — a game map, a sphere's
// raster — geohash is recursive halving in world pixels: thirty-two children
// to a cell, addresses that refine by appending a character. It divides any
// ground at all, because it never asks what the picture is of. On a PLANE it
// halves the world square itself (`geohashExtent` below), so the grid covers
// the full map and stays put across lens switches; on a sphere it halves the
// declared surface, exactly as it always has.
//
// REAL. A plane that declares an earth Mercator window and says its body is
// earth (bend-or) is a picture OF somewhere, and its geohashes are the real
// ones: the classic WGS84 arithmetic — longitude first, five bits to a
// character, the same alphabet, run in degrees — with the cells put on the
// picture through the declared flattening. The window is city-sized, so the
// telescope's three layers start at the depth whose cells first fit the
// window (`baseDepth`) rather than at one character.
//
// The halving itself is arithmetic the vectors gate compares byte for byte,
// so nothing about how it is written here may move a float: the mask walk,
// the axis alternation and the order of the two divisions are the recorded
// behaviour, in both modes.

import type { CellID, CellLabel, GroundedCellSystem, LocatedCell, Pole, Ring } from "./contract.ts";
import type { CellSystem } from "./contract.ts";
import type { Coordinate, Extent, Ground } from "./ground.ts";
import { surfaceExtent } from "./ground.ts";
import { palette } from "./tokens.ts";
import { mercatorMapping, mercatorWindow, worldSurface } from "../semconv/geometry.ts";
import type { GeoMapping, World } from "../semconv/geometry.ts";
import { KEY_GEOMETRY_BODY } from "../semconv/keys.ts";

/**
 * The alphabet, in its canonical order — no `a`, `i`, `l` or `o`, so a hash
 * read aloud or typed from a screenshot cannot be misheard. It is the real
 * geohash alphabet in the real order, which is what lets the same characters
 * name real ground when the world is anchored to it.
 */
export const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz";

/**
 * The synthetic telescope's floor. Three characters is fifteen halvings — a
 * cell a few world pixels across on an 8192 square — which is depth enough
 * for a couple thousand pins and shallow enough that every level stays a
 * place. On an earth-anchored ground the floor is deeper and the ground's
 * own: ask {@link geohashDepth}.
 */
export const geohashMaxDepth = 3;

/**
 * The telescope is three layers of precision on every ground: the base
 * depth's cells, their children, and theirs. Level counts layers — the root
 * is 0 whether its children are one character (synthetic) or four (bend-or).
 */
const geohashLayers = 3;

/** The five bits of one character, most significant first. */
const masks = [16, 8, 4, 2, 1] as const;

/**
 * The ground geohash divides, which since Part 1 of the plane ruling is NOT
 * always `surfaceExtent`: a game map's grid must cover the full map and stay
 * put across lens switches, and two lenses of one world may declare
 * different surfaces. So on a PLANE with no earth anchoring, geohash divides
 * the whole world square `[0, -size, size, 0]` whenever the volume declares
 * one, and only a ground with no volume at all falls back to the surface
 * ladder. Spheres keep the ladder — their surface is the body's own picture —
 * and every other consumer of `surfaceExtent` (S2, the ground questions)
 * keeps it too.
 */
export function geohashExtent(ground: Ground): Extent {
  if (worldSurface(ground.world) !== "plane") return surfaceExtent(ground);
  const size = ground.tileGridSize;
  if (size === null) return surfaceExtent(ground);
  return [0, -size, size, 0];
}

// ---------------------------------------------------------------------------
// The synthetic halving
// ---------------------------------------------------------------------------

/**
 * The recursive halving itself: alternate axes, five bits to a character.
 *
 * A character outside the alphabet is skipped without consuming a halving —
 * the axis does not alternate past it — which is what the field's own
 * normalization makes unreachable and what this keeps true anyway.
 */
function bbox(ground: Ground, hash: CellID): Extent {
  const extent: [number, number, number, number] = [...geohashExtent(ground)];
  let splitX = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of masks) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (value & mask) extent[0] = middle;
        else extent[2] = middle;
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (value & mask) extent[1] = middle;
        else extent[3] = middle;
      }
      splitX = !splitX;
    }
  }
  return extent;
}

/**
 * The halving run backward: the same divisions, choosing at each one the side
 * the point is on. Computed when asked rather than stored, because a hash
 * stored beside a location could only ever be right for one ground.
 *
 * A point off the surface names nothing.
 */
function cellAt(ground: Ground, coordinate: Coordinate, depth: number): CellID {
  const extent: [number, number, number, number] = [...geohashExtent(ground)];
  const [x, y] = coordinate;
  if (x < extent[0] || x > extent[2] || y < extent[1] || y > extent[3]) return "";
  let splitX = true;
  let hash = "";
  for (let level = 0; level < depth; level++) {
    let value = 0;
    for (const mask of masks) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (x >= middle) {
          value |= mask;
          extent[0] = middle;
        } else {
          extent[2] = middle;
        }
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (y >= middle) {
          value |= mask;
          extent[1] = middle;
        } else {
          extent[3] = middle;
        }
      }
      splitX = !splitX;
    }
    // `value` is five bits, so the alphabet always has the character; the
    // fallback is what `noUncheckedIndexedAccess` costs and it never fires.
    hash += geohashAlphabet[value] ?? "";
  }
  return hash;
}

/**
 * The ordinal of a hash's last character. The root — and any hash ending in a
 * character the alphabet does not carry — is ordinal 0.
 */
function ordinal(id: CellID): number {
  return Math.max(0, geohashAlphabet.indexOf(id.slice(-1)));
}

/**
 * One remembered box. Callers sweep thousands of pins against one cell at a
 * time, so the last cell's extent is kept rather than re-halved per pin. It is
 * a cache over a pure function and nothing observes it: the key carries the
 * ground as well as the id, because a ground switch moves every box.
 */
let memo: { key: string; extent: Extent } | null = null;

function cachedBox(ground: Ground, id: CellID): Extent {
  const key = `${geohashExtent(ground).join(",")}|${id}`;
  if (memo?.key !== key) memo = { key, extent: bbox(ground, id) };
  return memo.extent;
}

// ---------------------------------------------------------------------------
// The real mode: classic WGS84 geohash through the declared window
// ---------------------------------------------------------------------------

/** One degree rectangle: a geohash cell's own ground. */
interface DegreeBox {
  readonly west: number;
  readonly south: number;
  readonly east: number;
  readonly north: number;
}

/** An earth-anchored ground, read once per world. */
interface RealGround {
  readonly mapping: GeoMapping;
  /** The declared window's ground, west/north/east/south degrees. */
  readonly window: DegreeBox;
  /** The smallest depth whose cells fit the window on either axis. */
  readonly baseDepth: number;
  /** The base-depth cells that intersect the window, in reading order. */
  roots?: CellID[];
}

/**
 * Real mode's condition, whole: a PLANE (a sphere's geohash is the synthetic
 * one over its picture), an invertible Mercator window, and a world that says
 * the body pictured is earth — mars is a sphere and carries no earth body, so
 * nothing here so much as looks at it.
 */
const realGrounds = new WeakMap<World, RealGround | null>();

function realGroundOf(ground: Ground): RealGround | null {
  const world = ground.world;
  const held = realGrounds.get(world);
  if (held !== undefined) return held;
  let fresh: RealGround | null = null;
  if (worldSurface(world) === "plane" && world.attrs[KEY_GEOMETRY_BODY] === "earth") {
    const mapping = mercatorMapping(world);
    const declared = mercatorWindow(world);
    if (mapping && declared) {
      const [west, north, east, south] = declared.deg;
      const window: DegreeBox = { west, south, east, north };
      fresh = { mapping, window, baseDepth: baseDepthOf(window) };
    }
  }
  realGrounds.set(world, fresh);
  return fresh;
}

/**
 * A depth's cell spans, exact. Five bits alternate starting with longitude,
 * so `d` characters split longitude `ceil(5d/2)` times and latitude
 * `floor(5d/2)` — every span is 360° or 180° over a power of two, which a
 * double carries without rounding.
 */
function cellSpans(depth: number): { lng: number; lat: number } {
  const bits = 5 * depth;
  return {
    lng: 360 / 2 ** Math.ceil(bits / 2),
    lat: 180 / 2 ** Math.floor(bits / 2),
  };
}

/**
 * The smallest depth ≥ 1 at which a cell's longitude span fits the window's
 * OR its latitude span does, capped at 9. Depth 1 is a hemisphere-sized cell
 * and depth 3 would swallow a city whole; for bend-or's window
 * (0.32909° × 0.23650°) this is 4, because the depth-4 latitude span
 * (0.17578125°) fits where the longitude span (0.3515625°) just misses.
 */
function baseDepthOf(window: DegreeBox): number {
  const lngSpan = window.east - window.west;
  const latSpan = window.north - window.south;
  for (let depth = 1; depth < 9; depth++) {
    const spans = cellSpans(depth);
    if (spans.lng <= lngSpan || spans.lat <= latSpan) return depth;
  }
  return 9;
}

/** The classic mask walk, forward: a hash to its degree rectangle. */
function degreeBox(hash: CellID): DegreeBox {
  const lng: [number, number] = [-180, 180];
  const lat: [number, number] = [-90, 90];
  let splitLng = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of masks) {
      if (splitLng) {
        const middle = (lng[0] + lng[1]) / 2;
        if (value & mask) lng[0] = middle;
        else lng[1] = middle;
      } else {
        const middle = (lat[0] + lat[1]) / 2;
        if (value & mask) lat[0] = middle;
        else lat[1] = middle;
      }
      splitLng = !splitLng;
    }
  }
  return { west: lng[0], south: lat[0], east: lng[1], north: lat[1] };
}

/** The classic mask walk, backward: true degrees to a hash. */
function encodeLatLng(lat: number, lng: number, depth: number): CellID {
  const lngRange: [number, number] = [-180, 180];
  const latRange: [number, number] = [-90, 90];
  let splitLng = true;
  let hash = "";
  for (let level = 0; level < depth; level++) {
    let value = 0;
    for (const mask of masks) {
      if (splitLng) {
        const middle = (lngRange[0] + lngRange[1]) / 2;
        if (lng >= middle) {
          value |= mask;
          lngRange[0] = middle;
        } else {
          lngRange[1] = middle;
        }
      } else {
        const middle = (latRange[0] + latRange[1]) / 2;
        if (lat >= middle) {
          value |= mask;
          latRange[0] = middle;
        } else {
          latRange[1] = middle;
        }
      }
      splitLng = !splitLng;
    }
    hash += geohashAlphabet[value] ?? "";
  }
  return hash;
}

/**
 * The real address under a point, at any depth. A point outside the declared
 * window still HAS a real geohash — the ground is real earth and every point
 * of the picture's plane is somewhere on it — so nothing here refuses.
 */
function realCellAt(real: RealGround, coordinate: Coordinate, depth: number): CellID {
  const [lat, lng] = real.mapping.toLatLng(coordinate[0], -coordinate[1]);
  return encodeLatLng(lat, lng, depth);
}

/**
 * A real cell's rectangle in world pixels, through the declared flattening.
 * Mercator keeps latitude lines horizontal, so the cell stays a world-pixel
 * rectangle and its four corners are two: the south-west and the north-east.
 * `toWorld` answers y-down pixels; the lane speaks y negative-down, and the
 * sign flips here, at the one call where the conventions meet.
 */
function realExtent(real: RealGround, box: DegreeBox): Extent {
  const [minimumX, bottom] = real.mapping.toWorld(box.south, box.west);
  const [maximumX, top] = real.mapping.toWorld(box.north, box.east);
  return [minimumX, -bottom, maximumX, -top];
}

/**
 * Whether a cell's ground and the window share any real area. Strict on
 * every edge: a cell that only touches the window's boundary pictures none
 * of it, and enumerating it would draw a rectangle with nothing inside.
 */
function intersectsWindow(box: DegreeBox, window: DegreeBox): boolean {
  return box.west < window.east && box.east > window.west &&
    box.south < window.north && box.north > window.south;
}

/**
 * The base-depth cells that intersect the window, in reading order: north to
 * south, west to east — a 2×3-ish handful for a city window. These are the
 * root's children and the whole of the root plan.
 */
function rootsOf(real: RealGround): CellID[] {
  if (real.roots) return real.roots;
  const spans = cellSpans(real.baseDepth);
  const window = real.window;
  const firstColumn = Math.floor((window.west + 180) / spans.lng);
  const lastColumn = Math.floor((window.east + 180) / spans.lng);
  const firstRow = Math.floor((window.south + 90) / spans.lat);
  const lastRow = Math.floor((window.north + 90) / spans.lat);
  const roots: CellID[] = [];
  for (let row = lastRow; row >= firstRow; row--) {
    for (let column = firstColumn; column <= lastColumn; column++) {
      const box: DegreeBox = {
        west: -180 + column * spans.lng,
        east: -180 + (column + 1) * spans.lng,
        south: -90 + row * spans.lat,
        north: -90 + (row + 1) * spans.lat,
      };
      if (!intersectsWindow(box, window)) continue;
      // The cell's centre is exactly inside it, so the walk back lands on it.
      roots.push(encodeLatLng(
        (box.south + box.north) / 2,
        (box.west + box.east) / 2,
        real.baseDepth,
      ));
    }
  }
  real.roots = roots;
  return roots;
}

/** Geohash on real earth, bound to one anchored ground. */
function realGrounded(ground: Ground, real: RealGround): GroundedCellSystem {
  const base = real.baseDepth;
  // The PLAN only enumerates cells that intersect the window — the grid is
  // the window's even though the addresses are the planet's — and the plan
  // asks `children`, so the filter lives here. Order is the alphabet's, which
  // is the synthetic child order and the palette order.
  const children = (id: CellID): CellID[] => {
    if (id === "") return [...rootsOf(real)];
    return [...geohashAlphabet]
      .map((character) => id + character)
      .filter((child) => intersectsWindow(degreeBox(child), real.window));
  };
  // The sibling ordinal is the position among the WINDOW'S siblings, the way
  // S2's is a child position: with absent siblings filtered out, the last
  // character's own ordinal would leave holes in the palette walk.
  const childIndex = (id: CellID): number => {
    if (id === "") return 0;
    return Math.max(0, children(id.length <= base ? "" : id.slice(0, -1)).indexOf(id));
  };
  const box = (id: CellID): Extent => {
    if (id !== "") return realExtent(real, degreeBox(id));
    // The root is the whole of what the plan can show: the union of its own
    // children's rectangles, overhang and all.
    let union: [number, number, number, number] | null = null;
    for (const root of rootsOf(real)) {
      const extent = realExtent(real, degreeBox(root));
      union = union === null
        ? [...extent]
        : [
          Math.min(union[0], extent[0]),
          Math.min(union[1], extent[1]),
          Math.max(union[2], extent[2]),
          Math.max(union[3], extent[3]),
        ];
    }
    return union ?? surfaceExtent(ground);
  };
  return {
    // Level counts layers, not characters: the root's children are base-depth
    // cells, and the telescope is three layers on every ground.
    level(id: CellID): number {
      return id === "" ? 0 : id.length - base + 1;
    },

    parent(id: CellID): CellID {
      return id.length <= base ? "" : id.slice(0, -1);
    },

    children,
    childIndex,

    contains(id: CellID, at: Coordinate): boolean {
      if (id === "") return true;
      const cell = degreeBox(id);
      const [lat, lng] = real.mapping.toLatLng(at[0], -at[1]);
      return lng >= cell.west && lng <= cell.east && lat >= cell.south && lat <= cell.north;
    },

    descendTarget(id: CellID, at: Coordinate): CellID {
      return realCellAt(real, at, id === "" ? base : id.length + 1);
    },

    bbox: box,

    center(id: CellID): Coordinate {
      const extent = box(id);
      return [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2];
    },

    ring(id: CellID): Ring {
      const [minimumX, minimumY, maximumX, maximumY] = box(id);
      return [
        [minimumX, minimumY],
        [minimumX, maximumY],
        [maximumX, maximumY],
        [maximumX, minimumY],
        [minimumX, minimumY],
      ];
    },

    // A city window pictures no pole, and a Mercator window cannot.
    poleContained(): Pole | null {
      return null;
    },

    label(id: CellID): CellLabel {
      return { context: id.slice(0, -1), principal: id.slice(-1) };
    },

    colorKey(id: CellID): number {
      return childIndex(id) % palette.length;
    },

    normalizeInput(text: string): string {
      return [...text.toLocaleLowerCase()]
        .filter((character) => geohashAlphabet.includes(character))
        .slice(0, base + 2)
        .join("");
    },

    // Whole or nothing: a real address is at least base-depth characters —
    // anything shorter names a cell the window's grid never shows, so it is a
    // draft the field holds while the session stays put. The root is a place.
    parseInput(text: string): CellID | null {
      if (text === "") return "";
      return text.length < base ? null : text;
    },

    locate(at: Coordinate): LocatedCell | null {
      return { label: "Geohash", value: realCellAt(real, at, base + 2) };
    },
  };
}

// ---------------------------------------------------------------------------
// The ground-aware contract
// ---------------------------------------------------------------------------

/**
 * The depth the base layer starts at: 1 on a synthetic ground, the window's
 * own {@link baseDepthOf} on an earth-anchored one (4 for bend-or).
 */
export function geohashBaseDepth(ground: Ground): number {
  return realGroundOf(ground)?.baseDepth ?? 1;
}

/**
 * The hash-length floor of this ground's telescope — the depth `locate`
 * answers at and the length the navigator's field accepts. Always the base
 * depth plus two: three layers of precision, wherever the layers start
 * (3 on a synthetic ground, 6 for bend-or).
 */
export function geohashDepth(ground: Ground): number {
  return geohashBaseDepth(ground) + 2;
}

/**
 * The pin card's fixed-depth address, independent of where the grid stands.
 * The one caller that names a depth of its own.
 */
export function geohashCellAt(
  ground: Ground,
  coordinate: Coordinate,
  depth: number = geohashDepth(ground),
): CellID {
  const real = realGroundOf(ground);
  if (real) return realCellAt(real, coordinate, depth);
  return cellAt(ground, coordinate, depth);
}

/** Geohash, bound to one synthetic ground. */
function grounded(ground: Ground): GroundedCellSystem {
  const real = realGroundOf(ground);
  if (real) return realGrounded(ground, real);
  return {
    level(id: CellID): number {
      return id.length;
    },

    parent(id: CellID): CellID {
      return id.slice(0, -1);
    },

    children(id: CellID): CellID[] {
      return [...geohashAlphabet].map((character) => id + character);
    },

    childIndex(id: CellID): number {
      return ordinal(id);
    },

    contains(id: CellID, at: Coordinate): boolean {
      const extent = cachedBox(ground, id);
      const [x, y] = at;
      return x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3];
    },

    descendTarget(id: CellID, at: Coordinate): CellID {
      return cellAt(ground, at, id.length + 1);
    },

    bbox(id: CellID): Extent {
      return bbox(ground, id);
    },

    center(id: CellID): Coordinate {
      const extent = bbox(ground, id);
      return [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2];
    },

    ring(id: CellID): Ring {
      const [minimumX, minimumY, maximumX, maximumY] = bbox(ground, id);
      return [
        [minimumX, minimumY],
        [minimumX, maximumY],
        [maximumX, maximumY],
        [maximumX, minimumY],
        [minimumX, minimumY],
      ];
    },

    // A halved rectangle on a flat picture circles nothing.
    poleContained(): Pole | null {
      return null;
    },

    label(id: CellID): CellLabel {
      return { context: id.slice(0, -1), principal: id.slice(-1) };
    },

    colorKey(id: CellID): number {
      return ordinal(id) % palette.length;
    },

    normalizeInput(text: string): string {
      return [...text.toLocaleLowerCase()]
        .filter((character) => geohashAlphabet.includes(character))
        .slice(0, geohashMaxDepth)
        .join("");
    },

    // Every normalized spelling is a place — the empty string is the root —
    // so parsing never refuses.
    parseInput(text: string): CellID | null {
      return text;
    },

    locate(at: Coordinate): LocatedCell | null {
      const value = cellAt(ground, at, geohashMaxDepth);
      return value ? { label: "Geohash", value } : null;
    },
  };
}

export const geohashSystem: CellSystem = {
  slug: "geohash",
  name: "Geohash",
  short: "G",

  // Geohash divides anything: it never asks what the picture is of.
  appliesTo(): boolean {
    return true;
  },

  // Three layers of precision on every ground; only where they START moves.
  maxLevel(): number {
    return geohashLayers;
  },

  // The field takes a whole address at the floor: three characters on a
  // synthetic ground, six on bend-or. internal/app/grid.go answers the same
  // ground-aware number, so the navigator's maxlength follows.
  inputLength(ground: Ground): number {
    return geohashDepth(ground);
  },

  on: grounded,
};
