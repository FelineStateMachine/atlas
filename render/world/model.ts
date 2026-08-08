// One semantic world, presented once for both panes.
//
// The network boundary is schema + packed vNext tables. This model applies
// the world's replaceable Presentation to its FeatureSets; it never receives
// a v3 document, numeric compatibility identity, or GeoJSON translation.

import type { Ground } from "@atlas/analysis";
import type { Collection, Lens, TileGrid, WorldPayload } from "../data/payload.ts";
import type { OpenWorld } from "../data/plane.ts";
import type {
  Feature, Geometry, Property, RasterPyramid, Style,
} from "../data/semantic.ts";

/** `[x, y]` in OL world coordinates: x east, y negative-down. */
export type Coordinate = [number, number];

/** A closed or open run of coordinates: a ring, or a line. */
export type Line = Coordinate[];

/** A coordinate-space position enters OpenLayers by flipping y exactly once. */
export function displayPosition(position: readonly [number, number]): Coordinate {
  return [position[0], -position[1]];
}

/** Geographic coordinates to the native tile-plane space used by producers. */
export function project(grid: TileGrid, lat: number, lng: number): Coordinate {
  const worldTiles = 2 ** grid.sourceZoom;
  const xTile = ((lng + 180) / 360) * worldTiles;
  const yTile = (1 - Math.asinh(Math.tan((lat * Math.PI) / 180)) / Math.PI) / 2 * worldTiles;
  return [(xTile - grid.firstTile) * grid.tileSize, -(yTile - grid.firstTile) * grid.tileSize];
}

/** Kept for test fixtures extracted before coordinate spaces became native. */
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
  readonly member: string;
  readonly shard: number;
  readonly feature: Feature;
  readonly priority: number;
}

/** One path or area feature, already in display coordinates. */
export interface ShapeRecord {
  readonly id: string;
  readonly title: string;
  readonly subtitle: string;
  readonly collection: Collection;
  readonly kind: "path" | "area";
  readonly shard: number;
  readonly lines: readonly Line[];
  readonly holes: readonly (readonly Line[])[];
  readonly center: Coordinate | null;
  readonly feature: Feature;
}

/** The whole of one world, built once from native semantics. */
export class WorldModel {
  readonly points: readonly PointRecord[];
  readonly shapes: readonly ShapeRecord[];
  readonly pointByID: ReadonlyMap<string, PointRecord>;
  readonly shapeByID: ReadonlyMap<string, ShapeRecord>;
  readonly collections: readonly Collection[];
  readonly slug: string;
  readonly title: string;
  readonly lenses: readonly Lens[];
  readonly attrs: Readonly<Record<string, string>>;
  readonly grid: TileGrid;

  constructor(open: OpenWorld) {
    const { volume, world } = open;
    this.slug = world.id;
    this.title = world.title;
    this.grid = {
      sourceZoom: world.coordinateSpace.sourceZoom,
      firstTile: world.coordinateSpace.firstTile,
      tileSize: world.coordinateSpace.tileSize,
      size: world.coordinateSpace.size,
    };
    this.attrs = propertiesToAttrs(world.claims);
    this.lenses = world.rasterPyramids.map(presentRaster);

    const sets = new Map(world.featureSets.map((set) => [set.id, set]));
    const styles = new Map(world.presentation.styles.map((style) => [style.id, style]));
    const labels = new Map(world.presentation.legend.map((entry) => [entry.layer, entry.label]));
    const collections: Collection[] = [];
    const points: PointRecord[] = [];
    const shapes: ShapeRecord[] = [];

    for (const layer of world.presentation.layers) {
      const set = sets.get(layer.featureSet);
      const style = styles.get(layer.style);
      if (!set || !style) throw new Error(`layer ${layer.id} has an unresolved feature set or style`);
      const kind = semanticKind(set.semanticType);
      const asset = style.iconAsset ? volume.assets.get(style.iconAsset) : undefined;
      const collection: Collection = {
        id: layer.id, featureSet: set.id, style: style.id,
        title: labels.get(layer.id) || set.title, group: layer.group, kind,
        icon: style.icon || style.symbol, iconAsset: asset?.path ?? "", iconPicture: style.iconPicture,
        color: presentedColor(kind, style), visible: layer.visible,
        labelPolicy: layer.labelPolicy, renderAs: style.renderAs,
        attrs: propertiesToAttrs(set.claims),
      };
      collections.push(collection);
      for (const feature of set.features) {
        if (feature.geometry.kind === 1) points.push(pointRecord(feature, collection, points.length));
        else {
          const shape = shapeRecord(feature, collection);
          if (shape) shapes.push(shape);
        }
      }
    }

    const sizes = new Map<string | number, number>();
    for (const point of points) sizes.set(point.collection.id, (sizes.get(point.collection.id) ?? 0) + 1);
    this.points = points.map((point, index) => ({
      ...point, index, priority: priorityOf(sizes.get(point.collection.id) ?? 0, point.id),
    }));
    this.shapes = shapes;
    this.pointByID = new Map(this.points.map((point) => [point.id, point]));
    this.shapeByID = new Map(shapes.map((shape) => [shape.id, shape]));
    this.collections = collections;
  }

  feature(id: string): PointRecord | ShapeRecord | null {
    return this.pointByID.get(id) ?? this.shapeByID.get(id) ?? null;
  }

  ground(lens: Lens | null): Ground {
    return {
      tileGridSize: this.grid.size,
      lens: lens ? { surface: lens.surface ?? null, bounds: lens.bounds ?? null } : null,
      world: { attrs: this.attrs },
    };
  }
}

export function priorityOf(members: number, id: string): number {
  const rarity = Math.max(0, 1_000_000 - Math.min(members, 999) * 1000);
  return rarity + (stableRank(id) % 1000);
}

/** FNV-1a over the durable identity; arbitrary strings rank reproducibly. */
export function stableRank(id: string): number {
  let hash = 2166136261 >>> 0;
  for (let i = 0; i < id.length; i++) {
    hash ^= id.charCodeAt(i);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash >>> 0;
}

function pointRecord(feature: Feature, collection: Collection, index: number): PointRecord {
  const position = feature.geometry.parts[0]?.rings[0]?.[0];
  if (!position || feature.geometry.parts.length !== 1 || feature.geometry.parts[0]?.rings.length !== 1 ||
      feature.geometry.parts[0]?.rings[0]?.length !== 1) {
    throw new Error(`point ${feature.id} has invalid native geometry`);
  }
  return {
    id: feature.id, index, title: feature.title, collection, coordinate: displayPosition(position),
    member: feature.relationships.find((edge) => edge.predicate === "within")?.target ?? "",
    shard: feature.shard, feature, priority: 0,
  };
}

function shapeRecord(feature: Feature, collection: Collection): ShapeRecord | null {
  const { lines, holes } = displayGeometry(feature.geometry);
  if (!lines.length) return null;
  return {
    id: feature.id, title: feature.title, subtitle: feature.subtitle, collection,
    kind: feature.geometry.kind === 2 ? "path" : "area", shard: feature.shard,
    lines, holes, center: feature.center ? displayPosition(feature.center) : null, feature,
  };
}

function displayGeometry(geometry: Geometry): { lines: Line[]; holes: Line[][] } {
  const lines: Line[] = [];
  const holes: Line[][] = [];
  for (const part of geometry.parts) {
    if (geometry.kind === 2) {
      const line = part.rings[0];
      if (part.rings.length !== 1 || !line) continue;
      lines.push(line.map(displayPosition));
      holes.push([]);
      continue;
    }
    if (geometry.kind === 3) {
      const [outer, ...inner] = part.rings;
      if (!outer) continue;
      lines.push(outer.map(displayPosition));
      holes.push(inner.map((ring) => ring.map(displayPosition)));
    }
  }
  return { lines, holes };
}

function semanticKind(value: string): "point" | "path" | "area" {
  const kind = value.startsWith("geometry.") ? value.slice("geometry.".length) : value;
  if (kind !== "point" && kind !== "path" && kind !== "area") throw new Error(`semantic geometry kind ${value} is unsupported`);
  return kind;
}

function presentRaster(raster: RasterPyramid): Lens {
  const coverage: Record<string, { x: number; y: number; w: number; h: number; bits: string }> = {};
  for (const level of raster.coverage) {
    coverage[String(level.zoom)] = {
      x: level.x, y: level.y, w: level.w, h: level.h, bits: base64(level.bits),
    };
  }
  const lens: Lens = {
    id: raster.id, name: raster.name, tiles: raster.id, template: raster.template,
    minZoom: raster.minZoom, maxZoom: raster.maxZoom, fullZoom: raster.fullZoom,
    sourceZoom: raster.sourceZoom, formats: raster.formats,
    interpolate: raster.interpolate,
  };
  return {
    ...lens,
    ...(raster.bounds ? { bounds: raster.bounds } : {}),
    ...(raster.surface ? { surface: raster.surface } : {}),
    ...(raster.background ? { background: raster.background } : {}),
    ...(raster.shard ? { shard: raster.shard } : {}),
    ...(Object.keys(coverage).length ? { coverage } : {}),
  };
}

function presentedColor(kind: string, style: Style): string {
  return kind === "path" && style.stroke ? style.stroke : style.fill;
}

function propertiesToAttrs(properties: readonly Property[]): Readonly<Record<string, string>> {
  const attrs: Record<string, string> = {};
  for (const property of properties) attrs[property.name] = propertyText(property.value);
  return attrs;
}

function propertyText(value: Property["value"]): string {
  if (value instanceof Uint8Array) return base64(value);
  return String(value);
}

function base64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}
