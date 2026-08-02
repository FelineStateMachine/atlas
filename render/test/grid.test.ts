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
import type { FeatureLike } from "ol/Feature.js";
import { cellSystems, gridCellVisual } from "@atlas/analysis";
import type { PlanCell } from "@atlas/analysis";
import { cellMoved, drawGrid } from "../chart/grid.ts";
import { labelFitsCell } from "../chart/styles.ts";
import type { DrawnCell } from "../chart/grid.ts";
import { WorldModel, worldGrid } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import { EMPTY_SCENE } from "../scene/read.ts";
import { LocationTable } from "../data/atlasloc.ts";
import { FIXTURES, locations, payloads, tileGrid } from "./fixtures.ts";
import type { LocationsFixture } from "./fixtures.ts";

const PARITY = join(FIXTURES, "..", "..", "parity");
const PLANS = join(FIXTURES, "..", "..", "analysis", "plans");

/**
 * A page with a ruler on it.
 *
 * The fit gate measures an address in the pitch it will be drawn in, so it
 * needs a canvas; a monospace stub is enough, and it is the honest shape of
 * the thing — every hash at a level is the same length, so a level keeps or
 * drops its labels as one.
 */
(globalThis as unknown as Record<string, unknown>).document = {
  createElement: () => ({
    getContext: () => ({
      font: "",
      measureText(text: string) {
        const size = Number(/(\d+)px/.exec(String(this.font))?.[1] ?? 10);
        return { width: text.length * size * 0.6 };
      },
    }),
  }),
};

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
function draw(model: WorldModel, cell: string): {
  cells: DrawnCell[]; extent: readonly number[] | null;
} {
  const lens = model.payload.lenses[0] ?? null;
  const ground = model.ground(lens);
  const system = cellSystems.require("geohash");
  const standing = new Visibility(model, EMPTY_SCENE, lens?.shard ?? 0, null);
  const chosen = new VectorSource({ wrapX: false });
  const context = new VectorSource({ wrapX: false });
  const drawn = drawGrid(ground, system, cell,
    [...standing.standing()], chosen, context);
  const cells = [...chosen.getFeatures(), ...context.getFeatures()]
    .map((feature) => feature.get("gridCell") as DrawnCell);
  return { cells, extent: drawn.extent };
}

test("the root grid over Mars is the one the tour recorded", () => {
  const recorded = step("mars", "grid-open");
  const drawn = draw(world("mars", "global"), recorded.prefix);
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
  const drawn = draw(world("mars", "global"), recorded.prefix);
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
  drawGrid(ground, system, "m", [], chosen, context);
  const roleOf = (source: VectorSource) => new Set(
    source.getFeatures().map((feature) => (feature.get("gridCell") as DrawnCell).role));
  assert.deepEqual([...roleOf(chosen)].sort(), ["child", "scope"]);
  assert.deepEqual([...roleOf(context)], ["neighbor"]);
});

// ---- the subgrid at the depth cap --------------------------------------
//
// THE DEFECT, as the reader met it: hold a geohash cell two levels down with
// the subdivision on, and the chart draws no children at all — though the cap
// is three, the plan holds all thirty-two of them, and typing a depth-three
// address works.
//
// The plan was never the problem. `golden/analysis/plans/contract.json` pins
// it: the same ground the tour walks, held at "m6", with thirty-two children
// under it. What was wrong is *when* the question "can this cell carry its
// address?" was asked. A child too small for its label draws **nothing**
// (analysis/cellsystems/visual.ts), so that one boolean decides whether the
// subdivision exists — and it was answered once, while the grid was being
// built, against the resolution the camera was *leaving*. Descending flies the
// camera in afterwards; nothing asked again. The deeper the held cell, the
// smaller its children are against the zoom they were judged at, and at two
// levels down every one of them failed.
//
// So the fit gate belongs to the style function, where OpenLayers hands the
// resolution being drawn at. These tests are that, in numbers taken from the
// contract and from the tour.

interface PlanStep {
  step: string;
  system: string;
  cell: string;
  subgridVisible: boolean;
  plan: { hash: string; extent: number[]; role: string }[];
}

function contractStep(name: string): PlanStep {
  const contract = JSON.parse(readFileSync(
    join(PLANS, "contract.json"), "utf8")) as { steps: PlanStep[] };
  const found = contract.steps.find((entry) => entry.step === name);
  assert.ok(found, `the plan contract has a ${name} case`);
  return found;
}

/** Every feature one draw built, chosen path first, in the sources' own order. */
function features(model: WorldModel, cell: string): FeatureLike[] {
  const lens = model.payload.lenses[0] ?? null;
  const chosen = new VectorSource({ wrapX: false });
  const context = new VectorSource({ wrapX: false });
  drawGrid(model.ground(lens), cellSystems.require("geohash"), cell, [], chosen, context);
  return [...chosen.getFeatures(), ...context.getFeatures()];
}

/** Every cell one draw planned, with the plan cell the style will read. */
function planned(model: WorldModel, cell: string): PlanCell[] {
  return features(model, cell).map((feature) => feature.get("gridPlan") as PlanCell);
}

/** What one cell paints at one resolution: null is "nothing at all". */
function paints(cell: PlanCell, resolution: number, subgridVisible: boolean): boolean {
  const model = world("mars", "global");
  const ground = model.ground(model.payload.lenses[0] ?? null);
  return gridCellVisual(ground, cellSystems.require("geohash"), cell, {
    subgridVisible,
    labelled: labelFitsCell(cell.hash, cell.role, cell.extent, resolution),
  }) !== null;
}

test("a cell held two levels down plans every one of its children", () => {
  const recorded = contractStep("geohash-depth-2");
  const cells = planned(world("mars", "global"), recorded.cell);
  assert.deepEqual(
    cells.map((cell) => [cell.hash, cell.role]).sort(),
    recorded.plan.map((cell) => [cell.hash, cell.role]).sort(),
    "the contract's own plan, cell for cell");
  assert.equal(cells.filter((cell) => cell.role === "child").length, 32,
    "thirty-two children, one level above the cap");
  // AND NOTHING ABOUT HOW THEY LOOK IS DECIDED HERE. A visual baked onto the
  // feature is a visual decided at the wrong moment: the camera moves between
  // a grid being built and a reader seeing it, and the answer never changed
  // afterwards.
  for (const feature of features(world("mars", "global"), "m6")) {
    assert.equal(feature.get("gridVisual"), undefined,
      "the tokens are the style function's answer, at the resolution it is handed");
  }
});

test("and draws them, once the camera has landed on the cell it was flown to", () => {
  const recorded = contractStep("geohash-depth-2");
  const cells = planned(world("mars", "global"), recorded.cell);
  const children = cells.filter((cell) => cell.role === "child");
  // The resolution the parent was being read at when the descent was asked
  // for: `grid-descended` in the mars tour, holding "m" one level up. Judged
  // there, a 32-pixel child is 16 pixels across and cannot carry "m60".
  const leaving = 2.007843137254902;
  assert.equal(children.some((cell) => paints(cell, leaving, true)), false,
    "no child fits its address at the zoom the camera is leaving — which is what was drawn");
  // Where the camera actually lands: the held cell, 256 by 128, fitted into
  // the window with its padding, snapped to the pyramid's own ladder.
  const landed = 0.25;
  for (const child of children) {
    assert.equal(paints(child, landed, true), true,
      `child ${child.hash} draws once the camera is over it`);
  }
});

test("the subdivision still answers to the reader's own switch", () => {
  const cells = planned(world("mars", "global"), "m6");
  const child = cells.find((cell) => cell.role === "child");
  const scope = cells.find((cell) => cell.role === "scope");
  assert.ok(child && scope);
  assert.equal(paints(child, 0.25, false), false, "the subgrid put away draws no children");
  assert.equal(paints(scope, 0.25, false), true, "and the held cell keeps its own boundary");
});

test("a cell at the cap is a leaf with nothing under it", () => {
  const recorded = contractStep("geohash-leaf");
  const cells = planned(world("mars", "global"), recorded.cell);
  assert.equal(cells.filter((cell) => cell.role === "leaf").length, 1);
  assert.equal(cells.filter((cell) => cell.role === "child").length, 0,
    "the floor of the telescope divides no further");
  // A leaf is drawn whether or not its address fits: it is the ground the
  // reader asked for, not a subdivision offered to them.
  const leaf = cells.find((cell) => cell.role === "leaf");
  assert.ok(leaf);
  assert.equal(paints(leaf, 64, true), true, "even from far enough out to lose its chip");
});

test("a chip is measured as it will be drawn, not against a round number", () => {
  // Two hashes of different lengths in the same cell: a flat pixel threshold
  // keeps or drops both, and one of them is twice as wide as the other.
  const extent: [number, number, number, number] = [0, 0, 32, 32];
  assert.equal(labelFitsCell("m", "child", extent, 1.2), true);
  assert.equal(labelFitsCell("m6sz1", "child", extent, 1.2), false,
    "a label wider than its cell names the neighbours instead");
});

// ---- cycling the system is not a descent -------------------------------
//
// THE DEFECT: press the cell-system key with a cell held and both panes moved
// the camera. The chart fitted its view and the sphere turned the planet — to
// ground the reader was already looking at, because cycling carries the held
// cell across to the cell of the next system covering the same place. "m" in
// geohash becomes "24" in S2, and read as a string that is a descent.

test("a cell that only changed its spelling is not a cell that moved", () => {
  assert.equal(cellMoved({ system: "geohash", cell: "m" }, { system: "s2", cell: "24" }), false,
    "the same ground under a new name: the camera is already there");
  assert.equal(cellMoved({ system: "s2", cell: "24" }, { system: "geohash", cell: "m" }), false,
    "and back again");
});

test("a descent within one system still moves the camera", () => {
  assert.equal(cellMoved({ system: "geohash", cell: "m" }, { system: "geohash", cell: "m6" }), true);
  assert.equal(cellMoved({ system: "geohash", cell: "m6" }, { system: "geohash", cell: "m" }), true,
    "ascending is a move too: the reader asked for the ground one level out");
  assert.equal(cellMoved({ system: "s2", cell: "24" }, { system: "s2", cell: "241" }), true,
    "including the descent made straight after a cycle");
});

test("redrawing the same grid moves nothing", () => {
  assert.equal(cellMoved({ system: "geohash", cell: "m" }, { system: "geohash", cell: "m" }), false,
    "a filter moving must not drag the reader back to the cell they are in");
  assert.equal(cellMoved({ system: "", cell: "" }, { system: "geohash", cell: "" }), false,
    "and opening a grid over the ground already on screen is not a request to go anywhere");
});
