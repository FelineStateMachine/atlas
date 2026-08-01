import { elements } from "./dom.js";
import { state } from "./state.js";
import { viewMaxZoom } from "./navigation.js";
import { currentGridExtent, gridMaxDepth, pinInGridCell } from "./grid.js";

export function exposeDiagnostics() {
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
    labelsHeld: state.labelsHeld,
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
