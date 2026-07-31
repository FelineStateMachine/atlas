// Compares two parity-tour logs and reports every field that differs, step
// by step. Usage:
//
//   node compare.mjs baseline.json candidate.json [--ignore path,path,...]
//
// Ignore paths are dot-joined suffixes matched against the full path of a
// leaf value (for example "tileStats.requested" or "domNodes"). The exit
// code is 0 when the logs agree on everything not ignored, 1 otherwise.
import { readFileSync } from "node:fs";

const [, , baselinePath, candidatePath, ...rest] = process.argv;
if (!baselinePath || !candidatePath) {
  console.error("usage: node compare.mjs baseline.json candidate.json [--ignore a,b]");
  process.exit(2);
}
const ignoreFlag = rest.indexOf("--ignore");
const ignored = ignoreFlag >= 0 ? rest[ignoreFlag + 1].split(",") : [];

const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
const candidate = JSON.parse(readFileSync(candidatePath, "utf8"));

const isIgnored = (path) => ignored.some(
  (suffix) => path === suffix || path.endsWith(`.${suffix}`),
);

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

function diffStep(name, left, right) {
  const a = new Map(leaves(left));
  const b = new Map(leaves(right));
  const problems = [];
  for (const [path, value] of a) {
    if (isIgnored(path)) continue;
    if (!b.has(path)) problems.push(`  ${path}: ${value} → (missing)`);
    else if (b.get(path) !== value) problems.push(`  ${path}: ${value} → ${b.get(path)}`);
  }
  for (const [path, value] of b) {
    if (!a.has(path) && !isIgnored(path)) problems.push(`  ${path}: (missing) → ${value}`);
  }
  if (problems.length > 0) {
    console.log(`step ${name}:`);
    for (const problem of problems) console.log(problem);
  }
  return problems.length;
}

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
  if (twin) total += diffStep(step.name, step.snapshot, twin.snapshot);
}

if (total === 0) {
  console.log(`identical across ${baseline.steps.length} steps` +
    (ignored.length > 0 ? ` (ignoring ${ignored.join(", ")})` : ""));
} else {
  console.log(`${total} differences`);
  process.exit(1);
}
