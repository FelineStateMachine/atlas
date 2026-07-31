import "ol/ol.css";
import "./app.css";

import Feature from "ol/Feature.js";
import OLMap from "ol/Map.js";
import View from "ol/View.js";
import { defaults as defaultControls } from "ol/control/defaults.js";
import { createEmpty, extend, getCenter } from "ol/extent.js";
import Point from "ol/geom/Point.js";
import MultiPolygon from "ol/geom/MultiPolygon.js";
import Polygon from "ol/geom/Polygon.js";
import { defaults as defaultInteractions } from "ol/interaction/defaults.js";
import TileLayer from "ol/layer/Tile.js";
import VectorLayer from "ol/layer/Vector.js";
import Projection from "ol/proj/Projection.js";
import VectorSource from "ol/source/Vector.js";
import XYZ from "ol/source/XYZ.js";
import {
  Fill,
  Icon,
  Stroke,
  Style,
  Text,
} from "ol/style.js";
import TileGrid from "ol/tilegrid/TileGrid.js";

const $ = (selector) => document.querySelector(selector);
const elements = {
  game: $("#game-select"),
  map: $("#map-select"),
  variant: $("#variant-select"),
  variantField: $("#variant-field"),
  gameTitle: $("#game-title"),
  title: $("#map-title"),
  meta: $("#map-meta"),
  legend: $("#legend"),
  search: $("#pin-search"),
  searchResults: $("#search-results"),
  visibleCount: $("#visible-count"),
  viewport: $("#map"),
  layers: $("#layers"),
  zoneToggle: $("#zone-toggle"),
  zoneCount: $("#zone-count"),
  zoneIndex: $("#zone-index"),
  zoneList: $("#zone-list"),
  loading: $("#map-loading"),
  labelsHint: $("#labels-hint"),
  gridHint: $("#grid-hint"),
  soloChip: $("#solo-chip"),
  overview: $("#overview"),
  overviewShelf: $("#overview-shelf"),
  overviewDock: $("#overview-dock"),
  overviewCanvas: $("#overview-canvas"),
  overviewViewport: $("#overview-viewport"),
  gridNavigator: $("#grid-navigator"),
  gridInput: $("#grid-input"),
  gridBack: $("#grid-back"),
  subgridToggle: $("#subgrid-toggle"),
  detail: $("#pin-detail"),
  detailTitle: $("#detail-title"),
  detailCategory: $("#detail-category"),
  detailDescription: $("#detail-description"),
  detailID: $("#detail-id"),
  detailCoordinates: $("#detail-coordinates"),
  detailLinks: $("#detail-links"),
  detailDot: $("#detail-dot"),
  sidebar: $("#sidebar"),
  mobileLegend: $("#mobile-legend"),
  fatal: $("#fatal-error"),
  fatalMessage: $("#fatal-message"),
};

const palette = [
  "#d6f36b", "#72d5f4", "#ff9e64", "#df83ff", "#62e6ae",
  "#ff6f91", "#f4d35e", "#8aa9ff", "#e7a56d", "#83d483",
];

const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz";
const geohashMaxDepth = 3;
const overzoomLevels = 2;

const state = {
  catalog: null,
  game: null,
  map: null,
  variant: null,
  engine: null,
  projection: null,
  layers: null,
  sources: null,
  hiddenCategories: new Set(),
  collapsedSections: new Set(),
  pins: [],
  pinByID: new Map(),
  selectedPin: null,
  hoveredPin: null,
  pinLabelsVisible: false,
  gridEnabled: false,
  // Whether the cells are drawn, which is a separate question from whether one
  // of them is holding the map to a place.
  subgridVisible: true,
  gridPrefix: "",
  zonesVisible: true,
  zoneRecords: new Map(),
  highlightedZones: new Set(),
  focusedZoneID: null,
  search: "",
  fitZoom: 0,
  zoneTitleCount: 0,
  eligibleLocations: 0,
  tileRun: 0,
  tileStats: { requested: 0, loaded: 0, errors: 0, peakPending: 0 },
  overviewRun: 0,
  overviewKey: "",
  overviewPointer: null,
  overviewDocked: false,
  renderedShard: 0,
  mapRun: 0,
  textByMap: new Map(),
  restore: null,
  settling: false,
  styleCache: new Map(),
  markerIcons: new Map(),
};

async function start() {
  bindUIEvents();
  try {
    const response = await fetch("/static/catalog.json");
    if (!response.ok) throw new Error(`catalog request returned ${response.status}`);
    state.catalog = await response.json();
    if (!state.catalog.games.length) throw new Error("the embedded catalog contains no maps");
    initializeMap();
    populateSelect(elements.game, state.catalog.games, "title");
    const route = readRoute();
    const session = readSession();
    // An address naming somewhere else was typed on purpose, so it wins and
    // arrives clean; anything else resumes where the reader left off.
    const resuming = session &&
      (!route.game || (route.game === session.game && route.map === session.map));
    state.restore = resuming ? session : null;
    await selectGame(
      route.game || session?.game || state.catalog.games[0].slug,
      route.map || session?.map,
    );
    state.restore = null;
    exposeDiagnostics();
    requestAnimationFrame(() => elements.viewport.focus({ preventScroll: true }));
  } catch (error) {
    elements.loading.hidden = true;
    elements.fatalMessage.textContent = error instanceof Error ? error.message : String(error);
    elements.fatal.hidden = false;
  }
}

// A vector layer left to itself stretches the last frame it drew through an
// animation, which leaves markers the wrong size mid-zoom and blank ground
// wherever a pan lands. These are all drawn afresh each frame instead, so a
// view that is being steered shows what it is passing over.
function eagerVector(options) {
  return new VectorLayer({ updateWhileAnimating: true, ...options });
}

function initializeMap() {
  const size = state.catalog.tileGrid.size;
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
    text: new VectorSource({ wrapX: false }),
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
    text: eagerVector({
      source: state.sources.text,
      style: textFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 220,
      zIndex: 30,
    }),
    textDetail: eagerVector({
      source: state.sources.text,
      style: textDetailFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 220,
      zIndex: 45,
    }),
    zoneText: eagerVector({
      source: state.sources.text,
      style: zoneTextFeatureStyle,
      renderOrder: featureOrder,
      renderBuffer: 220,
      zIndex: 41,
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
        selectGridPrefix(cell.hash);
        return;
      }
    }
    const pin = state.engine.forEachFeatureAtPixel(
      event.pixel,
      (feature, layer) => (isAnnotationLayer(layer) && feature.get("pin")) || null,
      { hitTolerance: 5, layerFilter: isAnnotationLayer },
    );
    if (pin) showPin(pin);
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
    const hovered = hit?.category.displayType === "text" ? null : hit;
    setHoveredPin(hovered || null);
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
function isAnnotationLayer(layer) {
  return layer === state.layers.pins || layer === state.layers.zonePins ||
    layer === state.layers.pinLabels || layer === state.layers.priority ||
    layer === state.layers.text || layer === state.layers.textDetail ||
    layer === state.layers.zoneText;
}

function createView(maxZoom) {
  const size = state.catalog.tileGrid.size;
  return new View({
    projection: state.projection,
    center: [size / 2, -size / 2],
    resolutions: resolutions(maxZoom),
    minZoom: 0,
    maxZoom,
    extent: [0, -size, size, 0],
    constrainResolution: false,
    smoothResolutionConstraint: false,
    showFullExtent: true,
    zoom: 0,
  });
}

function bindUIEvents() {
  elements.game.addEventListener("change", () => { void selectGame(elements.game.value); });
  elements.map.addEventListener("change", () => { void selectMap(Number(elements.map.value)); });
  elements.variant.addEventListener("change", () => selectVariant(Number(elements.variant.value)));

  elements.legend.addEventListener("change", (event) => {
    const input = event.target;
    if (input.dataset.sectionToggle) {
      toggleSection(input.dataset.sectionToggle);
      return;
    }
    if (!input.dataset.category) return;
    const categoryID = Number(input.dataset.category);
    if (input.checked) state.hiddenCategories.delete(categoryID);
    else state.hiddenCategories.add(categoryID);
    applyPinFilters();
    syncSectionSwitches();
  });

  // Which row the pointer is over is worked out from where the pointer is,
  // rather than read from :hover. Scrolling moves the list under a still
  // cursor, and the browser does not always revise :hover when it does -- so a
  // row kept offering an action to a pointer that had long since left it.
  // Recomputing on scroll settles it, because the answer comes from the
  // pointer's actual position either way.
  let pointerAt = null;
  const markHoveredRow = () => {
    for (const marked of elements.layers.querySelectorAll(".is-hovered")) {
      marked.classList.remove("is-hovered");
    }
    if (!pointerAt) return;
    const under = document.elementFromPoint(pointerAt.x, pointerAt.y);
    const row = under?.closest(".category-row, .layer-header");
    if (row && elements.layers.contains(row)) row.classList.add("is-hovered");
  };
  elements.layers.addEventListener("pointermove", (event) => {
    pointerAt = { x: event.clientX, y: event.clientY };
    markHoveredRow();
  }, { passive: true });
  elements.layers.addEventListener("pointerleave", () => {
    pointerAt = null;
    markHoveredRow();
  });
  elements.layers.addEventListener("scroll", markHoveredRow, { passive: true });

  elements.layers.addEventListener("click", (event) => {
    const only = event.target.closest("[data-only-category], [data-only-section]");
    if (only) {
      // The button sits inside the row's label, whose default action would
      // otherwise toggle the very checkbox this is meant to override.
      event.preventDefault();
      event.stopPropagation();
      showOnly(only.dataset.onlyCategory
        ? { category: Number(only.dataset.onlyCategory) }
        : { section: only.dataset.onlySection });
      return;
    }
    const button = event.target.closest("[data-section]");
    if (!button) return;
    const key = button.dataset.section;
    if (state.collapsedSections.has(key)) state.collapsedSections.delete(key);
    else state.collapsedSections.add(key);
    syncSectionCollapse();
  });

  elements.soloChip.addEventListener("click", () => setAllCategories(true));
  $("#expand-all").addEventListener("click", () => setAllSectionsCollapsed(false));
  $("#collapse-all").addEventListener("click", () => setAllSectionsCollapsed(true));
  $("#show-all").addEventListener("click", () => setAllCategories(true));
  $("#hide-all").addEventListener("click", () => setAllCategories(false));
  elements.zoneToggle.addEventListener("change", () => {
    setZonesVisible(elements.zoneToggle.checked);
  });
  elements.zoneIndex.addEventListener("click", (event) => {
    const button = event.target.closest("[data-zone]");
    if (!button) return;
    jumpToZone(Number(button.dataset.zone));
  });
  elements.zoneIndex.addEventListener("contextmenu", (event) => {
    const button = event.target.closest("[data-zone]");
    if (!button) return;
    event.preventDefault();
    toggleZoneHighlight(Number(button.dataset.zone));
  });
  elements.search.addEventListener("input", () => {
    state.search = elements.search.value.trim().toLocaleLowerCase();
    applyPinFilters();
    renderSearchResults();
  });
  elements.searchResults.addEventListener("click", (event) => {
    const result = event.target.closest("[data-location]");
    if (!result) return;
    const pin = state.pinByID.get(Number(result.dataset.location));
    if (pin) revealPin(pin);
  });

  elements.detailLinks.addEventListener("click", (event) => {
    const button = event.target.closest("[data-location]");
    if (!button) return;
    const pin = state.pinByID.get(Number(button.dataset.location));
    if (pin) revealPin(pin);
  });

  $("#close-detail").addEventListener("click", closeDetail);
  $("#zoom-in").addEventListener("click", () => changeZoom(1));
  $("#zoom-out").addEventListener("click", () => changeZoom(-1));
  elements.gridBack.addEventListener("click", ascendGrid);
  elements.gridInput.addEventListener("input", () => {
    const maximum = gridMaxDepth();
    const value = [...elements.gridInput.value.toLocaleLowerCase()]
      .filter((character) => geohashAlphabet.includes(character))
      .slice(0, maximum)
      .join("");
    elements.gridInput.value = value;
    selectGridPrefix(value);
  });
  elements.subgridToggle.addEventListener("click", () => {
    setSubgridVisible(!state.subgridVisible);
    elements.viewport.focus({ preventScroll: true });
  });
  // Escape leaves the field before it leaves the level: the first press hands
  // the keyboard back to the map, and the next one telescopes out as ever.
  // Space reaches the subgrid from inside the field too, since a hash has no
  // spaces in it and clearing the mesh is what is most wanted while typing one.
  elements.gridInput.addEventListener("keydown", (event) => {
    if (event.key === " ") {
      event.preventDefault();
      setSubgridVisible(!state.subgridVisible);
      return;
    }
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    elements.viewport.focus({ preventScroll: true });
  });

  elements.viewport.addEventListener("keydown", (event) => {
    if (isEditableTarget(event.target)) return;
    if (event.key === "+" || event.key === "=") changeZoom(1);
    else if (event.key === "-") changeZoom(-1);
    else if (event.key === "Escape") closeDetail();
    else return;
    event.preventDefault();
  });
  // The webview's own menu offers Reload and Inspect Element, which belong to a
  // browser rather than to a map. Text fields keep theirs, where cut and paste
  // are worth having.
  window.addEventListener("contextmenu", (event) => {
    if (isEditableTarget(event.target)) return;
    event.preventDefault();
  });

  window.addEventListener("keydown", (event) => {
    // Reload keeps its usual shortcut now that the menu offering it is gone.
    // The session is restored on the way back, so the view returns as it was.
    if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "r") {
      event.preventDefault();
      location.reload();
      return;
    }
    if (isEditableTarget(event.target)) return;
    if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
      event.preventDefault();
      elements.search.focus();
      elements.search.select();
      return;
    }
    // ⌘B on a Mac, Ctrl+B elsewhere: the usual shortcut for putting a sidebar
    // away, and the map is the reason the window is open.
    if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "b") {
      event.preventDefault();
      toggleSidebar();
      return;
    }
    if (handleGridKey(event)) return;
    if (event.key.toLocaleLowerCase() === "z") {
      event.preventDefault();
      state.pinLabelsVisible = !state.pinLabelsVisible;
      elements.labelsHint.textContent = `Z · labels ${state.pinLabelsVisible ? "on" : "off"}`;
      state.layers.pinLabels.changed();
    }
  });
  window.addEventListener("hashchange", () => {
    const route = readRoute();
    if (route.game === state.game?.slug && route.map === state.map?.slug) return;
    // An address typed by hand names where to go, so it overrides what the
    // game remembers.
    state.restore = null;
    void selectGame(route.game || state.catalog.games[0].slug, route.map);
  });
  window.addEventListener("resize", () => {
    state.engine?.updateSize();
    updateOverviewViewport();
  });
  // Holding the pointer down steers the map: a reader crossing the world drags
  // the locator there in one sweep instead of clicking their way across it.
  // Capturing the pointer keeps the sweep alive past the edge of the overview,
  // where the destination is simply the nearest point still on the map.
  elements.overview.addEventListener("pointerdown", (event) => {
    if (state.overviewDocked) return;
    if (event.pointerType === "mouse" && event.button !== 0) return;
    event.preventDefault();
    elements.overview.setPointerCapture(event.pointerId);
    state.overviewPointer = event.pointerId;
    overviewNavigate(event, true);
  });
  elements.overview.addEventListener("pointermove", (event) => {
    if (state.overviewPointer !== event.pointerId) return;
    event.preventDefault();
    overviewNavigate(event, false);
  });
  const releaseOverview = (event) => {
    if (state.overviewPointer !== event.pointerId) return;
    state.overviewPointer = null;
    settleView();
  };
  elements.overview.addEventListener("pointerup", releaseOverview);
  elements.overview.addEventListener("pointercancel", releaseOverview);
  elements.overviewDock.addEventListener("click", () => {
    setOverviewDocked(!state.overviewDocked);
    elements.viewport.focus({ preventScroll: true });
  });
  elements.mobileLegend.addEventListener("click", () => {
    const open = elements.sidebar.classList.toggle("is-open");
    elements.mobileLegend.setAttribute("aria-expanded", String(open));
  });
}

// A map arrives in two pieces: its layers, categories and regions as JSON, and
// its locations packed as parallel arrays. Nothing here is fetched until the
// map is opened, so the catalog can grow without the wait growing with it.
async function loadMap(entry) {
  const [detail, packed] = await Promise.all([
    fetch(`/static/catalog/${entry.id}.json`).then((r) => r.json()),
    fetch(`/static/catalog/${entry.id}.bin`).then((r) => r.arrayBuffer()),
  ]);
  const categories = detail.groups.flatMap((group) => group.categories);
  unpackLocations(packed, categories);
  return {
    ...entry,
    variants: detail.variants,
    groups: detail.groups,
    zones: detail.zones || [],
    sections: legendSections(detail.groups),
  };
}

// The reader of packLocations. Each field is a view straight onto the buffer,
// laid out so no copying or realignment is needed to get at it.
function unpackLocations(buffer, categories) {
  const view = new DataView(buffer);
  const magic = String.fromCharCode(...new Uint8Array(buffer, 0, 8));
  if (magic !== "ATLASLOC") throw new Error("location payload is not in the expected form");
  const version = view.getUint16(8, true);
  if (version !== 2) throw new Error(`location payload is version ${version}, and this reads 2`);
  const count = view.getUint32(10, true);

  let at = 16;
  const ids = new Int32Array(buffer, at, count);
  const latitudes = new Float32Array(buffer, (at += count * 4), count);
  const longitudes = new Float32Array(buffer, (at += count * 4), count);
  const regions = new Int32Array(buffer, (at += count * 4), count);
  const shards = new Int32Array(buffer, (at += count * 4), count);
  const offsets = new Uint32Array(buffer, (at += count * 4), count + 1);
  const owners = new Uint16Array(buffer, (at += (count + 1) * 4), count);
  const titles = new Uint8Array(buffer, at + count * 2);

  const decoder = new TextDecoder();
  for (const category of categories) category.locations = [];
  for (let index = 0; index < count; index++) {
    categories[owners[index]].locations.push({
      id: ids[index],
      title: decoder.decode(titles.subarray(offsets[index], offsets[index + 1])),
      lat: latitudes[index],
      lng: longitudes[index],
      regionId: regions[index] || undefined,
      shard: shards[index] || undefined,
    });
  }
}

// Descriptions and cross-references are half the catalog by weight and are read
// one pin at a time, so a map's are fetched the first time one of its pins is
// opened, and not at all if none ever is.
async function mapText(mapID) {
  if (!state.textByMap.has(mapID)) {
    state.textByMap.set(
      mapID,
      fetch(`/static/catalog/${mapID}.text`).then((r) => r.json()).catch(() => ({})),
    );
  }
  return state.textByMap.get(mapID);
}

async function selectGame(slug, mapSlug) {
  state.game = state.catalog.games.find((game) => game.slug === slug) || state.catalog.games[0];
  // Only an explicitly named map overrides what this game remembers.
  if (!mapSlug && !state.restore) state.restore = readSession(state.game.slug);
  elements.game.value = state.game.slug;
  populateSelect(elements.map, state.game.maps, "title", "id");
  elements.map.disabled = state.game.maps.length === 1;
  elements.map.title = elements.map.disabled ? "This game has one map" : "Choose a map";
  // Returning to a game returns to where it was left, so wandering off to
  // another game and back does not cost the reader their place.
  const remembered = state.restore?.game === state.game.slug ? state.restore : null;
  const wanted = (mapSlug && state.game.maps.find((item) => item.slug === mapSlug)) ||
    (remembered && state.game.maps.find((item) => item.slug === remembered.map));
  await selectMap((wanted || state.game.maps[0]).id);
}

// The address carries where the reader is, and nothing about what they have
// filtered or searched. Reloading therefore lands on the same map with a clean
// legend, which is the quickest way to start a fresh question about it.
//
// A fragment rather than a path: the window has no address bar to make a path
// worth reading, the app can be mounted under a workspace prefix that a pushed
// path would navigate out of, and a fragment cannot 404. Slash-separated
// because both slugs contain dashes.
// The window is reopened far more often than it is refreshed, and reopening it
// to a default view discards work the reader did to reach where they were. The
// whole arrangement is kept: which map, which layer, where the view sits, what
// is filtered out and which groups are folded.
const sessionKey = "atlas.session";

// Kept per game rather than as a single place, so wandering off to another
// game and coming back does not cost the reader where they were in this one.
function saveSession() {
  // Nothing is written while a map is being swapped in: the arrangement is
  // half the old one and half the new until it settles.
  if (!state.map || !state.variant || state.settling) return;
  const view = state.engine?.getView();
  try {
    const stored = allSessions();
    stored.last = state.game.slug;
    stored.games[state.game.slug] = {
      game: state.game.slug,
      map: state.map.slug,
      variant: state.map.variants.indexOf(state.variant),
      center: view?.getCenter(),
      zoom: view?.getZoom(),
      hidden: [...state.hiddenCategories],
      collapsed: [...state.collapsedSections],
      overviewDocked: state.overviewDocked,
    };
    localStorage.setItem(sessionKey, JSON.stringify(stored));
  } catch {
    // A browsing session that cannot be written is not worth failing over.
  }
}

function allSessions() {
  try {
    const stored = JSON.parse(localStorage.getItem(sessionKey) || "null");
    if (stored && stored.games) return stored;
  } catch {
    // Falls through to a fresh record.
  }
  return { last: "", games: {} };
}

function readSession(gameSlug) {
  const stored = allSessions();
  const wanted = gameSlug || stored.last;
  const entry = wanted && stored.games[wanted];
  return entry && entry.game && entry.map ? entry : null;
}

function readRoute() {
  const [game, map] = decodeURIComponent(location.hash.replace(/^#\/?/, ""))
    .split("/")
    .map((part) => part.trim());
  return { game, map };
}

function writeRoute() {
  if (!state.game || !state.map) return;
  const route = `#${state.game.slug}/${state.map.slug}`;
  if (location.hash === route) return;
  // Replaced rather than pushed: this records a location, not a trail.
  history.replaceState(null, "", route);
}

async function selectMap(id) {
  const entry = state.game.maps.find((map) => map.id === id) || state.game.maps[0];
  // A map opened while another is still arriving must not be overtaken by it.
  const run = ++state.mapRun;
  elements.loading.hidden = false;
  const loaded = await loadMap(entry);
  if (state.mapRun !== run) return;

  state.settling = true;
  state.map = loaded;
  document.documentElement.style.setProperty("--icon-outset-color", iconOutsetColor());
  elements.map.value = String(state.map.id);
  populateSelect(elements.variant, state.map.variants, "name");
  elements.variantField.hidden = state.map.variants.length < 2;
  state.styleCache.clear();
  setHoveredPin(null);
  state.gridPrefix = "";
  state.hiddenCategories.clear();
  // Zones are a navigation aid, not the primary filter surface: keep boundaries
  // drawn but fold the index away so pin groups stay above the fold.
  state.collapsedSections.clear();
  state.collapsedSections.add("zones");
  for (const group of state.map.groups) {
    for (const category of group.categories) {
      if (!category.visible) state.hiddenCategories.add(category.id);
    }
  }
  const restore = state.restore?.map === state.map.slug ? state.restore : null;
  if (restore) {
    state.hiddenCategories = new Set(restore.hidden);
    state.collapsedSections = new Set(restore.collapsed);
  }
  // Where the corner of the screen is wanted is a preference about the game
  // rather than about one of its maps, so it carries across them.
  setOverviewDocked(Boolean(state.restore?.overviewDocked), false);
  state.search = "";
  elements.search.value = "";
  elements.searchResults.hidden = true;
  closeDetail();
  renderLegend();
  renderZones();
  buildPins();
  selectVariant(restore ? clamp(restore.variant, 0, state.map.variants.length - 1) : 0, true);
  elements.gameTitle.textContent = state.game.title;
  elements.title.textContent = state.map.title;
  elements.meta.textContent =
    `${formatNumber(state.map.pinCount)} locations · ${formatNumber(state.map.zones?.length || 0)} regions`;
  elements.sidebar.classList.remove("is-open");
  elements.mobileLegend.setAttribute("aria-expanded", "false");
  writeRoute();
  state.settling = false;
  saveSession();
}

function selectVariant(index, resetView = false) {
  const priorVariant = state.variant;
  state.variant = state.map.variants[index] || state.map.variants[0];
  const variant = state.variant;
  const carried = resetView ? null : carryViewAcrossShards(priorVariant, variant);
  const maxViewZoom = viewMaxZoom(variant);
  elements.variant.value = String(state.map.variants.indexOf(state.variant));
  if (resetView) state.engine.setView(createView(maxViewZoom));
  else {
    state.engine.getView().setMinZoom(variant.minZoom);
    state.engine.getView().setMaxZoom(maxViewZoom);
  }

  const tileRun = ++state.tileRun;
  let pending = 0;
  state.tileStats = { requested: 0, loaded: 0, errors: 0, peakPending: 0 };
  elements.loading.hidden = false;
  const trackLoading = (source) => {
    source.on("tileloadstart", () => {
      if (state.tileRun !== tileRun) return;
      pending++;
      state.tileStats.requested++;
      state.tileStats.peakPending = Math.max(state.tileStats.peakPending, pending);
    });
    const tileFinished = (failed) => {
      if (state.tileRun !== tileRun) return;
      pending = Math.max(0, pending - 1);
      if (failed) state.tileStats.errors++;
      else state.tileStats.loaded++;
      if (pending === 0 && state.tileStats.loaded > 0) elements.loading.hidden = true;
    };
    source.on("tileloadend", () => tileFinished(false));
    source.on("tileloaderror", () => tileFinished(true));
    return source;
  };

  const buildSource = (maxLevel, wanted) => trackLoading(new XYZ({
    projection: state.projection,
    tileGrid: new TileGrid({
      extent: [0, -state.catalog.tileGrid.size, state.catalog.tileGrid.size, 0],
      origin: [0, 0],
      // Keep the source grid native. Views may overzoom this last resolution,
      // but no nonexistent tile level is requested.
      resolutions: resolutions(maxLevel),
      tileSize: state.catalog.tileGrid.tileSize,
    }),
    cacheSize: 64,
    interpolate: variant.interpolate,
    transition: 0,
    wrapX: false,
    tileUrlFunction: ([zoom, x, y]) => {
      const format = variant.formats[zoom];
      if (!format || x < 0 || y < 0 || !wanted(zoom, x, y)) return undefined;
      return `/static/tiles/${encodeURIComponent(variant.tiles)}/${zoom}/${x}/${y}.${format}`;
    },
  }));

  const fullZoom = variant.fullZoom ?? variant.maxZoom;
  const base = buildSource(fullZoom, (zoom, x, y) => tileExists(variant, zoom, x, y));
  const previous = state.layers.raster.getSource();
  state.layers.raster.setSource(base);
  state.layers.raster.setExtent(activeExtent());
  // Tiles matching the map's background were never written; painting that
  // colour behind the layer makes their absence invisible.
  state.layers.raster.setBackground(variant.background || undefined);
  previous?.clear();

  const previousDetail = state.layers.rasterDetail.getSource();
  if (variant.maxZoom > fullZoom) {
    state.layers.rasterDetail.setSource(buildSource(
      variant.maxZoom,
      (zoom, x, y) => zoom > fullZoom && tileExists(variant, zoom, x, y),
    ));
    state.layers.rasterDetail.setExtent(activeExtent());
    state.layers.rasterDetail.setVisible(true);
  } else {
    state.layers.rasterDetail.setVisible(false);
  }
  previousDetail?.clear();
  renderGrid();
  if (state.gridEnabled) {
    refreshPrioritySource();
    state.layers.pins.changed();
    state.layers.text.changed();
    state.layers.priority.changed();
  }
  state.overviewKey = "";
  renderOverview();
  const resume = state.restore?.map === state.map.slug ? state.restore : null;
  // Zones and pins are built before a variant is chosen, so on a split map the
  // first render shows every layer at once. Comparing against what was actually
  // rendered catches that as well as a later switch between layers.
  if (state.renderedShard !== (variant.shard || 0)) {
    renderZones();
    applyPinFilters();
  }
  if (resume?.center && Number.isFinite(resume.zoom)) {
    // Resuming: land exactly where the reader left the view, rather than
    // fitting the map and losing where they were.
    const view = state.engine.getView();
    view.setCenter(resume.center);
    view.setZoom(resume.zoom);
    state.fitZoom = resume.zoom;
    updateVisibleCount();
  } else if (resetView) {
    requestAnimationFrame(fitMap);
  } else if (carried) {
    state.engine.getView().animate({ center: carried, duration: 200 });
    updateVisibleCount();
  } else {
    updateVisibleCount();
  }
  saveSession();
}

// Levels are sparse: background tiles are never written, and levels above
// fullZoom exist only where the capture reached. Asking for a tile that was
// never emitted would be a wasted request and a 404, so consult the coverage
// bitset first and let OpenLayers show the parent tile instead.
function tileExists(variant, zoom, x, y) {
  const coverage = variant.coverage?.[zoom];
  if (!coverage) return true;
  const column = x - coverage.x;
  const row = y - coverage.y;
  if (column < 0 || row < 0 || column >= coverage.w || row >= coverage.h) return false;
  const bits = coverageBits(variant, zoom, coverage);
  const index = row * coverage.w + column;
  return (bits[index >> 3] & (1 << (index & 7))) !== 0;
}

function coverageBits(variant, zoom, coverage) {
  if (!coverage.decoded) {
    const binary = atob(coverage.bits);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
    coverage.decoded = bytes;
  }
  return coverage.decoded;
}

function resolutions(maxZoom) {
  const base = state.catalog.tileGrid.size / state.catalog.tileGrid.tileSize;
  return Array.from({ length: maxZoom + 1 }, (_, zoom) => base / (2 ** zoom));
}

function viewMaxZoom(variant) {
  return variant.maxZoom + overzoomLevels;
}

// The overview is drawn once per variant from the shallowest pyramid level big
// enough to read, then only the viewport rectangle moves.
const overviewTargetSize = 168;

function renderOverview() {
  const variant = state.variant;
  const canvas = elements.overviewCanvas;
  const context = canvas.getContext("2d");
  const extent = activeExtent();
  const world = state.catalog.tileGrid.size;
  const tileSize = state.catalog.tileGrid.tileSize;
  const width = extent[2] - extent[0];
  const height = extent[3] - extent[1];

  let level = 0;
  while (level < variant.fullZoom &&
    (Math.max(width, height) / world) * tileSize * (2 ** level) < overviewTargetSize) {
    level++;
  }
  const scale = (tileSize * (2 ** level)) / world;
  canvas.width = Math.max(1, Math.round(width * scale));
  canvas.height = Math.max(1, Math.round(height * scale));
  context.clearRect(0, 0, canvas.width, canvas.height);
  if (variant.background) {
    context.fillStyle = variant.background;
    context.fillRect(0, 0, canvas.width, canvas.height);
  }

  const run = ++state.overviewRun;
  const format = variant.formats[level];
  const first = Math.floor((extent[0] / world) * (2 ** level));
  const last = Math.ceil((extent[2] / world) * (2 ** level));
  const top = Math.floor((-extent[3] / world) * (2 ** level));
  const bottom = Math.ceil((-extent[1] / world) * (2 ** level));
  for (let y = top; y < bottom; y++) {
    for (let x = first; x < last; x++) {
      if (!format || !tileExists(variant, level, x, y)) continue;
      const image = new Image();
      image.onload = () => {
        if (state.overviewRun !== run) return;
        context.drawImage(
          image,
          x * tileSize - extent[0] * scale,
          y * tileSize + extent[3] * scale,
          tileSize,
          tileSize,
        );
      };
      image.src = `/static/tiles/${encodeURIComponent(variant.tiles)}/${level}/${x}/${y}.${format}`;
    }
  }
  updateOverviewViewport();
}

// Hidden while the whole map is on screen: a locator that says "all of it"
// tells the reader nothing they cannot already see.
function updateOverviewViewport() {
  const view = state.engine?.getView();
  if (!view || !state.variant) return;
  const extent = activeExtent();
  const visible = view.calculateExtent(state.engine.getSize());
  const width = extent[2] - extent[0];
  const height = extent[3] - extent[1];
  // Fitting the map lands on its extent to within a fraction of a pixel, so an
  // exact comparison reports the whole map as not quite visible and leaves a
  // locator on screen that marks the entire map.
  const slack = (view.getResolution() || 0) * 4;
  const covered =
    visible[0] <= extent[0] + slack && visible[1] <= extent[1] + slack &&
    visible[2] >= extent[2] - slack && visible[3] >= extent[3] - slack;
  const key = covered ? "hidden" : visible.map(Math.round).join(",");
  if (key === state.overviewKey) return;
  state.overviewKey = key;
  // The handle goes with it: there is nothing to send for while the whole map
  // is already on screen.
  elements.overviewShelf.hidden = covered;
  if (covered) return;

  const canvas = elements.overviewCanvas;
  const left = clamp((visible[0] - extent[0]) / width, 0, 1);
  const right = clamp((visible[2] - extent[0]) / width, 0, 1);
  const upper = clamp((extent[3] - visible[3]) / height, 0, 1);
  const lower = clamp((extent[3] - visible[1]) / height, 0, 1);
  const box = elements.overviewViewport.style;
  box.left = `${left * canvas.width}px`;
  box.top = `${upper * canvas.height}px`;
  // A floor keeps the rectangle findable when the view is deep enough that a
  // true-to-scale box would be a couple of pixels.
  box.width = `${Math.max(8, (right - left) * canvas.width)}px`;
  box.height = `${Math.max(8, (lower - upper) * canvas.height)}px`;
}

// Put away rather than switched off: the overview keeps drawing, and the handle
// it leaves behind is the whole of the way back. A reader who wants the corner
// of their map usually wants it for that map's whole session, so the choice is
// remembered with the rest of what a game is left in.
function setOverviewDocked(docked, remember = true) {
  state.overviewDocked = docked;
  // A sweep in progress ends where it is rather than following the panel out.
  if (docked && state.overviewPointer !== null) {
    state.overviewPointer = null;
    settleView();
  }
  elements.overviewShelf.classList.toggle("is-docked", docked);
  elements.overviewDock.setAttribute("aria-expanded", String(!docked));
  const label = docked ? "Bring the overview back" : "Put the overview away";
  elements.overviewDock.setAttribute("aria-label", label);
  elements.overviewDock.title = label;
  if (remember) saveSession();
}

// The press that opens a gesture eases across, so a plain click reads as a
// move rather than a cut. Everything after it lands at once: a view that eased
// its way to each new position would trail behind the hand steering it.
function overviewNavigate(event, ease) {
  const view = state.engine?.getView();
  if (!view || !state.variant) return;
  const rect = elements.overviewCanvas.getBoundingClientRect();
  const extent = activeExtent();
  const fraction = (value, low, high) => clamp((value - low) / (high - low), 0, 1);
  const center = [
    extent[0] + fraction(event.clientX, rect.left, rect.right) * (extent[2] - extent[0]),
    extent[3] - fraction(event.clientY, rect.top, rect.bottom) * (extent[3] - extent[1]),
  ];
  if (ease) {
    view.animate({ center, duration: 180 });
    return;
  }
  view.cancelAnimations();
  view.setCenter(center);
}

// What a view owes the rest of the page once it has come to rest.
function settleView() {
  updateVisibleCount();
  saveSession();
}

// The layers of a split map are the same ground at different heights, stacked
// down one sheet. Switching between them should leave the reader over the same
// place, so the view is carried across by its position within each layer's box
// rather than by its absolute coordinates.
function carryViewAcrossShards(previous, next) {
  if (!previous || !next || !previous.shard || !next.shard) return null;
  if (previous.shard === next.shard || !previous.bounds || !next.bounds) return null;
  const view = state.engine?.getView();
  const centre = view?.getCenter();
  if (!centre) return null;
  const across = (centre[0] - previous.bounds.x) / previous.bounds.width;
  const down = (-centre[1] - previous.bounds.y) / previous.bounds.height;
  return [
    next.bounds.x + clamp(across, 0, 1) * next.bounds.width,
    -(next.bounds.y + clamp(down, 0, 1) * next.bounds.height),
  ];
}

function toggleSidebar() {
  const shell = document.querySelector(".app-shell");
  const collapsed = shell.classList.toggle("sidebar-collapsed");
  elements.mobileLegend.setAttribute("aria-expanded", String(!collapsed));
  // The map keeps its own idea of how big it is.
  requestAnimationFrame(() => state.engine?.updateSize());
}

function activeExtent() {
  const size = state.catalog.tileGrid.size;
  const bounds = state.variant?.bounds || { x: 0, y: 0, width: size, height: size };
  return [bounds.x, -(bounds.y + bounds.height), bounds.x + bounds.width, -bounds.y];
}

// What the grid divides is the ground the map covers, which is not the window
// its raster is cut from: a piece of a sheet keeps a margin so the title drawn
// beside it survives the crop. On a big map that margin is nothing; on a small
// one it is a fifth of the width, and the cells over it are cells the map never
// gets -- b naming blank sheet while m carries what b should have held.
function gridExtent() {
  const surface = state.variant?.surface;
  if (!surface) return activeExtent();
  return [
    surface.x,
    -(surface.y + surface.height),
    surface.x + surface.width,
    -surface.y,
  ];
}

function handleGridKey(event) {
  if (event.key.toLocaleLowerCase() === "g") {
    event.preventDefault();
    toggleGrid();
    // The grid is opened to go somewhere, so the field that takes a hash is
    // ready for the rest of what is being typed: "gm6" arrives at m6. Closing
    // hands the keyboard back to the map, where the shortcuts live.
    if (state.gridEnabled) {
      elements.gridInput.focus();
      elements.gridInput.select();
    } else {
      elements.viewport.focus({ preventScroll: true });
    }
    return true;
  }
  // Space is about what the chosen cell is divided into, not where the reader
  // is: the cell stays chosen and the pins outside it stay put away, while the
  // subdivision inside it gets out of the way of the map underneath.
  // The button answers its own space bar, and would otherwise be pressed twice.
  if (event.key === " " && state.gridEnabled && event.target !== elements.subgridToggle) {
    event.preventDefault();
    setSubgridVisible(!state.subgridVisible);
    return true;
  }
  if (event.key === "Escape" && state.gridEnabled) {
    event.preventDefault();
    ascendGrid();
    return true;
  }
  return false;
}

function toggleGrid(enabled = !state.gridEnabled) {
  state.gridEnabled = enabled;
  // Opening the grid divides the cell again. A reader who put the subgrid away
  // and then closed the grid altogether is starting over, not resuming.
  if (enabled) setSubgridVisible(true);
  renderGrid();
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.text.changed();
  state.layers.priority.changed();
}

// Dividing the chosen cell and being held to it are two questions. The
// subdivision is how a reader picks the next place; once picked, the mesh over
// the map is often the last thing they want to look at, and until now the only
// way to be rid of it was to give up the scope it was holding. Nothing is
// really hidden: the cell keeps its boundary and the ancestors keep theirs.
function setSubgridVisible(visible) {
  state.subgridVisible = visible;
  state.layers.grid.changed();
  state.layers.gridContext.changed();
  elements.subgridToggle.setAttribute("aria-pressed", String(visible));
  elements.subgridToggle.setAttribute("aria-label",
    visible ? "Hide the subgrid" : "Show the subgrid");
  updateGridHint();
}

function updateGridHint() {
  if (!state.gridEnabled) {
    elements.gridHint.textContent = "G · grid off";
    return;
  }
  // Compact while it is carrying a hash: the mode, the place and the state of
  // the subdivision are one reading, not three separate ones.
  elements.gridHint.textContent = `G-${state.gridPrefix || "root"}` +
    (state.subgridVisible ? "" : " no subgrid");
}

function selectGridPrefix(prefix) {
  const maximum = gridMaxDepth();
  const normalized = [...prefix.toLocaleLowerCase()]
    .filter((character) => geohashAlphabet.includes(character))
    .slice(0, maximum)
    .join("");
  const changed = normalized !== state.gridPrefix;
  state.gridPrefix = normalized;
  if (!state.gridEnabled) state.gridEnabled = true;
  renderGrid();
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.text.changed();
  state.layers.priority.changed();
  if (!changed) return;
  closeDetail();
  state.engine.getView().fit(currentGridExtent(), {
    size: state.engine.getSize(),
    padding: [52, 52, 52, 52],
    maxZoom: viewMaxZoom(state.variant),
    duration: 180,
  });
}

function ascendGrid() {
  if (!state.gridEnabled) return;
  if (!state.gridPrefix) {
    toggleGrid(false);
    return;
  }
  selectGridPrefix(state.gridPrefix.slice(0, -1));
}

function renderGrid() {
  if (!state.sources) return;
  state.sources.grid.clear();
  state.sources.gridContext.clear();
  elements.gridNavigator.hidden = !state.gridEnabled;
  updateGridHint();
  if (!state.gridEnabled || !state.variant) return;

  const maximum = gridMaxDepth();
  elements.gridInput.maxLength = maximum;
  elements.gridInput.value = state.gridPrefix;
  elements.gridBack.title = state.gridPrefix ? "Back one geohash level" : "Close geohash grid";

  renderGridContext();
  if (state.gridPrefix.length >= maximum) {
    addGridFeature(state.gridPrefix, currentGridExtent(), "leaf");
    return;
  }
  // The cell the reader is inside, outlined rather than tiled. It is the one
  // part of the grid that survives putting the grid away: what is on screen is
  // still a chosen place, and a boundary says so where a bare map does not.
  if (state.gridPrefix) {
    addGridFeature(state.gridPrefix, currentGridExtent(), "scope");
  }
  for (const character of geohashAlphabet) {
    const hash = state.gridPrefix + character;
    addGridFeature(hash, geohashExtent(hash), "child");
  }
}

function renderGridContext() {
  for (let depth = 0; depth < state.gridPrefix.length; depth++) {
    const parent = state.gridPrefix.slice(0, depth);
    const selected = state.gridPrefix.slice(0, depth + 1);
    for (const character of geohashAlphabet) {
      const hash = parent + character;
      if (hash === selected) continue;
      addGridFeature(hash, geohashExtent(hash), "neighbor", state.gridPrefix.length - depth);
    }
  }
}

function addGridFeature(hash, extent, role, contextDistance = 0) {
  const [minimumX, minimumY, maximumX, maximumY] = extent;
  const count = state.pins.filter((pin) => {
    if (pin.filteredHidden) return false;
    const [x, y] = pin.coordinate;
    return x >= minimumX && x <= maximumX && y >= minimumY && y <= maximumY;
  }).length;
  const feature = new Feature({
    geometry: new Polygon([[
      [minimumX, minimumY],
      [minimumX, maximumY],
      [maximumX, maximumY],
      [maximumX, minimumY],
      [minimumX, minimumY],
    ]]),
    gridCell: { hash, extent, role, count, contextDistance },
    priority: role === "neighbor"
      ? -contextDistance * 100 + geohashAlphabet.indexOf(hash[hash.length - 1])
      : geohashAlphabet.indexOf(hash[hash.length - 1]),
  });
  const source = role === "neighbor" ? state.sources.gridContext : state.sources.grid;
  source.addFeature(feature);
}

function currentGridExtent() {
  return geohashExtent(state.gridPrefix);
}

function geohashExtent(hash) {
  const extent = [...gridExtent()];
  let splitX = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of [16, 8, 4, 2, 1]) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (value & mask) extent[0] = middle;
        else extent[2] = middle;
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (value & mask) extent[1] = middle;
        else extent[3] = middle;
      }
      splitX = !splitX;
    }
  }
  return extent;
}

function gridMaxDepth() {
  return geohashMaxDepth;
}

function pinInGridCell(pin) {
  if (!state.gridEnabled || !state.gridPrefix) return false;
  const extent = currentGridExtent();
  const [x, y] = pin.coordinate;
  return x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3];
}

function populateSelect(select, items, labelKey, valueKey) {
  select.replaceChildren();
  items.forEach((item, index) => {
    const option = document.createElement("option");
    option.value = valueKey ? String(item[valueKey]) : (item.slug || String(index));
    option.textContent = item[labelKey];
    select.append(option);
  });
}

// Categories drawn as text are labels for the ground itself -- Area, Region,
// Province -- and are read and edited as one set. Gathered here rather than
// left in place, where "Area" sits between Altar and Bank and reads like
// another kind of marker.
function legendSections(groups) {
  const sections = [];
  const text = [];
  for (const group of groups) {
    const drawn = [];
    for (const category of group.categories) {
      (category.displayType === "text" ? text : drawn).push(category);
    }
    if (drawn.length) {
      sections.push({ key: `group-${group.id}`, title: group.title, categories: drawn });
    }
  }
  // Above the pin groups, under the zones: labels and boundaries both say where
  // you are, rather than what is worth going to.
  if (text.length) sections.unshift({ key: "text", title: "Text", categories: text });
  return sections;
}

function renderLegend() {
  const fragment = document.createDocumentFragment();
  for (const section of state.map.sections) {
    const element = document.createElement("section");
    element.className = "layer-section";
    element.dataset.layerSection = section.key;
    const locations = section.categories.reduce((total, category) => total + category.locations.length, 0);
    element.append(layerHeader(section.key, section.title, locations));
    const toggles = document.createElement("div");
    toggles.className = "category-toggles";
    for (const category of section.categories) toggles.append(categoryToggle(category));
    element.append(toggles);
    fragment.append(element);
  }
  elements.legend.replaceChildren(fragment);
  syncSectionCollapse();
  syncSectionSwitches();
}

// Mirrors the markup of the static zones header so every layer section reads the
// same: disclosure on the left, one switch on the right.
function layerHeader(key, title, count) {
  const header = document.createElement("div");
  header.className = "layer-header";

  const disclosure = document.createElement("button");
  disclosure.type = "button";
  disclosure.className = "layer-title";
  disclosure.dataset.section = key;
  const chevron = document.createElement("span");
  chevron.className = "layer-chevron";
  chevron.setAttribute("aria-hidden", "true");
  chevron.innerHTML = '<svg viewBox="0 0 24 24"><path d="m9 6 6 6-6 6"/></svg>';
  const name = document.createElement("span");
  name.textContent = title;
  const total = document.createElement("span");
  total.className = "layer-count";
  total.textContent = formatNumber(count);
  disclosure.append(chevron, name, total);

  const only = onlyButton(`Show only ${title}`);
  only.dataset.onlySection = key;

  const toggle = document.createElement("label");
  toggle.className = "layer-switch";
  const checkbox = document.createElement("input");
  checkbox.type = "checkbox";
  checkbox.dataset.sectionToggle = key;
  checkbox.setAttribute("aria-label", `Show ${title}`);
  const knob = document.createElement("span");
  knob.setAttribute("aria-hidden", "true");
  toggle.append(checkbox, knob);

  header.append(disclosure, only, toggle);
  return header;
}

// A target rather than the word "only": the legend's remaining words are then
// all things the map is actually about. The label arrives on rest, for anyone
// meeting the icon for the first time.
function onlyButton(label) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "only-button";
  button.dataset.label = label;
  button.setAttribute("aria-label", label);
  button.innerHTML =
    '<svg viewBox="0 0 16 16" aria-hidden="true">' +
    '<circle cx="8" cy="8" r="4.6"/><circle cx="8" cy="8" r="1.3" fill="currentColor" stroke="none"/>' +
    '<path d="M8 1v2M8 13v2M1 8h2M13 8h2"/></svg>';
  return button;
}

function categoryToggle(category) {
  const isText = category.displayType === "text";
  const row = document.createElement("label");
  row.className = "category-row";
  applyCategoryVisual(row, category);
  const checkbox = document.createElement("input");
  checkbox.type = "checkbox";
  checkbox.dataset.category = String(category.id);
  checkbox.checked = !state.hiddenCategories.has(category.id);
  const icon = document.createElement("span");
  if (isText) {
    icon.className = "text-symbol";
    icon.textContent = "Tt";
    icon.title = "Drawn as a text label";
  } else {
    icon.className = "category-icon";
    applyCategoryGlyph(icon, category, initials(category.title));
    icon.title = category.icon || category.title;
  }
  const name = document.createElement("span");
  name.className = "category-name";
  name.textContent = category.title;
  const locations = document.createElement("span");
  locations.className = "category-count";
  locations.textContent = formatNumber(category.locations.length);
  // Overlaid on the count rather than appended: these pills wrap, and a row
  // that grows on hover would shove the one under the cursor somewhere else.
  const only = onlyButton(`Show only ${category.title}`);
  only.dataset.onlyCategory = String(category.id);
  row.append(checkbox, icon, name, only, locations);
  return row;
}

// There is one level of nesting here -- sections hold rows, not more sections --
// so folding by a depth and folding entirely are the same move, and only the
// one exists.
function setAllSectionsCollapsed(collapsed) {
  state.collapsedSections.clear();
  if (collapsed) {
    for (const button of elements.layers.querySelectorAll("[data-section]")) {
      state.collapsedSections.add(button.dataset.section);
    }
  }
  syncSectionCollapse();
}

function syncSectionCollapse() {
  for (const button of elements.layers.querySelectorAll("[data-section]")) {
    const collapsed = state.collapsedSections.has(button.dataset.section);
    button.setAttribute("aria-expanded", String(!collapsed));
    button.closest(".layer-section").classList.toggle("is-collapsed", collapsed);
  }
  saveSession();
}

function buildPins() {
  state.sources.pins.clear();
  state.sources.text.clear();
  state.sources.priority.clear();
  state.pins = [];
  state.pinByID.clear();
  for (const group of state.map.groups) {
    for (const category of group.categories) {
      if (category.displayType !== "text") prepareMarkerIcon(category);
      for (const location of category.locations) {
        const pin = {
          location,
          category,
          group,
          coordinate: project(location.lat, location.lng),
          filteredHidden: false,
          priority: pinPriority(category, location),
          feature: null,
        };
        pin.feature = new Feature({
          geometry: new Point(pin.coordinate),
          pin,
          priority: pin.priority,
        });
        state.pins.push(pin);
        state.pinByID.set(location.id, pin);
        if (category.displayType === "text") state.sources.text.addFeature(pin.feature);
        else state.sources.pins.addFeature(pin.feature);
      }
    }
  }
  applyPinFilters();
}

function renderZones() {
  const zones = (state.map.zones || []).filter(onActiveShard);
  state.renderedShard = state.variant?.shard || 0;
  state.sources.zones.clear();
  state.sources.zoneTitles.clear();
  state.zoneRecords.clear();
  state.highlightedZones.clear();
  state.focusedZoneID = null;
  state.zoneTitleCount = 0;
  setZonesVisible(true);
  elements.zoneIndex.hidden = zones.length === 0;
  elements.zoneCount.textContent = formatNumber(zones.length);

  for (const zone of zones) {
    const zoneExtent = createEmpty();
    const geometries = [];
    let hasGeometry = false;
    for (const rawGeometry of zone.features || []) {
      const geometry = projectZoneGeometry(rawGeometry);
      if (!geometry) continue;
      hasGeometry = true;
      geometries.push(geometry);
      extend(zoneExtent, geometry.getExtent());
      state.sources.zones.addFeature(new Feature({
        geometry,
        zone,
        color: colorFor(zone.id),
        child: zone.parentRegionId != null,
      }));
    }
    if (!hasGeometry) continue;
    const color = colorFor(zone.id);
    const center = zone.center ? project(zone.center.lat, zone.center.lng) : getCenter(zoneExtent);
    const span = Math.max(zoneExtent[2] - zoneExtent[0], zoneExtent[3] - zoneExtent[1]);
    state.sources.zoneTitles.addFeature(new Feature({
      geometry: new Point(center),
      zone,
      color,
      child: zone.parentRegionId != null,
      span,
      priority: (zone.parentRegionId == null ? 2_000_000 : 1_000_000) + Math.round(span),
    }));
    state.zoneRecords.set(zone.id, {
      zone,
      extent: zoneExtent.slice(),
      geometries,
      color,
    });
  }
  state.zoneTitleCount = state.sources.zoneTitles.getFeatures().length;
  renderZoneScrim();
  renderZoneIndex();
}

// Highlighting a zone is a request to look at one place, so everything else
// recedes: the whole map is covered and the zone is cut back out of it.
// Shading only the neighbouring zones demoted them rather than raising the
// zone asked for, and left the unclaimed space between them at full strength.
function renderZoneScrim() {
  state.sources.zoneScrim.clear();
  if (!state.highlightedZones.size) return;
  const [left, bottom, right, top] = state.projection.getExtent();
  // Rings alternate direction with their depth so the fill counts its way in
  // and out: the map, then the zone as a hole, then any hole of the zone's own
  // back to solid.
  const rings = [orientRing([
    [left, bottom], [right, bottom], [right, top], [left, top], [left, bottom],
  ], true)];
  for (const zoneID of state.highlightedZones) {
    for (const geometry of state.zoneRecords.get(zoneID)?.geometries || []) {
      const polygons = geometry.getType() === "MultiPolygon"
        ? geometry.getPolygons()
        : [geometry];
      for (const polygon of polygons) {
        polygon.getLinearRings().forEach((ring, index) => {
          rings.push(orientRing(ring.getCoordinates(), index > 0));
        });
      }
    }
  }
  state.sources.zoneScrim.addFeature(new Feature({ geometry: new Polygon(rings) }));
}

// Twice the signed area of a ring: negative when its points run clockwise.
function orientRing(ring, clockwise) {
  let area = 0;
  for (let index = 0, prior = ring.length - 1; index < ring.length; prior = index++) {
    area += ring[prior][0] * ring[index][1] - ring[index][0] * ring[prior][1];
  }
  return (area < 0) === clockwise ? ring : ring.slice().reverse();
}

function renderZoneIndex() {
  const fragment = document.createDocumentFragment();
  for (const { zone, depth } of orderedZones()) {
    const record = state.zoneRecords.get(zone.id);
    if (!record) continue;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "zone-index-item";
    button.dataset.zone = String(zone.id);
    button.style.setProperty("--zone-color", record.color);
    button.style.setProperty("--zone-depth", String(depth));
    button.setAttribute("aria-pressed", "false");
    button.title = `${zone.title}: click to jump; right-click to toggle highlight`;

    const marker = document.createElement("span");
    marker.className = "zone-index-marker";
    marker.setAttribute("aria-hidden", "true");
    const title = document.createElement("span");
    title.textContent = zone.title;
    button.append(marker, title);
    fragment.append(button);
  }
  elements.zoneList.replaceChildren(fragment);
  elements.zoneIndex.hidden = state.zoneRecords.size === 0;
}

function orderedZones() {
  const zones = [...state.zoneRecords.values()].map((record) => record.zone);
  const zoneIDs = new Set(zones.map((zone) => zone.id));
  const children = new Map();
  for (const zone of zones) {
    const parentID = zoneIDs.has(zone.parentRegionId) ? zone.parentRegionId : null;
    if (!children.has(parentID)) children.set(parentID, []);
    children.get(parentID).push(zone);
  }
  for (const entries of children.values()) {
    entries.sort((left, right) => left.title.localeCompare(right.title, undefined, {
      numeric: true,
      sensitivity: "base",
    }));
  }

  const ordered = [];
  const visited = new Set();
  const append = (zone, depth) => {
    if (visited.has(zone.id)) return;
    visited.add(zone.id);
    ordered.push({ zone, depth });
    for (const child of children.get(zone.id) || []) append(child, depth + 1);
  };
  for (const zone of children.get(null) || []) append(zone, 0);
  for (const zone of zones) append(zone, 0);
  return ordered;
}

function setZonesVisible(visible) {
  state.zonesVisible = visible;
  elements.zoneToggle.checked = visible;
  state.layers.zoneScrim.setVisible(visible);
  state.layers.zones.setVisible(visible);
  state.layers.zoneTitles.setVisible(visible);
  state.layers.zoneTitleDetail.setVisible(visible);
}

function jumpToZone(zoneID) {
  const record = state.zoneRecords.get(zoneID);
  if (!record) return;
  state.focusedZoneID = zoneID;
  updateZoneIndexState();
  closeDetail();
  state.engine.getView().fit(record.extent, {
    size: state.engine.getSize(),
    padding: [54, 54, 54, 54],
    maxZoom: viewMaxZoom(state.variant),
    duration: 220,
  });
  elements.sidebar.classList.remove("is-open");
  elements.mobileLegend.setAttribute("aria-expanded", "false");
}

function toggleZoneHighlight(zoneID) {
  if (!state.zoneRecords.has(zoneID)) return;
  if (state.highlightedZones.has(zoneID)) state.highlightedZones.delete(zoneID);
  else {
    state.highlightedZones.add(zoneID);
    setZonesVisible(true);
  }
  state.hoveredPin = null;
  updateZonePinFocus();
  refreshPinRendering();
  updateZoneIndexState();
  renderZoneScrim();
  state.layers.zones.changed();
  state.layers.zoneTitles.changed();
  state.layers.zoneTitleDetail.changed();
}

function updateZoneIndexState() {
  elements.zoneIndex.classList.toggle("has-highlights", state.highlightedZones.size > 0);
  for (const button of elements.zoneList.querySelectorAll("[data-zone]")) {
    const zoneID = Number(button.dataset.zone);
    const highlighted = state.highlightedZones.has(zoneID);
    button.classList.toggle("is-highlighted", highlighted);
    button.classList.toggle("is-current", state.focusedZoneID === zoneID);
    button.setAttribute("aria-pressed", String(highlighted));
  }
}

function projectZoneGeometry(rawGeometry) {
  if (!rawGeometry?.coordinates) return null;
  const point = ([longitude, latitude]) => project(latitude, longitude);
  if (rawGeometry.type === "Polygon") {
    return new Polygon(rawGeometry.coordinates.map((ring) => ring.map(point)));
  }
  if (rawGeometry.type === "MultiPolygon") {
    return new MultiPolygon(
      rawGeometry.coordinates.map((polygon) => polygon.map((ring) => ring.map(point))),
    );
  }
  return null;
}

function project(latitude, longitude) {
  const grid = state.catalog.tileGrid;
  const worldTiles = 2 ** grid.sourceZoom;
  const xTile = ((longitude + 180) / 360) * worldTiles;
  const latitudeRadians = latitude * Math.PI / 180;
  const yTile = (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * worldTiles;
  return [
    (xTile - grid.firstTile) * grid.tileSize,
    -(yTile - grid.firstTile) * grid.tileSize,
  ];
}

function applyPinFilters() {
  for (const pin of state.pins) {
    const categoryHidden = state.hiddenCategories.has(pin.category.id);
    const searchHidden = state.search &&
      !pin.location.title.toLocaleLowerCase().includes(state.search);
    pin.filteredHidden = Boolean(categoryHidden || searchHidden);
  }
  updateZonePinFocus();
  refreshPinRendering();
  if (state.selectedPin?.filteredHidden) closeDetail();
}

function updateZonePinFocus() {
  if (!state.highlightedZones.size) {
    for (const pin of state.pins) pin.insideHighlightedZone = false;
    return;
  }
  const records = [...state.highlightedZones]
    .map((zoneID) => state.zoneRecords.get(zoneID))
    .filter(Boolean);
  for (const pin of state.pins) {
    pin.insideHighlightedZone = records.some((record) =>
      record.geometries.some((geometry) => geometryContainsCoordinate(geometry, pin.coordinate)));
  }
}

function geometryContainsCoordinate(geometry, coordinate) {
  if (geometry.intersectsCoordinate(coordinate)) return true;
  const closest = geometry.getClosestPoint(coordinate);
  const x = closest[0] - coordinate[0];
  const y = closest[1] - coordinate[1];
  return x * x + y * y <= 1;
}

function refreshPinRendering() {
  state.eligibleLocations = state.pins.filter((pin) => !pinIsHidden(pin)).length;
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.zonePins.changed();
  state.layers.pinLabels.changed();
  state.layers.text.changed();
  state.layers.zoneText.changed();
  state.layers.textDetail.changed();
  state.layers.priority.changed();
  updateVisibleCount();
}

function refreshPrioritySource() {
  state.sources.priority.clear();
  for (const pin of state.pins) {
    if (pinIsHidden(pin)) continue;
    const searched = Boolean(state.search) &&
      pin.location.title.toLocaleLowerCase().includes(state.search);
    if (pin === state.selectedPin || pin === state.hoveredPin || searched || pinInGridCell(pin)) {
      state.sources.priority.addFeature(pin.feature);
    }
  }
}

function setHoveredPin(pin) {
  if (state.hoveredPin === pin) return;
  state.hoveredPin = pin;
  if (!state.sources) return;
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.priority.changed();
}

function renderSearchResults() {
  if (!state.search) {
    elements.searchResults.hidden = true;
    elements.searchResults.replaceChildren();
    return;
  }
  const matches = state.pins
    .filter((pin) => !state.hiddenCategories.has(pin.category.id) &&
      pin.location.title.toLocaleLowerCase().includes(state.search))
    .sort((a, b) => a.location.title.localeCompare(b.location.title))
    .slice(0, 20);
  const fragment = document.createDocumentFragment();
  if (!matches.length) {
    const empty = document.createElement("p");
    empty.className = "search-empty";
    empty.textContent = "No visible locations match.";
    fragment.append(empty);
  }
  for (const pin of matches) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "search-result";
    button.dataset.location = String(pin.location.id);
    applyCategoryVisual(button, pin.category);
    const dot = document.createElement("span");
    dot.className = "result-dot";
    applyCategoryGlyph(dot, pin.category, initials(pin.category.title));
    const copy = document.createElement("span");
    const title = document.createElement("strong");
    title.textContent = pin.location.title;
    const category = document.createElement("small");
    category.textContent = `${pin.category.title} · ${pin.group.title}`;
    copy.append(title, category);
    button.append(dot, copy);
    fragment.append(button);
  }
  elements.searchResults.replaceChildren(fragment);
  elements.searchResults.hidden = false;
}

function setAllCategories(visible) {
  state.hiddenCategories.clear();
  if (!visible) {
    for (const group of state.map.groups) {
      for (const category of group.categories) state.hiddenCategories.add(category.id);
    }
  }
  syncLegendCheckboxes();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

function syncLegendCheckboxes() {
  for (const checkbox of document.querySelectorAll("[data-category]")) {
    checkbox.checked = !state.hiddenCategories.has(Number(checkbox.dataset.category));
  }
}

function toggleSection(key) {
  const section = state.map.sections.find((item) => item.key === key);
  if (!section) return;
  const hasVisible = section.categories.some((category) => !state.hiddenCategories.has(category.id));
  for (const category of section.categories) {
    if (hasVisible) state.hiddenCategories.add(category.id);
    else state.hiddenCategories.delete(category.id);
  }
  syncLegendCheckboxes();
  applyPinFilters();
  syncSectionSwitches();
}

// Isolating is the common request of a long legend: "just the Korok Seeds",
// out of a hundred and sixty categories. Everything else is hidden rather than
// remembered, so Show all is the single, obvious way back.
function showOnly(target) {
  if (!state.map) return;
  // Asking to isolate what is already isolated means the reader is done with
  // it, so the same control lets them back out.
  if (isOnly(target)) {
    setAllCategories(true);
    return;
  }
  state.hiddenCategories.clear();
  for (const section of state.map.sections) {
    const wanted = target.section === section.key;
    for (const category of section.categories) {
      if (!wanted && target.category !== category.id) {
        state.hiddenCategories.add(category.id);
      }
    }
  }
  syncLegendCheckboxes();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

// True when what is on screen is already exactly what this target would isolate.
function isOnly(target) {
  for (const section of state.map.sections) {
    for (const category of section.categories) {
      const wanted = target.section === section.key || target.category === category.id;
      if (wanted === state.hiddenCategories.has(category.id)) return false;
    }
  }
  return true;
}

// Derived rather than remembered, so the chip is right however the state was
// reached -- including by switching categories off one at a time.
function updateSoloChip() {
  const chip = elements.soloChip;
  if (!state.map) {
    chip.hidden = true;
    return;
  }
  let onlyVisible = null;
  let visibleCount = 0;
  let soleSection = null;
  let sectionsShowing = 0;
  for (const section of state.map.sections) {
    let shown = 0;
    for (const category of section.categories) {
      if (state.hiddenCategories.has(category.id)) continue;
      shown++;
      visibleCount++;
      onlyVisible = category;
    }
    if (shown > 0) {
      sectionsShowing++;
      soleSection = shown === section.categories.length ? section : null;
    }
  }
  let label = "";
  if (visibleCount === 1 && onlyVisible) label = onlyVisible.title;
  else if (sectionsShowing === 1 && soleSection) label = soleSection.title;
  chip.hidden = !label;
  chip.textContent = label ? `only: ${label}` : "";
  chip.title = label ? `Showing only ${label} — click to show everything` : "";
}

function syncSectionSwitches() {
  if (!state.map) return;
  for (const section of state.map.sections) {
    const input = elements.legend.querySelector(`input[data-section-toggle="${section.key}"]`);
    if (input) {
      input.checked = section.categories.some((category) => !state.hiddenCategories.has(category.id));
    }
  }
  updateSoloChip();
  saveSession();
}

function revealPin(pin) {
  state.hiddenCategories.delete(pin.category.id);
  state.search = "";
  elements.search.value = "";
  elements.searchResults.hidden = true;
  syncLegendCheckboxes();
  applyPinFilters();
  syncSectionSwitches();
  showPin(pin, true);
}

function showPin(pin, focus = false) {
  state.selectedPin = pin;
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.pinLabels.changed();
  state.layers.text.changed();
  state.layers.textDetail.changed();
  state.layers.priority.changed();
  elements.detailTitle.textContent = pin.location.title;
  elements.detailCategory.textContent = `${pin.group.title} / ${pin.category.title}`;
  elements.detailDescription.textContent = "";
  elements.detailLinks.hidden = true;
  fillPinText(pin);
  elements.detailID.textContent = String(pin.location.id);
  elements.detailCoordinates.textContent =
    `${pin.location.lat.toFixed(6)}, ${pin.location.lng.toFixed(6)}`;
  applyCategoryVisual(elements.detailDot, pin.category);
  applyCategoryGlyph(elements.detailDot, pin.category, initials(pin.category.title));
  elements.detail.hidden = false;
  if (focus) {
    const view = state.engine.getView();
    view.animate({
      center: pin.coordinate,
      zoom: Math.min(viewMaxZoom(state.variant), Math.max(view.getZoom() || 0, 4)),
      duration: 220,
    });
  }
}

// The words belonging to a pin are fetched the first time one is opened, so a
// map that is only ever looked at never pays for them. The panel opens on what
// is already known and fills in when they arrive; a pin closed or changed in
// the meantime is left alone.
async function fillPinText(pin) {
  const text = await mapText(state.map.id);
  if (state.selectedPin !== pin) return;
  const entry = text[String(pin.location.id)] || {};
  pin.location.description = entry.d || "";
  pin.location.links = entry.l || [];
  elements.detailDescription.textContent =
    cleanDescription(entry.d) || "No description is included in the archive.";
  renderDetailLinks(pin);
}

// The source wrote these as mapgenie URLs. They are rebuilt as in-app jumps so
// they still work with no network, and dropped when the target is not on this
// map.
function renderDetailLinks(pin) {
  const links = (pin.location.links || []).filter((link) => state.pinByID.has(link.locationId));
  elements.detailLinks.hidden = links.length === 0;
  if (!links.length) {
    elements.detailLinks.replaceChildren();
    return;
  }
  const fragment = document.createDocumentFragment();
  for (const link of links) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "detail-link";
    button.dataset.location = String(link.locationId);
    button.textContent = link.title;
    fragment.append(button);
  }
  elements.detailLinks.replaceChildren(fragment);
}

function closeDetail() {
  state.selectedPin = null;
  if (state.sources) {
    refreshPrioritySource();
    state.layers.pins.changed();
    state.layers.pinLabels.changed();
    state.layers.text.changed();
    state.layers.textDetail.changed();
    state.layers.priority.changed();
  }
  elements.detail.hidden = true;
}

function fitMap() {
  if (!state.engine || !state.variant) return;
  const view = state.engine.getView();
  view.fit(activeExtent(), {
    size: state.engine.getSize(),
    padding: [36, 36, 36, 36],
    maxZoom: state.variant.maxZoom,
    duration: 0,
  });
  state.fitZoom = view.getZoom() || 0;
  updateVisibleCount();
}

function changeZoom(delta) {
  const view = state.engine.getView();
  const current = view.getZoom() || 0;
  view.animate({
    zoom: clamp(current + delta, state.variant.minZoom, viewMaxZoom(state.variant)),
    duration: 140,
  });
}

function updateVisibleCount() {
  const zoom = state.engine?.getView().getZoom();
  elements.viewport.dataset.zoom = Number.isFinite(zoom) ? zoom.toFixed(3) : "";
  if (!state.engine || !state.pins.length) {
    elements.visibleCount.textContent = "0 locations visible";
    return;
  }
  const extent = state.engine.getView().calculateExtent(state.engine.getSize());
  let inView = 0;
  for (const pin of state.pins) {
    if (pinIsHidden(pin)) continue;
    const [x, y] = pin.coordinate;
    if (x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3]) inView++;
  }
  elements.visibleCount.textContent =
    `${formatNumber(state.eligibleLocations)} enabled · ${formatNumber(inView)} in view`;
}

function featureOrder(left, right) {
  return (left.get("priority") || 0) - (right.get("priority") || 0);
}

function pinFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone || isPriorityPin(pin)) return null;
  return markerStyles(pin, false);
}

function zonePinFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || !state.highlightedZones.size || !pin.insideHighlightedZone ||
      pinIsHidden(pin) || isPriorityPin(pin)) {
    return null;
  }
  return markerStyles(pin, false);
}

function pinLabelFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || !state.pinLabelsVisible || !atMaximumNativeZoom()) return null;
  return markerLabelStyle(pin);
}

function textFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone || isPriorityPin(pin)) return null;
  if (atMaximumNativeZoom()) return null;
  const minimumZoom = state.fitZoom + Math.log2(textDetailRatio(pin.category));
  if ((state.engine.getView().getZoom() || 0) < minimumZoom) return null;
  return textStyles(pin, false);
}

function textDetailFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin) || pin.insideHighlightedZone ||
      isPriorityPin(pin) || !atMaximumNativeZoom()) {
    return null;
  }
  return textStyles(pin, false);
}

function zoneTextFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || !state.highlightedZones.size || !pin.insideHighlightedZone ||
      pinIsHidden(pin) || isPriorityPin(pin)) {
    return null;
  }
  return textStyles(pin, false);
}

function priorityFeatureStyle(feature) {
  const pin = feature.get("pin");
  if (!pin || pinIsHidden(pin)) return null;
  if (pin.category.displayType === "text") return textStyles(pin, pin === state.selectedPin);
  const marker = markerStyles(pin, pin === state.selectedPin);
  if (pin === state.hoveredPin || pin === state.selectedPin) {
    return [marker, markerLabelStyle(pin)];
  }
  return marker;
}

function markerStyles(pin, selected) {
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

function markerLabelStyle(pin) {
  const key = `marker-label:${pin.location.id}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const style = new Style({
    text: new Text({
      text: pin.location.title,
      offsetY: 21,
      font: '700 10px "Arial Narrow", "Roboto Condensed", sans-serif',
      fill: new Fill({ color: "#f4f6ed" }),
      stroke: new Stroke({ color: "rgba(0,0,0,0.98)", width: 3 }),
      backgroundFill: new Fill({ color: "rgba(10,13,9,0.72)" }),
      padding: [2, 4, 2, 4],
      overflow: true,
    }),
    zIndex: pin.priority,
  });
  state.styleCache.set(key, style);
  return style;
}

function prepareMarkerIcon(category) {
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

function dropMarkerStyles() {
  for (const key of state.styleCache.keys()) {
    if (key.startsWith("marker:")) state.styleCache.delete(key);
  }
}

function markerIconKey(category) {
  return `${category.iconAsset || ""}:${categoryColor(category)}:${state.map?.iconOutset || "light"}`;
}

const outsetColors = {
  light: "rgba(255, 255, 255, 0.96)",
  dark: "rgba(7, 9, 7, 0.98)",
};

function iconOutsetColor() {
  return outsetColors[state.map?.iconOutset === "dark" ? "dark" : "light"];
}

// A marker is drawn in its category's colour and edged with the map's outset,
// so a colour close to that outset leaves the marker with nothing to stand
// against. Rather than edge it the other way -- which would put a pale rim on
// a map that asked for dark ones -- the colour itself is taken to a lighter or
// darker variant of the same hue, so the legend still matches the map.
function legibleIconColor(color) {
  const dark = state.map?.iconOutset === "dark";
  const luminance = relativeLuminance(color);
  if (dark ? luminance > 0.3 : luminance < 0.88) return color;
  return withLightness(color, dark ? 0.74 : 0.42);
}

function withLightness(color, lightness) {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return color;
  const [r, g, b] = [0, 2, 4].map((at) => parseInt(hex.slice(at, at + 2), 16) / 255);
  const high = Math.max(r, g, b);
  const low = Math.min(r, g, b);
  const light = (high + low) / 2;
  let hue = 0;
  const chroma = high - low;
  const saturation = chroma === 0 ? 0 : chroma / (1 - Math.abs(2 * light - 1));
  if (chroma !== 0) {
    if (high === r) hue = ((g - b) / chroma) % 6;
    else if (high === g) hue = (b - r) / chroma + 2;
    else hue = (r - g) / chroma + 4;
    hue *= 60;
    if (hue < 0) hue += 360;
  }
  const c = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = lightness - c / 2;
  const [rr, gg, bb] =
    hue < 60 ? [c, x, 0] : hue < 120 ? [x, c, 0] : hue < 180 ? [0, c, x] :
    hue < 240 ? [0, x, c] : hue < 300 ? [x, 0, c] : [c, 0, x];
  const channel = (value) =>
    Math.round((value + m) * 255).toString(16).padStart(2, "0");
  return `#${channel(rr)}${channel(gg)}${channel(bb)}`;
}

function relativeLuminance(color) {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return 0.5;
  const channel = (offset) => parseInt(hex.slice(offset, offset + 2), 16) / 255;
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4);
}

function textStyles(pin, selected) {
  const key = `text:${pin.location.id}:${selected ? 1 : 0}`;
  if (state.styleCache.has(key)) return state.styleCache.get(key);
  const style = new Style({
    text: new Text({
      text: pin.location.title,
      font: `${selected ? "900" : "800"} 14px "Arial Narrow", "Roboto Condensed", sans-serif`,
      fill: new Fill({ color: selected ? "#d6f36b" : "#f3f5e9" }),
      stroke: new Stroke({ color: "rgba(0,0,0,0.95)", width: 4 }),
      backgroundFill: new Fill({ color: selected ? "rgba(17,21,14,0.9)" : "rgba(10,13,9,0.58)" }),
      backgroundStroke: selected ? new Stroke({ color: "#d6f36b", width: 1 }) : undefined,
      padding: [3, 6, 3, 6],
      overflow: true,
    }),
    zIndex: selected ? 20_000_000 : pin.priority + 3_000_000,
  });
  const styles = [style];
  state.styleCache.set(key, styles);
  return styles;
}

function gridStyle(feature, resolution) {
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
          ? `rgba(3, 5, 3, ${Math.min(0.52, 0.30 + cell.contextDistance * 0.06)})`
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
          color: neighbor ? "rgba(4,6,4,0.88)" : "rgba(9,12,8,0.76)",
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
function labelFitsCell(cell, font, size, padding, resolution) {
  if (!resolution) return true;
  const width = measureLabel(cell.hash, font) + padding[1] + padding[3];
  const height = size + padding[0] + padding[2];
  return width <= (cell.extent[2] - cell.extent[0]) / resolution &&
    height <= (cell.extent[3] - cell.extent[1]) / resolution;
}

const labelRuler = document.createElement("canvas").getContext("2d");
const labelWidths = new Map();

function measureLabel(text, font) {
  const key = `${font}|${text}`;
  let width = labelWidths.get(key);
  if (width === undefined) {
    labelRuler.font = font;
    width = labelRuler.measureText(text).width;
    labelWidths.set(key, width);
  }
  return width;
}

const zoneScrimFill = new Style({ fill: new Fill({ color: "rgba(3, 5, 3, 0.62)" }) });

function zoneScrimStyle() {
  return state.zonesVisible ? zoneScrimFill : null;
}

function zoneStyle(feature) {
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

function zoneTitleStyle(feature) {
  if (!state.zonesVisible || atMaximumNativeZoom()) return null;
  const child = feature.get("child");
  const highlighted = state.highlightedZones.has(feature.get("zone").id);
  const zoom = state.engine.getView().getZoom() || 0;
  if (!highlighted && child && zoom < state.fitZoom + 3) return null;
  const spanPixels = feature.get("span") / state.engine.getView().getResolution();
  if (!highlighted && state.zoneTitleCount > 40 && spanPixels < 52) return null;
  return renderedZoneTitleStyle(feature);
}

function zoneTitleDetailStyle(feature) {
  if (!state.zonesVisible || !atMaximumNativeZoom()) return null;
  return renderedZoneTitleStyle(feature);
}

function renderedZoneTitleStyle(feature) {
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
          ? "#11140f"
          : dimmed
            ? "rgba(185,192,177,0.56)"
            : child ? "#c7ccbd" : "#f1f4e7",
      }),
      stroke: new Stroke({
        color: highlighted
          ? "rgba(255,255,255,0.96)"
          : dimmed ? "rgba(0,0,0,0.82)" : "rgba(0,0,0,0.95)",
        width: highlighted ? 2 : dimmed ? 3 : 4,
      }),
      backgroundFill: new Fill({
        color: highlighted ? color : dimmed ? "rgba(4,6,4,0.84)" : "rgba(11,14,10,0.72)",
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

function zoneContextDimmed(zoneID) {
  if (!state.highlightedZones.size || state.highlightedZones.has(zoneID)) return false;
  for (const highlightedID of state.highlightedZones) {
    if (zoneIsAncestorOf(zoneID, highlightedID)) return false;
  }
  return true;
}

function zoneIsAncestorOf(candidateID, zoneID) {
  const visited = new Set();
  let parentID = state.zoneRecords.get(zoneID)?.zone.parentRegionId;
  while (parentID != null && !visited.has(parentID)) {
    if (parentID === candidateID) return true;
    visited.add(parentID);
    parentID = state.zoneRecords.get(parentID)?.zone.parentRegionId;
  }
  return false;
}

function isPriorityPin(pin) {
  return pin === state.selectedPin || pin === state.hoveredPin ||
    pinInGridCell(pin) ||
    (Boolean(state.search) && pin.location.title.toLocaleLowerCase().includes(state.search));
}

function pinIsHidden(pin) {
  return pin.filteredHidden || pinIsZoneCulled(pin) || pinIsGridCulled(pin) ||
    !onActiveShard(pin.location);
}

// A map split into layers offers one at a time. Anything belonging to another
// layer is elsewhere in the world, not merely filtered out.
function onActiveShard(item) {
  const shard = state.variant?.shard;
  return !shard || !item.shard || item.shard === shard;
}

function pinIsZoneCulled(pin) {
  if (!state.highlightedZones.size || pin.insideHighlightedZone) return false;
  if (pin === state.selectedPin) return false;
  return !(Boolean(state.search) &&
    pin.location.title.toLocaleLowerCase().includes(state.search));
}

// Descending into a cell narrows the question to what is inside it, the same
// way highlighting a zone does. What lies outside was still being drawn, so the
// answer stayed as crowded as it was before the cell was chosen.
function pinIsGridCulled(pin) {
  if (!state.gridEnabled || !state.gridPrefix || pinInGridCell(pin)) return false;
  if (pin === state.selectedPin) return false;
  return !(Boolean(state.search) &&
    pin.location.title.toLocaleLowerCase().includes(state.search));
}

function pinPriority(category, location) {
  const rarity = Math.max(0, 1_000_000 - Math.min(category.locations.length, 999) * 1000);
  return rarity + (stableRank(location.id) % 1000);
}

function textDetailRatio(category) {
  if (category.locations.length > 200) return 4;
  if (category.locations.length > 75) return 2.5;
  return 1;
}

function atMaximumNativeZoom() {
  const zoom = state.engine?.getView().getZoom() || 0;
  return zoom >= (state.variant?.maxZoom || 0) - 0.05;
}

function stableRank(value) {
  return Math.imul(Number(value) || 0, 2654435761) >>> 0;
}

function colorFor(id) {
  const value = Math.abs(Number(id) || 0);
  return palette[(Math.imul(value, 2654435761) >>> 0) % palette.length];
}

function hexToRGBA(value, alpha) {
  const hex = String(value || "#ffffff").replace("#", "");
  const expanded = hex.length === 3
    ? hex.split("").map((character) => character + character).join("")
    : hex.slice(0, 6).padEnd(6, "f");
  const number = Number.parseInt(expanded, 16);
  return `rgba(${(number >> 16) & 255}, ${(number >> 8) & 255}, ${number & 255}, ${alpha})`;
}

function applyCategoryVisual(element, category) {
  element.style.setProperty("--pin-color", categoryColor(category));
}

// One colour for a category wherever it is drawn, so the legend and the map
// agree on what it looks like.
function categoryColor(category) {
  return legibleIconColor(category.color || colorFor(category.id));
}

function applyCategoryGlyph(element, category, fallback) {
  element.classList.remove("has-source-icon");
  element.style.removeProperty("--pin-icon");
  element.textContent = "";
  if (!category.iconAsset) {
    element.textContent = fallback;
    return;
  }
  element.style.setProperty("--pin-icon", `url("${iconURL(category.iconAsset)}")`);
  element.classList.add("has-source-icon");
}

function iconURL(asset) {
  const path = asset.split("/").map((segment) => encodeURIComponent(segment)).join("/");
  return `/static/icons/${path}`;
}

function initials(value) {
  return value.split(/\s+/).slice(0, 2).map((part) => part[0] || "").join("");
}

function cleanDescription(value) {
  return String(value || "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/[*_`>#]/g, "")
    .replace(/\r/g, "")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function formatNumber(value) {
  return new Intl.NumberFormat().format(value);
}

function clamp(value, minimum, maximum) {
  return Math.min(maximum, Math.max(minimum, value));
}

function isEditableTarget(target) {
  return target instanceof HTMLElement &&
    (target.matches("input, textarea, select") || target.isContentEditable);
}

function exposeDiagnostics() {
  const snapshot = () => ({
    game: state.game?.title,
    map: state.map?.title,
    variant: state.variant?.name,
    zoom: state.engine?.getView().getZoom(),
    center: state.engine?.getView().getCenter(),
    resolution: state.engine?.getView().getResolution(),
    nativeMaxZoom: state.variant?.maxZoom,
    maxZoom: state.variant ? viewMaxZoom(state.variant) : null,
    interpolate: state.variant?.interpolate,
    tileStats: { ...state.tileStats },
    pins: state.pins.length,
    eligibleLocations: state.eligibleLocations,
    domNodes: document.querySelectorAll("*").length,
    canvases: document.querySelectorAll("canvas").length,
    rasterCacheSize: 64,
    pinLabelsVisible: state.pinLabelsVisible,
    hoveredPin: state.hoveredPin?.location.title || null,
    selectedPin: state.selectedPin?.location.title || null,
    fitZoom: state.fitZoom,
    filters: {
      hiddenCategories: [...state.hiddenCategories].sort(),
      collapsedSections: [...state.collapsedSections].sort(),
    },
    ui: {
      sidebarCollapsed: document.querySelector(".app-shell")
        ?.classList.contains("sidebar-collapsed") ?? false,
      detailOpen: !elements.detail.hidden,
      detailTitle: elements.detailTitle.textContent || null,
      searchQuery: state.search,
      searchResultsVisible: !elements.searchResults.hidden,
      soloChip: elements.soloChip.hidden ? null : elements.soloChip.textContent,
      visibleCountText: elements.visibleCount.textContent,
      overviewDocked: state.overviewDocked,
      subgridVisible: state.subgridVisible,
    },
    zones: {
      visible: state.zonesVisible,
      count: state.zoneRecords.size,
      focused: state.zoneRecords.get(state.focusedZoneID)?.zone.title || null,
      highlighted: [...state.highlightedZones]
        .map((zoneID) => state.zoneRecords.get(zoneID)?.zone.title)
        .filter(Boolean),
      focusedPins: state.pins.filter((pin) => !pin.filteredHidden &&
        pin.insideHighlightedZone).length,
    },
    grid: {
      enabled: state.gridEnabled,
      prefix: state.gridPrefix,
      maximumDepth: gridMaxDepth(),
      extent: state.gridEnabled ? currentGridExtent() : null,
      cells: [
        ...state.sources.grid.getFeatures(),
        ...state.sources.gridContext.getFeatures(),
      ].map((feature) => feature.get("gridCell")),
      priorityPins: state.gridPrefix
        ? state.pins.filter((pin) => !pin.filteredHidden && pinInGridCell(pin)).length
        : 0,
    },
  });
  window.__atlasDebug = {
    snapshot,
    // Raster layers are reachable so a render fault can be narrowed to the
    // complete base or the deep detail riding on top of it.
    setLayerVisible: (name, visible) => state.layers[name]?.setVisible(visible),
  };
  window.render_game_to_text = () => JSON.stringify({
    coordinateSystem: "ATLAS:PIXELS; origin top-left; x increases right; y decreases downward",
    ...snapshot(),
  });
  window.advanceTime = () => state.engine?.renderSync();
}

start();
