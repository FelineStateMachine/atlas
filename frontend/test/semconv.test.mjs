// The label-policy ladder, pure and DOM-free: the reader's override
// outranks the collection's declared word, which outranks the zone's own,
// and silence all the way down means "always" -- the meaning every bundle
// from before the key already carried.
import assert from "node:assert/strict";
import test from "node:test";

import { labelPolicy } from "../src/semconv.js";
import { state } from "../src/state.js";

test("labelPolicy climbs override, collection, zone, default", () => {
  state.labelOverrides.clear();
  const quietZone = { attrs: { "atlas.label.policy": "quiet" } };

  assert.equal(labelPolicy({}), "always", "silence means always");
  assert.equal(labelPolicy({ attrs: {} }), "always");
  assert.equal(labelPolicy(quietZone), "quiet", "the zone's own word");

  const collection = { id: "watersheds", attrs: { "atlas.label.policy": "always" } };
  assert.equal(labelPolicy(quietZone, collection), "always",
    "the collection outranks the zone");

  state.labelOverrides.set("watersheds", "quiet");
  assert.equal(labelPolicy(quietZone, collection), "quiet",
    "the reader outranks the producer");
  state.labelOverrides.clear();
});

test("a v2 zone answers overrides through its implicit collection", () => {
  state.labelOverrides.clear();
  const zone = { attrs: {} };
  assert.equal(labelPolicy(zone), "always");
  state.labelOverrides.set("zones", "quiet");
  assert.equal(labelPolicy(zone), "quiet",
    "no collection on the wire, yet the override still lands");
  state.labelOverrides.clear();
});
