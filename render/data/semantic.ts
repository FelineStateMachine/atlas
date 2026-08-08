// The semantic vNext graph as the browser reads it.
//
// Nothing in this module knows the v3 document shapes. Stable field identities
// select columns, wire kinds select decoders, and unknown typed properties stay
// attached to the entity that owns them.

import { PackedTable, type Schema } from "./vnext.ts";

export type Position = readonly [number, number];

export interface GeometryPart {
  readonly rings: readonly (readonly Position[])[];
}

export interface Geometry {
  readonly kind: 1 | 2 | 3;
  readonly parts: readonly GeometryPart[];
}

export type PropertyValue = boolean | bigint | number | string | Uint8Array;

export interface Property {
  readonly id: string;
  readonly name: string;
  readonly kind: number;
  readonly value: PropertyValue;
}

export interface Relationship {
  readonly predicate: string;
  readonly target: string;
}

export interface Provenance {
  readonly source: string;
  readonly nativeID: string;
  readonly capturedAt: string;
}

export interface Feature {
  readonly id: string;
  readonly title: string;
  readonly subtitle: string;
  readonly description: string;
  readonly center: Position | null;
  readonly shard: number;
  readonly geometry: Geometry;
  readonly properties: readonly Property[];
  readonly relationships: readonly Relationship[];
  readonly provenance: readonly Provenance[];
}

export interface FeatureSet {
  readonly id: string;
  readonly title: string;
  readonly semanticType: string;
  readonly properties: readonly FieldDefinition[];
  readonly claims: readonly Property[];
  readonly features: readonly Feature[];
}

export interface FieldDefinition {
  readonly id: string;
  readonly name: string;
  readonly kind: number;
  readonly optional: boolean;
}

export interface CoordinateSpace {
  readonly id: string;
  readonly kind: string;
  readonly unit: string;
  readonly definition: string;
  readonly extent: readonly [number, number, number, number];
  readonly sourceZoom: number;
  readonly originX: number;
  readonly originY: number;
  readonly tileSize: number;
  readonly size: number;
}

export interface RasterRect {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

export interface RasterCoverage {
  readonly zoom: number;
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
  readonly bits: Uint8Array;
}

export interface RasterPyramid {
  readonly id: string;
  readonly name: string;
  readonly codec: string;
  readonly tileSize: number;
  readonly minZoom: number;
  readonly maxZoom: number;
  readonly fullZoom: number;
  readonly sourceZoom: number;
  readonly template: string;
  readonly formats: readonly string[];
  readonly bounds: RasterRect | null;
  readonly surface: RasterRect | null;
  readonly interpolate: boolean;
  readonly background: string;
  readonly shard: number;
  readonly coverage: readonly RasterCoverage[];
}

export interface Style {
  readonly id: string;
  readonly symbol: string;
  readonly icon: string;
  readonly iconAsset: string;
  readonly iconPicture: boolean;
  readonly renderAs: string;
  readonly stroke: string;
  readonly fill: string;
}

export interface Layer {
  readonly id: string;
  readonly featureSet: string;
  readonly style: string;
  readonly group: string;
  readonly labelPolicy: string;
  readonly visible: boolean;
  readonly order: number;
  readonly minZoom: number;
  readonly maxZoom: number;
}

export interface LegendEntry {
  readonly layer: string;
  readonly label: string;
  readonly order: number;
}

export interface Presentation {
  readonly id: string;
  readonly title: string;
  readonly styles: readonly Style[];
  readonly layers: readonly Layer[];
  readonly legend: readonly LegendEntry[];
}

export interface World {
  readonly id: string;
  readonly title: string;
  readonly coordinateSpace: CoordinateSpace;
  readonly claims: readonly Property[];
  readonly featureSets: readonly FeatureSet[];
  readonly rasterPyramids: readonly RasterPyramid[];
  readonly presentation: Presentation;
}

export interface Asset {
  readonly id: string;
  readonly mediaType: string;
  readonly path: string;
  readonly provenance: string;
}

export interface Volume {
  readonly id: string;
  readonly title: string;
  readonly worlds: readonly World[];
  readonly assets: ReadonlyMap<string, Asset>;
}

export const TABLE_NAMES = [
  "volumes", "worlds", "coordinate-spaces", "feature-sets",
  "property-definitions", "features", "relationships", "provenance",
  "raster-pyramids", "raster-formats", "raster-coverage", "presentations",
  "styles", "layers", "legend", "assets",
] as const;

export type TableName = typeof TABLE_NAMES[number];
export type NativeTables = ReadonlyMap<TableName, PackedTable>;

/** The browser-side form of format/vnext.CoreID. */
export async function coreID(name: string): Promise<string> {
  const source = new TextEncoder().encode(`dev.atlas.core\0${name}`);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", source)).slice(0, 16);
  digest[6] = ((digest[6] ?? 0) & 0x0f) | 0x50;
  digest[8] = ((digest[8] ?? 0) & 0x3f) | 0x80;
  const plain = [...digest].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${plain.slice(0, 8)}-${plain.slice(8, 12)}-${plain.slice(12, 16)}-${plain.slice(16, 20)}-${plain.slice(20)}`;
}

/** Decode the durable geometry encoding without translating through GeoJSON. */
export function decodeGeometry(bytes: Uint8Array): Geometry {
  if (bytes.byteLength < 5) throw new Error("geometry is shorter than its header");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const kind = bytes[0];
  if (kind !== 1 && kind !== 2 && kind !== 3) throw new Error(`geometry kind ${kind ?? 0} is unsupported`);
  const count = view.getUint32(1, true);
  const parts: GeometryPart[] = [];
  let at = 5;
  for (let part = 0; part < count; part++) {
    const rings = readCount(view, at, "geometry part");
    at += 4;
    const decoded: Position[][] = [];
    for (let ring = 0; ring < rings; ring++) {
      const positions = readCount(view, at, "geometry ring");
      at += 4;
      const length = positions * 16;
      if (at > bytes.byteLength || length > bytes.byteLength - at) throw new Error("geometry positions are truncated");
      const line: Position[] = [];
      for (let position = 0; position < positions; position++) {
        line.push([view.getFloat64(at, true), view.getFloat64(at + 8, true)]);
        at += 16;
      }
      decoded.push(line);
    }
    parts.push({ rings: decoded });
  }
  if (at !== bytes.byteLength) throw new Error("geometry has trailing bytes");
  return { kind, parts };
}

function readCount(view: DataView, at: number, subject: string): number {
  if (at < 0 || at + 4 > view.byteLength) throw new Error(`${subject} is truncated`);
  return view.getUint32(at, true);
}

const CORE_FIELDS = [
  "volume.id", "volume.title",
  "world.id", "world.title", "world.coordinateSpace",
  "coordinate.id", "coordinate.world", "coordinate.kind", "coordinate.unit",
  "coordinate.definition", "coordinate.extent", "coordinate.sourceZoom",
  "coordinate.originX", "coordinate.originY", "coordinate.tileSize", "coordinate.size",
  "featureSet.id", "featureSet.world", "featureSet.title", "featureSet.semanticType",
  "propertyDefinition.featureSet", "propertyDefinition.field", "propertyDefinition.name",
  "propertyDefinition.kind", "propertyDefinition.optional",
  "feature.id", "feature.featureSet", "feature.title", "feature.subtitle",
  "feature.description", "feature.center", "feature.shard", "feature.geometry",
  "relationship.feature", "relationship.predicate", "relationship.target",
  "provenance.feature", "provenance.source", "provenance.nativeID", "provenance.capturedAt",
  "rasterPyramid.id", "rasterPyramid.world", "rasterPyramid.name", "rasterPyramid.codec",
  "rasterPyramid.tileSize", "rasterPyramid.minZoom", "rasterPyramid.maxZoom",
  "rasterPyramid.fullZoom", "rasterPyramid.sourceZoom", "rasterPyramid.template",
  "rasterPyramid.bounds", "rasterPyramid.surface", "rasterPyramid.interpolate",
  "rasterPyramid.background", "rasterPyramid.shard",
  "rasterFormat.raster", "rasterFormat.zoom", "rasterFormat.format",
  "rasterCoverage.raster", "rasterCoverage.zoom", "rasterCoverage.x", "rasterCoverage.y",
  "rasterCoverage.w", "rasterCoverage.h", "rasterCoverage.bits",
  "presentation.id", "presentation.world", "presentation.title",
  "style.id", "style.presentation", "style.symbol", "style.icon", "style.iconAsset",
  "style.iconPicture", "style.renderAs", "style.stroke", "style.fill",
  "layer.id", "layer.presentation", "layer.featureSet", "layer.style", "layer.group",
  "layer.labelPolicy", "layer.visible", "layer.order", "layer.minZoom", "layer.maxZoom",
  "legend.presentation", "legend.layer", "legend.label", "legend.order",
  "asset.id", "asset.mediaType", "asset.path", "asset.provenance",
] as const;

type CoreField = typeof CORE_FIELDS[number];

class NativeTable {
  readonly #table: PackedTable;
  readonly #schema: Schema;
  readonly #ids: ReadonlyMap<string, string>;
  readonly #fields: ReadonlyMap<string, { readonly id: string; readonly name: string; readonly kind: number }>;

  constructor(table: PackedTable, schema: Schema, ids: ReadonlyMap<string, string>) {
    this.#table = table;
    this.#schema = schema;
    this.#ids = ids;
    const type = schema.types.find((candidate) => candidate.id === table.rootType);
    if (!type) throw new Error(`schema does not describe table type ${table.rootType}`);
    this.#fields = new Map(type.fields.map((field) => [field.id, field]));
  }

  get rows(): number { return this.#table.rows; }

  string(name: CoreField, row: number): string {
    const value = this.#column(name).string(row);
    if (value === null) throw new Error(`${name} is null at row ${row}`);
    return value;
  }

  optionalString(name: CoreField, row: number): string {
    return this.#column(name).string(row) ?? "";
  }

  int(name: CoreField, row: number): number {
    const value = this.#column(name).int64(row);
    if (value === null) throw new Error(`${name} is null at row ${row}`);
    return safeNumber(value, name);
  }

  bool(name: CoreField, row: number): boolean {
    const value = this.#column(name).bool(row);
    if (value === null) throw new Error(`${name} is null at row ${row}`);
    return value;
  }

  bytes(name: CoreField, row: number): Uint8Array {
    const value = this.#column(name).bytes(row);
    if (value === null) throw new Error(`${name} is null at row ${row}`);
    return value;
  }

  optionalBytes(name: CoreField, row: number): Uint8Array | null {
    return this.#column(name).bytes(row);
  }

  identifier(name: CoreField, row: number): string {
    const value = this.#column(name).identifier(row);
    if (value === null) throw new Error(`${name} is null at row ${row}`);
    return value;
  }

  extensions(row: number, owner: string): Property[] {
    const core = new Set(CORE_FIELDS.filter((name) => name.startsWith(`${owner}.`)).map((name) => this.#ids.get(name)));
    const properties: Property[] = [];
    for (const column of this.#table.columns) {
      if (core.has(column.id) || column.isNull(row)) continue;
      const field = this.#fields.get(column.id);
      if (!field) throw new Error(`schema does not describe extension field ${column.id}`);
      const value = columnValue(column, row);
      if (value !== null) properties.push({ id: field.id, name: field.name, kind: field.kind, value });
    }
    return properties;
  }

  #column(name: CoreField) {
    const id = this.#ids.get(name);
    if (!id) throw new Error(`browser core schema has no ${name}`);
    const column = this.#table.column(id);
    if (!column) throw new Error(`table ${this.#table.rootType} has no ${name}`);
    return column;
  }
}

interface WorldBuilder {
  id: string;
  title: string;
  coordinateSpace: CoordinateSpace | null;
  claims: Property[];
  featureSets: FeatureSetBuilder[];
  rasterPyramids: RasterBuilder[];
  presentation: PresentationBuilder | null;
}

interface FeatureSetBuilder extends Omit<FeatureSet, "features" | "properties"> {
  features: FeatureBuilder[];
  properties: FieldDefinition[];
}

interface FeatureBuilder extends Omit<Feature, "relationships" | "provenance"> {
  relationships: Relationship[];
  provenance: Provenance[];
}

interface RasterBuilder extends Omit<RasterPyramid, "formats" | "coverage"> {
  formats: string[];
  coverage: RasterCoverage[];
}

interface PresentationBuilder extends Omit<Presentation, "styles" | "layers" | "legend"> {
  styles: Style[];
  layers: Layer[];
  legend: LegendEntry[];
}

/** Reconstruct the semantic graph directly from schema-described packed tables. */
export async function decodeVolume(schema: Schema, tables: NativeTables): Promise<Volume> {
  const ids = new Map<string, string>();
  await Promise.all(CORE_FIELDS.map(async (name) => ids.set(name, await coreID(name))));
  const table = (name: TableName): NativeTable => {
    const held = tables.get(name);
    if (!held) throw new Error(`volume has no data/${name}.pack`);
    return new NativeTable(held, schema, ids);
  };

  const root = table("volumes");
  if (root.rows !== 1) throw new Error(`volume table has ${root.rows} rows`);
  const worlds = decodeWorlds(table);
  const sets = decodeFeatureSets(table, worlds);
  const features = decodeFeatures(table, sets);
  decodeEvidence(table, features);
  decodeRasters(table, worlds);
  decodePresentation(table, worlds);
  const assets = decodeAssets(table);
  const complete = [...worlds.values()].map(completeWorld);
  return { id: root.string("volume.id", 0), title: root.string("volume.title", 0), worlds: complete, assets };
}

function decodeWorlds(table: (name: TableName) => NativeTable): Map<string, WorldBuilder> {
  const source = table("worlds");
  const worlds = new Map<string, WorldBuilder>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("world.id", row);
    worlds.set(id, {
      id, title: source.string("world.title", row), coordinateSpace: null,
      claims: source.extensions(row, "world"), featureSets: [], rasterPyramids: [], presentation: null,
    });
  }
  const coordinates = table("coordinate-spaces");
  for (let row = 0; row < coordinates.rows; row++) {
    const world = requireFrom(worlds, coordinates.string("coordinate.world", row), "coordinate world");
    world.coordinateSpace = {
      id: coordinates.string("coordinate.id", row), kind: coordinates.string("coordinate.kind", row),
      unit: coordinates.string("coordinate.unit", row), definition: coordinates.string("coordinate.definition", row),
      extent: decodeExtent(coordinates.bytes("coordinate.extent", row)),
      sourceZoom: coordinates.int("coordinate.sourceZoom", row),
      originX: coordinates.int("coordinate.originX", row), originY: coordinates.int("coordinate.originY", row),
      tileSize: coordinates.int("coordinate.tileSize", row), size: coordinates.int("coordinate.size", row),
    };
  }
  return worlds;
}

function decodeFeatureSets(
  table: (name: TableName) => NativeTable,
  worlds: ReadonlyMap<string, WorldBuilder>,
): Map<string, FeatureSetBuilder> {
  const source = table("feature-sets");
  const sets = new Map<string, FeatureSetBuilder>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("featureSet.id", row);
    const set: FeatureSetBuilder = {
      id, title: source.string("featureSet.title", row), semanticType: source.string("featureSet.semanticType", row),
      claims: source.extensions(row, "featureSet"), properties: [], features: [],
    };
    requireFrom(worlds, source.string("featureSet.world", row), "feature-set world").featureSets.push(set);
    sets.set(id, set);
  }
  const definitions = table("property-definitions");
  for (let row = 0; row < definitions.rows; row++) {
    requireFrom(sets, definitions.string("propertyDefinition.featureSet", row), "property definition set").properties.push({
      id: definitions.identifier("propertyDefinition.field", row),
      name: definitions.string("propertyDefinition.name", row), kind: definitions.int("propertyDefinition.kind", row),
      optional: definitions.bool("propertyDefinition.optional", row),
    });
  }
  return sets;
}

function decodeFeatures(
  table: (name: TableName) => NativeTable,
  sets: ReadonlyMap<string, FeatureSetBuilder>,
): Map<string, FeatureBuilder> {
  const source = table("features");
  const features = new Map<string, FeatureBuilder>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("feature.id", row);
    const center = source.optionalBytes("feature.center", row);
    const feature: FeatureBuilder = {
      id, title: source.string("feature.title", row), subtitle: source.optionalString("feature.subtitle", row),
      description: source.optionalString("feature.description", row), center: center ? decodePosition(center) : null,
      shard: source.int("feature.shard", row), geometry: decodeGeometry(source.bytes("feature.geometry", row)),
      properties: source.extensions(row, "feature"), relationships: [], provenance: [],
    };
    requireFrom(sets, source.string("feature.featureSet", row), "feature set").features.push(feature);
    features.set(id, feature);
  }
  return features;
}

function decodeEvidence(
  table: (name: TableName) => NativeTable,
  features: ReadonlyMap<string, FeatureBuilder>,
): void {
  const relationships = table("relationships");
  for (let row = 0; row < relationships.rows; row++) {
    requireFrom(features, relationships.string("relationship.feature", row), "relationship feature").relationships.push({
      predicate: relationships.string("relationship.predicate", row), target: relationships.string("relationship.target", row),
    });
  }
  const provenance = table("provenance");
  for (let row = 0; row < provenance.rows; row++) {
    requireFrom(features, provenance.string("provenance.feature", row), "provenance feature").provenance.push({
      source: provenance.string("provenance.source", row), nativeID: provenance.string("provenance.nativeID", row),
      capturedAt: provenance.string("provenance.capturedAt", row),
    });
  }
}

function decodeRasters(
  table: (name: TableName) => NativeTable,
  worlds: ReadonlyMap<string, WorldBuilder>,
): void {
  const source = table("raster-pyramids");
  const rasters = new Map<string, RasterBuilder>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("rasterPyramid.id", row);
    const bounds = source.optionalBytes("rasterPyramid.bounds", row);
    const surface = source.optionalBytes("rasterPyramid.surface", row);
    const raster: RasterBuilder = {
      id, name: source.string("rasterPyramid.name", row), codec: source.string("rasterPyramid.codec", row),
      tileSize: source.int("rasterPyramid.tileSize", row), minZoom: source.int("rasterPyramid.minZoom", row),
      maxZoom: source.int("rasterPyramid.maxZoom", row), fullZoom: source.int("rasterPyramid.fullZoom", row),
      sourceZoom: source.int("rasterPyramid.sourceZoom", row), template: source.string("rasterPyramid.template", row),
      bounds: bounds ? decodeRect(bounds) : null, surface: surface ? decodeRect(surface) : null,
      interpolate: source.bool("rasterPyramid.interpolate", row), background: source.optionalString("rasterPyramid.background", row),
      shard: source.int("rasterPyramid.shard", row), formats: [], coverage: [],
    };
    requireFrom(worlds, source.string("rasterPyramid.world", row), "raster world").rasterPyramids.push(raster);
    rasters.set(id, raster);
  }
  const formats = table("raster-formats");
  const byRaster = new Map<string, Map<number, string>>();
  for (let row = 0; row < formats.rows; row++) {
    const id = formats.string("rasterFormat.raster", row);
    requireFrom(rasters, id, "raster format raster");
    const held = byRaster.get(id) ?? new Map<number, string>();
    held.set(formats.int("rasterFormat.zoom", row), formats.string("rasterFormat.format", row));
    byRaster.set(id, held);
  }
  for (const [id, raster] of rasters) {
    const held = byRaster.get(id) ?? new Map<number, string>();
    for (let zoom = raster.minZoom; zoom <= raster.maxZoom; zoom++) raster.formats.push(held.get(zoom) ?? "");
  }
  const coverage = table("raster-coverage");
  for (let row = 0; row < coverage.rows; row++) {
    requireFrom(rasters, coverage.string("rasterCoverage.raster", row), "raster coverage raster").coverage.push({
      zoom: coverage.int("rasterCoverage.zoom", row), x: coverage.int("rasterCoverage.x", row),
      y: coverage.int("rasterCoverage.y", row), w: coverage.int("rasterCoverage.w", row),
      h: coverage.int("rasterCoverage.h", row), bits: coverage.bytes("rasterCoverage.bits", row),
    });
  }
}

function decodePresentation(
  table: (name: TableName) => NativeTable,
  worlds: ReadonlyMap<string, WorldBuilder>,
): void {
  const source = table("presentations");
  const presentations = new Map<string, PresentationBuilder>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("presentation.id", row);
    const presentation: PresentationBuilder = {
      id, title: source.string("presentation.title", row), styles: [], layers: [], legend: [],
    };
    requireFrom(worlds, source.string("presentation.world", row), "presentation world").presentation = presentation;
    presentations.set(id, presentation);
  }
  const styles = table("styles");
  for (let row = 0; row < styles.rows; row++) {
    requireFrom(presentations, styles.string("style.presentation", row), "style presentation").styles.push({
      id: styles.string("style.id", row), symbol: styles.optionalString("style.symbol", row),
      icon: styles.optionalString("style.icon", row), iconAsset: styles.optionalString("style.iconAsset", row),
      iconPicture: styles.bool("style.iconPicture", row), renderAs: styles.optionalString("style.renderAs", row),
      stroke: styles.optionalString("style.stroke", row), fill: styles.optionalString("style.fill", row),
    });
  }
  const layers = table("layers");
  for (let row = 0; row < layers.rows; row++) {
    requireFrom(presentations, layers.string("layer.presentation", row), "layer presentation").layers.push({
      id: layers.string("layer.id", row), featureSet: layers.string("layer.featureSet", row),
      style: layers.string("layer.style", row), group: layers.optionalString("layer.group", row),
      labelPolicy: layers.optionalString("layer.labelPolicy", row), visible: layers.bool("layer.visible", row),
      order: layers.int("layer.order", row), minZoom: layers.int("layer.minZoom", row), maxZoom: layers.int("layer.maxZoom", row),
    });
  }
  const legend = table("legend");
  for (let row = 0; row < legend.rows; row++) {
    requireFrom(presentations, legend.string("legend.presentation", row), "legend presentation").legend.push({
      layer: legend.string("legend.layer", row), label: legend.string("legend.label", row), order: legend.int("legend.order", row),
    });
  }
  for (const presentation of presentations.values()) {
    presentation.layers.sort((left, right) => left.order - right.order || left.id.localeCompare(right.id));
    presentation.legend.sort((left, right) => left.order - right.order || left.layer.localeCompare(right.layer));
  }
}

function decodeAssets(table: (name: TableName) => NativeTable): ReadonlyMap<string, Asset> {
  const source = table("assets");
  const assets = new Map<string, Asset>();
  for (let row = 0; row < source.rows; row++) {
    const id = source.string("asset.id", row);
    assets.set(id, {
      id, mediaType: source.string("asset.mediaType", row), path: source.string("asset.path", row),
      provenance: source.optionalString("asset.provenance", row),
    });
  }
  return assets;
}

function completeWorld(builder: WorldBuilder): World {
  if (!builder.coordinateSpace) throw new Error(`world ${builder.id} has no coordinate space`);
  if (!builder.presentation) throw new Error(`world ${builder.id} has no presentation`);
  return {
    id: builder.id, title: builder.title, coordinateSpace: builder.coordinateSpace,
    claims: builder.claims, featureSets: builder.featureSets, rasterPyramids: builder.rasterPyramids,
    presentation: builder.presentation,
  };
}

function decodeExtent(bytes: Uint8Array): readonly [number, number, number, number] {
  if (bytes.byteLength !== 32) throw new Error("extent is not four float64 values");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return [view.getFloat64(0, true), view.getFloat64(8, true), view.getFloat64(16, true), view.getFloat64(24, true)];
}

function decodePosition(bytes: Uint8Array): Position {
  if (bytes.byteLength !== 16) throw new Error("position is not two float64 values");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return [view.getFloat64(0, true), view.getFloat64(8, true)];
}

function decodeRect(bytes: Uint8Array): RasterRect {
  const [x, y, width, height] = decodeExtent(bytes);
  return { x, y, width, height };
}

function columnValue(column: ReturnType<PackedTable["column"]> & {}, row: number): PropertyValue | null {
  switch (column.kind) {
    case 1: return column.bool(row);
    case 2: return column.int64(row);
    case 3: return column.float64(row);
    case 4: return column.string(row);
    case 5: return column.bytes(row);
    case 6: return column.identifier(row);
    default: throw new Error(`field ${column.id} has unsupported wire kind ${column.kind}`);
  }
}

function safeNumber(value: bigint, subject: string): number {
  if (value > BigInt(Number.MAX_SAFE_INTEGER) || value < BigInt(Number.MIN_SAFE_INTEGER)) {
    throw new Error(`${subject} exceeds JavaScript's safe integer range`);
  }
  return Number(value);
}

function requireFrom<K, V>(source: ReadonlyMap<K, V>, key: K, subject: string): V {
  const held = source.get(key);
  if (held === undefined) throw new Error(`${subject} ${String(key)} does not exist`);
  return held;
}
