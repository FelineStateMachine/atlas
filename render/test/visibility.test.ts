// What stands, and why: the rules of `world/visibility.ts` over a small world.
//
// This is the seam's half of the count three surfaces have to agree on, so
// the tests are written as the sentences the rules are: a hidden collection
// takes its pins off the map, a search takes everything it does not name, a
// highlight conjoins across collections and unites within one, a held cell
// narrows the same way — and the selected and the searched are spared by both
// of the last two, because the card open about a feature must not lose it.

import test from "node:test";
import { strict as assert } from "node:assert";
import { EMPTY_SCENE } from "../scene/read.ts";
import type { Scene } from "../scene/read.ts";
import { WorldModel, project } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import type { Collection, TileGrid, WorldPayload } from "../data/payload.ts";
import { LocationTable } from "../data/atlasloc.ts";

const grid: TileGrid = { sourceZoom: 5, firstTile: 0, tileSize: 256, size: 8192 };

/** Two point collections, one area collection with two areas side by side. */
function payload(): WorldPayload {
  // GeoJSON pairs are [lng, lat], in the volume's own world space: a square
  // anchored at its north-west corner and running east and south.
  const square = (lng: number, lat: number, size: number) => [[
    [lng, lat], [lng + size, lat], [lng + size, lat - size], [lng, lat - size], [lng, lat],
  ]];
  const collections: Collection[] = [
    { id: 1, title: "Shrines", kind: "point", visible: true },
    { id: 2, title: "Chests", kind: "point", visible: true },
    {
      id: 3, title: "Regions", kind: "area", visible: true,
      features: [
        { id: 100, title: "West", geometry: [{ type: "Polygon", coordinates: square(0, 90, 90) }] },
        { id: 101, title: "East", geometry: [{ type: "Polygon", coordinates: square(90, 90, 90) }] },
      ],
    },
    {
      id: 4, title: "Districts", kind: "area", visible: true,
      features: [
        { id: 200, title: "North", geometry: [{ type: "Polygon", coordinates: square(0, 90, 180) }] },
      ],
    },
  ];
  return { lenses: [], collections };
}

/** Positions are lat/lng in the world's own space; these are far apart. */
const points = [
  { id: 1, owner: 0, lat: 40, lng: 10, member: 0, shard: 0, title: "Alpha" },
  { id: 2, owner: 0, lat: 40, lng: 120, member: 0, shard: 0, title: "Beta" },
  { id: 3, owner: 1, lat: 10, lng: 10, member: 0, shard: 1, title: "Gamma" },
  { id: 4, owner: 1, lat: 10, lng: 120, member: 0, shard: 2, title: "Alpha Two" },
];

function table(): LocationTable {
  const encoder = new TextEncoder();
  const n = points.length;
  const titles = points.map((point) => encoder.encode(point.title));
  const total = titles.reduce((held, run) => held + run.length, 0);
  const buffer = new ArrayBuffer(20 + 26 * n + total);
  const bytes = new Uint8Array(buffer);
  bytes.set(encoder.encode("ATLASLOC"), 0);
  const header = new DataView(buffer);
  header.setUint16(8, 3, true);
  header.setUint32(10, n, true);
  const id = new Int32Array(buffer, 16, n);
  const lat = new Float32Array(buffer, 16 + 4 * n, n);
  const lng = new Float32Array(buffer, 16 + 8 * n, n);
  const shard = new Int32Array(buffer, 16 + 16 * n, n);
  const offsets = new Uint32Array(buffer, 16 + 20 * n, n + 1);
  const owner = new Uint16Array(buffer, 20 + 24 * n, n);
  let at = 0;
  points.forEach((point, i) => {
    id[i] = point.id; lat[i] = point.lat; lng[i] = point.lng;
    shard[i] = point.shard; owner[i] = point.owner; offsets[i] = at;
    bytes.set(titles[i] ?? new Uint8Array(), 20 + 26 * n + at);
    at += (titles[i] ?? new Uint8Array()).length;
  });
  offsets[n] = at;
  return LocationTable.over(buffer);
}

const model = new WorldModel("w", payload(), grid, table());

function scene(over: Partial<Scene> = {}): Scene {
  return { ...EMPTY_SCENE, ...over };
}

test("with nothing filtered, everything stands", () => {
  const standing = new Visibility(model, scene(), 0, null);
  assert.equal(standing.eligible, 4);
  assert.equal(standing.drawn, 4 + 3, "pins and shapes are one count");
  assert.equal(standing.listable, 7);
});

test("a hidden collection takes its own off the map and out of the count", () => {
  const standing = new Visibility(model, scene({ hidden: new Set(["1"]) }), 0, null);
  assert.equal(standing.eligible, 2);
  assert.deepEqual([...standing.standing()].map((point) => point.title), ["Gamma", "Alpha Two"]);

  const shapes = new Visibility(model, scene({ hidden: new Set(["3"]) }), 0, null);
  assert.equal(shapes.shapesShown.length, 1, "a hidden area collection is not drawn");
});

test("a search leaves what it names, and promotes it", () => {
  const standing = new Visibility(model, scene({ search: "alpha" }), 0, null);
  assert.deepEqual([...standing.standing()].map((point) => point.title), ["Alpha", "Alpha Two"]);
  assert.equal(standing.at(0).promoted, true);
  assert.equal(standing.at(1).promoted, false, "a filtered pin is not promoted");
});

test("a split world offers one layer at a time", () => {
  // Shard 1 admits the unsharded pins and its own; shard 2's belongs
  // elsewhere in the world rather than being merely filtered out.
  const standing = new Visibility(model, scene(), 1, null);
  assert.deepEqual([...standing.standing()].map((point) => point.title),
    ["Alpha", "Beta", "Gamma"]);
});

test("highlights conjoin across collections and unite within one", () => {
  // West holds Alpha and Gamma; North holds all four. Highlighting West alone
  // leaves the two inside it.
  const west = new Visibility(model, scene({ highlighted: new Set(["100"]) }), 0, null);
  assert.deepEqual([...west.standing()].map((point) => point.title), ["Alpha", "Gamma"]);

  // West and East together are alternatives within one collection: their
  // union stands.
  const both = new Visibility(model, scene({ highlighted: new Set(["100", "101"]) }), 0, null);
  assert.equal(both.eligible, 4);

  // West and North are two collections, so they are conditions: the pins
  // inside both. North contains everything, so the answer is West's.
  const across = new Visibility(model, scene({ highlighted: new Set(["100", "200"]) }), 0, null);
  assert.deepEqual([...across.standing()].map((point) => point.title), ["Alpha", "Gamma"]);
  assert.equal(across.focusedPins, 2);
});

test("the selected and the searched are spared a cull, and nothing else is", () => {
  const spared = new Visibility(
    model, scene({ highlighted: new Set(["100"]), selected: "2" }), 0, null);
  assert.ok([...spared.standing()].some((point) => point.title === "Beta"),
    "the card open about a feature does not lose it");

  const searched = new Visibility(
    model, scene({ highlighted: new Set(["100"]), search: "beta" }), 0, null);
  assert.deepEqual([...searched.standing()].map((point) => point.title), ["Beta"]);
});

test("a held cell narrows exactly the way a highlight does", () => {
  const west = project(grid, 0, 0)[0];
  const middle = project(grid, 0, 90)[0];
  const inCell = (at: readonly [number, number]) => at[0] >= west && at[0] < middle;
  const standing = new Visibility(model, scene({ gridCell: "9q" }), 0, inCell);
  assert.deepEqual([...standing.standing()].map((point) => point.title), ["Alpha", "Gamma"]);
  assert.equal(standing.priorityPins, 2, "what the cell holds is what it counts");
  assert.equal(standing.at(0).promoted, true, "a pin in the held cell is promoted");
});

test("a pin's place in the crowd is stable and rarity-first", () => {
  const shrines = model.points.filter((point) => point.collection.id === 1);
  const chests = model.points.filter((point) => point.collection.id === 2);
  // Equal-sized collections rank equally by rarity, so the tie-break is the
  // id's own hash — and it is the same on every run.
  assert.equal(shrines.length, 2);
  assert.equal(chests.length, 2);
  const again = new WorldModel("w", payload(), grid, table());
  assert.deepEqual(again.points.map((point) => point.priority),
    model.points.map((point) => point.priority));
});
