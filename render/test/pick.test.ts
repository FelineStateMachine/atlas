// What a pointer resolves to, and where the answer goes.
//
// Two surfaces can be pointed at and, once a grid is up, two different things
// can be under the pointer. The rules below are the reference implementation's
// and every one of them was a defect in this seam before it: the canvas
// resolved features only, so a grid could be drawn and never descended, and
// the sphere answered a press with the nearest pin however deep the reader was
// telescoping.
//
//   A CELL ANSWERS FIRST. While the grid is up a click is a request to go one
//   level in, and the pins standing on the cell are not consulted at all.
//   Reversed, the grid would be undescendable anywhere a marker happened to
//   be, which is everywhere worth descending to.
//   AND ONLY A NEIGHBOUR OR A CHILD IS AN ANSWER. The cell the reader is
//   already in lies over its own children -- as an outline, or as the whole
//   grid at the floor of the telescope -- so a plan that answered for `scope`
//   or `leaf` would hand back the address already held on every click.
//   THE TOLERANCES DIFFER, AND SAY DIFFERENT THINGS. A marker is a point and
//   is given four pixels of slack; a cell is a region the reader is pointing
//   into and is given one, because slack around a boundary is a boundary that
//   cannot be told from the cell beyond it.
//   AND THE TWO ANSWERS TAKE DIFFERENT ROADS. A feature is an identity the
//   session selects; a cell is an address the session holds. Two forms, two
//   routes, and a click that resolved to one must never post the other.
//
// The map here is a stub of OpenLayers' own hit-test contract -- walk the
// stack the layer filter allows, in z-order, stop at the first truthy answer
// -- so what is checked is the seam's use of it: which layers it asks, with
// what tolerance, in what order, and what it does with the answer.

import test from "node:test";
import { strict as assert } from "node:assert";
import { setLevel } from "../log.ts";

setLevel("error");

// ---- the page ---------------------------------------------------------

interface Field { value: string }

const fields = new Map<string, Field>();
const nodes = new Map<string, unknown>();
const events: CustomEvent[] = [];

/** A canvas-ish node, enough of one for `globe.gl` to load over. */
function stubElement(): unknown {
  return {
    width: 0, height: 0, style: {}, classList: { add: () => {} },
    getContext: () => ({ measureText: () => ({ width: 0 }), fillRect: () => {} }),
    appendChild: () => {}, insertBefore: () => {}, setAttribute: () => {},
    addEventListener: () => {},
  };
}

const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class { setAttribute(): void { /* the hover writes one */ } };
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
host.Image = class { src = ""; crossOrigin = ""; };
host.document = {
  querySelector: (selector: string) => nodes.get(selector) ?? null,
  createElement: stubElement,
  createElementNS: stubElement,
  createTextNode: stubElement,
  getElementsByTagName: () => [stubElement()],
  querySelectorAll: () => [],
  addEventListener: () => {},
  head: stubElement(),
  body: stubElement(),
};
// The window is the real global -- `globe.gl` reaches for its animation frames
// as it loads -- with the one method the reports use recording instead.
host.window = globalThis;
host.dispatchEvent = (event: Event) => {
  events.push(event as CustomEvent);
  return true;
};

/** The two hidden forms the shell renders, empty again. */
function page(): void {
  fields.clear();
  nodes.clear();
  events.length = 0;
  for (const id of ["#atlas-pick-feature", "#atlas-grid-pick-cell"]) {
    const field: Field = { value: "" };
    fields.set(id, field);
    nodes.set(id, field);
  }
  nodes.set("#atlas-pick", {});
  nodes.set("#atlas-grid-pick", {});
}

// Imported after the page exists: `globe.gl` reads the document as it loads.
const { AtlasChart } = await import("../chart/element.ts");
const { AtlasGlobe } = await import("../globe/element.ts");

// ---- the map the chart asks -------------------------------------------

/** One feature as a hit test sees it: a bag of properties on some layer. */
function feature(values: Record<string, unknown>): { get(key: string): unknown } {
  return { get: (key: string) => values[key] };
}

interface Probe {
  readonly tolerance: number | undefined;
  readonly layers: readonly unknown[];
}

interface Drawn {
  readonly feature: { get(key: string): unknown };
  readonly layer: unknown;
}

/**
 * OpenLayers' hit-test contract, in fifteen lines: the drawn stack in z-order
 * from the top, narrowed by the caller's layer filter, stopping at the first
 * truthy answer the caller's own callback gives.
 */
class StubMap {
  readonly probes: Probe[] = [];
  readonly handlers = new Map<string, (event: unknown) => void>();
  private readonly stack: readonly Drawn[];
  constructor(stack: readonly Drawn[]) { this.stack = stack; }

  on(name: string, handler: (event: unknown) => void): void {
    this.handlers.set(name, handler);
  }

  forEachFeatureAtPixel(
    _pixel: number[],
    callback: (feature: unknown, layer: unknown) => unknown,
    options?: { hitTolerance?: number; layerFilter?: (layer: unknown) => boolean },
  ): unknown {
    const asked = this.stack.filter((drawn) => options?.layerFilter?.(drawn.layer) ?? true);
    this.probes.push({
      tolerance: options?.hitTolerance,
      layers: [...new Set(asked.map((drawn) => drawn.layer))],
    });
    for (const drawn of asked) {
      const answer = callback(drawn.feature, drawn.layer);
      if (answer) return answer;
    }
    return undefined;
  }
}

const GRID = { name: "grid" };
const GRID_CONTEXT = { name: "gridContext" };
const PINS = { name: "pins" };
const ZONES = { name: "zones" };

/** A grid feature as `chart/grid.ts` mints one. */
function cell(hash: string, role: string): { get(key: string): unknown } {
  return feature({ gridCell: { hash, extent: [0, 0, 1, 1], role, count: 0, contextDistance: 0 } });
}

const PIN = feature({ record: { id: "1849", index: 0 } });
const ZONE = feature({ record: { id: "RS", kind: "area" } });

/** A world where one pin stands and one piece of ground is drawn. */
function context(gridOn: boolean): unknown {
  return {
    scene: { gridSystem: gridOn ? "geohash" : "", volume: "mars", world: "surface" },
    system: gridOn ? { slug: "geohash" } : null,
    cell: "",
    visibility: {
      at: () => ({ hidden: false, promoted: false, passesHighlights: false }),
      shapesShown: [ZONE.get("record")],
    },
  };
}

interface Seam { [key: string]: unknown }

/** A chart standing over a drawn stack, with nothing else wired. */
function chartOver(stack: readonly Drawn[], gridOn = true): { seam: Seam; map: StubMap } {
  page();
  const chart = new AtlasChart();
  const seam = chart as unknown as Seam;
  const map = new StubMap(stack);
  seam.map = map;
  seam.context = context(gridOn);
  seam.gridLayers = [GRID, GRID_CONTEXT];
  return { seam, map };
}

function hit(seam: Seam): unknown {
  return (seam.hit as (pixel: number[]) => unknown).call(seam, [10, 10]);
}

/** The stack a descendable grid draws: a child under the pins, a neighbour over. */
const DESCENDABLE: Drawn[] = [
  { feature: cell("9qb", "neighbor"), layer: GRID_CONTEXT },
  { feature: PIN, layer: PINS },
  { feature: cell("9qa", "child"), layer: GRID },
  { feature: ZONE, layer: ZONES },
];

// ---- the order --------------------------------------------------------

test("a cell answers before the pin standing on it", () => {
  const { seam } = chartOver(DESCENDABLE);
  assert.deepEqual(hit(seam), { kind: "cell", cell: "9qb" });
});

test("a cell under the pins answers too, because depth is not the question", () => {
  const { seam } = chartOver([
    { feature: PIN, layer: PINS },
    { feature: cell("9qa", "child"), layer: GRID },
  ]);
  assert.deepEqual(hit(seam), { kind: "cell", cell: "9qa" });
});

test("with no cell under the pointer the pin answers, and beats the ground", () => {
  const { seam } = chartOver([
    { feature: PIN, layer: PINS },
    { feature: ZONE, layer: ZONES },
  ]);
  assert.deepEqual(hit(seam), { kind: "point", id: "1849" });
});

test("with neither, the ground answers", () => {
  const { seam } = chartOver([{ feature: ZONE, layer: ZONES }]);
  assert.deepEqual(hit(seam), { kind: "area", id: "RS" });
});

test("a click on nothing at all is a miss", () => {
  const { seam } = chartOver([]);
  assert.equal(hit(seam), null);
});

// ---- which cells are answers ------------------------------------------

test("the cell the reader is already in is not an answer, and the pin under it is", () => {
  for (const role of ["scope", "leaf"]) {
    const { seam } = chartOver([
      { feature: cell("9q", role), layer: GRID },
      { feature: PIN, layer: PINS },
    ]);
    assert.deepEqual(hit(seam), { kind: "point", id: "1849" },
      `a ${role} cell answered, so the grid could not be descended`);
  }
});

test("a scope over its own child hands back the child", () => {
  const { seam } = chartOver([
    { feature: cell("9q", "scope"), layer: GRID },
    { feature: cell("9qa", "child"), layer: GRID },
  ]);
  assert.deepEqual(hit(seam), { kind: "cell", cell: "9qa" });
});

test("with the grid off no cell is looked for at all", () => {
  const { seam, map } = chartOver(DESCENDABLE, false);
  assert.deepEqual(hit(seam), { kind: "point", id: "1849" });
  assert.equal(map.probes.length, 1, "the grid was asked about while it was off");
});

// ---- what is asked, and how closely ------------------------------------

test("the cell is asked of the grid's own layers, at one pixel", () => {
  const { seam, map } = chartOver(DESCENDABLE);
  hit(seam);
  const cells = map.probes[0];
  assert.equal(cells?.tolerance, 1);
  assert.deepEqual(cells?.layers, [GRID_CONTEXT, GRID],
    "the cell hit test was offered a layer that is not the grid's");
});

test("a feature is asked of everything, at four", () => {
  const { seam, map } = chartOver([{ feature: ZONE, layer: ZONES }]);
  hit(seam);
  const features = map.probes[1];
  assert.equal(features?.tolerance, 4);
  assert.deepEqual(features?.layers, [ZONES]);
});

test("a hidden pin is not a hit, and the ground under it is", () => {
  const { seam } = chartOver([
    { feature: PIN, layer: PINS },
    { feature: ZONE, layer: ZONES },
  ]);
  (seam.context as { visibility: { at: () => unknown } }).visibility.at =
    () => ({ hidden: true, promoted: false, passesHighlights: false });
  assert.deepEqual(hit(seam), { kind: "area", id: "RS" });
});

// ---- where the answer goes ---------------------------------------------

function click(seam: Seam, map: StubMap): void {
  (seam.wire as () => void).call(seam);
  map.handlers.get("singleclick")?.({ pixel: [10, 10] });
}

test("a cell posts the address, and never the selection", () => {
  const { seam, map } = chartOver(DESCENDABLE);
  click(seam, map);
  assert.equal(fields.get("#atlas-grid-pick-cell")?.value, "9qb");
  assert.equal(fields.get("#atlas-pick-feature")?.value, "",
    "descending a grid selected a feature as well");
  assert.deepEqual(events.map((event) => event.type), ["atlas:grid-pick"]);
});

test("a feature posts the selection, and never an address", () => {
  const { seam, map } = chartOver([{ feature: PIN, layer: PINS }]);
  click(seam, map);
  assert.equal(fields.get("#atlas-pick-feature")?.value, "1849");
  assert.equal(fields.get("#atlas-grid-pick-cell")?.value, "",
    "picking a feature moved the grid as well");
  assert.deepEqual(events.map((event) => event.type), ["atlas:pick"]);
});

test("a click on nothing posts neither", () => {
  const { seam, map } = chartOver([]);
  click(seam, map);
  assert.equal(events.length, 0);
});

test("the hover is about features and never about cells", () => {
  const { seam, map } = chartOver(DESCENDABLE);
  (seam.wire as () => void).call(seam);
  map.handlers.get("pointermove")?.({ pixel: [10, 10], dragging: false });
  // The cell over the pin does not stop the pin lifting under the pointer,
  // and no cell was ever asked for: a cell has no hover reading.
  assert.equal((seam.context as { hovered: string | null }).hovered, "1849");
  assert.equal(map.probes.length, 1);
  assert.equal(map.probes[0]?.tolerance, 4);
});

// ---- the sphere ---------------------------------------------------------

/** A flattening that is its own inverse to the nearest degree. */
const MAPPING = {
  toWorld: (lat: number, lng: number) => [lng * 2, lat * 2] as const,
  toLatLng: (x: number, y: number) => [y / 2, x / 2] as const,
};

interface Descent {
  readonly held: string;
  readonly at: readonly [number, number];
}

/** A globe over a world a system divides, recording what it was asked. */
function globeOver(options: {
  gridOn: boolean;
  held?: string;
  answer?: string;
  pins?: { id: string; coordinate: [number, number] }[];
}): { seam: Seam; asked: Descent[] } {
  page();
  const asked: Descent[] = [];
  const globe = new AtlasGlobe();
  const seam = globe as unknown as Seam;
  seam.equirect = { mapping: MAPPING, px: [0, 0, 8192, 4096] };
  seam.context = {
    scene: { gridSystem: options.gridOn ? "geohash" : "" },
    system: options.gridOn
      ? {
        slug: "geohash",
        on: () => ({
          descendTarget: (held: string, at: readonly [number, number]) => {
            asked.push({ held, at });
            return options.answer ?? "";
          },
        }),
      }
      : null,
    ground: {},
    cell: options.held ?? "",
    visibility: { standing: () => options.pins ?? [] },
  };
  return { seam, asked };
}

function press(seam: Seam, lat: number, lng: number): void {
  (seam.pick as (lat: number, lng: number) => void).call(seam, lat, lng);
}

test("a press on the sphere descends one level below the cell being held", () => {
  const { seam, asked } = globeOver({ gridOn: true, held: "9q", answer: "9qb" });
  press(seam, 10, 20);
  // The point pressed, flattened back onto the picture: x east, y negative
  // down, which is the space every coordinate in this lane lives in.
  assert.deepEqual(asked, [{ held: "9q", at: [40, -20] }]);
  assert.equal(fields.get("#atlas-grid-pick-cell")?.value, "9qb");
  assert.deepEqual(events.map((event) => event.type), ["atlas:grid-pick"]);
});

test("a press at the floor of the telescope goes nowhere", () => {
  const { seam, asked } = globeOver({ gridOn: true, held: "9qbxu4", answer: "" });
  press(seam, 10, 20);
  assert.equal(asked.length, 1, "the system was not asked");
  assert.equal(events.length, 0, "an empty address was posted as if it were the root");
});

test("a press while a grid is up never picks the pin under it", () => {
  const { seam } = globeOver({
    gridOn: true, held: "9q", answer: "9qb",
    pins: [{ id: "1849", coordinate: [40, -20] }],
  });
  press(seam, 10, 20);
  assert.equal(fields.get("#atlas-pick-feature")?.value, "");
});

test("with no grid up a press is a pick, as it has always been", () => {
  const { seam, asked } = globeOver({
    gridOn: false, pins: [{ id: "1849", coordinate: [40, -20] }],
  });
  press(seam, 10, 20);
  assert.equal(asked.length, 0);
  assert.equal(fields.get("#atlas-pick-feature")?.value, "1849");
  assert.deepEqual(events.map((event) => event.type), ["atlas:pick"]);
});
