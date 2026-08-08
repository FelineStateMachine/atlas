export interface FieldSchema {
  readonly id: string;
  readonly name: string;
  readonly kind: number;
  readonly optional?: boolean;
}

export interface TypeSchema {
  readonly id: string;
  readonly name: string;
  readonly fields: readonly FieldSchema[];
}

export interface Schema {
  readonly types: readonly TypeSchema[];
}

export interface FieldProjection {
  readonly known: readonly string[];
  readonly foreign: readonly string[];
}

const HEADER_SIZE = 80;
const DIRECTORY_SIZE = 64;
const MAGIC = "ATLASPK\0";

interface ColumnDirectory {
  readonly id: string;
  readonly kind: number;
  readonly optional: boolean;
  readonly present: Uint8Array;
  readonly payload: Uint8Array;
}

export class PackedColumn {
  readonly id: string;
  readonly kind: number;
  readonly optional: boolean;
  readonly #rows: number;
  readonly #present: Uint8Array;
  readonly #payload: Uint8Array;
  readonly #view: DataView;
  readonly #decoder = new TextDecoder();

  constructor(rows: number, directory: ColumnDirectory) {
    this.id = directory.id;
    this.kind = directory.kind;
    this.optional = directory.optional;
    this.#rows = rows;
    this.#present = directory.present;
    this.#payload = directory.payload;
    this.#view = new DataView(directory.payload.buffer, directory.payload.byteOffset, directory.payload.byteLength);
    this.#validateShape();
  }

  isNull(row: number): boolean {
    this.#checkRow(row);
    const byte = this.#present[Math.floor(row / 8)];
    return byte === undefined || (byte & (1 << (row % 8))) === 0;
  }

  bool(row: number): boolean | null {
    this.#expect(1);
    return this.isNull(row) ? null : this.#payload[row] !== 0;
  }

  int64(row: number): bigint | null {
    this.#expect(2);
    return this.isNull(row) ? null : this.#view.getBigInt64(row * 8, true);
  }

  float64(row: number): number | null {
    this.#expect(3);
    return this.isNull(row) ? null : this.#view.getFloat64(row * 8, true);
  }

  string(row: number): string | null {
    this.#expect(4);
    const value = this.#variable(row);
    return value === null ? null : this.#decoder.decode(value);
  }

  bytes(row: number): Uint8Array | null {
    this.#expect(5);
    return this.#variable(row);
  }

  identifier(row: number): string | null {
    this.#expect(6);
    if (this.isNull(row)) return null;
    return formatID(this.#payload.subarray(row * 16, (row + 1) * 16));
  }

  #variable(row: number): Uint8Array | null {
    if (this.isNull(row)) return null;
    const offsetsLength = (this.#rows + 1) * 4;
    const start = this.#view.getUint32(row * 4, true);
    const end = this.#view.getUint32((row + 1) * 4, true);
    return this.#payload.subarray(offsetsLength + start, offsetsLength + end);
  }

  #validateShape(): void {
    const expected = this.kind === 1 ? this.#rows
      : this.kind === 2 || this.kind === 3 ? this.#rows * 8
      : this.kind === 6 ? this.#rows * 16
      : undefined;
    if (expected !== undefined && this.#payload.byteLength !== expected) throw new Error(`column ${this.id} has an invalid payload length`);
    if (this.kind === 4 || this.kind === 5) {
      const offsetsLength = (this.#rows + 1) * 4;
      if (this.#payload.byteLength < offsetsLength) throw new Error(`column ${this.id} has truncated offsets`);
      let previous = 0;
      const contentLength = this.#payload.byteLength - offsetsLength;
      for (let row = 0; row <= this.#rows; row++) {
        const offset = this.#view.getUint32(row * 4, true);
        if (offset < previous || offset > contentLength) throw new Error(`column ${this.id} has invalid offsets`);
        previous = offset;
      }
    }
  }

  #expect(kind: number): void {
    if (this.kind !== kind) throw new Error(`column ${this.id} has wire kind ${this.kind}, not ${kind}`);
  }

  #checkRow(row: number): void {
    if (!Number.isInteger(row) || row < 0 || row >= this.#rows) throw new RangeError(`row ${row} is outside the table`);
  }
}

export class PackedTable {
  readonly rows: number;
  readonly rootType: string;
  readonly schemaHash: string;
  readonly columns: readonly PackedColumn[];
  readonly #byID: ReadonlyMap<string, PackedColumn>;

  private constructor(rows: number, rootType: string, schemaHash: string, columns: readonly PackedColumn[]) {
    this.rows = rows;
    this.rootType = rootType;
    this.schemaHash = schemaHash;
    this.columns = columns;
    this.#byID = new Map(columns.map((column) => [column.id, column]));
  }

  static over(source: ArrayBuffer | Uint8Array): PackedTable {
	const bytes = source instanceof Uint8Array ? source : new Uint8Array(source);
	const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (bytes.byteLength < HEADER_SIZE || new TextDecoder().decode(bytes.subarray(0, 8)) !== MAGIC) throw new Error("typed block has no Atlas magic");
    if (view.getUint16(8, true) !== 1) throw new Error("typed block framing is unsupported");
    const rows = view.getUint32(12, true);
    const count = view.getUint32(16, true);
    const directoryEnd = HEADER_SIZE + count * DIRECTORY_SIZE;
    if (view.getUint32(68, true) !== DIRECTORY_SIZE || view.getBigUint64(72, true) !== BigInt(directoryEnd) || directoryEnd > bytes.byteLength) {
      throw new Error("typed block directory framing is invalid");
    }
    const columns: PackedColumn[] = [];
    for (let index = 0; index < count; index++) {
      const at = HEADER_SIZE + index * DIRECTORY_SIZE;
      const kind = bytes[at + 16];
      if (kind === undefined || kind < 1 || kind > 6 || bytes[at + 17] !== kind) throw new Error("column has unsupported encoding");
      const present = boundedSlice(bytes, view.getBigUint64(at + 20, true), view.getBigUint64(at + 28, true));
      const payload = boundedSlice(bytes, view.getBigUint64(at + 36, true), view.getBigUint64(at + 44, true));
      if (present.byteLength !== Math.ceil(rows / 8)) throw new Error("column presence bitmap is invalid");
      if (crc32(present, payload) !== view.getUint32(at + 52, true)) throw new Error("column checksum does not match");
      columns.push(new PackedColumn(rows, {
        id: formatID(bytes.subarray(at, at + 16)), kind,
        optional: (view.getUint16(at + 18, true) & 1) !== 0, present, payload,
      }));
    }
    return new PackedTable(rows, formatID(bytes.subarray(52, 68)), hex(bytes.subarray(20, 52)), columns);
  }

  column(id: string): PackedColumn | undefined { return this.#byID.get(normalizeID(id)); }
}

export function mergeSchemas(...schemas: readonly Schema[]): Schema {
  const types = new Map<string, { id: string; name: string; fields: Map<string, FieldSchema> }>();
  for (const schema of schemas) {
    for (const incoming of schema.types) {
      const typeID = normalizeID(incoming.id);
      const current = types.get(typeID) ?? { id: typeID, name: incoming.name, fields: new Map<string, FieldSchema>() };
      if (incoming.name < current.name) current.name = incoming.name;
      for (const field of incoming.fields) {
        const fieldID = normalizeID(field.id);
        const held = current.fields.get(fieldID);
        if (held && held.kind !== field.kind) throw new Error(`field ${fieldID} changes wire kind from ${held.kind} to ${field.kind}`);
        const name = held && held.name < field.name ? held.name : field.name;
        const optional = Boolean(held?.optional || field.optional);
        current.fields.set(fieldID, optional ? { id: fieldID, name, kind: field.kind, optional: true } : { id: fieldID, name, kind: field.kind });
      }
      types.set(typeID, current);
    }
  }
  return {
    types: [...types.values()].sort(byID).map((type) => ({
      id: type.id, name: type.name, fields: [...type.fields.values()].sort(byID),
    })),
  };
}

export function projectFields(table: PackedTable, knownSchema: Schema, fileSchema: Schema): FieldProjection {
  const fileType = fileSchema.types.find((type) => normalizeID(type.id) === table.rootType);
  if (!fileType) throw new Error(`file schema does not describe table type ${table.rootType}`);
  const fileKinds = new Map(fileType.fields.map((field) => [normalizeID(field.id), field.kind]));
  const knownType = knownSchema.types.find((type) => normalizeID(type.id) === table.rootType);
  const knownKinds = new Map((knownType?.fields ?? []).map((field) => [normalizeID(field.id), field.kind]));
  const known: string[] = [];
  const foreign: string[] = [];
  for (const column of table.columns) {
    if (fileKinds.get(column.id) !== column.kind) throw new Error(`file schema disagrees with wire kind for ${column.id}`);
    const expected = knownKinds.get(column.id);
    if (expected === undefined) foreign.push(column.id);
    else if (expected !== column.kind) throw new Error(`known schema disagrees with wire kind for ${column.id}`);
    else known.push(column.id);
  }
  return { known, foreign };
}

function boundedSlice(bytes: Uint8Array, offset64: bigint, length64: bigint): Uint8Array {
  if (offset64 > BigInt(Number.MAX_SAFE_INTEGER) || length64 > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error("column range exceeds JavaScript precision");
  const offset = Number(offset64);
  const length = Number(length64);
  if (offset > bytes.byteLength || length > bytes.byteLength - offset) throw new Error("column range exceeds block");
  return bytes.subarray(offset, offset + length);
}

function normalizeID(id: string): string {
  const plain = id.toLowerCase().replaceAll("-", "");
  if (!/^[0-9a-f]{32}$/.test(plain)) throw new Error(`ID ${id} is not 128 bits`);
  return `${plain.slice(0, 8)}-${plain.slice(8, 12)}-${plain.slice(12, 16)}-${plain.slice(16, 20)}-${plain.slice(20)}`;
}

function formatID(bytes: Uint8Array): string { return normalizeID(hex(bytes)); }
function hex(bytes: Uint8Array): string { return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join(""); }
function byID(left: { readonly id: string }, right: { readonly id: string }): number { return left.id.localeCompare(right.id); }

function crc32(...parts: readonly Uint8Array[]): number {
  let crc = 0xffffffff;
  for (const part of parts) {
    for (const byte of part) {
      crc ^= byte;
      for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}
