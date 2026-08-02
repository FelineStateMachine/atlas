import Point from "ol/geom/Point.js";
import {
  Fill,
  Icon,
  Stroke,
  Style,
  Text,
} from "ol/style.js";

import { anyShapeCollectionVisible, collectionFor, collectionOf, isCollectionHidden } from "./collections.js";
import { gridTheme } from "./constants.js";
import { gridCellVisual } from "./grid.js";
import { labelPolicy, labelSilenced } from "./semconv.js";
import { state } from "./state.js";
import {
  atMaximumNativeZoom,
  isPriorityPin,
  pinIsHidden,
} from "./features.js";
import {
  categoryColor,
  hexToRGBA,
  iconOutsetColor,
  iconURL,
  initials,
} from "./theme.js";

export function featureOrder(left, right) {
  return (left.get("priority") || 0) - (right.get("priority") || 0);
}

export function pinFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.passesZoneFilters || isPriorityPin(pin)) return null;
  return markerStyles(pin, false);
}

export function zonePinFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || !state.highlightedZones.size || !pin.passesZoneFilters ||
      pinIsHidden(pin) || isPriorityPin(pin)) {
    return null;
  }
  return markerStyles(pin, false);
}

// A pin's label draws when its collection's names are spoken -- curated
// always, or the reader's toggle -- or while Z is held. The key reveals what
// is merely optional, never what the reader silenced by hand, and the zoom
// has no say either way: what was asked for is every name at once.
export function pinLabelFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin)) return null;
  if (labelPolicy(null, pin.category) !== "always" &&
      !(state.labelsHeld && !labelSilenced(pin.category))) {
    return null;
  }
  return markerLabelStyle(pin);
}

export function priorityFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin)) return null;
  const marker = markerStyles(pin, pin === state.selectedPin);
  if (pin === state.hoveredPin || pin === state.selectedPin) {
    return [marker, markerLabelStyle(pin)];
  }
  return marker;
}

export function markerStyles(pin, selected) {
  const key = `marker:${pin.category.id}:${selected ? 1 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const color = categoryColor(pin.category);
  const zIndex = selected ? 20_000_000 : pin.priority;
  const renderedIcon = state.markerIcons.get(markerIconKey(pin.category));
  let style;
  if (renderedIcon) {
    style = new Style({
      image: new Icon({
        src: renderedIcon,
        width: selected ? 36 : 31,
        height: selected ? 36 : 31,
      }),
      zIndex,
    });
  } else {
    style = new Style({
      text: new Text({
        text: initials(pin.category.title),
        font: `900 ${selected ? 15 : 13}px Inter, sans-serif`,
        fill: new Fill({ color }),
        stroke: new Stroke({ color: iconOutsetColor(), width: selected ? 4 : 3 }),
      }),
      zIndex,
    });
  }
  state.styleCache.set(key, style);
  return style;
}

export function markerLabelStyle(pin) {
  const key = `marker-label:${pin.location.id}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const style = new Style({
    text: new Text({
      text: pin.location.title,
      offsetY: 21,
      font: '700 10px "Arial Narrow", "Roboto Condensed", sans-serif',
      fill: new Fill({ color: "#f2ece0" }),
      stroke: new Stroke({ color: "rgba(0,0,0,0.98)", width: 3 }),
      backgroundFill: new Fill({ color: "rgba(10,12,17,0.72)" }),
      padding: [2, 4, 2, 4],
      overflow: true,
    }),
    zIndex: pin.priority,
  });
  state.styleCache.set(key, style);
  return style;
}

export function prepareMarkerIcon(category) {
  if (!category.iconAsset) return;
  const key = markerIconKey(category);
  if (state.markerIcons.has(key)) return;
  state.markerIcons.set(key, null);
  const source = new Image();
  source.onload = () => {
    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;
    const context = canvas.getContext("2d");
    const color = categoryColor(category);

    // A glyph is a silhouette, so it takes the category colour. A picture
    // already carries its own, and flattening it to one colour would leave
    // nothing but its outline filled in.
    const tinted = document.createElement("canvas");
    tinted.width = 64;
    tinted.height = 64;
    const tintedContext = tinted.getContext("2d");
    tintedContext.imageSmoothingEnabled = !category.iconPicture;
    tintedContext.drawImage(source, 6, 6, 52, 52);
    if (!category.iconPicture) {
      tintedContext.globalCompositeOperation = "source-in";
      tintedContext.fillStyle = color;
      tintedContext.fillRect(0, 0, 64, 64);
    }

    const outline = document.createElement("canvas");
    outline.width = 64;
    outline.height = 64;
    const outlineContext = outline.getContext("2d");
    outlineContext.drawImage(source, 6, 6, 52, 52);
    outlineContext.globalCompositeOperation = "source-in";
    outlineContext.fillStyle = iconOutsetColor();
    outlineContext.fillRect(0, 0, 64, 64);

    for (let y = -3; y <= 3; y++) {
      for (let x = -3; x <= 3; x++) {
        if (x * x + y * y <= 10) context.drawImage(outline, x, y);
      }
    }
    context.drawImage(tinted, 0, 0);
    state.markerIcons.set(key, canvas.toDataURL("image/png"));
    // A raster is keyed by asset and colour, so it stands in for every category
    // drawn that way -- Shrine and Daedric Shrine, House and House (Ownable) --
    // and only one of them asked for it. Every marker style is dropped rather
    // than that category's two, because the others cached their initials while
    // this was still loading and have nothing to tell them the icon arrived.
    dropMarkerStyles();
    state.layers.pins.changed();
    state.layers.zonePins.changed();
    state.layers.priority.changed();
  };
  source.onerror = () => state.markerIcons.set(key, false);
  source.src = iconURL(category.iconAsset);
}

export function dropMarkerStyles() {
  for (const key of state.styleCache.keys()) {
    if (key.startsWith("marker:")) state.styleCache.delete(key);
  }
}

export function markerIconKey(category) {
  return `${category.iconAsset || ""}:${categoryColor(category)}:${state.world?.iconOutset || "light"}`;
}

// gridLabelFont spells one chunk of a cell label. Monospace, as the field
// these are typed into already is: every hash at a level is the same length,
// so in a fixed pitch a level keeps or drops its labels as one.
function gridLabelFont(weight, size) {
  return `${weight} ${size}px ${gridTheme.labelFont}`;
}

// gridStyle is the chart's adapter over gridCellVisual: the shared tokens
// become an outline-and-fill style plus, when the cell can carry it, a small
// chip in the cell's bottom-right corner -- the bounding-box convention --
// spelling the hash with its prefix faint and its final character bright.
export function gridStyle(feature, resolution) {
  const cell = feature.get("gridCell");
  if (!cell) return null;
  const neighbor = cell.role === "neighbor";
  const size = neighbor ? gridTheme.neighborLabelSizePx : gridTheme.labelSizePx;
  const font = gridLabelFont(900, size);
  const padding = neighbor ? [2, 4, 2, 4] : [3, 5, 3, 5];
  const labelled = labelFitsCell(cell, font, size, padding, resolution);
  const visual = gridCellVisual(cell, {
    subgridVisible: state.subgridVisible,
    labelled,
  });
  if (!visual) return null;

  // The system is part of the key: "1" names a geohash cell and an S2 face,
  // and the two are drawn nowhere near each other.
  const key = `grid:${state.gridSystem}:${cell.hash}:${cell.role}:${cell.contextDistance}:` +
    `${labelled ? 1 : 0}:${visual.bare ? 1 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);

  const zIndex = cell.role === "leaf" || cell.role === "scope" ? 100 : feature.get("priority");
  const styles = [new Style({
    fill: visual.fill
      ? new Fill({ color: hexToRGBA(visual.fill.color, visual.fill.opacity) })
      : undefined,
    stroke: new Stroke({
      color: hexToRGBA(visual.line.color, visual.line.opacity),
      width: visual.line.widthPx,
    }),
    zIndex,
  })];
  if (visual.label) {
    // The prefix is context and the final character is the address's
    // principal digit, so the prefix takes a lighter cut of the same fixed
    // pitch at the same size -- weight alone carries the faintness, because
    // mixing sizes on one line moves the baselines and the address wobbles.
    const chunks = visual.label.prefix
      ? [visual.label.prefix, gridLabelFont(500, size), visual.label.final, gridLabelFont(900, size)]
      : visual.label.final;
    const inset = gridTheme.labelInsetPx * resolution;
    const corner = chipVertex(feature.getGeometry());
    if (corner) styles.push(new Style({
      geometry: new Point([corner[0] - inset, corner[1] + inset]),
      text: new Text({
        text: chunks,
        font: gridLabelFont(900, size),
        textAlign: "right",
        textBaseline: "bottom",
        fill: new Fill({ color: hexToRGBA(visual.label.color, visual.label.textAlpha) }),
        backgroundFill: new Fill({ color: visual.label.chip }),
        padding,
        overflow: true,
      }),
      zIndex: zIndex + 1,
    }));
  }
  state.styleCache.set(key, styles);
  return styles;
}

// chipVertex finds where the chip belongs: the bottom-right-most vertex of
// the cell as it is actually drawn -- clipped to the surface, closed along
// a pole, curved however the system curves. For a rectangle that is exactly
// the bounding-box corner, so geohash chips sit where they always have; for
// a cell whose bounding box wanders off its own ground -- an S2 face
// unwrapped past the seam -- the chip stays on the cell it names.
function chipVertex(geometry) {
  const polygons = geometry.getType() === "MultiPolygon"
    ? geometry.getCoordinates()
    : [geometry.getCoordinates()];
  let corner = null;
  for (const polygon of polygons) {
    for (const [x, y] of polygon[0] || []) {
      if (!corner || x - y > corner[0] - corner[1]) corner = [x, y];
    }
  }
  return corner;
}

// A hash names the cell it sits in, so a label wider than its cell names the
// neighbours instead -- and at the depth where cells are smallest, that is
// every label at once. The cell keeps its outline and colour; only the word
// waits for a zoom that has room for it.
export function labelFitsCell(cell, font, size, padding, resolution) {
  if (!resolution) return true;
  const width = measureLabel(cell.hash, font) + padding[1] + padding[3];
  const height = size + padding[0] + padding[2];
  return width <= (cell.extent[2] - cell.extent[0]) / resolution &&
    height <= (cell.extent[3] - cell.extent[1]) / resolution;
}

export const labelRuler = document.createElement("canvas").getContext("2d");
export const labelWidths = new Map();

export function measureLabel(text, font) {
  const key = `${font}|${text}`;
  let width = labelWidths.get(key);
  if (width === undefined) {
    labelRuler.font = font;
    width = labelRuler.measureText(text).width;
    labelWidths.set(key, width);
  }
  return width;
}

export const zoneScrimFill = new Style({ fill: new Fill({ color: "rgba(5, 8, 16, 0.62)" }) });

// The scrim exists only while something is highlighted, and hiding a
// collection withdraws its highlights, so an all-hidden map has no scrim
// feature left to draw; the check covers the moment in between.
export function zoneScrimStyle() {
  return anyShapeCollectionVisible() ? zoneScrimFill : null;
}

export function zoneStyle(feature, resolution) {
  const zone = feature.get("zone");
  if (isCollectionHidden(collectionOf(zone))) return null;
  const child = feature.get("child");
  const highlighted = state.highlightedZones.has(zone.id);
  const dimmed = zoneContextDimmed(zone.id);
  // A path is a line and a weight, and the weight is its collection's to
  // declare -- drawn at the width the world gives it rather than a width the
  // screen does. A feature spelling a width of its own is still honoured.
  const groundWidth = Number(zone.attrs?.["atlas.stroke.width_px"]) ||
    Number(collectionFor(zone)?.attrs?.["atlas.stroke.width_px"]) || 0;
  if (groundWidth > 0 && feature.getGeometry()?.getType() === "MultiLineString") {
    return pathZoneStyle(zone, feature.get("color"), groundWidth / resolution, highlighted, dimmed);
  }
  const key = `zone:${zone.id}:${child ? 1 : 0}:${highlighted ? 1 : dimmed ? 2 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const color = feature.get("color");
  if (dimmed) {
    // No fill of its own: the scrim below has already taken this zone down
    // with the rest of the map, and a second wash would single it out again.
    const dimmedStyle = new Style({
      stroke: new Stroke({
        color: hexToRGBA(color, child ? 0.34 : 0.44),
        width: child ? 1 : 1.3,
        lineDash: child ? [3, 4] : [7, 5],
      }),
      zIndex: child ? 101 : 100,
    });
    state.styleCache.set(key, dimmedStyle);
    return dimmedStyle;
  }
  const style = new Style({
    fill: new Fill({ color: hexToRGBA(color, child ? 0.035 : 0.07) }),
    stroke: new Stroke({
      color: hexToRGBA(color, child ? 0.5 : 0.72),
      width: child ? 1.2 : 1.8,
      lineDash: child ? [3, 4] : [7, 5],
    }),
  });
  if (!highlighted) {
    state.styleCache.set(key, style);
    return style;
  }
  const highlightedStyles = [
    new Style({
      stroke: new Stroke({
        color: "rgba(255,255,255,0.94)",
        width: 5,
      }),
      zIndex: 9000,
    }),
    new Style({
      fill: new Fill({ color: hexToRGBA(color, 0.22) }),
      stroke: new Stroke({
        color,
        width: 2.6,
      }),
      zIndex: 9001,
    }),
  ];
  state.styleCache.set(key, highlightedStyles);
  return highlightedStyles;
}

// pathZoneStyle draws a line zone as the one stroke it is: a translucent
// band at ground width with round joins, and a thin centerline so the path
// reads at any zoom. Widths depend on the view's resolution, so these styles
// are built per call rather than cached; a world holds few path zones.
function pathZoneStyle(zone, color, width, highlighted, dimmed) {
  const band = Math.max(width, 1.5);
  if (highlighted) {
    return [
      new Style({
        stroke: new Stroke({
          color: "rgba(255,255,255,0.94)",
          width: band + 4,
          lineCap: "round",
          lineJoin: "round",
        }),
        zIndex: 9000,
      }),
      new Style({
        stroke: new Stroke({
          color: hexToRGBA(color, 0.9),
          width: band,
          lineCap: "round",
          lineJoin: "round",
        }),
        zIndex: 9001,
      }),
    ];
  }
  return [
    new Style({
      stroke: new Stroke({
        color: hexToRGBA(color, dimmed ? 0.14 : 0.3),
        width: band,
        lineCap: "round",
        lineJoin: "round",
      }),
    }),
    new Style({
      stroke: new Stroke({
        color: hexToRGBA(color, dimmed ? 0.4 : 0.8),
        width: Math.min(1.6, band),
        lineDash: [7, 5],
      }),
    }),
  ];
}

export function zoneTitleStyle(feature) {
  if (atMaximumNativeZoom()) return null;
  const zone = feature.get("zone");
  if (isCollectionHidden(collectionOf(zone))) return null;
  if (quietChipHidden(zone)) return null;
  const highlighted = state.highlightedZones.has(zone.id);
  // A quiet name, once asked for, skips the crowd-thinning below: like
  // holding Z for pin labels, asking means every one of them.
  if (labelPolicy(zone) === "quiet") return renderedZoneTitleStyle(feature);
  const child = feature.get("child");
  const zoom = state.engine.getView().getZoom() || 0;
  if (!highlighted && child && zoom < state.fitZoom + 3) return null;
  const spanPixels = feature.get("span") / state.engine.getView().getResolution();
  if (!highlighted && state.zoneTitleCount > 40 && spanPixels < 52) return null;
  return renderedZoneTitleStyle(feature);
}

export function zoneTitleDetailStyle(feature) {
  if (!atMaximumNativeZoom()) return null;
  const zone = feature.get("zone");
  if (isCollectionHidden(collectionOf(zone))) return null;
  if (quietChipHidden(zone)) return null;
  return renderedZoneTitleStyle(feature);
}

// A quiet zone's name is context, not headline: the chip waits until the
// reader asks after that zone in particular -- highlighting it, selecting it
// -- or asks after every name at once by holding Z. Z reveals what is merely
// optional, never what was silenced: a collection the reader quieted by hand
// stays quiet under the key, because the choice was theirs and the key is
// not an override.
function quietChipHidden(zone) {
  if (labelPolicy(zone) !== "quiet") return false;
  if (state.highlightedZones.has(zone.id) || state.selectedZone === zone) return false;
  const silenced = state.labelOverrides.get(collectionOf(zone)) === "quiet";
  return silenced || !state.labelsHeld;
}

export function renderedZoneTitleStyle(feature) {
  const child = feature.get("child");
  const zone = feature.get("zone");
  const highlighted = state.highlightedZones.has(zone.id);
  const dimmed = zoneContextDimmed(zone.id);
  const key = `zone-title:${zone.id}:${child ? 1 : 0}:${highlighted ? 1 : dimmed ? 2 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const color = feature.get("color");
  const style = new Style({
    text: new Text({
      text: zone.title.toLocaleUpperCase(),
      font: `${child ? "700 12px" : "800 14px"} "Arial Narrow", "Roboto Condensed", sans-serif`,
      fill: new Fill({
        color: highlighted
          ? "#10131a"
          : dimmed
            ? "rgba(196,191,177,0.56)"
            : child ? "#c8c2b4" : "#f2ece0",
      }),
      stroke: new Stroke({
        color: highlighted
          ? "rgba(255,255,255,0.96)"
          : dimmed ? "rgba(0,0,0,0.82)" : "rgba(0,0,0,0.95)",
        width: highlighted ? 2 : dimmed ? 3 : 4,
      }),
      backgroundFill: new Fill({
        color: highlighted ? color : dimmed ? "rgba(8,11,18,0.84)" : "rgba(13,16,23,0.72)",
      }),
      backgroundStroke: new Stroke({
        color: highlighted ? "#ffffff" : hexToRGBA(color, dimmed ? 0.24 : 0.55),
        width: highlighted ? 2 : 1,
      }),
      padding: [5, 8, 5, 8],
      overflow: true,
    }),
    zIndex: highlighted ? 9_100_000 : feature.get("priority"),
  });
  state.styleCache.set(key, style);
  return style;
}

export function zoneContextDimmed(zoneID) {
  if (!state.highlightedZones.size || state.highlightedZones.has(zoneID)) return false;
  for (const highlightedID of state.highlightedZones) {
    if (zoneIsAncestorOf(zoneID, highlightedID)) return false;
  }
  return true;
}

export function zoneIsAncestorOf(candidateID, zoneID) {
  const visited = new Set();
  let parentID = state.zoneRecords.get(zoneID)?.zone.parent;
  while (parentID != null && !visited.has(parentID)) {
    if (parentID === candidateID) return true;
    visited.add(parentID);
    parentID = state.zoneRecords.get(parentID)?.zone.parent;
  }
  return false;
}
