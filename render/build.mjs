// The seam's build: one entry point, one file out.
//
// esbuild, no plugins, no framework, no CSS. The stylesheet system is the
// application's asset and is served from /assets by the application itself
// (docs/app.md §2.2); the seam contributes exactly one script, to /static,
// which is the tree the host mounted and which answers 404 when it mounted
// none. That separation is the deletability principle in the file layout:
// delete this lane and the page loses a script tag, not its stylesheet.

import { build, context } from "esbuild";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
const watch = process.argv.includes("--watch");
const outfile = resolve(root, process.env.ATLAS_SEAM_OUT ?? "dist/app.js");

/** @type {import("esbuild").BuildOptions} */
const options = {
  absWorkingDir: root,
  entryPoints: ["boot.ts"],
  bundle: true,
  format: "esm",
  target: ["safari16", "chrome110", "firefox110"],
  legalComments: "none",
  minify: !watch,
  sourcemap: watch,
  outfile,
  logLevel: "info",
};

if (watch) {
  const built = await context(options);
  await built.watch();
} else {
  await build(options);
}
