// Presentation-facing types shared by the native model and renderers.
//
// The wire contract itself lives in semantic.ts and vnext.ts. The older
// document-shaped interfaces below remain only for extracted test fixtures;
// production has no endpoint or decoder for them.

import {
  KEY_LABEL_POLICY,
  KEY_RENDER_AS,
  KEY_STROKE_WIDTH_PX,
} from "@atlas/analysis/semconv/keys";

/** `atlas.*` keys to their values, flat, as every entity carries them. */
export type Attrs = Readonly<Record<string, string>>;

/** A rectangle in y-down world pixels, the space a lens declares itself in. */
export interface PixelRect {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/** Sparse-level tile presence: format.md §6.3.1. */
export interface CoverageLevel {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
  readonly bits: string;
}

/** One raster pyramid picturing a world. */
export interface Lens {
  readonly id?: string;
  readonly name: string;
  /** Stable presentation key, derived from the semantic raster identity. */
  readonly tiles: string;
  /** Native blob path template; URLs substitute z/x/y/format directly. */
  readonly template?: string;
  readonly minZoom: number;
  readonly maxZoom: number;
  readonly fullZoom: number;
  readonly sourceZoom: number;
  readonly formats: readonly string[];
  readonly bounds?: PixelRect;
  readonly surface?: PixelRect;
  readonly interpolate: boolean;
  readonly background?: string;
  readonly shard?: number;
  readonly coverage?: Readonly<Record<string, CoverageLevel>>;
}

/** A point in the volume's own world space, as payloads spell it. */
export interface LatLng {
  readonly lat: number;
  readonly lng: number;
}

/** GeoJSON-shaped geometry in the volume's own world space. */
export interface Geometry {
  readonly type: string;
  readonly coordinates: unknown;
}

/** An inline path or area feature. Point collections carry none. */
export interface ShapeFeature {
  readonly id: number;
  readonly title: string;
  readonly subtitle?: string;
  readonly hasText?: boolean;
  readonly parent?: number;
  readonly center?: LatLng;
  readonly shard?: number;
  readonly geometry: readonly Geometry[];
  readonly attrs?: Attrs;
}

/** The kinds a collection declares. Every feature is exactly one. */
export type Kind = "point" | "path" | "area";

/** One ordered group of features. The array's order is load-bearing. */
export interface Collection {
  /** The presentation layer identity; never a position or compatibility hash. */
  readonly id: string | number;
  readonly featureSet?: string;
  readonly style?: string;
  readonly title: string;
  readonly group?: string;
  readonly kind: Kind;
  readonly icon?: string;
  readonly iconAsset?: string;
  readonly iconPicture?: boolean;
  readonly color?: string;
  readonly iconColor?: string;
  readonly visible: boolean;
  readonly labelPolicy?: string;
  readonly renderAs?: string;
  readonly attrs?: Attrs;
  readonly features?: readonly ShapeFeature[];
}

/** `worlds/<slug>.json`. */
export interface WorldPayload {
  readonly grid?: { readonly sourceZoom: number; readonly firstTile: number };
  readonly lenses: readonly Lens[];
  readonly collections: readonly Collection[];
  readonly attrs?: Attrs;
  readonly merged?: readonly unknown[];
}

/** One entry of `worlds/<slug>.text`, fetched when a card opens. */
export interface TextEntry {
  readonly d?: string;
  readonly l?: readonly unknown[];
  readonly a?: Attrs;
}

/** `worlds/<slug>.text`: feature id as a string, to its prose. */
export type TextPayload = Readonly<Record<string, TextEntry>>;

/** The volume's world square and the window its worlds were cut from. */
export interface TileGrid {
  readonly sourceZoom: number;
  /** Native windows may begin at different source tile columns and rows. */
  readonly originX?: number;
  readonly originY?: number;
  /** Legacy square-window origin; native coordinate spaces do not use it. */
  readonly firstTile: number;
  readonly tileSize: number;
  readonly size: number;
  /** The authoritative coordinate-space extent, converted to y-up display coordinates. */
  readonly extent?: readonly [number, number, number, number];
}

/** One world as the manifest lists it. */
export interface WorldEntry {
  readonly slug: string;
  readonly title: string;
  readonly parent?: string;
  readonly iconOutset?: string;
  readonly center: LatLng;
  readonly points: number;
  readonly paths: number;
  readonly areas: number;
  readonly updatedAt: string;
}

/** One volume as `/data/catalog.json` lists it. */
export interface CatalogVolume {
  readonly slug: string;
  readonly title: string;
  readonly stamp: string;
  readonly base: string;
  readonly tileGrid: TileGrid;
  readonly worlds: readonly WorldEntry[];
}

/** `/data/catalog.json`. Composed when asked for, never cached. */
export interface Catalog {
  readonly volumes: readonly CatalogVolume[];
  readonly bundlesDir: string;
}

/** The extension of every tile at zoom `z`: `formats[z − minZoom]`. */
export function tileFormat(lens: Lens, z: number): string | null {
  return lens.formats[z - lens.minZoom] ?? null;
}

/** The native template; the fallback exists only for hand-built test lenses. */
export function tileTemplate(lens: Lens): string {
  return lens.template ?? `tiles/${lens.tiles}/{z}/{x}/{y}.{format}`;
}

/** The label policy an area collection curates, `always` when it says nothing. */
export function labelPolicy(collection: Collection): "always" | "quiet" {
  return (collection.labelPolicy || collection.attrs?.[KEY_LABEL_POLICY]) === "quiet" ? "quiet" : "always";
}

/** How a point collection draws: markers, or floating text. Absent means pin. */
export function renderAs(collection: Collection): "pin" | "text" {
  return (collection.renderAs || collection.attrs?.[KEY_RENDER_AS]) === "text" ? "text" : "pin";
}

/** The ground width of a path collection's features, in world pixels. */
export function strokeWidth(collection: Collection): number {
  const declared = Number(collection.attrs?.[KEY_STROKE_WIDTH_PX]);
  return Number.isFinite(declared) && declared > 0 ? declared : 0;
}
