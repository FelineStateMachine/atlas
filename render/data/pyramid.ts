// Which tiles exist, and where to ask for them.
//
// A lens is a pyramid over the volume's world square: `tileSize` pixels per
// tile edge, level `z` cut into `2^z` tiles per axis across the whole square,
// so level 0 is the square in one tile. That is the inherited constant set
// (issue #5 §5.1) and it is frozen; nothing here derives it from anything.
//
// Two rules make the difference between asking for what is there and asking
// for everything:
//
//   BOUNDS. A lens fills a window of the square, not always the square. Mars
//   is 8192 × 4096 of an 8192 square, so its level 6 holds 64 × 32 tiles and
//   not 64 × 64 — and the 2,048 the golden inventory records are exactly the
//   ones inside the window.
//
//   COVERAGE. Past `fullZoom` a pyramid holds only what the capture reached,
//   and the per-level bitset says which. A reader must consult it before
//   requesting a tile: asking for one that was never written is a wasted
//   request, and the correct fallback is the parent tile drawn larger
//   (format.md §6.3.1).
//
// Both together are what makes the base/detail layer split of the chart
// honest: the base layer draws the complete levels and can always be
// upsampled, the detail layer draws the patchy ones and asks only where the
// bitset says yes.

import type { CoverageLevel, Lens, PixelRect, TileGrid } from "./payload.ts";
import { tileFormat } from "./payload.ts";

/** A half-open tile window at one level: `x0 ≤ x < x1`, `y0 ≤ y < y1`. */
export interface TileWindow {
  readonly x0: number;
  readonly x1: number;
  readonly y0: number;
  readonly y1: number;
}

/** The world-pixel window a lens fills: its own bounds, or the whole square. */
export function lensWindow(lens: Lens, grid: TileGrid): PixelRect {
  return lens.bounds ?? { x: 0, y: 0, width: grid.size, height: grid.size };
}

/** The world-pixel edge of one tile at zoom `z`. */
export function tileEdge(grid: TileGrid, z: number): number {
  return grid.size / 2 ** z;
}

/** The tiles of level `z` the lens's window touches. */
export function tileWindowAt(lens: Lens, grid: TileGrid, z: number): TileWindow {
  const edge = tileEdge(grid, z);
  const window = lensWindow(lens, grid);
  const span = 2 ** z;
  const clamp = (value: number) => Math.min(Math.max(value, 0), span);
  return {
    x0: clamp(Math.floor(window.x / edge)),
    x1: clamp(Math.ceil((window.x + window.width) / edge)),
    y0: clamp(Math.floor(window.y / edge)),
    y1: clamp(Math.ceil((window.y + window.height) / edge)),
  };
}

/**
 * A level's bitset, decoded once.
 *
 * `bits` is standard base64 over a row-major bitset of the level's window,
 * least significant bit first within each byte. The decode is `atob`, which
 * every host this seam runs in has — browser, headless browser, and the Node
 * that runs its tests.
 */
export class Coverage {
  private readonly bits: Uint8Array;

  private readonly level: CoverageLevel;

  constructor(level: CoverageLevel) {
    this.level = level;
    const binary = atob(level.bits);
    this.bits = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) this.bits[i] = binary.charCodeAt(i);
  }

  /** Whether tile `(x, y)` of this level was written. */
  has(x: number, y: number): boolean {
    const c = x - this.level.x;
    const r = y - this.level.y;
    if (c < 0 || r < 0 || c >= this.level.w || r >= this.level.h) return false;
    const i = r * this.level.w + c;
    return ((this.bits[i >> 3] ?? 0) & (1 << (i & 7))) !== 0;
  }
}

/** A lens's coverage, decoded once per level, keyed by zoom. */
export class LensCoverage {
  private readonly levels = new Map<number, Coverage | null>();

  private readonly lens: Lens;

  constructor(lens: Lens) {
    this.lens = lens;
  }

  /**
   * Whether tile `(z, x, y)` exists.
   *
   * A level with no coverage entry is fully covered, so the answer is yes
   * inside the lens's own window and no outside it: a request for a tile the
   * window never held is as wasted as a request for one the bitset denies.
   */
  has(grid: TileGrid, z: number, x: number, y: number): boolean {
    if (z < this.lens.minZoom || z > this.lens.maxZoom) return false;
    const window = tileWindowAt(this.lens, grid, z);
    if (x < window.x0 || x >= window.x1 || y < window.y0 || y >= window.y1) return false;
    let coverage = this.levels.get(z);
    if (coverage === undefined) {
      const declared = this.lens.coverage?.[String(z)];
      coverage = declared ? new Coverage(declared) : null;
      this.levels.set(z, coverage);
    }
    return coverage === null || coverage.has(x, y);
  }
}

/** The path of one tile under the volume's base, or null where none exists. */
export function tilePath(lens: Lens, z: number, x: number, y: number): string | null {
  const extension = tileFormat(lens, z);
  if (!extension) return null;
  return `tiles/${lens.tiles}/${z}/${x}/${y}.${extension}`;
}

/**
 * Every tile a lens holds, as inventory names (`<z>/<x>/<y>.<ext>`), in the
 * inventory's own order: z, then x, then y, numerically.
 *
 * This exists for the tests — the golden per-lens inventories are the record
 * of what a pyramid holds, and a reader that agrees with them agrees about
 * bounds, about coverage, and about the format per level all at once.
 */
export function inventoryNames(lens: Lens, grid: TileGrid): string[] {
  const coverage = new LensCoverage(lens);
  const names: string[] = [];
  for (let z = lens.minZoom; z <= lens.maxZoom; z++) {
    const extension = tileFormat(lens, z);
    if (!extension) continue;
    const window = tileWindowAt(lens, grid, z);
    for (let x = window.x0; x < window.x1; x++) {
      for (let y = window.y0; y < window.y1; y++) {
        if (coverage.has(grid, z, x, y)) names.push(`${z}/${x}/${y}.${extension}`);
      }
    }
  }
  return names;
}
