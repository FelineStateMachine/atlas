// The geohash cell system: recursive halving of the map's surface in world
// pixels, 32 children to a cell, addresses that refine by appending a
// character. It divides any map at all, because it never asks what the
// picture is of -- which is why it was the first system and why its math
// moved here VERBATIM: the parity harness compares its floats and strings
// byte for byte, and nothing about the extraction may move them.
import { geohashAlphabet, geohashMaxDepth, palette } from "../constants.js";

import { surfaceExtent } from "./index.js";

// bbox is the recursive halving itself, unchanged from the original
// geohashExtent: alternate axes, five bits to a character.
function bbox(hash) {
  const extent = [...surfaceExtent()];
  let splitX = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of [16, 8, 4, 2, 1]) {
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

// cellAt is the reverse of bbox: the same halvings, choosing at each one
// the side the point is on. Computed when asked rather than stored,
// because the grid divides the ground a lens covers and a split world
// gives each layer its own -- a hash stored beside a location could only
// be right for one.
function cellAt(coordinate, depth) {
  const extent = [...surfaceExtent()];
  const [x, y] = coordinate;
  if (x < extent[0] || x > extent[2] || y < extent[1] || y > extent[3]) return "";
  let splitX = true;
  let hash = "";
  for (let level = 0; level < depth; level++) {
    let value = 0;
    for (const mask of [16, 8, 4, 2, 1]) {
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
    hash += geohashAlphabet[value];
  }
  return hash;
}

let memo = { key: null, extent: null };

function containsBox(id) {
  const surface = surfaceExtent();
  const key = `${surface.join(",")}|${id}`;
  if (memo.key !== key) memo = { key, extent: bbox(id) };
  return memo.extent;
}

export const geohashSystem = {
  slug: "geohash",
  name: "Geohash",
  short: "G",

  appliesTo() {
    return true;
  },

  maxLevel() {
    return geohashMaxDepth;
  },

  inputLength() {
    return geohashMaxDepth;
  },

  level(id) {
    return id.length;
  },

  parent(id) {
    return id.slice(0, -1);
  },

  children(id) {
    return [...geohashAlphabet].map((character) => id + character);
  },

  childIndex(id) {
    return Math.max(0, geohashAlphabet.indexOf(id[id.length - 1]));
  },

  contains(id, coordinate) {
    // Callers sweep thousands of pins against one cell at a time, so the
    // last cell's box is remembered rather than re-halved per pin. The
    // memo keys on the surface too: a lens switch moves every box.
    const extent = containsBox(id);
    const [x, y] = coordinate;
    return x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3];
  },

  descendTarget(id, coordinate) {
    return cellAt(coordinate, id.length + 1);
  },

  bbox,

  center(id) {
    const extent = bbox(id);
    return [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2];
  },

  ring(id) {
    const [minimumX, minimumY, maximumX, maximumY] = bbox(id);
    return [
      [minimumX, minimumY],
      [minimumX, maximumY],
      [maximumX, maximumY],
      [maximumX, minimumY],
      [minimumX, minimumY],
    ];
  },

  poleContained() {
    return null;
  },

  label(id) {
    return { context: id.slice(0, -1), principal: id.slice(-1) };
  },

  colorKey(id) {
    return Math.max(0, geohashAlphabet.indexOf(id[id.length - 1])) % palette.length;
  },

  normalizeInput(text) {
    return [...text.toLocaleLowerCase()]
      .filter((character) => geohashAlphabet.includes(character))
      .slice(0, geohashMaxDepth)
      .join("");
  },

  // Every normalized spelling is a place -- the empty string is the root
  // -- so parsing never refuses.
  parseInput(text) {
    return text;
  },

  locate(coordinate) {
    const value = cellAt(coordinate, geohashMaxDepth);
    return value ? { label: "Geohash", value } : null;
  },
};

// cellAtDepth serves the one caller that names a depth of its own: the
// pin card's fixed-depth address, independent of where the grid stands.
export function geohashCellAt(coordinate, depth = geohashMaxDepth) {
  return cellAt(coordinate, depth);
}
