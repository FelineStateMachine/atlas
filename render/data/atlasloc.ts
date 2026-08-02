// `ATLASLOC` version 3, read where it landed.
//
// `docs/format.md` §7 is the whole specification of these bytes: parallel
// little-endian typed arrays, laid out so a reader can build views directly
// over the downloaded buffer with no parsing and no per-feature allocation.
// This module is that reader, and it keeps the promise the layout was
// designed for — nothing here copies a column.
//
// Titles are the one exception, and only on demand: a UTF-8 run is decoded
// when someone asks for it, and the decoded string is remembered, because a
// world's titles are read by the search, the dock and every label, and
// decoding two thousand of them twice is work nobody asked for.
//
// The reader is deliberately strict about three things and lenient about
// everything else: the magic, the version, and the title offsets. Those are
// the only values in the payload that can point outside it.

const MAGIC = "ATLASLOC";
const VERSION = 3;
const HEADER = 16;

/** One packed point feature, materialised only when something asks. */
export interface PackedLocation {
  readonly id: number;
  readonly owner: number;
  readonly lat: number;
  readonly lng: number;
  readonly member: number;
  readonly shard: number;
  readonly title: string;
}

/**
 * Views over a downloaded `worlds/<slug>.bin`.
 *
 * The columns are public because everything that draws wants them as arrays,
 * not as objects: a hit test walks `lat`/`lng` without touching a title, and
 * a filter walks `owner` without touching anything else.
 */
export class LocationTable {
  readonly count: number;
  readonly id: Int32Array;
  readonly lat: Float32Array;
  readonly lng: Float32Array;
  readonly member: Int32Array;
  readonly shard: Int32Array;
  readonly owner: Uint16Array;

  private readonly titleOffsets: Uint32Array;
  private readonly titleBytes: Uint8Array;
  private readonly titles: (string | undefined)[];
  private static readonly decoder = new TextDecoder();

  private constructor(buffer: ArrayBuffer) {
    const bytes = new Uint8Array(buffer);
    if (bytes.byteLength < HEADER) {
      throw new Error("ATLASLOC: the payload is shorter than its own header");
    }
    const magic = LocationTable.decoder.decode(bytes.subarray(0, 8));
    if (magic !== MAGIC) {
      throw new Error(`ATLASLOC: magic is ${JSON.stringify(magic)}, not ${MAGIC}`);
    }
    const header = new DataView(buffer);
    const version = header.getUint16(8, true);
    if (version !== VERSION) {
      // A reader refuses a version it does not know (format.md §7.3). Guessing
      // at a layout is worse than saying so.
      throw new Error(`ATLASLOC: version ${version} is not version ${VERSION}`);
    }
    const n = header.getUint32(10, true);
    this.count = n;

    // Every offset below is format.md §7's table, in its own order. The two
    // reserved bytes at 14 exist so each column that follows is four-byte
    // aligned, which is what makes these views legal at all.
    this.id = new Int32Array(buffer, HEADER, n);
    this.lat = new Float32Array(buffer, HEADER + 4 * n, n);
    this.lng = new Float32Array(buffer, HEADER + 8 * n, n);
    this.member = new Int32Array(buffer, HEADER + 12 * n, n);
    this.shard = new Int32Array(buffer, HEADER + 16 * n, n);
    this.titleOffsets = new Uint32Array(buffer, HEADER + 20 * n, n + 1);
    this.owner = new Uint16Array(buffer, 20 + 24 * n, n);

    const start = 20 + 26 * n;
    const end = this.titleOffsets[n] ?? 0;
    if (start + end > bytes.byteLength) {
      throw new Error("ATLASLOC: the title region runs past the end of the payload");
    }
    this.titleBytes = bytes.subarray(start, start + end);
    this.titles = new Array<string | undefined>(n);
  }

  /** Build the views. Throws when the bytes are not this format. */
  static over(buffer: ArrayBuffer): LocationTable {
    return new LocationTable(buffer);
  }

  /** Location `i`'s title, decoded once and kept. */
  title(i: number): string {
    const held = this.titles[i];
    if (held !== undefined) return held;
    const from = this.titleOffsets[i] ?? 0;
    const to = this.titleOffsets[i + 1] ?? from;
    // Non-decreasing offsets are the producer's promise; a payload that broke
    // it gets an empty title rather than a thrown page.
    const decoded = to > from
      ? LocationTable.decoder.decode(this.titleBytes.subarray(from, to))
      : "";
    this.titles[i] = decoded;
    return decoded;
  }

  /** One location as a record. Allocates: for cards and picks, not for draws. */
  at(i: number): PackedLocation {
    if (i < 0 || i >= this.count) throw new RangeError(`ATLASLOC: no location ${i}`);
    return {
      id: this.id[i] ?? 0,
      owner: this.owner[i] ?? 0,
      lat: this.lat[i] ?? 0,
      lng: this.lng[i] ?? 0,
      member: this.member[i] ?? 0,
      shard: this.shard[i] ?? 0,
      title: this.title(i),
    };
  }

  /** Every location, in the packed payload's own order. */
  *all(): Generator<PackedLocation> {
    for (let i = 0; i < this.count; i++) yield this.at(i);
  }
}
