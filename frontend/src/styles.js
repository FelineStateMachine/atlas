import {
  Fill,
  Icon,
  Stroke,
  Style,
  Text,
} from "ol/style.js";

import { geohashAlphabet, palette } from "./constants.js";
import { state } from "./state.js";
import {
  atMaximumNativeZoom,
  isPriorityPin,
  pinIsHidden,
  textDetailRatio,
} from "./pins.js";
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
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone || isPriorityPin(pin)) return null;
  return markerStyles(pin, false);
}

export function zonePinFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || !state.highlightedZones.size || !pin.insideHighlightedZone ||
      pinIsHidden(pin) || isPriorityPin(pin)) {
    return null;
  }
  return markerStyles(pin, false);
}

export function pinLabelFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || !state.pinLabelsVisible || !atMaximumNativeZoom()) return null;
  return markerLabelStyle(pin);
}

export function textFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone || isPriorityPin(pin)) return null;
  if (atMaximumNativeZoom()) return null;
  const minimumZoom = state.fitZoom + Math.log2(textDetailRatio(pin.category));
  if ((state.engine.getView().getZoom() || 0) < minimumZoom) return null;
  return textStyles(pin, false);
}

export function textDetailFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone ||
      isPriorityPin(pin) || !atMaximumNativeZoom()) {
    return null;
  }
  return textStyles(pin, false);
}

export function zoneTextFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || !state.highlightedZones.size || !pin.insideHighlightedZone ||
      pinIsHidden(pin) || isPriorityPin(pin)) {
    return null;
  }
  return textStyles(pin, false);
}

export function priorityFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin)) return null;
  if (pin.category.displayType === "text") return textStyles(pin, pin === state.selectedPin);
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
  return `${category.iconAsset || ""}:${categoryColor(category)}:${state.map?.iconOutset || "light"}`;
}

export function textStyles(pin, selected) {
  const key = `text:${pin.location.id}:${selected ? 1 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const style = new Style({
    text: new Text({
      text: pin.location.title,
      font: `${selected ? "900" : "800"} 14px "Arial Narrow", "Roboto Condensed", sans-serif`,
      fill: new Fill({ color: selected ? "#7fd0ea" : "#f2ece0" }),
      stroke: new Stroke({ color: "rgba(0,0,0,0.95)", width: 4 }),
      backgroundFill: new Fill({ color: selected ? "rgba(13,16,23,0.9)" : "rgba(10,12,17,0.58)" }),
      backgroundStroke: selected ? new Stroke({ color: "#3aa5c9", width: 1 }) : undefined,
      padding: [3, 6, 3, 6],
      overflow: true,
    }),
    zIndex: selected ? 20_000_000 : pin.priority + 3_000_000,
  });
  const styles = [style];
  state.styleCache.set(key, styles);
  return styles;
}

export function gridStyle(feature, resolution) {
  const cell = feature.get("gridCell");
  if (!cell) return null;
  const leaf = cell.role === "leaf";
  const scope = cell.role === "scope";
  const neighbor = cell.role === "neighbor";
  // Monospace, as the field these are typed into already is: a hash is a code,
  // and in a proportional face m6w is half again the width of m6j, so a level
  // lost its labels a few cells at a time as the map shrank. Every hash at a
  // level is the same length, so in a fixed pitch they are the same width, and
  // the level keeps or drops its labels as one.
  const size = leaf || scope ? 15 : neighbor ? 10 : 11;
  const weight = leaf || scope ? 900 : neighbor ? 750 : 800;
  const font = `${weight} ${size}px ui-monospace, SFMono-Regular, Menlo, monospace`;
  const padding = neighbor ? [2, 4, 2, 4] : [3, 5, 3, 5];
  const labelled = labelFitsCell(cell, font, size, padding, resolution);
  // A subdivision is offered so it can be named and descended into, and one too
  // small to carry its names is a mesh laid over the map. It waits for the zoom
  // that has room for them, so the cells and their labels arrive together --
  // and putting the grid away by hand leaves exactly that state, the one the
  // map arrives at on its own. What stays either way is the boundary of the
  // chosen cell and the shaded ancestors around it, which say where the reader
  // is and dim what is outside.
  if (cell.role === "child" && (!labelled || !state.subgridVisible)) return null;
  // The scope is drawn two ways: with the subgrid inside it, where a bare
  // outline would be lost among its own children, and without, where the
  // boundary is what is left of the cell and carries its name.
  const bare = scope && state.subgridVisible;
  const key = `grid:${cell.hash}:${cell.role}:${cell.contextDistance}:` +
    `${labelled ? 1 : 0}:${bare ? 1 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const color = palette[Math.max(0, geohashAlphabet.indexOf(cell.hash[cell.hash.length - 1])) %
    palette.length];
  const style = new Style({
    fill: scope
      ? undefined
      : new Fill({
        color: neighbor
          ? `rgba(5, 8, 16, ${Math.min(0.52, 0.30 + cell.contextDistance * 0.06)})`
          : hexToRGBA(color, leaf ? 0.14 : 0.055),
      }),
    stroke: new Stroke({
      color: leaf || scope ? "#ffffff" : hexToRGBA(color, neighbor ? 0.44 : 0.82),
      width: leaf ? 2.5 : scope ? (bare ? 1.8 : 2.5) : neighbor ? 1 : 1.4,
    }),
    text: labelled && !bare
      ? new Text({
        text: cell.hash,
        font,
        fill: new Fill({
          color: leaf || scope ? "#ffffff" : neighbor ? hexToRGBA(color, 0.72) : color,
        }),
        stroke: new Stroke({ color: "rgba(0,0,0,0.96)", width: 4 }),
        backgroundFill: new Fill({
          color: neighbor ? "rgba(8,11,18,0.88)" : "rgba(12,15,22,0.76)",
        }),
        padding,
        overflow: true,
      })
      : undefined,
    zIndex: leaf || scope ? 100 : feature.get("priority"),
  });
  state.styleCache.set(key, style);
  return style;
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

export function zoneScrimStyle() {
  return state.zonesVisible ? zoneScrimFill : null;
}

export function zoneStyle(feature) {
  if (!state.zonesVisible) return null;
  const zone = feature.get("zone");
  const child = feature.get("child");
  const highlighted = state.highlightedZones.has(zone.id);
  const dimmed = zoneContextDimmed(zone.id);
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

export function zoneTitleStyle(feature) {
  if (!state.zonesVisible || atMaximumNativeZoom()) return null;
  const child = feature.get("child");
  const highlighted = state.highlightedZones.has(feature.get("zone").id);
  const zoom = state.engine.getView().getZoom() || 0;
  if (!highlighted && child && zoom < state.fitZoom + 3) return null;
  const spanPixels = feature.get("span") / state.engine.getView().getResolution();
  if (!highlighted && state.zoneTitleCount > 40 && spanPixels < 52) return null;
  return renderedZoneTitleStyle(feature);
}

export function zoneTitleDetailStyle(feature) {
  if (!state.zonesVisible || !atMaximumNativeZoom()) return null;
  return renderedZoneTitleStyle(feature);
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
  let parentID = state.zoneRecords.get(zoneID)?.zone.parentRegionId;
  while (parentID != null && !visited.has(parentID)) {
    if (parentID === candidateID) return true;
    visited.add(parentID);
    parentID = state.zoneRecords.get(parentID)?.zone.parentRegionId;
  }
  return false;
}
