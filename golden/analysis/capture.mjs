// Capture the analysis vectors and the cell plans (issue #5 §6.1, item 6).
//
//   node golden/analysis/capture.mjs          # rewrite every fixture
//   node golden/analysis/capture.mjs --check  # capture in memory, write nothing
//
// This script is the recording instrument, not the gate; `run.mjs` is the
// gate. Two rules shape it:
//
//  1. Every expectation is *recorded from the current implementation*, and
//     every expectation that the frontend's hand-derived test suite already
//     pins is *also checked against the literal from that suite* on the way
//     past (the `hand` field below). A recorded value that disagrees with a
//     hand-derived one aborts the capture. That is what keeps the goldens
//     from being a tautology: the numbers were derived from the halving
//     rules and the S2 library's own vectors first, and the recording only
//     puts them in a language-neutral file.
//
//  2. Every case names its ground. The current engine reads the ground out
//     of the application's shared client state; the fixtures name it
//     explicitly, which is issue #5 §5.4's de-globalization written down a
//     milestone early. See README.md.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import * as engine from "./engine/current.mjs";

const here = path.dirname(new URL(import.meta.url).pathname);
const repoRoot = path.resolve(here, "..", "..");
const write = !process.argv.includes("--check");

// --- grounds ---------------------------------------------------------------

// The ground descriptor is everything the cell systems read about the
// surface they divide. The current engine takes each field from a global:
//
//   tileGridSize  state.volume.tileGrid.size   the world square
//   lens.surface  state.lens.surface           the lens's declared surface
//   lens.bounds   state.lens.bounds            its raster window
//   world.attrs   state.world.attrs            the semconv geometry keys
//
// surfaceExtent is derived from the first three and recorded as a golden of
// its own, so a candidate that carries the ground differently still has to
// land on the same numbers.
const grounds = new Map();

function ground(key, about, descriptor) {
  const record = { key, about, ...descriptor };
  grounds.set(key, record);
  return record;
}

// The hand-derived grounds: the ones frontend/test/cellsystems.test.mjs
// works on, carried over verbatim so its numbers keep meaning something.
ground("test/square-1024", "The unit square of the hand-derived suite: a 1024 world declaring the whole of itself as its lens surface, and no geometry attributes at all — the plane every geohash number in the suite was halved out of.", {
  tileGridSize: 1024,
  lens: { surface: { x: 0, y: 0, width: 1024, height: 1024 }, bounds: null },
  world: { attrs: {} },
});

ground("test/square-1024-bounds", "The same world with a lens that declares no surface, only a raster window: the fallback branch of surfaceExtent.", {
  tileGridSize: 1024,
  lens: { surface: null, bounds: { x: 128, y: 256, width: 512, height: 256 } },
  world: { attrs: {} },
});

ground("test/square-1024-no-lens", "The same world with no lens at all: the last fallback, the whole world square.", {
  tileGridSize: 1024,
  lens: null,
  world: { attrs: {} },
});

ground("test/sphere-8192x4096", "The hand-derived sphere: the top half of an 8192 world square declared as a whole equirectangular ground. The S2 vectors — leaf 0x47a1cbd595522b39 and its level-10 parent 47a1cb — are the Go library's own, read through this declaration.", {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 8192, height: 4096 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
  },
});

// The tour grounds, read out of the bundle fixtures. A grid-touching parity
// step happened on one volume's world and lens; these are those.
const fixtures = JSON.parse(fs.readFileSync(path.join(repoRoot, "golden/parity/FIXTURES.json"), "utf8"));

function tourGround(volume, worldTitle, lensName) {
  const base = path.join(repoRoot, "golden/fixtures/bundles", volume);
  const manifest = JSON.parse(fs.readFileSync(path.join(base, "manifest.json"), "utf8"));
  const world = manifest.worlds.find((entry) => entry.title === worldTitle);
  assert.ok(world, `${volume}: no world titled ${worldTitle} in the bundle fixture`);
  const payload = JSON.parse(fs.readFileSync(path.join(base, "worlds", `${world.slug}.payload.json`), "utf8"));
  const index = payload.lenses.findIndex((lens) => lens.name === lensName);
  assert.ok(index >= 0, `${volume}/${world.slug}: no lens named ${lensName}`);
  const lens = payload.lenses[index];
  const key = `${volume}/${world.slug}/${index}`;
  if (grounds.has(key)) return grounds.get(key);
  return ground(key, `${manifest.volume.title} — ${world.title}, lens "${lens.name}". Read from the bundle fixture at golden/fixtures/bundles/${volume}.`, {
    tileGridSize: manifest.tileGrid.size,
    lens: { surface: lens.surface ?? null, bounds: lens.bounds ?? null },
    // A world payload with no attrs block reaches the engine as an absent
    // object; every reader spells it `map?.attrs?.[key]`, so an empty one is
    // the same ground.
    world: { attrs: payload.attrs ?? {} },
    provenance: { volume, world: world.slug, lens: lens.name, lensIndex: index },
  });
}

// --- vector families -------------------------------------------------------

const families = new Map();

function family(name, about) {
  const record = { family: name, about, cases: [] };
  families.set(name, record);
  return record;
}

// add records one case: the call, and what the current implementation
// answers. `hand` is the literal the frontend suite pins, when it pins one;
// it is checked and then dropped, because the fixture carries the recording.
async function add(into, spec) {
  const { name, ground: key, system = null, call, args = [], hand, note } = spec;
  const on = grounds.get(key);
  assert.ok(on, `no ground named ${key}`);
  const value = await evaluate(on, system, call, args);
  if (hand !== undefined) {
    assert.deepEqual(value, hand,
      `${into.family}/${name}: the engine disagrees with the hand-derived golden`);
  }
  const record = { name, ground: key, ...(system ? { system } : {}), call, args, expect: value };
  if (note) record.note = note;
  if (hand !== undefined) record.handDerived = true;
  into.cases.push(record);
  return value;
}

// evaluate is the whole dispatch table: eight calls, and the eighteen
// contract methods behind `invoke`. run.mjs carries the same table, and a
// Go or TypeScript consumer of these fixtures needs only this much.
async function evaluate(on, system, call, args) {
  switch (call) {
    case "surfaceExtent":
      return engine.surfaceExtent(on);
    case "applicableSystems":
      return engine.applicableSystems(on);
    case "geohashCellAt":
      return engine.geohashCellAt(on, args[0], args[1]);
    case "equivalentCell":
      return engine.equivalentCell(on, args[0], args[1], args[2]);
    case "clipRingX":
      return engine.clipRingX(args[0], args[1], args[2]);
    default:
      assert.ok(system, `${call}: a contract method needs a system`);
      return engine.invoke(on, system, call, args);
  }
}

// The point the S2 vectors are taken at, in world pixels of the declared
// sphere: latitude 49.703498679, longitude 11.770681595 — the s2js/Go
// library's own test point, whose leaf is 0x47a1cbd595522b39.
const s2Latitude = 49.703498679;
const s2Longitude = 11.770681595;
const s2Point = [
  ((s2Longitude + 180) / 360) * 8192,
  -(((90 - s2Latitude) / 180) * 4096),
];

async function captureVectors() {
  // 1. the ground itself ----------------------------------------------------
  const surface = family("surface",
    "What the ground is, before any cell is named: the surface extent every system divides, and which systems are willing to divide it. The three surfaceExtent branches — a declared lens surface, a raster window, neither — are the whole of the fallback ladder.");
  await add(surface, {
    name: "extent-from-lens-surface", ground: "test/square-1024", call: "surfaceExtent",
    hand: [0, -1024, 1024, -0],
  });
  await add(surface, {
    name: "extent-from-lens-bounds", ground: "test/square-1024-bounds", call: "surfaceExtent",
    hand: [128, -512, 640, -256],
  });
  await add(surface, {
    name: "extent-from-world-square", ground: "test/square-1024-no-lens", call: "surfaceExtent",
    hand: [0, -1024, 1024, -0],
  });
  await add(surface, {
    name: "extent-sphere", ground: "test/sphere-8192x4096", call: "surfaceExtent",
  });
  await add(surface, {
    name: "systems-on-a-plane", ground: "test/square-1024", call: "applicableSystems",
    hand: ["geohash"],
    note: "S2 only divides a map that declares what its picture is of; geohash divides anything.",
  });
  await add(surface, {
    name: "systems-on-a-sphere", ground: "test/sphere-8192x4096", call: "applicableSystems",
    hand: ["geohash", "s2"],
  });
  await add(surface, {
    name: "geohash-applies-to-a-plane", ground: "test/square-1024", system: "geohash",
    call: "appliesTo", hand: true,
  });
  await add(surface, {
    name: "s2-refuses-a-plane", ground: "test/square-1024", system: "s2",
    call: "appliesTo", hand: false,
  });
  await add(surface, {
    name: "s2-applies-to-a-sphere", ground: "test/sphere-8192x4096", system: "s2",
    call: "appliesTo", hand: true,
  });
  for (const [slug, depth] of [["geohash", 3], ["s2", 11]]) {
    await add(surface, {
      name: `${slug}-max-level`, ground: "test/sphere-8192x4096", system: slug,
      call: "maxLevel", hand: depth,
      note: slug === "s2" ? "Port levels count the root as 0 and the six faces as 1, so the S2 level-10 floor is port level 11." : undefined,
    });
    await add(surface, {
      name: `${slug}-input-length`, ground: "test/sphere-8192x4096", system: slug,
      call: "inputLength", hand: slug === "geohash" ? 3 : 6,
    });
  }

  // 2. identity -------------------------------------------------------------
  const identity = family("identity",
    "Cell ids are opaque strings and \"\" is the root. This family pins what a system says about one id on its own: its depth, its parent, its ordinal among siblings, the two cuts of its label, and its palette index. Geohash ids refine by appending a character; S2 ids do not, which is the whole reason the contract speaks in opaque strings.");
  for (const spec of [
    { id: "", level: 0, parent: "" },
    { id: "m", level: 1, parent: "" },
    { id: "m6", level: 2, parent: "m" },
    { id: "m6s", level: 3, parent: "m6" },
  ]) {
    await add(identity, {
      name: `geohash-level-${spec.id || "root"}`, ground: "test/square-1024", system: "geohash",
      call: "level", args: [spec.id], hand: spec.level,
    });
    await add(identity, {
      name: `geohash-parent-${spec.id || "root"}`, ground: "test/square-1024", system: "geohash",
      call: "parent", args: [spec.id], hand: spec.parent,
    });
  }
  await add(identity, {
    name: "geohash-child-index", ground: "test/square-1024", system: "geohash",
    call: "childIndex", args: ["m6"], hand: 6,
  });
  await add(identity, {
    name: "geohash-label", ground: "test/square-1024", system: "geohash",
    call: "label", args: ["m6s"], hand: { context: "m6", principal: "s" },
  });
  for (const id of ["0", "m", "m6", "m6s", "mz"]) {
    await add(identity, {
      name: `geohash-color-key-${id}`, ground: "test/square-1024", system: "geohash",
      call: "colorKey", args: [id],
    });
  }
  await add(identity, {
    name: "s2-level-of-a-face", ground: "test/sphere-8192x4096", system: "s2",
    call: "level", args: ["1"], hand: 1,
  });
  await add(identity, {
    name: "s2-level-47a1cb", ground: "test/sphere-8192x4096", system: "s2",
    call: "level", args: ["47a1cb"], hand: 11,
    note: "port level = S2 level + 1.",
  });
  await add(identity, {
    name: "s2-parent-of-a-face-is-the-root", ground: "test/sphere-8192x4096", system: "s2",
    call: "parent", args: ["1"], hand: "",
  });
  const s2Chain = ["47a1cb"];
  while (s2Chain[0] !== "") {
    s2Chain.unshift(await engine.invoke(grounds.get("test/sphere-8192x4096"), "s2", "parent", [s2Chain[0]]));
  }
  for (const id of s2Chain.slice(1)) {
    await add(identity, {
      name: `s2-parent-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "parent", args: [id],
    });
    await add(identity, {
      name: `s2-level-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "level", args: [id],
    });
    await add(identity, {
      name: `s2-child-index-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "childIndex", args: [id],
    });
    await add(identity, {
      name: `s2-label-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "label", args: [id],
    });
    await add(identity, {
      name: `s2-color-key-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "colorKey", args: [id],
      note: id === s2Chain[1] ? "Sibling tokens can share their final character, so the accent comes from the sibling ordinal instead." : undefined,
    });
  }

  // 3. hierarchy ------------------------------------------------------------
  const hierarchy = family("hierarchy",
    "children() in its STABLE order, and the parent link back. Order is contractual: the plan emits children in it, the palette indexes into it, and the parity harness compares the result positionally.");
  const geohashChildren = await add(hierarchy, {
    name: "geohash-children-of-m", ground: "test/square-1024", system: "geohash",
    call: "children", args: ["m"],
  });
  assert.equal(geohashChildren.length, 32, "a geohash cell has 32 children");
  assert.equal(geohashChildren[0], "m0");
  assert.equal(geohashChildren[31], "mz");
  await add(hierarchy, {
    name: "geohash-children-of-the-root", ground: "test/square-1024", system: "geohash",
    call: "children", args: [""],
  });
  await add(hierarchy, {
    name: "s2-faces", ground: "test/sphere-8192x4096", system: "s2",
    call: "children", args: [""], hand: ["1", "3", "5", "7", "9", "b"],
    note: "The six faces of the cube, in face order. This is the root's child list.",
  });
  const s2Children = await add(hierarchy, {
    name: "s2-children-of-47a1cb", ground: "test/sphere-8192x4096", system: "s2",
    call: "children", args: ["47a1cb"],
  });
  assert.equal(s2Children.length, 4, "an S2 cell has four children");
  for (const child of s2Children) {
    await add(hierarchy, {
      name: `s2-parent-of-${child}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "parent", args: [child], hand: "47a1cb",
    });
    await add(hierarchy, {
      name: `s2-child-index-of-${child}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "childIndex", args: [child],
    });
  }
  assert.deepEqual(
    await Promise.all(s2Children.map((child) =>
      engine.invoke(grounds.get("test/sphere-8192x4096"), "s2", "childIndex", [child]))),
    [0, 1, 2, 3],
    "sibling ordinals are stable",
  );
  await add(hierarchy, {
    name: "s2-children-of-a-face", ground: "test/sphere-8192x4096", system: "s2",
    call: "children", args: ["5"],
    note: "The north-polar face's four children — the ids that do not refine by appending.",
  });

  // 4. containment ----------------------------------------------------------
  const containment = family("containment",
    "Containment is boundary-INCLUSIVE, and descent agrees with it: the child under a point is a cell that contains the point. locate() is the same question asked for the pin card, in the system's own address.");
  await add(containment, {
    name: "geohash-contains-top-right-corner", ground: "test/square-1024", system: "geohash",
    call: "contains", args: ["m6", [704, -672]], hand: true,
    note: "The maximum corner is inside: boundaries are inclusive on both sides.",
  });
  await add(containment, {
    name: "geohash-contains-bottom-left-corner", ground: "test/square-1024", system: "geohash",
    call: "contains", args: ["m6", [672, -704]], hand: true,
  });
  await add(containment, {
    name: "geohash-excludes-just-past-the-edge", ground: "test/square-1024", system: "geohash",
    call: "contains", args: ["m6", [704.0001, -672]], hand: false,
  });
  await add(containment, {
    name: "geohash-descend-root", ground: "test/square-1024", system: "geohash",
    call: "descendTarget", args: ["", [688, -688]], hand: "m",
  });
  await add(containment, {
    name: "geohash-descend-m", ground: "test/square-1024", system: "geohash",
    call: "descendTarget", args: ["m", [688, -688]], hand: "m6",
  });
  await add(containment, {
    name: "geohash-cell-at-depth-3", ground: "test/square-1024", call: "geohashCellAt",
    args: [[688, -688], 3], hand: "m6s",
  });
  await add(containment, {
    name: "geohash-cell-at-off-the-surface", ground: "test/square-1024", call: "geohashCellAt",
    args: [[-1, -1], 3], hand: "",
    note: "Off the surface names nothing.",
  });
  await add(containment, {
    name: "geohash-locate", ground: "test/square-1024", system: "geohash",
    call: "locate", args: [[688, -688]], hand: { label: "Geohash", value: "m6s" },
  });
  await add(containment, {
    name: "geohash-locate-off-the-surface", ground: "test/square-1024", system: "geohash",
    call: "locate", args: [[-1, -1]], hand: null,
  });
  await add(containment, {
    name: "s2-locate", ground: "test/sphere-8192x4096", system: "s2",
    call: "locate", args: [s2Point], hand: { label: "S2", value: "47a1cb" },
    note: "The library's own vector: (49.703498679, 11.770681595) is leaf 0x47a1cbd595522b39, whose level-10 parent is 47a1cb. The point arrives in world pixels through the declared mapping.",
  });
  // The eleven-hop descent, each hop containing the point it descended for.
  let held = "";
  for (let hop = 0; hop < 11; hop++) {
    const next = await add(containment, {
      name: `s2-descend-hop-${hop}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "descendTarget", args: [held, s2Point],
    });
    assert.ok(next, `descent step ${hop} names a cell`);
    await add(containment, {
      name: `s2-contains-hop-${hop}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "contains", args: [next, s2Point], hand: true,
    });
    held = next;
  }
  assert.equal(held, "47a1cb", "eleven hops land on the level-10 cell");
  await add(containment, {
    name: "s2-descent-has-a-floor", ground: "test/sphere-8192x4096", system: "s2",
    call: "descendTarget", args: [held, s2Point], hand: "",
    note: "The telescope stops at S2 level 10.",
  });

  // 5. geometry -------------------------------------------------------------
  const geometry = family("geometry",
    "The shapes: bbox in OL world coordinates (x east, y negative-down), the centre, and the closed ring. Every geohash number here is the recursive halving of the surface, five bits to a character, alternating axes — the arithmetic the app has always done.");
  await add(geometry, {
    name: "geohash-bbox-root", ground: "test/square-1024", system: "geohash",
    call: "bbox", args: [""], hand: [0, -1024, 1024, -0],
  });
  await add(geometry, {
    name: "geohash-bbox-m", ground: "test/square-1024", system: "geohash",
    call: "bbox", args: ["m"], hand: [640, -768, 768, -512],
    note: "m = alphabet index 19 = 10011: x right, y bottom, x left, y up, x right.",
  });
  await add(geometry, {
    name: "geohash-bbox-m6", ground: "test/square-1024", system: "geohash",
    call: "bbox", args: ["m6"], hand: [672, -704, 704, -672],
    note: "6 = 00110, continuing on the alternate axis.",
  });
  await add(geometry, {
    name: "geohash-bbox-m6s", ground: "test/square-1024", system: "geohash",
    call: "bbox", args: ["m6s"],
  });
  for (const id of ["", "m", "m6", "m6s"]) {
    await add(geometry, {
      name: `geohash-center-${id || "root"}`, ground: "test/square-1024", system: "geohash",
      call: "center", args: [id],
    });
  }
  const geohashRing = await add(geometry, {
    name: "geohash-ring-m6", ground: "test/square-1024", system: "geohash",
    call: "ring", args: ["m6"],
  });
  assert.equal(geohashRing.length, 5, "a geohash ring is four corners and the closure");
  assert.deepEqual(geohashRing[0], geohashRing[4], "the ring closes");
  await add(geometry, {
    name: "geohash-poles", ground: "test/square-1024", system: "geohash",
    call: "poleContained", args: ["m6"], hand: null,
    note: "A plane holds no pole; geohash always answers null.",
  });
  await add(geometry, {
    name: "s2-bbox-root-is-the-surface", ground: "test/sphere-8192x4096", system: "s2",
    call: "bbox", args: [""],
  });
  for (const id of ["47a1cb", "5", "1"]) {
    await add(geometry, {
      name: `s2-bbox-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "bbox", args: [id],
    });
    await add(geometry, {
      name: `s2-center-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "center", args: [id],
    });
  }
  const clip = "clipRingX cuts a closed ring against the surface's two vertical edges — the Sutherland-Hodgman walk the chart uses to draw a ring that stayed continuous across the antimeridian as its two pieces.";
  const straddling = [[80, -10], [80, -30], [120, -30], [120, -10], [80, -10]];
  const shifted = straddling.map(([x, y]) => [x - 100, y]);
  await add(geometry, {
    name: "clip-straddling-right-edge", ground: "test/square-1024", call: "clipRingX",
    args: [straddling, 0, 100], note: clip,
  });
  await add(geometry, {
    name: "clip-shifted-a-world-left", ground: "test/square-1024", call: "clipRingX",
    args: [shifted, 0, 100],
  });
  await add(geometry, {
    name: "clip-entirely-outside", ground: "test/square-1024", call: "clipRingX",
    args: [shifted.map(([x, y]) => [x - 200, y]), 0, 100], hand: [],
    note: "A ring entirely outside survives as nothing.",
  });
  const inside = [[10, -10], [10, -20], [20, -20], [20, -10], [10, -10]];
  await add(geometry, {
    name: "clip-entirely-inside", ground: "test/square-1024", call: "clipRingX",
    args: [inside, 0, 100], hand: inside,
    note: "A ring entirely inside comes back whole, closed, in its own order.",
  });

  // 6. continuity -----------------------------------------------------------
  const continuity = family("continuity",
    "Antimeridian continuity and pole handling: the two properties an unwrapped ring has to keep. A ring never jumps a seam — consecutive points stay less than half a world apart — and its closing point joins the loop in the frame the walk ended in, which for a polar face is a whole world of longitude away from where it began. poleContained names the pole a cell circles, so the adapters can close the loop along the picture's own edge.");
  for (const id of ["47a1cb", "5", "b", "1"]) {
    const ring = await add(continuity, {
      name: `s2-ring-${id}`, ground: "test/sphere-8192x4096", system: "s2",
      call: "ring", args: [id],
    });
    assert.ok(ring.length > 8, `${id}: edges arrive tessellated`);
    for (let at = 1; at < ring.length; at++) {
      assert.ok(Math.abs(ring[at][0] - ring[at - 1][0]) < 4096,
        `${id}: no seam-sized jumps, the loop is continuous`);
    }
    const drift = Math.abs(ring[0][0] - ring[ring.length - 1][0]) % 8192;
    assert.ok(drift < 1e-6 || Math.abs(drift - 8192) < 1e-6,
      `${id}: the closure lands on the opening point, some frame over`);
    assert.equal(ring[0][1], ring[ring.length - 1][1], `${id}: same latitude row`);
  }
  await add(continuity, {
    name: "s2-north-pole-face", ground: "test/sphere-8192x4096", system: "s2",
    call: "poleContained", args: ["5"], hand: "north",
  });
  await add(continuity, {
    name: "s2-south-pole-face", ground: "test/sphere-8192x4096", system: "s2",
    call: "poleContained", args: ["b"], hand: "south",
  });
  await add(continuity, {
    name: "s2-no-pole", ground: "test/sphere-8192x4096", system: "s2",
    call: "poleContained", args: ["47a1cb"], hand: null,
  });

  // 7. input ----------------------------------------------------------------
  const input = family("input",
    "What the navigator field keeps of a keystroke, and when the draft becomes a place. Geohash parses whole always — every normalized spelling is an address. S2 is whole-or-nothing: a partial token is not yet a place, a token past the floor clamps and re-tokenizes, and a non-token is refused with null.");
  for (const [text, kept] of [["M6X!", "m6x"], ["aM6iLo", "m6"], ["m6w9", "m6w"]]) {
    await add(input, {
      name: `geohash-normalize-${text}`, ground: "test/square-1024", system: "geohash",
      call: "normalizeInput", args: [text], hand: kept,
    });
  }
  await add(input, {
    name: "geohash-parse-root", ground: "test/square-1024", system: "geohash",
    call: "parseInput", args: [""], hand: "",
    note: "The root is a place.",
  });
  await add(input, {
    name: "geohash-parse-m6", ground: "test/square-1024", system: "geohash",
    call: "parseInput", args: ["m6"], hand: "m6",
  });
  await add(input, {
    name: "s2-normalize", ground: "test/sphere-8192x4096", system: "s2",
    call: "normalizeInput", args: ["47A1 CB!"], hand: "47a1cb",
  });
  await add(input, {
    name: "s2-parse-root", ground: "test/sphere-8192x4096", system: "s2",
    call: "parseInput", args: [""], hand: "",
  });
  await add(input, {
    name: "s2-parse-token", ground: "test/sphere-8192x4096", system: "s2",
    call: "parseInput", args: ["47a1cb"], hand: "47a1cb",
  });
  await add(input, {
    name: "s2-parse-truncated-leaf", ground: "test/sphere-8192x4096", system: "s2",
    call: "parseInput", args: ["47a1cbd595522b39".slice(0, 6)], hand: "47a1cb",
  });
  await add(input, {
    name: "s2-refuses-a-non-token", ground: "test/sphere-8192x4096", system: "s2",
    call: "parseInput", args: ["xyz"], hand: null,
    note: "Not hex, not a place.",
  });
  await add(input, {
    name: "s2-clamps-a-deeper-token", ground: "test/sphere-8192x4096", system: "s2",
    call: "parseInput", args: ["47a1cbd595522b39"],
    note: "A token deeper than the telescope's floor clamps to level 10 and comes back canonically spelled.",
  });

  // 8. carry ----------------------------------------------------------------
  const carry = family("carry",
    "equivalentCell: a place carried across systems. The two hierarchies share no boundaries, so what survives is the point and the precision — the cell of the new system holding the old cell's centre, at the depth whose area is closest on a log scale.");
  await add(carry, {
    name: "root-carries-to-root", ground: "test/sphere-8192x4096", call: "equivalentCell",
    args: ["geohash", "s2", ""], hand: "",
  });
  const carried = await add(carry, {
    name: "geohash-m6-to-s2", ground: "test/sphere-8192x4096", call: "equivalentCell",
    args: ["geohash", "s2", "m6"],
    note: "A level-2 geohash cell is 256x128 of the 8192x4096 world; the S2 cell holding its centre at the nearest area sits within a level either way.",
  });
  const sphere = grounds.get("test/sphere-8192x4096");
  const token = await engine.invoke(sphere, "s2", "parseInput", [carried]);
  assert.ok(token, "the translation names a real cell");
  await add(carry, {
    name: "carried-cell-holds-the-old-centre", ground: "test/sphere-8192x4096", system: "s2",
    call: "contains", args: [token, await engine.invoke(sphere, "geohash", "center", ["m6"])],
    hand: true,
  });
  const level = await engine.invoke(sphere, "s2", "level", [token]);
  assert.ok(Math.abs(level - 4) <= 1, `precision carries: level ${level} ~ 4`);
  const home = await add(carry, {
    name: "and-back-to-geohash", ground: "test/sphere-8192x4096", call: "equivalentCell",
    args: ["s2", "geohash", token],
  });
  await add(carry, {
    name: "round-trip-holds-the-s2-centre", ground: "test/sphere-8192x4096", system: "geohash",
    call: "contains", args: [home, await engine.invoke(sphere, "s2", "center", [token])],
    hand: true,
  });
  assert.ok(Math.abs(home.length - 2) <= 1, `round trip keeps depth: "${home}"`);
  await add(carry, {
    name: "geohash-m-to-s2", ground: "test/sphere-8192x4096", call: "equivalentCell",
    args: ["geohash", "s2", "m"],
  });
  await add(carry, {
    name: "s2-face-to-geohash", ground: "test/sphere-8192x4096", call: "equivalentCell",
    args: ["s2", "geohash", "5"],
    note: "A polar face carried onto a plane's halving: the face's centre is the north pole itself.",
  });
}

// --- plans -----------------------------------------------------------------

// The plan is the contract's one composite output: which cells the grid
// shows, in a FROZEN positional order (issue #5 §5.4). Recording it means
// recording the exact inputs the tour step had — ground, system, held cell —
// and the exact array that came back, cell for cell, key for key.
const visualCases = [
  { labelled: true },
  { labelled: false },
];

async function planFor(on, system, cell, subgridVisible) {
  const plan = await engine.cellPlan(on, system, cell);
  const visuals = [];
  for (const planned of plan) {
    const row = {};
    for (const options of visualCases) {
      row[options.labelled ? "labelled" : "unlabelled"] =
        await engine.cellVisual(on, system, planned, { subgridVisible, labelled: options.labelled });
    }
    visuals.push(row);
  }
  return { plan, visuals };
}

// The tour's own record of the cells drawn, as a map from id to the four
// fields the diagnostics snapshot keeps. OpenLayers hands its features back
// in the spatial index's order, not the plan's, so this is a set check —
// the ordering claim is the plan fixture's, and this is the tie back to the
// parity baseline that says the same cells reached the screen.
function tourCells(step) {
  const cells = new Map();
  for (const cell of step.snapshot.grid.cells) {
    cells.set(cell.hash, {
      extent: cell.extent, role: cell.role, contextDistance: cell.contextDistance,
    });
  }
  return cells;
}

// Comparison runs through JSON, here and in the gate, because JSON is where
// these goldens live: it cannot carry a negative zero, and the engine mints
// several (a surface whose top edge is y = -0). Both sides are normalized by
// being written the way the fixture is written.
export function same(a, b) {
  return JSON.stringify(a) === JSON.stringify(b);
}

function checkAgainstTour(volume, step, plan) {
  const recorded = tourCells(step);
  assert.equal(plan.length, recorded.size,
    `${volume}/${step.name}: the plan draws ${plan.length} cells, the tour recorded ${recorded.size}`);
  for (const cell of plan) {
    const seen = recorded.get(cell.hash);
    assert.ok(seen, `${volume}/${step.name}: the tour never drew ${cell.hash}`);
    assert.ok(
      same({ extent: cell.extent, role: cell.role, contextDistance: cell.contextDistance }, seen),
      `${volume}/${step.name}: ${cell.hash} differs from what the tour drew: ` +
      `${JSON.stringify(cell.extent)} ${cell.role} ${cell.contextDistance} vs ${JSON.stringify(seen)}`);
  }
}

async function capturePlans() {
  const written = [];
  for (const volume of fixtures.volumes) {
    const tour = JSON.parse(fs.readFileSync(
      path.join(repoRoot, "golden/parity", volume.slug, "tour.json"), "utf8"));
    const steps = [];
    let on = null;
    for (const step of tour.steps) {
      const grid = step.snapshot.grid;
      const touches = grid && (grid.enabled || step.name.includes("grid"));
      if (!touches) continue;
      const at = tourGround(volume.slug, step.snapshot.world, step.snapshot.lens);
      on = at;
      if (!grid.enabled) {
        steps.push({
          step: step.name,
          ground: at.key,
          enabled: false,
          system: grid.system,
          cell: grid.prefix,
          note: "The grid is put away: the app asks for no plan and draws no cells. The parity baseline records `grid.cells: []` at this step.",
          plan: [],
        });
        continue;
      }
      const subgridVisible = step.snapshot.ui.subgridVisible;
      const { plan, visuals } = await planFor(at, grid.system, grid.prefix, subgridVisible);
      checkAgainstTour(volume.slug, step, plan);
      assert.ok(same(await engine.invoke(at, grid.system, "bbox", [grid.prefix]), grid.extent),
        `${volume.slug}/${step.name}: the held cell's extent differs from the tour's`);
      steps.push({
        step: step.name,
        ground: at.key,
        enabled: true,
        system: grid.system,
        cell: grid.prefix,
        subgridVisible,
        plan,
        visuals,
      });
    }
    assert.ok(steps.length > 0, `${volume.slug}: no grid-touching steps`);
    written.push({
      volume: volume.slug,
      about: `Every grid-touching step of the parity tour for ${volume.title}, as the plan the current engine emits for the step's ground, system and held cell. Order is the contract.`,
      source: `golden/parity/${volume.slug}/tour.json`,
      ground: on.key,
      steps,
    });
  }

  // The contract cases the tour cannot reach. The tour holds geohash at
  // depths 0 and 1 on five volumes and never cycles to S2, so the roles and
  // the systems it leaves unexercised are pinned here on the fixture grounds
  // they belong to. Without these, the leaf role, the polar plan and the
  // whole of S2's emission order would land on M6 unpinned.
  const marsGround = tourGround("mars", "Global", "Viking MDIM 2.1");
  const contract = [];
  for (const spec of [
    { name: "geohash-depth-2", system: "geohash", cell: "m6", subgridVisible: true,
      note: "One level below anything the tour holds: 31 neighbours at distance 2, 31 at distance 1, the scope, and 32 children." },
    { name: "geohash-leaf", system: "geohash", cell: "m6s", subgridVisible: true,
      note: "The floor of the geohash telescope (depth 3). A cell at maxLevel emits as a `leaf` and its children are never asked for — the one role the tour never reaches." },
    { name: "geohash-leaf-subgrid-hidden", system: "geohash", cell: "m6s", subgridVisible: false,
      note: "The same plan with the subdivision put away: the plan is identical and only the style tokens move, which is the seam boundary this pair pins." },
    { name: "s2-root", system: "s2", cell: "", subgridVisible: true,
      note: "S2 at the root: the six faces, in face order, each ring a tessellated geodesic loop." },
    { name: "s2-polar-face", system: "s2", cell: "5", subgridVisible: true,
      note: "The north-polar face held: five neighbouring faces, the scope, four children — and the pole field carried on the cells that circle it." },
    { name: "s2-leaf", system: "s2", cell: "47a1cb", subgridVisible: true,
      note: "The S2 floor: port level 11 is maxLevel, so 47a1cb emits as a `leaf` after ten generations of neighbours — the deepest plan either system produces, and the positional order of ten ancestor rings." },
  ]) {
    const { plan, visuals } = await planFor(marsGround, spec.system, spec.cell, spec.subgridVisible);
    contract.push({
      step: spec.name,
      ground: marsGround.key,
      enabled: true,
      system: spec.system,
      cell: spec.cell,
      subgridVisible: spec.subgridVisible,
      note: spec.note,
      plan,
      visuals,
    });
  }
  written.push({
    volume: "contract",
    about: "The plans the tour cannot reach: the leaf role, the deeper geohash levels, and every S2 plan. Same ground as the Mars tour steps, so a difference here is a difference in the systems and not in the ground.",
    source: "issue #5 §5.4 — the guarantees the tour leaves unexercised",
    ground: marsGround.key,
    steps: contract,
  });
  return written;
}

// --- writing ---------------------------------------------------------------

function put(relative, value) {
  const at = path.join(here, relative);
  fs.mkdirSync(path.dirname(at), { recursive: true });
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (write) fs.writeFileSync(at, text);
  return { relative, bytes: text.length };
}

async function main() {
  await captureVectors();
  const plans = await capturePlans();

  // Each ground carries what it derives: the surface every system divides,
  // and which systems will divide it. Both are goldens — a candidate that
  // carries the ground differently still has to land on these numbers.
  for (const [, on] of grounds) {
    on.surfaceExtent = await engine.surfaceExtent(on);
    on.systems = await engine.applicableSystems(on);
  }

  const wrote = [];
  wrote.push(put("vectors/grounds.json", {
    about: "The grounds every vector and plan names. This is the shape issue #5 §5.4 de-globalizes into a `Ground` descriptor: the current engine reads each field out of the application's shared client state, and a case that names its ground here is a case that has declared its inputs. See README.md.",
    reads: {
      tileGridSize: "state.volume.tileGrid.size — the world square, the last fallback for the surface",
      "lens.surface": "state.lens.surface — the lens's declared surface, preferred when present",
      "lens.bounds": "state.lens.bounds — its raster window, used when no surface is declared",
      "world.attrs": "state.world.attrs — the semconv geometry keys: atlas.geometry.surface and the projection's px/deg mapping",
      surfaceExtent: "derived: [minX, minY, maxX, maxY] in OL world coordinates, x east and y negative-down. Recorded as a golden of its own.",
    },
    grounds: Object.fromEntries([...grounds].map(([key, value]) => [key, value])),
  }));
  for (const [name, value] of families) {
    wrote.push(put(`vectors/${name}.json`, value));
  }
  for (const value of plans) {
    wrote.push(put(`plans/${value.volume}.json`, value));
  }

  const cases = [...families.values()].reduce((total, value) => total + value.cases.length, 0);
  const planned = plans.reduce((total, value) =>
    total + value.steps.filter((step) => step.enabled).length, 0);
  const cells = plans.reduce((total, value) =>
    total + value.steps.reduce((count, step) => count + step.plan.length, 0), 0);
  for (const file of wrote) {
    console.log(`  ${file.relative.padEnd(28)} ${String(file.bytes).padStart(9)} bytes`);
  }
  console.log(`\n${grounds.size} grounds, ${families.size} vector families, ${cases} vectors, ` +
    `${planned} plans over ${cells} cells${write ? "" : " (checked, nothing written)"}`);
}

await main();
