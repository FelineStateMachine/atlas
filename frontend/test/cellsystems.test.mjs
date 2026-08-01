// Golden tests for the cell systems: pure math, no DOM, no OpenLayers.
// The geohash values are hand-derived from the halving rules -- not
// computed by the code under test -- so a regression in the extraction
// shows up as a number, not a tautology.
import assert from "node:assert/strict";
import test from "node:test";

import { state } from "../src/state.js";
import { clipRingX, equivalentCell, surfaceExtent } from "../src/cellsystems/index.js";
import { geohashSystem, geohashCellAt } from "../src/cellsystems/geohash.js";
import { s2System } from "../src/cellsystems/s2.js";

function onSquareSurface() {
  state.volume = { tileGrid: { size: 1024 } };
  state.lens = { surface: { x: 0, y: 0, width: 1024, height: 1024 } };
}

test("surfaceExtent prefers the surface and falls back to bounds", () => {
  onSquareSurface();
  assert.deepEqual(surfaceExtent(), [0, -1024, 1024, -0]);
  state.lens = { bounds: { x: 128, y: 256, width: 512, height: 256 } };
  assert.deepEqual(surfaceExtent(), [128, -512, 640, -256]);
  state.lens = null;
  assert.deepEqual(surfaceExtent(), [0, -1024, 1024, -0]);
});

test("bbox halves the way geohash always has", () => {
  onSquareSurface();
  assert.deepEqual(geohashSystem.bbox(""), [0, -1024, 1024, -0]);
  // m = alphabet index 19 = 10011: x right, y bottom, x left, y up, x right.
  assert.deepEqual(geohashSystem.bbox("m"), [640, -768, 768, -512]);
  // 6 = 00110, continuing on the alternate axis.
  assert.deepEqual(geohashSystem.bbox("m6"), [672, -704, 704, -672]);
});

test("cellAt is bbox run backward", () => {
  onSquareSurface();
  const center = [688, -688];
  assert.equal(geohashSystem.descendTarget("", center), "m");
  assert.equal(geohashSystem.descendTarget("m", center), "m6");
  assert.equal(geohashCellAt(center, 3), "m6s");
  assert.equal(geohashCellAt([-1, -1], 3), "", "off the surface names nothing");
});

test("contains is boundary-inclusive", () => {
  onSquareSurface();
  assert.equal(geohashSystem.contains("m6", [704, -672]), true);
  assert.equal(geohashSystem.contains("m6", [672, -704]), true);
  assert.equal(geohashSystem.contains("m6", [704.0001, -672]), false);
});

test("normalizeInput keeps what the alphabet keeps", () => {
  onSquareSurface();
  assert.equal(geohashSystem.normalizeInput("M6X!"), "m6x");
  assert.equal(geohashSystem.normalizeInput("aM6iLo"), "m6");
  assert.equal(geohashSystem.normalizeInput("m6w9"), "m6w");
  assert.equal(geohashSystem.parseInput(""), "", "the root is a place");
});

test("clipRingX cuts a wrapped ring against the surface edges", () => {
  // The clip may start its ring at any vertex; the shape is what matters.
  const canonical = (ring) => {
    const open = ring.slice(0, -1).map(([x, y]) => `${x},${y}`);
    const start = open.indexOf([...open].sort()[0]);
    return [...open.slice(start), ...open.slice(0, start)].join(" ");
  };
  // A square straddling the right edge at x=100: the piece as it lies.
  const straddling = [[80, -10], [80, -30], [120, -30], [120, -10], [80, -10]];
  assert.equal(
    canonical(clipRingX(straddling, 0, 100)),
    canonical([[80, -10], [80, -30], [100, -30], [100, -10], [80, -10]]),
  );
  // Shifted a world (100) left, the other piece appears at the left edge.
  const shifted = straddling.map(([x, y]) => [x - 100, y]);
  assert.equal(
    canonical(clipRingX(shifted, 0, 100)),
    canonical([[0, -10], [0, -30], [20, -30], [20, -10], [0, -10]]),
  );
  // A ring entirely outside survives as nothing.
  assert.deepEqual(clipRingX(shifted.map(([x, y]) => [x - 200, y]), 0, 100), []);
  // A ring entirely inside comes back whole, closed, in its own order.
  const inside = [[10, -10], [10, -20], [20, -20], [20, -10], [10, -10]];
  assert.deepEqual(clipRingX(inside, 0, 100), inside);
});

test("plan primitives stay stable", () => {
  onSquareSurface();
  const children = geohashSystem.children("m");
  assert.equal(children.length, 32);
  assert.equal(children[0], "m0");
  assert.equal(children[31], "mz");
  assert.equal(geohashSystem.childIndex("m6"), 6);
  assert.equal(geohashSystem.parent("m6"), "m");
  assert.equal(geohashSystem.level(""), 0);
  assert.deepEqual(geohashSystem.label("m6s"), { context: "m6", principal: "s" });
  const ring = geohashSystem.ring("m6");
  assert.equal(ring.length, 5);
  assert.deepEqual(ring[0], ring[4], "the ring closes");
  assert.deepEqual(geohashSystem.locate([688, -688]), { label: "Geohash", value: "m6s" });
  assert.equal(geohashSystem.locate([-1, -1]), null);
});

// --- S2 over the declared sphere -------------------------------------

// A Mars-shaped map: the top half of an 8192 world square declared as the
// whole equirectangular ground.
function onSphereMap() {
  state.volume = { tileGrid: { size: 8192 } };
  state.lens = { surface: { x: 0, y: 0, width: 8192, height: 4096 } };
  state.world = {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
  };
}

test("s2 applies only where a sphere is declared", () => {
  onSphereMap();
  assert.equal(s2System.appliesTo(state.world), true);
  assert.equal(s2System.appliesTo({ attrs: {} }), false);
  assert.equal(geohashSystem.appliesTo(state.world), true, "geohash applies everywhere");
});

test("s2 tokens carry the Go library's vectors", () => {
  onSphereMap();
  // (49.703498679, 11.770681595) is leaf 0x47a1cbd595522b39; its level-10
  // parent is token 47a1cb. The point in world pixels comes through the
  // declared mapping.
  const world = [((11.770681595 + 180) / 360) * 8192, -(((90 - 49.703498679) / 180) * 4096)];
  const row = s2System.locate(world, state.world);
  assert.deepEqual(row, { label: "S2", value: "47a1cb" });
  assert.equal(s2System.level("47a1cb"), 11, "port level = s2 level + 1");
  assert.equal(s2System.parent("1"), "", "a face's parent is the root");
});

test("s2 children nest exactly and stay four wide", () => {
  onSphereMap();
  const faces = s2System.children("");
  assert.deepEqual(faces, ["1", "3", "5", "7", "9", "b"]);
  const children = s2System.children("47a1cb");
  assert.equal(children.length, 4);
  for (const child of children) {
    assert.equal(s2System.parent(child), "47a1cb", `${child} nests`);
  }
  assert.deepEqual(
    children.map((child) => s2System.childIndex(child)),
    [0, 1, 2, 3],
    "sibling ordinals are stable",
  );
});

test("s2 containment agrees with descent", () => {
  onSphereMap();
  const world = [((11.770681595 + 180) / 360) * 8192, -(((90 - 49.703498679) / 180) * 4096)];
  let id = "";
  for (let hop = 0; hop < 11; hop++) {
    id = s2System.descendTarget(id, world);
    assert.ok(id, `descent step ${hop} names a cell`);
    assert.equal(s2System.contains(id, world), true, `${id} holds its own point`);
  }
  assert.equal(id, "47a1cb", "eleven hops land on the level-10 cell");
  assert.equal(s2System.descendTarget(id, world), "", "the telescope has a floor");
});

test("s2 input parses whole or not at all", () => {
  onSphereMap();
  assert.equal(s2System.parseInput(""), "");
  assert.equal(s2System.parseInput("47a1cb"), "47a1cb");
  assert.equal(s2System.parseInput("47a1cbd595522b39".slice(0, 6)), "47a1cb");
  assert.equal(s2System.parseInput("xyz"), null, "not hex, not a place");
  assert.equal(s2System.normalizeInput("47A1 CB!"), "47a1cb");
});

test("a place carries across systems at like precision", () => {
  onSphereMap();
  assert.equal(equivalentCell(geohashSystem, s2System, "", state.world), "", "root maps to root");
  // A level-2 geohash cell is 256x128 of the 8192x4096 world; the S2 cell
  // holding its center at the nearest area sits within a level either way.
  const token = s2System.parseInput(equivalentCell(geohashSystem, s2System, "m6", state.world));
  assert.ok(token, "the translation names a real cell");
  assert.equal(s2System.contains(token, geohashSystem.center("m6")), true,
    "the new cell holds the old center");
  assert.ok(Math.abs(s2System.level(token) - 4) <= 1,
    `precision carries: level ${s2System.level(token)} ~ 4`);
  // And back: the round trip stays on the same ground at the same depth.
  const home = equivalentCell(s2System, geohashSystem, token, state.world);
  assert.equal(geohashSystem.contains(home, s2System.center(token)), true);
  assert.ok(Math.abs(home.length - 2) <= 1, `round trip keeps depth: "${home}"`);
});

test("s2 rings close, stay continuous, and know the poles", () => {
  onSphereMap();
  const ring = s2System.ring("47a1cb");
  assert.ok(ring.length > 8, "edges arrive tessellated");
  assert.deepEqual(ring[0], ring[ring.length - 1], "the ring closes");
  // Continuity holds through the closing segment, and for the pole faces
  // whose loops accumulate a whole world of longitude: the closure joins
  // in the frame the walk ended in, a world over from where it began.
  for (const id of ["47a1cb", "5", "b"]) {
    const loop = s2System.ring(id);
    for (let at = 1; at < loop.length; at++) {
      assert.ok(
        Math.abs(loop[at][0] - loop[at - 1][0]) < 4096,
        `${id}: no seam-sized jumps, the loop is continuous`,
      );
    }
    const drift = Math.abs(loop[0][0] - loop[loop.length - 1][0]) % 8192;
    assert.ok(drift < 1e-6 || Math.abs(drift - 8192) < 1e-6,
      `${id}: the closure lands on the opening point, some frame over`);
    assert.equal(loop[0][1], loop[loop.length - 1][1], `${id}: same latitude row`);
  }
  // Face 2 (token 5) holds the north pole; face 5 (token b) the south.
  assert.equal(s2System.poleContained("5"), "north");
  assert.equal(s2System.poleContained("b"), "south");
  assert.equal(s2System.poleContained("47a1cb"), null);
});
