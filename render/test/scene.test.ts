// The scene description, read off a synthetic node.
//
// `readScene` takes the smallest structural interface a real `Element`
// satisfies, so the contract in docs/app.md §4 can be tested without a
// browser: the node below is a hand-written stand-in for what
// `internal/app/templates/viewport.tmpl` renders, and it is the same shape
// the templates wave produces.

import test from "node:test";
import { strict as assert } from "node:assert";
import { EMPTY_SCENE, readCamera, readScene, sceneChange } from "../scene/read.ts";
import type { StateNode } from "../scene/read.ts";

/** A stand-in for the rendered `#atlas-viewport-state` node. */
function node(
  attributes: Record<string, string>,
  children: { className: string; value: string }[] = [],
): StateNode {
  return {
    getAttribute: (name) => attributes[name] ?? null,
    querySelectorAll: (selector) => children
      .filter((child) => selector === `data.${child.className}`)
      .map((child) => ({ getAttribute: (name: string) => name === "value" ? child.value : null })),
  };
}

const full = () => node({
  "data-volume": "bend-or",
  "data-base": "/data/v/bend-or/f0feba1cd00c",
  "data-world": "2026-08-02",
  "data-lens": "Basemap",
  "data-lens-index": "0",
  "data-surface": "plane",
  "data-selected": "1849",
  "data-search": "shrine",
  "data-grid-system": "geohash",
  "data-grid-cell": "9q5c",
  "data-subgrid": "1",
  "data-camera": "4096,-4096,1.34,0",
}, [
  { className: "hidden-collection", value: "12" },
  { className: "hidden-collection", value: "31" },
  { className: "highlighted-feature", value: "77" },
  { className: "label-override", value: "39191589=quiet" },
]);

test("every field of the state node arrives", () => {
  const scene = readScene(full());
  assert.equal(scene.volume, "bend-or");
  assert.equal(scene.base, "/data/v/bend-or/f0feba1cd00c");
  assert.equal(scene.world, "2026-08-02");
  assert.equal(scene.lens, "Basemap");
  assert.equal(scene.surface, "plane");
  assert.equal(scene.selected, "1849");
  assert.equal(scene.search, "shrine");
  assert.equal(scene.gridSystem, "geohash");
  assert.equal(scene.gridCell, "9q5c");
  assert.equal(scene.subgrid, 1);
  assert.deepEqual([...scene.hidden], ["12", "31"]);
  assert.deepEqual([...scene.highlighted], ["77"]);
  assert.equal(scene.overrides.get("39191589"), "quiet");
  assert.deepEqual(scene.camera, { x: 4096, y: -4096, zoom: 1.34, rotation: 0 });
});

test("a page with no volume open reads as the empty scene", () => {
  assert.deepEqual(readScene(null), EMPTY_SCENE);
  const bare = readScene(node({}));
  assert.equal(bare.volume, "");
  assert.equal(bare.surface, "plane");
  assert.equal(bare.camera, null);
  assert.equal(bare.hidden.size, 0);
});

test("a camera is four numbers or it is absent", () => {
  assert.deepEqual(readCamera("1,2,3,4"), { x: 1, y: 2, zoom: 3, rotation: 4 });
  // Until the seam has reported one, there is none — and half a camera
  // points somewhere nobody chose, so it is treated as none too.
  assert.equal(readCamera(""), null);
  assert.equal(readCamera("1,2,3"), null);
  assert.equal(readCamera("1,2,3,x"), null);
});

test("an override is read whole or not at all", () => {
  const scene = readScene(node({}, [
    { className: "label-override", value: "1=always" },
    { className: "label-override", value: "2=shouting" },
    { className: "label-override", value: "=quiet" },
    { className: "label-override", value: "3" },
  ]));
  assert.deepEqual([...scene.overrides], [["1", "always"]]);
});

test("what moved is named, so a swap reconciles rather than rebuilds", () => {
  const was = readScene(full());
  assert.equal(sceneChange(was, readScene(full())).any, false);

  const hidden = readScene(node({ ...attributesOf(full()), "data-search": "shrine" }, [
    { className: "hidden-collection", value: "12" },
  ]));
  const change = sceneChange(was, hidden);
  assert.equal(change.filters, true);
  assert.equal(change.world, false);
  assert.equal(change.lens, false);

  // A different world is a different everything: a lens change rides along,
  // because the lens list belongs to the world.
  const elsewhere = sceneChange(was, readScene(node({ "data-volume": "bend-or", "data-world": "other" })));
  assert.equal(elsewhere.world, true);
  assert.equal(elsewhere.lens, true);
});

function attributesOf(source: StateNode): Record<string, string> {
  const names = [
    "data-volume", "data-base", "data-world", "data-lens", "data-lens-index",
    "data-surface", "data-selected", "data-search", "data-grid-system",
    "data-grid-cell", "data-subgrid", "data-camera",
  ];
  const out: Record<string, string> = {};
  for (const name of names) {
    const value = source.getAttribute(name);
    if (value !== null) out[name] = value;
  }
  return out;
}
