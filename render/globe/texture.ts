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
  private baseKey = "";
  private detailKey = "";

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

  /** Put the pyramid tiles away. A globe nobody is looking at holds none. */
  clear(): void {
    this.tiles.clear();
    this.detailKey = "";
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

  /** Composite the base skin, once per lens. Returns whether anything moved. */
  async base(
    base: string,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<void> {
    const key = `${base}/${lens.tiles}`;
    if (key === this.baseKey) return;
    this.baseKey = key;
    this.lens = lens.tiles;
    this.paper?.clearRect(0, 0, TEXTURE_WIDTH, TEXTURE_HEIGHT);
    const z = this.baseLevel(lens);
    await this.paint(z, lens, url, changed, null);
    log.info("the sphere has its skin", { op: "render", lens: lens.name, z });
  }

  /**
   * Composite the neighbourhood under the camera.
   *
   * The window is a degree box the caller worked out from the camera; only
   * tiles inside it, at the level the camera's distance asked for, and only
   * where the coverage admits one.
   */
  async detail(
    lens: Lens,
    z: number,
    at: { x: number; y: number; width: number; height: number },
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
  ): Promise<void> {
    const key = `${lens.tiles}/${z}/${Math.round(at.x)}/${Math.round(at.y)}`;
    if (key === this.detailKey) return;
    this.detailKey = key;
    this.tiles.clear();
    this.lens = lens.tiles;
    await this.paint(z, lens, url, changed, at);
  }

  private async paint(
    z: number,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
    limit: { x: number; y: number; width: number; height: number } | null,
  ): Promise<void> {
    const paper = this.paper;
    if (!paper) return;
    paper.imageSmoothingEnabled = lens.interpolate;
    const coverage = new LensCoverage(lens);
    const edge = this.grid.size / 2 ** z;
    const box = limit ?? this.window;
    const x0 = Math.max(Math.floor(box.x / edge), Math.floor(this.window.x / edge));
    const x1 = Math.min(
      Math.ceil((box.x + box.width) / edge),
      Math.ceil((this.window.x + this.window.width) / edge));
    const y0 = Math.max(Math.floor(box.y / edge), Math.floor(this.window.y / edge));
    const y1 = Math.min(
      Math.ceil((box.y + box.height) / edge),
      Math.ceil((this.window.y + this.window.height) / edge));
    const scale = (edge / this.window.width) * TEXTURE_WIDTH;
    const scaleY = (edge / this.window.height) * TEXTURE_HEIGHT;
    for (let x = x0; x < x1; x++) {
      for (let y = y0; y < y1; y++) {
        if (!coverage.has(this.grid, z, x, y)) continue;
        const at = url(z, x, y);
        if (!at) continue;
        const image = await load(at);
        if (!image) continue;
        const [px, py] = this.place(x * edge, y * edge);
        paper.drawImage(image, px, py, scale, scaleY);
        if (limit) this.tiles.set(`${z}/${x}/${y}`, true);
        changed();
      }
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
