// The style tokens: pure, and the same numbers on both projections.

import assert from "node:assert/strict";
import test from "node:test";

import { cellPlan } from "../cellsystems/plan.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { gridCellVisual } from "../cellsystems/visual.ts";
import { gridTheme, palette } from "../cellsystems/tokens.ts";
import { square } from "./grounds.ts";

const plan = cellPlan(square, geohashSystem, "m6");
const of = (hash: string) => {
  const cell = plan.find((candidate) => candidate.hash === hash);
  assert.ok(cell, `${hash} is not in this plan`);
  return cell;
};
const tokens = (hash: string, subgridVisible: boolean, labelled: boolean) =>
  gridCellVisual(square, geohashSystem, of(hash), { subgridVisible, labelled });

test("the held cell draws as an outline while its subdivision is showing", () => {
  const scope = tokens("m6", true, true);
  assert.ok(scope);
  assert.equal(scope.bare, true);
  assert.equal(scope.line.color, gridTheme.lineWhite, "the chosen path draws white");
  assert.equal(scope.line.widthPx, gridTheme.widths.scopeBare);
  assert.equal(scope.fill, null, "a scope is not tiled");
  assert.equal(scope.label, null, "and it is not labelled either, while the subdivision reads");
});

test("putting the subdivision away thickens the boundary and gives it back its label", () => {
  const put = tokens("m6", false, true);
  assert.ok(put);
  assert.equal(put.bare, false);
  assert.equal(put.line.widthPx, gridTheme.widths.scope);
  assert.deepEqual(put.label, {
    prefix: "m",
    final: "6",
    color: gridTheme.lineWhite,
    textAlpha: 1,
    chip: gridTheme.chip,
    sizePx: gridTheme.labelSizePx,
  });
});

test("a child too small to label draws nothing at all", () => {
  assert.equal(tokens("m60", true, false), null, "no room for the label, no cell");
  assert.equal(tokens("m60", false, true), null, "subdivision put away, no cell");
  assert.equal(tokens("m60", false, false), null);
  const drawn = tokens("m60", true, true);
  assert.ok(drawn);
  assert.equal(drawn.line.opacity, gridTheme.childLineAlpha);
  assert.equal(drawn.fill?.opacity, gridTheme.childFillAlpha);
  assert.equal(drawn.line.color, palette[0], "child 0 wears the wheel's first accent");
});

test("a neighbour dims with its distance, and the dimming has a floor", () => {
  const near = tokens("m0", true, true);
  const far = tokens("0", true, true);
  assert.ok(near && far);
  assert.equal(near.fill?.color, gridTheme.dimColor);
  assert.equal(near.fill?.opacity, gridTheme.dimBase + 1 * gridTheme.dimStep);
  assert.equal(far.fill?.opacity, gridTheme.dimBase + 2 * gridTheme.dimStep);
  assert.equal(near.line.opacity, gridTheme.neighborLineAlpha);
  assert.equal(near.label?.sizePx, gridTheme.neighborLabelSizePx);
  assert.equal(near.label?.chip, gridTheme.neighborChip);
  // The cap keeps a deep hierarchy from fading to black.
  const capped = gridCellVisual(square, geohashSystem,
    { ...of("0"), contextDistance: 40 }, { subgridVisible: true, labelled: true });
  assert.equal(capped?.fill?.opacity, gridTheme.dimCap);
});

test("a leaf is filled in its own accent", () => {
  const leaf = cellPlan(square, geohashSystem, "m6s").at(-1);
  assert.ok(leaf);
  assert.equal(leaf.role, "leaf");
  const drawn = gridCellVisual(square, geohashSystem, leaf,
    { subgridVisible: true, labelled: true });
  assert.ok(drawn);
  assert.equal(drawn.bare, false, "there is no subdivision to be bare of");
  assert.equal(drawn.line.color, gridTheme.lineWhite);
  assert.equal(drawn.line.widthPx, gridTheme.widths.leaf);
  assert.equal(drawn.fill?.opacity, gridTheme.leafFillAlpha);
  assert.equal(drawn.fill?.color, palette[geohashSystem.on(square).colorKey("m6s")]);
});

test("the tokens are values: nothing a renderer holds can change them", () => {
  const once = tokens("m0", true, true);
  const twice = tokens("m0", true, true);
  assert.deepEqual(twice, once);
  assert.notEqual(twice, once, "a fresh object each time, so a mutating renderer cannot leak");
});
