// The module-resolution rule that swaps the application-shaped imports of
// `frontend/src/grid.js` for the inert stubs beside this file.
//
// Node's own loader does the work, so the cell math is loaded from its real
// source files, unbundled and unedited — which is the point: the vectors are
// a recording of the current implementation, and anything that rewrote the
// source before running it would weaken that.
//
// The rule is exported for `registerHooks` (node 22.15+, in-thread and
// synchronous) and this file doubles as the hooks module for `register` on
// older runtimes, which loads it on a thread of its own.

const swapped = new Set(["./dom.js", "./detail.js", "./navigation.js", "./features.js"]);

export function stubbed(specifier, parentURL) {
  return swapped.has(specifier) && (parentURL || "").includes("/frontend/src/");
}

let stubURL = "";

export function initialize(data) {
  stubURL = data.stubURL;
}

export async function resolve(specifier, context, next) {
  if (stubbed(specifier, context.parentURL)) return { url: stubURL, shortCircuit: true };
  return next(specifier, context);
}
