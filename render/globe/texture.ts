// The sphere's skin: an equirectangular composite of a lens's own tiles.
//
// A world that declares `atlas.geometry.surface: sphere` also declares the
// window of world pixels its raster fills and the degrees that window
// pictures (format.md §6.7). Equirectangular is the one projection that
// drapes a sphere without resampling — which is why the globe asks for it by
// name — so the composite is a straight copy: the declared window's pixels,
// laid into a texture whose edges are the declared degrees.
//
// TWO PASSES, and the difference between them is the whole of the globe's
// tile budget:
//
//   THE BASE SKIN is composited once per lens, from the shallowest complete
//   level big enough to read. It is what a reader sees the moment the sphere
//   comes up, and it never changes.
//
//   THE DETAIL is composited under the camera, from as deep as the camera is
//   close, and only where the coverage says a tile was written. It is drawn
//   into the same texture — a sphere with a hundred patch meshes on it is a
//   hundred draw calls to say what one texture already says — and the tiles
//   that went into it are the bookkeeping the parity harness reads.

import { logger } from "../log.ts";
import type { Lens, TileGrid } from "../data/payload.ts";
import { LensCoverage } from "../data/pyramid.ts";

const log = logger("globe");

/** The texture's own size. Big enough for a deep pass, small enough to hold. */
const TEXTURE_WIDTH = 4096;
const TEXTURE_HEIGHT = 2048;

/** The base skin wants at least this many pixels across before it reads. */
const BASE_TARGET = 2048;

/** A rectangle of world pixels, as the world declares its equirect window. */
export interface Window {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

/** The composite, and the account of what went into it. */
export class Skin {
  readonly canvas: HTMLCanvasElement;
  /** Detail tiles currently draped under the camera, keyed `z/x/y`. */
  readonly tiles = new Map<string, true>();
  /** The pyramid the neighbourhood is drawn from. */
  lens = "";
  private readonly paper: CanvasRenderingContext2D | null;
  /**
   * What the texture is known to hold, per pass, and what a live pass is in
   * the middle of laying down.
   *
   * A key is committed only when its paint finishes un-superseded, because a
   * paint that was abandoned half way left the texture holding neither the
   * composite it was replacing nor the one it was drawing. Committing on
   * entry -- which is what the first cut of this file did -- makes an
   * abandoned pass permanent: the key says the skin is there, so nobody ever
   * asks for it again, and the sphere stays black.
   */
  private baseKey = "";
  private basePainting = "";
  private detailKey = "";
  private detailPainting = "";
  /**
   * Which composite is current. A paint walks tiles one image at a time and
   * awaits each, so a camera that moved -- or a sphere that was put away --
   * while it was walking would otherwise go on adding tiles to a
   * neighbourhood nobody is over any more. The tour asserts exactly this: a
   * put-away globe keeps no pyramid tiles.
   *
   * THERE IS ONE COUNTER PER PASS, and that is the point of them. The two
   * passes are independent -- the base skin is composited once per lens and
   * never changes, the neighbourhood is recomposited every time the camera
   * moves -- so a counter they shared let the cheap, frequent pass cancel the
   * expensive, once-per-lens one. `openLens` drops the neighbourhood and then
   * starts the base skin without waiting for it; the first `refreshDetail` of
   * an entry, seen from far enough out to want no detail at all, drops the
   * neighbourhood again while the base skin is suspended on its first tile.
   * With one counter that second drop cancelled the base skin, and with the
   * key already committed nothing retried it. Each pass now answers only to
   * its own counter.
   */
  private baseGeneration = 0;
  private detailGeneration = 0;

  private readonly window: Window;
  private readonly grid: TileGrid;

  constructor(window: Window, grid: TileGrid) {
    this.window = window;
    this.grid = grid;
    this.canvas = document.createElement("canvas");
    this.canvas.width = TEXTURE_WIDTH;
    this.canvas.height = TEXTURE_HEIGHT;
    this.paper = this.canvas.getContext("2d", { willReadFrequently: false });
  }

  /**
   * Put the pyramid tiles away. A globe nobody is looking at holds none.
   *
   * This is the whole-skin form: both passes are cancelled, including a base
   * skin still arriving. The texture's pixels are left alone -- what was
   * painted stays painted -- but nothing is claimed as current any more, so
   * whatever comes back asks for its composite again.
   */
  clear(): void {
    this.baseGeneration++;
    this.detailGeneration++;
    this.basePainting = "";
    this.tiles.clear();
    this.detailKey = "";
    this.detailPainting = "";
    this.lens = "";
  }

  /**
   * Forget the neighbourhood without forgetting the skin under it.
   *
   * Only the detail pass is cancelled. A base skin arriving underneath is
   * none of this call's business, and cancelling it here is what left the
   * sphere black on first entry.
   */
  clearDetail(): void {
    this.detailGeneration++;
    this.tiles.clear();
    this.detailKey = "";
    this.detailPainting = "";
  }

  /** Where a world pixel lands on the texture. */
  private place(x: number, y: number): [number, number] {
    return [
      ((x - this.window.x) / this.window.width) * TEXTURE_WIDTH,
      ((y - this.window.y) / this.window.height) * TEXTURE_HEIGHT,
    ];
  }

  /** The shallowest level whose picture of the window is worth showing. */
  baseLevel(lens: Lens): number {
    const fraction = this.window.width / this.grid.size;
    let z = lens.minZoom;
    while (z < lens.fullZoom && this.grid.tileSize * 2 ** z * fraction < BASE_TARGET) z++;
    return z;
  }

  /**
   * Composite the base skin, once per lens.
   *
   * The two passes can now be in flight at once, because neither cancels the
   * other any more, and they share one texture: a base skin still arriving
   * wipes and repaints over a neighbourhood that got there first. That costs
   * a patch of shallower tiles until the camera next moves, and it is only
   * reachable by zooming past the skin's own depth inside the few hundred
   * milliseconds the skin takes to arrive — which is a far better trade than
   * what the shared counter bought, where the neighbourhood cancelled the
   * skin outright and the sphere stayed black.
   */
  async base(
    base: string,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<void> {
    const key = `${base}/${lens.tiles}`;
    if (key === this.baseKey || key === this.basePainting) return;
    // A newer base skin supersedes an older one: the texture is about to be
    // wiped, so what it held is no longer what `baseKey` says it held, and
    // the key goes back to naming nothing until this paint earns it.
    const mine = ++this.baseGeneration;
    this.basePainting = key;
    this.baseKey = "";
    this.lens = lens.tiles;
    this.paper?.clearRect(0, 0, TEXTURE_WIDTH, TEXTURE_HEIGHT);
    const z = this.baseLevel(lens);
    if (!await this.paint(mine, z, lens, url, changed, null)) return;
    this.baseKey = key;
    this.basePainting = "";
    log.info("the sphere has its skin", { op: "render", lens: lens.name, z });
  }

  /**
   * Composite the neighbourhood under the camera.
   *
   * The caller names the tiles, by column and row of the pyramid's own grid,
   * because that is the unit the neighbourhood is budgeted in: a block of
   * (2·reach+1)² tiles around the one the camera faces. Working the block out
   * as a rectangle of world pixels and re-deriving the tiles from it is what
   * put a hundred tiles under a camera the reference gave eighty-one — a
   * rectangle landing a hair past a boundary takes the whole next row.
   */
  async detail(
    lens: Lens,
    z: number,
    wanted: readonly (readonly [number, number])[],
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<void> {
    const key = `${lens.tiles}/${z}/` +
      wanted.map(([x, y]) => `${x},${y}`).sort().join(" ");
    if (key === this.detailKey || key === this.detailPainting) return;
    // Same bargain as the base pass, and the same latent bug avoided: the
    // neighbourhood is only this one once its tiles are actually on the
    // texture, so a camera that keeps moving retries rather than settling for
    // a block that was abandoned half drawn.
    const mine = ++this.detailGeneration;
    this.detailPainting = key;
    this.detailKey = "";
    this.tiles.clear();
    this.lens = lens.tiles;
    if (!await this.paint(mine, z, lens, url, changed, wanted)) return;
    this.detailKey = key;
    this.detailPainting = "";
  }

  /**
   * Walk the tiles of one pass, drawing each as it arrives.
   *
   * Returns whether the walk finished as itself. `false` means a newer pass
   * of the same kind -- or a clear -- came through while this one was waiting
   * on an image, and the caller must not claim the texture holds what it was
   * drawing. The counter it answers to is its own pass's: `only` is the
   * neighbourhood's tile list, and so also the answer to which pass this is.
   */
  private async paint(
    mine: number,
    z: number,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
    only: readonly (readonly [number, number])[] | null,
  ): Promise<boolean> {
    const paper = this.paper;
    if (!paper) return false;
    paper.imageSmoothingEnabled = lens.interpolate;
    const coverage = new LensCoverage(lens);
    const edge = this.grid.size / 2 ** z;
    const scale = (edge / this.window.width) * TEXTURE_WIDTH;
    const scaleY = (edge / this.window.height) * TEXTURE_HEIGHT;
    for (const [x, y] of only ?? this.windowTiles(edge)) {
      if (!coverage.has(this.grid, z, x, y)) continue;
      const at = url(z, x, y);
      if (!at) continue;
      const image = await load(at);
      if (mine !== (only ? this.detailGeneration : this.baseGeneration)) return false;
      if (!image) continue;
      const [px, py] = this.place(x * edge, y * edge);
      paper.drawImage(image, px, py, scale, scaleY);
      if (only) this.tiles.set(`${z}/${x}/${y}`, true);
      changed();
    }
    return true;
  }

  /** Every tile of one level the declared window covers, column by column. */
  private *windowTiles(edge: number): Generator<[number, number]> {
    const x0 = Math.floor(this.window.x / edge);
    const x1 = Math.ceil((this.window.x + this.window.width) / edge);
    const y0 = Math.floor(this.window.y / edge);
    const y1 = Math.ceil((this.window.y + this.window.height) / edge);
    for (let x = x0; x < x1; x++) {
      for (let y = y0; y < y1; y++) yield [x, y];
    }
  }
}

function load(url: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const image = new Image();
    image.crossOrigin = "anonymous";
    image.onload = () => resolve(image);
    image.onerror = () => resolve(null);
    image.src = url;
  });
}
