// The label-policy ladder, pure and DOM-free: the reader's override
// outranks the collection's declared word, which outranks the kind's own
// default -- areas speak unasked, paths wait -- and a zone answers through
// the collection the wire stamped it with.
import assert from "node:assert/strict";
import test from "node:test";

import { featureAttributeRows, labelPolicy, labelSilenced, renderAs } from "../src/semconv.js";
import { state } from "../src/state.js";

test("labelPolicy climbs override, collection, kind default", () => {
  state.labelOverrides.clear();
  const districts = { id: 41, kind: "area", attrs: {} };
  const hydro = { id: 42, kind: "area", attrs: { "atlas.label.policy": "quiet" } };
  const creeks = { id: 43, kind: "path", attrs: { "atlas.stroke.width_px": "10" } };

  assert.equal(labelPolicy(null, districts), "always", "silence means always for areas");
  assert.equal(labelPolicy(null, hydro), "quiet", "the collection's curated word");
  assert.equal(labelPolicy(null, creeks), "quiet", "paths wait by default");

  state.labelOverrides.set(42, "always");
  assert.equal(labelPolicy(null, hydro), "always", "the reader outranks the producer");
  state.labelOverrides.clear();
});

test("a point collection's labels wait unless its curation asked for text", () => {
  state.labelOverrides.clear();
  const garages = { id: 51, kind: "point", attrs: { "atlas.render.as": "pin" } };
  const cities = { id: 52, kind: "point", attrs: { "atlas.render.as": "text" } };

  assert.equal(labelPolicy(null, garages), "quiet", "markers wait for Z");
  assert.equal(labelPolicy(null, cities), "always", "curated text means names speak");

  state.labelOverrides.set(52, "quiet");
  assert.equal(labelPolicy(null, cities), "quiet", "the reader can quiet the names");
  assert.ok(labelSilenced(cities), "and that silence is theirs, so Z respects it");
  assert.ok(!labelSilenced(garages), "curated quiet is merely optional, so Z reveals it");
  state.labelOverrides.clear();
});

test("a zone answers through the collection the wire stamped it with", () => {
  state.labelOverrides.clear();
  const hydro = { id: 42, kind: "area", attrs: { "atlas.label.policy": "quiet" } };
  state.world = { collectionsById: new Map([[42, hydro]]) };
  const zone = { id: 7, collectionId: 42 };
  assert.equal(labelPolicy(zone), "quiet", "the collection speaks for its zone");
  state.labelOverrides.set(42, "always");
  assert.equal(labelPolicy(zone), "always", "the override lands by collection id");
  state.labelOverrides.clear();
  state.world = null;
});

test("renderAs reads the declared attribute alone", () => {
  assert.equal(renderAs({ attrs: { "atlas.render.as": "text" } }), "text");
  assert.equal(renderAs({ attrs: {} }), "pin", "silence means markers");
  assert.equal(renderAs({}), "pin", "no attrs at all still means markers");
});

test("featureAttributeRows curates, labels, and skips machinery", () => {
  const rows = featureAttributeRows({
    "atlas.hydro.huc12": "170703010101",
    "atlas.geo.lat": "44.05",
    "atlas.geo.lon": "-121.3",
    "atlas.stroke.width_px": "12",
    "atlas.label.policy": "quiet",
  });
  assert.deepEqual(rows, [{ label: "HUC-12", value: "170703010101" }],
    "only material survives: geo has rows of its own, rendering is machinery");
  assert.deepEqual(featureAttributeRows(undefined), [], "no attrs, no rows");
});
