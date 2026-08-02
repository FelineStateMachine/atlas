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
// Nothing here interprets a payload. The types are `payload.ts`, the packed
// locations are `atlasloc.ts`, and what to draw is the scene's business.

import { logger } from "../log.ts";
import { LocationTable } from "./atlasloc.ts";
import type { Catalog, Lens, TextPayload, WorldPayload } from "./payload.ts";
import { tileFormat } from "./payload.ts";

const log = logger("data");

/** A build's URL prefix: `/data/v/<slug>/<stamp12>`. */
export type Base = string;

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
 * Payloads are kept per URL, which is per build, because the URL names the
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

  /** `worlds/<slug>.json` — read when the world opens. */
  world(base: Base, world: string): Promise<WorldPayload> {
    return this.json<WorldPayload>(`${base}/worlds/${world}.json`);
  }

  /** `worlds/<slug>.text` — read lazily, when a card opens. */
  text(base: Base, world: string): Promise<TextPayload> {
    return this.json<TextPayload>(`${base}/worlds/${world}.text`);
  }

  /**
   * `worlds/<slug>.bin` — every point feature, as views over the downloaded
   * buffer. The response is read as an `ArrayBuffer` precisely so the views
   * can be built over it without a copy.
   */
  locations(base: Base, world: string): Promise<LocationTable> {
    return this.keep(`${base}/worlds/${world}.bin`, async (url) => {
      const buffer = await this.bytes(url);
      const table = LocationTable.over(buffer);
      log.info("the packed locations are open", {
        op: "render", path: url, count: table.count,
      });
      return table;
    });
  }

  /** Where an icon asset lives. Icons are fetched by the browser, as images. */
  iconURL(base: Base, asset: string): string {
    return `${base}/icons/${asset}`;
  }

  /** Where one tile lives, or null when the lens holds no such level. */
  tileURL(base: Base, lens: Lens, z: number, x: number, y: number): string | null {
    const extension = tileFormat(lens, z);
    if (!extension) return null;
    return `${base}/tiles/${lens.tiles}/${z}/${x}/${y}.${extension}`;
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
