// The world as the seam holds it: every feature, in world coordinates, once.
//
// Three payloads arrive from `/data` and one model comes out of them. The
// model is built once per world and is immutable afterwards: filters, grids
// and selections are questions asked *of* it (`visibility.ts`), never edits
// to it, which is what lets the chart and the globe read the same registry
// and answer the same counts.
//
// TWO CONVENTIONS MEET HERE, and both are load-bearing.
//
//   A payload speaks **y-down world pixels** and spells positions as
//   `lat`/`lng` in the volume's own world space — which is the slippy-tile
//   grid the capture was cut from, not WGS 84. `project` is that conversion,
//   and it is the same arithmetic the application does server-side
//   (`internal/app/world.go`). It is **not** written down in `docs/format.md`,
//   which is a gap in the format spec rather than a decision of this lane; it
//   is written down in `docs/render-seam.md` §3 until it moves where it
//   belongs.
//
//   Everything downstream — OpenLayers, the analysis lane's cell systems, the
//   grid, the overview — speaks **OL world coordinates**: x east, y
//   negative-down. The sign flips exactly once, here.

import type { Ground } from "@atlas/analysis";
import type { LocationTable } from "../data/atlasloc.ts";
import type {
  Collection, Geometry, Lens, ShapeFeature, TileGrid, WorldPayload,
} from "../data/payload.ts";

/** `[x, y]` in OL world coordinates: x east, y negative-down. */
export type Coordinate = [number, number];

/** A closed or open run of coordinates: a ring, or a line. */
export type Line = Coordinate[];

/**
 * Where a `lat`/`lng` pair lands on the world square.
 *
 * A pin arrives as a latitude and longitude in the source tile grid, which is
 * not the same grid for every volume: most worlds are cut from a 32-tile
 * square at zoom 13, and a world cut from a window of its own carries where
 * that window sits (format.md §6.2). Both describe the same world square, so
 * everything downstream works in world units either way.
 */
export function project(grid: TileGrid, lat: number, lng: number): Coordinate {
  const worldTiles = 2 ** grid.sourceZoom;
  const xTile = ((lng + 180) / 360) * worldTiles;
  const yTile = (1 - Math.asinh(Math.tan((lat * Math.PI) / 180)) / Math.PI) / 2 * worldTiles;
  return [
    (xTile - grid.firstTile) * grid.tileSize,
    -(yTile - grid.firstTile) * grid.tileSize,
  ];
}

/** The volume's grid, with the world's own window override applied. */
export function worldGrid(volume: TileGrid, payload: WorldPayload): TileGrid {
  return payload.grid ? { ...volume, ...payload.grid } : volume;
}

/** One point feature, standing where it stands. */
export interface PointRecord {
  readonly id: string;
  readonly index: number;
  readonly title: string;
  readonly collection: Collection;
  readonly coordinate: Coordinate;
  readonly member: number;
  readonly shard: number;
  /**
   * The order this pin takes its place in when pins overlap: rarer
   * collections first, ties broken by a stable hash of the id so two runs
   * draw the crowd in the same order. Also the declutter priority — a label
   * that has to give way gives way to a rarer one, never to whichever
   * happened to be built first.
   */
  readonly priority: number;
}

/** One path or area feature, projected. */
export interface ShapeRecord {
  readonly id: string;
  readonly title: string;
  readonly subtitle: string;
  readonly collection: Collection;
  readonly kind: "path" | "area";
  readonly shard: number;
  /** Outer rings (areas) or lines (paths), already in world coordinates. */
  readonly lines: readonly Line[];
  /** Inner rings, per outer ring, for areas that carry holes. */
  readonly holes: readonly (readonly Line[])[];
  readonly center: Coordinate | null;
  readonly feature: ShapeFeature;
}

/** The whole of one world, built once. */
export class WorldModel {
  readonly points: readonly PointRecord[];
  readonly shapes: readonly ShapeRecord[];
  readonly pointByID: ReadonlyMap<string, PointRecord>;
  readonly shapeByID: ReadonlyMap<string, ShapeRecord>;
  readonly collections: readonly Collection[];

  readonly slug: string;
  readonly payload: WorldPayload;
  readonly grid: TileGrid;

  constructor(
    slug: string,
    payload: WorldPayload,
    grid: TileGrid,
    table: LocationTable | null,
  ) {
    this.slug = slug;
    this.payload = payload;
    this.grid = grid;
    this.collections = payload.collections;
    const points: PointRecord[] = [];
    const pointByID = new Map<string, PointRecord>();
    if (table) {
      // The packed payload's `owner` indexes the collections array, which is
      // the only collection identity a reader needs and the whole reason that
      // array's order is significant (format.md §7.1).
      const sizes = new Map<number, number>();
      for (let i = 0; i < table.count; i++) {
        const owner = table.owner[i] ?? 0;
        sizes.set(owner, (sizes.get(owner) ?? 0) + 1);
      }
      for (let i = 0; i < table.count; i++) {
        const owner = table.owner[i] ?? 0;
        const collection = payload.collections[owner];
        if (!collection || collection.kind !== "point") continue;
        const id = String(table.id[i] ?? 0);
        const record: PointRecord = {
          id,
          index: i,
          title: table.title(i),
          collection,
          coordinate: project(grid, table.lat[i] ?? 0, table.lng[i] ?? 0),
          member: table.member[i] ?? 0,
          shard: table.shard[i] ?? 0,
          priority: priorityOf(sizes.get(owner) ?? 0, table.id[i] ?? 0),
        };
        points.push(record);
        pointByID.set(id, record);
      }
    }
    this.points = points;
    this.pointByID = pointByID;

    const shapes: ShapeRecord[] = [];
    const shapeByID = new Map<string, ShapeRecord>();
    for (const collection of payload.collections) {
      if (collection.kind === "point") continue;
      for (const feature of collection.features ?? []) {
        const record = shapeRecord(grid, collection, feature);
        if (!record) continue;
        shapes.push(record);
        shapeByID.set(record.id, record);
      }
    }
    this.shapes = shapes;
    this.shapeByID = shapeByID;
  }

  /** Either kind of feature, by the id the application spells it with. */
  feature(id: string): PointRecord | ShapeRecord | null {
    return this.pointByID.get(id) ?? this.shapeByID.get(id) ?? null;
  }

  /** The ground a cell system divides: the analysis lane's own descriptor. */
  ground(lens: Lens | null): Ground {
    return {
      tileGridSize: this.grid.size,
      lens: lens ? { surface: lens.surface ?? null, bounds: lens.bounds ?? null } : null,
      world: { attrs: this.payload.attrs ?? {} },
    };
  }
}

/**
 * A pin's place in the crowd.
 *
 * Rarity first — a collection with three members is what a reader came to
 * find, and one with nine hundred is the texture behind it — then a stable
 * hash of the feature's own id, so a tie is broken the same way on every run
 * and in every pane.
 */
export function priorityOf(members: number, id: number): number {
  const rarity = Math.max(0, 1_000_000 - Math.min(members, 999) * 1000);
  return rarity + (stableRank(id) % 1000);
}

/** A small deterministic hash: the same id ranks the same way forever. */
export function stableRank(id: number): number {
  let hash = 2166136261 >>> 0;
  const text = String(id);
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash >>> 0;
}

function shapeRecord(
  grid: TileGrid,
  collection: Collection,
  feature: ShapeFeature,
): ShapeRecord | null {
  const lines: Line[] = [];
  const holes: Line[][] = [];
  for (const geometry of feature.geometry ?? []) {
    for (const { outer, inner } of readGeometry(grid, geometry)) {
      lines.push(outer);
      holes.push(inner);
    }
  }
  if (!lines.length) return null;
  return {
    id: String(feature.id),
    title: feature.title ?? "",
    subtitle: feature.subtitle ?? "",
    collection,
    kind: collection.kind === "path" ? "path" : "area",
    shard: feature.shard ?? 0,
    lines,
    holes,
    center: feature.center ? project(grid, feature.center.lat, feature.center.lng) : null,
    feature,
  };
}

type Piece = { outer: Line; inner: Line[] };

/**
 * GeoJSON-shaped geometry, projected into world coordinates.
 *
 * Coordinate pairs are `[lng, lat]` as GeoJSON writes them, in the volume's
 * own world space. Types are read leniently: a geometry whose type does not
 * fit its collection's kind was refused at build time (format.md §9.3), so a
 * type this reader does not know is a newer producer, not a broken bundle.
 */
function readGeometry(grid: TileGrid, geometry: Geometry): Piece[] {
  const point = (pair: unknown): Coordinate => {
    const [lng, lat] = pair as [number, number];
    return project(grid, lat, lng);
  };
  const line = (raw: unknown): Line => (raw as unknown[]).map(point);
  const polygon = (raw: unknown): Piece => {
    const rings = (raw as unknown[]).map(line);
    const [outer, ...inner] = rings;
    return { outer: outer ?? [], inner };
  };
  switch (geometry.type) {
    case "Polygon":
      return [polygon(geometry.coordinates)];
    case "MultiPolygon":
      return (geometry.coordinates as unknown[]).map(polygon);
    case "LineString":
      return [{ outer: line(geometry.coordinates), inner: [] }];
    case "MultiLineString":
      return (geometry.coordinates as unknown[]).map((raw) => ({ outer: line(raw), inner: [] }));
    default:
      return [];
  }
}
