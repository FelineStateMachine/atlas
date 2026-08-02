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
  /**
   * Which composite is current. A paint walks tiles one image at a time and
   * awaits each, so a camera that moved -- or a sphere that was put away --
   * while it was walking would otherwise go on adding tiles to a
   * neighbourhood nobody is over any more. The tour asserts exactly this: a
   * put-away globe keeps no pyramid tiles.
   */
  private generation = 0;

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
    this.generation++;
    this.tiles.clear();
    this.detailKey = "";
    this.lens = "";
  }

  /** Forget the neighbourhood without forgetting the skin under it. */
  clearDetail(): void {
    this.generation++;
    this.tiles.clear();
    this.detailKey = "";
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
    if (key === this.detailKey) return;
    this.generation++;
    this.detailKey = key;
    this.tiles.clear();
    this.lens = lens.tiles;
    await this.paint(z, lens, url, changed, wanted);
  }

  private async paint(
    z: number,
    lens: Lens,
    url: (z: number, x: number, y: number) => string | null,
    changed: () => void,
    only: readonly (readonly [number, number])[] | null,
  ): Promise<void> {
    const paper = this.paper;
    if (!paper) return;
    const mine = this.generation;
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
      if (mine !== this.generation) return;
      if (!image) continue;
      const [px, py] = this.place(x * edge, y * edge);
      paper.drawImage(image, px, py, scale, scaleY);
      if (only) this.tiles.set(`${z}/${x}/${y}`, true);
      changed();
    }
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
