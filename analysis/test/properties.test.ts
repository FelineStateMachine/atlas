// The properties that used to be recorded, now asserted.
//
// The golden layer once pinned twenty-eight recorded plans and re-checked
// them against captured tours. The recordings are gone with the layer, and
// what stands in their place is this file: the invariants the recordings were
// evidence *of*, written as properties and run over invented grounds and
// invented locations — numbers no capture ever contained, so nothing here can
// be a transcript of an implementation agreeing with itself.
//
// Four properties, each over a plane and a sphere:
//
//   Containment.  A child cell's extent lies within its parent's, and the
//                 parent contains what the child holds.
//   Hierarchy.    parent() and children() are inverse both ways, and the
//                 ordinal is the position in the stable child order.
//   Carry.        The ⌘G crossing (equivalentCell): the place and the
//                 precision survive, the address does not.
//   Plan order.   cellPlan emits in the documented positional order, and
//                 nothing follows a leaf.

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import type { CellID, CellSystem, GroundedCellSystem } from "../cellsystems/contract.ts";
import type { Coordinate, Extent, Ground } from "../cellsystems/ground.ts";
import { extentArea } from "../cellsystems/ground.ts";
import { cellPlan } from "../cellsystems/plan.ts";
import { equivalentCell } from "../cellsystems/registry.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { s2System } from "../cellsystems/s2.ts";
import { districtSystem } from "./districts.ts";

// --- the invented worlds -----------------------------------------------------
//
// Deliberately none of the recorded numbers: a 2048 world with an off-centre
// lens window, and a 4096x2048 sphere half the recorded one's size. If these
// tests pass because an implementation memorized a fixture, they will fail
// here, which is the point of inventing.

/** A 2048 plane whose lens covers an off-centre window, districted for the third system. */
const field: Ground = {
  tileGridSize: 2048,
  lens: { surface: { x: 96, y: 160, width: 1536, height: 1280 }, bounds: null },
  world: { attrs: { "atlas.district.grid": "3x3" } },
};

/** A 4096x2048 world square declared as a whole equirectangular sphere. */
const globe: Ground = {
  tileGridSize: 4096,
  lens: { surface: { x: 0, y: 0, width: 4096, height: 2048 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,4096,2048",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
  },
};

/** Invented locations: mid-ground, near a corner, near the seam, near a pole. */
const fieldProbes: readonly Coordinate[] = [
  [700.25, -500.75],
  [1601.5, -1400.125],
  [100.03125, -170.0625],
];

const globeProbes: readonly Coordinate[] = [
  [1234.5, -567.875],
  [4090.25, -1023.5],
  [17.75, -1990.0625],
  [2048.5, -60.25],
];

/** The systems on trial, each with the ground it divides and the points that drive it. */
const subjects: readonly {
  name: string;
  system: CellSystem;
  ground: Ground;
  probes: readonly Coordinate[];
}[] = [
  { name: "geohash on the plane", system: geohashSystem, ground: field, probes: fieldProbes },
  { name: "districts on the plane", system: districtSystem, ground: field, probes: fieldProbes },
  { name: "geohash on the sphere", system: geohashSystem, ground: globe, probes: globeProbes },
  { name: "s2 on the sphere", system: s2System, ground: globe, probes: globeProbes },
];

/** The descent chain under a probe, root first, down to the telescope's floor. */
function chainUnder(
  on: GroundedCellSystem,
  probe: Coordinate,
  floor: number,
): CellID[] {
  const chain: CellID[] = [""];
  for (let hop = 0; hop < floor; hop++) {
    const last = chain[chain.length - 1];
    if (last === undefined) break;
    const next = on.descendTarget(last, probe);
    if (!next) break;
    chain.push(next);
  }
  return chain;
}

/**
 * Whether `inner` lies within `outer`, allowing the one thing the continuity
 * rule permits: an extent measured a whole world of x away, because a ring
 * that crossed the seam was unwrapped from its own first vertex. The slack is
 * for float arithmetic, not geometry.
 */
function nestsWithin(inner: Extent, outer: Extent, width: number, slack: number): boolean {
  for (const shift of [-width, 0, width]) {
    if (
      inner[0] + shift >= outer[0] - slack &&
      inner[2] + shift <= outer[2] + slack &&
      inner[1] >= outer[1] - slack &&
      inner[3] <= outer[3] + slack
    ) {
      return true;
    }
  }
  return false;
}

for (const { name, system, ground, probes } of subjects) {
  const on = system.on(ground);
  const floor = system.maxLevel(ground);
  const width = ground.lens?.surface?.width ?? 0;

  describe(`containment: ${name}`, () => {
    it("a child cell's extent lies within its parent's", () => {
      for (const probe of probes) {
        for (const parent of chainUnder(on, probe, floor - 1)) {
          const outer = on.bbox(parent);
          for (const child of on.children(parent)) {
            // Two places an extent stops being a containment fact, both of
            // them the picture's doing and not the hierarchy's. A parent
            // whose extent already spans the whole world of x — the root — is
            // cut at the picture's own edges, while a seam-crossing child
            // lawfully runs off them (the continuity rule: rings run off the
            // surface rather than wrapping), so only y can be measured there.
            // And around a pole the drawn extent is the bbox of a boundary
            // walk: a parent that circles the pole has a ring that never
            // reaches it, while the child cornered exactly on it does, so the
            // child's box runs past the parent's in y too. The spherical
            // predicate below is the truth at the poles; the box is not.
            const outerIsWholeWorld = outer[2] - outer[0] >= width - 1e-6;
            const polar = on.poleContained(child) !== null || on.poleContained(parent) !== null;
            const inner = on.bbox(child);
            if (polar) {
              // Judged by contains() below, in the system's own arithmetic.
            } else if (outerIsWholeWorld) {
              assert.ok(
                inner[1] >= outer[1] - 1e-6 && inner[3] <= outer[3] + 1e-6,
                `${child || "the root"} leaves ${parent || "the root"} in y`,
              );
            } else {
              assert.ok(
                nestsWithin(inner, outer, width, 1e-6),
                `${child}'s extent ${JSON.stringify(inner)} leaves its parent's ${JSON.stringify(outer)}`,
              );
            }
            // And the containment the extent pictures: the parent holds the
            // child's own interior, asked in the system's exact arithmetic.
            assert.ok(
              on.contains(parent, on.center(child)),
              `${parent || "the root"} does not contain the centre of its child ${child}`,
            );
          }
        }
      }
    });
  });

  describe(`hierarchy: ${name}`, () => {
    it("parent and children are inverse both ways", () => {
      for (const probe of probes) {
        for (const id of chainUnder(on, probe, floor - 1)) {
          const kids = on.children(id);
          assert.deepEqual(on.children(id), kids, "the child order shuffled between calls");
          for (const child of kids) {
            assert.equal(on.parent(child), id, `${child} does not lead back to ${id || "the root"}`);
            assert.equal(on.level(child), on.level(id) + 1);
          }
          if (id !== "") {
            assert.ok(on.children(on.parent(id)).includes(id),
              `${on.parent(id) || "the root"} does not own up to ${id}`);
          }
        }
      }
    });

    it("the sibling ordinal is the position in the stable order", () => {
      for (const probe of probes) {
        for (const id of chainUnder(on, probe, floor - 1)) {
          on.children(id).forEach((child, at) => {
            assert.equal(on.childIndex(child), at,
              `${child} is child ${at} of ${id || "the root"} but calls itself ${on.childIndex(child)}`);
          });
        }
      }
    });
  });

  describe(`plan order: ${name}`, () => {
    // The order is reconstructed here from the documented rule and nothing
    // else: the ancestors' other children from the root down as neighbours,
    // then the held cell as a leaf and nothing after it, or as a scope
    // followed by every one of its children in the stable order.
    it("plans emit in the documented positional order", () => {
      for (const probe of probes) {
        for (const held of chainUnder(on, probe, floor)) {
          const expected: { hash: CellID; role: string; contextDistance: number }[] = [];
          const chain: CellID[] = [held];
          for (let id = held; id !== ""; ) {
            id = on.parent(id);
            chain.unshift(id);
          }
          for (let depth = 0; depth < chain.length - 1; depth++) {
            const here = chain[depth];
            const selected = chain[depth + 1];
            if (here === undefined || selected === undefined) continue;
            for (const id of on.children(here)) {
              if (id === selected) continue;
              expected.push({ hash: id, role: "neighbor", contextDistance: chain.length - 1 - depth });
            }
          }
          if (held && on.level(held) >= floor) {
            expected.push({ hash: held, role: "leaf", contextDistance: 0 });
          } else {
            if (held) expected.push({ hash: held, role: "scope", contextDistance: 0 });
            for (const id of on.children(held)) {
              expected.push({ hash: id, role: "child", contextDistance: 0 });
            }
          }
          assert.deepEqual(
            cellPlan(ground, system, held).map(
              (cell) => ({ hash: cell.hash, role: cell.role, contextDistance: cell.contextDistance }),
            ),
            expected,
            `the plan for "${held}" is not in emission order`,
          );
        }
      }
    });

    it("nothing follows a leaf, and context distance descends across the run", () => {
      for (const probe of probes) {
        const chain = chainUnder(on, probe, floor);
        const deepest = chain[chain.length - 1];
        if (deepest === undefined || on.level(deepest) < floor) continue;
        const plan = cellPlan(ground, system, deepest);
        assert.equal(plan[plan.length - 1]?.role, "leaf");
        assert.equal(plan.filter((cell) => cell.role !== "neighbor").length, 1,
          "a leaf plan is neighbours and the leaf, nothing else");
        const distances = plan
          .filter((cell) => cell.role === "neighbor")
          .map((cell) => cell.contextDistance);
        for (let at = 1; at < distances.length; at++) {
          const previous = distances[at - 1];
          const current = distances[at];
          assert.ok(previous !== undefined && current !== undefined && current <= previous,
            "the root's neighbours come first and the held cell's siblings last");
        }
      }
    });
  });
}

// --- the carry ---------------------------------------------------------------
//
// ⌘G cycles the grid system, and the held cell crosses with it. The two
// hierarchies share no boundaries, so the invariant is not about the address —
// it is that the *place* survives (the new cell holds the old cell's centre)
// and the *precision* survives (the new cell's area is the closest on a log
// scale among the cells of its own descent chain, because levels grow
// geometrically and "closest in kind" is what a reader means by "where I
// was"). The root is the one address both hierarchies spell alike, and it
// carries to itself.

const crossings: readonly {
  name: string;
  ground: Ground;
  from: CellSystem;
  to: CellSystem;
  probes: readonly Coordinate[];
}[] = [
  { name: "geohash to districts on the plane", ground: field, from: geohashSystem, to: districtSystem, probes: fieldProbes },
  { name: "districts to geohash on the plane", ground: field, from: districtSystem, to: geohashSystem, probes: fieldProbes },
  { name: "geohash to s2 on the sphere", ground: globe, from: geohashSystem, to: s2System, probes: globeProbes },
  { name: "s2 to geohash on the sphere", ground: globe, from: s2System, to: geohashSystem, probes: globeProbes },
];

for (const { name, ground, from, to, probes } of crossings) {
  const source = from.on(ground);
  const target = to.on(ground);

  describe(`carry: ${name}`, () => {
    it("the root carries to the root", () => {
      assert.equal(equivalentCell(ground, from, to, ""), "");
    });

    it("the place survives: the carried cell holds the old cell's centre", () => {
      for (const probe of probes) {
        for (const held of chainUnder(source, probe, from.maxLevel(ground))) {
          if (held === "") continue;
          const carried = equivalentCell(ground, from, to, held);
          assert.ok(carried, `"${held}" carried to nowhere on a ground both systems divide`);
          assert.ok(target.contains(carried, source.center(held)),
            `"${carried}" does not hold the centre of "${held}", which is the place that was carried`);
        }
      }
    });

    it("the precision survives: no neighbouring depth fits closer on a log scale", () => {
      for (const probe of probes) {
        for (const held of chainUnder(source, probe, from.maxLevel(ground))) {
          if (held === "") continue;
          const carried = equivalentCell(ground, from, to, held);
          if (!carried) continue;
          const area = extentArea(source.bbox(held));
          const fit = (id: CellID): number =>
            Math.abs(Math.log(extentArea(target.bbox(id)) / area));
          // One level out: the parent is a worse fit, or there is no parent.
          const parent = target.parent(carried);
          if (parent !== "") {
            assert.ok(fit(carried) <= fit(parent) + 1e-9,
              `"${held}" carried to "${carried}" but its parent "${parent}" fits closer`);
          }
          // One level in: the child under the same centre is a worse fit — or
          // out of reach, because the carry stops at the telescope's floor
          // even where the arithmetic could keep halving (geohash's
          // descendTarget does; the floor asymmetry, docs/analysis.md).
          const deeper = target.level(carried) < to.maxLevel(ground)
            ? target.descendTarget(carried, source.center(held))
            : "";
          if (deeper) {
            assert.ok(fit(carried) <= fit(deeper) + 1e-9,
              `"${held}" carried to "${carried}" but its child "${deeper}" fits closer`);
          }
        }
      }
    });
  });
}
