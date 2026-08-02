import OLMap from "ol/Map.js";
import View from "ol/View.js";
import { defaults as defaultControls } from "ol/control/defaults.js";
import { defaults as defaultInteractions } from "ol/interaction/defaults.js";
import TileLayer from "ol/layer/Tile.js";
import VectorLayer from "ol/layer/Vector.js";
import Projection from "ol/proj/Projection.js";
import VectorSource from "ol/source/Vector.js";

import { collectionOf, isCollectionHidden } from "./collections.js";
import { elements } from "./dom.js";
import { state } from "./state.js";
import {
  featureOrder,
  gridStyle,
  pinFeatureStyle,
  pinLabelFeatureStyle,
  priorityFeatureStyle,
  zonePinFeatureStyle,
  zoneScrimStyle,
  zoneStyle,
  zoneTitleDetailStyle,
  zoneTitleStyle,
} from "./styles.js";
import { settleView } from "./navigation.js";
import { updateOverviewViewport } from "./overview.js";
import { selectGridCell } from "./grid.js";
import { setHoveredPin } from "./features.js";
import { showFeature, showPin } from "./detail.js";

// A vector layer left to itself stretches the last frame it drew through an
// animation, which leaves markers the wrong size mid-zoom and blank ground
// wherever a pan lands. These are all drawn afresh each frame instead, so a
// view that is being steered shows what it is passing over.
export function eagerVector(options) {
  return new VectorLayer({ updateWhileAnimating: true, ...options });
}

export function initializeMap() {
  const size = state.volume.tileGrid.size;
  const worldExtent = [0, -size, size, 0];
  state.projection = new Projection({
    code: "ATLAS:PIXELS",
    units: "pixels",
    extent: worldExtent,
  });
  state.sources = {
    grid: new VectorSource({ wrapX: false }),
    gridContext: new VectorSource({ wrapX: false }),
    zoneScrim: new VectorSource({ wrapX: false }),
    zones: new VectorSource({ wrapX: false }),
    zoneTitles: new VectorSource({ wrapX: false }),
    pins: new VectorSource({ wrapX: false }),
    priority: new VectorSource({ wrapX: false }),
  };
  state.layers = {
    raster: new TileLayer({ zIndex: 0 }),
    // Levels above the complete one are only captured in patches. They ride on
    // top of the base layer so the fully-covered pyramid still shows through
    // wherever the deep capture has a gap.
    rasterDetail: new TileLayer({ zIndex: 1 }),
    grid: eagerVector({
      source: state.sources.grid,
      style: gridStyle,
      renderOrder: featureOrder,
      renderBuffer: 40,
      zIndex: 5,
    }),
    // Under the zones and over the map: the dimming is something the map is
    // seen through, not something drawn on top of the zone that was asked for.
    zoneScrim: eagerVector({
      source: state.sources.zoneScrim,
      style: zoneScrimStyle,
      zIndex: 6,
    }),
    zones: eagerVector({
      source: state.sources.zones,
      style: zoneStyle,
      renderBuffer: 32,
      zIndex: 10,
    }),
    zoneTitles: eagerVector({
      source: state.sources.zoneTitles,
      style: zoneTitleStyle,
      renderOrder: featureOrder,
      renderBuffer: 160,
      zIndex: 20,
    }),
    zoneTitleDetail: eagerVector({
      source: state.sources.zoneTitles,
      style: zoneTitleDetailStyle,
      renderOrder: featureOrder,
      renderBuffer: 160,
      zIndex: 44,
    }),
    // Nothing is decluttered. Dropping whatever overlaps makes a crowded area
    // quietly show less than it holds, for text labels as much as for markers;
    // zooming in to separate them is the reader's job.
    pins: eagerVector({
      source: state.sources.pins,
      style: pinFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 48,
      zIndex: 40,
    }),
    zonePins: eagerVector({
      source: state.sources.pins,
      style: zonePinFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 48,
      zIndex: 42,
    }),
    pinLabels: eagerVector({
      source: state.sources.pins,
      style: pinLabelFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 180,
      zIndex: 45,
    }),
    gridContext: eagerVector({
      source: state.sources.gridContext,
      style: gridStyle,
      renderOrder: featureOrder,
      renderBuffer: 40,
      zIndex: 48,
    }),
    priority: eagerVector({
      source: state.sources.priority,
      style: priorityFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 220,
      zIndex: 50,
    }),
  };
  state.engine = new OLMap({
    target: elements.viewport,
    layers: Object.values(state.layers),
    controls: defaultControls({
      attribution: false,
      rotate: false,
      zoom: false,
    }),
    interactions: defaultInteractions({
      onFocusOnly: false,
    }),
    view: createView(5),
  });

  state.engine.on("movestart", () => {
    state.engine.getViewport().classList.add("is-dragging");
  });
  state.engine.on("moveend", () => {
    state.engine.getViewport().classList.remove("is-dragging");
    // A view set straight to a new centre has arrived by the next frame, so a
    // sweep across the overview finishes its journey many times over. Counting
    // the pins in view and writing the session down are worth doing once the
    // hand steering it comes to rest, which is what settleView is for.
    if (state.overviewPointer !== null) return;
    settleView();
  });
  // The locator follows the view as it moves rather than only once it settles.
  // Repeated calls that would draw the same rectangle cost nothing.
  state.engine.on("postrender", updateOverviewViewport);
  // OpenLayers can condition wheel handling on focus when its target is
  // keyboard-focusable. Refocus during the wheel's capture phase so returning
  // from any sidebar control never costs a discarded scroll gesture.
  elements.viewport.addEventListener("wheel", () => {
    if (document.activeElement !== elements.viewport) {
      elements.viewport.focus({ preventScroll: true });
    }
  }, { capture: true, passive: true });
  state.engine.on("singleclick", (event) => {
    if (state.gridEnabled) {
      const cell = state.engine.forEachFeatureAtPixel(
        event.pixel,
        // The cell the reader is already in lies over its own children, and
        // answering for them would mean the grid could not be descended.
        (feature) => {
          const under = feature.get("gridCell");
          return under && under.role !== "leaf" && under.role !== "scope" ? under : null;
        },
        {
          hitTolerance: 1,
          layerFilter: (layer) =>
            layer === state.layers.grid || layer === state.layers.gridContext,
        },
      );
      if (cell) {
        selectGridCell(cell.hash);
        return;
      }
    }
    const pin = state.engine.forEachFeatureAtPixel(
      event.pixel,
      (feature, layer) => (isAnnotationLayer(layer) && feature.get("pin")) || null,
      { hitTolerance: 5, layerFilter: isAnnotationLayer },
    );
    if (pin) {
      showPin(pin);
      return;
    }
    // A click that lands on no marker falls through to the ground itself:
    // the topmost shape feature under the pointer opens the same card its
    // legend row does. A hidden collection's ground is not drawn and so
    // answers no hit-test, but the rule is spelled out here rather than
    // left riding on a renderer detail.
    const zone = state.engine.forEachFeatureAtPixel(
      event.pixel,
      (feature) => {
        const ground = feature.get("zone");
        return ground && !isCollectionHidden(collectionOf(ground)) ? ground : null;
      },
      { layerFilter: (layer) => layer === state.layers.zones },
    );
    if (zone) showFeature(zone);
  });
  state.engine.on("pointermove", (event) => {
    if (event.dragging) {
      setHoveredPin(null);
      return;
    }
    const hit = state.engine.forEachFeatureAtPixel(
      event.pixel,
      (feature, layer) => (isAnnotationLayer(layer) && feature.get("pin")) || null,
      { hitTolerance: 4, layerFilter: isAnnotationLayer },
    );
    setHoveredPin(hit || null);
    const gridHit = state.gridEnabled && state.engine.hasFeatureAtPixel(event.pixel, {
      layerFilter: (layer) =>
        layer === state.layers.grid || layer === state.layers.gridContext,
    });
    state.engine.getTargetElement().style.cursor = hit || gridHit ? "pointer" : "";
  });
  state.engine.getViewport().addEventListener("pointerleave", () => setHoveredPin(null));
}

// Every layer a pin can be drawn in, including the two it moves to while a zone
// is highlighted. Leaving those out left the pins of the highlighted zone --
// the only ones still on screen -- answering neither the pointer nor a click.
export function isAnnotationLayer(layer) {
  return layer === state.layers.pins || layer === state.layers.zonePins ||
    layer === state.layers.pinLabels || layer === state.layers.priority;
}

// createView holds the camera to the map rather than to the world square
// it is cut from: a map narrower or shorter than the square -- a wikimap's
// margin, a planet's 2:1 sheet -- centers in the pane when the whole of it
// is on screen, instead of hugging the square's top corner across the dead
// half nothing is drawn in.
export function createView(maxZoom, extent = null) {
  const size = state.volume.tileGrid.size;
  const held = extent || [0, -size, size, 0];
  return new View({
    projection: state.projection,
    center: [(held[0] + held[2]) / 2, (held[1] + held[3]) / 2],
    resolutions: resolutions(maxZoom),
    minZoom: 0,
    maxZoom,
    extent: held,
    constrainResolution: false,
    smoothResolutionConstraint: false,
    showFullExtent: true,
    zoom: 0,
  });
}

export function resolutions(maxZoom) {
  const base = state.volume.tileGrid.size / state.volume.tileGrid.tileSize;
  return Array.from({ length: maxZoom + 1 }, (_, zoom) => base / (2 ** zoom));
}
