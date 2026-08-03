// The packed locations, judged against the corpus.
//
// The corpus carries three things that make this a real test rather than a
// round trip against my own opinion: the record list, the packed byte length,
// and the SHA-256 of the packed bytes as the reference implementation wrote
// them. So the test PACKS the corpus records per `docs/format.md` §7, checks
// the bytes it produced against the recorded digest — which pins the layout,
// the alignment, the offsets and the encoding all at once — and only then
// reads them back with the seam's reader and holds every column to the
// record.
//
// If the layout were misread in any way that mattered, the digest would move
// before the reader ever ran.

import { createHash } from "node:crypto";
import test from "node:test";
import { strict as assert } from "node:assert";
import { LocationTable } from "../data/atlasloc.ts";
import { locations, payloads, volumes } from "./fixtures.ts";
import type { LocationsFixture } from "./fixtures.ts";

/** Pack corpus records exactly as format.md §7 lays the bytes out. */
function pack(fixture: LocationsFixture): ArrayBuffer {
  const n = fixture.locations.length;
  const encoder = new TextEncoder();
  const titles = fixture.locations.map((record) => encoder.encode(record.title));
  const titleBytes = titles.reduce((held, run) => held + run.length, 0);
  const buffer = new ArrayBuffer(20 + 26 * n + titleBytes);
  const bytes = new Uint8Array(buffer);
  bytes.set(encoder.encode("ATLASLOC"), 0);
  const header = new DataView(buffer);
  header.setUint16(8, 3, true);
  header.setUint32(10, n, true);

  const id = new Int32Array(buffer, 16, n);
  const lat = new Float32Array(buffer, 16 + 4 * n, n);
  const lng = new Float32Array(buffer, 16 + 8 * n, n);
  const member = new Int32Array(buffer, 16 + 12 * n, n);
  const shard = new Int32Array(buffer, 16 + 16 * n, n);
  const offsets = new Uint32Array(buffer, 16 + 20 * n, n + 1);
  const owner = new Uint16Array(buffer, 20 + 24 * n, n);

  let at = 0;
  fixture.locations.forEach((record, i) => {
    id[i] = record.id;
    lat[i] = record.lat;
    lng[i] = record.lng;
    member[i] = record.member;
    shard[i] = record.shard;
    owner[i] = record.owner;
    offsets[i] = at;
    bytes.set(titles[i] ?? new Uint8Array(), 20 + 26 * n + at);
    at += (titles[i] ?? new Uint8Array()).length;
  });
  offsets[n] = at;
  return buffer;
}

test("the packed layout is the one the corpus was written with", () => {
  let checked = 0;
  for (const slug of volumes()) {
    for (const world of payloads(slug).keys()) {
      const fixture = locations(slug, world);
      const packed = pack(fixture);
      assert.equal(packed.byteLength, fixture.packedBytes,
        `${slug}/${world}: packed length`);
      const digest = createHash("sha256").update(new Uint8Array(packed)).digest("hex");
      assert.equal(digest, fixture.packedSha256, `${slug}/${world}: packed digest`);
      checked++;
    }
  }
  assert.equal(checked, 2, "the city and the planet: every corpus world was checked");
});

test("the reader answers every column the corpus records", () => {
  for (const slug of volumes()) {
    for (const world of payloads(slug).keys()) {
      const fixture = locations(slug, world);
      const table = LocationTable.over(pack(fixture));
      assert.equal(table.count, fixture.count, `${slug}/${world}: count`);
      for (let i = 0; i < table.count; i++) {
        assert.deepEqual(table.at(i), fixture.locations[i], `${slug}/${world}: location ${i}`);
      }
    }
  }
});

test("a payload that is not this format is refused rather than guessed at", () => {
  const stranger = new ArrayBuffer(64);
  assert.throws(() => LocationTable.over(stranger), /magic/);

  const wrongVersion = new ArrayBuffer(64);
  new Uint8Array(wrongVersion).set(new TextEncoder().encode("ATLASLOC"), 0);
  new DataView(wrongVersion).setUint16(8, 2, true);
  assert.throws(() => LocationTable.over(wrongVersion), /version 2/);
});

test("titles are decoded once and are UTF-8, not NUL-terminated", () => {
  const fixture: LocationsFixture = {
    count: 2, packedBytes: 0, packedSha256: "", world: "w",
    locations: [
      { id: 1, owner: 0, lat: 0, lng: 0, member: 0, shard: 0, title: "Cañón — 峰" },
      { id: 2, owner: 1, lat: 0, lng: 0, member: 0, shard: 0, title: "" },
    ],
  };
  const table = LocationTable.over(pack(fixture));
  assert.equal(table.title(0), "Cañón — 峰");
  assert.equal(table.title(0), "Cañón — 峰");
  assert.equal(table.title(1), "");
});
