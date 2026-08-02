// The render lane (issue #5 §5.5): the deletable seam that pictures a volume.
//
// Two custom elements are the whole surface. The lane stands on exactly three
// published contracts — the `/data` plane (docs/format.md), the scene
// description (docs/app.md §4), the analysis API (docs/analysis.md) — plus
// the diagnostics duty (golden/parity/SCHEMA.md). Nothing imports this
// package; `golden/depcheck` and the ESLint boundary rules say so, and
// `docs/render-seam.md` is the brief to rewrite it from those contracts
// alone.
//
// This file is the entry point the bundle is built from and the map of the
// lane. It exports the pieces a test needs and nothing a page does: a page
// gets the elements by loading the bundle, which registers them.

export { boot, register } from "./boot.ts";
export { AtlasViewport } from "./viewport.ts";
export { AtlasChart } from "./chart/element.ts";
export { AtlasGlobe } from "./globe/element.ts";
export { snapshot } from "./diagnostics.ts";
export type { SeamSnapshot } from "./diagnostics.ts";

export { LocationTable } from "./data/atlasloc.ts";
export { DataPlane } from "./data/plane.ts";
export { Coverage, LensCoverage, inventoryNames, tileWindowAt } from "./data/pyramid.ts";
export { readScene, sceneChange } from "./scene/read.ts";
export type { Scene, SceneChange } from "./scene/read.ts";
export { WorldModel, project, worldGrid } from "./world/model.ts";
export { Visibility } from "./world/visibility.ts";
export { logger, setLevel } from "./log.ts";
