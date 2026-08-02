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

/** A stub node: enough of one for `globe.gl` to load over. */
function stubNode(): Record<string, unknown> {
  return {
    width: 0, height: 0, style: {}, classList: { add: () => {} },
    getContext: () => null, appendChild: () => {}, insertBefore: () => {},
    setAttribute: () => {}, addEventListener: () => {},
  };
}

const nodes = new Map<string, unknown>();

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
  querySelectorAll: () => [],
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
