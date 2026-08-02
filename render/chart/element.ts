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
import LineString from "ol/geom/LineString.js";
import VectorLayer from "ol/layer/Vector.js";
import VectorSource from "ol/source/Vector.js";
import Style from "ol/style/Style.js";
import { defaults as defaultControls } from "ol/control/defaults.js";
import type { FeatureLike } from "ol/Feature.js";
import type Projection from "ol/proj/Projection.js";
import type { CellVisual } from "@atlas/analysis";

import { logger } from "../log.ts";
import type { DataPlane } from "../data/plane.ts";
import type { WorldContext } from "../context.ts";
import type { PointRecord, ShapeRecord } from "../world/model.ts";
import { COORDINATE_SYSTEM, atlasProjection, fitResolution, lensExtent, viewMaxZoom } from "./projection.ts";
import { TileCounter, buildRaster } from "./raster.ts";
import type { Raster } from "./raster.ts";
import { Styles } from "./styles.ts";
import type { StyleContext } from "./styles.ts";
import { drawGrid } from "./grid.ts";
import type { DrawnCell } from "./grid.ts";
import { Overview } from "./overview.ts";

const log = logger("chart");

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
  private styles: Styles | null = null;
  private context: WorldContext | null = null;
  private plane: DataPlane | null = null;
  private overview: Overview | null = null;
  private fitZoom: number | null = null;
  private drawnCells: DrawnCell[] = [];
  private gridExtent: number[] | null = null;
  private lensKey = "";
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
      this.worldKey = worldKey;
      this.lensKey = "";
    }
    if (lensKey !== this.lensKey) {
      this.openLens(context, this.lensKey === "");
      this.lensKey = lensKey;
    }
    this.restyle();
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
    this.writeCount();
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
  private writeCount(): void {
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
    const size = this.map?.getSize();
    const extent = size ? this.view?.calculateExtent(size) : null;
    if (!extent) return count(context.visibility.drawn);
    const [minX = 0, minY = 0, maxX = 0, maxY = 0] = extent;
    const inside = (x: number, y: number) =>
      x >= minX && x <= maxX && y >= minY && y <= maxY;
    let seen = 0;
    context.model.points.forEach((point, index) => {
      if (context.visibility.at(index).hidden) return;
      if (inside(point.coordinate[0], point.coordinate[1])) seen += 1;
    });
    for (const shape of context.visibility.shapesShown) {
      if (shape.lines.some((line) => line.some(([x, y]) => inside(x, y)))) seen += 1;
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
    this.overview?.draw(extent ?? undefined);
  }

  /** Force a synchronous frame, so a driver can read a settled camera. */
  renderSync(): void {
    this.map?.renderSync();
  }

  disconnectedCallback(): void {
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

    this.view = new View({
      projection: this.projection,
      center: [context.grid.size / 2, -context.grid.size / 2],
      zoom: 0,
      maxZoom: viewMaxZoom(context.lens),
      constrainResolution: false,
      enableRotation: false,
    });
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
    return [
      eager(this.sources.grid, 5, (f) => this.gridStyle(f)),
      eager(this.sources.zoneScrim, 6, () => this.styles?.scrim()),
      eager(this.sources.zones, 10, (f, r) => this.shapeStyle(f, r)),
      eager(this.sources.zoneTitles, 20, (f) => this.titleStyle(f, false)),
      eager(this.sources.pins, 40, (f) => this.pinStyle(f, false)),
      eager(this.sources.pins, 42, (f) => this.pinStyle(f, true)),
      eager(this.sources.zoneTitles, 44, (f) => this.titleStyle(f, true)),
      eager(this.sources.pins, 45, (f) => this.labelStyle(f), { declutter: true, renderBuffer: 180 }),
      eager(this.sources.gridContext, 48, (f) => this.gridStyle(f)),
      eager(this.sources.priority, 50, (f) => this.priorityStyle(f), { renderBuffer: 220 }),
    ];
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
    this.view.setMaxZoom(viewMaxZoom(context.lens));

    const extent = lensExtent(context.lens, context.grid);
    const size = this.map.getSize() ?? [1, 1];
    this.fitZoom = this.view.getZoomForResolution(
      fitResolution(extent, size[0] ?? 1, size[1] ?? 1)) ?? null;
    // Shard crossing: swapping to a lens that draws another layer of the same
    // split world keeps the camera exactly where it was. The reader stepped
    // between floors of one building, not into another world, and a refit
    // would throw away the place they had found.
    if (fresh) {
      const camera = context.scene.camera;
      if (camera) this.goTo(camera.x, camera.y, camera.zoom, camera.rotation);
      else this.view.fit(extent, { size, nearest: false });
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
      const geometry = shape.kind === "area"
        ? new Polygon(rings(shape))
        : new LineString(shape.lines[0] ?? []);
      this.sources.zones.addFeature(new Feature({ geometry, record: shape }));
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
   * The cut-outs are wound against the outer ring so the fill rule leaves a
   * hole rather than a second layer of dimming — a ring's winding in a
   * payload is nobody's promise, so it is normalised here.
   */
  private fillScrim(context: WorldContext): void {
    this.sources.zoneScrim.clear();
    const highlighted = context.visibility.highlightedShapes
      .filter((shape) => shape.kind === "area");
    if (!highlighted.length) return;
    const size = context.grid.size;
    const outer: [number, number][] = [[0, 0], [size, 0], [size, -size], [0, -size], [0, 0]];
    const holes = highlighted.flatMap((shape) => shape.lines.map((line) => wind(line, false)));
    this.sources.zoneScrim.addFeature(new Feature({
      geometry: new Polygon([wind(outer, true), ...holes]),
    }));
  }

  private drawGrid(context: WorldContext): void {
    this.sources.grid.clear();
    this.sources.gridContext.clear();
    this.drawnCells = [];
    this.gridExtent = null;
    if (!context.system || !context.scene.gridSystem) return;
    const resolution = this.view?.getResolution() ?? 1;
    const standing = [...context.visibility.standing()];
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
      const found = this.hit(event.pixel);
      const id = found?.id ?? null;
      if (id === this.context.hovered) return;
      this.context.hovered = id;
      this.restyle();
      this.setAttribute("data-hovered", id ?? "");
    });
    map.on("singleclick", (event) => {
      const found = this.hit(event.pixel);
      // The seam resolves the hit; the identity is the application's to act
      // on. Composed and bubbling, so the page's one glue listener can hear
      // it from outside the element (issue #5 §4.4).
      this.dispatchEvent(new CustomEvent("atlas:pick", {
        bubbles: true, composed: true,
        detail: { feature: found?.id ?? "", kind: found?.kind ?? "" },
      }));
    });
    map.on("moveend", () => {
      this.report();
      // The window moved, so how much of what is drawn it is over has moved
      // with it. The count is the camera's answer and belongs to the camera's
      // own event, not to a filter's.
      this.writeCount();
    });
  }

  private hit(pixel: number[]): { id: string; kind: string } | null {
    const map = this.map;
    const context = this.context;
    if (!map || !context) return null;
    let found: { id: string; kind: string } | null = null;
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
    if (this.settle !== undefined) clearTimeout(this.settle);
    this.settle = setTimeout(() => {
      const camera = this.camera();
      if (camera && this.onSettled) this.onSettled(camera);
    }, 400) as unknown as number;
  }
}

function rings(shape: ShapeRecord): [number, number][][] {
  const out: [number, number][][] = [];
  shape.lines.forEach((line, index) => {
    out.push(line.map((point) => [point[0], point[1]] as [number, number]));
    for (const hole of shape.holes[index] ?? []) {
      out.push(hole.map((point) => [point[0], point[1]] as [number, number]));
    }
  });
  return out;
}

function centreOf(shape: ShapeRecord): [number, number] | null {
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
