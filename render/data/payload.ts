// What `/data` says, as types.
//
// These are `docs/format.md` §4 and §6 written as TypeScript, and nothing
// more: no defaults are applied here, no key is renamed, nothing is dropped.
// A reader is lenient (format.md §2.3) — every optional field really is
// optional, unknown keys ride along untouched, and the only thing this seam
// refuses is a `formatVersion` it does not know, which the catalog never
// carries because the server already refused it.
//
// The seam reads these shapes and nothing else off the wire. Everything the
// application decided — what is hidden, what is highlighted, which lens is
// open — arrives through the scene description instead (`scene/`), because
// data flows one way and the payload is not where a session lives.

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
  readonly name: string;
  readonly tiles: string;
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
  readonly id: number;
  readonly title: string;
  readonly group?: string;
  readonly kind: Kind;
  readonly icon?: string;
  readonly iconAsset?: string;
  readonly iconPicture?: boolean;
  readonly color?: string;
  readonly iconColor?: string;
  readonly visible: boolean;
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
  readonly firstTile: number;
  readonly tileSize: number;
  readonly size: number;
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

/** The label policy an area collection curates, `always` when it says nothing. */
export function labelPolicy(collection: Collection): "always" | "quiet" {
  return collection.attrs?.["atlas.label.policy"] === "quiet" ? "quiet" : "always";
}

/** How a point collection draws: markers, or floating text. Absent means pin. */
export function renderAs(collection: Collection): "pin" | "text" {
  return collection.attrs?.["atlas.render.as"] === "text" ? "text" : "pin";
}

/** The ground width of a path collection's features, in world pixels. */
export function strokeWidth(collection: Collection): number {
  const declared = Number(collection.attrs?.["atlas.stroke.width_px"]);
  return Number.isFinite(declared) && declared > 0 ? declared : 0;
}
