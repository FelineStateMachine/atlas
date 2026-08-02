// One parity tour against the rewritten application, from a fresh launch.
//
//   node golden/parity/run.mjs --volume <slug> [--bundles <dir>] [--out <file>]
//                              [--static <dir>] [--keep]
//
// THE FRESH-LAUNCH RULE, by construction. Every invocation starts its own
// `atlas serve` over its own session directory and its own browser session,
// and tears both down afterwards. Nothing is carried between volumes and
// nothing survives a run: the session store the rewrite keeps on disk is a
// temporary directory born here and removed at the end, which is the
// rewrite's answer to the reference tour clearing `localStorage` at both ends.
//
// WHAT IS LAUNCHED is `atlas serve` -- the headless host (docs/app.md §1),
// the successor of the reference build's ATLAS_HEADLESS=1 -- with the
// library pinned to the fixture link farm and the seam's built bundle mounted
// under /static. Both are required: an application with no seam serves a
// complete page and answers no viewport question, which is the deletability
// principle and not something to discover halfway through a tour.
//
// THE TOUR IS INJECTED rather than built in. It is harness code and the
// application under test carries no client JavaScript of its own beyond the
// seam's boot module, so `golden/parity/tour.js` is evaluated into the page
// and its log read back out of it. That is the rewrite's answer to the
// reference's dev-only /parity/result route, and it keeps the route out of
// the application.
//
// The browser is driven through playwright-cli via npx, so the only
// prerequisite beyond Node is a Playwright Chromium already installed.

import { spawn, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { decodePNG, describe } from "./pixels.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : fallback;
};

/**
 * Walk one volume and answer the tour's log.
 *
 * Exported so `compare.mjs` can run the six volumes in one process without
 * paying for a Node start-up each time; the command line below is the same
 * call with the arguments spelled out.
 */
export async function runTour({
  volume,
  bundles,
  staticDir = join(repoRoot, "dist/static"),
  onLog = () => {},
  // The extended half of the walk -- picks, keys and pictures (SCHEMA.md
  // §2.1). It is asked for rather than assumed, because the six committed
  // baselines were captured before it existed and a walk that took steps they
  // do not hold differs from them at the step list before any field is read.
  extended = false,
  // Where the pictures land. One directory per volume, made here; the driver
  // writes into it and this process reads them back to describe them. It is
  // deliberately *not* torn down with the session and the browser: the caller
  // is what compares the pictures, and it does that after this returns. A
  // caller with nowhere in mind gets a temporary directory and the operating
  // system's own sweeping.
  shots = extended ? mkdtempSync(join(tmpdir(), "atlas-parity-shots-")) : "",
}) {
  const sessions = mkdtempSync(join(tmpdir(), "atlas-parity-data-"));
  const cliDir = mkdtempSync(join(tmpdir(), "atlas-parity-cli-"));
  if (shots) mkdirSync(shots, { recursive: true });
  const cli = (...cliArgs) => {
    const result = spawnSync(
      "npx",
      ["--yes", "--package=@playwright/cli@latest", "playwright-cli", ...cliArgs],
      { cwd: cliDir, encoding: "utf8", timeout: 1_800_000, maxBuffer: 64 * 1024 * 1024 },
    );
    if (result.status !== 0) {
      throw new Error(`playwright-cli ${cliArgs[0]} failed:\n${result.stdout}\n${result.stderr}`);
    }
    return result.stdout;
  };

  const app = spawn("go", ["run", "./cmd/atlas", "serve",
    "-addr", "127.0.0.1:0",
    "-bundles", bundles,
    "-data", sessions,
    "-static", staticDir,
  ], { cwd: repoRoot, stdio: ["ignore", "pipe", "pipe"], detached: true });

  // `go run` fronts the real binary, so teardown signals the whole process
  // group rather than the runner alone. Ports are given back politely: the
  // host shuts its listener down on SIGTERM.
  const stopApp = () => {
    try { process.kill(-app.pid, "SIGTERM"); } catch { /* already gone */ }
  };
  process.on("exit", stopApp);

  try {
    const url = await new Promise((ready, fail) => {
      let out = "";
      let errors = "";
      const timer = setTimeout(
        () => fail(new Error(`atlas serve never printed an address:\n${errors}`)), 180_000);
      app.stdout.on("data", (chunk) => {
        out += chunk;
        const found = out.match(/https?:\/\/\S+/);
        if (found) { clearTimeout(timer); ready(found[0]); }
      });
      app.stderr.on("data", (chunk) => { errors += chunk; });
      app.on("exit", (code) => {
        clearTimeout(timer);
        fail(new Error(`atlas serve exited ${code} before serving:\n${errors}`));
      });
    });
    onLog(`serving ${url}`);

    const source = readFileSync(join(here, "tour.js"), "utf8");

    // `/` is the doorway: with no session behind it the application sends the
    // reader to a volume of its own choosing, and the tour's first act is to
    // put the select on the volume this run is about -- which is the same
    // first act the reference tour performed against the same doorway.
    cli("open", url);
    // The log comes back through the driver's own return value.
    //
    // An earlier draft had the page POST it to a little sink this process
    // opened beside the application, which is what the reference tour did
    // with its dev-only /parity/result route. A page served from one
    // localhost port cannot reach another one: Chromium drops the request
    // before the sink ever hears it, whatever the sink says about origins.
    // So the walk's log is returned rather than posted, and `--raw` makes the
    // driver's answer the whole of standard output.
    const answer = cli("run-code", `async page => {
      await page.waitForFunction(
        () => window.__atlasDebug && document.querySelectorAll(".category-row").length > 0,
        null, { timeout: 120000 });
      await page.addScriptTag({ content: ${JSON.stringify(source)} });
      // The walk is started rather than awaited, and then watched: a tour of
      // sixty steps takes minutes, and the step a stall happened on is the
      // only useful thing to know about a stall.
      await page.evaluate((options) => {
        window.__atlasTourDone = "";
        window.__atlasTour(options).then(
          (log) => { window.__atlasTourLog = log; window.__atlasTourDone = "walked"; },
          (error) => { window.__atlasTourDone = "threw: " + (error && error.stack || error); });
      }, ${JSON.stringify({ volume, extended })});
      let state = "";
      let at = "";
      // The same watch, now also the page's photographer. A screenshot step
      // publishes what it wants shot and waits; this takes it with the
      // browser's own screenshot -- the only way to see a WebGL sphere, which
      // has nothing to read back through a 2D context -- and says so. The
      // watch runs four times a second rather than once so that a walk of a
      // dozen pictures does not spend a dozen seconds waiting to be noticed.
      for (let waited = 0; waited < 1_200_000 && !state; waited += 250) {
        await page.waitForTimeout(250);
        const seen = await page.evaluate(() => ({
          at: window.__atlasTourAt || "",
          done: window.__atlasTourDone,
          want: window.__atlasShotWant || null,
        }));
        at = seen.at;
        state = seen.done;
        if (seen.want) {
          const want = seen.want;
          try {
            await page.locator(want.selector).first()
              .screenshot({ path: ${JSON.stringify(shots)} + "/" + want.file });
            await page.evaluate((name) => {
              window.__atlasShotWant = null;
              window.__atlasShotTaken = name;
            }, want.name);
          } catch (error) {
            await page.evaluate((said) => {
              window.__atlasShotWant = null;
              window.__atlasShotError = said.why;
              window.__atlasShotFailed = said.name;
            }, { name: want.name, why: String((error && error.message) || error) });
          }
        }
      }
      if (state !== "walked") {
        return JSON.stringify({ stalled: state || "no answer", at });
      }
      return JSON.stringify(await page.evaluate(() => window.__atlasTourLog));
    }`, "--raw");

    // `--raw` answers the returned string as a JSON string literal, so the
    // log is one unwrapping away -- unless the driver itself failed, in which
    // case it answers prose with a zero exit status and the unwrapping is the
    // only place that would notice. Said plainly here rather than as "is not
    // valid JSON" three frames down.
    let log;
    try {
      log = JSON.parse(JSON.parse(answer));
    } catch {
      throw new Error(`${volume}: the driver answered no log:\n${answer}`);
    }
    if (log.stalled) {
      throw new Error(`${volume}: the tour ${log.stalled} (last step: ${log.at || "none"})`);
    }
    onLog(`${log.steps.length} steps`);
    // The pictures, described and checked against the one thing a picture can
    // be judged on with no golden beside it.
    //
    // This is `checkCanvas` asked of a photograph rather than of a canvas, and
    // it is here rather than in the page because the sphere is the case that
    // matters and the sphere is WebGL: `getImageData` answers nothing there,
    // which is exactly why every globe step in six baselines could be right
    // about a black planet. A count of colours is a floor and not a
    // resemblance -- a sphere drawn black with its pins on it clears it -- and
    // the resemblance is the committed picture, compared in `compare.mjs`.
    log.shotsDir = shots;
    log.screens = [];
    for (const want of log.shots ?? []) {
      const path = join(shots, want.file);
      try {
        const picture = describe(decodePNG(readFileSync(path)));
        log.screens.push({ step: want.name, file: want.file, ...picture });
        if (want.nonBlank && picture.distinct <= 1) {
          log.problems.push(`${want.name}: the pane photographed as one flat colour`);
        }
      } catch (error) {
        log.problems.push(`${want.name}: the picture could not be read — ` +
          String(error.message ?? error));
      }
    }
    if (log.screens.length > 0) onLog(`${log.screens.length} pictures → ${shots}`);
    // A tour that finished but found the map, the footer and the dock telling
    // different stories has failed, whatever it recorded.
    if (log.problems.length > 0) {
      const error = new Error(
        [`${volume}: the tour found ${log.problems.length} problems`, ...log.problems].join("\n  "));
      error.log = log;
      throw error;
    }
    return log;
  } finally {
    try { cli("close"); } catch { /* browser already gone */ }
    stopApp();
    rmSync(sessions, { recursive: true, force: true });
    rmSync(cliDir, { recursive: true, force: true });
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const volume = flag("--volume");
  const bundles = flag("--bundles");
  if (!volume || !bundles) {
    console.error("usage: node golden/parity/run.mjs --volume <slug> --bundles <dir> [--out f]" +
      " [--extended] [--shots <dir>]");
    process.exit(2);
  }
  try {
    const extended = args.includes("--extended");
    const log = await runTour({
      volume, bundles: resolve(bundles),
      staticDir: resolve(flag("--static", join(repoRoot, "dist/static"))),
      onLog: (line) => console.log(line),
      extended,
      ...(flag("--shots") ? { shots: resolve(flag("--shots")) } : {}),
    });
    const out = flag("--out");
    if (out) writeFileSync(out, `${JSON.stringify(log, null, 2)}\n`);
    console.log(`${volume}: ${log.steps.length} steps${out ? ` → ${out}` : ""}`);
  } catch (error) {
    if (error.log && flag("--out")) {
      writeFileSync(flag("--out"), `${JSON.stringify(error.log, null, 2)}\n`);
    }
    console.error(String(error.message ?? error));
    process.exit(1);
  }
}
