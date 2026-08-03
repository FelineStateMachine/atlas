// Reading the corpus, for the tests that judge the lane against real data.
//
// `testdata/corpus/bundles/<slug>/` is a committed extraction of one build:
// the manifest, each world's payload and unpacked locations, and a per-pyramid
// tile inventory with content and decoded-pixel digests. No rasters and no
// archives are in the repository, so everything here is JSON — which is
// exactly enough to hold a reader to the format.

import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import type { TileGrid, WorldPayload } from "../data/payload.ts";

const here = fileURLToPath(new URL(".", import.meta.url));

/** The repository's corpus root. */
export const FIXTURES = join(here, "..", "..", "testdata", "corpus", "bundles");

/** Every fixture volume, by slug. */
export function volumes(): string[] {
  return readdirSync(FIXTURES).filter((name) => !name.startsWith("."));
}

function read<T>(...path: string[]): T {
  return JSON.parse(readFileSync(join(FIXTURES, ...path), "utf8")) as T;
}

/** The manifest's tile grid: the world square every pyramid is cut over. */
export function tileGrid(slug: string): TileGrid {
  return read<{ tileGrid: TileGrid }>(slug, "manifest.json").tileGrid;
}

/** Every world payload of a volume, keyed by world slug. */
export function payloads(slug: string): Map<string, WorldPayload> {
  const out = new Map<string, WorldPayload>();
  for (const name of readdirSync(join(FIXTURES, slug, "worlds"))) {
    if (!name.endsWith(".payload.json")) continue;
    out.set(name.replace(".payload.json", ""), read<WorldPayload>(slug, "worlds", name));
  }
  return out;
}

/** One packed-locations extraction: the records, and the digest of the bytes. */
export interface LocationsFixture {
  readonly count: number;
  readonly packedBytes: number;
  readonly packedSha256: string;
  readonly world: string;
  readonly locations: readonly {
    id: number; owner: number; lat: number; lng: number;
    member: number; shard: number; title: string;
  }[];
}

export function locations(slug: string, world: string): LocationsFixture {
  return read<LocationsFixture>(slug, "worlds", `${world}.locations.json`);
}

/** One pyramid's tile inventory: names, in the order it records them. */
export interface Inventory {
  readonly pyramid: string;
  readonly count: number;
  readonly tiles: readonly { name: string }[];
}

export function inventories(slug: string): Inventory[] {
  const dir = join(FIXTURES, slug, "tiles");
  return readdirSync(dir)
    .filter((name) => name.endsWith(".json"))
    .map((name) => read<Inventory>(slug, "tiles", name));
}
