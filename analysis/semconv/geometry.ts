// The geometry half of the published attribute vocabulary, read by the
// analysis lane.
//
// Issue #5 §3.2 lets this lane depend on "its own math and the published
// attribute vocabulary" and nothing else. `spec/registry.yaml` is that
// vocabulary's one machine-readable source; `./keys.ts` beside this file is
// generated from it, and so is `format/semconv`. This module is the lane's
// *reader* for the four `atlas.geometry.*` keys a cell system can be asked
// about: it never writes a key, never validates a bundle, and knows nothing
// about how a world reached it.
//
//   atlas.geometry.surface       "sphere" | "plane" (absent means plane)
//   atlas.geometry.projection    "equirect" | "mercator"
//   atlas.geometry.<p>.px        x,y,w,h   — the raster window, world pixels
//   atlas.geometry.<p>.deg       west,north,east,south — what it pictures
//
// Coordinates crossing this boundary are **y-down world pixels**: the space
// a packed location lives in, y increasing downward from the world square's
// top-left. The cell systems work in OL world coordinates (y negative-down)
// and flip the sign at the call, which is the one place the two conventions
// meet.

import {
  KEY_GEOMETRY_EQUIRECT_DEG,
  KEY_GEOMETRY_EQUIRECT_PX,
  KEY_GEOMETRY_MERCATOR_DEG,
  KEY_GEOMETRY_MERCATOR_PX,
  KEY_GEOMETRY_PROJECTION,
  KEY_GEOMETRY_SURFACE,
} from "./keys.ts";

/** A world's attribute bag: `atlas.*` keys to their string values. */
export type WorldAttrs = Readonly<Record<string, string>>;

/**
 * The subject of a cell system: one ground you can stand on, carrying the
 * attributes that say what its picture is of. It is the world half of a
 * {@link import("../cellsystems/ground.ts").Ground}.
 */
export interface World {
  readonly attrs: WorldAttrs;
}

/**
 * A flattening, both ways round. `toLatLng` reads world pixels as true
 * degrees; `toWorld` puts a degree pair back on the picture. The pair is what
 * lets a geodesic system address a raster.
 */
export interface GeoMapping {
  toLatLng(worldX: number, worldY: number): readonly [number, number];
  toWorld(lat: number, lng: number): readonly [number, number];
}

/** The flattenings this reader knows how to invert. */
type Projection = "equirect" | "mercator";

/** Each projection's declared window, by the generated key that carries it. */
const WINDOW: Readonly<Record<Projection, { px: string; deg: string }>> = {
  equirect: { px: KEY_GEOMETRY_EQUIRECT_PX, deg: KEY_GEOMETRY_EQUIRECT_DEG },
  mercator: { px: KEY_GEOMETRY_MERCATOR_PX, deg: KEY_GEOMETRY_MERCATOR_DEG },
};

/**
 * What the world's raster pictures. A world that says nothing is a plane,
 * which every world was until the planets arrived.
 */
export function worldSurface(world: World | null | undefined): string {
  const declared = world?.attrs?.[KEY_GEOMETRY_SURFACE];
  return declared ? declared : "plane";
}

/**
 * Whichever flattening the world declares — equirectangular or Web-Mercator —
 * answered as the same two functions either way. Null when nothing is
 * declared or the declaration is malformed, which a validated bundle never is.
 */
export function geoMapping(world: World | null | undefined): GeoMapping | null {
  return equirectMapping(world) ?? mercatorMapping(world);
}

/**
 * The flattening a spherical world declares: the raster window it fills in
 * world pixels and the ground that window pictures in degrees. Only this
 * projection drapes a sphere without resampling, which is why the globe asks
 * for it by name.
 */
export function equirectMapping(world: World | null | undefined): GeoMapping | null {
  const window = declaredWindow(world, "equirect");
  if (!window) return null;
  const [x, y, w, h] = window.px;
  const [west, north, east, south] = window.deg;
  return {
    toLatLng(worldX, worldY) {
      return [
        north - ((worldY - y) / h) * (north - south),
        west + ((worldX - x) / w) * (east - west),
      ];
    },
    toWorld(lat, lng) {
      return [
        x + ((lng - west) / (east - west)) * w,
        y + ((north - lat) / (north - south)) * h,
      ];
    },
  };
}

/**
 * The Web-Mercator reader: x linear in longitude, y linear in the projected
 * latitude asinh(tan φ), which is what a real-world tile window actually is.
 */
export function mercatorMapping(world: World | null | undefined): GeoMapping | null {
  const window = declaredWindow(world, "mercator");
  if (!window) return null;
  const [x, y, w, h] = window.px;
  const [west, north, east, south] = window.deg;
  const project = (lat: number) => Math.asinh(Math.tan((lat * Math.PI) / 180));
  const unproject = (value: number) => (Math.atan(Math.sinh(value)) * 180) / Math.PI;
  const top = project(north);
  const bottom = project(south);
  return {
    toLatLng(worldX, worldY) {
      return [
        unproject(top - ((worldY - y) / h) * (top - bottom)),
        west + ((worldX - x) / w) * (east - west),
      ];
    },
    toWorld(lat, lng) {
      return [
        x + ((lng - west) / (east - west)) * w,
        y + ((top - project(lat)) / (top - bottom)) * h,
      ];
    },
  };
}

type Quad = readonly [number, number, number, number];

/** One declared window: the raster it fills and the ground it pictures. */
export interface ProjectedWindow {
  /** `x, y, w, h` — world pixels, y down. */
  readonly px: Quad;
  /** `west, north, east, south` — degrees at the window's edges. */
  readonly deg: Quad;
}

/**
 * The Mercator window itself, degrees and all, for the one consumer that
 * needs more than the two functions: an earth-anchored plane's geohash sizes
 * its grid from the window's degree spans (analysis/cellsystems/geohash.ts).
 */
export function mercatorWindow(world: World | null | undefined): ProjectedWindow | null {
  return declaredWindow(world, "mercator");
}

/**
 * The px/deg pair of one declared projection, or null when the world declares
 * a different projection, none at all, or a pair that cannot be read. A window
 * with no width, no height, or a degenerate degree range is not a mapping:
 * inverting it would divide by zero, and silence beats a plausible NaN.
 *
 * The projection key gates the equirect pair outright — "required when
 * surface is `sphere`", and numbers under an undeclared flattening are
 * numbers this cannot read. The MERCATOR pair alone is also readable when no
 * projection is declared at all: the registry's projection enum names only
 * `equirect`, so a plane georeferenced by a real-world tile window (bend-or)
 * carries `atlas.geometry.mercator.*` with no projection key, and the keys
 * themselves say what the numbers are.
 */
function declaredWindow(
  world: World | null | undefined,
  projection: Projection,
): ProjectedWindow | null {
  const attrs = world?.attrs;
  if (!attrs) return null;
  const declared = attrs[KEY_GEOMETRY_PROJECTION];
  if (declared !== projection && !(projection === "mercator" && declared === undefined)) {
    return null;
  }
  const px = quad(attrs[WINDOW[projection].px]);
  const deg = quad(attrs[WINDOW[projection].deg]);
  if (!px || !deg) return null;
  const [, , w, h] = px;
  const [west, north, east, south] = deg;
  if (!w || !h || north === south || west === east) return null;
  return { px, deg };
}

/** A comma-separated four-tuple of finite numbers, or null. */
function quad(value: string | undefined): Quad | null {
  const parts = (value ?? "").split(",").map(Number);
  const [a, b, c, d] = parts;
  if (parts.length !== 4) return null;
  if (a === undefined || b === undefined || c === undefined || d === undefined) return null;
  if ([a, b, c, d].some(Number.isNaN)) return null;
  return [a, b, c, d];
}
