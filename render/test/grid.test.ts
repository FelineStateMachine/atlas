// The grid the chart draws, judged against the documented halving.
//
// Geohash divides a ground by alternating halvings, five bits to a
// character, most significant bit first, x first — and the axis does NOT
// reset between characters, so an odd character splits x three times and y
// twice, and an even character does the opposite. That is the whole of the
// arithmetic, so every extent below is computed from it by hand and inlined:
// nothing here reads a recording, and a change to the halving moves these
// numbers before it moves anything a reader sees.
//
// THE HAND DERIVATION, used throughout and spot-checked against literals:
// for an odd-position character of ordinal v (bits b16 b8 b4 b2 b1), the
// splits land x, y, x, y, x — so column = 4·b16 + 2·b4 + b1 of eight columns
// and row = 2·b8 + b2 of four rows, rows counted from the extent's bottom
// (a set bit keeps the upper half, and "up" in OL world coordinates is
// toward 0). An even-position character starts on y: column = 2·b8 + b2 of
// four, row = 4·b16 + 2·b4 + b1 of eight.
//
// The ground is real: mars, whose lens declares an 8192 × 4096 surface, so
// its level-1 cells are 1024 × 1024 — and bend-or, a plane city on the whole
// 8192 square, whose level-1 cells are 1024 × 2048. The pin counts are the
// corpus's own locations, recounted here by an independent sweep against the
// hand-derived boxes.

import test from "node:test";
import { strict as assert } from "node:assert";
import VectorSource from "ol/source/Vector.js";
import type { FeatureLike } from "ol/Feature.js";
import { cellSystems, gridCellVisual } from "@atlas/analysis";
import type { Extent, PlanCell } from "@atlas/analysis";
import { cellMoved, drawGrid, planCells } from "../chart/grid.ts";
import { labelFitsCell } from "../chart/styles.ts";
import type { DrawnCell } from "../chart/grid.ts";
import { WorldModel, worldGrid } from "../world/model.ts";
import type { PointRecord } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import { EMPTY_SCENE } from "../scene/read.ts";
import { LocationTable } from "../data/atlasloc.ts";
import { locations, payloads, tileGrid } from "./fixtures.ts";
import type { LocationsFixture } from "./fixtures.ts";

/** The alphabet, spelled out by hand: the child order and the plan order. */
const ALPHABET = "0123456789bcdefghjkmnpqrstuvwxyz";

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
 * As a diagnostics snapshot would carry it.
 *
 * A snapshot is JSON, and JSON has one zero: the analysis lane's surface
 * extent legitimately produces `-0` for the top edge of a lens anchored at
 * y = 0 (docs/analysis.md), which serializes as `0`. Comparing through the
 * serializer is comparing what a reader of the snapshot would actually see.
 */
function asRecorded<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

/** The hand derivation from the header, one character at a time. */
function handCell(surface: Extent, hash: string): Extent {
  let [minX, minY, maxX, maxY] = surface;
  for (let depth = 0; depth < hash.length; depth++) {
    const v = ALPHABET.indexOf(hash[depth] ?? "");
    const odd = depth % 2 === 0;
    const column = odd ? ((v >> 4) & 1) * 4 + ((v >> 2) & 1) * 2 + (v & 1) : ((v >> 3) & 1) * 2 + ((v >> 1) & 1);
    const row = odd ? ((v >> 3) & 1) * 2 + ((v >> 1) & 1) : ((v >> 4) & 1) * 4 + ((v >> 2) & 1) * 2 + (v & 1);
    const columns = odd ? 8 : 4;
    const rows = odd ? 4 : 8;
    const width = (maxX - minX) / columns;
    const height = (maxY - minY) / rows;
    minX += width * column;
    maxX = minX + width;
    minY += height * row;
    maxY = minY + height;
  }
  return [minX, minY, maxX, maxY];
}

test("the hand derivation lands on the numbers worked out on paper", () => {
  const mars: Extent = [0, -4096, 8192, 0];
  // "0" is every bit clear: the bottom-left corner. "m" is ordinal 19,
  // binary 10011 — column 4+0+1 = 5, row 0+1 = 1. "z" is 31: the top-right.
  assert.deepEqual(handCell(mars, "0"), [0, -4096, 1024, -3072]);
  assert.deepEqual(handCell(mars, "m"), [5120, -3072, 6144, -2048]);
  assert.deepEqual(handCell(mars, "z"), [7168, -1024, 8192, 0]);
  // "6" in second position starts on y: ordinal 6, binary 00110 — column
  // 2·0 + 1 = 1 of four 256-wide columns, row 0 + 2·1 + 0 = 2 of eight
  // 128-tall rows. A depth-2 cell is 256 × 128, not 128 × 256.
  assert.deepEqual(handCell(mars, "m6"), [5376, -2816, 5632, -2688]);
  // And the third character is back on x: 32 × 32.
  assert.deepEqual(handCell(mars, "m60"), [5376, -2816, 5408, -2784]);
  assert.deepEqual(handCell(mars, "m6s"), [5504, -2752, 5536, -2720]);
});

/** The independent recount: standing pins inside a hand-derived box. */
function sweep(points: readonly PointRecord[], extent: Extent): number {
  // Closed intervals on every edge, exactly as the containment rule reads: a
  // pin on a boundary belongs to both cells it touches.
  return points.filter(({ coordinate: [x, y] }) =>
    x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3]).length;
}

/** Draw one grid the way the chart does, and answer what it held. */
function draw(model: WorldModel, cell: string): {
  cells: DrawnCell[]; extent: readonly number[] | null; standing: PointRecord[];
} {
  const lens = model.payload.lenses[0] ?? null;
  const ground = model.ground(lens);
  const system = cellSystems.require("geohash");
  const standing = [...new Visibility(model, EMPTY_SCENE, lens?.shard ?? 0, null).standing()];
  const chosen = new VectorSource({ wrapX: false });
  const context = new VectorSource({ wrapX: false });
  const drawn = drawGrid(ground, system, cell, standing, chosen, context);
  // `drawn.cells` is the plan's own order; the sources hand features back in
  // whatever order their index likes, so the two are checked against each
  // other as sets and the ordering claims are made of the draw's account.
  const held = [...chosen.getFeatures(), ...context.getFeatures()]
    .map((feature) => (feature.get("gridCell") as DrawnCell).hash).sort();
  assert.deepEqual(held, drawn.cells.map((cell) => cell.hash).sort(),
    "every planned cell became a feature on one of the two sources");
  return { cells: drawn.cells, extent: drawn.extent, standing };
}

test("the root grid over Mars is the thirty-two children of the lens's surface", () => {
  const surface: Extent = [0, -4096, 8192, 0];
  const { cells, extent, standing } = draw(world("mars", "global"), "");
  assert.deepEqual(asRecorded(extent), [...surface], "the ground the system divides");
  assert.equal(standing.length, 2048, "every pin the corpus world holds is standing");
  assert.deepEqual(
    cells.map((cell) => [cell.hash, cell.role, cell.contextDistance]),
    [...ALPHABET].map((character) => [character, "child", 0]),
    "one child per character, in the alphabet's own order, and nothing else");
  for (const cell of cells) {
    assert.deepEqual(asRecorded([...cell.extent]), asRecorded([...handCell(surface, cell.hash)]),
      `cell ${cell.hash}: the hand-derived box`);
    assert.equal(cell.count, sweep(standing, handCell(surface, cell.hash)),
      `cell ${cell.hash}: the pins it holds, recounted independently`);
  }
  // Two anchors read straight off the corpus, so the sweep itself is held to
  // something: of the 2,048 Trek locations, 26 stand in "0" and 95 in "m".
  assert.equal(cells.find((cell) => cell.hash === "0")?.count, 26);
  assert.equal(cells.find((cell) => cell.hash === "m")?.count, 95);
});

test("descending into a cell holds its neighbours as context", () => {
  const surface: Extent = [0, -4096, 8192, 0];
  const { cells, extent, standing } = draw(world("mars", "global"), "m");
  assert.deepEqual(asRecorded(extent), [5120, -3072, 6144, -2048],
    "the held cell is the ground now");
  const roles: Record<string, number> = {};
  for (const cell of cells) roles[cell.role] = (roles[cell.role] ?? 0) + 1;
  assert.deepEqual(roles, { child: 32, scope: 1, neighbor: 31 },
    "the subdivision, the held boundary, and every sibling dimmed around it");
  for (const cell of cells) {
    assert.equal(cell.contextDistance, cell.role === "neighbor" ? 1 : 0,
      `cell ${cell.hash}: one level of ancestors means one distance`);
    assert.deepEqual(asRecorded([...cell.extent]), asRecorded([...handCell(surface, cell.hash)]),
      `cell ${cell.hash}: the hand-derived box`);
    assert.equal(cell.count, sweep(standing, handCell(surface, cell.hash)),
      `cell ${cell.hash}: the pins it holds, recounted independently`);
  }
  assert.deepEqual(
    cells.filter((cell) => cell.role === "child").map((cell) => cell.hash),
    [...ALPHABET].map((character) => `m${character}`),
    "the children refine the held address by one character");
});

test("a plane city's root grid divides the whole world square", () => {
  // bend-or's Basemap declares the full 8192 square as its surface, so a
  // level-1 cell here is 1024 × 2048 — same alphabet, different ground.
  const surface: Extent = [0, -8192, 8192, 0];
  const { cells, extent, standing } = draw(world("bend-or", "2026-08-02"), "");
  assert.deepEqual(asRecorded(extent), [...surface]);
  assert.equal(standing.length, 65, "the city corpus stands 65 pins");
  assert.deepEqual(asRecorded([...(cells.find((cell) => cell.hash === "0")?.extent ?? [])]),
    [0, -8192, 1024, -6144], "column 0, bottom row of four 2048-tall rows");
  assert.deepEqual(asRecorded([...(cells.find((cell) => cell.hash === "z")?.extent ?? [])]),
    [7168, -2048, 8192, 0], "column 7, top row");
  for (const cell of cells) {
    assert.equal(cell.count, sweep(standing, handCell(surface, cell.hash)),
      `cell ${cell.hash}: the pins it holds, recounted independently`);
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
// The plan was never the problem: held at "m6", it carries thirty-two
// children, in the frozen emission order the first test below spells out by
// hand. What was wrong is *when* the question "can this cell carry its
// address?" was asked. A child too small for its label draws **nothing**
// (analysis/cellsystems/visual.ts), so that one boolean decides whether the
// subdivision exists — and it was answered once, while the grid was being
// built, against the resolution the camera was *leaving*. Descending flies the
// camera in afterwards; nothing asked again. The deeper the held cell, the
// smaller its children are against the zoom they were judged at, and at two
// levels down every one of them failed.
//
// So the fit gate belongs to the style function, where OpenLayers hands the
// resolution being drawn at. These tests are that, in numbers derived from
// the halving and from the ruler stub above.

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

test("a cell held two levels down plans every one of its children, in the frozen order", () => {
  // The emission order, spelled out by hand from the contract: each
  // ancestor's children root-first, skipping the one on the path — thirty-one
  // at distance two, thirty-one at distance one — then the held cell as the
  // scope, then its thirty-two children.
  const expected: [string, string, number][] = [
    ...[...ALPHABET].filter((c) => c !== "m").map((c): [string, string, number] => [c, "neighbor", 2]),
    ...[...ALPHABET].filter((c) => c !== "6").map((c): [string, string, number] => [`m${c}`, "neighbor", 1]),
    ["m6", "scope", 0],
    ...[...ALPHABET].map((c): [string, string, number] => [`m6${c}`, "child", 0]),
  ];
  const model = world("mars", "global");
  const plan = planCells(
    model.ground(model.payload.lenses[0] ?? null), cellSystems.require("geohash"), "m6");
  assert.deepEqual(
    plan.map((cell) => [cell.hash, cell.role, cell.contextDistance]), expected,
    "ninety-five cells, position for position");
  assert.equal(plan.filter((cell) => cell.role === "child").length, 32,
    "thirty-two children, one level above the cap");
  assert.deepEqual(
    planned(model, "m6").map((cell) => cell.hash).sort(),
    plan.map((cell) => cell.hash).sort(),
    "and the drawn features carry that same plan, cell for cell");
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
  const cells = planned(world("mars", "global"), "m6");
  const children = cells.filter((cell) => cell.role === "child");
  // The gate's own arithmetic, with the ruler stub above: a three-character
  // chip at 11 px is 3 · 11 · 0.6 + 10 of padding = 29.8 screen pixels, and a
  // child of "m6" is 32 world pixels across — so it carries its address only
  // below 32 / 29.8 ≈ 1.07 world pixels per screen pixel.
  const leaving = 2; // reading "m" from far enough out that a child is 16 px
  assert.equal(children.some((cell) => paints(cell, leaving, true)), false,
    "no child fits its address at the zoom the camera is leaving — which is what was drawn");
  // Where the camera actually lands: a rung of the pyramid's own ladder well
  // under the gate, where the same child is 128 screen pixels across.
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
  const cells = planned(world("mars", "global"), "m6s");
  assert.equal(cells.filter((cell) => cell.role === "leaf").length, 1);
  assert.equal(cells.filter((cell) => cell.role === "child").length, 0,
    "the floor of the telescope divides no further");
  // A leaf is drawn whether or not its address fits: it is the ground the
  // reader asked for, not a subdivision offered to them.
  const leaf = cells.find((cell) => cell.role === "leaf");
  assert.ok(leaf);
  assert.deepEqual(asRecorded([...leaf.extent]), [5504, -2752, 5536, -2720],
    "a 32-pixel square, three characters down");
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
