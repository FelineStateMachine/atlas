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
import { LineMaterial } from "three/examples/jsm/lines/LineMaterial.js";
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
  private readonly told: () => void;

  constructor(told: () => void) {
    this.told = told;
    observers.push(this);
  }

  /** The browser saying the box moved, which is the only thing it ever says. */
  fire(): void {
    this.told();
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
/**
 * The one thing a pane's teardown asks of its own element: that it can be
 * emptied. globe.gl's destructor gives back the renderer and leaves its canvas
 * exactly where it put it, holding the last frame it drew, so the container is
 * emptied by hand -- and the count is what a test reads that by.
 */
host.HTMLElement = class {
  emptied = 0;

  replaceChildren(): void {
    this.emptied += 1;
  }
};
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
  const dimensions: [string, number][] = [];
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
    width: (px: number) => { dimensions.push(["width", px]); },
    height: (px: number) => { dimensions.push(["height", px]); },
    /** The test's own handles on the stub. */
    dimensions,
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

// ---- the sphere, and the box it is drawn in ---------------------------
//
// The pane moves without the window doing anything: the dock folds under a
// keystroke and the panel beside the map comes out the first time a search has
// something to say. globe.gl is told its dimensions once, at entry, so a
// sphere that did not hear about the fold went on drawing at the old size
// until something else poked it -- which is the defect. The chart has watched
// its own box since the lifecycle work; this is the same duty on the sphere,
// and it carries two more units that remember a size: a `Line2` carries its
// width in pixels and can only do it by being told the window it is measured
// against, and the horizon cull is a question about the camera's aspect.

/** A pane with a box, which is what a stub element otherwise has no idea of. */
function sized(element: object, width: number, height: number): void {
  Object.assign(element, { clientWidth: width, clientHeight: height });
}

/** A grid boundary as `cellBoundary` leaves one: an object wearing a fat line. */
function boundary(width: number, height: number): { material: LineMaterial } {
  const material = new LineMaterial({ linewidth: 2 });
  material.resolution.set(width, height);
  const line = new THREE.Object3D() as THREE.Object3D & { material: LineMaterial };
  line.material = material;
  return line;
}

test("a sphere whose pane changed size is told, in every unit that remembers one", () => {
  observers.length = 0;
  const { element, globe } = built();
  sized(element, 900, 600);
  // A re-connect is what puts the observer back; the first one is the build's,
  // and there is no globe to measure before there is a globe.
  element.connectedCallback();
  assert.equal(observers.length, 1, "one observer");
  assert.deepEqual(observers[0]?.targets, [element], "watching its own pane");

  const line = boundary(900, 600);
  inside(element).cells.add(line as unknown as THREE.Object3D);
  const card = nameCard("Elysium", { x: 0, y: 0, z: -101 });
  card.visible = true;
  inside(element).cells.add(card);

  sized(element, 1280, 720);
  observers[0]?.fire();
  assert.deepEqual(globe.dimensions, [["width", 1280], ["height", 720]],
    "globe.gl measures once and has to be told");
  assert.deepEqual([line.material.resolution.x, line.material.resolution.y], [1280, 720],
    "a fat line not told the new window draws at the old scale");
  assert.equal(card.visible, false,
    "and the horizon is asked again: a new aspect is a new answer");
});

test("a sphere put away behind the chart is not told it is zero pixels wide", () => {
  observers.length = 0;
  const { element, globe } = built();
  sized(element, 0, 0);
  element.connectedCallback();
  observers[0]?.fire();
  assert.deepEqual(globe.dimensions, [],
    "a pane measured mid-transition, or hidden, has no size to draw a planet at");
});

test("the sphere's size observer comes off with the element and goes back on", () => {
  observers.length = 0;
  const { element } = built();
  sized(element, 900, 600);
  element.connectedCallback();
  assert.equal(observers.length, 1);

  element.disconnectedCallback();
  assert.equal(observers[0]?.live, false, "the observer is stopped, not merely forgotten");

  // A disconnect took the globe with it, so the element that comes back has
  // nothing to measure until it is entered again -- exactly the chart's rule.
  element.connectedCallback();
  assert.equal(observers.length, 1, "there is no planet yet to resize");

  const next = built();
  sized(next.element, 900, 600);
  next.element.connectedCallback();
  assert.equal(observers.filter((seen) => seen.live).length, 1,
    "and there is never more than one live: a pane measured twice is a pane counted twice");
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

// ---- and the sphere a world change withdraws --------------------------
//
// A disconnect is the element leaving the page. This is the page staying and
// the *world* leaving: the reader opens a volume whose world is a game map,
// and a sphere is no longer a thing this page offers. The reference destroys
// the instance, forgets what it was textured for and empties the container
// (`syncGlobe`, frontend/src/globe.js); leaving it standing is what put Night
// City's map under a planet still wearing Mars's skin.

/** What a retirement is measured by, past the fields a disconnect clears. */
interface Retired extends Inside {
  context: unknown;
  equirect: unknown;
  emptied: number;
  hidden: boolean;
}

function retired(element: object): Retired {
  return element as unknown as Retired;
}

test("a sphere the world no longer offers is put down, not merely hidden", () => {
  const { element, globe } = built();
  retired(element).context = { model: { slug: "mars" } };
  retired(element).equirect = { px: [0, 0, 3600, 1800] };
  const texture = new THREE.CanvasTexture(stubNode() as unknown as HTMLCanvasElement);
  inside(element).texture = texture;
  const seen: string[] = [];
  watch(texture, seen, "skin");

  element.retire();
  assert.equal(element.hidden, true, "the pane is off screen");
  assert.equal(globe.destructs(), 1, "the renderer is given back");
  assert.deepEqual(seen, ["skin"], "and the skin with it");
  assert.equal(retired(element).emptied, 1,
    "the container is emptied: globe.gl's destructor leaves its canvas holding the last frame");
  assert.equal(element.built, false);
  // The difference from a disconnect, and the whole of the stale-skin defect:
  // this element is not about that world any more. A disconnect keeps the
  // context, because the same world is what the next entry builds from.
  assert.equal(retired(element).context, null, "the world it was showing is forgotten");
  assert.equal(retired(element).equirect, null, "and the flattening that world declared");
});

test("retiring a sphere that was never built costs nothing", () => {
  const element = new AtlasGlobe();
  element.retire();
  element.retire();
  assert.equal(retired(element).emptied, 0, "there was no canvas to take away");
  assert.equal(element.hidden, true);
});
