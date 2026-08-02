// The corner locator: the whole world, and where the camera is on it.
//
// The application renders the shelf, the dock button and the two surfaces —
// `#overview-canvas` and `#overview-viewport` — and marks them
// `hx-morph-skip-children`, because a swap that dropped the canvas would cost
// a redraw of a whole world to say one boolean. Everything inside them is the
// seam's.
//
// The world is drawn ONCE per lens, from the shallowest pyramid level big
// enough to read at the shelf's size, and after that only the rectangle
// moves. That is the whole performance argument: a locator that recomposited
// the world on every camera event would be the most expensive thing on the
// page, and it says exactly as much either way.
//
// The rectangle is written in whole pixels. The parity harness reads it as
// `"left top width height"` and sub-pixel drift between two runs of one tour
// is a difference that means nothing — the same discipline the camera check
// already used.

import type OLMap from "ol/Map.js";
import { logger } from "../log.ts";
import type { WorldContext } from "../context.ts";
import { lensExtent } from "./projection.ts";

const log = logger("overview");

/** How big the drawn world should be before it is worth reading. */
const TARGET_SIZE = 168;

/** The mark the sphere's camera leaves: a dot, in whole pixels. */
const MARK_SIZE = 22;

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

export class Overview {
  private drawnKey = "";
  /**
   * The mark the sphere last left, kept so a redraw the chart asked for does
   * not overwrite it. While the globe is up the chart has not moved, so its
   * extent is a stale answer to a question only the sphere can answer.
   */
  private marked: readonly number[] | null = null;

  private readonly map: OLMap;
  private readonly context: () => WorldContext | null;

  constructor(map: OLMap, context: () => WorldContext | null) {
    this.map = map;
    this.context = context;
  }

  /** Forget the drawn world, so the next draw recomposites it. */
  forget(): void {
    this.drawnKey = "";
  }

  /**
   * Redraw what needs redrawing: the world rarely, the rectangle always.
   *
   * `over` is the camera's own extent when the caller has one the map does
   * not — which is the sphere's case, where the locator is the only written
   * form of the globe's camera and the chart underneath has not moved.
   */
  draw(over?: readonly number[]): void {
    const context = this.context();
    const canvas = document.querySelector<HTMLCanvasElement>("#overview-canvas");
    const box = document.querySelector<HTMLElement>("#overview-viewport");
    if (!context || !context.lens || !canvas || !box) return;
    const extent = lensExtent(context.lens, context.grid);
    const key = `${context.base}/${context.model.slug}/${context.lens.tiles}`;
    if (key !== this.drawnKey) {
      this.drawnKey = key;
      void this.compose(canvas, context, extent);
    }
    if (over && over.length === 2) this.marked = over;
    this.locate(box, canvas, extent, over ?? this.marked ?? undefined);
  }

  /** Give the locator back to the chart: the sphere has been put away. */
  release(): void {
    this.marked = null;
  }

  /**
   * Where the camera is, as a box in whole pixels over the drawn world.
   *
   * `over` of length two is a *point* rather than a window, which is the
   * sphere's case: half a planet is always out of sight, so what the locator
   * marks there is the place the camera faces and not an extent it can see.
   * The mark is a fixed dot -- 22 pixels, the size every recorded globe step
   * carries -- because a rectangle drawn true to a camera hanging over a
   * curved surface says nothing a reader can use.
   *
   * The pixels are the canvas's own, not its CSS box: the composite is
   * written at one device pixel per drawn pixel and the reticle is read back
   * in those, which is what makes "117 53 22 22" reproducible.
   */
  private locate(
    box: HTMLElement,
    canvas: HTMLCanvasElement,
    extent: number[],
    over?: readonly number[],
  ): void {
    const size = this.map.getSize();
    if (!size && !over) return;
    const width = (extent[2] ?? 0) - (extent[0] ?? 0);
    const height = (extent[3] ?? 0) - (extent[1] ?? 0);
    if (!width || !height) return;
    const scaleX = canvas.width / width;
    const scaleY = canvas.height / height;
    if (over && over.length === 2) {
      // On the sphere the locator is always worth having: half a planet is
      // out of sight whatever the camera does.
      const marked = document.querySelector<HTMLElement>("#atlas-overview");
      if (marked) marked.hidden = false;
      const across = clamp(((over[0] ?? 0) - (extent[0] ?? 0)) / width, 0, 1);
      const down = clamp(((extent[3] ?? 0) - (over[1] ?? 0)) / height, 0, 1);
      box.style.left = `${Math.round(across * canvas.width - MARK_SIZE / 2)}px`;
      box.style.top = `${Math.round(down * canvas.height - MARK_SIZE / 2)}px`;
      box.style.width = `${MARK_SIZE}px`;
      box.style.height = `${MARK_SIZE}px`;
      return;
    }
    const camera = over ?? this.map.getView().calculateExtent(size ?? [1, 1]);
    // Hidden while the whole map is on screen: a locator that says "all of
    // it" tells the reader nothing they cannot already see. Fitting the map
    // lands on its extent to within a fraction of a pixel, so the comparison
    // carries four pixels of slack -- an exact one reports the whole map as
    // not quite visible and leaves a locator on screen marking everything.
    const shelf = document.querySelector<HTMLElement>("#atlas-overview");
    if (shelf) {
      const slack = (this.map.getView().getResolution() ?? 0) * 4;
      shelf.hidden =
        (camera[0] ?? 0) <= (extent[0] ?? 0) + slack &&
        (camera[1] ?? 0) <= (extent[1] ?? 0) + slack &&
        (camera[2] ?? 0) >= (extent[2] ?? 0) - slack &&
        (camera[3] ?? 0) >= (extent[3] ?? 0) - slack;
      if (shelf.hidden) return;
    }
    const left = ((camera[0] ?? 0) - (extent[0] ?? 0)) * scaleX;
    const top = ((extent[3] ?? 0) - (camera[3] ?? 0)) * scaleY;
    box.style.left = `${Math.round(left)}px`;
    box.style.top = `${Math.round(top)}px`;
    box.style.width = `${Math.round(((camera[2] ?? 0) - (camera[0] ?? 0)) * scaleX)}px`;
    box.style.height = `${Math.round(((camera[3] ?? 0) - (camera[1] ?? 0)) * scaleY)}px`;
  }

  /**
   * The world, composited once.
   *
   * The level chosen is the shallowest whose picture of the *lens's own
   * window* is big enough to read — a lens filling a quarter of the world
   * square needs to go two levels deeper for the same number of pixels.
   */
  private async compose(
    canvas: HTMLCanvasElement,
    context: WorldContext,
    extent: number[],
  ): Promise<void> {
    const lens = context.lens;
    if (!lens) return;
    const width = (extent[2] ?? 0) - (extent[0] ?? 0);
    const height = (extent[3] ?? 0) - (extent[1] ?? 0);
    const fraction = Math.max(width, height) / context.grid.size;
    let z = lens.minZoom;
    while (z < lens.fullZoom && context.grid.tileSize * 2 ** z * fraction < TARGET_SIZE) z++;

    const edge = context.grid.size / 2 ** z;
    const scale = (context.grid.tileSize * 2 ** z) / context.grid.size;
    canvas.width = Math.max(1, Math.round(width * scale));
    canvas.height = Math.max(1, Math.round(height * scale));
    const paper = canvas.getContext("2d");
    if (!paper) return;
    paper.imageSmoothingEnabled = lens.interpolate;
    if (lens.background) {
      paper.fillStyle = lens.background;
      paper.fillRect(0, 0, canvas.width, canvas.height);
    }
    const x0 = Math.floor((extent[0] ?? 0) / edge);
    const x1 = Math.ceil((extent[2] ?? 0) / edge);
    const y0 = Math.floor(-(extent[3] ?? 0) / edge);
    const y1 = Math.ceil(-(extent[1] ?? 0) / edge);
    let drawn = 0;
    for (let x = x0; x < x1; x++) {
      for (let y = y0; y < y1; y++) {
        const url = this.tileURL(context, z, x, y);
        if (!url) continue;
        const image = await load(url);
        if (!image) continue;
        paper.drawImage(
          image,
          (x * edge - (extent[0] ?? 0)) * scale,
          (y * edge + (extent[3] ?? 0)) * scale,
          edge * scale, edge * scale,
        );
        drawn++;
      }
    }
    log.debug("the locator has its world", { op: "render", lens: lens.name, tiles: drawn, z });
  }

  private tileURL(context: WorldContext, z: number, x: number, y: number): string | null {
    const lens = context.lens;
    if (!lens) return null;
    const extension = lens.formats[z - lens.minZoom];
    if (!extension) return null;
    return `${context.base}/tiles/${lens.tiles}/${z}/${x}/${y}.${extension}`;
  }
}

function load(url: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const image = new Image();
    image.onload = () => resolve(image);
    // A tile the locator cannot have is a gap in a thumbnail, not a failure.
    image.onerror = () => resolve(null);
    image.src = url;
  });
}
