// The two reports that leave the seam, checked against the page they write to.
//
// `render/data/report.ts` is the one module in the lane that touches the
// document and never touches the network, and both halves of that are worth
// pinning: it fills fields the application rendered and raises an event the
// application listens for, and it does nothing else at all. So the page here
// is a stub of exactly what `internal/app/templates/shell.tmpl` renders -- two
// hidden forms and their fields -- and the assertions are about what lands in
// them and what is dispatched afterwards.
//
// The rule the pick tests exist for is the miss. A click on open water is not
// a request to close the card the reader is reading; the card is put away by
// Escape and by its own button, and a report that posted an empty identity
// would take it away on every stray click.

import test from "node:test";
import { strict as assert } from "node:assert";
import { reportCamera, reportPick } from "../data/report.ts";

interface Field {
  value: string;
}

/** What the page offers the report, and what it heard back. */
interface Page {
  readonly fields: Map<string, Field>;
  readonly events: CustomEvent[];
}

/**
 * A page carrying the named nodes and nothing else. `mount([])` is the host
 * that rendered no such form: the report is lost, and nothing throws.
 */
function mount(ids: string[]): Page {
  const fields = new Map<string, Field>();
  const nodes = new Map<string, unknown>();
  for (const id of ids) {
    const field: Field = { value: "" };
    fields.set(id, field);
    nodes.set(id, field);
  }
  // The forms themselves are only ever looked up, never written.
  if (ids.some((id) => id.startsWith("#atlas-pick"))) nodes.set("#atlas-pick", {});
  if (ids.some((id) => id.startsWith("#atlas-camera"))) nodes.set("#atlas-camera", {});
  const events: CustomEvent[] = [];
  globalThis.document = {
    querySelector: (selector: string) => nodes.get(selector) ?? null,
  } as unknown as Document;
  globalThis.window = {
    dispatchEvent: (event: Event) => {
      events.push(event as CustomEvent);
      return true;
    },
  } as unknown as Window & typeof globalThis;
  return { fields, events };
}

test("a pick fills the field the page rendered and says so", () => {
  const page = mount(["#atlas-pick-feature"]);
  reportPick({ feature: "1849", kind: "area" });
  assert.equal(page.fields.get("#atlas-pick-feature")?.value, "1849");
  assert.equal(page.events.length, 1);
  const raised = page.events[0];
  assert.equal(raised?.type, "atlas:pick");
  // It is heard on the window, where the form listens, so it does not travel.
  assert.equal(raised?.bubbles, false);
});

test("the kind travels in the event and posts no field", () => {
  const page = mount(["#atlas-pick-feature"]);
  reportPick({ feature: "1849", kind: "point" });
  const detail = page.events[0]?.detail as { feature: string; kind: string };
  assert.deepEqual(detail, { feature: "1849", kind: "point" });
  // One field posts, which is the identity. The session looks the rest up.
  assert.equal(page.fields.size, 1);
});

test("a miss posts nothing and leaves the open card alone", () => {
  const page = mount(["#atlas-pick-feature"]);
  const field = page.fields.get("#atlas-pick-feature");
  assert.ok(field);
  field.value = "1849";
  reportPick({ feature: "", kind: "" });
  assert.equal(field.value, "1849", "a miss overwrote the selection that was posted");
  assert.equal(page.events.length, 0, "a miss posted a request");
});

test("a page that renders no pick form loses the report and nothing else", () => {
  const page = mount([]);
  assert.doesNotThrow(() => reportPick({ feature: "1849", kind: "point" }));
  assert.equal(page.events.length, 0);
});

test("the camera fills all five fields and says so", () => {
  const page = mount([
    "#atlas-camera-world", "#atlas-camera-x", "#atlas-camera-y",
    "#atlas-camera-zoom", "#atlas-camera-rotation",
  ]);
  reportCamera({
    volume: "tunic", world: "overworld", x: 120.5, y: -40, zoom: 6.25, rotation: 0.5,
  });
  assert.equal(page.fields.get("#atlas-camera-world")?.value, "overworld");
  assert.equal(page.fields.get("#atlas-camera-zoom")?.value, "6.25");
  assert.equal(page.fields.get("#atlas-camera-rotation")?.value, "0.5");
  assert.equal(page.events[0]?.type, "atlas:camera");
});

test("a page that renders no camera form loses the report and nothing else", () => {
  const page = mount([]);
  assert.doesNotThrow(() => reportCamera({
    volume: "tunic", world: "overworld", x: 0, y: 0, zoom: 1, rotation: 0,
  }));
  assert.equal(page.events.length, 0);
});
