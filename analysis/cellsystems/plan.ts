// The plan: the one account of which cells a grid shows.
//
// `cellPlan(ground, system, cellID)` is issue #5 §5.4's contractual output,
// and its **emission order is frozen**: the plan is read positionally, cell
// for cell — "cell 12 of 32, emission order is the contract" — so a
// reordering is a failure, not a detail. The order property in
// analysis/test/properties.test.ts holds it.
//
// THE ORDER, which is the whole contract:
//
//   1. For each ancestor from the root down, that ancestor's children except
//      the one on the path to the held cell, as `neighbor`s carrying their
//      distance from it. The root's neighbours come first and the held cell's
//      siblings come last, so contextDistance descends across the run.
//   2. Then either
//      a. the held cell as a `leaf`, if it sits at the system's maxLevel —
//         and then nothing follows, because a leaf has nothing to divide; or
//      b. the held cell as a `scope`, followed by every one of its children as
//         `child`s in the system's stable child order.
//
// The root plan — no held cell — is case 2b with no ancestors and no scope:
// the root's children, and nothing else.
//
// Session state arrives as arguments. Which system is active and which cell is
// held live in the app's session (§4.1); this function is handed both and
// keeps neither.

import type { CellID, CellSystem, GroundedCellSystem, Pole, Ring } from "./contract.ts";
import type { Extent, Ground } from "./ground.ts";
import type { CellRole } from "./tokens.ts";

/**
 * One cell of a plan.
 *
 * The key order below is the order the shared vectors serialize: the
 * fixtures are compared as JSON text, so it is cheaper to keep than to
 * explain. Only the array order is contractual, but a diff nobody has to
 * read is worth the discipline.
 */
export interface PlanCell {
  /** The cell's opaque id. The field keeps the name `hash` whatever system minted it. */
  readonly hash: CellID;
  readonly extent: Extent;
  readonly ring: Ring;
  readonly pole: Pole | null;
  readonly childIndex: number;
  readonly role: CellRole;
  /** How many levels above the held cell this cell's parent sits; 0 off the ancestor run. */
  readonly contextDistance: number;
}

/**
 * The cells a grid shows, in frozen emission order.
 *
 * `cellID` is the held cell — `""` for the root. `system` must be one that
 * `appliesTo` this ground; a system that does not is a caller error, not a
 * plan.
 */
export function cellPlan(ground: Ground, system: CellSystem, cellID: CellID): PlanCell[] {
  const on = system.on(ground);
  const cells: PlanCell[] = [];
  const chain = ancestorChain(on, cellID);
  for (let depth = 0; depth < chain.length - 1; depth++) {
    const here = chain[depth];
    const selected = chain[depth + 1];
    if (here === undefined || selected === undefined) continue;
    for (const id of on.children(here)) {
      if (id === selected) continue;
      cells.push(planCell(on, id, "neighbor", chain.length - 1 - depth));
    }
  }
  if (cellID && on.level(cellID) >= system.maxLevel(ground)) {
    cells.push(planCell(on, cellID, "leaf", 0));
    return cells;
  }
  // The cell the reader is inside, outlined rather than tiled. It is the one
  // part of the grid that survives putting the subdivision away: what is on
  // screen is still a chosen place, and a boundary says so where a bare map
  // does not.
  if (cellID) {
    cells.push(planCell(on, cellID, "scope", 0));
  }
  for (const id of on.children(cellID)) {
    cells.push(planCell(on, id, "child", 0));
  }
  return cells;
}

/** The walk from the root down to the held cell, root first. */
export function ancestorChain(on: GroundedCellSystem, id: CellID): CellID[] {
  const chain: CellID[] = [id];
  let held = id;
  while (held) {
    held = on.parent(held);
    chain.unshift(held);
  }
  return chain;
}

function planCell(
  on: GroundedCellSystem,
  id: CellID,
  role: CellRole,
  contextDistance: number,
): PlanCell {
  return {
    hash: id,
    extent: on.bbox(id),
    ring: on.ring(id),
    pole: on.poleContained(id),
    childIndex: on.childIndex(id),
    role,
    contextDistance,
  };
}
