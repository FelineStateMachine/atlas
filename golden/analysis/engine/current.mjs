// The current tree's cell systems, driven from plain node.
//
// This is the adapter the vector gate runs against, and it is the one file
// that knows where the implementation lives. When M6 lands the clean
// `analysis/cellsystems` lane, write a sibling adapter exporting the same
// eight functions and change the single import line at the top of
// `golden/analysis/run.mjs`. Nothing else in the gate, and nothing in the
// fixtures, refers to the implementation at all.
//
// The current implementation reaches into the application's shared client
// state for its ground — `state.volume.tileGrid.size`, `state.lens.surface`
// or `.bounds`, `state.world.attrs` — which is exactly what issue #5 §5.4
// de-globalizes into a `Ground` descriptor. `applyGround` is the seam where
// that happens today: it takes the recorded descriptor and writes it back
// into the globals the engine reads, so the fixtures can be language-neutral
// while the engine under test still is not.
import { createRequire, register, registerHooks } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

import { stubbed } from "./hooks.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..");
const frontend = path.join(repoRoot, "frontend");

export const engineName = "frontend/src — the current tree (issue #5's golden reference)";

// The engine's own dependency (s2js) and the frontend's module graph both
// resolve from frontend/, so a missing install is a clear message rather
// than a stack trace out of node's resolver.
function requireInstall() {
  try {
    createRequire(path.join(frontend, "package.json")).resolve("s2js");
  } catch {
    throw new Error(
      "golden/analysis: frontend/node_modules is missing — the current-tree engine needs " +
      "s2js and OpenLayers to run. Install them with `npm --prefix frontend ci`.",
    );
  }
}

let loaded = null;

async function engine() {
  if (loaded) return loaded;
  requireInstall();
  const stubURL = pathToFileURL(path.join(here, "stubs.mjs")).href;
  if (typeof registerHooks === "function") {
    // node 22.15+: the hook runs in this thread, synchronously.
    registerHooks({
      resolve(specifier, context, next) {
        if (stubbed(specifier, context.parentURL)) return { url: stubURL, shortCircuit: true };
        return next(specifier, context);
      },
    });
  } else {
    register(pathToFileURL(path.join(here, "hooks.mjs")), {
      parentURL: import.meta.url,
      data: { stubURL },
    });
  }
  const src = (name) => pathToFileURL(path.join(frontend, "src", name)).href;
  const [state, cellsystems, geohash, s2, grid] = await Promise.all([
    import(src("state.js")),
    import(src("cellsystems/index.js")),
    import(src("cellsystems/geohash.js")),
    import(src("cellsystems/s2.js")),
    import(src("grid.js")),
  ]);
  loaded = { state: state.state, cellsystems, geohash, s2, grid };
  return loaded;
}

// applyGround writes a recorded ground descriptor into the implicit global
// state the current engine reads it from. The descriptor's shape is the
// shape M6's `Ground` has to carry; see golden/analysis/README.md.
function applyGround(held, ground) {
  held.state.volume = ground.tileGridSize === null
    ? null
    : { tileGrid: { size: ground.tileGridSize } };
  held.state.lens = ground.lens === null ? null : {
    ...(ground.lens.surface ? { surface: ground.lens.surface } : {}),
    ...(ground.lens.bounds ? { bounds: ground.lens.bounds } : {}),
  };
  held.state.world = { attrs: ground.world.attrs };
}

function systemOf(held, slug) {
  const system = held.cellsystems.cellSystems.find((candidate) => candidate.slug === slug);
  if (!system) throw new Error(`golden/analysis: no cell system named ${slug}`);
  return system;
}

// --- the eight functions an engine adapter provides ------------------------

export async function surfaceExtent(ground) {
  const held = await engine();
  applyGround(held, ground);
  return held.cellsystems.surfaceExtent();
}

export async function applicableSystems(ground) {
  const held = await engine();
  applyGround(held, ground);
  return held.cellsystems.applicableSystems(held.state.world).map((system) => system.slug);
}

// invoke calls one method of one system's 18-method contract. `map`-taking
// methods (appliesTo, maxLevel, inputLength) are handed the ground's world,
// so a vector case never has to spell the world twice.
export async function invoke(ground, slug, method, args) {
  const held = await engine();
  applyGround(held, ground);
  const system = systemOf(held, slug);
  if (typeof system[method] !== "function") {
    throw new Error(`golden/analysis: ${slug} has no method ${method}`);
  }
  if (method === "appliesTo" || method === "maxLevel" || method === "inputLength") {
    return system[method](held.state.world);
  }
  return system[method](...args);
}

// geohashCellAt is the one function outside the contract the app calls by
// name: the pin card's fixed-depth address.
export async function geohashCellAt(ground, coordinate, depth) {
  const held = await engine();
  applyGround(held, ground);
  return held.geohash.geohashCellAt(coordinate, depth);
}

export async function equivalentCell(ground, fromSlug, toSlug, id) {
  const held = await engine();
  applyGround(held, ground);
  return held.cellsystems.equivalentCell(
    systemOf(held, fromSlug),
    systemOf(held, toSlug),
    id,
    held.state.world,
  );
}

export async function clipRingX(ring, minimumX, maximumX) {
  const held = await engine();
  return held.cellsystems.clipRingX(ring, minimumX, maximumX);
}

// cellPlan is issue #5 §5.4's `cellPlan(ground, system, cellID)`: the
// current tree spells it `gridCellPlan()` and reads the system and the held
// cell off the session state, so the arguments are written into the globals
// here. Emission order is the contract; nothing sorts what comes back.
export async function cellPlan(ground, slug, cellID) {
  const held = await engine();
  applyGround(held, ground);
  held.state.gridSystem = slug;
  held.state.gridCell = cellID;
  return held.grid.gridCellPlan();
}

// cellVisual is the pure style-token seam: the plan cell plus the two
// renderer-side questions (is the subgrid showing, does the label fit).
export async function cellVisual(ground, slug, cell, options) {
  const held = await engine();
  applyGround(held, ground);
  held.state.gridSystem = slug;
  return held.grid.gridCellVisual(cell, options);
}
