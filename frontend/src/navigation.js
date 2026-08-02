import { intersects } from "ol/extent.js";
import XYZ from "ol/source/XYZ.js";
import TileGrid from "ol/tilegrid/TileGrid.js";

import { isCollectionHidden } from "./collections.js";
import { state } from "./state.js";
import { elements, populateSelect } from "./dom.js";
import { overzoomLevels } from "./constants.js";
import { loadWorld } from "./catalog.js";
import { refreshCatalog } from "./library.js";
import { readSession, saveSession, writeRoute } from "./session.js";
import { createView, initializeMap, resolutions } from "./engine.js";
import { renderOverview, setOverviewDocked } from "./overview.js";
import { setDockFolded } from "./search.js";
import { renderGrid } from "./grid.js";
import { syncGlobe } from "./globe.js";
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

export async function selectVolume(slug, worldSlug) {
  state.volume = state.catalog.volumes.find((volume) => volume.slug === slug) || state.catalog.volumes[0];
  // The engine exists once there is a volume to size its world square from: every volume
  // carries its own tile grid now that each arrives in its own bundle.
  if (!state.engine) initializeMap();
  // Only an explicitly named world overrides what this volume remembers.
  if (!worldSlug && !state.restore) state.restore = readSession(state.volume.slug);
  elements.volume.value = state.volume.slug;
  populateSelect(elements.world, state.volume.worlds, "title");
  elements.world.disabled = state.volume.worlds.length === 1;
  elements.world.title = elements.world.disabled ? "This volume has one world" : "Choose a world";
  // Returning to a volume returns to where it was left, so wandering off to
  // another volume and back does not cost the reader their place.
  const remembered = state.restore?.volume === state.volume.slug ? state.restore : null;
  const wanted = (worldSlug && state.volume.worlds.find((item) => item.slug === worldSlug)) ||
    (remembered && state.volume.worlds.find((item) => item.slug === remembered.world));
  await selectWorld((wanted || state.volume.worlds[0]).slug);
}

export async function selectWorld(slug) {
  const entry = state.volume.worlds.find((world) => world.slug === slug) || state.volume.worlds[0];
  // A world opened while another is still arriving must not be overtaken by it.
  const run = ++state.worldRun;
  elements.loading.hidden = false;
  let loaded;
  try {
    loaded = await loadWorld(entry);
  } catch (error) {
    // The bundle moved underneath the catalog: refetching the catalog brings
    // the new build's URLs, and the refresh re-selects this world through them.
    // The error is spoken either way -- a coding fault in the load path would
    // otherwise wear the same silence as a stale stamp, forever.
    console.error(`world ${entry.slug} failed to load`, error);
    if (state.worldRun === run) {
      elements.loading.hidden = true;
      void refreshCatalog();
    }
    return;
  }
  if (state.worldRun !== run) return;

  state.settling = true;
  state.world = loaded;
  document.documentElement.style.setProperty("--icon-outset-color", iconOutsetColor());
  elements.world.value = state.world.slug;
  populateSelect(elements.lens, state.world.lenses, "name");
  elements.lensField.hidden = state.world.lenses.length < 2;
  state.styleCache.clear();
  setHoveredPin(null);
  state.gridCell = "";
  state.gridSystem = "geohash";
  state.hiddenCollections.clear();
  state.labelOverrides.clear();
  // Zones are a navigation aid, not the primary filter surface: keep boundaries
  // drawn but fold the section away so pin groups stay above the fold. The
  // pseudo-collection's own row starts unfolded, so the feature index is
  // there the moment the section is opened -- and in the DOM for anything
  // that reaches for a zone row without unfolding first.
  state.collapsedSections.clear();
  state.collapsedSections.add("zones");
  state.expandedCollections.clear();
  state.expandedCollections.add("zones");
  for (const group of state.world.groups) {
    for (const category of group.categories) {
      if (!category.visible) state.hiddenCollections.add(category.id);
    }
  }
  const restore = state.restore?.world === state.world.slug ? state.restore : null;
  if (restore) {
    state.hiddenCollections = new Set(restore.hidden);
    state.collapsedSections = new Set(restore.collapsed);
    state.expandedCollections = new Set(restore.expanded || []);
    state.labelOverrides = new Map(restore.labels || []);
  }
  // Where the corner of the screen is wanted is a preference about the volume
  // rather than about one of its maps, so it carries across them.
  setOverviewDocked(Boolean(state.restore?.overviewDocked), false);
  // A map opens on the map: the panel is away until a pin or a search gives it
  // something to hold. A volume that remembers otherwise is a reader who has
  // already said where they want it.
  setDockFolded(state.restore ? Boolean(state.restore.dockFolded) : true, false);
  state.dockDismissed = Boolean(state.restore?.dockDismissed);
  state.search = "";
  elements.search.value = "";
  elements.dock.hidden = false;
  closeDetail();
  renderLegend();
  renderZones();
  buildPins();
  selectLens(restore ? clamp(restore.lens, 0, state.world.lenses.length - 1) : 0, true);
  // A map that declares itself a sphere offers the globe; every map opens
  // on the chart either way.
  syncGlobe();
  // The remembered arrangement has been spent. Kept, it would be read again by
  // the next thing that asks: a lens swap would drop the reader back where
  // the session opened, and switching to another volume would hand that volume the
  // dock and overview this one was left with instead of its own -- selectVolume
  // only reads a volume's session when nothing is held here.
  state.restore = null;
  elements.sidebar.classList.remove("is-open");
  elements.mobileLegend.setAttribute("aria-expanded", "false");
  writeRoute();
  state.settling = false;
  saveSession();
}

export function selectLens(index, resetView = false) {
  const priorLens = state.lens;
  state.lens = state.world.lenses[index] || state.world.lenses[0];
  const lens = state.lens;
  const carried = resetView ? null : carryViewAcrossShards(priorLens, lens);
  const maxViewZoom = viewMaxZoom(lens);
  elements.lens.value = String(state.world.lenses.indexOf(state.lens));
  if (resetView) state.engine.setView(createView(maxViewZoom, activeExtent()));
  else {
    // The view is rebuilt around the new lens's own window -- shards do
    // not share one -- but keeps the place the reader was carried to.
    const previous = state.engine.getView();
    const held = createView(maxViewZoom, activeExtent());
    held.setCenter(previous.getCenter());
    held.setZoom(previous.getZoom() || 0);
    state.engine.setView(held);
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
      extent: [0, -state.volume.tileGrid.size, state.volume.tileGrid.size, 0],
      origin: [0, 0],
      // Keep the source grid native. Views may overzoom this last resolution,
      // but no nonexistent tile level is requested.
      resolutions: resolutions(maxLevel),
      tileSize: state.volume.tileGrid.tileSize,
    }),
    cacheSize: 64,
    interpolate: lens.interpolate,
    transition: 0,
    wrapX: false,
    tileUrlFunction: ([zoom, x, y]) => {
      const format = lens.formats[zoom];
      if (!format || x < 0 || y < 0 || !wanted(zoom, x, y)) return undefined;
      return `${state.volume.base}/tiles/${encodeURIComponent(lens.tiles)}/${zoom}/${x}/${y}.${format}`;
    },
  }));

  const fullZoom = lens.fullZoom ?? lens.maxZoom;
  const base = buildSource(fullZoom, (zoom, x, y) => tileExists(lens, zoom, x, y));
  const previous = state.layers.raster.getSource();
  state.layers.raster.setSource(base);
  state.layers.raster.setExtent(activeExtent());
  // Tiles matching the map's background were never written; painting that
  // colour behind the layer makes their absence invisible.
  state.layers.raster.setBackground(lens.background || undefined);
  previous?.clear();

  const previousDetail = state.layers.rasterDetail.getSource();
  if (lens.maxZoom > fullZoom) {
    state.layers.rasterDetail.setSource(buildSource(
      lens.maxZoom,
      (zoom, x, y) => zoom > fullZoom && tileExists(lens, zoom, x, y),
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
  // Only a world being opened has a view to restore. A lens is a different
  // picture of the ground the reader is already looking at -- spring for
  // summer, one layer of a split map for another -- so the swap is made
  // underneath them and everything else, the view included, stays as it is.
  const resume = resetView && state.restore?.world === state.world.slug ? state.restore : null;
  // Zones and pins are built before a lens is chosen, so on a split world the
  // first render shows every layer at once. Comparing against what was actually
  // rendered catches that as well as a later switch between layers.
  if (state.renderedShard !== (lens.shard || 0)) {
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
export function tileExists(lens, zoom, x, y) {
  const coverage = lens.coverage?.[zoom];
  if (!coverage) return true;
  const column = x - coverage.x;
  const row = y - coverage.y;
  if (column < 0 || row < 0 || column >= coverage.w || row >= coverage.h) return false;
  const bits = coverageBits(lens, zoom, coverage);
  const index = row * coverage.w + column;
  return (bits[index >> 3] & (1 << (index & 7))) !== 0;
}

export function coverageBits(lens, zoom, coverage) {
  if (!coverage.decoded) {
    const binary = atob(coverage.bits);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
    coverage.decoded = bytes;
  }
  return coverage.decoded;
}

export function viewMaxZoom(lens) {
  return lens.maxZoom + overzoomLevels;
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
  const size = state.volume.tileGrid.size;
  const bounds = state.lens?.bounds || { x: 0, y: 0, width: size, height: size };
  return [bounds.x, -(bounds.y + bounds.height), bounds.x + bounds.width, -bounds.y];
}

export function fitMap() {
  if (!state.engine || !state.lens) return;
  const view = state.engine.getView();
  view.fit(activeExtent(), {
    size: state.engine.getSize(),
    padding: [36, 36, 36, 36],
    maxZoom: state.lens.maxZoom,
    duration: 0,
  });
  state.fitZoom = view.getZoom() || 0;
  updateVisibleCount();
}

export function changeZoom(delta) {
  const view = state.engine.getView();
  const current = view.getZoom() || 0;
  view.animate({
    zoom: clamp(current + delta, state.lens.minZoom, viewMaxZoom(state.lens)),
    duration: 140,
  });
}

// The footer counts features of every kind: pins by their coordinate, zones
// by whether their ground reaches into the view. "Enabled" is what the
// legend's ledger lets draw; "in view" is the part of it under the window.
export function updateVisibleCount() {
  const zoom = state.engine?.getView().getZoom();
  elements.viewport.dataset.zoom = Number.isFinite(zoom) ? zoom.toFixed(3) : "";
  if (!state.engine || !state.world) {
    elements.visibleCount.textContent = "0 features enabled";
    return;
  }
  const extent = state.engine.getView().calculateExtent(state.engine.getSize());
  let enabled = state.eligibleLocations;
  let inView = 0;
  for (const pin of state.pins) {
    if (pinIsHidden(pin)) continue;
    const [x, y] = pin.coordinate;
    if (x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3]) inView++;
  }
  if (!isCollectionHidden("zones")) {
    enabled += state.zoneRecords.size;
    for (const record of state.zoneRecords.values()) {
      if (intersects(record.extent, extent)) inView++;
    }
  }
  elements.visibleCount.textContent =
    `${formatNumber(enabled)} features enabled · ${formatNumber(inView)} in view`;
}
