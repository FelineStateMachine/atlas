// The shared contract vectors, answered by the lane that owns the arithmetic.
//
// `analysis/testdata/cells/` is the language-neutral half of the two-lane
// bargain: the same JSON is read by this suite and by `tests/cells/` on the Go
// side, so the two copies of the cell arithmetic are held to one set of
// numbers rather than to each other's company. The hand-derived cases are
// marked `handDerived: true` — the geohash ones halved out on paper, the S2
// ones the Go library's own test values — and every ground is either one of
// those derivations or a descriptor read from a public corpus bundle.
//
// Everything is compared as JSON text, byte for byte. That is not laziness:
// JSON is the only form both lanes share, it cannot carry a negative zero
// (which the surface arithmetic mints and both lanes must normalize away),
// and a comparison the fixture's own serialization defines is one no lane can
// quietly loosen.

import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { describe, it } from "node:test";
import path from "node:path";
import { fileURLToPath } from "node:url";

import type { GroundedCellSystem } from "../cellsystems/contract.ts";
import type { Coordinate, Ground } from "../cellsystems/ground.ts";
import { surfaceExtent } from "../cellsystems/ground.ts";
import { applicableSystems, cellSystems, equivalentCell } from "../cellsystems/registry.ts";
import { geohashCellAt } from "../cellsystems/geohash.ts";
import { clipRingX } from "../cellsystems/ring.ts";
import type { Ring } from "../cellsystems/contract.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const vectorsDir = path.join(here, "..", "testdata", "cells");

/** One recorded ground: a Ground descriptor plus the two derived goldens. */
interface RecordedGround extends Ground {
  readonly key: string;
  readonly surfaceExtent: readonly number[];
  readonly systems: readonly string[];
}

/** One recorded call. Arguments are heterogeneous by family, so they stay unknown. */
interface Vector {
  readonly name: string;
  readonly ground: string;
  readonly system?: string;
  readonly call: string;
  readonly args?: readonly unknown[];
  readonly expect: unknown;
  readonly handDerived?: boolean;
}

const readJSON = (name: string): unknown =>
  JSON.parse(readFileSync(path.join(vectorsDir, name), "utf8"));

const grounds = (readJSON("grounds.json") as { grounds: Record<string, RecordedGround> }).grounds;

const families = readdirSync(vectorsDir)
  .filter((name) => name.endsWith(".json") && name !== "grounds.json")
  .sort();

/** JSON text is what "equal" means here; see the header. */
const text = (value: unknown): string => JSON.stringify(value) ?? "null";

/**
 * The whole dispatch table a consumer of the vectors needs: four calls that
 * stand on their own, three contract methods that answer about the ground,
 * and the grounded fifteen behind everything else.
 */
function evaluate(ground: Ground, spec: Vector): unknown {
  const args = spec.args ?? [];
  switch (spec.call) {
    case "surfaceExtent":
      return surfaceExtent(ground);
    case "applicableSystems":
      return applicableSystems(ground).map((system) => system.slug);
    case "geohashCellAt":
      return geohashCellAt(ground, args[0] as Coordinate, args[1] as number);
    case "equivalentCell":
      return equivalentCell(
        ground,
        cellSystems.require(args[0] as string),
        cellSystems.require(args[1] as string),
        args[2] as string,
      );
    case "clipRingX":
      return clipRingX(args[0] as Ring, args[1] as number, args[2] as number);
  }
  assert.ok(spec.system, `${spec.name}: a contract call must name its system`);
  const system = cellSystems.require(spec.system);
  if (spec.call === "appliesTo" || spec.call === "maxLevel" || spec.call === "inputLength") {
    return system[spec.call](ground);
  }
  const grounded = system.on(ground);
  const method = grounded[spec.call as keyof GroundedCellSystem] as unknown;
  assert.equal(typeof method, "function", `${spec.system} has no method ${spec.call}`);
  return (method as (...rest: unknown[]) => unknown)(...args);
}

describe("the shared vectors: grounds", () => {
  // The four hand-derived grounds and the two corpus ones, by name: a fixture
  // that silently lost a ground would otherwise pass by answering nothing.
  it("records the six grounds both lanes stand on", () => {
    assert.deepEqual(Object.keys(grounds).sort(), [
      "bend-or/2026-08-02/0",
      "mars/global/0",
      "test/sphere-8192x4096",
      "test/square-1024",
      "test/square-1024-bounds",
      "test/square-1024-no-lens",
    ]);
  });

  // The descriptors carry their own derived goldens: the rectangle every
  // system divides, and which systems are willing to divide it. A candidate
  // free to carry the ground differently still has to land on these numbers.
  for (const [key, ground] of Object.entries(grounds)) {
    it(`${key} derives its own surface and systems`, () => {
      assert.equal(text(surfaceExtent(ground)), text(ground.surfaceExtent));
      assert.equal(
        text(applicableSystems(ground).map((system) => system.slug)),
        text(ground.systems),
      );
    });
  }
});

for (const file of families) {
  const family = readJSON(file) as { family: string; cases: Vector[] };
  describe(`the shared vectors: ${family.family}`, () => {
    assert.ok(family.cases.length > 0, `${file} records no cases`);
    for (const spec of family.cases) {
      it(spec.name, () => {
        const ground = grounds[spec.ground];
        assert.ok(ground, `${file} names a ground grounds.json does not record: ${spec.ground}`);
        const value = evaluate(ground, spec);
        assert.equal(
          text(value),
          text(spec.expect),
          `${family.family}/${spec.name}${spec.handDerived ? " (hand-derived)" : ""}`,
        );
      });
    }
  });
}
