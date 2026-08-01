import { mapText } from "./catalog.js";
import { elements } from "./dom.js";
import { geohashAt } from "./grid.js";
import { syncLegendCheckboxes, syncSectionSwitches } from "./legend.js";
import { viewMaxZoom } from "./navigation.js";
import { applyPinFilters, refreshPrioritySource } from "./pins.js";
import { renderSearchResults, revealDock } from "./search.js";
import { state } from "./state.js";
import { applyCategoryVisual, applyCategoryGlyph, initials } from "./theme.js";
import { cleanDescription } from "./util.js";

export function revealPin(pin) {
  state.hiddenCategories.delete(pin.category.id);
  syncLegendCheckboxes();
  applyPinFilters();
  syncSectionSwitches();
  showPin(pin, true);
}

export function showPin(pin, focus = false) {
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
  // A latitude of this map's own is hard to hold in the head and harder to
  // compare, so the pin also names the cell it stands in -- the same three
  // characters the grid draws and the navigator takes, which say where a place
  // is by saying which part of the map it is in. A pin off the ground the grid
  // divides has no cell to name, and says nothing rather than the nearest one.
  const cell = geohashAt(pin.coordinate);
  elements.detailCell.textContent = cell;
  elements.detailCellField.hidden = !cell;
  // A map composed from several sources says where a pin came from. The
  // payload's merge account names every contribution; a pin it does not name
  // is the map's own, and says nothing rather than guessing.
  const source = pinSource(pin);
  elements.detailSource.textContent = source;
  elements.detailSourceField.hidden = !source;
  applyCategoryVisual(elements.detailDot, pin.category);
  applyCategoryGlyph(elements.detailDot, pin.category, initials(pin.category.title));
  elements.detail.hidden = false;
  revealDock();
  renderSearchResults();
  if (focus) {
    const view = state.engine.getView();
    view.animate({
      center: pin.coordinate,
      zoom: Math.min(viewMaxZoom(state.variant), Math.max(view.getZoom() || 0, 4)),
      duration: 220,
    });
  }
}

// pinSource reads a pin's provenance out of the map's merge account: a pin in
// a source-titled group or adopted into a native category came from that
// source, a pin another source matched is the origin's word corroborated, and
// every other pin is the origin's alone. The index is built once per map and
// thrown away with it.
function pinSource(pin) {
  const merged = state.map?.merged;
  if (!merged?.length) return "";
  const origin = merged.find((account) => account.origin)?.source || "";
  if (state.pinSourceIndex?.map !== state.map) {
    const byID = new Map();
    for (const account of merged) {
      for (const adopted of account.adopted || []) {
        byID.set(adopted.d, account.source);
      }
      for (const pair of account.matched || []) {
        const confirmed = `confirmed by ${account.source}`;
        byID.set(pair.w, origin ? `${origin} · ${confirmed}` : confirmed);
      }
    }
    state.pinSourceIndex = { map: state.map, byID };
  }
  for (const account of merged) {
    if (!account.origin && pin.group.title === account.source) return account.source;
  }
  return state.pinSourceIndex.byID.get(pin.location.id) || origin;
}

// The words belonging to a pin are fetched the first time one is opened, so a
// map that is only ever looked at never pays for them. The panel opens on what
// is already known and fills in when they arrive; a pin closed or changed in
// the meantime is left alone.
export async function fillPinText(pin) {
  const text = await mapText();
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
export function renderDetailLinks(pin) {
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

export function closeDetail() {
  const hadSelection = state.selectedPin !== null;
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
  if (hadSelection) renderSearchResults();
}
