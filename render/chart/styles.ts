// How the chart draws what it draws.
//
// One class holds every mark the chart makes, because the marks answer each
// other: a pin's label is the pin's size away from it, a scrim is the exact
// complement of the shapes it dims, and a promoted pin is the same pin drawn
// again in a layer above. Splitting them across files would split decisions
// that have to agree.
//
// The grid is the one thing here that is NOT decided here. Its colours,
// weights, alphas and chip are `gridCellVisual`'s, from the analysis lane,
// and this module adapts those tokens into `ol/style` without adding one of
// its own — which is what lets the chart and the globe read as one instrument
// (issue #5 §5.4: renderers adapt tokens; no renderer owns a cell rule).

import Circle from "ol/style/Circle.js";
import Fill from "ol/style/Fill.js";
import Icon from "ol/style/Icon.js";
import Stroke from "ol/style/Stroke.js";
import Style from "ol/style/Style.js";
import Text from "ol/style/Text.js";
import Point from "ol/geom/Point.js";
import type { FeatureLike } from "ol/Feature.js";
import { gridTheme, paletteColor } from "@atlas/analysis";
import type { CellVisual } from "@atlas/analysis";
import type { Collection } from "../data/payload.ts";
import { labelPolicy, renderAs } from "../data/payload.ts";
import type { PointRecord, ShapeRecord } from "../world/model.ts";
import type { Visibility } from "../world/visibility.ts";
import type { Scene } from "../scene/read.ts";

/** The rim a world's markers wear to stay legible against its art. */
export const OUTSET_COLORS: Readonly<Record<string, string>> = {
  light: "rgba(255, 255, 255, 0.96)",
  dark: "rgba(7, 9, 7, 0.98)",
};

const LABEL_FONT = "600 11px ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif";
const TITLE_FONT = "600 12px ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif";

/** Everything a style needs to know that is not on the feature itself. */
export interface StyleContext {
  visibility: Visibility;
  scene: Scene;
  labelsHeld: boolean;
  hovered: string | null;
  outset: string;
  iconURL(asset: string): string;
}

/** The colour a collection wears everywhere it is drawn. */
export function collectionColor(collection: Collection, ordinal: number): string {
  return collection.color || collection.iconColor || paletteColor(ordinal);
}

/**
 * The marks, and the state they read.
 *
 * The context is mutated in place by the chart rather than rebuilt, because
 * every style function closes over it: a filter that lands has to be visible
 * to marks already handed to OpenLayers.
 */
export class Styles {
  private readonly icons = new Map<number, Icon | null>();
  private readonly colors = new Map<number, string>();

  readonly context: StyleContext;

  constructor(context: StyleContext) {
    this.context = context;
  }

  /** Remember the palette ordinal of every collection, in payload order. */
  learn(collections: readonly Collection[]): void {
    this.icons.clear();
    this.colors.clear();
    collections.forEach((collection, ordinal) => {
      this.colors.set(collection.id, collectionColor(collection, ordinal));
    });
  }

  color(collection: Collection): string {
    return this.colors.get(collection.id) ?? paletteColor(collection.id);
  }

  private icon(collection: Collection): Icon | null {
    const held = this.icons.get(collection.id);
    if (held !== undefined) return held;
    let built: Icon | null = null;
    if (collection.iconAsset) {
      // A glyph is a monochrome mark the reader tints; a picture is drawn as
      // it was authored. `atlas.icon.kind` names what a file suffix used to
      // imply, and `iconPicture` is the manifest's own copy of the same fact.
      const picture = collection.iconPicture ||
        collection.attrs?.["atlas.icon.kind"] === "picture";
      built = new Icon({
        src: this.context.iconURL(collection.iconAsset),
        ...(picture ? {} : { color: this.color(collection) }),
        width: 15,
        height: 15,
        declutterMode: "none",
      });
    }
    this.icons.set(collection.id, built);
    return built;
  }

  /** The pin itself: a rim, a disc, and the collection's mark over it. */
  pin(point: PointRecord, promoted: boolean): Style[] {
    const color = this.color(point.collection);
    const selected = point.id === this.context.scene.selected;
    const hovered = point.id === this.context.hovered;
    const radius = selected ? 9 : hovered ? 8 : 7;
    const marks: Style[] = [
      new Style({
        image: new Circle({
          radius,
          fill: new Fill({ color }),
          stroke: new Stroke({ color: this.context.outset, width: selected ? 2.5 : 1.5 }),
        }),
        zIndex: promoted ? 20_000_000 : point.priority,
      }),
    ];
    const icon = this.icon(point.collection);
    if (icon) marks.push(new Style({ image: icon, zIndex: promoted ? 20_000_001 : point.priority }));
    return marks;
  }

  /** A point collection curated as text draws its name and no marker. */
  textSymbol(point: PointRecord): Style {
    return new Style({
      text: new Text({
        text: point.title,
        font: TITLE_FONT,
        fill: new Fill({ color: this.color(point.collection) }),
        stroke: new Stroke({ color: this.context.outset, width: 3 }),
        overflow: true,
      }),
      zIndex: point.priority,
    });
  }

  /**
   * The name beside a pin.
   *
   * A pin collection's names are optional furniture: they appear while Z is
   * held, for the pin the reader is pointing at, for the selected one, and
   * for whatever a search or a held cell promoted. That is the whole of the
   * label ladder's drawing side — which names are *offered* is the
   * application's decision and arrives in the scene.
   */
  pinLabel(point: PointRecord, promoted: boolean): Style | null {
    const wanted = this.context.labelsHeld || promoted ||
      renderAs(point.collection) === "text";
    if (!wanted || !point.title) return null;
    return new Style({
      text: new Text({
        text: point.title,
        font: LABEL_FONT,
        offsetY: 16,
        fill: new Fill({ color: "#f2f5f9" }),
        stroke: new Stroke({ color: "rgba(6, 9, 14, 0.86)", width: 3 }),
        overflow: true,
        // Declutter is priority-ordered: a name that has to give way gives
        // way to a rarer collection's, never to whichever was built first.
        // The promoted layer is not decluttered at all, which is the
        // selected/searched bypass.
        declutterMode: promoted ? "none" : "declutter",
      }),
      zIndex: promoted ? 9_100_000 : point.priority,
    });
  }

  /** A path drawn at its declared ground width, never thinner than a hair. */
  path(shape: ShapeRecord, resolution: number, highlighted: boolean): Style[] {
    const color = this.color(shape.collection);
    const ground = Number(shape.collection.attrs?.["atlas.stroke.width_px"] ?? 0);
    const width = Math.max(1.4, ground > 0 ? ground / resolution : 2);
    return [
      new Style({
        stroke: new Stroke({ color: "rgba(6, 9, 14, 0.5)", width: width + 2 }),
        zIndex: 9,
      }),
      new Style({
        stroke: new Stroke({ color, width, lineCap: "round", lineJoin: "round" }),
        zIndex: highlighted ? 11 : 10,
      }),
    ];
  }

  /** An area: its own accent, faintly filled, brighter when it is highlighted. */
  area(shape: ShapeRecord, highlighted: boolean): Style {
    const color = this.color(shape.collection);
    return new Style({
      fill: new Fill({ color: withAlpha(color, highlighted ? 0.28 : 0.1) }),
      stroke: new Stroke({ color, width: highlighted ? 2.4 : 1.4 }),
      zIndex: highlighted ? 11 : 10,
    });
  }

  /**
   * The scrim: everything the highlights did not claim, dimmed.
   *
   * It is drawn UNDER the shapes and over the raster, because the dimming is
   * something the map is seen through, not something painted on top of the
   * ground that was asked for.
   */
  scrim(): Style {
    return new Style({ fill: new Fill({ color: "rgba(4, 7, 14, 0.55)" }), zIndex: 6 });
  }

  /**
   * An area's name on the map.
   *
   * `atlas.label.policy` is the curation — `always`, or `quiet`, which means
   * only on highlight, selection, or an explicit reveal — and the reader's own
   * override arrives in the scene and wins. Holding Z reveals what is
   * optional and never revives what someone chose to quiet, which is the one
   * asymmetry in the ladder and the reason the override is consulted first.
   */
  areaTitle(shape: ShapeRecord, promoted: boolean): Style | null {
    if (!shape.title) return null;
    const override = this.context.scene.overrides.get(String(shape.collection.id));
    const policy = override ?? labelPolicy(shape.collection);
    const speaking = shape.kind === "area" && policy === "always";
    const revealed = promoted || (this.context.labelsHeld && override !== "quiet");
    if (!speaking && !revealed) return null;
    return new Style({
      text: new Text({
        text: shape.title,
        font: TITLE_FONT,
        fill: new Fill({ color: "#e8eef6" }),
        stroke: new Stroke({ color: "rgba(6, 9, 14, 0.8)", width: 3 }),
        overflow: true,
        declutterMode: promoted ? "none" : "declutter",
      }),
      zIndex: promoted ? 9_100_000 : 20,
    });
  }

  /** One grid cell, adapted from the analysis lane's tokens and nothing else. */
  grid(visual: CellVisual): Style[] {
    const marks: Style[] = [
      new Style({
        stroke: new Stroke({
          color: withAlpha(visual.line.color, visual.line.opacity),
          width: visual.line.widthPx,
        }),
        ...(visual.fill
          ? { fill: new Fill({ color: withAlpha(visual.fill.color, visual.fill.opacity) }) }
          : {}),
      }),
    ];
    if (visual.label) {
      const { prefix, final, color, textAlpha, chip, sizePx } = visual.label;
      marks.push(new Style({
        text: new Text({
          text: `${prefix}${final}`,
          font: `600 ${sizePx}px ${gridTheme.labelFont}`,
          textAlign: "right",
          textBaseline: "bottom",
          offsetX: -gridTheme.labelInsetPx,
          offsetY: -gridTheme.labelInsetPx,
          fill: new Fill({ color: withAlpha(color, textAlpha) }),
          backgroundFill: new Fill({ color: chip }),
          padding: [2, 4, 2, 4],
          declutterMode: "none",
        }),
        // The chip belongs in the cell's bottom-right corner, which is the
        // bounding-box convention the label placement below relies on.
        geometry: cornerOf,
      }));
    }
    return marks;
  }
}

/** A CSS colour at an alpha, whether it arrived as hex or as rgb(). */
export function withAlpha(color: string, alpha: number): string {
  if (alpha >= 1) return color;
  if (color.startsWith("#") && (color.length === 7 || color.length === 4)) {
    const full = color.length === 4
      ? `#${color[1]}${color[1]}${color[2]}${color[2]}${color[3]}${color[3]}`
      : color;
    const r = Number.parseInt(full.slice(1, 3), 16);
    const g = Number.parseInt(full.slice(3, 5), 16);
    const b = Number.parseInt(full.slice(5, 7), 16);
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  }
  if (color.startsWith("rgba(")) return color;
  if (color.startsWith("rgb(")) return `rgba(${color.slice(4, -1)}, ${alpha})`;
  return color;
}

/**
 * The bottom-right corner of a cell, where its chip sits.
 *
 * The corner is taken from the drawn geometry's own extent, so a cell the
 * antimeridian cut into two pieces carries a chip on each piece rather than
 * one chip halfway between them.
 */
function cornerOf(feature: FeatureLike): Point | undefined {
  const geometry = feature.getGeometry();
  if (!geometry) return undefined;
  const [, minimumY = 0, maximumX = 0] = geometry.getExtent();
  return new Point([maximumX, minimumY]);
}
