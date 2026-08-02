// The legend's pointer, and the mark two carried stylesheet rules read.
//
// `.only-button` and `.label-toggle` are drawn at `opacity: 0` and revealed
// only under `.category-row.is-hovered` / `.layer-header.is-hovered`. Nothing
// wrote that class, so both controls were on the page, wired to their routes,
// and permanently invisible -- a defect no unit test could have caught,
// because every Go test and every recorded step reads the buttons' attributes
// and all of them were right.
//
// So what is checked here is the mark itself, over a page stubbed down to the
// three things the tracker actually asks of one: a container under a selector,
// a hit test that answers with an element, and rows that can wear a class.
// The hit test is a stub *by necessity* -- `elementFromPoint` is a layout
// question and there is no layout in Node -- which is exactly why the manual
// check with a browser is part of this fix and not optional.
//
// The cases are the four ways the answer can be wrong:
//
//   THE ROW UNDER THE POINTER, and only that one.
//   THE POINTER LEAVING, which must take the mark with it -- a row left
//   offering an action to a cursor somewhere else is the bug the reference
//   wrote this function to avoid.
//   THE LIST SCROLLING UNDER A STILL CURSOR, which is the same bug arriving
//   the other way round and the reason the answer comes from the pointer's
//   position rather than from `:hover`.
//   AND THE SWAP. The legend is a morph region: the container can be replaced
//   whole, and it can be handed back intact carrying marks from before. One
//   pass has to survive both, and it must not leave a second listener behind
//   on a container it has already wired.

import test from "node:test";
import { strict as assert } from "node:assert";
import { RowHover } from "../hover.ts";

/** A row of the legend: a class list, and a place in a container. */
class StubRow {
  readonly classes = new Set<string>();
  readonly matches: string;
  constructor(matches: string) {
    this.matches = matches;
  }

  readonly classList = {
    add: (name: string) => { this.classes.add(name); },
    remove: (name: string) => { this.classes.delete(name); },
  };

  /** The row is what a hit test lands on; a child would answer the same. */
  closest(selector: string): StubRow | null {
    return selector.includes(this.matches) ? this : null;
  }

  get marked(): boolean {
    return this.classes.has("is-hovered");
  }
}

interface Listener {
  handler: (event: unknown) => void;
}

/** The `#layers` container: the rows it holds, and what was wired to it. */
class StubLayers {
  readonly listeners = new Map<string, Listener[]>();
  readonly rows: StubRow[];
  constructor(rows: StubRow[]) {
    this.rows = rows;
  }

  addEventListener(name: string, handler: (event: unknown) => void,
    options?: { signal?: AbortSignal }): void {
    const held = this.listeners.get(name) ?? [];
    const entry: Listener = { handler };
    held.push(entry);
    this.listeners.set(name, held);
    // The signal is honoured because it is the whole of the teardown: a
    // tracker that stopped and left its listeners on the page would keep
    // marking rows for an element nobody is looking at.
    options?.signal?.addEventListener("abort", () => {
      this.listeners.set(name, (this.listeners.get(name) ?? []).filter((one) => one !== entry));
    });
  }

  querySelectorAll(selector: string): StubRow[] {
    const wanted = selector.replace(".", "");
    return this.rows.filter((row) => row.classes.has(wanted));
  }

  contains(other: unknown): boolean {
    return this.rows.includes(other as StubRow);
  }

  fire(name: string, event: Record<string, unknown> = {}): void {
    for (const listener of [...this.listeners.get(name) ?? []]) listener.handler(event);
  }

  /** How many handlers are listening, which is how double-wiring is caught. */
  get wiring(): number {
    return [...this.listeners.values()].reduce((total, held) => total + held.length, 0);
  }
}

/** The page: one container under `#layers`, and a hit test we decide. */
function mount(layers: StubLayers | null) {
  const page = { layers, under: null as StubRow | null };
  globalThis.document = {
    querySelector: (selector: string) => (selector === "#layers" ? page.layers : null),
    elementFromPoint: () => page.under,
  } as unknown as Document;
  return page;
}

function row(kind: "category-row" | "layer-header" = "category-row"): StubRow {
  return new StubRow(kind);
}

test("the row under the pointer wears the mark, and no other", () => {
  const [first, second, header] = [row(), row(), row("layer-header")];
  const layers = new StubLayers([first, second, header]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();

  page.under = second;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(second.marked, true);
  assert.equal(first.marked, false);

  // Moving on takes the mark with it: every pass clears before it writes.
  page.under = header;
  layers.fire("pointermove", { clientX: 10, clientY: 4 });
  assert.equal(header.marked, true, "a section head marks like a row");
  assert.equal(second.marked, false, "the mark was left behind on the old row");
  hover.stop();
});

test("a hit outside the list marks nothing", () => {
  const inside = row();
  const outside = row();
  const layers = new StubLayers([inside]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();

  // A card or a menu drawn over the legend answers the hit test with an
  // element of its own, and the listener's scope does not make that element
  // one of this list's rows.
  page.under = outside;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(outside.marked, false);
  assert.equal(inside.marked, false);
  hover.stop();
});

test("the pointer leaving takes the mark with it", () => {
  const only = row();
  const layers = new StubLayers([only]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();

  page.under = only;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(only.marked, true);

  layers.fire("pointerleave");
  assert.equal(only.marked, false, "the list kept offering an action to a pointer that had gone");
  hover.stop();
});

test("scrolling under a still cursor re-asks where the pointer is", () => {
  const above = row();
  const below = row();
  const layers = new StubLayers([above, below]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();

  page.under = above;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(above.marked, true);

  // THE POINTER DID NOT MOVE. The list did, which is the case `:hover` is
  // unreliable for and the reason this is tracked rather than styled.
  page.under = below;
  layers.fire("scroll");
  assert.equal(below.marked, true);
  assert.equal(above.marked, false);
  hover.stop();
});

test("a swap that replaces the list is wired again, once", () => {
  const first = row();
  const layers = new StubLayers([first]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();
  const wiring = layers.wiring;
  assert.ok(wiring > 0, "the list was never wired at all");

  // The same container, handed back by a morph. It is already listening, and
  // a rescan that wired it again would answer one pointer move twice.
  hover.rescan();
  assert.equal(layers.wiring, wiring, "a morphed-in container was wired twice");

  // A stale mark, which is what a morph leaves when it carries a row across
  // whole: the pointer is elsewhere and the row still says it is under it.
  first.classes.add("is-hovered");
  hover.rescan();
  assert.equal(first.marked, false, "a mark survived the swap that invalidated it");

  // And now the other kind of swap: a different node under the same selector.
  const fresh = row();
  const replaced = new StubLayers([fresh]);
  page.layers = replaced;
  hover.rescan();
  assert.equal(replaced.wiring, wiring, "the replacement list was left unwired");
  page.under = fresh;
  replaced.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(fresh.marked, true);
  hover.stop();
});

test("a page with no legend is not an error", () => {
  mount(null);
  const hover = new RowHover();
  hover.start();
  hover.rescan();
  hover.stop();
});

test("a tracker that stopped marks nothing again", () => {
  const only = row();
  const layers = new StubLayers([only]);
  const page = mount(layers);
  const hover = new RowHover();
  hover.start();
  page.under = only;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(only.marked, true);

  hover.stop();
  assert.equal(layers.wiring, 0, "the listeners outlived the element that made them");

  // The element comes back -- a morph can put the same one back -- and mints a
  // new tracker rather than reviving the spent one. One life, one wiring: the
  // mark still lands exactly once.
  const revived = new RowHover();
  revived.start();
  page.under = only;
  layers.fire("pointermove", { clientX: 10, clientY: 40 });
  assert.equal(only.marked, true);
  assert.equal(layers.wiring, 3, "a reconnected element stacked a second set of listeners");
  revived.stop();
});
