// Test-only adapter for the committed v3 JSON corpus. Production code has no
// legacy presentation reader; these old extractions remain useful as expected
// pictures while the corpus is rewritten as semantic fixtures.

import type { LocationTable } from "../data/atlasloc.ts";
import type { Collection, Geometry as LegacyGeometry, WorldPayload, TileGrid } from "../data/payload.ts";
import type { OpenWorld } from "../data/plane.ts";
import type {
  Feature, Geometry, GeometryPart, Property, RasterCoverage, RasterPyramid,
} from "../data/semantic.ts";
import { project } from "../world/model.ts";

export function legacyOpenWorld(
  slug: string,
  payload: WorldPayload,
  grid: TileGrid,
  locations: LocationTable | null,
): OpenWorld {
  const sets = payload.collections.map((collection, owner) => ({
    id: `legacy/set/${collection.id}`,
    title: collection.title,
    semanticType: `geometry.${collection.kind}`,
    properties: [],
    claims: properties(collection.attrs),
    features: features(collection, owner, grid, locations),
  }));
  const styles = payload.collections.map((collection) => ({
    id: `legacy/style/${collection.id}`, symbol: "", icon: collection.icon ?? "",
    iconAsset: "", iconPicture: Boolean(collection.iconPicture),
    renderAs: collection.renderAs ?? collection.attrs?.["atlas.render.as"] ?? "",
    stroke: collection.kind === "path" ? collection.color ?? collection.iconColor ?? "" : "",
    fill: collection.color ?? collection.iconColor ?? "",
  }));
  const layers = payload.collections.map((collection, order) => ({
    id: String(collection.id), presentation: "legacy/presentation",
    featureSet: `legacy/set/${collection.id}`, style: `legacy/style/${collection.id}`,
    group: collection.group ?? "", labelPolicy: collection.labelPolicy ?? collection.attrs?.["atlas.label.policy"] ?? "",
    visible: collection.visible, order, minZoom: 0, maxZoom: 32,
  }));
  const rasters = payload.lenses.map((lens): RasterPyramid => ({
    id: lens.id ?? lens.tiles, name: lens.name, codec: "image/fixture", tileSize: grid.tileSize,
    minZoom: lens.minZoom, maxZoom: lens.maxZoom, fullZoom: lens.fullZoom,
    sourceZoom: lens.sourceZoom, template: lens.template ?? `tiles/${lens.tiles}/{z}/{x}/{y}.{format}`,
    formats: lens.formats, bounds: lens.bounds ?? null, surface: lens.surface ?? null,
    interpolate: lens.interpolate, background: lens.background ?? "", shard: lens.shard ?? 0,
    coverage: Object.entries(lens.coverage ?? {}).map(([zoom, level]): RasterCoverage => ({
      zoom: Number(zoom), x: level.x, y: level.y, w: level.w, h: level.h, bits: unbase64(level.bits),
    })),
  }));
  return {
    volume: { id: "legacy", title: "Legacy fixture", worlds: [], assets: new Map() },
    world: {
      id: slug, title: slug,
      coordinateSpace: {
        id: `${slug}/space`, kind: "projected", unit: "pixel", definition: "atlas:tile-plane",
        extent: [0, 0, grid.size, grid.size], sourceZoom: grid.sourceZoom,
        firstTile: grid.firstTile, tileSize: grid.tileSize, size: grid.size,
      },
      claims: properties(payload.attrs), featureSets: sets, rasterPyramids: rasters,
      presentation: {
        id: "legacy/presentation", title: slug, styles, layers,
        legend: layers.map((layer, order) => ({ layer: layer.id, label: sets[order]?.title ?? "", order })),
      },
    },
  };
}

function features(
  collection: Collection,
  owner: number,
  grid: TileGrid,
  locations: LocationTable | null,
): Feature[] {
  const out: Feature[] = [];
  if (collection.kind === "point" && locations) {
    for (let row = 0; row < locations.count; row++) {
      if (locations.owner[row] !== owner) continue;
      const shown = project(grid, locations.lat[row] ?? 0, locations.lng[row] ?? 0);
      const id = String(locations.id[row] ?? 0);
      const position = [shown[0], -shown[1]] as const;
      out.push(feature(id, locations.title(row), {
        kind: 1, parts: [{ rings: [[position]] }],
      }, locations.shard[row] ?? 0, [], locations.member[row] ? [{ predicate: "within", target: String(locations.member[row]) }] : []));
    }
  }
  for (const shape of collection.features ?? []) {
    const relationships = shape.parent === undefined ? [] : [{ predicate: "within", target: String(shape.parent) }];
    const held = feature(String(shape.id), shape.title, legacyGeometry(shape.geometry, collection.kind, grid), shape.shard ?? 0,
      properties(shape.attrs), relationships);
    (held as { subtitle: string }).subtitle = shape.subtitle ?? "";
    if (shape.center) {
      const shown = project(grid, shape.center.lat, shape.center.lng);
      (held as { center: readonly [number, number] }).center = [shown[0], -shown[1]];
    }
    out.push(held);
  }
  return out;
}

function feature(
  id: string,
  title: string,
  geometry: Geometry,
  shard: number,
  attrs: readonly Property[],
  relationships: readonly { readonly predicate: string; readonly target: string }[],
): Feature {
  return {
    id, title, subtitle: "", description: "", center: null, shard, geometry,
    properties: attrs, relationships, provenance: [],
  };
}

function legacyGeometry(items: readonly LegacyGeometry[], kind: Collection["kind"], grid: TileGrid): Geometry {
  const parts: GeometryPart[] = [];
  for (const item of items) {
    switch (item.type) {
      case "Polygon": parts.push({ rings: rings(item.coordinates, grid) }); break;
      case "MultiPolygon": for (const polygon of item.coordinates as unknown[]) parts.push({ rings: rings(polygon, grid) }); break;
      case "LineString": parts.push({ rings: [line(item.coordinates, grid)] }); break;
      case "MultiLineString": for (const raw of item.coordinates as unknown[]) parts.push({ rings: [line(raw, grid)] }); break;
    }
  }
  return { kind: kind === "path" ? 2 : 3, parts };
}

function rings(raw: unknown, grid: TileGrid): readonly (readonly (readonly [number, number])[])[] {
  return (raw as unknown[]).map((held) => line(held, grid));
}

function line(raw: unknown, grid: TileGrid): readonly (readonly [number, number])[] {
  return (raw as [number, number][]).map(([lng, lat]) => {
    const shown = project(grid, lat, lng);
    return [shown[0], -shown[1]] as const;
  });
}

function properties(attrs?: Readonly<Record<string, string>>): Property[] {
  return Object.entries(attrs ?? {}).map(([name, value]) => ({ id: `legacy:${name}`, name, kind: 4, value }));
}

function unbase64(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}
