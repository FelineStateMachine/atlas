// Runs the behavior-parity tour without a hand on the keyboard. The dev
// build (`go run -tags dev .`) is a desktop window, but it also serves the
// same frontend over plain HTTP and writes the port to inspector.url in the
// app's config dir -- so a headless Chromium can load the app, call
// window.__atlasTour() (the same entry F9 reaches), and let the tour post
// its log to the dev-only /parity/result route as ever.
//
// The fresh-launch rule is honored by construction: every invocation starts
// its own app process and a new browser session, and nothing touches either
// before the tour does. The tour itself clears localStorage at both ends.
//
//   node frontend/parity/run.mjs [--bundles <dir>] [--out <file>]
//
// --bundles pins ATLAS_BUNDLES_DIR so a recorded baseline names exactly one
// bundle build; --out copies the freshly written tour log there. The browser
// is driven through playwright-cli via npx, so the only prerequisite beyond
// Node is a Playwright Chromium already installed (npx playwright install
// chromium). macOS paths, like the dev loop itself.

import { spawn, spawnSync } from "node:child_process";
import { copyFileSync, mkdtempSync, readdirSync, readFileSync, statSync } from "node:fs";
import { tmpdir, homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const configDir = join(homedir(), "Library/Application Support/dev.felinestatemachine.atlas");
const parityDir = join(configDir, "parity");
const inspectorURL = join(configDir, "inspector.url");

const args = process.argv.slice(2);
const flag = (name) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : undefined;
};

const sleep = (ms) => new Promise((resolveSleep) => setTimeout(resolveSleep, ms));

// The playwright-cli keeps its session beside its working directory, so each
// run drives a browser of its own from a scratch dir and leaves nothing in
// the repo.
const cliDir = mkdtempSync(join(tmpdir(), "atlas-parity-"));
const cli = (...cliArgs) => {
  const result = spawnSync(
    "npx",
    ["--yes", "--package=@playwright/cli@latest", "playwright-cli", ...cliArgs],
    { cwd: cliDir, encoding: "utf8", timeout: 600_000 },
  );
  if (result.status !== 0) {
    throw new Error(`playwright-cli ${cliArgs[0]} failed:\n${result.stdout}\n${result.stderr}`);
  }
  return result.stdout;
};

// A launch is only this run's if its port file is newer than this moment:
// the file survives from earlier sessions and would otherwise answer for a
// server that is no longer there.
const launchedAt = Date.now();
// ATLAS_HEADLESS keeps the sweep out of the reader's way: the dev build
// serves the same frontend over HTTP and opens no window at all.
const app = spawn("go", ["run", "-tags", "dev", "."], {
  cwd: repoRoot,
  env: {
    ...process.env,
    ATLAS_HEADLESS: "1",
    ...(flag("--bundles") ? { ATLAS_BUNDLES_DIR: resolve(flag("--bundles")) } : {}),
  },
  stdio: "ignore",
  detached: true,
});

// `go run` fronts the real binary, so teardown signals the whole process
// group rather than the runner alone.
const stopApp = () => {
  try { process.kill(-app.pid, "SIGTERM"); } catch { /* already gone */ }
};
process.on("exit", stopApp);

try {
  let url = "";
  for (let i = 0; i < 60 && !url; i++) {
    await sleep(1000);
    try {
      if (statSync(inspectorURL).mtimeMs >= launchedAt) {
        url = readFileSync(inspectorURL, "utf8").trim();
      }
    } catch { /* not written yet */ }
  }
  if (!url) throw new Error("the dev build never wrote inspector.url");

  cli("open", url);
  const badge = cli("run-code", `async page => {
    await page.waitForFunction(() =>
      window.__atlasTour && window.__atlasDebug &&
      document.querySelectorAll(".category-row").length > 0, null, { timeout: 60000 });
    await page.evaluate(() => { window.__atlasTour(); });
    await page.waitForFunction(() => {
      const badge = document.querySelector("#parity-badge");
      return badge && (badge.textContent.includes("complete") || badge.textContent.includes("failed"));
    }, null, { timeout: 480000 });
    return await page.evaluate(() => document.querySelector("#parity-badge").textContent);
  }`);
  // A tour that finished but found the map, the footer and the dock telling
  // different stories fails here, with the mismatches read out of the log it
  // still wrote.
  // The badge's own words, not merely the word "complete": the CLI's output
  // wraps the value returned to us, and matching loosely let a tour that
  // finished red be reported green.
  if (!badge.includes("parity tour complete")) {
    const failed = readdirSync(parityDir)
      .filter((name) => name.startsWith("tour-") && name.endsWith(".json"))
      .map((name) => join(parityDir, name))
      .filter((path) => statSync(path).mtimeMs >= launchedAt)
      .sort((a, b) => statSync(b).mtimeMs - statSync(a).mtimeMs)[0];
    const found = failed ? JSON.parse(readFileSync(failed, "utf8")).problems || [] : [];
    throw new Error([`tour did not complete:\n${badge}`, ...found].join("\n  "));
  }

  // The tour posted its log to /parity/result, which writes a timestamped
  // file; the one belonging to this run is the one born after the launch.
  const newest = readdirSync(parityDir)
    .filter((name) => name.startsWith("tour-") && name.endsWith(".json"))
    .map((name) => join(parityDir, name))
    .filter((path) => statSync(path).mtimeMs >= launchedAt)
    .sort((a, b) => statSync(b).mtimeMs - statSync(a).mtimeMs)[0];
  if (!newest) throw new Error("the tour reported success but no result file arrived");

  const steps = JSON.parse(readFileSync(newest, "utf8")).steps.length;
  if (flag("--out")) copyFileSync(newest, flag("--out"));
  console.log(`${badge.match(/parity tour complete[^"]*/)?.[0] || "complete"}`);
  console.log(`result: ${flag("--out") || newest} (${steps} steps)`);
} finally {
  try { cli("close"); } catch { /* browser already gone */ }
  stopApp();
}
