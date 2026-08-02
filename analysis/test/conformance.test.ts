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
import { sphere, square, squareFromBounds } from "./grounds.ts";

/** The 1024 square, told it has districts. */
const districted: Ground = {
  ...square,
  world: { attrs: { "atlas.district.grid": "3x3" } },
};

describeConformance("geohash on a plain square", {
  system: geohashSystem,
  ground: square,
  probe: [688, -688],
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
  boundaryProbes: [
    ["", [128, -512]],
    ["", [640, -256]],
  ],
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
