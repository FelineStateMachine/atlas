// The S2 cell system: the sphere divided by the six faces of a cube
// projected onto it, each face a quadtree of exactly-nested quadrilateral
// cells addressed by Hilbert-curve tokens. It is the second system through
// the port and the proof the port holds nothing geohash-shaped: identities
// that do not refine by appending characters (the children of 47a1cb are
// 47a1ca4 and its siblings), cells whose edges are straight on neither
// projection, and an address that only means something on a map declaring
// what its picture is of. The math is s2js's -- the Go library ported,
// test vectors and all -- and this file only bridges it to world pixels
// through the map's declared flattening.
import { s2 } from "s2js";

import { palette } from "../constants.js";
import { geoMapping, worldSurface } from "../semconv.js";
import { state } from "../state.js";

import { surfaceExtent } from "./index.js";

// The telescope stops at S2 level 10 -- cells a few kilometers across on a
// Mars-sized body, tokens six characters long -- which is depth enough for
// a couple thousand pins and shallow enough that every level stays a
// place, not a coordinate.
const deepestS2Level = 10;

// Port levels count the root as 0 and the six faces as 1; S2 counts the
// faces as level 0. The two bookkeepings meet here and nowhere else.
const portLevel = (s2Level) => s2Level + 1;
const s2Level = (id) => s2.cellid.level(s2.cellid.fromToken(id));

// The mapping bridge, remembered per map: world pixels are only meaningful
// through the flattening the map declares.
let bridge = { map: null, mapping: null };

function mapping() {
  if (bridge.map !== state.world) {
    bridge = { map: state.world, mapping: geoMapping(state.world) };
  }
  return bridge.mapping;
}

function latLngOf(coordinate) {
  const held = mapping();
  if (!held) return null;
  const [lat, lng] = held.toLatLng(coordinate[0], -coordinate[1]);
  return s2.LatLng.fromDegrees(lat, lng);
}

function degreesOf(point) {
  const ll = s2.LatLng.fromPoint(point);
  return [(ll.lat * 180) / Math.PI, (ll.lng * 180) / Math.PI];
}

// Leafs are looked up per pin every filter pass, so each coordinate's leaf
// is remembered against the mapping that produced it.
const leafMemo = new WeakMap();

function leafOf(coordinate) {
  const held = mapping();
  if (!held) return 0n;
  const memo = leafMemo.get(coordinate);
  if (memo && memo.mapping === held) return memo.leaf;
  const ll = latLngOf(coordinate);
  const leaf = ll ? s2.cellid.fromLatLng(ll) : 0n;
  if (typeof coordinate === "object") leafMemo.set(coordinate, { mapping: held, leaf });
  return leaf;
}

export const s2System = {
  slug: "s2",
  name: "S2",
  short: "S2",

  // Only a map that declares what its picture is of -- a sphere, through
  // any invertible flattening -- can be divided geodesically.
  appliesTo(map) {
    return worldSurface(map) === "sphere" && geoMapping(map) !== null;
  },

  maxLevel() {
    return portLevel(deepestS2Level);
  },

  // A level-10 token is six characters; the field takes no more.
  inputLength() {
    return 6;
  },

  level(id) {
    return id === "" ? 0 : portLevel(s2Level(id));
  },

  parent(id) {
    const ci = s2.cellid.fromToken(id);
    const level = s2.cellid.level(ci);
    if (level === 0) return "";
    return s2.cellid.toToken(s2.cellid.parent(ci, level - 1));
  },

  children(id) {
    if (id === "") {
      return [0, 1, 2, 3, 4, 5].map((face) => s2.cellid.toToken(s2.cellid.fromFace(face)));
    }
    return s2.cellid.children(s2.cellid.fromToken(id)).map(s2.cellid.toToken);
  },

  childIndex(id) {
    const ci = s2.cellid.fromToken(id);
    const level = s2.cellid.level(ci);
    return level === 0 ? s2.cellid.face(ci) : s2.cellid.childPosition(ci, level);
  },

  contains(id, coordinate) {
    return s2.cellid.contains(s2.cellid.fromToken(id), leafOf(coordinate));
  },

  descendTarget(id, coordinate) {
    const leaf = leafOf(coordinate);
    if (leaf === 0n) return "";
    const target = id === "" ? 0 : s2Level(id) + 1;
    if (target > deepestS2Level) return "";
    return s2.cellid.toToken(s2.cellid.parent(leaf, target));
  },

  bbox(id) {
    if (id === "") return surfaceExtent();
    const ring = this.ring(id);
    let minimumX = Infinity;
    let minimumY = Infinity;
    let maximumX = -Infinity;
    let maximumY = -Infinity;
    for (const [x, y] of ring) {
      minimumX = Math.min(minimumX, x);
      minimumY = Math.min(minimumY, y);
      maximumX = Math.max(maximumX, x);
      maximumY = Math.max(maximumY, y);
    }
    return [minimumX, minimumY, maximumX, maximumY];
  },

  center(id) {
    const held = mapping();
    const [lat, lng] = degreesOf(s2.Cell.fromCellID(s2.cellid.fromToken(id)).center());
    const [x, y] = held.toWorld(lat, lng);
    return [x, -y];
  },

  // The boundary as world pixels: the four geodesic edges, each walked in
  // small great-circle steps on the sphere itself, then unwrapped so a
  // loop that crossed the antimeridian stays continuous -- the adapters
  // wrap or split it, each in its own way.
  ring(id) {
    const held = mapping();
    const cell = s2.Cell.fromCellID(s2.cellid.fromToken(id));
    const level = s2.cellid.level(s2.cellid.fromToken(id));
    const steps = Math.max(2, Math.min(16, 2 ** (4 - level) * 2));
    const surface = surfaceExtent();
    const width = surface[2] - surface[0];
    const points = [];
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
        const [worldX, worldY] = held.toWorld(lat, lng);
        points.push([worldX, -worldY]);
      }
    }
    // Sequential unwrap: a jump of more than half the world is the seam,
    // not the ground.
    for (let at = 1; at < points.length; at++) {
      let delta = points[at][0] - points[at - 1][0];
      if (delta > width / 2) points[at][0] -= width;
      else if (delta < -width / 2) points[at][0] += width;
    }
    // The closing point joins the loop in the frame the walk ended in, not
    // the one it began in: a pole cell's loop accumulates a whole world of
    // longitude, and closing back to the original frame would hand the
    // adapters a segment sweeping the entire planet -- a phantom edge the
    // boundary draws and the fill fans spokes to.
    const closing = [...points[0]];
    const delta = closing[0] - points[points.length - 1][0];
    if (delta > width / 2) closing[0] -= width;
    else if (delta < -width / 2) closing[0] += width;
    points.push(closing);
    return points;
  },

  poleContained(id) {
    const ci = s2.cellid.fromToken(id);
    if (s2.cellid.contains(ci, s2.cellid.fromLatLng(s2.LatLng.fromDegrees(90, 0)))) {
      return "north";
    }
    if (s2.cellid.contains(ci, s2.cellid.fromLatLng(s2.LatLng.fromDegrees(-90, 0)))) {
      return "south";
    }
    return null;
  },

  label(id) {
    return { context: id.slice(0, -1), principal: id.slice(-1) };
  },

  // Sibling tokens can share their final character, so the accent comes
  // from the sibling ordinal instead -- neighbors always differ.
  colorKey(id) {
    return this.childIndex(id) % palette.length;
  },

  normalizeInput(text) {
    return [...text.toLocaleLowerCase()]
      .filter((character) => "0123456789abcdef".includes(character))
      .slice(0, 6)
      .join("");
  },

  // A partial token is not yet a place: the field holds the draft and the
  // state stays put until the address parses whole. Deeper tokens clamp to
  // the telescope's floor, re-tokenized so the spelling is canonical.
  parseInput(text) {
    if (text === "") return "";
    const ci = s2.cellid.fromToken(text);
    if (ci === 0n || !s2.cellid.valid(ci)) return null;
    const level = s2.cellid.level(ci);
    if (level > deepestS2Level) {
      return s2.cellid.toToken(s2.cellid.parent(ci, deepestS2Level));
    }
    return s2.cellid.toToken(ci);
  },

  locate(coordinate) {
    const leaf = leafOf(coordinate);
    if (leaf === 0n) return null;
    return {
      label: "S2",
      value: s2.cellid.toToken(s2.cellid.parent(leaf, deepestS2Level)),
    };
  },
};
