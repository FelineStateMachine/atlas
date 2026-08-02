// The clean lane's cell systems, driven from plain node (issue #5 §5.4, M6).
//
// The sibling of `current.mjs`, and the reason that file's banner exists: the
// gate imports one module and calls eight functions, so re-pointing it at
// `analysis/cellsystems` was a one-line change in `run.mjs` and nothing in the
// fixtures moved. The vectors and plans were recorded from the old
// implementation before this one was written, which is what makes them an
// oracle rather than a transcript.
//
// This adapter is much smaller than `current.mjs`, and every line it does not
// have is the de-globalization: there is no `applyGround` writing a recorded
// descriptor back into module state, no loader hook swapping four
// application-shaped imports for stubs, and no session fields to set before
// asking for a plan. A ground is an argument. The held cell is an argument.
//
// The lane is TypeScript; node strips the types on import (22.18+ / 24+), so
// there is still no bundler in the gate.
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..");
const lane = pathToFileURL(path.join(repoRoot, "analysis", "cellsystems", "index.ts")).href;

export const engineName = "analysis/cellsystems — the clean-room lane (issue #5 §5.4)";

let loaded = null;

// s2js resolves from the repository root's node_modules, where the analysis
// workspace installs it. A missing install is a setup fault and reads better
// as one sentence than as a resolver stack.
async function engine() {
  if (loaded) return loaded;
  try {
    loaded = await import(lane);
  } catch (error) {
    throw new Error(
      `golden/analysis: the analysis lane could not be loaded (${error.message}). ` +
      "It needs its one dependency and a node that strips types: `npm ci` at the " +
      "repository root, node 22.18+.",
    );
  }
  return loaded;
}

function systemOf(held, slug) {
  return held.cellSystems.require(slug);
}

// --- the eight functions an engine adapter provides ------------------------

export async function surfaceExtent(ground) {
  const held = await engine();
  return held.surfaceExtent(ground);
}

export async function applicableSystems(ground) {
  const held = await engine();
  return held.applicableSystems(ground).map((system) => system.slug);
}

// invoke calls one method of one system's 18-method contract. Three of the
// eighteen answer about the ground before any cell is named — appliesTo,
// maxLevel, inputLength — and live on the system itself; the other fifteen
// are reached through `on(ground)`, which is where the ground stops being
// implicit.
export async function invoke(ground, slug, method, args) {
  const held = await engine();
  const system = systemOf(held, slug);
  if (method === "appliesTo" || method === "maxLevel" || method === "inputLength") {
    return system[method](ground);
  }
  const grounded = system.on(ground);
  if (typeof grounded[method] !== "function") {
    throw new Error(`golden/analysis: ${slug} has no method ${method}`);
  }
  return grounded[method](...args);
}

// geohashCellAt is the one function outside the contract the app calls by
// name: the pin card's fixed-depth address.
export async function geohashCellAt(ground, coordinate, depth) {
  const held = await engine();
  return held.geohashCellAt(ground, coordinate, depth);
}

export async function equivalentCell(ground, fromSlug, toSlug, id) {
  const held = await engine();
  return held.equivalentCell(ground, systemOf(held, fromSlug), systemOf(held, toSlug), id);
}

export async function clipRingX(ring, minimumX, maximumX) {
  const held = await engine();
  return held.clipRingX(ring, minimumX, maximumX);
}

// cellPlan is issue #5 §5.4's `cellPlan(ground, system, cellID)`, spelled
// exactly that way in the lane. Emission order is the contract; nothing sorts
// what comes back.
export async function cellPlan(ground, slug, cellID) {
  const held = await engine();
  return held.cellPlan(ground, systemOf(held, slug), cellID);
}

// cellVisual is the pure style-token seam: the plan cell plus the two
// renderer-side questions (is the subgrid showing, does the label fit).
export async function cellVisual(ground, slug, cell, options) {
  const held = await engine();
  return held.gridCellVisual(ground, systemOf(held, slug), cell, options);
}
