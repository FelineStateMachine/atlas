// `<atlas-chart>` — the plane, drawn.
//
// Twelve layers, in one z-order that is a set of arguments rather than a
// convention:
//
//    0  raster            the complete pyramid
//    1  rasterDetail      the patchy levels above it, riding on top
//    5  grid              the chosen cell and its subdivision, UNDER the pins
//    6  zoneScrim         the dimming, seen THROUGH, not painted over
//   10  zones             paths and areas
//   20  zoneTitles        their names, at the curated policy
//   40  pins              the markers
//   42  zonePins          the markers a highlight claimed, over the ground
//   44  zoneTitleDetail   a name a highlight or a hold revealed, over the pins
//   45  pinLabels         the names beside the markers, decluttered
//   48  gridContext       the dimmed neighbours, OVER the pins they dim
//   50  priority          selected, hovered, searched, in-cell — over everything
//
// The element owns no discrete state. Everything it draws it was told: the
// scene says what is hidden, highlighted, searched, selected and held, and
// the model says what exists. What it owns is the camera, the hover, and the
// bookkeeping of tiles — the continuous half of issue #5 §4.1 — and the
// camera it reports upward once per settle.

import OLMap from "ol/Map.js";
import View from "ol/View.js";
import Feature from "ol/Feature.js";
import Point from "ol/geom/Point.js";
import Polygon from "ol/geom/Polygon.js";
import MultiPolygon from "ol/geom/MultiPolygon.js";
import MultiLineString from "ol/geom/MultiLineString.js";
import VectorLayer from "ol/layer/Vector.js";
import VectorSource from "ol/source/Vector.js";
import Style from "ol/style/Style.js";
import { defaults as defaultControls } from "ol/control/defaults.js";
import type { FeatureLike } from "ol/Feature.js";
import type Projection from "ol/proj/Projection.js";
import type { CellVisual } from "@atlas/analysis";

import { logger } from "../log.ts";
import type { DataPlane } from "../data/plane.ts";
import { reportGridPick, reportPick } from "../data/report.ts";
import type { WorldContext } from "../context.ts";
import type { Lens } from "../data/payload.ts";
import type { PointRecord, ShapeRecord } from "../world/model.ts";
import { COORDINATE_SYSTEM, atlasProjection, lensExtent, viewMaxZoom } from "./projection.ts";
import { TileCounter, buildRaster } from "./raster.ts";
import type { Raster } from "./raster.ts";
import { Styles } from "./styles.ts";
import type { StyleContext } from "./styles.ts";
import { drawGrid } from "./grid.ts";
import type { DrawnCell } from "./grid.ts";
import { Overview } from "./overview.ts";

const log = logger("chart");

/**
 * How close a jump gets. A reader who reached for a feature by name wants to
 * see what is around it; a reader already deeper than this keeps their depth,
 * because they were reading something and the jump is a detour rather than a
 * reset.
 */
const FOCUS_ZOOM = 4;

/**
 * The room a held cell is given inside the window. A cell fitted edge to edge
 * has its own boundary drawn off the screen, and the boundary is the whole
 * point of holding it.
 */
const GRID_FIT_PADDING = 52;

/**
 * The room a piece of ground is given when a jump fits the camera to it. A
 * boundary drawn hard against the window edge reads as ground running off the
 * screen rather than as a shape with a size.
 */
const ZONE_FIT_PADDING = 54;

/**
 * The rectangle a shape occupies, or nothing when it carries no drawable
 * geometry. Outer rings and lines are enough: a hole is inside its own ring.
 *
 * Cached against the record, because it is asked once per shape every time the
 * footer recounts and a district can carry thousands of vertices. The records
 * are rebuilt with the world, so the cache empties with them.
 */
const shapeExtents = new WeakMap<ShapeRecord, [number, number, number, number] | null>();

function shapeExtent(shape: ShapeRecord): [number, number, number, number] | null {
  const held = shapeExtents.get(shape);
  if (held !== undefined) return held;
  let minimumX = Infinity;
  let minimumY = Infinity;
  let maximumX = -Infinity;
  let maximumY = -Infinity;
  for (const line of shape.lines) {
    for (const [x, y] of line) {
      if (x < minimumX) minimumX = x;
      if (y < minimumY) minimumY = y;
      if (x > maximumX) maximumX = x;
      if (y > maximumY) maximumY = y;
    }
  }
  const extent: [number, number, number, number] | null =
    Number.isFinite(minimumX) ? [minimumX, minimumY, maximumX, maximumY] : null;
  shapeExtents.set(shape, extent);
  return extent;
}


/**
 * Where a camera standing over one layer of a split sheet stands over another.
 *
 * By its position inside each layer's own box rather than by its coordinates:
 * the layers picture the same ground drawn in different places on the sheet,
 * so an absolute centre carried across lands somewhere else entirely. Only a
 * crossing between two *different* shards that both declare a box has anywhere
 * to be carried to; every other lens swap is a different picture of the ground
 * the reader is already looking at and moves nothing.
 */
function carryAcrossShards(
  previous: Lens | null,
  next: Lens | null,
  standing: { x: number; y: number } | null,
): [number, number] | null {
  if (!previous?.shard || !next?.shard || previous.shard === next.shard) return null;
  const from = previous.bounds;
  const to = next.bounds;
  if (!from || !to || !standing) return null;
  const clamp = (value: number) => Math.min(Math.max(value, 0), 1);
  const across = (standing.x - from.x) / from.width;
  const down = (-standing.y - from.y) / from.height;
  return [to.x + clamp(across) * to.width, -(to.y + clamp(down) * to.height)];
}

/**
 * What a pixel on the canvas resolved to.
 *
 * Two things can be under a pointer and they are not the same kind of answer,
 * which is why this is a discriminated union rather than an id and a label. A
 * feature has an identity the session stores and the card, the dock and the
 * legend all mark; a cell has an *address*, which no feature has and which
 * belongs to a different concern entirely. Flattening the two into one string
 * would leave every caller guessing which route it was holding.
 */
export interface CellPick {
  readonly kind: "cell";
  readonly cell: string;
}

/** A feature under the pointer: a pin, a piece of ground, or a run of one. */
export interface FeaturePick {
  readonly kind: "point" | "area" | "path";
  readonly id: string;
}

export type Pick = CellPick | FeaturePick;

/** What the chart publishes about itself, in the golden key names. */
export interface ChartDiagnostics {
  coordinateSystem: string;
  zoom: number | null;
  center: [number, number] | null;
  resolution: number | null;
  fitZoom: number | null;
  grid: { extent: number[] | null; cells: DrawnCell[] };
}

/**
 * The chart pane.
 *
 * Custom elements are progressive by construction: until this is defined the
 * tag renders nothing and the page is whole without it, which is the
 * deletability principle costing the application one script tag.
 */
export class AtlasChart extends HTMLElement {
  private map: OLMap | null = null;
  private view: View | null = null;
  private projection: Projection | null = null;
  private raster: Raster | null = null;
  private readonly counter = new TileCounter();
  private readonly sources = {
    grid: new VectorSource({ wrapX: false }),
    gridContext: new VectorSource({ wrapX: false }),
    zoneScrim: new VectorSource({ wrapX: false }),
    zones: new VectorSource({ wrapX: false }),
    zoneTitles: new VectorSource({ wrapX: false }),
    pins: new VectorSource({ wrapX: false }),
    priority: new VectorSource({ wrapX: false }),
  };
  /**
   * The two layers a cell is ever drawn on, held so a click can ask them and
   * only them. The chosen path lies under the pins and the dimmed context over
   * them, which is a z-order argument (chart/grid.ts) and is exactly why the
   * hit test cannot be a question about depth.
   */
  private gridLayers: VectorLayer<VectorSource>[] = [];
  private styles: Styles | null = null;
  private context: WorldContext | null = null;
  /** The feature the card is open about, so a change in it can be acted on. */
  private selected = "";
  /** The view a jump borrowed, and the view the jump landed on. */
  private borrowed: { x: number; y: number; zoom: number; rotation: number } | null = null;
  private landed: { x: number; y: number; zoom: number; rotation: number } | null = null;
  private sizes: ResizeObserver | null = null;
  /** The cell the camera was last flown to hold. */
  private heldCell = "";
  private plane: DataPlane | null = null;
  private overview: Overview | null = null;
  private fitZoom: number | null = null;
  private drawnCells: DrawnCell[] = [];
  private gridExtent: number[] | null = null;
  private lensKey = "";
  /** The lens the chart was last drawn with, so a swap can be told from what. */
  private shownLens: Lens | null = null;
  private worldKey = "";
  private settle: number | undefined;
  private onSettled: ((report: { x: number; y: number; zoom: number; rotation: number }) => void) | null = null;

  /** The host wires these once; the element never reaches for them itself. */
  attach(plane: DataPlane, settled: (report: { x: number; y: number; zoom: number; rotation: number }) => void): void {
    this.plane = plane;
    this.onSettled = settled;
  }

  /** Draw a world. Idempotent: the same context redraws nothing. */
  show(context: WorldContext): void {
    this.context = context;
    const worldKey = `${context.base}/${context.model.slug}`;
    const lensKey = `${worldKey}#${context.lens?.tiles ?? ""}@${context.lensIndex}`;
    if (worldKey !== this.worldKey) {
      this.build(context);
      this.watchSize();
      this.worldKey = worldKey;
      this.lensKey = "";
    }
    if (lensKey !== this.lensKey) {
      this.openLens(context, this.lensKey === "");
      this.lensKey = lensKey;
    }
    // Before the selection is followed, because a descend spends the view a
    // jump was holding and the card closing must not hand it back over the top
    // of the cell being flown to. The reference releases the hold and then
    // closes the card, in that order, for exactly this reason (grid.js,
    // selectGridCell); here the two are a swap apart -- the server closes the
    // card, the scene comes back with both facts in it -- so the order is kept
    // by reading the cell first.
    this.releaseOnDescend(context);
    this.follow(context);
    this.restyle();
    // The footer's own sentence. It is written here and on a camera event and
    // on a filter, and deliberately *not* from `restyle` -- holding Z restyles
    // and the reference never recounted for it, which on a pane put away
    // behind the sphere is the difference between the count it recorded and a
    // count of nothing.
    this.writeCount();
    // The first camera a volume ever has is the one the fit produced, and a
    // fit without an animation raises no `moveend` -- so without this the
    // opening view is the one camera the server is never told about, and the
    // reader's own first arrangement is the only one that reopens wrong.
    this.report();
  }

  /**
   * Keep the map the size of its pane.
   *
   * OpenLayers measures once and has to be told when the container moves, and
   * on this page the container moves without the window doing anything: the
   * panel beside the map comes out the first time a search has something to
   * say, and the sidebar folds under a keystroke. A map that did not hear
   * about it would draw at the old size and, worse, go on believing the whole
   * world still fits its window -- which is the question the corner locator
   * puts itself away over.
   */
  private watchSize(): void {
    if (this.sizes || typeof ResizeObserver === "undefined") return;
    this.sizes = new ResizeObserver(() => {
      // A pane measured mid-transition can be zero across, and telling
      // OpenLayers its map is zero pixels wide throws its size away. A pane
      // that is *put away* is a different case and its zero is the truth:
      // behind the sphere the chart has no window, and a chart with no window
      // has nothing in view and fits nothing -- which is what every recorded
      // globe step says once anything asks it again.
      if (!this.hidden && (!this.clientWidth || !this.clientHeight)) return;
      this.map?.updateSize();
      this.overview?.draw();
      // A window that changed size is over a different amount of what is
      // drawn -- unless there is no window, in which case the sentence on
      // screen is still the last true thing this pane said and the reader is
      // reading the sphere anyway.
      if (!this.hidden) this.writeCount();
    });
    this.sizes.observe(this);
  }

  /**
   * A cell the reader moved to retires the view a jump was holding.
   *
   * Descending is the reader saying where they want to be. Whatever panel jump
   * was holding a view for them is spent by that, and the loan has to be
   * cancelled *before* the selection is read: the same swap that carries the
   * new cell may carry a closed card, and a card closing gives the borrowed
   * view back — over the top of the cell about to be fitted.
   *
   * It reads `heldCell`, which `drawGrid` owns and updates; between here and
   * there nothing else looks at it, so the two halves of one scene see the same
   * answer to "did the cell move".
   */
  private releaseOnDescend(context: WorldContext): void {
    if (context.cell === this.heldCell) return;
    this.borrowed = null;
    this.landed = null;
  }

  /**
   * The camera's side of a selection, and the way back from it.
   *
   * A feature reached for from a panel is worth moving the camera for: the
   * reader asked for something they could not see. A feature clicked on the
   * canvas is already in front of them, and the pick goes through the same
   * route, so the test is whether the camera can already see it.
   *
   * The way back is the other half. A jump borrows the view, and closing the
   * card gives it back — unless the reader steered while the card was open,
   * in which case the view they steered to is the one they chose and the loan
   * is off. "Steered" is read off the camera rather than off the events: if
   * the camera is not where the jump put it, a hand moved it.
   */
  private follow(context: WorldContext): void {
    const selected = context.scene.selected;
    if (selected === this.selected) return;
    this.selected = selected;
    const view = this.view;
    if (!view) return;
    if (!selected) {
      const back = this.borrowed;
      this.borrowed = null;
      const standing = this.camera();
      // Where the jump left the camera, to a pixel: sub-pixel drift out of an
      // easing animation is not a reader's hand.
      const steered = !this.landed || !standing ||
        Math.abs(standing.zoom - this.landed.zoom) > 1e-3 ||
        Math.abs(standing.x - this.landed.x) > 0.5 ||
        Math.abs(standing.y - this.landed.y) > 0.5;
      this.landed = null;
      if (back && !steered) this.goTo(back.x, back.y, back.zoom, back.rotation);
      return;
    }
    const feature = context.model.feature(selected);
    if (!feature) return;
    const standing = this.camera();
    const lens = context.lens;
    if (!standing || !lens) return;
    // Each jump holds afresh, overwriting whatever the last one left: closing
    // a card undoes that card's own move and nothing older. Reading three rows
    // in turn steps back through them one view at a time, and none of the
    // three can hand back a place from before the reader went looking.
    this.borrowed = standing;
    if ("coordinate" in feature) {
      // A point is one place: go to it, and go no further in than the reader
      // already is.
      const at = feature.coordinate;
      const zoom = Math.min(viewMaxZoom(lens), Math.max(standing.zoom, FOCUS_ZOOM));
      view.animate({ center: [at[0], at[1]], zoom, duration: 220 });
      this.landed = { x: at[0], y: at[1], zoom, rotation: standing.rotation };
      return;
    }
    // Ground is an area, not a point, so the camera is fitted to it rather
    // than flown at its middle: a district reached for from an index is shown
    // whole, with room around it. A shape carrying no drawable geometry has no
    // extent to fit and the camera stays where it is.
    const extent = shapeExtent(feature);
    if (!extent) return;
    const landed = this.fitTo(extent, ZONE_FIT_PADDING, viewMaxZoom(lens));
    if (!landed) return;
    this.landed = { ...landed, rotation: standing.rotation };
  }

  /**
   * Fly the camera to an extent, and answer where it will land.
   *
   * The destination is wanted before the flight, because closing a card gives
   * back the view the jump borrowed only if the camera is still where the jump
   * put it — and an eased animation cannot be asked where it is going. So the
   * fit is made instantly, read, undone, and then flown: no frame is drawn in
   * between, and the answer is OpenLayers' own rather than a second
   * implementation of its arithmetic.
   */
  private fitTo(
    extent: readonly [number, number, number, number],
    padding: number,
    maxZoom: number,
  ): { x: number; y: number; zoom: number } | null {
    const view = this.view;
    if (!view) return null;
    const standing = this.camera();
    const resolution = view.getResolution();
    view.fit(extent as [number, number, number, number], {
      size: this.map?.getSize(),
      padding: [padding, padding, padding, padding],
      maxZoom,
      duration: 0,
    });
    const target = view.getCenter();
    const targetResolution = view.getResolution();
    const targetZoom = view.getZoom();
    if (!target || targetResolution === undefined || targetZoom === undefined) return null;
    if (standing && resolution !== undefined) {
      view.setCenter([standing.x, standing.y]);
      view.setResolution(resolution);
    }
    view.animate({ center: target, resolution: targetResolution, duration: 220 });
    return { x: target[0] ?? 0, y: target[1] ?? 0, zoom: targetZoom };
  }

  /** Restyle in place: a filter moved, or a selection, or the held key. */
  restyle(): void {
    const context = this.context;
    if (!context || !this.styles) return;
    this.styles.context.visibility = context.visibility;
    this.styles.context.scene = context.scene;
    this.styles.context.labelsHeld = context.labelsHeld;
    this.styles.context.hovered = context.hovered;
    this.styles.context.outset = context.outset;
    this.fillPriority(context);
    this.fillScrim(context);
    this.drawGrid(context);
    for (const source of Object.values(this.sources)) source.changed();
    this.map?.render();
    this.overview?.draw();
  }

  /**
   * The footer's sentence.
   *
   * "N of M features in view": M is what the map is drawing and the server
   * renders it, N is how many of them the window is actually over and nobody
   * but this pane can say -- it is a question about the camera. The
   * application leaves the element carrying its own half and this completes
   * it once there is a camera to ask (docs/app.md §6.1).
   *
   * It is one set counted twice, said as one sentence, and the number after
   * "of" is the same number the dock puts at the top of its list.
   */
  writeCount(): void {
    const footer = document.querySelector("#visible-count");
    const context = this.context;
    if (!footer || !context) return;
    const drawn = context.visibility.drawn;
    if (drawn === 0) {
      footer.textContent = "No features shown";
      return;
    }
    footer.textContent = `${this.inView(context)} of ${count(drawn)} features` +
      ` in view`;
  }

  /** How many of the standing features the window is over. */
  private inView(context: WorldContext): string {
    // The window, from whatever size the map has -- and a pane put away behind
    // the sphere has none at all. That is not a missing measurement to fall
    // back from: a chart nobody is looking through has nothing in view, which
    // is what every recorded globe step that recounts at all says.
    const extent = this.view?.calculateExtent(this.hidden ? [0, 0] : this.map?.getSize());
    if (!extent) return count(context.visibility.drawn);
    const [minX = 0, minY = 0, maxX = 0, maxY = 0] = extent;
    const inside = (x: number, y: number) =>
      x >= minX && x <= maxX && y >= minY && y <= maxY;
    let seen = 0;
    context.model.points.forEach((point, index) => {
      if (context.visibility.at(index).hidden) return;
      if (inside(point.coordinate[0], point.coordinate[1])) seen += 1;
    });
    // Ground is counted by whether the window is *over* it, not by whether a
    // corner of it happens to fall inside: a district big enough to fill the
    // screen has every one of its vertices off it, and a reader looking at
    // nothing but that district is looking at one feature rather than none.
    for (const shape of context.visibility.shapesShown) {
      const extent = shapeExtent(shape);
      if (!extent) continue;
      if (extent[0] <= maxX && extent[2] >= minX && extent[1] <= maxY && extent[3] >= minY) {
        seen += 1;
      }
    }
    return count(seen);
  }

  /** The camera, as the diagnostics and the globe both read it. */
  camera(): { x: number; y: number; zoom: number; rotation: number } | null {
    const view = this.view;
    const centre = view?.getCenter();
    const zoom = view?.getZoom();
    if (!view || !centre || zoom === undefined) return null;
    return { x: centre[0] ?? 0, y: centre[1] ?? 0, zoom, rotation: view.getRotation() };
  }

  /**
   * One step of a zoom control, held to the lens's own depth.
   *
   * A step, not a factor: the chart's zoom is a level and the buttons move it
   * by one, easing rather than cutting so a reader can see which way the
   * ground went. The floor and the ceiling are the lens's -- there is nothing
   * shallower than its first level and nothing deeper than two levels of
   * overzoom past its last.
   */
  nudgeZoom(delta: number): void {
    const view = this.view;
    const lens = this.context?.lens;
    if (!view || !lens) return;
    const standing = view.getZoom() ?? 0;
    view.animate({
      zoom: Math.min(Math.max(standing + delta, lens.minZoom), viewMaxZoom(lens)),
      duration: 140,
    });
  }

  /** Put the camera somewhere — the globe handing a view back, or a jump. */
  goTo(x: number, y: number, zoom: number, rotation = 0): void {
    this.view?.setCenter([x, y]);
    this.view?.setZoom(zoom);
    this.view?.setRotation(rotation);
  }

  /**
   * Redraw the corner locator.
   *
   * The shelf's `hidden` is this lane's answer written onto the application's
   * markup, so a swap that re-rendered the corner has just put a shelf back on
   * screen that the camera says has nothing to say. It is the same duty
   * `writeCount` does for the footer, and it is owed at the same moment.
   */
  redrawOverview(): void {
    this.overview?.draw();
  }

  /** Tiles since the lens was chosen. */
  get tileStats() {
    return this.counter.stats;
  }

  /** What the chart says about itself. */
  diagnostics(): ChartDiagnostics {
    const camera = this.camera();
    return {
      coordinateSystem: COORDINATE_SYSTEM,
      zoom: camera?.zoom ?? null,
      center: camera ? [camera.x, camera.y] : null,
      resolution: this.view?.getResolution() ?? null,
      fitZoom: this.fitZoom,
      grid: { extent: this.gridExtent, cells: this.drawnCells },
    };
  }

  /**
   * Put the locator's rectangle where another pane's camera is.
   *
   * The sphere has no OpenLayers view to ask, and the corner locator is the
   * one place its camera is ever written down — in whole pixels, on a surface
   * the chart shares. So the globe hands its extent here rather than moving
   * the chart underneath it, which would fetch tiles for a pane nobody is
   * looking at.
   */
  locate(extent: readonly number[] | null): void {
    if (!extent) this.overview?.release();
    this.overview?.draw(extent ?? undefined);
  }

  /** Force a synchronous frame, so a driver can read a settled camera. */
  renderSync(): void {
    // A map whose pane has no size has no frame to render, and asking for one
    // walks a null frame state. It happens for a moment whenever the chrome
    // moves -- a sidebar folding, a panel coming out -- and the harness's
    // settle loop forces a frame on every poll, so the moment is reachable.
    if (!this.map?.getSize()) return;
    this.map.renderSync();
  }

  /**
   * Come back to the page.
   *
   * A morph can take an element out and put the same one back, and everything
   * the disconnect gave up has to go back on when it does — the observer, and
   * the map's own target, which `setTarget(undefined)` took away.
   *
   * Only a *re*-connect has anything to rewire. On the first one there is no
   * map yet: the observer is wired with the world, from `show`, and wiring it
   * here as well would start measuring a pane before there is anything in it
   * to measure.
   */
  connectedCallback(): void {
    if (!this.map) return;
    this.map.setTarget(this);
    this.watchSize();
  }

  /**
   * Leave the page.
   *
   * The map lets go of its target, which is what stops OpenLayers drawing into
   * an element nobody can see. The observer has to be told separately: a
   * `ResizeObserver` is held by the browser, not by this element, so one left
   * running outlives the pane it was watching and keeps its callback — and the
   * map, the overview and the world context that callback closes over — alive
   * for as long as the page is open. The settle timer is the same fact in a
   * smaller form: a camera reported four hundred milliseconds after the pane
   * went away is a report about a world the reader has left.
   */
  disconnectedCallback(): void {
    this.sizes?.disconnect();
    this.sizes = null;
    if (this.settle !== undefined) clearTimeout(this.settle);
    this.settle = undefined;
    this.map?.setTarget(undefined);
  }

  // ---- building -------------------------------------------------------

  private build(context: WorldContext): void {
    this.map?.setTarget(undefined);
    this.projection = atlasProjection(context.grid);
    const styleContext: StyleContext = {
      visibility: context.visibility,
      scene: context.scene,
      labelsHeld: context.labelsHeld,
      hovered: context.hovered,
      outset: context.outset,
      iconURL: (asset) => this.plane?.iconURL(context.base, asset) ?? "",
    };
    this.styles = new Styles(styleContext);
    this.styles.learn(context.model.collections);

    this.view = this.viewFor(context);
    this.map = new OLMap({
      target: this,
      view: this.view,
      controls: defaultControls({ attribution: false, rotate: false, zoom: false }),
      layers: this.layers(),
    });
    this.fillFeatures(context);
    this.wire();
    this.overview = new Overview(this.map, () => this.context);
    log.info("a world is on the chart", {
      op: "render", volume: context.scene.volume, world: context.model.slug,
      points: context.model.points.length, shapes: context.model.shapes.length,
    });
  }

  private layers(): VectorLayer<VectorSource>[] {
    const eager = (
      source: VectorSource,
      zIndex: number,
      style: (feature: FeatureLike, resolution: number) => Style[] | Style | undefined,
      options: { declutter?: boolean; renderBuffer?: number } = {},
    ) => new VectorLayer({
      source,
      zIndex,
      style,
      updateWhileAnimating: true,
      updateWhileInteracting: true,
      renderBuffer: options.renderBuffer ?? 64,
      ...(options.declutter ? { declutter: true } : {}),
    });
    const grid = eager(this.sources.grid, 5, (f) => this.gridStyle(f));
    const gridContext = eager(this.sources.gridContext, 48, (f) => this.gridStyle(f));
    this.gridLayers = [grid, gridContext];
    return [
      grid,
      eager(this.sources.zoneScrim, 6, () => this.styles?.scrim()),
      eager(this.sources.zones, 10, (f, r) => this.shapeStyle(f, r)),
      eager(this.sources.zoneTitles, 20, (f) => this.titleStyle(f, false)),
      eager(this.sources.pins, 40, (f) => this.pinStyle(f, false)),
      eager(this.sources.pins, 42, (f) => this.pinStyle(f, true)),
      eager(this.sources.zoneTitles, 44, (f) => this.titleStyle(f, true)),
      eager(this.sources.pins, 45, (f) => this.labelStyle(f), { declutter: true, renderBuffer: 180 }),
      gridContext,
      eager(this.sources.priority, 50, (f) => this.priorityStyle(f), { renderBuffer: 220 }),
    ];
  }

  /**
   * The camera, built for one lens.
   *
   * Three of these options are not decoration and were the source of a long
   * hunt. **`resolutions`** is the ladder the pyramid actually has -- the
   * world square over the tile size, halved per level -- and a view given the
   * ladder converts between a zoom and a resolution by walking it, where a
   * view left to derive one from a maximum and a factor arrives at answers
   * that differ in their last bit. The baselines compare a camera exactly, so
   * the last bit is the difference between agreeing and not. **`extent`** and
   * **`showFullExtent`** hold the camera over the ground the lens drew: widen
   * the window -- fold the sidebar away -- and the view slides back inside
   * its own picture rather than panning off the edge of it, which is what
   * `sidebar-collapsed` records. And **`smoothResolutionConstraint: false`**
   * is what makes the two ends of the ladder ends rather than springs.
   *
   * It is rebuilt per lens because none of the three can be changed
   * afterwards, and a lens is a different picture of the ground.
   */
  private viewFor(context: WorldContext): View {
    const extent = lensExtent(context.lens, context.grid);
    const maxZoom = viewMaxZoom(context.lens);
    const base = context.grid.size / context.grid.tileSize;
    return new View({
      projection: this.projection ?? undefined,
      center: [
        ((extent[0] ?? 0) + (extent[2] ?? 0)) / 2,
        ((extent[1] ?? 0) + (extent[3] ?? 0)) / 2,
      ],
      resolutions: Array.from({ length: maxZoom + 1 }, (_, zoom) => base / 2 ** zoom),
      minZoom: 0,
      maxZoom,
      extent: extent as [number, number, number, number],
      constrainResolution: false,
      smoothResolutionConstraint: false,
      showFullExtent: true,
      zoom: 0,
      enableRotation: false,
    });
  }

  private openLens(context: WorldContext, fresh: boolean): void {
    if (!this.map || !this.view || !this.projection || !this.plane || !context.lens) return;
    if (this.raster) {
      this.map.removeLayer(this.raster.base);
      this.map.removeLayer(this.raster.detail);
    }
    this.counter.reset();
    this.raster = buildRaster(
      this.plane, context.base, context.lens, context.grid, this.projection, this.counter);
    this.map.addLayer(this.raster.base);
    this.map.addLayer(this.raster.detail);
    // A lens is a different picture of the ground, and three of the camera's
    // options cannot be changed after it is built, so it is rebuilt. The
    // camera itself is carried across by hand below.
    const standing = this.camera();
    const carried = fresh ? null : carryAcrossShards(this.shownLens, context.lens, standing);
    this.shownLens = context.lens;
    this.view = this.viewFor(context);
    this.map.setView(this.view);
    if (!fresh && standing) {
      this.goTo(standing.x, standing.y, standing.zoom, standing.rotation);
      // The layers of a split map are the same ground at different heights,
      // stacked down one sheet. Stepping between them leaves the reader over
      // the same place rather than at the same coordinates, which on a sheet
      // where each layer has its own box is a different point.
      if (carried) this.view.animate({ center: carried, duration: 200 });
    }

    const extent = lensExtent(context.lens, context.grid);
    const size = this.map.getSize() ?? [1, 1];
    // Shard crossing: swapping to a lens that draws another layer of the same
    // split world keeps the camera exactly where it was. The reader stepped
    // between floors of one building, not into another world, and a refit
    // would throw away the place they had found.
    if (fresh) {
      const camera = context.scene.camera;
      if (camera) this.goTo(camera.x, camera.y, camera.zoom, camera.rotation);
      else this.view.fit(extent, { size, nearest: false });
      // `fitZoom` is what "the whole map fits" is measured against, and it is
      // decided once, when the world opens: the zoom the reader arrived at,
      // whether that came from a fit or from the camera they left behind.
      // Recomputing it later is what left the corner locator arguing with a
      // camera that had moved on.
      this.fitZoom = this.view.getZoom() ?? null;
    }
    this.overview?.forget();
  }

  // ---- features -------------------------------------------------------

  private fillFeatures(context: WorldContext): void {
    this.sources.pins.clear();
    this.sources.zones.clear();
    this.sources.zoneTitles.clear();
    for (const point of context.model.points) {
      this.sources.pins.addFeature(new Feature({
        geometry: new Point(point.coordinate),
        record: point,
        priority: point.priority,
      }));
    }
    for (const shape of context.model.shapes) {
      this.sources.zones.addFeature(new Feature({
        geometry: shapeGeometry(shape), record: shape,
      }));
      const centre = shape.center ?? centreOf(shape);
      if (centre) {
        this.sources.zoneTitles.addFeature(new Feature({
          geometry: new Point(centre), record: shape,
        }));
      }
    }
  }

  private fillPriority(context: WorldContext): void {
    this.sources.priority.clear();
    context.model.points.forEach((point, index) => {
      if (!context.visibility.at(index).promoted) return;
      this.sources.priority.addFeature(new Feature({
        geometry: new Point(point.coordinate), record: point, priority: point.priority,
      }));
    });
  }

  /**
   * The scrim: one polygon over the whole world with the highlighted shapes
   * cut out of it, even-odd.
   *
   * Rings alternate direction with their depth so the fill counts its way in
   * and out: the world, then each piece of the shape as a hole, then any hole
   * of that piece's own back to solid. A ring's winding in a payload is
   * nobody's promise, so it is normalised here — and the depth that decides
   * it is the ring's place *within its own part*, which is why the parts
   * cannot be flattened first. A path zone has no ring to cut and is skipped:
   * the highlighted line draws above the dimming at its own weight.
   */
  private fillScrim(context: WorldContext): void {
    this.sources.zoneScrim.clear();
    const highlighted = context.visibility.highlightedShapes
      .filter((shape) => shape.kind === "area");
    if (!highlighted.length) return;
    const size = context.grid.size;
    const outer: [number, number][] = [[0, 0], [size, 0], [size, -size], [0, -size], [0, 0]];
    this.sources.zoneScrim.addFeature(new Feature({
      geometry: scrimGeometry(outer, highlighted),
    }));
  }

  private drawGrid(context: WorldContext): void {
    this.sources.grid.clear();
    this.sources.gridContext.clear();
    this.drawnCells = [];
    this.gridExtent = null;
    if (!context.system || !context.scene.gridSystem) return;
    const resolution = this.view?.getResolution() ?? 1;
    const standing = [...context.visibility.withoutCell()];
    const drawn = drawGrid(
      context.ground, context.system, context.cell, context.subgridVisible,
      resolution, standing, this.sources.grid, this.sources.gridContext);
    // The plan's own order is the contract; what is read back for the
    // diagnostics is the sources' order, chosen path first and context after,
    // which is the order the baselines record.
    this.drawnCells = [
      ...this.sources.grid.getFeatures(),
      ...this.sources.gridContext.getFeatures(),
    ].map((feature) => feature.get("gridCell") as DrawnCell);
    this.gridExtent = drawn.extent ? [...drawn.extent] : null;
    // Descending holds the cell: the camera is flown to the ground the
    // address names, the same way the navigator's own field does it. Only a
    // change of cell moves it -- redrawing the same grid because a filter
    // moved must not drag the reader back.
    //
    // THIS IS THE ONE FIT, and every way into a cell arrives at it: the
    // navigator's field, the back button, Escape, a click on the canvas and a
    // press on the sphere. The reference fitted synchronously inside
    // `selectGridCell` because the cell moved synchronously; here a cell moves
    // by a round trip, so the fit belongs to the arrival rather than to the
    // ask. A second fit fired off at the click would be the same arithmetic
    // said twice, racing its own confirmation and answering for a cell the
    // server had not yet agreed to -- and it is precisely the fit the parity
    // baselines pinned, so there is one of it.
    if (context.cell !== this.heldCell) {
      this.heldCell = context.cell;
      // Ascending is a move too: the reader asked for the ground one level
      // out, and the camera goes there the same way it came in.
      //
      // The size is whatever the map has, and a map put away behind the sphere
      // has none -- which OpenLayers reads as its own hundred-pixel default
      // and which the recorded tours are a reading of: a cell fitted into no
      // window at all lands at the deepest zoom the lens allows, over the
      // middle of the ground. Refusing to fit at all would leave the camera
      // somewhere the reference never left it.
      if (this.gridExtent && this.view && context.lens) {
        this.view.fit(this.gridExtent as [number, number, number, number], {
          size: this.map?.getSize(),
          padding: [GRID_FIT_PADDING, GRID_FIT_PADDING, GRID_FIT_PADDING, GRID_FIT_PADDING],
          maxZoom: viewMaxZoom(context.lens), nearest: false, duration: 180,
        });
      }
    }
  }

  // ---- styles ---------------------------------------------------------

  private standingOf(feature: FeatureLike): { record: PointRecord; promoted: boolean } | null {
    const record = feature.get("record") as PointRecord | undefined;
    const context = this.context;
    if (!record || !context) return null;
    const standing = context.visibility.at(record.index);
    if (standing.hidden) return null;
    return { record, promoted: standing.promoted };
  }

  private pinStyle(feature: FeatureLike, zoneLayer: boolean): Style[] | undefined {
    const found = this.standingOf(feature);
    if (!found || !this.styles || !this.context) return undefined;
    const claimed = this.context.visibility.at(found.record.index).passesHighlights;
    // The same markers twice: once under the ground, once over it for the
    // ones a highlight claimed, so a claimed pin is never lost under the
    // shape that claimed it.
    if (zoneLayer !== claimed) return undefined;
    if (found.promoted) return undefined;
    if (this.context.scene.hidden.has(String(found.record.collection.id))) return undefined;
    return this.styles.pin(found.record, false);
  }

  private labelStyle(feature: FeatureLike): Style | undefined {
    const found = this.standingOf(feature);
    if (!found || !this.styles || found.promoted) return undefined;
    return this.styles.pinLabel(found.record, false) ?? undefined;
  }

  private priorityStyle(feature: FeatureLike): Style[] | undefined {
    const found = this.standingOf(feature);
    if (!found || !this.styles) return undefined;
    const marks = this.styles.pin(found.record, true);
    const label = this.styles.pinLabel(found.record, true);
    return label ? [...marks, label] : marks;
  }

  private shapeStyle(feature: FeatureLike, resolution: number): Style[] | undefined {
    const record = feature.get("record") as ShapeRecord | undefined;
    const context = this.context;
    if (!record || !context || !this.styles) return undefined;
    if (!context.visibility.shapesShown.includes(record)) return undefined;
    const highlighted = context.scene.highlighted.has(record.id) ||
      context.scene.selected === record.id;
    return record.kind === "path"
      ? this.styles.path(record, resolution, highlighted)
      : [this.styles.area(record, highlighted)];
  }

  private titleStyle(feature: FeatureLike, detail: boolean): Style | undefined {
    const record = feature.get("record") as ShapeRecord | undefined;
    const context = this.context;
    if (!record || !context || !this.styles) return undefined;
    if (!context.visibility.shapesShown.includes(record)) return undefined;
    const promoted = context.scene.highlighted.has(record.id) ||
      context.scene.selected === record.id;
    if (promoted !== detail) return undefined;
    return this.styles.areaTitle(record, promoted) ?? undefined;
  }

  private gridStyle(feature: FeatureLike): Style[] | undefined {
    const visual = feature.get("gridVisual") as CellVisual | null | undefined;
    if (!visual || !this.styles) return undefined;
    return this.styles.grid(visual);
  }

  // ---- interaction ----------------------------------------------------

  private wire(): void {
    const map = this.map;
    if (!map) return;
    map.on("pointermove", (event) => {
      if (event.dragging || !this.context) return;
      // The hover is about features and never about cells: what it draws is a
      // marker lifting under the pointer, and a cell has no such reading.
      const found = this.hitFeature(event.pixel);
      const id = found?.id ?? null;
      if (id === this.context.hovered) return;
      this.context.hovered = id;
      this.restyle();
      this.setAttribute("data-hovered", id ?? "");
    });
    map.on("singleclick", (event) => {
      const found = this.hit(event.pixel);
      // A click on nothing is not a pick. The hover above is untouched by it:
      // leaving a feature still clears the cursor, because that is continuous
      // state this pane owns, and a selection is not.
      if (!found) return;
      // The seam resolves the hit; what it resolved to is the application's to
      // act on, and it acts on it through the forms the page renders for
      // exactly this (data/report.ts, issue #5 §4.4). Two answers, two forms,
      // two concerns -- a selection and a place.
      if (found.kind === "cell") {
        reportGridPick(found.cell);
        return;
      }
      reportPick({ feature: found.id, kind: found.kind });
    });
    map.on("moveend", () => {
      this.report();
      // The corner locator answers two questions about the camera -- where it
      // is, and whether it can see the whole map at once -- and both of them
      // are this event's. Redrawing it only when a filter moved left the
      // shelf put away across a whole zoom: the map no longer fitted, and
      // nothing had told the corner so.
      this.overview?.draw();
      // The window moved, so how much of what is drawn it is over has moved
      // with it. The count is the camera's answer and belongs to the camera's
      // own event, not to a filter's -- and a pane with no window has no
      // answer to give.
      if (!this.hidden) this.writeCount();
    });
  }

  /**
   * What is under a pixel: a cell first, then a feature.
   *
   * THE ORDER IS THE WHOLE OF IT. While a grid is up the reader is
   * telescoping, and a click is a request to go one level in -- so a cell
   * answers before the pins standing on it are consulted at all, and a cell
   * that answered returns without asking. Reversed, the grid would be
   * undescendable anywhere a pin happened to be, which is everywhere worth
   * descending to.
   */
  private hit(pixel: number[]): Pick | null {
    return this.hitCell(pixel) ?? this.hitFeature(pixel);
  }

  /**
   * The cell under a pixel, or nothing.
   *
   * TWO RULES, AND BOTH ARE THE REFERENCE'S OWN.
   *
   * ONLY THE GRID'S OWN LAYERS ARE ASKED, which is what a hit test over a
   * dozen layers cannot say for itself: everything else on the map is a
   * feature and answers the second question, not this one.
   *
   * AND ONLY A NEIGHBOUR OR A CHILD IS AN ANSWER. The cell the reader is
   * already inside is drawn over its own children -- as an outline while the
   * subdivision is up, and as the whole of the grid when the telescope has
   * bottomed out -- so a plan that answered for `scope` or `leaf` would hand
   * back the address already held on every click, and the grid could not be
   * descended at all. A neighbour is a step sideways and a child is a step in;
   * those are the two moves a click on a grid means.
   *
   * The tolerance is one pixel rather than the four a marker gets. A cell is a
   * region and the reader is pointing *into* it; slack around a boundary would
   * only make the boundary ambiguous, and a boundary that cannot be told apart
   * from the cell beyond it is a grid nobody can steer by.
   */
  private hitCell(pixel: number[]): CellPick | null {
    const map = this.map;
    const context = this.context;
    if (!map || !context?.system || !context.scene.gridSystem) return null;
    let found: CellPick | null = null;
    map.forEachFeatureAtPixel(pixel, (feature) => {
      const cell = feature.get("gridCell") as DrawnCell | undefined;
      if (!cell || (cell.role !== "neighbor" && cell.role !== "child")) return false;
      found = { kind: "cell", cell: cell.hash };
      return true;
    }, {
      hitTolerance: 1,
      layerFilter: (layer) => this.gridLayers.some((one) => one === layer),
    });
    return found;
  }

  private hitFeature(pixel: number[]): FeaturePick | null {
    const map = this.map;
    const context = this.context;
    if (!map || !context) return null;
    let found: FeaturePick | null = null;
    map.forEachFeatureAtPixel(pixel, (feature) => {
      const record = feature.get("record") as PointRecord | ShapeRecord | undefined;
      if (!record) return false;
      if ("index" in record) {
        if (context.visibility.at(record.index).hidden) return false;
        found = { id: record.id, kind: "point" };
        return true;
      }
      if (!context.visibility.shapesShown.includes(record)) return false;
      found = { id: record.id, kind: record.kind };
      return true;
    }, { hitTolerance: 4 });
    return found;
  }

  /**
   * The camera's one whisper upward.
   *
   * Debounced, because a settling camera that posted every frame would fight
   * the reader's own hand, and answered with `204` so nothing swaps under
   * them (docs/app.md §4.3).
   */
  private report(): void {
    // A pane put away reports nothing. Where the reader left off is where they
    // were looking, and behind the sphere the chart is not it: its camera
    // there is whatever a fit into a window of no size produced, and saving
    // that would reopen the volume somewhere nobody ever stood.
    if (this.hidden) return;
    if (this.settle !== undefined) clearTimeout(this.settle);
    this.settle = setTimeout(() => {
      const camera = this.camera();
      if (camera && this.onSettled) this.onSettled(camera);
    }, 400) as unknown as number;
  }
}

/** A ring or a run, copied out of the model so nothing downstream edits it. */
function copyLine(line: readonly (readonly [number, number])[]): [number, number][] {
  return line.map((point) => [point[0], point[1]] as [number, number]);
}

/**
 * One shape's drawn geometry: every part, kept apart.
 *
 * A district is one feature and may be a dozen separate pieces of ground; a
 * trail is one feature and may be fifty runs of it. Both are multi-part by
 * nature, so both are drawn by a multi-part geometry — a single `Polygon`
 * reads its first ring as the exterior and every later one as a hole, so the
 * second piece of ground would be punched out of the first, and a single
 * `LineString` has room for one run and silently drops the other forty-nine.
 */
export function shapeGeometry(shape: ShapeRecord): MultiPolygon | MultiLineString {
  return shape.kind === "area"
    ? new MultiPolygon(parts(shape))
    : new MultiLineString(shape.lines.map(copyLine));
}

/**
 * An area's parts, nested the way a polygon is: each part its own exterior
 * followed by that part's own interior rings, and nobody else's.
 *
 * The model already keeps them apart — `holes[i]` belongs to `lines[i]` — and
 * this is where that survives into the drawing. Flattened into one ring list,
 * a two-piece district reads as one piece with the other punched out of it.
 */
export function parts(shape: ShapeRecord): [number, number][][][] {
  return shape.lines.map((line, index) => [
    copyLine(line),
    ...(shape.holes[index] ?? []).map(copyLine),
  ]);
}

/**
 * The scrim: the world, then every highlighted area's parts cut out of it,
 * each part's interior rings wound back to solid.
 */
export function scrimGeometry(
  world: readonly (readonly [number, number])[],
  highlighted: readonly ShapeRecord[],
): Polygon {
  const rings: [number, number][][] = [wind(world, true)];
  for (const shape of highlighted) {
    // A path zone has no ring to cut from the dimming.
    if (shape.kind !== "area") continue;
    for (const part of parts(shape)) {
      part.forEach((ring, index) => rings.push(wind(ring, index > 0)));
    }
  }
  return new Polygon(rings);
}

/**
 * Where a shape's name is anchored when the payload did not say.
 *
 * The middle of the union of every part's extent — outer rings are enough,
 * because a hole is inside its own ring and cannot widen the box. It is a
 * question about the model rather than about the drawing, so splitting the
 * drawn geometry into parts does not move it by a bit.
 */
export function centreOf(shape: ShapeRecord): [number, number] | null {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const line of shape.lines) {
    for (const [x, y] of line) {
      minX = Math.min(minX, x); maxX = Math.max(maxX, x);
      minY = Math.min(minY, y); maxY = Math.max(maxY, y);
    }
  }
  if (!Number.isFinite(minX)) return null;
  return [(minX + maxX) / 2, (minY + maxY) / 2];
}

/** A ring wound the way a fill rule needs it: counter-clockwise, or not. */
function wind(line: readonly (readonly [number, number])[], counter: boolean): [number, number][] {
  let area = 0;
  for (let i = 1; i < line.length; i++) {
    const a = line[i - 1];
    const b = line[i];
    if (!a || !b) continue;
    area += (b[0] - a[0]) * (b[1] + a[1]);
  }
  const copy = line.map((point) => [point[0], point[1]] as [number, number]);
  return (area < 0) === counter ? copy : copy.reverse();
}

/** A count as the chrome writes it: thousands separated, as the goldens read. */
function count(value: number): string {
  return value.toLocaleString("en-US");
}
