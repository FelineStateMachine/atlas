import { strict as assert } from "node:assert";
import test from "node:test";
import {
  mergeSchemas, PackedTable, projectFields,
  type FieldSchema, type Schema,
} from "../data/vnext.ts";

const ROOT = "11111111-1111-5111-8111-111111111111";
const TITLE = "22222222-2222-5222-8222-222222222222";
const EMERGENCY = "33333333-3333-5333-8333-333333333333";

test("vNext packed columns open as zero-copy typed views", () => {
  const buffer = fixtureBlock();
	const envelope = new Uint8Array(buffer.byteLength + 13);
	envelope.set(new Uint8Array(buffer), 7);
	const table = PackedTable.over(envelope.subarray(7, 7 + buffer.byteLength));
  assert.equal(table.rows, 2);
  assert.equal(table.rootType, ROOT);
  assert.equal(table.schemaHash, Array.from({ length: 32 }, (_, i) => i.toString(16).padStart(2, "0")).join(""));

  const title = table.column(TITLE);
  const emergency = table.column(EMERGENCY);
  assert.ok(title);
  assert.ok(emergency);
  assert.equal(title.string(0), "Clinic");
  assert.equal(title.isNull(1), true);
  assert.equal(emergency.int64(0), 4n);
  assert.equal(emergency.int64(1), 5n);
});

test("old browser schemas classify rather than discard foreign fields", () => {
  const title: FieldSchema = { id: TITLE, name: "title", kind: 4 };
  const emergency: FieldSchema = { id: EMERGENCY, name: "emergencyLevel", kind: 2, optional: true };
  const base: Schema = { types: [{ id: ROOT, name: "Feature", fields: [title] }] };
  const branch: Schema = { types: [{ id: ROOT, name: "Feature", fields: [emergency] }] };
  const file = mergeSchemas(base, branch);
  const projection = projectFields(PackedTable.over(fixtureBlock()), base, file);
  assert.deepEqual(projection.known, [TITLE]);
  assert.deepEqual(projection.foreign, [EMERGENCY]);
});

test("sideward schema union accepts new IDs and refuses changed meanings", () => {
  const left: Schema = { types: [{ id: ROOT, name: "Feature", fields: [{ id: TITLE, name: "title", kind: 4 }] }] };
  const right: Schema = { types: [{ id: ROOT, name: "Feature", fields: [{ id: EMERGENCY, name: "level", kind: 2 }] }] };
  assert.equal(mergeSchemas(left, right).types[0]?.fields.length, 2);

  const conflict: Schema = { types: [{ id: ROOT, name: "Feature", fields: [{ id: TITLE, name: "title", kind: 2 }] }] };
  assert.throws(() => mergeSchemas(left, conflict), /wire kind/);
});

test("a corrupt column is refused at the browser boundary", () => {
	const buffer = fixtureBlock();
	const bytes = new Uint8Array(buffer);
	bytes[buffer.byteLength - 1] = (bytes[buffer.byteLength - 1] ?? 0) ^ 0xff;
  assert.throws(() => PackedTable.over(buffer), /checksum/);
});

function fixtureBlock(): ArrayBuffer {
  const title = variableColumn([new TextEncoder().encode("Clinic"), null]);
  const emergency = fixedInt64Column([4n, 5n]);
  return block([
    { id: TITLE, kind: 4, optional: true, ...title },
    { id: EMERGENCY, kind: 2, optional: false, ...emergency },
  ]);
}

interface EncodedColumn {
  readonly id: string;
  readonly kind: number;
  readonly optional: boolean;
  readonly present: Uint8Array;
  readonly data: Uint8Array;
}

function block(columns: readonly EncodedColumn[]): ArrayBuffer {
  const header = 80;
  const directory = 64;
  const payloadAt = header + directory * columns.length;
  const length = columns.reduce((sum, column) => sum + column.present.length + column.data.length, payloadAt);
  const buffer = new ArrayBuffer(length);
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);
  bytes.set(new TextEncoder().encode("ATLASPK\0"), 0);
  view.setUint16(8, 1, true);
  view.setUint32(12, 2, true);
  view.setUint32(16, columns.length, true);
  for (let i = 0; i < 32; i++) bytes[20 + i] = i;
  bytes.set(idBytes(ROOT), 52);
  view.setUint32(68, directory, true);
  view.setBigUint64(72, BigInt(payloadAt), true);

  let payload = payloadAt;
  columns.forEach((column, index) => {
    const at = header + index * directory;
    bytes.set(idBytes(column.id), at);
    bytes[at + 16] = column.kind;
    bytes[at + 17] = column.kind;
    view.setUint16(at + 18, column.optional ? 1 : 0, true);
    view.setBigUint64(at + 20, BigInt(payload), true);
    view.setBigUint64(at + 28, BigInt(column.present.length), true);
    bytes.set(column.present, payload);
    payload += column.present.length;
    view.setBigUint64(at + 36, BigInt(payload), true);
    view.setBigUint64(at + 44, BigInt(column.data.length), true);
    bytes.set(column.data, payload);
    payload += column.data.length;
    view.setUint32(at + 52, crc32(column.present, column.data), true);
  });
  return buffer;
}

function variableColumn(values: readonly (Uint8Array | null)[]): Pick<EncodedColumn, "present" | "data"> {
  const present = new Uint8Array([0b01]);
  const offsets = new Uint32Array(values.length + 1);
  const length = values.reduce((sum, value) => sum + (value?.length ?? 0), 0);
  const data = new Uint8Array(offsets.byteLength + length);
  let at = 0;
  values.forEach((value, index) => {
    new DataView(data.buffer).setUint32(index * 4, at, true);
    if (value) {
      data.set(value, offsets.byteLength + at);
      at += value.length;
    }
  });
  new DataView(data.buffer).setUint32(values.length * 4, at, true);
  return { present, data };
}

function fixedInt64Column(values: readonly bigint[]): Pick<EncodedColumn, "present" | "data"> {
  const present = new Uint8Array([0b11]);
  const data = new Uint8Array(values.length * 8);
  values.forEach((value, index) => new DataView(data.buffer).setBigInt64(index * 8, value, true));
  return { present, data };
}

function idBytes(id: string): Uint8Array {
  return Uint8Array.from(id.replaceAll("-", "").match(/../g) ?? [], (pair) => Number.parseInt(pair, 16));
}

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
