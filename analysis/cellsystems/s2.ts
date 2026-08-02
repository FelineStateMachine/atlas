// The S2 cell system: the sphere divided by the six faces of a cube projected
// onto it, each face a quadtree of exactly-nested quadrilateral cells
// addressed by Hilbert-curve tokens.
//
// It is the proof the contract holds nothing geohash-shaped. Identities do not
// refine by appending characters — the children of 47a1cb are 47a1ca4 and its
// siblings. Cells have edges that are straight on neither projection. And an
// address only means something on a ground that declares what its picture is
// of, so this is the system that can refuse a ground.
//
// The spherical math is s2js's — the Go library ported, test vectors and all,
// and the one third-party dependency this lane keeps (issue #5 §9's pinned
// surface). This file only bridges it to world pixels through the ground's
// declared flattening.

import { s2 } from "s2js";

import type { CellID, CellLabel, GroundedCellSystem, LocatedCell, Pole, Ring } from "./contract.ts";
import type { CellSystem } from "./contract.ts";
import type { Coordinate, Extent, Ground } from "./ground.ts";
import { surfaceExtent } from "./ground.ts";
import { palette } from "./tokens.ts";
import { geoMapping, worldSurface } from "../semconv/geometry.ts";
import type { GeoMapping, World } from "../semconv/geometry.ts";

/**
 * The telescope stops at S2 level 10 — cells a few kilometres across on a
 * Mars-sized body, tokens six characters long — which is depth enough for a
 * couple of thousand pins and shallow enough that every level stays a place
 * rather than a coordinate.
 */
const deepestS2Level = 10;

/**
 * Port levels count the root as 0 and the six faces as 1; S2 counts the faces
 * as level 0. The two bookkeepings meet here and nowhere else.
 */
const portLevel = (level: number): number => level + 1;

const levelOfToken = (id: CellID): number => s2.cellid.level(s2.cellid.fromToken(id));

/**
 * The mapping bridge, remembered per world: world pixels are only meaningful
 * through the flattening the ground declares, and reading it is parsing.
 */
const mappings = new WeakMap<World, GeoMapping | null>();

function mappingOf(ground: Ground): GeoMapping | null {
  const world = ground.world;
  const held = mappings.get(world);
  if (held !== undefined) return held;
  const fresh = geoMapping(world);
  mappings.set(world, fresh);
  return fresh;
}

/** Degrees from a point on the unit sphere. */
function degreesOf(point: s2.Point): readonly [number, number] {
  const ll = s2.LatLng.fromPoint(point);
  return [(ll.lat * 180) / Math.PI, (ll.lng * 180) / Math.PI];
}

/**
 * The leaf cell under a coordinate, remembered against the mapping that
 * produced it: leaves are looked up per pin on every filter pass, and a
 * coordinate is a stable object for exactly as long as its pin is.
 */
const leaves = new WeakMap<object, { mapping: GeoMapping; leaf: bigint }>();

function leafOf(ground: Ground, coordinate: Coordinate): bigint {
  const mapping = mappingOf(ground);
  if (!mapping) return 0n;
  const memo = leaves.get(coordinate);
  if (memo && memo.mapping === mapping) return memo.leaf;
  const [lat, lng] = mapping.toLatLng(coordinate[0], -coordinate[1]);
  const leaf = s2.cellid.fromLatLng(s2.LatLng.fromDegrees(lat, lng));
  leaves.set(coordinate, { mapping, leaf });
  return leaf;
}

/**
 * The boundary as world pixels: the four geodesic edges, each walked in small
 * great-circle steps on the sphere itself, then unwrapped so a loop that
 * crossed the antimeridian stays continuous. The adapters wrap or split it,
 * each in its own way.
 */
function ringOf(ground: Ground, mapping: GeoMapping, id: CellID): Coordinate[] {
  const cellID = s2.cellid.fromToken(id);
  const cell = s2.Cell.fromCellID(cellID);
  const level = s2.cellid.level(cellID);
  const steps = Math.max(2, Math.min(16, 2 ** (4 - level) * 2));
  const surface = surfaceExtent(ground);
  const width = surface[2] - surface[0];
  const points: [number, number][] = [];
  for (let edge = 0; edge < 4; edge++) {
    const from = cell.vertex(edge).vector;
    const to = cell.vertex((edge + 1) % 4).vector;
    for (let step = 0; step < steps; step++) {
      const t = step / steps;
      const x = from.x + (to.x - from.x) * t;
      const y = from.y + (to.y - from.y) * t;
      const z = from.z + (to.z - from.z) * t;
      const norm = Math.hypot(x, y, z) || 1;
      const [lat, lng] = degreesOf(new s2.Point(x / norm, y / norm, z / norm));
      const [worldX, worldY] = mapping.toWorld(lat, lng);
      points.push([worldX, -worldY]);
    }
  }
  // Sequential unwrap: a jump of more than half the world is the seam, not
  // the ground.
  for (let at = 1; at < points.length; at++) {
    const current = points[at];
    const previous = points[at - 1];
    if (!current || !previous) continue;
    const delta = current[0] - previous[0];
    if (delta > width / 2) current[0] -= width;
    else if (delta < -width / 2) current[0] += width;
  }
  // The closing point joins the loop in the frame the walk ended in, not the
  // one it began in: a pole cell's loop accumulates a whole world of
  // longitude, and closing back to the original frame would hand the adapters
  // a segment sweeping the entire planet — a phantom edge the boundary draws
  // and the fill fans spokes to.
  const opening = points[0];
  const ending = points[points.length - 1];
  if (!opening || !ending) return points;
  const closing: [number, number] = [opening[0], opening[1]];
  const delta = closing[0] - ending[0];
  if (delta > width / 2) closing[0] -= width;
  else if (delta < -width / 2) closing[0] += width;
  points.push(closing);
  return points;
}

/**
 * The mapping this ground declares, or a refusal. Every geometric method needs
 * it, and `appliesTo` is the question a caller was supposed to ask first.
 */
function requireMapping(ground: Ground): GeoMapping {
  const mapping = mappingOf(ground);
  if (!mapping) {
    throw new Error(
      "s2: this ground declares no invertible flattening — ask appliesTo(ground) first " +
      "(analysis/cellsystems, issue #5 §5.4)",
    );
  }
  return mapping;
}

/** The surface as a rectangle: the root's own geometry, since the root is the ground. */
function surfaceRing(ground: Ground): Coordinate[] {
  const [minimumX, minimumY, maximumX, maximumY] = surfaceExtent(ground);
  return [
    [minimumX, minimumY],
    [minimumX, maximumY],
    [maximumX, maximumY],
    [maximumX, minimumY],
    [minimumX, minimumY],
  ];
}

// The root is `""` in every system (contract.ts), and S2's token vocabulary
// has no spelling for it — `fromToken("")` answers a cell id that is not the
// sphere and not an error. Every method below that could be handed the root
// answers for the whole ground instead of asking s2js a question it cannot
// hear. None of these cases is reachable from a plan (the root is never a plan
// cell) which is why the oracle never had to have an answer; the contract says
// it should, so this one does. See docs/analysis.md, "the root cases".

/** S2, bound to one ground. */
function grounded(ground: Ground): GroundedCellSystem {
  const system: GroundedCellSystem = {
    level(id: CellID): number {
      return id === "" ? 0 : portLevel(levelOfToken(id));
    },

    parent(id: CellID): CellID {
      if (id === "") return "";
      const cellID = s2.cellid.fromToken(id);
      const level = s2.cellid.level(cellID);
      if (level === 0) return "";
      return s2.cellid.toToken(s2.cellid.parent(cellID, level - 1));
    },

    children(id: CellID): CellID[] {
      if (id === "") {
        return [0, 1, 2, 3, 4, 5].map((face) => s2.cellid.toToken(s2.cellid.fromFace(face)));
      }
      return s2.cellid.children(s2.cellid.fromToken(id)).map(s2.cellid.toToken);
    },

    childIndex(id: CellID): number {
      if (id === "") return 0;
      const cellID = s2.cellid.fromToken(id);
      const level = s2.cellid.level(cellID);
      return level === 0 ? s2.cellid.face(cellID) : s2.cellid.childPosition(cellID, level);
    },

    // The root is the whole sphere: it holds every point the ground can name.
    contains(id: CellID, at: Coordinate): boolean {
      const leaf = leafOf(ground, at);
      if (id === "") return leaf !== 0n;
      return s2.cellid.contains(s2.cellid.fromToken(id), leaf);
    },

    descendTarget(id: CellID, at: Coordinate): CellID {
      const leaf = leafOf(ground, at);
      if (leaf === 0n) return "";
      const target = id === "" ? 0 : levelOfToken(id) + 1;
      if (target > deepestS2Level) return "";
      return s2.cellid.toToken(s2.cellid.parent(leaf, target));
    },

    // The root is the whole ground: six faces cover a sphere, and their union
    // in world pixels is the picture itself.
    bbox(id: CellID): Extent {
      if (id === "") return surfaceExtent(ground);
      let minimumX = Infinity;
      let minimumY = Infinity;
      let maximumX = -Infinity;
      let maximumY = -Infinity;
      for (const [x, y] of system.ring(id)) {
        minimumX = Math.min(minimumX, x);
        minimumY = Math.min(minimumY, y);
        maximumX = Math.max(maximumX, x);
        maximumY = Math.max(maximumY, y);
      }
      return [minimumX, minimumY, maximumX, maximumY];
    },

    center(id: CellID): Coordinate {
      const mapping = requireMapping(ground);
      if (id === "") {
        const [minimumX, minimumY, maximumX, maximumY] = surfaceExtent(ground);
        return [(minimumX + maximumX) / 2, (minimumY + maximumY) / 2];
      }
      const [lat, lng] = degreesOf(s2.Cell.fromCellID(s2.cellid.fromToken(id)).center());
      const [x, y] = mapping.toWorld(lat, lng);
      return [x, -y];
    },

    ring(id: CellID): Ring {
      const mapping = requireMapping(ground);
      if (id === "") return surfaceRing(ground);
      return ringOf(ground, mapping, id);
    },

    // The root circles both poles, and no single answer says so; nothing asks,
    // because a pole closure is a question about a cell being drawn.
    poleContained(id: CellID): Pole | null {
      if (id === "") return null;
      const cellID = s2.cellid.fromToken(id);
      if (s2.cellid.contains(cellID, s2.cellid.fromLatLng(s2.LatLng.fromDegrees(90, 0)))) {
        return "north";
      }
      if (s2.cellid.contains(cellID, s2.cellid.fromLatLng(s2.LatLng.fromDegrees(-90, 0)))) {
        return "south";
      }
      return null;
    },

    label(id: CellID): CellLabel {
      return { context: id.slice(0, -1), principal: id.slice(-1) };
    },

    // Sibling tokens can share their final character, so the accent comes from
    // the sibling ordinal instead — neighbours always differ.
    colorKey(id: CellID): number {
      return system.childIndex(id) % palette.length;
    },

    normalizeInput(text: string): string {
      return [...text.toLocaleLowerCase()]
        .filter((character) => "0123456789abcdef".includes(character))
        .slice(0, 6)
        .join("");
    },

    // A partial token is not yet a place: the field holds the draft and the
    // session stays put until the address parses whole. A deeper token clamps
    // to the telescope's floor, re-tokenized so the spelling is canonical.
    parseInput(text: string): CellID | null {
      if (text === "") return "";
      const cellID = s2.cellid.fromToken(text);
      if (cellID === 0n || !s2.cellid.valid(cellID)) return null;
      const level = s2.cellid.level(cellID);
      if (level > deepestS2Level) {
        return s2.cellid.toToken(s2.cellid.parent(cellID, deepestS2Level));
      }
      return s2.cellid.toToken(cellID);
    },

    locate(at: Coordinate): LocatedCell | null {
      const leaf = leafOf(ground, at);
      if (leaf === 0n) return null;
      return { label: "S2", value: s2.cellid.toToken(s2.cellid.parent(leaf, deepestS2Level)) };
    },
  };
  return system;
}

export const s2System: CellSystem = {
  slug: "s2",
  name: "S2",
  short: "S2",

  // Only a ground that declares what its picture is of — a sphere, through any
  // invertible flattening — can be divided geodesically.
  appliesTo(ground: Ground): boolean {
    return worldSurface(ground.world) === "sphere" && geoMapping(ground.world) !== null;
  },

  maxLevel(): number {
    return portLevel(deepestS2Level);
  },

  // A level-10 token is six characters; the field takes no more.
  inputLength(): number {
    return 6;
  },

  on: grounded,
};
