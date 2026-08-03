// A feature made of several pieces is drawn as several pieces.
//
// This suite exists because it was not. `chart/element.ts` built one
// `Polygon` per area out of a flat list of every ring the feature owned, and
// one `LineString` per path out of the *first* run of it. Both are lossy in
// the same direction, and both are silent:
//
//   AN AREA. A polygon reads its first ring as the exterior and every ring
//   after it as a hole. A district of two separate pieces of ground therefore
//   drew its second piece as a hole punched out of its first — the city's
//   `RS` zoning is fifty-three pieces, so fifty-two of them were subtracted
//   from the one that happened to come first.
//
//   A PATH. A line string holds one run. The Deschutes River Trail is
//   recorded as a hundred and forty-seven runs, and a hundred and forty-six
//   of them were dropped on the floor without a word.
//
// The model never lost the structure — `lines[i]` is a part and `holes[i]`
// belongs to *that* part (`world/model.ts`) — so the whole of the fix is to
// stop flattening it on the way to OpenLayers, and the whole of this suite is
// to hold the structure at every place it is read: the drawn geometry, the
// scrim's cut-outs, the title anchor and the containment test.
//
// The first cases are synthetic, because a two-piece square with a hole in
// each piece says in six coordinates what a real district says in six
// thousand. The last three are features read out of
// `testdata/corpus/bundles`, so the counts are a real producer's — two of
// the city's own, named, and then every shape of every corpus volume, plus
// the invented game plane's, held against the part structure its payload
// declares.

import test from "node:test";
import { strict as assert } from "node:assert";
import type MultiLineString from "ol/geom/MultiLineString.js";
import type MultiPolygon from "ol/geom/MultiPolygon.js";
import { WorldModel, worldGrid } from "../world/model.ts";
import type { Coordinate, Line, ShapeRecord } from "../world/model.ts";
import { shapeContains } from "../world/visibility.ts";
import type { Collection } from "../data/payload.ts";
import { payloads, tileGrid, volumes } from "./fixtures.ts";
import { gamePlane } from "./models.ts";

// The seam's chart element extends `HTMLElement` at the moment it is defined,
// which is one browser global too many for `node --test`. Nothing else in the
// module reaches for the page outside a method, so a bare stand-in is enough
// to import it and ask its geometry builders what they build.
(globalThis as unknown as { HTMLElement: unknown }).HTMLElement = class {};
const { centreOf, parts, scrimGeometry, shapeGeometry } = await import("../chart/element.ts");

const AREAS: Collection = { id: 1, title: "Districts", kind: "area", visible: true };
const PATHS: Collection = { id: 2, title: "Trails", kind: "path", visible: true };

/** A square, closed, running counter-clockwise. */
function square(x: number, y: number, side: number): Line {
  return [[x, y], [x + side, y], [x + side, y + side], [x, y + side], [x, y]];
}

function shape(
  kind: "area" | "path", lines: Line[], holes: Line[][] = [],
): ShapeRecord {
  return {
    id: "1", title: "Two Pieces", subtitle: "", collection: kind === "area" ? AREAS : PATHS,
    kind, shard: 0, lines, holes, center: null,
    feature: { id: 1, title: "Two Pieces", geometry: [] },
  };
}

/**
 * Two separate pieces of ground, each with a hole of its own.
 *
 * The holes are what tell a per-part builder from a flattening one: a
 * flattener produces four rings on one polygon, and there is no arrangement
 * of four rings on one polygon that means this.
 */
const TWO_PIECES = shape(
  "area",
  [square(0, 0, 10), square(20, 0, 10)],
  [[square(3, 3, 4)], [square(23, 3, 4)]],
);

/** Twice the signed area of a ring: positive counter-clockwise. */
function signedArea(ring: readonly Coordinate[]): number {
  let area = 0;
  for (let i = 0, prior = ring.length - 1; i < ring.length; prior = i++) {
    const a = ring[prior];
    const b = ring[i];
    if (!a || !b) continue;
    area += a[0] * b[1] - b[0] * a[1];
  }
  return area;
}

/** The same ring, whichever way round it is drawn. */
function sameRing(drawn: readonly Coordinate[], source: readonly Coordinate[]): boolean {
  const spell = (ring: readonly Coordinate[]) => JSON.stringify(ring);
  return spell(drawn) === spell(source) || spell(drawn) === spell([...source].reverse());
}

test("an area of several pieces draws as several filled parts", () => {
  const geometry = shapeGeometry(TWO_PIECES) as MultiPolygon;
  assert.equal(geometry.getType(), "MultiPolygon");
  const polygons = geometry.getPolygons();
  assert.equal(polygons.length, 2, "two pieces of ground are two polygons");
  // The defect in one assertion: flattened, this is one polygon of four
  // rings, and the second piece of ground is the first piece's hole.
  for (const polygon of polygons) {
    assert.equal(polygon.getLinearRings().length, 2, "an exterior and its own hole");
  }
  assert.deepEqual(polygons[0]?.getLinearRing(0)?.getCoordinates(), square(0, 0, 10));
  assert.deepEqual(polygons[1]?.getLinearRing(0)?.getCoordinates(), square(20, 0, 10));
});

test("a hole stays with the part that owns it", () => {
  assert.deepEqual(parts(TWO_PIECES), [
    [square(0, 0, 10), square(3, 3, 4)],
    [square(20, 0, 10), square(23, 3, 4)],
  ]);
  // And a part whose own hole list is missing is an exterior alone rather
  // than an exterior wearing the next part's holes.
  assert.deepEqual(parts(shape("area", [square(0, 0, 10), square(20, 0, 10)], [])), [
    [square(0, 0, 10)], [square(20, 0, 10)],
  ]);
});

test("a path keeps every run of itself", () => {
  const runs: Line[] = [[[0, 0], [1, 1]], [[5, 5], [6, 6], [7, 7]], [[9, 9], [10, 10]]];
  const geometry = shapeGeometry(shape("path", runs)) as MultiLineString;
  assert.equal(geometry.getType(), "MultiLineString");
  assert.deepEqual(geometry.getCoordinates(), runs);
});

test("the scrim cuts each part out and puts each part's holes back", () => {
  const world = square(0, -100, 100);
  const rings = scrimGeometry(world, [TWO_PIECES, shape("path", [[[0, 0], [1, 1]]])])
    .getLinearRings().map((ring) => ring.getCoordinates() as Coordinate[]);
  // The world, two exteriors and two interiors. A highlighted path has no
  // ring to cut from the dimming and contributes none.
  assert.equal(rings.length, 5);
  const worldRing = rings[0];
  assert.ok(worldRing);
  assert.ok(sameRing(worldRing, world), "the first ring is the world rectangle");
  const outward = Math.sign(signedArea(worldRing));
  assert.notEqual(outward, 0);
  const expected = [
    { source: square(0, 0, 10), winding: -outward },
    { source: square(3, 3, 4), winding: outward },
    { source: square(20, 0, 10), winding: -outward },
    { source: square(23, 3, 4), winding: outward },
  ];
  expected.forEach((want, index) => {
    const ring = rings[index + 1];
    assert.ok(ring);
    assert.ok(sameRing(ring, want.source), `ring ${index + 1} is its source ring`);
    assert.equal(
      Math.sign(signedArea(ring)), want.winding,
      `ring ${index + 1}: an exterior is wound against the world, an interior with it`);
  });
});

test("a single-part shape's scrim is unchanged", () => {
  const one = shape("area", [square(0, 0, 10)]);
  const rings = scrimGeometry(square(0, -100, 100), [one])
    .getLinearRings().map((ring) => ring.getCoordinates() as Coordinate[]);
  assert.equal(rings.length, 2);
  const world = rings[0];
  const cut = rings[1];
  assert.ok(world && cut);
  assert.equal(Math.sign(signedArea(cut)), -Math.sign(signedArea(world)));
});

test("the title anchors at the middle of every part, holes or not", () => {
  assert.deepEqual(centreOf(TWO_PIECES), [15, 5]);
  assert.deepEqual(centreOf(shape("area", [square(0, 0, 10), square(20, 0, 10)])), [15, 5]);
  assert.equal(centreOf(shape("area", [])), null);
});

test("containment asks every part, and every part's own holes", () => {
  assert.ok(shapeContains(TWO_PIECES, [5, 1]), "inside the first piece");
  assert.ok(shapeContains(TWO_PIECES, [25, 1]), "inside the second piece");
  assert.ok(!shapeContains(TWO_PIECES, [5, 5]), "inside the first piece's own hole");
  assert.ok(!shapeContains(TWO_PIECES, [25, 5]), "inside the second piece's own hole");
  assert.ok(!shapeContains(TWO_PIECES, [15, 5]), "between the two pieces");
  // One unit of grace: a pin dropped on a boundary meant the ground it is on.
  assert.ok(shapeContains(TWO_PIECES, [10.5, 5]), "half a unit outside a boundary");
  assert.ok(!shapeContains(TWO_PIECES, [12, 5]), "two units outside a boundary");
});

// ---- the fixtures' own features ----------------------------------------

/** The city world, built the way the chart builds it. */
function city(): WorldModel {
  const payload = payloads("bend-or").get("2026-08-02");
  assert.ok(payload, "the city fixture has a world payload");
  return new WorldModel("2026-08-02", payload, worldGrid(tileGrid("bend-or"), payload), null);
}

/**
 * How the payload itself spells a feature's parts, before anything projects
 * them: one entry per part, holding that part's rings by their vertex counts
 * — or, for a path, its single run's.
 */
function payloadParts(shape: ShapeRecord): number[][] {
  const rings = (part: unknown) => (part as unknown[]).map((ring) => (ring as unknown[]).length);
  const counts: number[][] = [];
  for (const geometry of shape.feature.geometry ?? []) {
    const coordinates = geometry.coordinates as unknown[];
    if (geometry.type === "MultiPolygon") {
      for (const polygon of coordinates) counts.push(rings(polygon));
    } else if (geometry.type === "Polygon") {
      counts.push(rings(coordinates));
    } else if (geometry.type === "MultiLineString") {
      for (const line of coordinates) counts.push([(line as unknown[]).length]);
    } else if (geometry.type === "LineString") {
      counts.push([coordinates.length]);
    }
  }
  return counts;
}

test("the city's zoning draws every piece of ground it owns", () => {
  const world = city();
  // `RS`: fifty-three separate pieces of ground, twenty-five holes among
  // them, and one piece carrying sixteen rings by itself.
  const zoning = world.shapeByID.get("247260267");
  assert.ok(zoning, "the city has its RS zoning");
  const geometry = shapeGeometry(zoning) as MultiPolygon;
  assert.equal(geometry.getType(), "MultiPolygon");
  const drawn = geometry.getPolygons().map(
    (polygon) => polygon.getLinearRings().map((ring) => ring.getCoordinates().length));
  assert.equal(drawn.length, 53, "fifty-three separate pieces of ground");
  assert.deepEqual(drawn, payloadParts(zoning), "every part, ring for ring, vertex for vertex");
  assert.equal(drawn.reduce((held, rings) => held + rings.length, 0), 78, "78 rings in all");
  // A flattening builder answers one polygon of seventy-eight rings here, of
  // which fifty-two are pieces of ground turned inside out.
  assert.notEqual(drawn.length, 1);
});

test("the city's longest trail draws all hundred and forty-seven of its runs", () => {
  const world = city();
  const trail = world.shapeByID.get("1071970836");
  assert.ok(trail, "the city has the Deschutes River Trail");
  assert.equal(trail.title, "Deschutes River Trail");
  const geometry = shapeGeometry(trail) as MultiLineString;
  assert.equal(geometry.getType(), "MultiLineString");
  const drawn = geometry.getLineStrings().map((line) => line.getCoordinates().length);
  assert.deepEqual(drawn, payloadParts(trail).map((counts) => counts[0]));
  assert.equal(drawn.length, 147, "not one run of it, and not one run short");
});

test("no shape in any volume, corpus or invented, loses a part", () => {
  // The corpus's two volumes, and the invented game plane beside them: mars
  // carries no shapes at all, so the multipart weight is the city's — its
  // zoning and trails alone are ninety-eight multipart features.
  const worlds: [string, WorldModel][] = [];
  for (const slug of volumes()) {
    for (const [name, payload] of payloads(slug)) {
      worlds.push(
        [`${slug}/${name}`, new WorldModel(name, payload, worldGrid(tileGrid(slug), payload), null)]);
    }
  }
  const city = gamePlane();
  for (const [name, payload] of city.worlds) {
    worlds.push(
      [`${city.slug}/${name}`, new WorldModel(name, payload, city.tileGrid, null)]);
  }
  let multipart = 0;
  for (const [label, world] of worlds) {
    for (const record of world.shapes) {
      const declared = payloadParts(record);
      if (declared.length > 1) multipart++;
      const where = `${label}: ${record.title} (${record.id})`;
      if (record.kind === "area") {
        const geometry = shapeGeometry(record) as MultiPolygon;
        assert.deepEqual(geometry.getPolygons().map(
          (polygon) => polygon.getLinearRings().map((ring) => ring.getCoordinates().length)),
        declared, where);
      } else {
        const geometry = shapeGeometry(record) as MultiLineString;
        assert.deepEqual(geometry.getLineStrings().map(
          (line) => [line.getCoordinates().length]), declared, where);
      }
    }
  }
  assert.ok(multipart > 90, `the volumes are multi-part enough to be a test (${multipart})`);
});
