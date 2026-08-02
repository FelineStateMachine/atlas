// The analysis-vectors gate (issue #5 §6, the `analysis-vectors` suite).
//
//   node golden/analysis/run.mjs           # every vector and every plan
//   node golden/analysis/run.mjs --verbose # name every case as it passes
//
// It runs on plain node — no browser, no bundler, no test framework — and it
// exits non-zero on the first family that disagrees. Every expectation is
// compared as JSON text: a vector byte for byte, a plan positionally, cell
// for cell, because the plan's emission order is the contract (§5.4).
//
// ────────────────────────────────────────────────────────────────────────────
// WHICH IMPLEMENTATION IS ON TRIAL (M6: the switch has been thrown)
//
// The line below is the switch, and it now names the clean lane. The gate
// judges `analysis/cellsystems` — the eight functions come out of
// `engine/cleanroom.mjs`, which adapts the lane's contract to them.
//
// `engine/current.mjs` stays where it is: it documents the oracle these
// fixtures were recorded from and it still runs, so `ATLAS_ANALYSIS_ENGINE=current`
// re-points the gate at the old tree for a side-by-side. The fixtures know
// about neither.
// ────────────────────────────────────────────────────────────────────────────
const engineModule = process.env.ATLAS_ANALYSIS_ENGINE === "current"
  ? "./engine/current.mjs"
  : "./engine/cleanroom.mjs";
const engine = await import(engineModule);

import fs from "node:fs";
import path from "node:path";

const here = path.dirname(new URL(import.meta.url).pathname);
const repoRoot = path.resolve(here, "..", "..");
const verbose = process.argv.includes("--verbose");

const read = (relative) => JSON.parse(fs.readFileSync(path.join(here, relative), "utf8"));

// JSON is where these goldens live, so JSON text is what "equal" means. It
// cannot carry a negative zero and the engine mints several (a surface whose
// top edge is y = -0), so normalizing both sides through the fixture's own
// serialization is the honest comparison rather than a loosened one.
const text = (value) => JSON.stringify(value);

const failures = [];

function fail(subject, expected, actual, extra) {
  failures.push({ subject, expected: text(expected), actual: text(actual), extra });
}

// --- the dispatch table ----------------------------------------------------
//
// Eight calls. Five stand on their own; everything else is a method of the
// 18-method contract, reached through `invoke`. A Go or TypeScript consumer
// of these fixtures needs exactly this table and nothing more.
async function evaluate(ground, spec) {
  switch (spec.call) {
    case "surfaceExtent":
      return engine.surfaceExtent(ground);
    case "applicableSystems":
      return engine.applicableSystems(ground);
    case "geohashCellAt":
      return engine.geohashCellAt(ground, spec.args[0], spec.args[1]);
    case "equivalentCell":
      return engine.equivalentCell(ground, spec.args[0], spec.args[1], spec.args[2]);
    case "clipRingX":
      return engine.clipRingX(spec.args[0], spec.args[1], spec.args[2]);
    default:
      return engine.invoke(ground, spec.system, spec.call, spec.args ?? []);
  }
}

// --- vectors ---------------------------------------------------------------

async function runVectors(grounds) {
  const files = fs.readdirSync(path.join(here, "vectors"))
    .filter((name) => name.endsWith(".json") && name !== "grounds.json")
    .sort();
  let count = 0;
  for (const file of files) {
    const family = read(path.join("vectors", file));
    for (const spec of family.cases) {
      const ground = grounds[spec.ground];
      const subject = `${family.family}/${spec.name}`;
      if (!ground) {
        fail(subject, `a ground named ${spec.ground}`, null, "grounds.json does not declare it");
        continue;
      }
      let value;
      try {
        value = await evaluate(ground, spec);
      } catch (error) {
        fail(subject, spec.expect, null, `threw: ${error.message}`);
        continue;
      }
      if (text(value) !== text(spec.expect)) {
        fail(subject, spec.expect, value, spec.handDerived ? "hand-derived golden" : undefined);
        continue;
      }
      count++;
      if (verbose) console.log(`  ok   ${subject}`);
    }
  }
  return { families: files.length, count };
}

// The ground descriptors are goldens themselves: surfaceExtent is derived
// from three of their fields, and a candidate that carries the ground
// differently still has to land on the same numbers.
async function runGrounds(grounds) {
  let count = 0;
  for (const [key, ground] of Object.entries(grounds)) {
    const extent = await engine.surfaceExtent(ground);
    if (text(extent) !== text(ground.surfaceExtent)) {
      fail(`grounds/${key}`, ground.surfaceExtent, extent, "the derived surface extent");
      continue;
    }
    const systems = await engine.applicableSystems(ground);
    if (text(systems) !== text(ground.systems)) {
      fail(`grounds/${key}`, ground.systems, systems, "the systems willing to divide it");
      continue;
    }
    count++;
    if (verbose) console.log(`  ok   grounds/${key}`);
  }
  return count;
}

// --- plans -----------------------------------------------------------------

// A plan is compared positionally: same length, then cell for cell in
// emission order. The first cell that differs is reported with its index,
// because "the twelfth cell moved" is the shape of the failure a reordering
// produces and a set comparison would hide it.
function comparePlan(subject, expected, actual) {
  if (expected.length !== actual.length) {
    fail(subject, `${expected.length} cells`, `${actual.length} cells`, "plan length");
    return false;
  }
  for (let at = 0; at < expected.length; at++) {
    if (text(expected[at]) !== text(actual[at])) {
      fail(`${subject}[${at}]`, expected[at], actual[at],
        `cell ${at} of ${expected.length}, emission order is the contract`);
      return false;
    }
  }
  return true;
}

async function runPlans(grounds) {
  const files = fs.readdirSync(path.join(here, "plans"))
    .filter((name) => name.endsWith(".json")).sort();
  let plans = 0;
  let cells = 0;
  for (const file of files) {
    const record = read(path.join("plans", file));
    const tour = record.source?.startsWith("golden/parity/")
      ? JSON.parse(fs.readFileSync(path.join(repoRoot, record.source), "utf8"))
      : null;
    for (const step of record.steps) {
      const subject = `${record.volume}/${step.step}`;
      const ground = grounds[step.ground];
      if (!ground) {
        fail(subject, `a ground named ${step.ground}`, null, "grounds.json does not declare it");
        continue;
      }
      if (!step.enabled) {
        // A step with the grid put away asks for no plan. The fixture says so
        // by carrying an empty one, and the engine is not called at all.
        if (step.plan.length !== 0) {
          fail(subject, [], step.plan, "a closed grid draws nothing; the fixture disagrees with itself");
        }
        continue;
      }
      const plan = await engine.cellPlan(ground, step.system, step.cell);
      if (!comparePlan(subject, step.plan, plan)) continue;
      let ok = true;
      for (let at = 0; at < plan.length && step.visuals; at++) {
        for (const [name, labelled] of [["labelled", true], ["unlabelled", false]]) {
          const want = step.visuals[at]?.[name];
          const got = await engine.cellVisual(ground, step.system, plan[at],
            { subgridVisible: step.subgridVisible, labelled });
          if (text(got) !== text(want)) {
            fail(`${subject}[${at}].${name}`, want, got, `style tokens for ${plan[at].hash}`);
            ok = false;
          }
        }
      }
      // The tie back to the parity baseline: the same cells, with the same
      // extents and roles, reached the screen on the step this plan was
      // recorded from. The tour's own order is OpenLayers' spatial index and
      // means nothing, so this half is a set check and the plan fixture above
      // is what pins the order.
      if (tour && ok) {
        const recorded = tour.steps.find((entry) => entry.name === step.step);
        if (!recorded) {
          fail(subject, `a tour step named ${step.step}`, null, record.source);
          ok = false;
        } else {
          const drawn = new Map(recorded.snapshot.grid.cells.map((cell) =>
            [cell.hash, { extent: cell.extent, role: cell.role, contextDistance: cell.contextDistance }]));
          for (const cell of plan) {
            const seen = drawn.get(cell.hash);
            const mine = { extent: cell.extent, role: cell.role, contextDistance: cell.contextDistance };
            if (!seen || text(seen) !== text(mine)) {
              fail(`${subject}:${cell.hash}`, seen ?? "drawn by the tour", mine,
                `the parity baseline (${record.source}) and this plan disagree`);
              ok = false;
              break;
            }
          }
          if (drawn.size !== plan.length) {
            fail(subject, `${drawn.size} cells on screen`, `${plan.length} planned`,
              `the parity baseline (${record.source}) drew a different number`);
            ok = false;
          }
        }
      }
      if (!ok) continue;
      plans++;
      cells += plan.length;
      if (verbose) console.log(`  ok   ${subject} (${plan.length} cells)`);
    }
  }
  return { files: files.length, plans, cells };
}

// --- the run ---------------------------------------------------------------

const grounds = read("vectors/grounds.json").grounds;

console.log(`analysis vectors — ${engine.engineName}\n`);

let groundsOK;
let vectors;
let plans;
try {
  groundsOK = await runGrounds(grounds);
  vectors = await runVectors(grounds);
  plans = await runPlans(grounds);
} catch (error) {
  // The engine failing to load at all is a setup fault, not a difference
  // from a golden, and it reads better as one line than as a stack.
  console.log(`  FAIL  the engine could not be driven\n        ${error.message}`);
  console.log("\nanalysis vectors: the engine could not be driven");
  process.exit(1);
}

if (failures.length > 0) {
  console.log();
  for (const failure of failures.slice(0, 12)) {
    console.log(`  FAIL  ${failure.subject}${failure.extra ? `  (${failure.extra})` : ""}`);
    console.log(`        golden:    ${failure.expected}`);
    console.log(`        candidate: ${failure.actual}`);
  }
  if (failures.length > 12) console.log(`  … and ${failures.length - 12} more`);
  console.log(
    "\nThe goldens are never edited to match the candidate (golden/HARNESS.md). " +
    "Fix the candidate, or write a waiver in golden/waivers.json with a reason.",
  );
  console.log(`\n${failures.length} differences from the goldens`);
  process.exit(1);
}

console.log(`${groundsOK} grounds, ${vectors.count} vectors in ${vectors.families} families, ` +
  `${plans.plans} plans over ${plans.cells} cells in ${plans.files} files: all byte-exact, ` +
  `plan order positional`);
