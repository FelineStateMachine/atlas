// What the two panes let go of when they leave the page — and take back up.
//
// A custom element's lifetime is not the page's. A morph swap replaces the
// viewport on every volume navigation, and everything the two panes hold that
// the browser does not hold *for* them survives that replacement unless the
// element says otherwise: a WebGL context and the animation frame that keeps
// asking for the next one, a `ResizeObserver` the browser owns and this lane
// merely started, a debounced camera report, and — worst of the four, because
// it is invisible — a listener closing over an element nobody can see, still
// drawing a planet out of a context nobody owns.
//
// THE OTHER HALF IS COMING BACK. A morph may put the *same* element back, so
// a teardown that left a memo standing is a teardown that breaks the next
// life: a lens key surviving its skin makes `openLens` skip compositing a
// texture that no longer exists, and the sphere comes up black. Every test
// below therefore disconnects and reconnects, and asks for one camera report
// per move afterwards rather than one per life the element has had.
//
// WHAT IS *NOT* GIVEN BACK is checked here too, and it is the subtler duty: a
// pin's material belongs to its collection and is shared with every pin
// wearing the same mark, its geometry is three's own quad shared with every
// sprite in the scene, and an element leaving the page is not a collection
// losing its mark.
//
// The panes are driven through their lifecycle callbacks over stubs — no
// WebGL, no OpenLayers target, no fixtures. Where a test reaches past
// `private` it is reaching for the subject itself: what these callbacks are
// *about* is the lifetime of fields no public method exposes.

import test from "node:test";
import { strict as assert } from "node:assert";
import * as THREE from "three";
import { setLevel } from "../log.ts";

setLevel("error");

// ---- the page the panes are drawn in ---------------------------------

/** A stub node: enough of one for `globe.gl` to load and for three to hold. */
function stubNode(): Record<string, unknown> {
  return {
    width: 0, height: 0, style: {}, classList: { add: () => {} },
    getContext: () => null, appendChild: () => {}, insertBefore: () => {},
    setAttribute: () => {}, addEventListener: () => {},
  };
}

/** Every observer this file has minted, live or not. */
const observers: StubObserver[] = [];

/** A `ResizeObserver` that says who it watched and whether it was stopped. */
class StubObserver {
  readonly targets: unknown[] = [];
  live = true;
  constructor() {
    observers.push(this);
  }

  observe(target: unknown): void {
    this.targets.push(target);
  }

  unobserve(): void {}

  disconnect(): void {
    this.live = false;
  }
}

const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class {};
host.window = globalThis;
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
host.Image = class { src = ""; crossOrigin = ""; };
host.ResizeObserver = StubObserver;
host.document = {
  createElement: stubNode, createElementNS: stubNode, createTextNode: stubNode,
  getElementsByTagName: () => [stubNode()], querySelector: () => null,
  querySelectorAll: () => [], addEventListener: () => {},
  head: stubNode(), body: stubNode(),
};

// Imported after the page exists: `globe.gl` reads the document as it loads.
const { AtlasGlobe, markerMaterial, nameCard } = await import("../globe/element.ts");
const { AtlasChart } = await import("../chart/element.ts");

// ---- what the panes hold, seen from outside ---------------------------

/**
 * The sphere's own fields, named so a test can put a globe where a build
 * would have left one. Lifetime is what is under test, and a lifetime is a
 * statement about fields.
 */
interface Inside {
  globe: unknown;
  texture: THREE.CanvasTexture | null;
  skin: unknown;
  pins: THREE.Group;
  labels: THREE.Group;
  cells: THREE.Group;
  lensKey: string;
  watchCamera(globe: unknown): void;
}

function inside(element: object): Inside {
  return element as unknown as Inside;
}

interface Charted {
  map: unknown;
  sizes: StubObserver | null;
  settle: number | undefined;
  report(): void;
}

function charted(element: object): Charted {
  return element as unknown as Charted;
}

/** The little of globe.gl the teardown speaks to, and a way to move it. */
function stubGlobe() {
  const handlers = new Set<() => void>();
  const scene = new THREE.Scene();
  let destructed = 0;
  const controls = {
    addEventListener: (_type: string, handler: () => void) => { handlers.add(handler); },
    removeEventListener: (_type: string, handler: () => void) => { handlers.delete(handler); },
  };
  return {
    scene: () => scene,
    controls: () => controls,
    camera: () => ({ position: { x: 0, y: 0, z: 300 } }),
    pointOfView: () => ({ lat: 12, lng: 34, altitude: 0.5 }),
    _destructor: () => { destructed += 1; },
    /** The test's own handles on the stub. */
    handlers,
    graph: scene,
    destructs: () => destructed,
    move: () => { for (const handler of [...handlers]) handler(); },
  };
}

/** The little of OpenLayers the chart's lifecycle speaks to. */
function stubMap() {
  const targets: unknown[] = [];
  return {
    targets,
    setTarget: (target: unknown) => { targets.push(target); },
    getSize: () => [800, 600],
    updateSize: () => {},
    render: () => {},
  };
}

/** Count what a teardown frees: three dispatches `dispose` on the way out. */
function watch(
  target: THREE.EventDispatcher<{ dispose: object }>, seen: string[], name: string,
): void {
  target.addEventListener("dispose", () => { seen.push(name); });
}

const somewhere = { x: 0, y: 0, z: 101 };

const mark = {
  asset: "", url: "", picture: false, color: "#4fb3d5",
  outset: "rgba(7, 9, 7, 0.98)", title: "Impact Craters",
};

/** A sphere with a globe under it, as a build would have left it. */
function built(): { element: InstanceType<typeof AtlasGlobe>; globe: ReturnType<typeof stubGlobe> } {
  const element = new AtlasGlobe();
  const globe = stubGlobe();
  const inner = inside(element);
  inner.globe = globe;
  inner.watchCamera(globe);
  globe.graph.add(inner.pins, inner.labels, inner.cells);
  return { element, globe };
}

// ---- the sphere -------------------------------------------------------

test("the camera's listener leaves with the element that was following it", () => {
  const { element, globe } = built();
  const seen: unknown[] = [];
  element.onCamera = (pov) => { seen.push(pov); };
  globe.move();
  assert.equal(seen.length, 1, "a move is one report");
  element.disconnectedCallback();
  assert.equal(globe.handlers.size, 0, "the handler came off the controls");
  assert.equal(element.onCamera, null, "and the corner is no longer being told");
  globe.move();
  assert.equal(seen.length, 1, "a detached sphere reports nothing");
});

test("a disconnected sphere gives back its renderer, its skin and its cards", () => {
  const { element, globe } = built();
  const inner = inside(element);
  const seen: string[] = [];

  const texture = new THREE.CanvasTexture(stubNode() as unknown as HTMLCanvasElement);
  inner.texture = texture;
  watch(texture, seen, "skin");
  inner.skin = {};

  for (const [group, title] of [[inner.labels, "Elysium"], [inner.cells, "Hellas"]] as const) {
    const card = nameCard(title, somewhere);
    watch((card.material as THREE.SpriteMaterial).map as THREE.Texture, seen, `${title}: texture`);
    watch(card.material as THREE.SpriteMaterial, seen, `${title}: material`);
    group.add(card);
  }

  element.disconnectedCallback();
  assert.equal(globe.destructs(), 1, "the renderer, the controls and the scene are disposed");
  assert.deepEqual(seen.sort(), [
    "Elysium: material", "Elysium: texture",
    "Hellas: material", "Hellas: texture",
    "skin",
  ], "and nothing the sphere minted is still held");
  assert.equal(inner.labels.children.length, 0, "the names are down");
  assert.equal(inner.cells.children.length, 0, "the grid with them");
  assert.equal(globe.graph.children.length, 0,
    "and the three groups came out of the scene before globe.gl emptied it");
  assert.equal(inner.texture, null);
  assert.equal(inner.skin, null);
});

test("a second disconnect is not a second teardown", () => {
  const { element, globe } = built();
  element.disconnectedCallback();
  element.disconnectedCallback();
  assert.equal(globe.destructs(), 1, "the renderer is disposed once, whatever the page does");
});

test("the pins' shared marks survive the element that wore them", () => {
  const { element } = built();
  const material = markerMaterial({ ...mark, title: "Dunes" }, false);
  const seen: string[] = [];
  watch(material, seen, "the collection's mark");
  const sprite = new THREE.Sprite(material);
  watch(sprite.geometry, seen, "three's shared sprite quad");
  inside(element).pins.add(sprite);
  element.seam.sprites.set("dunes", sprite);

  element.disconnectedCallback();
  assert.deepEqual(seen, [], "a sphere leaving is not a collection losing its mark");
  assert.equal(markerMaterial({ ...mark, title: "Dunes" }, false), material,
    "the cache is still good for the next sphere");
  assert.equal(inside(element).pins.children.length, 0, "the pins are down all the same");
  assert.equal(element.seam.sprites.size, 0, "and nothing is standing");
});

test("a sphere that comes back is built afresh rather than wired twice", () => {
  const { element, globe } = built();
  inside(element).lensKey = "mars/mars#viking-mdim";
  element.seam.detail.lens = "viking-mdim";
  element.seam.labels.key = "0:0:0.68:geohash:";
  element.disconnectedCallback();

  // What makes the next entry a build rather than a resumption.
  assert.equal(element.built, false, "nothing is built, so the next entry builds");
  assert.equal(inside(element).lensKey, "",
    "and the skin is composited again rather than skipped over a texture that is gone");
  assert.deepEqual(element.seam.labels, { key: "", group: null });
  assert.deepEqual(element.seam.grid, { group: null, cell: null, fitKey: "" });
  assert.equal(element.seam.detail.lens, "");
  assert.equal(element.seam.detail.tiles.size, 0);

  // The rebuild, and the one thing it must not do twice.
  const next = stubGlobe();
  inside(element).globe = next;
  inside(element).watchCamera(next);
  const seen: unknown[] = [];
  element.onCamera = (pov) => { seen.push(pov); };
  next.move();
  globe.move();
  assert.equal(seen.length, 1, "one report per move, from the sphere that is live");
});

// ---- the chart --------------------------------------------------------

test("the chart's size observer comes off with the element and goes back on", () => {
  observers.length = 0;
  const chart = new AtlasChart();
  const map = stubMap();
  charted(chart).map = map;

  chart.connectedCallback();
  assert.equal(observers.length, 1, "one observer");
  assert.deepEqual(observers[0]?.targets, [chart], "watching its own pane");
  assert.deepEqual(map.targets, [chart], "and the map is drawing into it again");

  chart.disconnectedCallback();
  assert.equal(observers[0]?.live, false, "the observer is stopped, not merely forgotten");
  assert.equal(charted(chart).sizes, null);
  assert.deepEqual(map.targets, [chart, undefined], "and the map has let the element go");

  chart.connectedCallback();
  assert.equal(observers.length, 2, "coming back wires a new one");
  assert.equal(observers.filter((seen) => seen.live).length, 1,
    "and there is never more than one live: a pane measured twice is a pane counted twice");
});

test("the first connect wires nothing, because there is no map to wire yet", () => {
  observers.length = 0;
  const chart = new AtlasChart();
  chart.connectedCallback();
  assert.deepEqual(observers, [],
    "the observer arrives with the world, from show(), not before there is one");
});

test("a chart that leaves the page does not report a camera afterwards", () => {
  const chart = new AtlasChart();
  charted(chart).report();
  assert.notEqual(charted(chart).settle, undefined, "a settle is pending");
  chart.disconnectedCallback();
  assert.equal(charted(chart).settle, undefined,
    "and it is cancelled: a camera reported after the pane went is a report about a world the reader has left");
});
