// The grid the chart draws, judged against a recorded parity step.
//
// `golden/parity/<slug>/tour.json` records, for every step, the cells the
// chart held: their ids, their extents, their roles, their context distance
// and how many pins each holds. That is the analysis lane's plan as a
// renderer actually consumed it, so reproducing a step is a check on four
// things at once — the ground the systems are handed, the plan's frozen
// emission order, the split between the chosen path and the dimmed context,
// and the containment test the counts come out of.
//
// This runs entirely offline: the world's payload and packed locations come
// from `golden/fixtures/`, and the expected cells come from the tour.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { strict as assert } from "node:assert";
import VectorSource from "ol/source/Vector.js";
import { cellSystems } from "@atlas/analysis";
import { drawGrid } from "../chart/grid.ts";
import type { DrawnCell } from "../chart/grid.ts";
import { WorldModel, worldGrid } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import { EMPTY_SCENE } from "../scene/read.ts";
import { LocationTable } from "../data/atlasloc.ts";
import { FIXTURES, locations, payloads, tileGrid } from "./fixtures.ts";
import type { LocationsFixture } from "./fixtures.ts";

const PARITY = join(FIXTURES, "..", "..", "parity");

interface Step {
  name: string;
  snapshot: { grid: { prefix: string; extent: number[] | null; cells: DrawnCell[] } };
}

function step(slug: string, name: string): Step["snapshot"]["grid"] {
  const tour = JSON.parse(
    readFileSync(join(PARITY, slug, "tour.json"), "utf8")) as { steps: Step[] };
  const found = tour.steps.find((entry) => entry.name === name);
  assert.ok(found, `${slug} has a ${name} step`);
  return found.snapshot.grid;
}

/** Pack a fixture's records so the model can be built from them. */
function table(fixture: LocationsFixture): LocationTable {
  const encoder = new TextEncoder();
  const n = fixture.locations.length;
  const titles = fixture.locations.map((record) => encoder.encode(record.title));
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
  const member = new Int32Array(buffer, 16 + 12 * n, n);
  const shard = new Int32Array(buffer, 16 + 16 * n, n);
  const offsets = new Uint32Array(buffer, 16 + 20 * n, n + 1);
  const owner = new Uint16Array(buffer, 20 + 24 * n, n);
  let at = 0;
  fixture.locations.forEach((record, i) => {
    id[i] = record.id; lat[i] = record.lat; lng[i] = record.lng;
    member[i] = record.member; shard[i] = record.shard; owner[i] = record.owner;
    offsets[i] = at;
    bytes.set(titles[i] ?? new Uint8Array(), 20 + 26 * n + at);
    at += (titles[i] ?? new Uint8Array()).length;
  });
  offsets[n] = at;
  return LocationTable.over(buffer);
}

function world(slug: string, name: string): WorldModel {
  const payload = payloads(slug).get(name);
  assert.ok(payload, `${slug}/${name} has a payload`);
  // The world's own window overrides the manifest's, which is the whole
  // difference between a coordinate on this world and one 850,000 pixels away.
  return new WorldModel(name, payload, worldGrid(tileGrid(slug), payload),
    table(locations(slug, name)));
}

/**
 * As the snapshot would carry it.
 *
 * A tour snapshot is JSON, and JSON has one zero: the analysis lane's
 * surface extent legitimately produces `-0` for the top edge of a lens
 * anchored at y = 0 (docs/analysis.md), which serializes as `0`. Comparing
 * through the serializer is comparing what a parity run would actually diff.
 */
function asRecorded<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

/** Draw one grid the way the chart does, and answer what it held. */
function draw(model: WorldModel, cell: string, subgrid: boolean): {
  cells: DrawnCell[]; extent: readonly number[] | null;
} {
  const lens = model.payload.lenses[0] ?? null;
  const ground = model.ground(lens);
  const system = cellSystems.require("geohash");
  const standing = new Visibility(model, EMPTY_SCENE, lens?.shard ?? 0, null);
  const chosen = new VectorSource({ wrapX: false });
  const context = new VectorSource({ wrapX: false });
  // The resolution a whole world sits at in a window about nine hundred
  // pixels wide, which is what the recorded tours were taken in.
  const drawn = drawGrid(ground, system, cell, subgrid, 9,
    [...standing.standing()], chosen, context);
  const cells = [...chosen.getFeatures(), ...context.getFeatures()]
    .map((feature) => feature.get("gridCell") as DrawnCell);
  return { cells, extent: drawn.extent };
}

test("the root grid over Mars is the one the tour recorded", () => {
  const recorded = step("mars", "grid-open");
  const drawn = draw(world("mars", "global"), recorded.prefix, true);
  assert.deepEqual(asRecorded(drawn.extent), recorded.extent, "the ground the system divides");
  assert.deepEqual(
    drawn.cells.map((cell) => [cell.hash, cell.role, cell.contextDistance]).sort(),
    recorded.cells.map((cell) => [cell.hash, cell.role, cell.contextDistance]).sort(),
    "the cells the grid holds");
  for (const cell of recorded.cells) {
    const mine = drawn.cells.find((candidate) => candidate.hash === cell.hash);
    assert.ok(mine, `cell ${cell.hash} was drawn`);
    assert.deepEqual(asRecorded([...mine.extent]), cell.extent, `cell ${cell.hash}: extent`);
    assert.equal(mine.count, cell.count, `cell ${cell.hash}: the pins it holds`);
  }
});

test("descending into a cell holds its neighbours as context", () => {
  const recorded = step("mars", "grid-descended");
  const drawn = draw(world("mars", "global"), recorded.prefix, true);
  assert.deepEqual(asRecorded(drawn.extent), recorded.extent, "the held cell is the ground now");
  const roles = (cells: DrawnCell[]) => {
    const held: Record<string, number> = {};
    for (const cell of cells) held[cell.role] = (held[cell.role] ?? 0) + 1;
    return held;
  };
  assert.deepEqual(roles(drawn.cells), roles(recorded.cells), "how many cells of each role");
  for (const cell of recorded.cells) {
    const mine = drawn.cells.find((candidate) => candidate.hash === cell.hash);
    assert.ok(mine, `cell ${cell.hash} was drawn`);
    assert.equal(mine.role, cell.role, `cell ${cell.hash}: role`);
    assert.equal(mine.contextDistance, cell.contextDistance, `cell ${cell.hash}: distance`);
    assert.equal(mine.count, cell.count, `cell ${cell.hash}: the pins it holds`);
  }
});

test("the chosen path draws under the pins and the context over them", () => {
  const model = world("mars", "global");
  const lens = model.payload.lenses[0] ?? null;
  const ground = model.ground(lens);
  const system = cellSystems.require("geohash");
  const chosen = new VectorSource({ wrapX: false });
  const context = new VectorSource({ wrapX: false });
  drawGrid(ground, system, "m", true, 9, [], chosen, context);
  const roleOf = (source: VectorSource) => new Set(
    source.getFeatures().map((feature) => (feature.get("gridCell") as DrawnCell).role));
  assert.deepEqual([...roleOf(chosen)].sort(), ["child", "scope"]);
  assert.deepEqual([...roleOf(context)], ["neighbor"]);
});
