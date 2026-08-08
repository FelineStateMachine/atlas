import { strict as assert } from "node:assert";
import test from "node:test";

import { DataPlane } from "../data/plane.ts";
import { demandWindowKey, replaceDemandWindow } from "../data/window.ts";

test("page zero is outline-only and features arrive through bounded pages", async () => {
  const requested: string[] = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (input: string | URL | Request) => {
    const url = String(input);
    requested.push(url);
    if (url.endsWith("/outline.json")) return Response.json(outline());
    if (url.includes("/features/roads.json")) {
      const continued = url.includes("after=more-is-demanded-later");
      return Response.json({
        features: continued ? [] : [feature()], next: continued ? "" : "more-is-demanded-later",
      });
    }
    return new Response(null, { status: 404 });
  }) as typeof fetch;
  try {
    const opened = await new DataPlane().world("/data/v/sample/abc123abc123", "sample-region");
    assert.equal(opened.world.featureSets[0]?.features.length, 1);
    assert.equal(opened.world.claims[0]?.value, 9223372036854775807n);
    assert.equal(opened.world.claims[1]?.value, -9223372036854775808n);
    assert.equal(opened.world.featureSets[0]?.features[0]?.properties[0]?.value, 9223372036854775807n);
    assert.equal(opened.world.featureSets[0]?.features[0]?.properties[1]?.value, -9223372036854775808n);
    assert.deepEqual(requested, [
      "/data/v/sample/abc123abc123/outline.json",
      "/data/v/sample/abc123abc123/features/roads.json?limit=1000&minX=0&minY=0&maxX=100&maxY=100",
      "/data/v/sample/abc123abc123/features/roads.json?limit=1000&minX=0&minY=0&maxX=100&maxY=100&after=more-is-demanded-later",
    ]);
    assert.ok(requested.every((url) => !url.endsWith("/data/features.pack")));
  } finally {
    globalThis.fetch = original;
  }
});

test("settled pans replace one world window and zoom changes keep distinct bounds", () => {
  const cache = new Map<string, number>();
  const prefix = "/data/v/sample/build/sample-region/";
  for (let pan = 0; pan < 100; pan++) {
    const key = `${prefix}${demandWindowKey({ x: pan, y: -pan, zoom: 3.1 + pan / 100, rotation: 0 })}`;
    replaceDemandWindow(cache, prefix, key, pan);
  }
  assert.equal(cache.size, 1);
  assert.notEqual(
    demandWindowKey({ x: 10, y: -10, zoom: 3.1, rotation: 0 }),
    demandWindowKey({ x: 10, y: -10, zoom: 3.9, rotation: 0 }),
  );
});

function outline(): Record<string, unknown> {
  return {
    version: 1, id: "sample", title: "Sample", assets: [],
    worlds: [{
      id: "sample-region", title: "Sample Region", claims: [{
        id: "33333333-3333-5333-8333-333333333333", name: "sample.maximum", kind: 2,
        value: "9223372036854775807",
      }, {
        id: "44444444-4444-5444-8444-444444444444", name: "sample.minimum", kind: 2,
        value: "-9223372036854775808",
      }],
      coordinateSpace: {
        id: "sample-space", kind: "projected", unit: "metre", definition: "sample",
        extent: [0, 0, 100, 100], sourceZoom: "0", originX: "0", originY: "0", tileSize: "256", size: "100",
      },
      featureSets: [{ id: "roads", title: "Roads", semanticType: "transport.road", properties: [], claims: [] }],
      rasterPyramids: [],
      presentation: { id: "default", title: "Default", styles: [], layers: [], legend: [] },
    }],
  };
}

function feature(): Record<string, unknown> {
  return {
    id: "road-1", title: "Sample Road", subtitle: "", description: "", center: null, shard: "0",
    geometry: { kind: 2, parts: [{ rings: [[[0, 0], [10, 10]]] }] },
    properties: [{
      id: "11111111-1111-5111-8111-111111111111", name: "sample.extreme", kind: 2,
      value: "9223372036854775807",
    }, {
      id: "22222222-2222-5222-8222-222222222222", name: "sample.minimum", kind: 2,
      value: "-9223372036854775808",
    }],
    relationships: [], provenance: [],
  };
}
