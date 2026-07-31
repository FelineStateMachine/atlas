import { build } from "esbuild";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));

await build({
  absWorkingDir: root,
  entryPoints: ["app.js"],
  bundle: true,
  legalComments: "none",
  minify: true,
  outfile: resolve(root, "../assets/app.js"),
  target: ["safari15"],
});
