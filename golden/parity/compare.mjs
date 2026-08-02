// The parity gate: the golden tour, walked against the candidate build, and
// diffed against the baselines step by step.
//
//   node golden/parity/compare.mjs                      # the whole gate
//   node golden/parity/compare.mjs --only mars          # one volume
//   node golden/parity/compare.mjs base.json cand.json  # two logs, by hand
//   node golden/parity/compare.mjs --only mars --extended  # walk the picks,
//                                                          # keys and pictures
//                                                          # before the
//                                                          # baselines hold them
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
//   ADVISORY FIELDS ARE RECORDED AND NOT COMPARED. Three of them, all about
//   tiles since the lens was chosen, all measuring the route two runs took
//   rather than the destination (SCHEMA.md §5). They stay in the log.
//
//   WAIVERS ARE PATHS, AND THEY ARE PRINTED. A waiver names snapshot paths
//   that are allowed to differ; every one of them is listed on every run, so
//   an accepted divergence stays visible as a cost rather than disappearing
//   into a green tick.

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fixtures, linkFarm } from "./library.mjs";
import { comparePixels, decodePNG } from "./pixels.mjs";
import { runTour } from "./run.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : fallback;
};
const has = (name) => args.includes(name);

/** The three fields a baseline records and does not bind (SCHEMA.md §5). */
const ADVISORY = ["tileStats.requested", "tileStats.loaded", "tileStats.peakPending"];

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
 * Whether a leaf is exempt at this step.
 *
 * Advisory names match as dotted suffixes, which is how the reference
 * comparer spelled them and what lets one name cover `tileStats.requested`
 * wherever it appears. Waived names match as prefixes of a path, so
 * `library.lenses` covers every element of that array without a waiver having
 * to know how many there are.
 *
 * A waiver may also name the steps it covers. That is not a loosening: a
 * divergence that happens at one step of sixty is a smaller accepted cost than
 * a field waived across the whole walk, and spelling the step is what keeps
 * the same field bound everywhere else. A waiver naming no steps covers all of
 * them, which is what the three standing ones do.
 */
function exempt(path, step, waived) {
  if (ADVISORY.some((suffix) => path === suffix || path.endsWith(`.${suffix}`))) return true;
  return waived.some((waiver) =>
    (!waiver.steps || waiver.steps.includes(step)) &&
    (waiver.paths ?? []).some((prefix) => path === prefix || path.startsWith(`${prefix}.`)));
}

function diffStep(name, left, right, waived) {
  const a = new Map(leaves(left));
  const b = new Map(leaves(right));
  const problems = [];
  for (const [path, value] of a) {
    if (exempt(path, name, waived)) continue;
    if (!b.has(path)) problems.push(`  ${path}: ${value} → (missing)`);
    else if (b.get(path) !== value) problems.push(`  ${path}: ${value} → ${b.get(path)}`);
  }
  for (const [path, value] of b) {
    if (!a.has(path) && !exempt(path, name, waived)) {
      problems.push(`  ${path}: (missing) → ${value}`);
    }
  }
  if (problems.length > 0) {
    console.log(`step ${name}:`);
    for (const problem of problems) console.log(problem);
  }
  return problems.length;
}

/**
 * Diff two logs. Answers the number of differences that were not exempt.
 *
 * `waived` is the waiver *entries* rather than a flat list of paths, because a
 * waiver's paths and the steps it covers have to be read together.
 */
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

// ---- the pictures -----------------------------------------------------

/**
 * Whether this baseline was captured with the extended half walked.
 *
 * It is asked of the baseline rather than set by a flag, and that is what
 * makes the switch a one-way door with no lever beside it. The six committed
 * baselines hold no `pick-*` step, so the gate walks exactly the tour they
 * were taken from and stays green; the moment the final capture wave commits
 * baselines that do hold them, every run of this gate walks the picks, the
 * keys and the pictures too, on every volume, forever. Nothing has to
 * remember to turn it on.
 */
function extendedBaseline(baseline) {
  return baseline.steps.some((step) => step.name.startsWith("pick-"));
}

/**
 * Diff the walk's pictures against the committed ones.
 *
 * A picture with no committed twin is reported as not captured rather than as
 * a pass: the screenshot steps are written before the build they will be
 * captured from is finished (SCHEMA.md §2.1.3), and a gate that said "0
 * differences" over an empty directory would be lying in the most expensive
 * direction. Answers how many pictures differed.
 */
export function compareScreens(slug, candidate) {
  const shots = candidate.shots ?? [];
  if (shots.length === 0) return 0;
  const committed = join(here, "screens", slug);
  let differing = 0;
  let uncaptured = 0;
  for (const shot of shots) {
    const golden = join(committed, shot.file);
    const taken = join(candidate.shotsDir ?? "", shot.file);
    if (!existsSync(golden)) {
      uncaptured += 1;
      continue;
    }
    if (!existsSync(taken)) {
      console.log(`  picture ${shot.name}: the walk took none`);
      differing += 1;
      continue;
    }
    const verdict = comparePixels(
      decodePNG(readFileSync(golden)), decodePNG(readFileSync(taken)));
    if (!verdict.ok) {
      console.log(`  picture ${shot.name}: ${verdict.reason}`);
      differing += 1;
    }
  }
  const measured = shots.length - uncaptured;
  if (measured > 0) {
    console.log(`  ${measured - differing} of ${measured} pictures within` +
      " the threshold (golden/parity/pixels.mjs)");
  }
  if (uncaptured > 0) {
    console.log(`  ${uncaptured} picture${uncaptured === 1 ? "" : "s"} not captured:` +
      ` no committed twin under golden/parity/screens/${slug}`);
  }
  return differing;
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
export function waiversFor(slug) {
  const all = JSON.parse(readFileSync(join(repoRoot, "golden/waivers.json"), "utf8"));
  const mine = all.filter((waiver) => waiver.suite === "parity-compare" &&
    (waiver.fixture === "*" || waiver.fixture.split(/,\s*/).includes(slug)));
  return {
    paths: mine.flatMap((waiver) => waiver.paths ?? []),
    entries: mine,
  };
}

/** One waiver as one line of the run's standing bill. */
function billOf(waiver) {
  const paths = (waiver.paths ?? []).join(", ");
  return waiver.steps
    ? `${paths} (at ${waiver.steps.join(", ")})`
    : paths;
}

// ---- the gate ---------------------------------------------------------

/**
 * The one thing this gate needs that the others do not.
 *
 * Every other gate in the harness reads bytes; this one drives the real
 * application in a real browser, over the seam's built bundle. A tree that has
 * not built the seam cannot walk the tour at all -- and saying so is a
 * different answer from saying the build is wrong, which is the same bargain
 * `generate-enrich` makes with a machine that does not hold the capture
 * archive. It says which command would let it judge, and it does not pretend
 * to have judged.
 */
function seamIsBuilt() {
  return existsSync(join(repoRoot, "dist/static/app.js"));
}

async function gate() {
  if (!seamIsBuilt()) {
    console.log("not judged: there is no built seam at dist/static/app.js, and the tour" +
      " walks the real application over it. `make static` builds it; a Playwright" +
      " Chromium is the other prerequisite.");
    return;
  }
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
    const extended = extendedBaseline(baseline) || has("--extended");
    try {
      candidate = await runTour({
        volume: volume.slug, bundles: farm,
        onLog: (line) => console.log(`  ${line}`),
        extended,
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
    const differences = compare(baseline, candidate, entries) +
      compareScreens(volume.slug, candidate);
    for (const waiver of entries) {
      console.log(`  waived: ${waiver.id} — ${billOf(waiver)}`);
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
//
// Guarded, because `capture.mjs` imports `compare` from here to hold a
// re-capture to the baseline it is extending: a module that ran the whole
// half-hour gate on being imported would be a strange thing to import.
if (import.meta.url === `file://${process.argv[1]}`) {
  const positional = args.filter((arg) => !arg.startsWith("--") &&
    args[args.indexOf(arg) - 1]?.startsWith("--") !== true);
  if (positional.length === 2) {
    const [baselinePath, candidatePath] = positional;
    const slug = JSON.parse(readFileSync(candidatePath, "utf8")).volume;
    const total = compare(
      JSON.parse(readFileSync(baselinePath, "utf8")),
      JSON.parse(readFileSync(candidatePath, "utf8")),
      waiversFor(slug).entries,
    );
    if (total === 0) console.log("identical");
    else { console.log(`${total} differences`); process.exit(1); }
  } else {
    await gate();
  }
}
