// The sphere's skin: an equirectangular composite of a lens's own tiles.
//
// A world that declares `atlas.geometry.surface: sphere` also declares the
// window of world pixels its raster fills and the degrees that window
// pictures (format.md §6.7). Equirectangular is the one projection that
// drapes a sphere without resampling — which is why the globe asks for it by
// name — so the composite is a straight copy: the declared window's pixels,
// laid into a texture whose edges are the declared degrees.
//
// ONE PASS, and its size is the reason there is only one. The base skin is
// composited once per lens, from the shallowest complete level big enough to
// read; it is what a reader sees the moment the sphere comes up, and it never
// changes. It is also a ceiling: 4096 pixels across the whole equirect window
// is level four exactly, so a deeper tile composited in here would be drawn
// at a quarter of the pitch it was captured at and three quarters of the
// capture thrown away before anything reached the screen.
//
// So the detail under the camera is NOT composited here. It is draped as its
// own mesh per tile, wearing its own texture at its own size — see
// `refreshDetail` in ./element.ts. This file used to hold a second pass that
// drew those tiles into this canvas, and the two passes sharing one texture,
// one counter and one key is what once left the sphere black on first entry.
// There is one pass now, and that whole class of defect went with the other.

import { logger } from "../log.ts";
import type { Lens, TileGrid } from "../data/payload.ts";
import { LensCoverage } from "../data/pyramid.ts";

const log = logger("globe");

/** The texture's own size. Big enough for the base skin, small enough to hold. */
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
  /** The pyramid the skin is woven from. */
  lens = "";
  private readonly paper: CanvasRenderingContext2D | null;
  /**
   * What the texture is known to hold, and what a live pass is in the middle
   * of laying down.
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
  /**
   * Which composite is current. A paint walks tiles one image at a time and
   * awaits each, so a lens that changed -- or a sphere that was put away --
   * while it was walking would otherwise go on drawing a pyramid nobody is
   * over any more.
   */
  private baseGeneration = 0;

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
   * Forget the skin. A pass still arriving is cancelled with it.
   *
   * The texture's pixels are left alone -- what was painted stays painted --
   * but nothing is claimed as current any more, so whatever comes back asks
   * for its composite again.
   */
  clear(): void {
    this.baseGeneration++;
    this.basePainting = "";
    this.lens = "";
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
   * A newer lens supersedes an older one in flight: the texture is about to
   * be wiped, so what it held is no longer what `baseKey` says it held, and
   * the key goes back to naming nothing until this paint earns it.
   */
  async base(
    base: string,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<void> {
    const key = `${base}/${lens.tiles}`;
    if (key === this.baseKey || key === this.basePainting) return;
    const mine = ++this.baseGeneration;
    this.basePainting = key;
    this.baseKey = "";
    this.lens = lens.tiles;
    this.paper?.clearRect(0, 0, TEXTURE_WIDTH, TEXTURE_HEIGHT);
    const z = this.baseLevel(lens);
    if (!await this.paint(mine, z, lens, url, changed)) return;
    this.baseKey = key;
    this.basePainting = "";
    log.info("the sphere has its skin", { op: "render", lens: lens.name, z });
  }

  /**
   * Walk the tiles of one pass, drawing each as it arrives.
   *
   * Returns whether the walk finished as itself. `false` means a newer pass
   * -- or a clear -- came through while this one was waiting on an image, and
   * the caller must not claim the texture holds what it was drawing.
   */
  private async paint(
    mine: number,
    z: number,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<boolean> {
    const paper = this.paper;
    if (!paper) return false;
    paper.imageSmoothingEnabled = lens.interpolate;
    const coverage = new LensCoverage(lens);
    const edge = this.grid.size / 2 ** z;
    const scale = (edge / this.window.width) * TEXTURE_WIDTH;
    const scaleY = (edge / this.window.height) * TEXTURE_HEIGHT;
    for (const [x, y] of this.windowTiles(edge)) {
      if (!coverage.has(this.grid, z, x, y)) continue;
      const at = url(z, x, y);
      if (!at) continue;
      const image = await load(at);
      if (mine !== this.baseGeneration) return false;
      if (!image) continue;
      const [px, py] = this.place(x * edge, y * edge);
      paper.drawImage(image, px, py, scale, scaleY);
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
