import { state } from "./state.js";
import { legendSections } from "./legend.js";

// A map arrives in two pieces: its layers, categories and regions as JSON, and
// its locations packed as parallel arrays. Nothing here is fetched until the
// map is opened, so the catalog can grow without the wait growing with it.
export async function loadMap(entry) {
  const [detailResponse, packedResponse] = await Promise.all([
    fetch(`${state.game.base}/maps/${entry.slug}.json`),
    fetch(`${state.game.base}/maps/${entry.slug}.bin`),
  ]);
  // A refusal here is almost always a stamp that has gone stale: the bundle
  // was replaced between the catalog fetch and this one. The caller refetches
  // the catalog and arrives at the new build's URLs.
  if (!detailResponse.ok || !packedResponse.ok) {
    throw new Error(`map ${entry.slug} is not served under this catalog any more`);
  }
  const [detail, packed] = await Promise.all([
    detailResponse.json(),
    packedResponse.arrayBuffer(),
  ]);
  const categories = detail.groups.flatMap((group) => group.categories);
  unpackLocations(packed, categories);
  return {
    ...entry,
    grid: detail.grid,
    variants: detail.variants,
    groups: detail.groups,
    zones: detail.zones || [],
    sections: legendSections(detail.groups),
  };
}

// The reader of packLocations. Each field is a view straight onto the buffer,
// laid out so no copying or realignment is needed to get at it.
export function unpackLocations(buffer, categories) {
  const view = new DataView(buffer);
  const magic = String.fromCharCode(...new Uint8Array(buffer, 0, 8));
  if (magic !== "ATLASLOC") throw new Error("location payload is not in the expected form");
  const version = view.getUint16(8, true);
  if (version !== 2) throw new Error(`location payload is version ${version}, and this reads 2`);
  const count = view.getUint32(10, true);

  let at = 16;
  const ids = new Int32Array(buffer, at, count);
  const latitudes = new Float32Array(buffer, (at += count * 4), count);
  const longitudes = new Float32Array(buffer, (at += count * 4), count);
  const regions = new Int32Array(buffer, (at += count * 4), count);
  const shards = new Int32Array(buffer, (at += count * 4), count);
  const offsets = new Uint32Array(buffer, (at += count * 4), count + 1);
  const owners = new Uint16Array(buffer, (at += (count + 1) * 4), count);
  const titles = new Uint8Array(buffer, at + count * 2);

  const decoder = new TextDecoder();
  for (const category of categories) category.locations = [];
  for (let index = 0; index < count; index++) {
    categories[owners[index]].locations.push({
      id: ids[index],
      title: decoder.decode(titles.subarray(offsets[index], offsets[index + 1])),
      lat: latitudes[index],
      lng: longitudes[index],
      regionId: regions[index] || undefined,
      shard: shards[index] || undefined,
    });
  }
}

// Descriptions and cross-references are half the catalog by weight and are read
// one pin at a time, so a map's are fetched the first time one of its pins is
// opened, and not at all if none ever is. The cache is keyed by the game's
// stamped base as well as the map, so an updated bundle is never answered
// with the words of the build it replaced.
export async function mapText() {
  const key = `${state.game.base}/${state.map.slug}`;
  if (!state.textByMap.has(key)) {
    state.textByMap.set(
      key,
      fetch(`${state.game.base}/maps/${state.map.slug}.text`).then((r) => r.json()).catch(() => ({})),
    );
  }
  return state.textByMap.get(key);
}
