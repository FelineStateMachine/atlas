// The seam's edge, where a bad answer must not become a broken page.
//
// Three duties live on the host, and each of them is a place the lane can be
// handed something it did not choose.
//
//   THE SYSTEM IS THE GROUND'S ANSWER, NOT THE SESSION'S. A record can name a
//   system this world cannot be divided by -- an older build wrote it, a hand
//   edited it, a bug let it through -- and the systems are exact rather than
//   approximate: S2 asked about a ground with no invertible flattening throws
//   outright. The host used to fall back to the registry, which is the one
//   answer that cannot be given, and the throw arrived in the middle of a
//   repaint.
//   SO A REPAINT IS FENCED. Whatever gets past the first rule costs one
//   repaint and one line on the stream, and never the map: a reader whose grid
//   is wrong can still read their world.
//   AND THE NAVIGATOR'S FIELD IS ASSISTED, NOT GOVERNED. What an address is
//   belongs to the server; this end shows the reader only what their system
//   could keep of what they typed, keeps the caret where they left it, and
//   gets out of the way of the route bound to the same field -- which is what
//   the recorded tours drive, one `.value` and one `input` event at a time.

import test from "node:test";
import { strict as assert } from "node:assert";
import { cellSystems } from "@atlas/analysis";
import type { Ground } from "@atlas/analysis";
import { setLevel } from "../log.ts";

setLevel("error");

/**
 * A stub node: enough of one for `globe.gl` to load over, and enough of one
 * for the card's definition list to be walked, inserted into and cleared.
 *
 * The list a node sits in is the node's own `row`, because the only structure
 * anything here reads is order: what a row was inserted after, and which rows
 * are still standing.
 */
class StubEl {
  readonly width = 0;
  readonly height = 0;
  readonly style = {};
  readonly classList = { add: () => {} };
  readonly attrs: Record<string, string> = {};
  readonly kids: StubEl[] = [];
  hidden = false;
  textContent = "";
  /** The list this node stands in, if it stands in one. */
  row: StubEl[] | null = null;

  getContext(): null { return null; }
  appendChild(): void {}
  insertBefore(): void {}
  addEventListener(): void {}

  setAttribute(name: string, value: string): void {
    this.attrs[name] = value;
  }

  append(...kids: StubEl[]): void {
    this.kids.push(...kids);
  }

  after(node: StubEl): void {
    if (!this.row) return;
    this.row.splice(this.row.indexOf(this) + 1, 0, node);
    node.row = this.row;
  }

  remove(): void {
    if (!this.row) return;
    this.row.splice(this.row.indexOf(this), 1);
    this.row = null;
  }
}

function stubNode(): Record<string, unknown> {
  return new StubEl() as unknown as Record<string, unknown>;
}

const nodes = new Map<string, unknown>();
/** The open card's definition list, in the order it is rendered. */
let list: StubEl[] = [];

const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class {
  /** The panes are looked up rather than held; this page renders neither. */
  querySelector(): unknown { return null; }
};
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
host.Image = class { src = ""; crossOrigin = ""; };
host.window = globalThis;
host.document = {
  querySelector: (selector: string) => nodes.get(selector) ?? null,
  querySelectorAll: (selector: string) => (selector === "[data-cell-system-row]"
    ? list.filter((node) => node.attrs["data-cell-system-row"] !== undefined)
    : []),
  createElement: stubNode,
  createElementNS: stubNode,
  createTextNode: stubNode,
  getElementsByTagName: () => [stubNode()],
  addEventListener: () => {},
  head: stubNode(),
  body: stubNode(),
};

const { AtlasViewport } = await import("../viewport.ts");

interface Seam { [key: string]: unknown }

function viewport(): Seam {
  nodes.clear();
  list = [];
  return new AtlasViewport() as unknown as Seam;
}

/** A game map: it says nothing about what its picture is of. */
const PLANE: Ground = { tileGridSize: 8192, lens: null, world: { attrs: {} } };

// ---- which system divides this world ---------------------------------

function chosen(seam: Seam, slug: string): unknown {
  return (seam.system as (scene: unknown, ground: Ground) => unknown)
    .call(seam, { gridSystem: slug, volume: "bend-or", world: "city" }, PLANE);
}

test("the system a plane offers is the one it gets", () => {
  const seam = viewport();
  assert.equal(chosen(seam, "geohash"), cellSystems.get("geohash"));
});

test("a system this ground refuses is not fallen back to", () => {
  const seam = viewport();
  // S2 is registered, and registered is not the question: it wants a sphere
  // with an invertible flattening and this is a game map. The old fallback
  // handed it over anyway, and it threw on the first cell it was asked about.
  assert.ok(cellSystems.get("s2"), "S2 is registered, which is what makes this a fallback");
  assert.equal(chosen(seam, "s2"), null);
});

test("a system nobody has written is not a system either", () => {
  assert.equal(chosen(viewport(), "h3"), null);
});

test("no system named is no grid, and no complaint", () => {
  assert.equal(chosen(viewport(), ""), null);
});

// ---- a repaint that cannot kill the map -------------------------------

test("a system that throws mid-repaint costs one line and not the page", () => {
  const seam = viewport();
  const thrown = new Error("s2: this ground declares no invertible flattening");
  seam.context = {
    scene: { volume: "bend-or", world: "city" },
    ground: PLANE,
    cell: "9q",
    lens: null,
    model: {},
    hovered: null,
    system: { slug: "s2", on: () => { throw thrown; } },
  };
  const said: unknown[][] = [];
  // The stream is *captured* rather than written to -- what the lane says
  // when a system refuses is the subject here, and the sink is where it says
  // it (render/log.ts). Reached through the global so the rule that forbids
  // writing to it bare has nothing to object to.
  const stream = globalThis.console;
  const held = stream.error;
  stream.error = (...args: unknown[]) => { said.push(args); };
  try {
    assert.doesNotThrow(() => (seam.refresh as () => void).call(seam));
  } finally {
    stream.error = held;
  }
  assert.equal(said.length, 1, "the failure was silent, or said twice");
  const fields = said[0]?.[1] as Record<string, unknown>;
  assert.equal(fields.system, "s2");
  assert.equal(fields.cell, "9q");
  assert.equal(fields.volume, "bend-or");
  assert.match(String(fields.error), /invertible flattening/);
});

// ---- the navigator's field --------------------------------------------

/** A text field, with the two things a caret is read and written through. */
function field(value: string, caret = value.length): Record<string, unknown> {
  const ranges: number[][] = [];
  return {
    value,
    selectionStart: caret,
    ranges,
    setSelectionRange: (from: number, to: number) => { ranges.push([from, to]); },
    addEventListener: () => {},
  };
}

/** A viewport standing over a world geohash divides. */
function dividing(): Seam {
  const seam = viewport();
  seam.context = { system: cellSystems.get("geohash"), ground: PLANE };
  return seam;
}

function normalize(seam: Seam, node: Record<string, unknown>): void {
  (seam.normalizeCell as (field: unknown) => void).call(seam, node);
}

test("what the system cannot keep is not left in the field", () => {
  const node = field("M6");
  normalize(dividing(), node);
  assert.equal(node.value, "m6");
});

test("the caret lands after what survived of what stood before it", () => {
  // "m!6" with the caret between the "!" and the "6": one character of what
  // was typed before it survives, so the caret belongs after one character.
  const node = field("m!6", 2);
  normalize(dividing(), node);
  assert.equal(node.value, "m6");
  assert.deepEqual(node.ranges, [[1, 1]]);
});

test("typing at the end stays at the end", () => {
  const node = field("m!6", 3);
  normalize(dividing(), node);
  assert.deepEqual(node.ranges, [[2, 2]]);
});

test("a keystroke that was already an address is not touched at all", () => {
  // THE TOUR'S OWN PATH. `type("#grid-input", "m")` sets the value and
  // dispatches one `input`; geohash keeps "m" whole, so the field, the caret
  // and the event the route is bound to are left exactly as they were found.
  const node = field("m");
  normalize(dividing(), node);
  assert.equal(node.value, "m");
  assert.deepEqual(node.ranges, [], "an untouched field had its caret moved");
});

test("with no system dividing the world the field is nobody's to rewrite", () => {
  const seam = viewport();
  seam.context = { system: null, ground: PLANE };
  const node = field("M6");
  normalize(seam, node);
  assert.equal(node.value, "M6");
});

// ---- the open card's cell addresses ------------------------------------
//
// Half of that card is the application's and half is this lane's, exactly as
// the footer's "N of M" sentence is: where a point stands is in the payload
// and the server writes it, and what that place is *called* only the analysis
// lane can say. So the row is rendered present, empty and hidden, and this
// fills it after every swap -- there is nothing to keep in step, because the
// card is an outerMorph region and the swap takes the last answer away with
// the node it was written on.
//
// The address is the fixed-depth one and not the grid's: the row says where
// this point is, and a reader descending the navigator did not ask for every
// open card to be re-spelled behind them.

/** A sphere with an invertible flattening: the ground S2 will divide. */
const SPHERE: Ground = {
  tileGridSize: 8192,
  lens: { surface: { x: 0, y: 0, width: 8192, height: 4096 }, bounds: null },
  world: {
    attrs: {
      "atlas.geometry.surface": "sphere",
      "atlas.geometry.projection": "equirect",
      "atlas.geometry.equirect.px": "0,0,8192,4096",
      "atlas.geometry.equirect.deg": "-180,90,180,-90",
    },
  },
};

/** The point every card below is about, and where it stands. */
const AT: [number, number] = [3000, -1500];

/** The card as the server renders it: the row present, empty and hidden. */
function card(): { field: StubEl; value: StubEl } {
  const field = new StubEl();
  field.hidden = true;
  field.row = list;
  list.push(field);
  const value = new StubEl();
  nodes.set("#detail-cell-field", field);
  nodes.set("#detail-cell", value);
  return { field, value };
}

/** A viewport standing over a world, with something selected on it. */
function selecting(ground: Ground, selected: string): Seam {
  const seam = viewport();
  seam.context = {
    ground,
    scene: { selected, volume: "bend-or", world: "city" },
    model: { pointByID: new Map([["1849", { id: "1849", coordinate: AT }]]) },
  };
  return seam;
}

function written(seam: Seam): void {
  (seam.writeCell as () => void).call(seam);
}

/** The rows this lane inserted, as the term and value a reader sees. */
function rows(): [string, string, string][] {
  return list
    .filter((node) => node.attrs["data-cell-system-row"] !== undefined)
    .map((node) => [
      node.attrs["data-cell-system-row"] ?? "",
      node.kids[0]?.textContent ?? "",
      node.kids[1]?.textContent ?? "",
    ]);
}

test("a point's card is given the fixed-depth address, and shown", () => {
  const seam = selecting(SPHERE, "1849");
  const { field, value } = card();
  written(seam);
  // Three characters, which is the exported helper's own depth. The grid's
  // depth is somewhere else entirely and moves under the reader's feet: at
  // depth 1 this point is "d" and at 2 it is "du".
  assert.equal(value.textContent, "dum");
  assert.equal(field.hidden, false, "a row with an address in it is a row to show");
});

test("every other system this ground offers names the place too", () => {
  const seam = selecting(SPHERE, "1849");
  card();
  written(seam);
  assert.deepEqual(rows(), [["s2", "S2", "8b3b07"]],
    "the second system's row is missing, or is not the system's own answer");
});

test("a ground S2 refuses is never asked", () => {
  // A game map says nothing about what its picture is of, so S2 does not apply
  // -- and S2 asked anyway throws rather than shrugging. `applicableSystems` is
  // the asking, and the card is geohash's alone.
  const seam = selecting(PLANE, "1849");
  const { field, value } = card();
  written(seam);
  assert.equal(value.textContent, "ffj", "the same point, on the ground it is actually on");
  assert.equal(field.hidden, false);
  assert.deepEqual(rows(), []);
});

test("a card that is now about a shape keeps nothing of the point before it", () => {
  const seam = selecting(SPHERE, "1849");
  const { field, value } = card();
  written(seam);
  assert.equal(rows().length, 1, "a row to be left behind");

  // The swap that opened the shape's card rendered no cell field, and the
  // rows this lane inserted are not the server's to take away.
  (seam.context as { scene: { selected: string } }).scene.selected = "1469115845";
  written(seam);
  assert.deepEqual(rows(), [], "the shape's card is wearing the point's address");
  assert.equal(field.hidden, true);
  assert.equal(value.textContent, "");
});

test("the swap that just landed decides, not the reconcile still in flight", () => {
  // A rescan runs the moment the card is swapped in; the context it is read
  // against still names the selection *before* the swap, because reconciling
  // is a payload away. The address written is the open card's.
  const seam = selecting(SPHERE, "");
  seam.watcher = { scene: { selected: "1849" } };
  const { field, value } = card();
  written(seam);
  assert.equal(value.textContent, "dum");
  assert.equal(field.hidden, false);
});

test("with no card on the page there is nothing to write and nothing to clear", () => {
  const seam = selecting(SPHERE, "1849");
  assert.doesNotThrow(() => written(seam));
  assert.doesNotThrow(() => written(viewport()));
});

// ---- wiring the field --------------------------------------------------

test("the field is wired once, in the capture phase, and never stops the event", () => {
  const seam = viewport();
  const wirings: { capture: boolean }[] = [];
  const node = {
    value: "",
    addEventListener: (name: string, _handler: unknown, options?: { capture?: boolean }) => {
      assert.equal(name, "input");
      wirings.push({ capture: options?.capture === true });
    },
  };
  nodes.set("#grid-input", node);
  const wire = seam.wireGridInput as () => void;
  wire.call(seam);
  wire.call(seam);
  assert.deepEqual(wirings, [{ capture: true }],
    "the field was wired twice, so one keystroke would be normalized twice");
});

test("a page with no navigator on it wires nothing and says nothing", () => {
  const seam = viewport();
  assert.doesNotThrow(() => (seam.wireGridInput as () => void).call(seam));
});

// ---- the pane a world change leaves standing ---------------------------
//
// WHICH PANE IS UP IS SEAM STATE, and that is right until the world changes.
// A filter must not drop the reader back to the chart, so the flag survives
// every swap -- and a volume change is the one swap where surviving is wrong.
// The reader opened Night City while the Mars globe was up: the server did its
// half (the toggle is rendered `hidden` for a world that declares no sphere,
// `topbar.tmpl`), and the sphere went on turning in front of it, still wearing
// the skin of the world they had left.
//
// The scene says which surface the world declares, so the pane is put right in
// the tick the scene moved rather than a payload later.

/** The two panes, as much of them as a pane flip touches. */
function panes() {
  const chart = {
    hidden: true, located: [] as unknown[], counted: 0,
    locate(where: unknown) { this.located.push(where); },
    show() {}, restyle() {}, writeCount() { this.counted += 1; }, redrawOverview() {},
  };
  const globe = { retired: 0, retire() { this.retired += 1; }, show() {} };
  return { chart, globe };
}

/** A viewport with the sphere up over a world, and a toggle saying so. */
function standing(): {
  seam: Seam;
  chart: ReturnType<typeof panes>["chart"];
  globe: ReturnType<typeof panes>["globe"];
  toggle: { pressed: string };
} {
  const seam = viewport();
  const { chart, globe } = panes();
  seam.querySelector = (selector: string) => (selector === "atlas-chart" ? chart : globe);
  seam.globeUp = true;
  // No volume in the catalog: the scene is read and the pane decided before a
  // payload is ever asked for, which is the point -- a sphere over the wrong
  // world must come down even if nothing else about the new one ever arrives.
  seam.catalog = { volumes: [] };
  const toggle = {
    pressed: "true",
    setAttribute: (name: string, value: string) => {
      if (name === "aria-pressed") toggle.pressed = value;
    },
  };
  nodes.set("#globe-toggle", toggle);
  return { seam, chart, globe, toggle };
}

function scene(surface: "plane" | "sphere"): Record<string, unknown> {
  return { volume: "cyberpunk-2077", base: "/data/cyberpunk", world: "night-city", surface };
}

const swapped = { volume: true, world: true, lens: true, filters: false,
  selection: false, grid: false, camera: false, any: true };

function applied(seam: Seam, surface: "plane" | "sphere"): Promise<void> {
  return (seam.apply as (scene: unknown, change: unknown) => Promise<void>)
    .call(seam, scene(surface), swapped);
}

test("a world that declares no sphere takes the sphere down with it", async () => {
  const { seam, chart, globe, toggle } = standing();
  await applied(seam, "plane");
  assert.equal(seam.globeUp, false, "the pane flag follows the world");
  assert.equal(chart.hidden, false, "the chart is back on screen");
  assert.deepEqual(chart.located, [null], "and reading its own camera again rather than being told");
  assert.equal(globe.retired, 1, "the planet is given back, not merely hidden");
  assert.equal(toggle.pressed, "false", "and the control says which pane is up");
});

test("a world that does declare one is left exactly as it was found", async () => {
  const { seam, chart, globe, toggle } = standing();
  await applied(seam, "sphere");
  assert.equal(seam.globeUp, true, "the reader is still on the sphere");
  assert.equal(chart.hidden, true);
  assert.equal(globe.retired, 0);
  assert.equal(toggle.pressed, "true");
});

test("putting the sphere down twice is putting it down once", async () => {
  // Every scene change over a plane comes through here -- a filter, a
  // selection, a grid -- so the second pass must be a pass over nothing. The
  // sphere is asked again and answers for free (`AtlasGlobe.retire` returns on
  // an element that is already about no world); what must not happen twice is
  // anything the reader would see.
  const { seam, chart } = standing();
  await applied(seam, "plane");
  await applied(seam, "plane");
  assert.equal(seam.globeUp, false);
  assert.equal(chart.hidden, false);
  assert.deepEqual(chart.located, [null], "the chart was put back once, not once per scene");
});
