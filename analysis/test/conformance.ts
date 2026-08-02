// The conformance suite: the properties every cell system must keep.
//
// Issue #5 §5.4 asks for "an executable property suite; a third system (H3,
// MGRS, a game's district grid) is a registry entry plus a green run". This is
// that suite. It is deliberately made of *properties* rather than examples —
// an example says what one cell did, a property says what the contract is —
// and it is run over geohash, over S2, and over an example third system that
// exists only to prove the contract holds nothing geohash-shaped.
//
// What the suite does NOT assert, and why, because a property suite is only
// honest if its silences are written down:
//
//   Winding.        A ring's signed area may be either sign. The antimeridian
//                   unwrap can reverse it for a loop that circles a pole, and
//                   nothing downstream reads the direction. Non-degeneracy is
//                   asserted; orientation is not.
//   A hard floor.   `descendTarget` at maxLevel answers "" in S2 and keeps
//                   halving in geohash. The systems disagree and the recorded
//                   behaviour is frozen, so what is asserted is that descent
//                   *terminates* — the telescope reaches maxLevel in maxLevel
//                   hops — not what it says once it is there. See
//                   docs/analysis.md, "the floor asymmetry".
//   Boundary probes. "Boundaries are inclusive" is checkable only where a
//                   system's geometry puts a point exactly on a boundary. A
//                   rectangle's corner is such a point; an S2 vertex is not,
//                   because S2 decides containment on a leaf lattice far finer
//                   than the degrees a vertex round-trips through. Each subject
//                   declares its own boundary probes, and a subject with none
//                   says so out loud.

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import type { CellID, CellSystem, GroundedCellSystem } from "../cellsystems/contract.ts";
import type { Coordinate, Ground } from "../cellsystems/ground.ts";
import { surfaceExtent, surfaceWidth } from "../cellsystems/ground.ts";
import { cellPlan } from "../cellsystems/plan.ts";
import { gridCellVisual } from "../cellsystems/visual.ts";
import { ringClosesOn } from "../cellsystems/ring.ts";
import { palette } from "../cellsystems/tokens.ts";

/** One system on one ground, plus the two things only its author knows. */
export interface ConformanceSubject {
  readonly system: CellSystem;
  readonly ground: Ground;

  /** A point well inside the ground, used to drive descent. */
  readonly probe: Coordinate;

  /**
   * Points this system's own geometry puts exactly on a cell boundary, as
   * `[cellID, point]` pairs the cell must contain. A system whose containment
   * is decided on a lattice finer than its geometry declares an empty list and
   * explains itself in a comment.
   */
  readonly boundaryProbes: readonly (readonly [CellID, Coordinate])[];
}

/** The full property suite, as node:test cases. */
export function describeConformance(name: string, subject: ConformanceSubject): void {
  const { system, ground, probe } = subject;
  describe(`conformance: ${name}`, () => {
    const on = system.on(ground);
    const floor = system.maxLevel(ground);
    const sample = sampleCells(on, probe, floor);

    // --- applicability ---------------------------------------------------

    it("applies to the ground it is being conformance-tested on", () => {
      assert.equal(system.appliesTo(ground), true);
      assert.ok(floor >= 1, "a system that divides nothing is not a system");
      assert.ok(system.inputLength(ground) >= 1, "the navigator must accept something");
      assert.ok(system.slug.length > 0 && system.name.length > 0 && system.short.length > 0);
    });

    // --- identity --------------------------------------------------------

    it("the root is level 0 and its own parent", () => {
      assert.equal(on.level(""), 0, 'the root "" is level 0');
      assert.equal(on.parent(""), "", "there is nothing above the root");
    });

    it("ids are opaque strings the system alone takes apart", () => {
      for (const id of sample.cells) {
        assert.equal(typeof id, "string");
        const { context, principal } = on.label(id);
        assert.equal(typeof context, "string");
        assert.equal(typeof principal, "string");
      }
    });

    it("every cell's accent is a palette index, and siblings differ", () => {
      for (const parent of sample.parents) {
        const keys = on.children(parent).map((id) => on.colorKey(id));
        for (const key of keys) {
          assert.ok(Number.isInteger(key) && key >= 0 && key < palette.length,
            `colorKey ${key} is not an index into a ${palette.length}-colour wheel`);
        }
        const neighbours = keys.slice(0, palette.length);
        assert.equal(new Set(neighbours).size, neighbours.length,
          "cells drawn side by side must not share an accent");
      }
    });

    // --- hierarchy -------------------------------------------------------

    it("children answer in a stable order, and the ordinal is that order", () => {
      for (const parent of sample.parents) {
        const once = on.children(parent);
        const twice = on.children(parent);
        assert.deepEqual(twice, once, "children() is not allowed to shuffle");
        assert.ok(once.length >= 2, "a level that does not divide is not a level");
        assert.equal(new Set(once).size, once.length, "a child appears once");
        once.forEach((id, at) => {
          assert.equal(on.childIndex(id), at,
            `${id} is child ${at} of ${parent || "the root"} but calls itself ${on.childIndex(id)}`);
        });
      }
    });

    it("parent and children are inverse, and level counts the hops", () => {
      for (const parent of sample.parents) {
        const level = on.level(parent);
        for (const id of on.children(parent)) {
          assert.equal(on.parent(id), parent, `${id} does not lead back to ${parent || "the root"}`);
          assert.equal(on.level(id), level + 1, `${id} is not one level below its parent`);
        }
      }
    });

    it("the ancestor chain of every sampled cell reaches the root", () => {
      for (const id of sample.cells) {
        let held = id;
        let hops = 0;
        while (held !== "" && hops <= floor + 1) {
          held = on.parent(held);
          hops++;
        }
        assert.equal(held, "", `${id} does not climb back to the root`);
        assert.equal(hops, on.level(id), `${id} claims a level its chain does not walk`);
      }
    });

    // --- containment -----------------------------------------------------

    it("a cell contains its own centre, and its parent contains it too", () => {
      for (const id of sample.cells) {
        if (id === "") continue;
        const centre = on.center(id);
        assert.equal(on.contains(id, centre), true, `${id} does not contain its own centre`);
        assert.equal(on.contains(on.parent(id), centre), true,
          `${on.parent(id) || "the root"} does not contain ${id}'s centre`);
      }
    });

    it("descent agrees with containment", () => {
      for (const parent of sample.parents) {
        for (const id of on.children(parent)) {
          const centre = on.center(id);
          assert.equal(on.descendTarget(parent, centre), id,
            `descending into ${parent || "the root"} at ${id}'s centre missed it`);
          assert.equal(on.contains(id, centre), true);
        }
      }
    });

    it("boundaries are inclusive", () => {
      if (subject.boundaryProbes.length === 0) {
        // Declared empty: see the note at the top of this file.
        return;
      }
      for (const [id, at] of subject.boundaryProbes) {
        assert.equal(on.contains(id, at), true,
          `${id} excludes ${JSON.stringify(at)}, which sits on its own boundary`);
      }
    });

    it("the telescope terminates at maxLevel", () => {
      let id = "";
      for (let hop = 0; hop < floor; hop++) {
        const next = on.descendTarget(id, probe);
        assert.ok(next, `descent stopped at level ${hop}, above the floor of ${floor}`);
        assert.equal(on.level(next), hop + 1, "a descent hop must be exactly one level");
        assert.equal(on.contains(next, probe), true);
        id = next;
      }
      assert.equal(on.level(id), floor, "descending maxLevel times lands on maxLevel");
    });

    it("locate names the point in this system's own address", () => {
      const row = on.locate(probe);
      assert.ok(row, "a point on the ground has an address");
      assert.ok(row.label.length > 0, "the row is named for the pin card");
      assert.ok(row.value.length > 0);
    });

    // --- geometry --------------------------------------------------------

    it("rings close, on the same row, some whole number of worlds over", () => {
      for (const id of sample.geometric) {
        const ring = on.ring(id);
        assert.ok(ring.length >= 4, `${id}'s ring is not a loop`);
        assert.ok(ringClosesOn(ground, ring),
          `${id}'s ring does not close: ${JSON.stringify(ring[0])} → ` +
          `${JSON.stringify(ring[ring.length - 1])}`);
      }
    });

    // Half a world is the unwrap's own threshold, and it is a bound rather
    // than a strict one on purpose: a cell touching a pole sweeps exactly half
    // the world of longitude in one tessellation step, which is ground and not
    // a seam. Anything past half is the seam, and the ring runs off the surface
    // instead of wrapping — that is the continuity rule, and cellRings() is
    // what cuts the result.
    it("rings stay continuous: no point ever jumps the seam", () => {
      const half = surfaceWidth(ground) / 2;
      for (const id of sample.geometric) {
        const ring = on.ring(id);
        for (let at = 1; at < ring.length; at++) {
          const current = ring[at];
          const previous = ring[at - 1];
          assert.ok(current && previous);
          assert.ok(Math.abs(current[0] - previous[0]) <= half,
            `${id}'s ring jumps ${Math.abs(current[0] - previous[0])} in x at step ${at}: ` +
            "a ring runs off the surface rather than wrapping, and the adapters cut it");
        }
      }
    });

    it("rings enclose real ground", () => {
      for (const id of sample.geometric) {
        assert.notEqual(signedArea(on.ring(id)), 0, `${id}'s ring encloses nothing`);
      }
    });

    it("the bbox is the ring's own bounding box", () => {
      for (const id of sample.geometric) {
        const ring = on.ring(id);
        const xs = ring.map((point) => point[0]);
        const ys = ring.map((point) => point[1]);
        const [minimumX, minimumY, maximumX, maximumY] = on.bbox(id);
        assert.ok(minimumX <= Math.min(...xs) + 1e-9 && maximumX >= Math.max(...xs) - 1e-9,
          `${id}'s bbox does not span its own ring in x`);
        assert.ok(minimumY <= Math.min(...ys) + 1e-9 && maximumY >= Math.max(...ys) - 1e-9,
          `${id}'s bbox does not span its own ring in y`);
      }
    });

    it("the root's bbox is the ground", () => {
      assert.deepEqual([...on.bbox("")], [...surfaceExtent(ground)]);
    });

    it("a cell circles at most one pole, and its parent circles the same one", () => {
      for (const id of sample.cells) {
        if (id === "") continue;
        const pole = on.poleContained(id);
        assert.ok(pole === null || pole === "north" || pole === "south");
        if (pole !== null && on.parent(id) !== "") {
          assert.equal(on.poleContained(on.parent(id)), pole,
            `${id} circles the ${pole} pole but ${on.parent(id) || "the root"} does not`);
        }
      }
      for (const parent of sample.parents) {
        // The root is not a cell: it is the whole ground, and a ground with
        // both poles on it cannot answer with one of them.
        if (parent === "") continue;
        for (const pole of ["north", "south"] as const) {
          const claimants = on.children(parent).filter((id) => on.poleContained(id) === pole);
          assert.ok(claimants.length <= 1,
            `${claimants.length} children of ${parent || "the root"} claim the ${pole} pole`);
        }
      }
    });

    // --- input -----------------------------------------------------------

    it("normalizing is idempotent", () => {
      const drafts = ["", "  ", "!!", ...sample.cells, ...sample.cells.map((id) => id.toUpperCase())];
      for (const draft of drafts) {
        const once = on.normalizeInput(draft);
        assert.equal(on.normalizeInput(once), once, `normalizing "${draft}" twice moved it`);
      }
    });

    it("parsing is whole or nothing, and never lengthens the address", () => {
      for (const draft of ["", "!", "zzzzzzzzzzzz", "0".repeat(40)]) {
        const parsed = on.parseInput(on.normalizeInput(draft));
        if (parsed === null) continue;
        assert.equal(on.parseInput(parsed), parsed, "a parsed id must parse to itself");
        assert.ok(on.level(parsed) <= floor,
          `parsing "${draft}" answered a cell below the telescope's floor`);
      }
    });

    it("every holdable id survives the navigator unchanged", () => {
      for (const id of sample.cells) {
        if (on.level(id) > floor) continue;
        assert.equal(on.normalizeInput(id), id, `the field would not keep "${id}"`);
        assert.equal(on.parseInput(id), id, `"${id}" does not parse back to itself`);
      }
    });

    // --- the plan --------------------------------------------------------

    it("a plan is deterministic and emits in the frozen order", () => {
      for (const held of sample.held) {
        const once = cellPlan(ground, system, held);
        const twice = cellPlan(ground, system, held);
        assert.deepEqual(twice, once, `the plan for "${held}" is not deterministic`);
        assertPlanOrder(on, floor, held, once);
      }
    });

    it("style tokens are pure, and a hidden subdivision draws nothing", () => {
      for (const held of sample.held.slice(0, 2)) {
        const plan = cellPlan(ground, system, held);
        for (const cell of plan) {
          for (const subgridVisible of [true, false]) {
            for (const labelled of [true, false]) {
              const options = { subgridVisible, labelled };
              const tokens = gridCellVisual(ground, system, cell, options);
              assert.deepEqual(gridCellVisual(ground, system, cell, options), tokens,
                "the same cell and the same questions must answer the same tokens");
              if (cell.role === "child" && (!subgridVisible || !labelled)) {
                assert.equal(tokens, null, "a child that cannot be read draws nothing at all");
                continue;
              }
              assert.ok(tokens, `${cell.hash} as ${cell.role} draws nothing`);
              assert.ok(tokens.line.widthPx > 0);
              assert.equal(tokens.bare, cell.role === "scope" && subgridVisible);
              assert.equal(tokens.label === null, !labelled || tokens.bare);
            }
          }
        }
      }
    });

    it("every plan cell carries a closed ring and its own extent", () => {
      for (const held of sample.held) {
        for (const cell of cellPlan(ground, system, held)) {
          assert.ok(ringClosesOn(ground, cell.ring), `${cell.hash} was planned with an open ring`);
          assert.equal(cell.extent.length, 4);
          assert.ok(cell.childIndex >= 0);
          assert.ok(cell.contextDistance >= 0);
        }
      }
    });
  });
}

/**
 * The plan's emission order, asserted rather than described: the ancestors'
 * neighbours from the root down, then either the held cell as a leaf and
 * nothing after it, or the held cell as a scope followed by its children.
 */
function assertPlanOrder(
  on: GroundedCellSystem,
  floor: number,
  held: CellID,
  plan: readonly { hash: CellID; role: string; contextDistance: number }[],
): void {
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
    plan.map((cell) => ({ hash: cell.hash, role: cell.role, contextDistance: cell.contextDistance })),
    expected,
    `the plan for "${held}" is not in emission order`,
  );
}

interface Sample {
  /** Every cell the run touches: the descent chain and all its siblings. */
  readonly cells: CellID[];
  /** Cells whose children are exercised as a set. */
  readonly parents: CellID[];
  /** Cells whose ring and bbox are checked — the root has neither. */
  readonly geometric: CellID[];
  /** Held cells to plan from: the root, two depths, and the floor. */
  readonly held: CellID[];
}

/** The cells one run exercises: enough to be a property, few enough to be a test. */
function sampleCells(on: GroundedCellSystem, probe: Coordinate, floor: number): Sample {
  const chain: CellID[] = [""];
  for (let hop = 0; hop < floor; hop++) {
    const last = chain[chain.length - 1];
    if (last === undefined) break;
    const next = on.descendTarget(last, probe);
    if (!next) break;
    chain.push(next);
  }
  const parents = chain.slice(0, -1);
  const cells = [...new Set([...chain, ...parents.flatMap((id) => on.children(id))])];
  // Ring and bbox work is the expensive half, and no system promises the root
  // a ring — six faces or thirty-two rectangles do not make one loop — so
  // geometry is checked below the root.
  const geometric = cells.filter((id) => id !== "").slice(0, 48);
  const held = [...new Set(
    [0, 1, Math.max(1, Math.floor(floor / 2)), chain.length - 1]
      .map((at) => chain[at])
      .filter((id): id is CellID => id !== undefined),
  )];
  return { cells, parents, geometric, held };
}

/** Twice the enclosed area, sign and all: zero means the loop encloses nothing. */
function signedArea(ring: readonly Coordinate[]): number {
  let sum = 0;
  for (let at = 0; at < ring.length - 1; at++) {
    const current = ring[at];
    const next = ring[at + 1];
    if (!current || !next) continue;
    sum += current[0] * next[1] - next[0] * current[1];
  }
  return sum;
}
