// The seam's line budget, as a warning (issue #5 §5.5).
//
// The target is ~3,000 authored TypeScript lines, and it is a guideline, not
// a wall: it exists so that a seam quietly growing a second application
// inside itself is visible in CI rather than discovered later. So this counts,
// prints, and — past the budget — warns. It never fails a build.
//
// What counts as authored: code. Blank lines and comment-only lines are not
// counted, because a lane whose contract is its documentation should not be
// taxed for explaining itself; the totals are printed either way so the ratio
// stays visible. Tests are counted separately and are not part of the budget:
// a lane is not improved by being under-tested.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const BUDGET = 3000;

function files(directory) {
  const found = [];
  for (const name of readdirSync(directory)) {
    if (name === "node_modules" || name === "dist") continue;
    const path = join(directory, name);
    if (statSync(path).isDirectory()) found.push(...files(path));
    else if (name.endsWith(".ts")) found.push(path);
  }
  return found;
}

/** Code, comments and blanks, told apart well enough to be honest. */
function measure(source) {
  let code = 0;
  let comment = 0;
  let blank = 0;
  let inBlock = false;
  for (const raw of source.split("\n")) {
    const line = raw.trim();
    if (inBlock) {
      comment++;
      if (line.includes("*/")) inBlock = false;
      continue;
    }
    if (!line) blank++;
    else if (line.startsWith("//")) comment++;
    else if (line.startsWith("/*")) {
      comment++;
      inBlock = !line.includes("*/");
    } else code++;
  }
  return { code, comment, blank };
}

const rows = [];
let lane = { code: 0, comment: 0, blank: 0 };
let tests = { code: 0, comment: 0, blank: 0 };
for (const path of files(root).sort()) {
  const counts = measure(readFileSync(path, "utf8"));
  const where = relative(root, path);
  rows.push({ where, ...counts });
  const into = where.startsWith("test/") ? tests : lane;
  into.code += counts.code;
  into.comment += counts.comment;
  into.blank += counts.blank;
}

const width = Math.max(...rows.map((row) => row.where.length));
for (const row of rows) {
  process.stdout.write(
    `${row.where.padEnd(width)}  ${String(row.code).padStart(5)} code  ` +
    `${String(row.comment).padStart(5)} prose\n`);
}
process.stdout.write(
  `\nthe seam: ${lane.code} authored lines of code, ${lane.comment} of prose ` +
  `(budget ${BUDGET})\nits tests: ${tests.code} lines of code, ${tests.comment} of prose\n`);

if (lane.code > BUDGET) {
  process.stdout.write(
    `\n::warning::the seam is ${lane.code - BUDGET} lines past its ~${BUDGET}-line ` +
    `guideline (issue #5 §5.5). This is a warning, not a wall: either the seam ` +
    `has grown a second application inside it, or the budget wants revising in ` +
    `the issue. Both are decisions, and neither is silent.\n`);
}
