// The `/data` plane, and the only place in the seam that reaches the network.
//
// One module owns fetching so the plane's three properties have one owner
// (issue #5 §9, and the ESLint rule that says so by name):
//
//   THE URL SHAPES. `/data/catalog.json` and `/data/v/{slug}/{stamp12}/…`,
//   and nothing else. A base carries the twelve-hex short stamp, so every URL
//   under it names exactly one build.
//
//   THE CACHE RULES. Everything under a base is immutable for a year, so a
//   payload is fetched once per build and kept for as long as the page holds
//   the build. The catalog is `no-store` and is never kept.
//
//   THE 404 AS A SIGNAL. When a newer build takes a slug over, every URL
//   under the old stamp is gone. A 404 from a base is not a broken link: it
//   is the build having moved, and the page's cue to ask the server for a new
//   scene. The seam says so on the stream and gives up on that base; it does
//   not reload the page, because navigation is the application's to decide.
//
// The one interpretation here is structural: schema-described typed columns
// become the semantic Volume graph. Presentation decisions remain the world's.

import { logger } from "../log.ts";
import type { Catalog, Lens } from "./payload.ts";
import { tileFormat, tileTemplate } from "./payload.ts";
import {
  decodeVolume, TABLE_NAMES,
  type NativeTables, type Volume, type World,
} from "./semantic.ts";
import { PackedTable, type Schema } from "./vnext.ts";

const log = logger("data");

/** A build's URL prefix: `/data/v/<slug>/<stamp12>`. */
export type Base = string;

export interface OpenWorld {
  readonly volume: Volume;
  readonly world: World;
}

/** Raised when a base answered 404 — the build moved out from under us. */
export class BuildMovedError extends Error {
  readonly url: string;

  constructor(url: string) {
    super(`the build serving ${url} has moved; the scene is stale`);
    this.name = "BuildMovedError";
    this.url = url;
  }
}

/**
 * One page's reading of the plane.
 *
 * Native volumes are kept per base, which is per build, because the URL names the
 * build. Two worlds of one volume share nothing but the base; two builds of
 * one volume share nothing at all, which is the whole cache story.
 */
export class DataPlane {
  private readonly payloads = new Map<string, Promise<unknown>>();

  /** The catalog, composed at the moment it is asked for. Never kept. */
  async catalog(): Promise<Catalog> {
    const response = await fetch("/data/catalog.json", { cache: "no-store" });
    if (!response.ok) throw new Error(`the catalog answered ${response.status}`);
    return (await response.json()) as Catalog;
  }

  /** One semantic world, reconstructed from the schema-described tables. */
  async world(base: Base, world: string): Promise<OpenWorld> {
    const volume = await this.volume(base);
    const found = volume.worlds.find((candidate) => candidate.id === world);
    if (!found) throw new Error(`${base} does not contain world ${world}`);
    return { volume, world: found };
  }

  /** Where an icon asset lives. Icons are fetched by the browser, as images. */
  iconURL(base: Base, asset: string): string {
    return iconURL(base, asset);
  }

  /** Where one tile lives, or null when the lens holds no such level. */
  tileURL(base: Base, lens: Lens, z: number, x: number, y: number): string | null {
    const extension = tileFormat(lens, z);
    if (!extension) return null;
    return `${base}/${fillTemplate(tileTemplate(lens), z, x, y, extension)}`;
  }

  private volume(base: Base): Promise<Volume> {
    return this.keep(`${base}/@semantic-volume`, async () => {
      const schemaURL = `${base}/schema.json`;
      const schemaBytes = new Uint8Array(await this.bytes(schemaURL));
      const schema = JSON.parse(new TextDecoder().decode(schemaBytes)) as Schema;
      const schemaHash = await sha256(schemaBytes);
      const entries = await Promise.all(TABLE_NAMES.map(async (name) => {
        const url = `${base}/data/${name}.pack`;
        const packed = PackedTable.over(await this.bytes(url));
        if (packed.schemaHash !== schemaHash) throw new Error(`${url} was encoded against another schema`);
        return [name, packed] as const;
      }));
      const tables = new Map(entries) as NativeTables;
      const volume = await decodeVolume(schema, tables);
      log.info("the semantic volume is open", {
        op: "render", path: base, worlds: volume.worlds.length,
      });
      return volume;
    });
  }

  private json<T>(url: string): Promise<T> {
    return this.keep(url, async (at) => (await this.response(at)).json() as Promise<T>);
  }

  private async bytes(url: string): Promise<ArrayBuffer> {
    return (await this.response(url)).arrayBuffer();
  }

  private async response(url: string): Promise<Response> {
    const response = await fetch(url);
    if (response.status === 404) {
      log.warn("a payload is gone, which means the build moved", {
        op: "render", path: url,
      });
      throw new BuildMovedError(url);
    }
    if (!response.ok) throw new Error(`${url} answered ${response.status}`);
    return response;
  }

  private keep<T>(url: string, read: (url: string) => Promise<T>): Promise<T> {
    const held = this.payloads.get(url) as Promise<T> | undefined;
    if (held) return held;
    // A failed read is forgotten rather than kept: a network hiccup should
    // cost a retry, not the rest of the session.
    const reading = read(url).catch((error: unknown) => {
      this.payloads.delete(url);
      throw error;
    });
    this.payloads.set(url, reading);
    return reading;
  }
}

/**
 * Where an icon asset lives.
 *
 * A free function as well as a method because an icon is the one thing under
 * a base that something other than the reader fetches: the browser asks for
 * it as an image, and both panes have to name it the same way or they compose
 * two rasters of one symbol.
 *
 * Every segment is encoded and the separators are not: an asset path is a
 * path, and a curated one may carry a space or a bracket — `Vault 101 (Ext)`
 * — which is a 404 if it goes onto the wire as it was written.
 */
export function iconURL(base: Base, asset: string): string {
  const path = asset.split("/").map((segment) => encodeURIComponent(segment)).join("/");
  return `${base}/${path}`;
}

export function fillTemplate(template: string, z: number, x: number, y: number, format: string): string {
  return template.replaceAll("{z}", String(z)).replaceAll("{x}", String(x))
    .replaceAll("{y}", String(y)).replaceAll("{format}", format);
}

async function sha256(bytes: Uint8Array): Promise<string> {
  const copy = Uint8Array.from(bytes);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", copy.buffer));
  return [...digest].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
