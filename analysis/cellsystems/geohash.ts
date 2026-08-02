// The geohash cell system: recursive halving of the ground in world pixels,
// thirty-two children to a cell, addresses that refine by appending a
// character.
//
// It divides any ground at all, because it never asks what the picture is of —
// which is why it was the first system. The halving itself is arithmetic the
// vectors gate compares byte for byte, so nothing about how it is written here
// may move a float: the mask walk, the axis alternation and the order of the
// two divisions are the recorded behaviour.

import type { CellID, CellLabel, GroundedCellSystem, LocatedCell, Pole, Ring } from "./contract.ts";
import type { CellSystem } from "./contract.ts";
import type { Coordinate, Extent, Ground } from "./ground.ts";
import { surfaceExtent } from "./ground.ts";
import { palette } from "./tokens.ts";

/**
 * The alphabet, in its canonical order — no `a`, `i`, `l` or `o`, so a hash
 * read aloud or typed from a screenshot cannot be misheard. Its order is the
 * child order and the palette order.
 */
export const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz";

/**
 * The telescope's floor. Three characters is fifteen halvings — a cell a few
 * world pixels across on an 8192 square — which is depth enough for a couple
 * thousand pins and shallow enough that every level stays a place.
 */
export const geohashMaxDepth = 3;

/** The five bits of one character, most significant first. */
const masks = [16, 8, 4, 2, 1] as const;

/**
 * The recursive halving itself: alternate axes, five bits to a character.
 *
 * A character outside the alphabet is skipped without consuming a halving —
 * the axis does not alternate past it — which is what the field's own
 * normalization makes unreachable and what this keeps true anyway.
 */
function bbox(ground: Ground, hash: CellID): Extent {
  const extent: [number, number, number, number] = [...surfaceExtent(ground)];
  let splitX = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of masks) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (value & mask) extent[0] = middle;
        else extent[2] = middle;
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (value & mask) extent[1] = middle;
        else extent[3] = middle;
      }
      splitX = !splitX;
    }
  }
  return extent;
}

/**
 * The halving run backward: the same divisions, choosing at each one the side
 * the point is on. Computed when asked rather than stored, because the grid
 * divides the ground a lens covers and a split world gives each lens its own —
 * a hash stored beside a location could only ever be right for one of them.
 *
 * A point off the surface names nothing.
 */
function cellAt(ground: Ground, coordinate: Coordinate, depth: number): CellID {
  const extent: [number, number, number, number] = [...surfaceExtent(ground)];
  const [x, y] = coordinate;
  if (x < extent[0] || x > extent[2] || y < extent[1] || y > extent[3]) return "";
  let splitX = true;
  let hash = "";
  for (let level = 0; level < depth; level++) {
    let value = 0;
    for (const mask of masks) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (x >= middle) {
          value |= mask;
          extent[0] = middle;
        } else {
          extent[2] = middle;
        }
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (y >= middle) {
          value |= mask;
          extent[1] = middle;
        } else {
          extent[3] = middle;
        }
      }
      splitX = !splitX;
    }
    // `value` is five bits, so the alphabet always has the character; the
    // fallback is what `noUncheckedIndexedAccess` costs and it never fires.
    hash += geohashAlphabet[value] ?? "";
  }
  return hash;
}

/**
 * The ordinal of a hash's last character. The root — and any hash ending in a
 * character the alphabet does not carry — is ordinal 0.
 */
function ordinal(id: CellID): number {
  return Math.max(0, geohashAlphabet.indexOf(id.slice(-1)));
}

/**
 * One remembered box. Callers sweep thousands of pins against one cell at a
 * time, so the last cell's extent is kept rather than re-halved per pin. It is
 * a cache over a pure function and nothing observes it: the key carries the
 * surface as well as the id, because a lens switch moves every box.
 */
let memo: { key: string; extent: Extent } | null = null;

function cachedBox(ground: Ground, id: CellID): Extent {
  const key = `${surfaceExtent(ground).join(",")}|${id}`;
  if (memo?.key !== key) memo = { key, extent: bbox(ground, id) };
  return memo.extent;
}

/**
 * The pin card's fixed-depth address, independent of where the grid stands.
 * The one caller that names a depth of its own.
 */
export function geohashCellAt(
  ground: Ground,
  coordinate: Coordinate,
  depth: number = geohashMaxDepth,
): CellID {
  return cellAt(ground, coordinate, depth);
}

/** Geohash, bound to one ground. */
function grounded(ground: Ground): GroundedCellSystem {
  return {
    level(id: CellID): number {
      return id.length;
    },

    parent(id: CellID): CellID {
      return id.slice(0, -1);
    },

    children(id: CellID): CellID[] {
      return [...geohashAlphabet].map((character) => id + character);
    },

    childIndex(id: CellID): number {
      return ordinal(id);
    },

    contains(id: CellID, at: Coordinate): boolean {
      const extent = cachedBox(ground, id);
      const [x, y] = at;
      return x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3];
    },

    descendTarget(id: CellID, at: Coordinate): CellID {
      return cellAt(ground, at, id.length + 1);
    },

    bbox(id: CellID): Extent {
      return bbox(ground, id);
    },

    center(id: CellID): Coordinate {
      const extent = bbox(ground, id);
      return [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2];
    },

    ring(id: CellID): Ring {
      const [minimumX, minimumY, maximumX, maximumY] = bbox(ground, id);
      return [
        [minimumX, minimumY],
        [minimumX, maximumY],
        [maximumX, maximumY],
        [maximumX, minimumY],
        [minimumX, minimumY],
      ];
    },

    // A halved rectangle on a flat picture circles nothing.
    poleContained(): Pole | null {
      return null;
    },

    label(id: CellID): CellLabel {
      return { context: id.slice(0, -1), principal: id.slice(-1) };
    },

    colorKey(id: CellID): number {
      return ordinal(id) % palette.length;
    },

    normalizeInput(text: string): string {
      return [...text.toLocaleLowerCase()]
        .filter((character) => geohashAlphabet.includes(character))
        .slice(0, geohashMaxDepth)
        .join("");
    },

    // Every normalized spelling is a place — the empty string is the root —
    // so parsing never refuses.
    parseInput(text: string): CellID | null {
      return text;
    },

    locate(at: Coordinate): LocatedCell | null {
      const value = cellAt(ground, at, geohashMaxDepth);
      return value ? { label: "Geohash", value } : null;
    },
  };
}

export const geohashSystem: CellSystem = {
  slug: "geohash",
  name: "Geohash",
  short: "G",

  // Geohash divides anything: it never asks what the picture is of.
  appliesTo(): boolean {
    return true;
  },

  maxLevel(): number {
    return geohashMaxDepth;
  },

  inputLength(): number {
    return geohashMaxDepth;
  },

  on: grounded,
};
