import { state } from "./state.js";
import { legendSections } from "./legend.js";

// A map arrives in two pieces: its layers and collections as JSON, and its
// point features packed as parallel arrays. Nothing here is fetched until the
// map is opened, so the catalog can grow without the wait growing with it.
export async function loadWorld(entry) {
  const [detailResponse, packedResponse] = await Promise.all([
    fetch(`${state.volume.base}/worlds/${entry.slug}.json`),
    fetch(`${state.volume.base}/worlds/${entry.slug}.bin`),
  ]);
  // A refusal here is almost always a stamp that has gone stale: the bundle
  // was replaced between the catalog fetch and this one. The caller refetches
  // the catalog and arrives at the new build's URLs.
  if (!detailResponse.ok || !packedResponse.ok) {
    throw new Error(`world ${entry.slug} is not served under this catalog any more`);
  }
  const [detail, packed] = await Promise.all([
    detailResponse.json(),
    packedResponse.arrayBuffer(),
  ]);
  const collections = detail.collections || [];
  unpackLocations(packed, collections);
  const sections = legendSections(collections);
  return {
    ...entry,
    grid: detail.grid,
    lenses: detail.lenses,
    collections,
    // One index by id, because everything downstream -- the hide set, the
    // label ladder, a zone asking after its own collection -- speaks in ids.
    collectionsById: new Map(collections.map((collection) => [collection.id, collection])),
    attrs: detail.attrs || {},
    merged: detail.merged || [],
    sections,
  };
}

// The reader of packLocations. Each field is a view straight onto the buffer,
// laid out so no copying or realignment is needed to get at it. The owner
// column indexes the collections array; only a point collection may own a
// packed location, and a payload saying otherwise is refused rather than
// guessed at.
export function unpackLocations(buffer, collections) {
  const view = new DataView(buffer);
  const magic = String.fromCharCode(...new Uint8Array(buffer, 0, 8));
  if (magic !== "ATLASLOC") throw new Error("location payload is not in the expected form");
  const version = view.getUint16(8, true);
  if (version !== 3) throw new Error(`location payload is version ${version}, and this reads 3`);
  const count = view.getUint32(10, true);

  let at = 16;
  const ids = new Int32Array(buffer, at, count);
  const latitudes = new Float32Array(buffer, (at += count * 4), count);
  const longitudes = new Float32Array(buffer, (at += count * 4), count);
  const members = new Int32Array(buffer, (at += count * 4), count);
  const shards = new Int32Array(buffer, (at += count * 4), count);
  const offsets = new Uint32Array(buffer, (at += count * 4), count + 1);
  const owners = new Uint16Array(buffer, (at += (count + 1) * 4), count);
  const titles = new Uint8Array(buffer, at + count * 2);

  const decoder = new TextDecoder();
  for (const collection of collections) {
    if (collection.kind === "point") collection.locations = [];
  }
  for (let index = 0; index < count; index++) {
    const owner = collections[owners[index]];
    if (!owner || owner.kind !== "point") {
      throw new Error(`location ${ids[index]} names collection ${owners[index]}, which is no point collection`);
    }
    owner.locations.push({
      id: ids[index],
      title: decoder.decode(titles.subarray(offsets[index], offsets[index + 1])),
      lat: latitudes[index],
      lng: longitudes[index],
      memberId: members[index] || undefined,
      shard: shards[index] || undefined,
    });
  }
}

// Descriptions and cross-references are half the catalog by weight and are read
// one feature at a time, so a map's are fetched the first time one of its
// features is opened, and not at all if none ever is. The cache is keyed by the
// volume's stamped base as well as the map, so an updated bundle is never
// answered with the words of the build it replaced.
export async function worldText() {
  const key = `${state.volume.base}/${state.world.slug}`;
  if (!state.textByWorld.has(key)) {
    state.textByWorld.set(
      key,
      fetch(`${state.volume.base}/worlds/${state.world.slug}.text`).then((r) => r.json()).catch(() => ({})),
    );
  }
  return state.textByWorld.get(key);
}
