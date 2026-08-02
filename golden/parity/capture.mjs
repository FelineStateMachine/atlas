// Captures the golden parity baselines: one extended tour per fixture
// volume, each from a fresh application launch, written to
// golden/parity/<slug>/tour.json.
//
//   node golden/parity/capture.mjs [--only <slug>] [--private] [--twice] [--verify]
//   node golden/parity/capture.mjs --capture-extended [--only <slug>]
//
// The fixture volumes are named in FIXTURES.json beside this file, each
// pinned to one bundle file by manifest stamp and sha256. The installed
// registry holds many builds of many volumes and the newest-wins fold
// decides which serves, so a run against it would name whatever happened to
// be newest that day. Instead this assembles a link farm holding exactly the
// fixture files and points the application at that, which makes the fold a
// no-op and the baseline reproducible from the same files anywhere.
//
// One volume is not in the registry and never will be: the city was built
// for the fixture set rather than installed, by the pipeline commands in
// golden/fixtures/README.md, into a directory beside the repository. Its
// entry carries builtInto, the repository-relative directory it was built
// into, and ATLAS_GOLDEN_CITY_DIR moves that -- the same variable and the
// same dist/bundles default golden/capture/capture.sh uses, so a machine
// that keeps its builds elsewhere says so once for both captures.
//
// A volume that may not be named in the repository is described in
// FIXTURES.private.json, which git ignores, and captured only under
// --private: that pass adds the private volumes to the library and writes
// their baselines under private/, which git ignores too. The library a
// baseline was taken from is part of what the baseline records -- every
// snapshot lists the volumes on offer -- so the two passes are separate
// libraries rather than one, and the committed baselines never see a name
// that may not be committed.
//
// --twice captures each volume a second time and diffs the pair with
// compare.mjs, which is how a baseline earns the word reproducible: a step
// that is timing-unstable shows up here and is fixed with a settle wait in
// the tour, never by taking the unstable field out of the data. Two fields
// resist that treatment and are declared advisory in SCHEMA.md instead --
// they stay recorded, they are simply not compared, and the reason is
// written down beside them.
// --verify rehashes each fixture file before capturing. It is slow -- the
// set is half a gigabyte -- and worth it when a baseline is about to be
// committed.
//
// THIS SCRIPT RUNS AGAINST THE GOLDEN REFERENCE, WHICH IS NO LONGER HERE. It
// drives `frontend/parity/run.mjs`, and `frontend/` was archived with the rest
// of the old tree when the rewrite landed -- the `golden-reference` tag keeps
// it checkout-able. So this is re-capture equipment: check that tag out, run
// it there, and commit the baselines it writes. What runs against the *new*
// build is `compare.mjs` beside it, which shares this script's library farm
// through `library.mjs` and nothing else.
//
// ONE MODE HERE IS THE EXCEPTION, and it says so where it is written:
// --capture-extended runs against the rewrite, because the steps it captures
// are ones the reference never had. It appends and never re-takes, and it
// refuses a build that does not already reproduce its baseline. The argument
// is in full beside the code, and in SCHEMA.md §6.1.
//
// The registry is read-only to this script: it reads and links, never
// writes. macOS paths, like the dev loop itself.

import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { createReadStream } from "node:fs";
import { mkdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { compare, waiversFor } from "./compare.mjs";
import { runTour } from "./run.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");
const fixtures = JSON.parse(readFileSync(join(here, "FIXTURES.json"), "utf8"));

const args = process.argv.slice(2);
const flag = (name) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : undefined;
};
const has = (name) => args.includes(name);

const registry = process.env.ATLAS_GOLDEN_BUNDLES ||
  join(homedir(), "Library/Application Support/dev.felinestatemachine.atlas/bundles");

// Where a fixture file is read from: the installed library for the volumes
// that live there, and the directory it was built into for the one that does
// not. Both are read-only to this script.
const sourceOf = (volume) => volume.builtInto
  ? resolve(repoRoot, process.env.ATLAS_GOLDEN_CITY_DIR || volume.builtInto, volume.file)
  : join(registry, volume.file);

// The private pass reads the ignored companion file, puts its volumes on the
// same library as the public ones -- the library the reader actually has --
// and captures only those, out of the way.
//
// It stays additive now that the public city fills the city slot: the private
// volumes join the six rather than standing in for one of them. A swap would
// make the private baseline comparable with the public city's, which is not
// what it is for; it is a private record of the same tour over a volume that
// may not be named here, kept under the ignored private/ and never diffed
// against anything committed. So the private pass is a seven-volume library
// and says so in every one of its snapshots -- which is the honest reading of
// a machine that has seven volumes installed.
let priv = [];
if (has("--private")) {
  try {
    priv = JSON.parse(readFileSync(join(here, "FIXTURES.private.json"), "utf8")).volumes;
  } catch {
    console.error("--private asked for, and no readable FIXTURES.private.json beside this script");
    process.exit(2);
  }
}
const library = [...fixtures.volumes, ...priv];
const capturing = has("--private") ? priv : fixtures.volumes;

const wanted = flag("--only")
  ? capturing.filter((volume) => volume.slug === flag("--only"))
  : capturing;
if (wanted.length === 0) {
  console.error(`no fixture volume named ${flag("--only")}`);
  process.exit(2);
}

const sha256 = (path) => new Promise((done, fail) => {
  const hash = createHash("sha256");
  createReadStream(path).on("error", fail).on("data", (chunk) => hash.update(chunk))
    .on("end", () => done(hash.digest("hex")));
});

// The farm is rebuilt from scratch every run: a link left over from an
// earlier fixture set would put one more volume on the select and move every
// baseline's volume list.
const farm = join(here, ".bundles");
rmSync(farm, { recursive: true, force: true });
mkdirSync(farm, { recursive: true });
for (const volume of library) {
  const source = sourceOf(volume);
  let info;
  try {
    info = statSync(source);
  } catch {
    console.error(`${volume.slug}: no bundle at ${source}`);
    if (volume.builtInto) {
      console.error("this one is built rather than installed: golden/fixtures/README.md,");
      console.error("'The city fixture', or point ATLAS_GOLDEN_CITY_DIR at where it was built");
    }
    process.exit(1);
  }
  if (info.size !== volume.bytes) {
    console.error(`${volume.file}: ${info.size} bytes, the fixture names ${volume.bytes}`);
    process.exit(1);
  }
  if (has("--verify")) {
    const digest = await sha256(source);
    if (digest !== volume.sha256) {
      console.error(`${volume.file}: sha256 ${digest}, the fixture names ${volume.sha256}`);
      process.exit(1);
    }
  }
  symlinkSync(source, join(farm, volume.file));
}
console.log(`fixture library: ${library.length} volumes linked into ${farm}`);
console.log(`capturing: ${wanted.map((volume) => volume.slug).join(", ")}`);

// ---- the extended half's own capture ----------------------------------
//
//   node golden/parity/capture.mjs --capture-extended [--only <slug>]
//
// THIS ONE RUNS AGAINST THE REWRITE, and it is the only mode here that does.
// Everything above captures the reference implementation, which is a tag
// away; the picks, the keys and the pictures are steps the reference never
// had and never will, so there is nothing of theirs to reproduce. Their
// baseline is the finished rewrite, and that makes this the one capture that
// has to argue for itself:
//
//   NOTHING ALREADY CAPTURED IS RE-TAKEN. The committed snapshots are copied
//   through untouched and the new steps are appended after them. A mode that
//   re-captured the whole walk from the candidate would launder every value
//   the reference set, which is the one thing `compare.mjs` says there is no
//   flag for -- and there is no flag for it here either.
//
//   THE WALK IS HELD TO THE BASELINE FIRST. The shared steps are diffed
//   against the committed ones before a byte is written, under the same
//   waivers the gate reads. A build that differs from its baseline is not a
//   build to take new baselines from, and this refuses rather than recording.
//
//   A RED WALK IS NOT A BASELINE. The tour's own checks include the ones the
//   new steps brought -- ⌘K focusing the search field, a click in grid mode
//   telescoping into the cell, a keystroke in a text field not reaching the
//   shortcuts -- so the fixes those steps were written for must have landed
//   before this can write anything (SCHEMA.md §2.1.4). A red walk is caught,
//   printed with the problems that made it red, and refused; the other five
//   volumes are still walked, because the wave wants to hear about all of
//   them rather than about the first one to go wrong.
async function captureExtended(volumes, farm) {
  let refused = 0;
  for (const volume of volumes) {
    const dir = join(here, volume.slug);
    const baselinePath = join(dir, "tour.json");
    const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
    const screens = join(here, "screens", volume.slug);
    rmSync(screens, { recursive: true, force: true });
    mkdirSync(screens, { recursive: true });
    console.log(`\n${volume.slug} (${volume.classification}) @ ${volume.shortStamp}`);
    let walked;
    try {
      walked = await runTour({
        volume: volume.slug, bundles: farm, extended: true, shots: screens,
        onLog: (line) => console.log(`  ${line}`),
      });
    } catch (error) {
      // A red walk is refused rather than thrown out of: the wave wants to
      // hear about all six volumes, and the awaiting-fix table (SCHEMA.md
      // §2.1.4) is read against exactly these lines.
      console.error(`  ${String(error.message ?? error).split("\n").join("\n  ")}`);
      refused += 1;
      continue;
    }
    if (JSON.stringify(walked.viewport) !== JSON.stringify(baseline.viewport)) {
      console.error(`  ${volume.slug}: walked in ${walked.viewport.join("×")},` +
        ` the baseline was taken in ${baseline.viewport.join("×")}`);
      refused += 1;
      continue;
    }
    const shared = walked.steps.slice(0, baseline.steps.length);
    const differences = compare(baseline, { ...walked, steps: shared },
      waiversFor(volume.slug).entries);
    if (differences > 0) {
      console.error(`  ${volume.slug}: ${differences} differences from the committed baseline` +
        " — a build that differs is not a build to extend a baseline from");
      refused += 1;
      continue;
    }
    const added = walked.steps.slice(baseline.steps.length);
    const merged = {
      ...baseline,
      problems: [],
      steps: [...baseline.steps, ...added],
      shots: walked.shots ?? [],
    };
    writeFileSync(baselinePath, `${JSON.stringify(merged, null, 2)}\n`);
    console.log(`  ${added.length} steps appended (${baseline.steps.length} carried through)` +
      `, ${(walked.shots ?? []).length} pictures → screens/${volume.slug}`);
  }
  if (refused > 0) {
    console.error(`\n${refused} of ${volumes.length} volumes refused`);
    process.exit(1);
  }
  console.log("\nremember: the pictures are ignored by git until" +
    " golden/parity/screens/.gitignore says otherwise");
}

if (has("--capture-extended")) {
  await captureExtended(wanted, farm);
  process.exit(0);
}

const run = (slug, out) => {
  const result = spawnSync("node", [
    join(repoRoot, "frontend/parity/run.mjs"),
    "--bundles", farm, "--volume", slug, "--out", out,
  ], { cwd: repoRoot, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  if (result.status !== 0) {
    console.error(result.stdout);
    console.error(result.stderr);
    throw new Error(`${slug}: the tour did not complete`);
  }
  return result.stdout.trim();
};

// The two fields a baseline records but does not bind. Both count tiles
// fetched since the lens was chosen, which measures the route two runs took
// to the same destination rather than the destination; a browser that
// sampled one fly-to a frame differently fetches a tile the other never
// wanted, and no amount of settling undoes a request already made. Every
// other field of tileStats -- errors, peakPending -- binds as usual.
const advisory = ["tileStats.requested", "tileStats.loaded"];

let unstable = 0;
for (const volume of wanted) {
  const dir = has("--private") ? join(here, "private", volume.slug) : join(here, volume.slug);
  mkdirSync(dir, { recursive: true });
  const baseline = join(dir, "tour.json");
  console.log(`\n${volume.slug} (${volume.classification}) @ ${volume.shortStamp}`);
  console.log(run(volume.slug, baseline));
  if (!has("--twice")) continue;
  const second = join(tmpdir(), `atlas-golden-${volume.slug}.json`);
  console.log(run(volume.slug, second));
  const diff = spawnSync("node", [
    join(repoRoot, "frontend/parity/compare.mjs"), baseline, second,
    "--ignore", advisory.join(","),
  ], { encoding: "utf8" });
  process.stdout.write(diff.stdout);
  if (diff.status !== 0) {
    unstable += 1;
    console.error(`${volume.slug}: two runs of one build disagree -- not a baseline yet`);
  }
}

// The farm is left standing: it costs nothing (links, not copies), it is
// ignored by git, and a reader chasing a difference wants to point the
// application at exactly the library the baseline was taken from.
if (unstable > 0) process.exit(1);
