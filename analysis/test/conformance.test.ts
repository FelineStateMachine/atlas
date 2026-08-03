// Every system, held to the same properties.
//
// Geohash and S2 are the two Atlas ships. `districts` is a third, written in
// the test tree for one run and never registered: issue #5 §5.4 says a third
// system is a registry entry plus a green conformance run, and the cheapest
// honest way to keep that promise true is to keep a third system passing.

import { describeConformance } from "./conformance.ts";
import { districtSystem } from "./districts.ts";
import { geohashSystem } from "../cellsystems/geohash.ts";
import { s2System } from "../cellsystems/s2.ts";
import type { Ground } from "../cellsystems/ground.ts";
import { anchored, sphere, square, squareFromBounds } from "./grounds.ts";

/** The 1024 square, told it has districts. */
const districted: Ground = {
  ...square,
  world: { attrs: { "atlas.district.grid": "3x3" } },
};

describeConformance("geohash on a plain square", {
  system: geohashSystem,
  ground: square,
  probe: [688, -688],
  // A plane's geohash divides the world square, which here is also the lens's
  // whole surface — but the root's top edge is the square's own 0, not the
  // surface ladder's -0.
  rootExtent: [0, -1024, 1024, 0],
  // A halved rectangle's corner is exactly on its own boundary, and geohash
  // decides containment on the same arithmetic that placed it there.
  boundaryProbes: [
    ["m6", [704, -672]],
    ["m6", [672, -704]],
    ["", [0, -1024]],
    ["", [1024, 0]],
  ],
});

describeConformance("geohash on a lens that declares only bounds", {
  system: geohashSystem,
  ground: squareFromBounds,
  probe: [300, -400],
  // A plane's grid covers the FULL map area whatever window the lens fills,
  // so the ground divided here is the 1024 square, not the 512 × 256 window.
  rootExtent: [0, -1024, 1024, 0],
  boundaryProbes: [
    ["", [0, -1024]],
    ["", [1024, 0]],
  ],
});

describeConformance("real geohash on an earth-anchored plane", {
  system: geohashSystem,
  ground: anchored,
  probe: [2048, -2048],
  // The root is the union of the twelve base cells the 0.8° × 0.5° window
  // intersects: columns 164–167 and rows 765–767 of the depth-4 grid, i.e.
  // -122.34375 … -120.9375 by 44.47265625 … 45. Through the window's own
  // formulas (x linear in longitude over 0.8° → 4096 px, y linear in
  // asinh(tan lat)) that is x = -0.34375/0.8·4096 … 1.0625/0.8·4096 and the
  // top row's north edge lands exactly on the window's own top, y = -0.
  rootExtent: [-1760.0000000000064, -4318.983024242055, 5440.000000000019, -0],
  // Declared empty, deliberately: a real cell's boundary is exact in
  // DEGREES — binary fractions of 360 — and a world-pixel probe reaches it
  // only through asinh(tan φ) and back, which can land the round-trip an ulp
  // to either side. The inclusive rule is a rule about the system's own
  // boundary arithmetic, and that arithmetic lives in degrees.
  boundaryProbes: [],
});

describeConformance("geohash on a sphere", {
  system: geohashSystem,
  ground: sphere,
  probe: [4363.848398961777, -916.9692745045334],
  boundaryProbes: [
    ["", [0, -4096]],
    ["", [8192, 0]],
  ],
});

describeConformance("s2 on an equirectangular sphere", {
  system: s2System,
  ground: sphere,
  probe: [4363.848398961777, -916.9692745045334],
  // Declared empty, deliberately. S2 decides containment on the leaf lattice —
  // thirty levels below anything this grid draws — so a ring vertex, which has
  // travelled through degrees and back, lands in whichever leaf the rounding
  // put it in and may belong to the neighbour. The inclusive rule is a rule
  // about a system's own boundary arithmetic, and S2's boundary is not
  // representable in the coordinates a caller can hand it.
  boundaryProbes: [],
});

// S2 is deliberately NOT run over `mercatorCity`, and the reason is a real
// limit of the recorded design rather than an oversight: see
// `s2-limits.test.ts`, which pins the limit so nobody rediscovers it as a bug.

describeConformance("a third system nobody designed the contract for", {
  system: districtSystem,
  ground: districted,
  probe: [688, -688],
  boundaryProbes: [
    ["4", [341.3333333333333, -682.6666666666666]],
    ["", [0, -1024]],
    ["", [1024, 0]],
  ],
});
