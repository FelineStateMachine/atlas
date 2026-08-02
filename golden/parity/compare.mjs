// The parity gate: the golden tour, walked against the candidate build, and
// diffed against the baselines step by step.
//
//   node golden/parity/compare.mjs                      # the whole gate
//   node golden/parity/compare.mjs --only mars          # one volume
//   node golden/parity/compare.mjs base.json cand.json  # two logs, by hand
//
// This is the M5+M6 exit and the definition of done: `golden/harness` runs it
// as the `parity-compare` suite, and a green run means the rewritten
// application and its seam reproduce, field for field, what the reference
// implementation did on every shape the format can take.
//
// THREE RULES, and the first one is the whole discipline.
//
//   BASELINES ARE NEVER EDITED TO MATCH THE CANDIDATE. A difference is a bug
//   in the new code or it is a waiver in `golden/waivers.json` with a written
//   reason. There is no third answer, and there is deliberately no flag here
//   that would produce one.
//
//   ADVISORY FIELDS ARE RECORDED AND NOT COMPARED. Two of them, both counting
//   tiles fetched since the lens was chosen, both measuring the route two runs
//   took rather than the destination (SCHEMA.md §5). They stay in the log.
//
//   WAIVERS ARE PATHS, AND THEY ARE PRINTED. A waiver names snapshot paths
//   that are allowed to differ; every one of them is listed on every run, so
//   an accepted divergence stays visible as a cost rather than disappearing
//   into a green tick.

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fixtures, linkFarm } from "./library.mjs";
import { runTour } from "./run.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : fallback;
};
const has = (name) => args.includes(name);

/** The two fields a baseline records and does not bind (SCHEMA.md §5). */
const ADVISORY = ["tileStats.requested", "tileStats.loaded"];

// ---- diffing ----------------------------------------------------------

function* leaves(value, path = "") {
  if (value !== null && typeof value === "object") {
    const keys = Array.isArray(value)
      ? value.map((_, index) => index)
      : Object.keys(value).sort();
    if (keys.length === 0) {
      yield [path, Array.isArray(value) ? "[]" : "{}"];
      return;
    }
    for (const key of keys) {
      yield* leaves(value[key], path ? `${path}.${key}` : String(key));
    }
    return;
  }
  yield [path, JSON.stringify(value)];
}

/**
 * Whether a leaf is exempt.
 *
 * Advisory names match as dotted suffixes, which is how the reference
 * comparer spelled them and what lets one name cover `tileStats.requested`
 * wherever it appears. Waived names match as prefixes of a path, so
 * `library.lenses` covers every element of that array without a waiver having
 * to know how many there are.
 */
function exempt(path, waived) {
  if (ADVISORY.some((suffix) => path === suffix || path.endsWith(`.${suffix}`))) return true;
  return waived.some((prefix) => path === prefix || path.startsWith(`${prefix}.`));
}

function diffStep(name, left, right, waived) {
  const a = new Map(leaves(left));
  const b = new Map(leaves(right));
  const problems = [];
  for (const [path, value] of a) {
    if (exempt(path, waived)) continue;
    if (!b.has(path)) problems.push(`  ${path}: ${value} → (missing)`);
    else if (b.get(path) !== value) problems.push(`  ${path}: ${value} → ${b.get(path)}`);
  }
  for (const [path, value] of b) {
    if (!a.has(path) && !exempt(path, waived)) problems.push(`  ${path}: (missing) → ${value}`);
  }
  if (problems.length > 0) {
    console.log(`step ${name}:`);
    for (const problem of problems) console.log(problem);
  }
  return problems.length;
}

/** Diff two logs. Answers the number of differences that were not exempt. */
export function compare(baseline, candidate, waived = []) {
  let total = 0;
  const baselineSteps = baseline.steps.map((step) => step.name);
  const candidateSteps = candidate.steps.map((step) => step.name);
  if (JSON.stringify(baselineSteps) !== JSON.stringify(candidateSteps)) {
    console.log("step lists differ:");
    console.log(`  baseline:  ${baselineSteps.join(", ")}`);
    console.log(`  candidate: ${candidateSteps.join(", ")}`);
    total += 1;
  }
  for (const step of baseline.steps) {
    const twin = candidate.steps.find((other) => other.name === step.name);
    if (twin) total += diffStep(step.name, step.snapshot, twin.snapshot, waived);
  }
  return total;
}

// ---- the waivers ------------------------------------------------------

/**
 * The declared divergences for this suite.
 *
 * A `parity-compare` waiver's `fixture` names the volumes it applies to --
 * `*` for all of them -- and its `paths` name the snapshot leaves it covers.
 * Both live in `golden/waivers.json`, which is the one place an accepted
 * difference is allowed to exist, and the harness prints the file on every
 * run whether or not this gate is the one that read it.
 */
function waiversFor(slug) {
  const all = JSON.parse(readFileSync(join(repoRoot, "golden/waivers.json"), "utf8"));
  const mine = all.filter((waiver) => waiver.suite === "parity-compare" &&
    (waiver.fixture === "*" || waiver.fixture.split(/,\s*/).includes(slug)));
  return {
    paths: mine.flatMap((waiver) => waiver.paths ?? []),
    entries: mine,
  };
}

// ---- the gate ---------------------------------------------------------

async function gate() {
  const set = fixtures();
  const only = flag("--only");
  const wanted = only ? set.volumes.filter((v) => v.slug === only) : set.volumes;
  if (wanted.length === 0) {
    console.error(`no fixture volume named ${only}`);
    process.exit(2);
  }
  const farm = await linkFarm(set.volumes, { verify: has("--verify") });
  console.log(`fixture library: ${set.volumes.length} volumes linked into ${farm}`);

  let failed = 0;
  for (const volume of wanted) {
    const baselinePath = join(here, volume.slug, "tour.json");
    const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
    const { paths, entries } = waiversFor(volume.slug);
    console.log(`\n${volume.slug} (${volume.classification}) @ ${volume.shortStamp}`);
    let candidate;
    let red = false;
    try {
      candidate = await runTour({
        volume: volume.slug, bundles: farm,
        onLog: (line) => console.log(`  ${line}`),
      });
    } catch (error) {
      // A tour that finished red has still recorded a walk, and the walk is
      // what says where the build went wrong. The run is a failure either
      // way; the diff is printed anyway, because "the footer and the map
      // disagree" and "the footer disagrees with the baseline" are usually
      // the same defect seen from two sides.
      console.log(`  ${String(error.message ?? error).split("\n").join("\n  ")}`);
      red = true;
      if (!error.log) { failed += 1; continue; }
      candidate = error.log;
    }
    if (flag("--save")) {
      writeFileSync(join(flag("--save"), `${volume.slug}.json`),
        `${JSON.stringify(candidate, null, 2)}\n`);
    }
    const differences = compare(baseline, candidate, paths);
    for (const waiver of entries) {
      console.log(`  waived: ${waiver.id} — ${(waiver.paths ?? []).join(", ")}`);
    }
    if (differences === 0 && !red) {
      console.log(`  identical across ${baseline.steps.length} steps` +
        ` (ignoring ${ADVISORY.join(", ")}${paths.length ? `; waived ${paths.length} paths` : ""})`);
    } else {
      // One volume, one verdict: a walk that found the surfaces out of step
      // and then differed from its baseline is one failure seen twice, not
      // two failures.
      if (differences > 0) console.log(`  ${differences} differences`);
      failed += 1;
    }
  }
  if (failed > 0) {
    console.log(`\n${failed} of ${wanted.length} volumes differ from their baseline`);
    process.exit(1);
  }
  console.log(`\nall ${wanted.length} volumes agree with their baselines`);
}

// Two paths given by hand is the reference comparer's own usage, kept so a
// saved candidate can be re-diffed without re-walking anything.
const positional = args.filter((arg) => !arg.startsWith("--") &&
  args[args.indexOf(arg) - 1]?.startsWith("--") !== true);
if (positional.length === 2) {
  const [baselinePath, candidatePath] = positional;
  const slug = JSON.parse(readFileSync(candidatePath, "utf8")).volume;
  const total = compare(
    JSON.parse(readFileSync(baselinePath, "utf8")),
    JSON.parse(readFileSync(candidatePath, "utf8")),
    waiversFor(slug).paths,
  );
  if (total === 0) console.log("identical");
  else { console.log(`${total} differences`); process.exit(1); }
} else {
  await gate();
}
