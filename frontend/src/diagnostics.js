import { anyShapeCollectionVisible } from "./collections.js";
import { elements } from "./dom.js";
import { state } from "./state.js";
import { viewMaxZoom } from "./navigation.js";
import { refreshCatalog } from "./library.js";
import { currentGridExtent, gridMaxLevel, pinInGridCell } from "./grid.js";
import { drawnFeatureCount, listableFeatures } from "./visibility.js";

export function exposeDiagnostics() {
  const snapshot = () => ({
    volume: state.volume?.title,
    world: state.world?.title,
    lens: state.lens?.name,
    zoom: state.engine?.getView().getZoom(),
    center: state.engine?.getView().getCenter(),
    resolution: state.engine?.getView().getResolution(),
    nativeMaxZoom: state.lens?.maxZoom,
    maxZoom: state.lens ? viewMaxZoom(state.lens) : null,
    interpolate: state.lens?.interpolate,
    tileStats: { ...state.tileStats },
    // The registry is state.features now, but the snapshot keeps the key the
    // parity diffs already know, so cross-build comparisons stay line-for-line
    // readable.
    pins: state.features.length,
    eligibleLocations: state.eligibleLocations,
    domNodes: document.querySelectorAll("*").length,
    canvases: document.querySelectorAll("canvas").length,
    rasterCacheSize: 64,
    labelsHeld: state.labelsHeld,
    hoveredPin: state.hoveredPin?.location.title || null,
    selectedPin: state.selectedPin?.location.title || null,
    fitZoom: state.fitZoom,
    filters: {
      // The state holds one hiddenCollections set now, but the snapshot keeps
      // the key the parity diffs already know, so cross-build comparisons
      // stay line-for-line readable.
      hiddenCategories: [...state.hiddenCollections].sort(),
      collapsedSections: [...state.collapsedSections].sort(),
    },
    ui: {
      sidebarCollapsed: document.querySelector(".app-shell")
        ?.classList.contains("sidebar-collapsed") ?? false,
      detailOpen: !elements.detail.hidden,
      detailTitle: elements.detail.hidden
        ? null
        : elements.detailTitle.textContent || null,
      searchQuery: state.search,
      searchResultsVisible: !elements.searchResults.hidden,
      soloChip: elements.soloChip.hidden ? null : elements.soloChip.textContent,
      visibleCountText: elements.visibleCount.textContent,
      overviewDocked: state.overviewDocked,
      dockFolded: state.dockFolded,
      dockDismissed: state.dockDismissed,
      subgridVisible: state.subgridVisible,
    },
    zones: {
      visible: anyShapeCollectionVisible(),
      count: state.zoneRecords.size,
      focused: state.zoneRecords.get(state.focusedZoneID)?.zone.title || null,
      highlighted: [...state.highlightedZones]
        .map((zoneID) => state.zoneRecords.get(zoneID)?.zone.title)
        .filter(Boolean),
      focusedPins: state.features.filter((pin) => !pin.filteredHidden &&
        pin.passesZoneFilters).length,
    },
    // The three places one filter has to land at once, side by side so a diff
    // can see them disagree. The first two are the model's own count of what
    // the map is drawing; the last two are what the footer and the dock
    // actually say. A surface that forgot to recount after a filter moved
    // shows up here as text out of step with the numbers beside it -- which is
    // the whole of what the tour's sync check reads.
    sync: {
      drawn: drawnFeatureCount(),
      listable: listableFeatures().length,
      footerText: elements.visibleCount.textContent,
      dockText: elements.dockCount.textContent,
      dockFlag: elements.dockFlag.hidden ? null : elements.dockFlag.textContent,
      dockRows: elements.searchResults.querySelectorAll(".search-result").length,
      searching: Boolean(state.search),
    },
    grid: {
      enabled: state.gridEnabled,
      system: state.gridSystem,
      prefix: state.gridCell,
      maximumDepth: gridMaxLevel(),
      extent: state.gridEnabled ? currentGridExtent() : null,
      cells: [
        ...state.sources.grid.getFeatures(),
        ...state.sources.gridContext.getFeatures(),
      ].map((feature) => feature.get("gridCell")),
      priorityPins: state.gridCell
        ? state.features.filter((pin) => !pin.filteredHidden && pinInGridCell(pin)).length
        : 0,
    },
  });
  window.__atlasDebug = {
    snapshot,
    // Raster layers are reachable so a render fault can be narrowed to the
    // complete base or the deep detail riding on top of it.
    setLayerVisible: (name, visible) => state.layers[name]?.setVisible(visible),
    // The library refresh normally rides the desktop event bridge; exposing
    // it lets a plain browser -- the parity tour, a test -- drive the same
    // reconciliation by hand.
    refreshCatalog,
  };
  window.render_game_to_text = () => JSON.stringify({
    coordinateSystem: "ATLAS:PIXELS; origin top-left; x increases right; y decreases downward",
    ...snapshot(),
  });
  window.advanceTime = () => state.engine?.renderSync();
}
