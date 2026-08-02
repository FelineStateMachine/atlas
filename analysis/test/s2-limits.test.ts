// Where S2 stops being useful, pinned so nobody rediscovers it as a bug.
//
// `appliesTo` asks two questions — is this a sphere, and is there an invertible
// flattening — and a Web-Mercator city window answers yes to both. It is still
// not a ground the grid should be offered on, and the reason is arithmetic
// rather than taste. This file records the limit instead of quietly changing
// `appliesTo`: the vectors gate has no mercator ground and would not have
// caught the change, and a silent narrowing of a system's applicability is
// exactly the kind of divergence the clean-room rule exists to prevent.
//
// The fix, when someone wants it, is a curation decision in the app (offer S2
// only on whole-world lenses) or a `Ground` that says what fraction of a world
// its surface pictures. Both are M6+ conversations; neither is this lane's to
// make unilaterally. See docs/analysis.md, "the whole-world assumption".

import assert from "node:assert/strict";
import test from "node:test";

import { s2System } from "../cellsystems/s2.ts";
import { surfaceWidth } from "../cellsystems/ground.ts";
import { mercatorCity } from "./grounds.ts";

test("S2 accepts a city window, because the ground answers both its questions", () => {
  assert.equal(s2System.appliesTo(mercatorCity), true);
});

test("but the seam unwrap treats the surface as one whole world", () => {
  // The unwrap corrects any step longer than half the surface, on the theory
  // that a half-world jump in x is the antimeridian. On a lens picturing a
  // fifth of a degree, a planetary cell's edge is many surfaces wide and the
  // correction has nothing to do with the seam.
  const on = s2System.on(mercatorCity);
  const ring = on.ring("5");
  const half = surfaceWidth(mercatorCity) / 2;
  const jumps = ring.filter((point, at) => {
    const previous = ring[at - 1];
    return at > 0 && previous !== undefined && Math.abs(point[0] - previous[0]) > half;
  });
  assert.ok(jumps.length > 0,
    "a face-sized cell on a city window does not stay continuous, and cannot");
});

test("the cells that matter on such a ground are far below the seam problem", () => {
  // The limit is about coarse cells. A cell at the telescope's floor is a few
  // kilometres across, comfortably inside a city window, and behaves.
  const on = s2System.on(mercatorCity);
  const probe = [2048, -2048] as const;
  let id = "";
  for (let hop = 0; hop < 11; hop++) id = on.descendTarget(id, probe);
  assert.equal(on.level(id), 11);
  assert.equal(on.contains(id, probe), true);
  const ring = on.ring(id);
  const half = surfaceWidth(mercatorCity) / 2;
  for (let at = 1; at < ring.length; at++) {
    const current = ring[at];
    const previous = ring[at - 1];
    assert.ok(current && previous);
    assert.ok(Math.abs(current[0] - previous[0]) <= half, "a leaf cell is continuous");
  }
});
