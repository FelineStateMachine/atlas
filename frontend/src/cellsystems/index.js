// A cell system divides a map's ground into an exact hierarchy the reader
// can telescope through. Geohash was the first and grew into the app's
// bones; this package is the port that pulls it back out, so a second
// system is one more file rather than another growth.
//
// The contract every system keeps:
//
//   slug, name              identity, and how the navigator names it
//   appliesTo(map)          whether this system can divide this map
//   maxLevel(map)           how deep the telescope goes
//   inputLength(map)        how many characters the navigator accepts
//   level(id)               depth of a cell; the root "" is level 0
//   parent(id)              one level up; "" from a level-1 cell
//   children(idOrRoot)      next level down, in a STABLE order
//   childIndex(id)          ordinal among siblings, for priority and color
//   contains(id, olXY)      whether a point is inside, boundaries INCLUSIVE
//   descendTarget(id, olXY) the child of id under the point, or ""
//   bbox(id)                [minX, minY, maxX, maxY] in OL world coords
//   center(id)              a point inside the cell, OL world coords
//   ring(id)                closed boundary loop, pre-tessellated, OL
//                           coords, CONTINUOUS across the antimeridian --
//                           an unwrapped ring may run past the surface's
//                           x-range, and the adapters wrap or split it
//   poleContained(id)       "north" | "south" | null, for ring closure
//   label(id)               { context, principal } -- the chip's two cuts
//   colorKey(id)            palette index for the cell's accent
//   normalizeInput(text)    what the navigator field keeps of a keystroke
//   parseInput(text)        a canonical id, or null while the text is not
//                           yet a place
//   locate(olXY, map)       { label, value } naming the point in this
//                           system's own address, for the pin card --
//                           each layer owns the attribute it mints
//
// Identifiers are opaque strings and "" is the root. Coordinates are OL
// world coordinates -- the space pin.coordinate lives in, x east and y
// negative-down.
import { state } from "../state.js";

import { geohashSystem } from "./geohash.js";

// surfaceExtent is the ground a cell system divides: the variant's surface
// where one is declared, its raster window otherwise, the whole world
// square when nothing narrower is known. The same numbers activeExtent
// produces, computed here without OpenLayers so the systems stay pure and
// node-testable.
export function surfaceExtent() {
  const surface = state.variant?.surface;
  if (surface) {
    return [
      surface.x,
      -(surface.y + surface.height),
      surface.x + surface.width,
      -surface.y,
    ];
  }
  const size = state.game?.tileGrid.size ?? 0;
  const bounds = state.variant?.bounds || { x: 0, y: 0, width: size, height: size };
  return [bounds.x, -(bounds.y + bounds.height), bounds.x + bounds.width, -bounds.y];
}

// The registry, in the order the navigator offers them.
export const cellSystems = [geohashSystem];

export function applicableSystems(map) {
  return cellSystems.filter((system) => system.appliesTo(map));
}

// activeSystem answers for whatever the reader chose, and for geohash when
// nothing chose otherwise -- a map that offers less than the state names
// must still divide sensibly.
export function activeSystem() {
  return cellSystems.find((system) => system.slug === state.gridSystem) || geohashSystem;
}
