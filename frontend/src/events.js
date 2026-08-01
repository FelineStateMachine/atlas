import { $, elements } from "./dom.js";
import { state } from "./state.js";
import { activeSystem } from "./cellsystems/index.js";
import {
  changeZoom,
  selectGame,
  selectMap,
  selectVariant,
  settleView,
  toggleSidebar,
} from "./navigation.js";
import {
  setAllCategories,
  setAllSectionsCollapsed,
  showOnly,
  syncSectionCollapse,
  syncSectionSwitches,
  toggleSection,
} from "./legend.js";
import { applyPinFilters, setLabelsHeld } from "./pins.js";
import { changeGlobeZoom, refreshGlobe, resizeGlobe, toggleGlobe } from "./globe.js";
import { jumpToZone, setZonesVisible, toggleZoneHighlight } from "./zones.js";
import { foldDockByHand, revealDock } from "./search.js";
import { importBundles } from "./library.js";
import { closeDetail, revealPin } from "./detail.js";
import {
  ascendGrid,
  handleGridKey,
  selectGridCell,
  cycleGridSystem,
  setSubgridVisible,
} from "./grid.js";
import { readRoute } from "./session.js";
import {
  overviewNavigate,
  setOverviewDocked,
  updateOverviewViewport,
} from "./overview.js";
import { isEditableTarget } from "./util.js";

export function bindUIEvents() {
  elements.game.addEventListener("change", () => { void selectGame(elements.game.value); });
  elements.map.addEventListener("change", () => { void selectMap(elements.map.value); });
  elements.addBundles.addEventListener("click", () => { void importBundles(); });
  elements.emptyOpen.addEventListener("click", () => { void importBundles(); });
  elements.variant.addEventListener("change", () => {
    selectVariant(Number(elements.variant.value));
    refreshGlobe();
  });
  elements.globeToggle.addEventListener("click", () => toggleGlobe());

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
    // applyPinFilters refreshes the dock list along with the canvas.
    applyPinFilters();
    // What a search finds is listed in the panel, so a search asked with the
    // panel away is a question with the answer out of sight.
    if (state.search) revealDock();
  });
  elements.dockFold.addEventListener("click", () => {
    foldDockByHand(!state.dockFolded);
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
  // One pair of buttons, whichever pane is showing: the chart steps its
  // zoom, the globe halves or doubles its distance.
  $("#zoom-in").addEventListener("click", () =>
    state.globeActive ? changeGlobeZoom(1) : changeZoom(1));
  $("#zoom-out").addEventListener("click", () =>
    state.globeActive ? changeGlobeZoom(-1) : changeZoom(-1));
  elements.gridBack.addEventListener("click", ascendGrid);
  elements.gridSystem.addEventListener("click", cycleGridSystem);
  elements.gridInput.addEventListener("input", () => {
    // The system says what the field keeps of a keystroke and when the
    // text has become a place: geohash goes somewhere on every character,
    // a token system waits until the address parses whole.
    const system = activeSystem();
    const value = system.normalizeInput(elements.gridInput.value);
    elements.gridInput.value = value;
    if (system.parseInput(value) !== null) selectGridCell(value);
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
    // The backquote flips panes from inside the field the way space works
    // the subgrid from it: no hash will ever spell either character.
    if (event.key === "`") {
      event.preventDefault();
      if (!elements.globeToggle.hidden) toggleGlobe();
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
    // ⌘G cycles which cell system divides the map, carrying the chosen
    // place across at like precision. Above the editable-target guard: the
    // switch is most wanted mid-thought, right from the token field.
    if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "g") {
      event.preventDefault();
      cycleGridSystem();
      return;
    }
    if (isEditableTarget(event.target)) return;
    // The backquote flips between chart and globe and touches nothing else:
    // selection, filters, and each pane's own camera all stay where they
    // were. Only a map that declared itself a sphere answers it.
    if (event.key === "`" && !event.metaKey && !event.ctrlKey && !event.altKey) {
      if (!elements.globeToggle.hidden) {
        event.preventDefault();
        toggleGlobe();
      }
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
      event.preventDefault();
      elements.search.focus();
      elements.search.select();
      return;
    }
    // ⌘⌥B before ⌘B, and by physical key: Option rewrites event.key on a Mac
    // (⌥B types "∫"), and on Windows Ctrl+Alt+B leaves the key readable, so
    // the code is the one name the chord keeps everywhere. The right panel
    // answers the same shortcut editors give their secondary sidebar.
    if ((event.metaKey || event.ctrlKey) && event.altKey && event.code === "KeyB") {
      event.preventDefault();
      if (!elements.dock.hidden) foldDockByHand(!state.dockFolded);
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
    // A key held down repeats, and every repeat after the first says what the
    // first one already said.
    if (event.key.toLocaleLowerCase() === "z") {
      event.preventDefault();
      if (!event.repeat) setLabelsHeld(true);
    }
  });
  window.addEventListener("keyup", (event) => {
    if (event.key.toLocaleLowerCase() === "z") setLabelsHeld(false);
  });
  // A window that loses focus with the key down never hears it come back up,
  // so ⌘-Tab away while reading the names and they would still be there on the
  // way back, with nothing holding them.
  window.addEventListener("blur", () => setLabelsHeld(false));
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
    resizeGlobe();
  });
  // The globe reporting where it looks is the overview's cue to move its
  // reticle, the same way the chart's view changes are.
  document.addEventListener("atlas:globe-camera", () => updateOverviewViewport());
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
