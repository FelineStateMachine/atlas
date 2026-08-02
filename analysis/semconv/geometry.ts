// The geometry half of the published attribute vocabulary, read by the
// analysis lane.
//
// Issue #5 §3.2 lets this lane depend on "its own math and the published
// attribute vocabulary" and nothing else. `format/semconv` is the vocabulary's
// Go home and `spec/registry.yaml` (§8, an M7 fast-follow) will be the one
// machine-readable source both ends generate from. Until that exists, this
// module is the lane's hand-written reader for the four `atlas.geometry.*`
// keys a cell system can be asked about — and it is deliberately a *reader*:
// it never writes a key, never validates a bundle, and knows nothing about
// how a world reached it.
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

const SURFACE = "atlas.geometry.surface";
const PROJECTION = "atlas.geometry.projection";

/**
 * What the world's raster pictures. A world that says nothing is a plane,
 * which every world was until the planets arrived.
 */
export function worldSurface(world: World | null | undefined): string {
  const declared = world?.attrs?.[SURFACE];
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

/**
 * The px/deg pair of one declared projection, or null when the world declares
 * a different projection, none at all, or a pair that cannot be read. A window
 * with no width, no height, or a degenerate degree range is not a mapping:
 * inverting it would divide by zero, and silence beats a plausible NaN.
 */
function declaredWindow(
  world: World | null | undefined,
  projection: string,
): { px: Quad; deg: Quad } | null {
  const attrs = world?.attrs;
  if (!attrs || attrs[PROJECTION] !== projection) return null;
  const px = quad(attrs[`atlas.geometry.${projection}.px`]);
  const deg = quad(attrs[`atlas.geometry.${projection}.deg`]);
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
