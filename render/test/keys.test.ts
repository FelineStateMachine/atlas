// The seam's keyboard, checked against the page it listens to.
//
// `render/keys.ts` is the second module in the lane that touches the document
// and nothing else -- it reads the application's controls, moves focus and
// which pane is up, and asks the viewport for the rest -- so the page here is
// a stub of exactly the ids shell.tmpl renders, and the assertions are about
// what a keystroke reaches and, at least as often, what it does not.
//
// THE RULES THESE TESTS EXIST FOR ARE THE SUPPRESSIONS. A shortcut that fires
// is visible the first time anybody presses it. A shortcut that fires while
// the reader is typing their own "g" into a search box, or one that leaves a
// keystroke unanswered so the machine sounds a rejection tone at every press,
// is the kind of thing that survives a whole release -- so the guard, the
// preventDefault, the repeat and the one load-bearing stopPropagation each
// get a test of their own.

import test from "node:test";
import { strict as assert } from "node:assert";
import { wireKeyboard } from "../keys.ts";
import type { KeyboardHost } from "../keys.ts";

/** A stand-in for an element, enough of one to be a keystroke's target. */
class StubElement {
  hidden = false;
  isContentEditable = false;
  focused = 0;
  selected = 0;
  readonly tagName: string;
  constructor(tagName: string) { this.tagName = tagName; }
  focus(): void { this.focused++; }
  select(): void { this.selected++; }
  contains(other: unknown): boolean { return other === this; }
}

interface Listener {
  handler: (event: unknown) => void;
  capture: boolean;
}

/** What the host was asked to do, and what the page was asked for. */
interface Harness {
  readonly nodes: Map<string, StubElement>;
  readonly labels: boolean[];
  readonly flips: number[];
  readonly zooms: number[];
  sphereUp: boolean;
  fire(name: string, event: Record<string, unknown>): Prevented;
  off(): void;
}

interface Prevented {
  prevented: boolean;
  stopped: boolean;
}

/**
 * A page carrying the shell's named controls and a window that records what
 * was wired to it. The AbortController is real: honouring the signal is how
 * the "wired twice, heard twice" bug is tested rather than assumed.
 */
function mount(ids: string[] = ["#map", "#grid-input", "#pin-search", "#globe-toggle"]): Harness {
  const nodes = new Map<string, StubElement>();
  for (const id of ids) {
    nodes.set(id, new StubElement(id === "#map" ? "DIV" : id === "#globe-toggle" ? "BUTTON" : "INPUT"));
  }
  const listeners = new Map<string, Listener[]>();
  globalThis.Element = StubElement as unknown as typeof Element;
  globalThis.document = {
    querySelector: (selector: string) => nodes.get(selector) ?? null,
  } as unknown as Document;
  globalThis.window = {
    addEventListener: (name: string, handler: (event: unknown) => void,
      options?: { capture?: boolean; signal?: AbortSignal }) => {
      const held = listeners.get(name) ?? [];
      const entry: Listener = { handler, capture: options?.capture === true };
      held.push(entry);
      listeners.set(name, held);
      options?.signal?.addEventListener("abort", () => {
        listeners.set(name, (listeners.get(name) ?? []).filter((one) => one !== entry));
      });
    },
  } as unknown as Window & typeof globalThis;

  const labels: boolean[] = [];
  const flips: number[] = [];
  const zooms: number[] = [];
  const harness: Harness = {
    nodes, labels, flips, zooms, sphereUp: false,
    fire(name, event) {
      const answer: Prevented = { prevented: false, stopped: false };
      const raised = {
        target: null, repeat: false, metaKey: false, ctrlKey: false, altKey: false,
        ...event,
        preventDefault: () => { answer.prevented = true; },
        stopPropagation: () => { answer.stopped = true; },
      };
      for (const listener of listeners.get(name) ?? []) listener.handler(raised);
      return answer;
    },
    off: () => { /* replaced below */ },
  };
  const host: KeyboardHost = {
    holdLabels: (down) => { labels.push(down); },
    flipPane: () => { flips.push(1); },
    zoomBy: (delta) => { zooms.push(delta); },
    get sphereUp() { return harness.sphereUp; },
  };
  harness.off = wireKeyboard(host);
  return harness;
}

/** The window itself, which is what a synthetic key from the tour is aimed at. */
const AT_WINDOW = { target: null };

test("Z is held: down raises the labels, up puts them back", () => {
  const page = mount();
  const down = page.fire("keydown", { key: "z", ...AT_WINDOW });
  assert.deepEqual(page.labels, [true]);
  assert.equal(down.prevented, true, "Z was not swallowed, so the machine complains about it");
  const up = page.fire("keyup", { key: "z", ...AT_WINDOW });
  assert.deepEqual(page.labels, [true, false]);
  assert.equal(up.prevented, false, "the key coming up was swallowed too");
});

test("a repeat says what the first press already said, and is still swallowed", () => {
  const page = mount();
  page.fire("keydown", { key: "z", ...AT_WINDOW });
  const again = page.fire("keydown", { key: "z", repeat: true, ...AT_WINDOW });
  assert.deepEqual(page.labels, [true], "an autorepeating Z raised the labels twice");
  assert.equal(again.prevented, true, "a repeat was left unanswered");
});

test("a window that loses focus mid-hold lets the labels go", () => {
  const page = mount();
  page.fire("keydown", { key: "z", ...AT_WINDOW });
  page.fire("blur", {});
  assert.deepEqual(page.labels, [true, false]);
});

test("the key coming up is heard from anywhere, even a field", () => {
  const page = mount();
  page.fire("keydown", { key: "Z", ...AT_WINDOW });
  page.fire("keyup", { key: "z", target: page.nodes.get("#pin-search") });
  assert.deepEqual(page.labels, [true, false],
    "a hold that ended over a text field left the labels up for good");
});

test("a reader typing into a field triggers nothing and is not swallowed", () => {
  const page = mount();
  for (const id of ["#pin-search", "#grid-input"]) {
    const pressed = page.fire("keydown", { key: "z", target: page.nodes.get(id) });
    assert.equal(pressed.prevented, false, `${id}: a typed character was swallowed`);
  }
  assert.deepEqual(page.labels, [], "typing raised the labels");
});

test("a select counts as editable, because it answers the space bar itself", () => {
  const page = mount();
  const select = new StubElement("SELECT");
  page.fire("keydown", { key: "z", target: select });
  assert.deepEqual(page.labels, []);
});

test("a contenteditable counts too", () => {
  const page = mount();
  const note = new StubElement("DIV");
  note.isContentEditable = true;
  page.fire("keydown", { key: "z", target: note });
  assert.deepEqual(page.labels, []);
});

test("⌘K reaches the search field and offers what is in it", () => {
  const page = mount();
  const pressed = page.fire("keydown", { key: "k", metaKey: true, ...AT_WINDOW });
  const search = page.nodes.get("#pin-search");
  assert.equal(search?.focused, 1);
  assert.equal(search?.selected, 1, "the field was focused without offering its text");
  assert.equal(pressed.prevented, true);
  assert.deepEqual(page.labels, [], "the ladder ran on past the branch that answered");
});

test("the backquote flips the panes only where a sphere is offered", () => {
  const page = mount();
  const toggle = page.nodes.get("#globe-toggle");
  assert.ok(toggle);
  toggle.hidden = true;
  const flat = page.fire("keydown", { key: "`", ...AT_WINDOW });
  assert.deepEqual(page.flips, [], "a flat map flipped to a sphere it does not have");
  assert.equal(flat.prevented, false, "a key nothing answered was swallowed anyway");

  toggle.hidden = false;
  const round = page.fire("keydown", { key: "`", ...AT_WINDOW });
  assert.deepEqual(page.flips, [1]);
  assert.equal(round.prevented, true);
});

test("Escape in the grid field hands the keyboard back and stops there", () => {
  const page = mount();
  const pressed = page.fire("keydown", { key: "Escape", target: page.nodes.get("#grid-input") });
  assert.equal(page.nodes.get("#map")?.focused, 1, "the map was not given the keyboard back");
  assert.equal(pressed.prevented, true);
  // The load-bearing half: without this the same press would also telescope
  // the grid out, and the field would never get an Escape of its own.
  assert.equal(pressed.stopped, true,
    "the field's Escape travelled on, so one press left the field and ascended");
});

test("Escape anywhere else is left alone, because a route answers it", () => {
  const page = mount();
  const onMap = page.fire("keydown", { key: "Escape", target: page.nodes.get("#map") });
  assert.equal(onMap.stopped, false, "the map's Escape was stopped before the window heard it");
  const onWindow = page.fire("keydown", { key: "Escape", ...AT_WINDOW });
  assert.equal(onWindow.stopped, false);
  assert.equal(onWindow.prevented, false);
});

test("the backquote reaches the field too, and is swallowed there either way", () => {
  const page = mount();
  const toggle = page.nodes.get("#globe-toggle");
  assert.ok(toggle);
  toggle.hidden = true;
  const flat = page.fire("keydown", { key: "`", target: page.nodes.get("#grid-input") });
  assert.equal(flat.prevented, true, "a backquote was typed into a field that takes a hash");
  assert.deepEqual(page.flips, []);

  toggle.hidden = false;
  page.fire("keydown", { key: "`", target: page.nodes.get("#grid-input") });
  assert.deepEqual(page.flips, [1]);
});

test("the map's own keys step the zoom, and only on the map", () => {
  const page = mount();
  for (const key of ["+", "=", "-"]) {
    page.fire("keydown", { key, target: page.nodes.get("#map") });
  }
  assert.deepEqual(page.zooms, [1, 1, -1]);
  page.fire("keydown", { key: "+", ...AT_WINDOW });
  assert.deepEqual(page.zooms, [1, 1, -1], "the zoom keys answered with the map unfocused");
});

test("the zoom keys are inert behind the sphere and still swallowed", () => {
  const page = mount();
  page.sphereUp = true;
  const pressed = page.fire("keydown", { key: "+", target: page.nodes.get("#map") });
  assert.deepEqual(page.zooms, [], "a key press zoomed the chart nobody is looking at");
  assert.equal(pressed.prevented, true);
});

test("the webview's menu is put away, except over text", () => {
  const page = mount();
  assert.equal(page.fire("contextmenu", AT_WINDOW).prevented, true);
  assert.equal(page.fire("contextmenu", { target: page.nodes.get("#map") }).prevented, true);
  assert.equal(page.fire("contextmenu", { target: page.nodes.get("#pin-search") }).prevented, false,
    "a text field lost the menu that carries cut and paste");
});

test("the keyboard comes off, and comes off whole", () => {
  const page = mount();
  page.off();
  page.fire("keydown", { key: "z", ...AT_WINDOW });
  page.fire("keyup", { key: "z", ...AT_WINDOW });
  page.fire("blur", {});
  assert.deepEqual(page.labels, [], "a viewport that left the page kept listening");
  assert.equal(page.fire("contextmenu", AT_WINDOW).prevented, false);
});

test("a page missing the controls loses the shortcut and nothing else", () => {
  const page = mount([]);
  assert.doesNotThrow(() => page.fire("keydown", { key: "k", metaKey: true, ...AT_WINDOW }));
  assert.doesNotThrow(() => page.fire("keydown", { key: "`", ...AT_WINDOW }));
  assert.doesNotThrow(() => page.fire("keydown", { key: "+", ...AT_WINDOW }));
  // Z asks nothing of the page, so it still answers.
  page.fire("keydown", { key: "z", ...AT_WINDOW });
  assert.deepEqual(page.labels, [true]);
});
