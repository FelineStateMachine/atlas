import XYZ from "ol/source/XYZ.js";
import TileGrid from "ol/tilegrid/TileGrid.js";

import { state } from "./state.js";
import { elements, populateSelect } from "./dom.js";
import { overzoomLevels } from "./constants.js";
import { loadMap } from "./catalog.js";
import { readSession, saveSession, writeRoute } from "./session.js";
import { createView, resolutions } from "./engine.js";
import { renderOverview, setOverviewDocked } from "./overview.js";
import { setDockFolded } from "./search.js";
import { renderGrid } from "./grid.js";
import { renderZones } from "./zones.js";
import { renderLegend } from "./legend.js";
import {
  applyPinFilters,
  buildPins,
  pinIsHidden,
  refreshPrioritySource,
  setHoveredPin,
} from "./pins.js";
import { closeDetail } from "./detail.js";
import { iconOutsetColor } from "./theme.js";
import { clamp, formatNumber } from "./util.js";

export async function selectGame(slug, mapSlug) {
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

export async function selectMap(id) {
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
  setDockFolded(Boolean(state.restore?.dockFolded), false);
  state.search = "";
  elements.search.value = "";
  elements.dock.hidden = false;
  closeDetail();
  renderLegend();
  renderZones();
  buildPins();
  selectVariant(restore ? clamp(restore.variant, 0, state.map.variants.length - 1) : 0, true);
  elements.sidebar.classList.remove("is-open");
  elements.mobileLegend.setAttribute("aria-expanded", "false");
  writeRoute();
  state.settling = false;
  saveSession();
}

export function selectVariant(index, resetView = false) {
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
export function tileExists(variant, zoom, x, y) {
  const coverage = variant.coverage?.[zoom];
  if (!coverage) return true;
  const column = x - coverage.x;
  const row = y - coverage.y;
  if (column < 0 || row < 0 || column >= coverage.w || row >= coverage.h) return false;
  const bits = coverageBits(variant, zoom, coverage);
  const index = row * coverage.w + column;
  return (bits[index >> 3] & (1 << (index & 7))) !== 0;
}

export function coverageBits(variant, zoom, coverage) {
  if (!coverage.decoded) {
    const binary = atob(coverage.bits);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
    coverage.decoded = bytes;
  }
  return coverage.decoded;
}

export function viewMaxZoom(variant) {
  return variant.maxZoom + overzoomLevels;
}

// What a view owes the rest of the page once it has come to rest.
export function settleView() {
  updateVisibleCount();
  saveSession();
}

// The layers of a split map are the same ground at different heights, stacked
// down one sheet. Switching between them should leave the reader over the same
// place, so the view is carried across by its position within each layer's box
// rather than by its absolute coordinates.
export function carryViewAcrossShards(previous, next) {
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

export function toggleSidebar() {
  const shell = document.querySelector(".app-shell");
  const collapsed = shell.classList.toggle("sidebar-collapsed");
  elements.mobileLegend.setAttribute("aria-expanded", String(!collapsed));
  // The map keeps its own idea of how big it is.
  requestAnimationFrame(() => state.engine?.updateSize());
}

export function activeExtent() {
  const size = state.catalog.tileGrid.size;
  const bounds = state.variant?.bounds || { x: 0, y: 0, width: size, height: size };
  return [bounds.x, -(bounds.y + bounds.height), bounds.x + bounds.width, -bounds.y];
}

export function fitMap() {
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

export function changeZoom(delta) {
  const view = state.engine.getView();
  const current = view.getZoom() || 0;
  view.animate({
    zoom: clamp(current + delta, state.variant.minZoom, viewMaxZoom(state.variant)),
    duration: 140,
  });
}

export function updateVisibleCount() {
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
