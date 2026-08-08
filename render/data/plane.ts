// The `/data` plane, and the only place in the seam that reaches the network.
//
// One module owns fetching so the plane's three properties have one owner
// (issue #5 §9, and the ESLint rule that says so by name):
//
//   THE URL SHAPES. `/data/catalog.json` and `/data/v/{slug}/{stamp12}/…`,
//   and nothing else. A base carries the twelve-hex short stamp, so every URL
//   under it names exactly one build.
//
//   THE CACHE RULES. Everything under a base is immutable for a year, so a
//   payload is fetched once per build and kept for as long as the page holds
//   the build. The catalog is `no-store` and is never kept.
//
//   THE 404 AS A SIGNAL. When a newer build takes a slug over, every URL
//   under the old stamp is gone. A 404 from a base is not a broken link: it
//   is the build having moved, and the page's cue to ask the server for a new
//   scene. The seam says so on the stream and gives up on that base; it does
//   not reload the page, because navigation is the application's to decide.
//
// The one interpretation here is structural: schema-described typed columns
// become the semantic Volume graph. Presentation decisions remain the world's.

import { logger } from "../log.ts";
import type { Catalog, Lens } from "./payload.ts";
import { tileFormat, tileTemplate } from "./payload.ts";
import {
  type Asset, type Feature, type Geometry, type Property, type PropertyValue,
  type Volume, type World,
} from "./semantic.ts";

const log = logger("data");
const MAX_VISIBLE_FEATURE_PAGES = 32;

/** A build's URL prefix: `/data/v/<slug>/<stamp12>`. */
export type Base = string;

export interface OpenWorld {
  readonly volume: Volume;
  readonly world: World;
}

export interface FeatureWindow {
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
}

/** Raised when a base answered 404 — the build moved out from under us. */
export class BuildMovedError extends Error {
  readonly url: string;

  constructor(url: string) {
    super(`the build serving ${url} has moved; the scene is stale`);
    this.name = "BuildMovedError";
    this.url = url;
  }
}

/**
 * One page's reading of the plane.
 *
 * Native volumes are kept per base, which is per build, because the URL names the
 * build. Two worlds of one volume share nothing but the base; two builds of
 * one volume share nothing at all, which is the whole cache story.
 */
export class DataPlane {
  private readonly payloads = new Map<string, Promise<unknown>>();

  /** The catalog, composed at the moment it is asked for. Never kept. */
  async catalog(): Promise<Catalog> {
    const response = await fetch("/data/catalog.json", { cache: "no-store" });
    if (!response.ok) throw new Error(`the catalog answered ${response.status}`);
    return (await response.json()) as Catalog;
  }

  /** One semantic world, reconstructed from the schema-described tables. */
  async world(base: Base, world: string, window: FeatureWindow | null = null): Promise<OpenWorld> {
    const outline = await this.outline(base);
    const found = outline.worlds.find((candidate) => candidate.id === world);
    if (!found) throw new Error(`${base} does not contain world ${world}`);
    const bounds = visibleBounds(found.coordinateSpace.extent, window);
    const pages = await Promise.all(found.featureSets.map((set) => this.features(base, set.id, bounds)));
    const opened = {
      ...found,
      featureSets: found.featureSets.map((set, index) => ({ ...set, features: pages[index] ?? [] })),
    };
    const volume = {
      ...outline,
      worlds: outline.worlds.map((candidate) => candidate.id === world ? opened : candidate),
    };
    log.info("the semantic world is open", {
      op: "render", path: base, world, features: pages.reduce((sum, page) => sum + page.length, 0),
    });
    return { volume, world: opened };
  }

  /** Where an icon asset lives. Icons are fetched by the browser, as images. */
  iconURL(base: Base, asset: string): string {
    return iconURL(base, asset);
  }

  /** Where one tile lives, or null when the lens holds no such level. */
  tileURL(base: Base, lens: Lens, z: number, x: number, y: number): string | null {
    const extension = tileFormat(lens, z);
    if (!extension) return null;
    return `${base}/${fillTemplate(tileTemplate(lens), z, x, y, extension)}`;
  }

  private outline(base: Base): Promise<Volume> {
    return this.keep(`${base}/@outline`, async () => decodeOutline(await this.json<unknown>(`${base}/outline.json`)));
  }

  private async features(base: Base, set: string, bounds: readonly [number, number, number, number]): Promise<readonly Feature[]> {
    const features: Feature[] = [];
    let after = "";
    for (let pageIndex = 0; pageIndex < MAX_VISIBLE_FEATURE_PAGES; pageIndex++) {
      const query = new URLSearchParams({
        limit: "1000", minX: String(bounds[0]), minY: String(bounds[1]),
        maxX: String(bounds[2]), maxY: String(bounds[3]),
      });
      if (after) query.set("after", after);
      const page = await this.json<Record<string, unknown>>(
        `${base}/features/${encodeURIComponent(set)}.json?${query.toString()}`,
      );
      for (const feature of array(page.features)) features.push(decodeFeature(object(feature)));
      after = text(page.next);
      if (!after) return features;
    }
    throw new Error(`visible feature window for ${set} exceeds ${MAX_VISIBLE_FEATURE_PAGES * 1000} features`);
  }

  private json<T>(url: string): Promise<T> {
    return this.keep(url, async (at) => (await this.response(at)).json() as Promise<T>);
  }

  private async response(url: string): Promise<Response> {
    const response = await fetch(url);
    if (response.status === 404) {
      log.warn("a payload is gone, which means the build moved", {
        op: "render", path: url,
      });
      throw new BuildMovedError(url);
    }
    if (!response.ok) throw new Error(`${url} answered ${response.status}`);
    return response;
  }

  private keep<T>(url: string, read: (url: string) => Promise<T>): Promise<T> {
    const held = this.payloads.get(url) as Promise<T> | undefined;
    if (held) return held;
    // A failed read is forgotten rather than kept: a network hiccup should
    // cost a retry, not the rest of the session.
    const reading = read(url).catch((error: unknown) => {
      this.payloads.delete(url);
      throw error;
    });
    this.payloads.set(url, reading);
    return reading;
  }
}

function visibleBounds(
  extent: readonly [number, number, number, number],
  window: FeatureWindow | null,
): readonly [number, number, number, number] {
  if (!window || !Number.isFinite(window.x) || !Number.isFinite(window.y) || !Number.isFinite(window.zoom)) return extent;
  const width = extent[2] - extent[0];
  const height = extent[3] - extent[1];
  const span = Math.max(width, height) / 2 ** Math.max(0, window.zoom);
  const x = Math.min(Math.max(window.x, extent[0]), extent[2]);
  const y = Math.min(Math.max(-window.y, extent[1]), extent[3]);
  return [
    Math.max(extent[0], x - span / 2), Math.max(extent[1], y - span / 2),
    Math.min(extent[2], x + span / 2), Math.min(extent[3], y + span / 2),
  ];
}

function decodeOutline(source: unknown): Volume {
  const root = object(source);
  if (number(root.version) !== 1) throw new Error("native outline version is unsupported");
  const assets = array(root.assets).map((entry) => decodeAsset(object(entry)));
  return {
    id: text(root.id), title: text(root.title), assets: new Map(assets.map((asset) => [asset.id, asset])),
    worlds: array(root.worlds).map((entry) => decodeWorld(object(entry))),
  };
}

function decodeWorld(raw: Record<string, unknown>): World {
  const coordinate = object(raw.coordinateSpace);
  const presentation = object(raw.presentation);
  return {
    id: text(raw.id), title: text(raw.title), claims: wireProperties(raw.claims),
    coordinateSpace: {
      id: text(coordinate.id), kind: text(coordinate.kind), unit: text(coordinate.unit),
      definition: text(coordinate.definition), extent: tuple4(coordinate.extent),
      sourceZoom: number(coordinate.sourceZoom), originX: number(coordinate.originX),
      originY: number(coordinate.originY), tileSize: number(coordinate.tileSize), size: number(coordinate.size),
    },
    featureSets: array(raw.featureSets).map((entry) => {
      const set = object(entry);
      return {
        id: text(set.id), title: text(set.title), semanticType: text(set.semanticType),
        properties: array(set.properties).map((field) => {
          const value = object(field);
          return { id: text(value.id), name: text(value.name), kind: number(value.kind), optional: Boolean(value.optional) };
        }),
        claims: wireProperties(set.claims), features: [],
      };
    }),
    rasterPyramids: array(raw.rasterPyramids).map((entry) => {
      const raster = object(entry);
      return {
        id: text(raster.id), name: text(raster.name), codec: text(raster.codec), tileSize: number(raster.tileSize),
        minZoom: number(raster.minZoom), maxZoom: number(raster.maxZoom), fullZoom: number(raster.fullZoom),
        sourceZoom: number(raster.sourceZoom), template: text(raster.template), formats: array(raster.formats).map(text),
        bounds: raster.bounds ? rect(object(raster.bounds)) : null, surface: raster.surface ? rect(object(raster.surface)) : null,
        interpolate: Boolean(raster.interpolate), background: text(raster.background), shard: number(raster.shard),
        coverage: array(raster.coverage).map((entry) => {
          const coverage = object(entry);
          return {
            zoom: number(coverage.zoom), x: number(coverage.x), y: number(coverage.y), w: number(coverage.w),
            h: number(coverage.h), bits: bytes(coverage.bits),
          };
        }),
      };
    }),
    presentation: {
      id: text(presentation.id), title: text(presentation.title),
      styles: array(presentation.styles).map((entry) => {
        const style = object(entry);
        return {
          id: text(style.id), symbol: text(style.symbol), icon: text(style.icon), iconAsset: text(style.iconAsset),
          iconPicture: Boolean(style.iconPicture), renderAs: text(style.renderAs), stroke: text(style.stroke), fill: text(style.fill),
        };
      }),
      layers: array(presentation.layers).map((entry) => {
        const layer = object(entry);
        return {
          id: text(layer.id), featureSet: text(layer.featureSet), style: text(layer.style), group: text(layer.group),
          labelPolicy: text(layer.labelPolicy), visible: Boolean(layer.visible), order: number(layer.order),
          minZoom: number(layer.minZoom), maxZoom: number(layer.maxZoom),
        };
      }),
      legend: array(presentation.legend).map((entry) => {
        const legend = object(entry);
        return { layer: text(legend.layer), label: text(legend.label), order: number(legend.order) };
      }),
    },
  };
}

function decodeFeature(raw: Record<string, unknown>): Feature {
  const center = raw.center;
  return {
    id: text(raw.id), title: text(raw.title), subtitle: text(raw.subtitle), description: text(raw.description),
    center: center ? tuple2(center) : null, shard: number(raw.shard), geometry: decodeWireGeometry(object(raw.geometry)),
    properties: wireProperties(raw.properties),
    relationships: array(raw.relationships).map((entry) => {
      const edge = object(entry);
      return { predicate: text(edge.predicate), target: text(edge.target) };
    }),
    provenance: array(raw.provenance).map((entry) => {
      const evidence = object(entry);
      return { source: text(evidence.source), nativeID: text(evidence.nativeId), capturedAt: text(evidence.capturedAt) };
    }),
  };
}

function decodeWireGeometry(raw: Record<string, unknown>): Geometry {
  const kind = number(raw.kind);
  if (kind !== 1 && kind !== 2 && kind !== 3) throw new Error(`geometry kind ${kind} is unsupported`);
  return {
    kind,
    parts: array(raw.parts).map((entry) => ({
      rings: array(object(entry).rings).map((ring) => array(ring).map(tuple2)),
    })),
  };
}

function wireProperties(source: unknown): readonly Property[] {
  return array(source).map((entry) => {
    const property = object(entry);
    const kind = number(property.kind);
    return { id: text(property.id), name: text(property.name), kind, value: wirePropertyValue(kind, property.value) };
  });
}

function wirePropertyValue(kind: number, value: unknown): PropertyValue {
  switch (kind) {
    case 1: return Boolean(value);
    case 2: return BigInt(text(value));
    case 3: return number(value);
    case 4: return text(value);
    case 5: return bytes(value);
    case 6: return text(value);
    default: throw new Error("property has no typed value");
  }
}

function decodeAsset(raw: Record<string, unknown>): Asset {
  return { id: text(raw.id), mediaType: text(raw.mediaType), path: text(raw.path), provenance: text(raw.provenance) };
}

function rect(raw: Record<string, unknown>): { x: number; y: number; width: number; height: number } {
  return { x: number(raw.x), y: number(raw.y), width: number(raw.width), height: number(raw.height) };
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("native response is not an object");
  return value as Record<string, unknown>;
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function text(value: unknown): string { return typeof value === "string" ? value : ""; }
function number(value: unknown): number { return typeof value === "number" ? value : Number(value ?? 0); }
function tuple2(value: unknown): readonly [number, number] {
  const held = array(value);
  return [number(held[0]), number(held[1])];
}
function tuple4(value: unknown): readonly [number, number, number, number] {
  const held = array(value);
  return [number(held[0]), number(held[1]), number(held[2]), number(held[3])];
}
function bytes(value: unknown): Uint8Array {
  if (typeof value !== "string" || !value) return new Uint8Array();
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}

/**
 * Where an icon asset lives.
 *
 * A free function as well as a method because an icon is the one thing under
 * a base that something other than the reader fetches: the browser asks for
 * it as an image, and both panes have to name it the same way or they compose
 * two rasters of one symbol.
 *
 * Every segment is encoded and the separators are not: an asset path is a
 * path, and a curated one may carry a space or a bracket — `Vault 101 (Ext)`
 * — which is a 404 if it goes onto the wire as it was written.
 */
export function iconURL(base: Base, asset: string): string {
  const path = asset.split("/").map((segment) => encodeURIComponent(segment)).join("/");
  return `${base}/${path}`;
}

export function fillTemplate(template: string, z: number, x: number, y: number, format: string): string {
  return template.replaceAll("{z}", String(z)).replaceAll("{x}", String(x))
    .replaceAll("{y}", String(y)).replaceAll("{format}", format);
}
