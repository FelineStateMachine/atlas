// A third system, written to be thrown away.
//
// Issue #5 §5.4: "a third system (H3, MGRS, a game's district grid) is a
// registry entry plus a green conformance run." This is the game's district
// grid, and it exists for one reason: to be a system nobody designing the
// contract had in mind, and to pass anyway.
//
// It is deliberately unlike both shipped systems. Its ids are dash-joined
// segments, so `parent` is not `slice(0, -1)` and the label's two cuts are not
// the last character and everything before it. It divides nine ways rather
// than thirty-two or four. It refuses a draft that ends mid-separator, so its
// whole-or-nothing parsing has something to refuse. Nothing about it is
// registered with the app — it lives in the test tree, which is the point:
// the contract is what admits it, not a place in the shipped registry.

import type {
  CellID,
  CellLabel,
  CellSystem,
  GroundedCellSystem,
  LocatedCell,
  Pole,
  Ring,
} from "../cellsystems/contract.ts";
import type { Coordinate, Extent, Ground } from "../cellsystems/ground.ts";
import { surfaceExtent } from "../cellsystems/ground.ts";
import { palette } from "../cellsystems/tokens.ts";

/** Three by three, read left to right and top to bottom. */
const across = 3;
const perLevel = across * across;

/** Three tiers: region, district, block. */
const depth = 3;

const separator = "-";

/** The segments of an address, or null if it is not one. */
function segments(id: CellID): number[] | null {
  if (id === "") return [];
  const parts = id.split(separator);
  const values: number[] = [];
  for (const part of parts) {
    if (!/^[0-8]$/.test(part)) return null;
    values.push(Number(part));
  }
  return values.length <= depth ? values : null;
}

/** The extent of an address: nine-way subdivision, one tier per segment. */
function bbox(ground: Ground, id: CellID): Extent {
  const [minimumX, minimumY, maximumX, maximumY] = surfaceExtent(ground);
  const box: [number, number, number, number] = [minimumX, minimumY, maximumX, maximumY];
  for (const value of segments(id) ?? []) {
    const column = value % across;
    const row = Math.floor(value / across);
    const width = (box[2] - box[0]) / across;
    const height = (box[3] - box[1]) / across;
    box[0] += column * width;
    box[2] = box[0] + width;
    // Row 0 is the top of the picture, and y is negative-down.
    box[3] -= row * height;
    box[1] = box[3] - height;
  }
  return box;
}

function grounded(ground: Ground): GroundedCellSystem {
  const cellAt = (at: Coordinate, tiers: number): CellID => {
    const [minimumX, minimumY, maximumX, maximumY] = surfaceExtent(ground);
    const [x, y] = at;
    if (x < minimumX || x > maximumX || y < minimumY || y > maximumY) return "";
    const box: [number, number, number, number] = [minimumX, minimumY, maximumX, maximumY];
    const parts: string[] = [];
    for (let tier = 0; tier < tiers; tier++) {
      const width = (box[2] - box[0]) / across;
      const height = (box[3] - box[1]) / across;
      const column = Math.min(across - 1, Math.floor((x - box[0]) / width));
      const row = Math.min(across - 1, Math.floor((box[3] - y) / height));
      box[0] += column * width;
      box[2] = box[0] + width;
      box[3] -= row * height;
      box[1] = box[3] - height;
      parts.push(String(row * across + column));
    }
    return parts.join(separator);
  };

  return {
    level(id) {
      return (segments(id) ?? []).length;
    },
    parent(id) {
      return id.split(separator).slice(0, -1).join(separator);
    },
    children(id) {
      return Array.from({ length: perLevel }, (_, at) =>
        id === "" ? String(at) : `${id}${separator}${at}`);
    },
    childIndex(id) {
      const parts = segments(id);
      const last = parts?.[parts.length - 1];
      return last ?? 0;
    },
    contains(id, at) {
      const [minimumX, minimumY, maximumX, maximumY] = bbox(ground, id);
      const [x, y] = at;
      return x >= minimumX && x <= maximumX && y >= minimumY && y <= maximumY;
    },
    descendTarget(id, at) {
      const level = (segments(id) ?? []).length;
      if (level >= depth) return "";
      return cellAt(at, level + 1);
    },
    bbox(id) {
      return bbox(ground, id);
    },
    center(id) {
      const [minimumX, minimumY, maximumX, maximumY] = bbox(ground, id);
      return [(minimumX + maximumX) / 2, (minimumY + maximumY) / 2];
    },
    ring(id): Ring {
      const [minimumX, minimumY, maximumX, maximumY] = bbox(ground, id);
      return [
        [minimumX, minimumY],
        [minimumX, maximumY],
        [maximumX, maximumY],
        [maximumX, minimumY],
        [minimumX, minimumY],
      ];
    },
    poleContained(): Pole | null {
      return null;
    },
    label(id): CellLabel {
      const parts = id === "" ? [] : id.split(separator);
      return {
        context: parts.slice(0, -1).join(separator),
        principal: parts[parts.length - 1] ?? "",
      };
    },
    colorKey(id) {
      const parts = segments(id);
      const last = parts?.[parts.length - 1];
      return (last ?? 0) % palette.length;
    },
    normalizeInput(text) {
      const kept = [...text.toLocaleLowerCase()]
        .filter((character) => "012345678-".includes(character))
        .join("");
      return kept
        .split(separator)
        .filter((part) => part !== "")
        .slice(0, depth)
        .join(separator);
    },
    // A draft that is not a whole address is not a place.
    parseInput(text) {
      const parts = segments(text);
      return parts === null ? null : text;
    },
    locate(at): LocatedCell | null {
      const value = cellAt(at, depth);
      return value ? { label: "District", value } : null;
    },
  };
}

export const districtSystem: CellSystem = {
  slug: "districts",
  name: "Districts",
  short: "D",

  // A district grid is a curation choice about one game's map, so it divides
  // only a ground whose world says it has districts.
  appliesTo(ground: Ground): boolean {
    return ground.world.attrs["atlas.district.grid"] === "3x3";
  },

  maxLevel(): number {
    return depth;
  },

  // Three tiers and two separators.
  inputLength(): number {
    return depth * 2 - 1;
  },

  on: grounded,
};
