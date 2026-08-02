// The cell-system contract: eighteen methods, and the rules they keep.
//
// A cell system divides a ground into an exact hierarchy a reader can
// telescope through. Geohash was the first and grew into the old app's bones;
// this contract is what pulled it back out, so a second system is one more
// file and a third is a registry entry (issue #5 §5.4).
//
// The eighteen split cleanly in two, and the split is the whole of the
// de-globalization:
//
//   THREE methods answer about a ground before any cell is named — whether
//   this system will divide it, how deep the telescope goes, how many
//   characters the navigator's field accepts.
//
//   FIFTEEN answer about a cell on a ground. They are reached through
//   `system.on(ground)`, which binds the ground once and hands back an object
//   whose methods take only their own arguments.
//
// THE RULES, which the conformance suite (analysis/test/conformance.ts)
// checks as properties rather than examples:
//
//   Opaque ids.            A cell id is an opaque string and "" is the root.
//                          Nothing outside a system may take it apart —
//                          geohash refines by appending a character, S2 does
//                          not (the children of 47a1cb are 47a1ca4 and its
//                          siblings), and a third system may do neither.
//   Stable child order.    `children(id)` answers in one order, forever. The
//                          plan emits in it, the palette indexes into it, and
//                          the vectors gate compares positionally.
//   Inclusive containment. `contains` is true on the boundary. A point on a
//                          shared edge is in both cells; nothing here owns a
//                          tie-break, because the caller asking is drawing,
//                          not partitioning.
//   Descent agrees.        `descendTarget(id, at)` names a child that
//                          `contains` the point, or "" at the floor.
//   Whole-or-nothing.      `parseInput` answers a canonical id or null. A
//                          draft is not a place; the field holds it and the
//                          session stays put.
//   Closed rings.          `ring(id)` returns a loop whose last point equals
//                          its first.
//   Continuity.            A ring never jumps the seam: consecutive points
//                          stay within half a world of each other in x, even
//                          where that runs the loop off the surface. The
//                          adapters wrap or split it; the system does not.
//   Poles.                 `poleContained(id)` says which pole a cell circles,
//                          because a loop around a pole closes along the
//                          picture's own top or bottom edge and only the
//                          system knows it needs to.
//
// Coordinates are OL world coordinates throughout: x east, y **negative-down**
// — the space a pin's coordinate lives in.

import type { Coordinate, Extent, Ground } from "./ground.ts";

/** An opaque cell address. `""` is the root of every system. */
export type CellID = string;

/** A closed boundary loop, pre-tessellated, in OL world coordinates. */
export type Ring = readonly Coordinate[];

/** Which pole a cell circles, when it circles one. */
export type Pole = "north" | "south";

/** The chip's two cuts: a faint context and a bright principal. */
export interface CellLabel {
  readonly context: string;
  readonly principal: string;
}

/** One named row for the pin card: this system's own address for a point. */
export interface LocatedCell {
  readonly label: string;
  readonly value: string;
}

/**
 * The fifteen methods that answer about a cell on a ground already bound.
 *
 * Every one of these is a pure function of the ground it was bound to and its
 * own arguments. Nothing here reads a session, a camera, or a page.
 */
export interface GroundedCellSystem {
  /** Depth of a cell. The root `""` is level 0. */
  level(id: CellID): number;

  /** One level up; `""` from a level-1 cell, and `""` from the root. */
  parent(id: CellID): CellID;

  /** The next level down, in a stable order. `children("")` are the roots. */
  children(id: CellID): CellID[];

  /** Ordinal among siblings — the priority order and the palette index. */
  childIndex(id: CellID): number;

  /** Whether a point is inside. Boundaries are inclusive. */
  contains(id: CellID, at: Coordinate): boolean;

  /** The child of `id` under the point, or `""` at the telescope's floor. */
  descendTarget(id: CellID, at: Coordinate): CellID;

  /** `[minX, minY, maxX, maxY]`, OL world coordinates. */
  bbox(id: CellID): Extent;

  /** A point inside the cell, OL world coordinates. */
  center(id: CellID): Coordinate;

  /** The closed, continuous boundary loop. See the continuity rule above. */
  ring(id: CellID): Ring;

  /** Which pole this cell circles, for ring closure; null for most cells. */
  poleContained(id: CellID): Pole | null;

  /** The chip's two cuts. */
  label(id: CellID): CellLabel;

  /** Palette index for the cell's accent; siblings must differ. */
  colorKey(id: CellID): number;

  /** What the navigator's field keeps of a keystroke. */
  normalizeInput(text: string): string;

  /** A canonical id, or null while the text is not yet a place. */
  parseInput(text: string): CellID | null;

  /** This system's own address for a point, for the pin card, or null. */
  locate(at: Coordinate): LocatedCell | null;
}

/**
 * A registered cell system: its identity, the three questions it answers
 * about a ground, and the binding that produces the other fifteen.
 *
 * Adding a system is implementing this and putting it in a registry. There is
 * no other seam — see `docs/analysis.md` for the walkthrough.
 */
export interface CellSystem {
  /** Stable machine name. Sessions persist it; it never changes. */
  readonly slug: string;

  /** How the navigator names it. */
  readonly name: string;

  /** The mark its cycle button wears. */
  readonly short: string;

  /** Whether this system can divide this ground at all. */
  appliesTo(ground: Ground): boolean;

  /** How deep the telescope goes on this ground. */
  maxLevel(ground: Ground): number;

  /** How many characters the navigator's field accepts on this ground. */
  inputLength(ground: Ground): number;

  /** Bind the ground once; the fifteen follow. */
  on(ground: Ground): GroundedCellSystem;
}
