import Feature from "ol/Feature.js";
import Point from "ol/geom/Point.js";

import { groupByCollection, isCollectionHidden, passesZoneFilters } from "./collections.js";
import { closeDetail } from "./detail.js";
import { elements } from "./dom.js";
import { pinInGridCell } from "./grid.js";
import { renderSearchResults } from "./search.js";
import { renderAs } from "./semconv.js";
import { updateVisibleCount } from "./navigation.js";
import { state } from "./state.js";
import { prepareMarkerIcon } from "./styles.js";
import { stableRank } from "./util.js";
import { project } from "./zones.js";

export function buildPins() {
  state.sources.pins.clear();
  state.sources.text.clear();
  state.sources.priority.clear();
  state.pins = [];
  state.pinByID.clear();
  for (const category of state.world.collections) {
    if (category.kind !== "point") continue;
    if (renderAs(category) !== "text") prepareMarkerIcon(category);
    for (const location of category.locations) {
      const pin = {
        location,
        category,
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
      if (renderAs(category) === "text") state.sources.text.addFeature(pin.feature);
      else state.sources.pins.addFeature(pin.feature);
    }
  }
  applyPinFilters();
}

export function applyPinFilters() {
  for (const pin of state.pins) {
    const collectionHidden = isCollectionHidden(pin.category.id);
    const searchHidden = state.search &&
      !pin.location.title.toLocaleLowerCase().includes(state.search);
    pin.filteredHidden = Boolean(collectionHidden || searchHidden);
  }
  updateZonePinFocus();
  refreshPinRendering();
  if (state.selectedPin?.filteredHidden) closeDetail();
  renderSearchResults();
  // Anything else drawing the pins -- the globe -- filters the same moment
  // the chart does.
  document.dispatchEvent(new Event("atlas:filters"));
}

// Highlights conjoin across collections and union within one: a pin stands
// only where every highlighted collection claims it. With the v2 wire's one
// implicit collection that is the plain union, so today's maps read exactly
// as they always have; the second collection arrives with the v3 wire.
export function updateZonePinFocus() {
  if (!state.highlightedZones.size) {
    for (const pin of state.pins) pin.passesZoneFilters = false;
    return;
  }
  const groups = groupByCollection([...state.highlightedZones]
    .map((zoneID) => state.zoneRecords.get(zoneID))
    .filter(Boolean));
  for (const pin of state.pins) {
    pin.passesZoneFilters = passesZoneFilters(groups, pin.coordinate);
  }
}

export function refreshPinRendering() {
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

export function refreshPrioritySource() {
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

export function setHoveredPin(pin) {
  if (state.hoveredPin === pin) return;
  state.hoveredPin = pin;
  if (!state.sources) return;
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.priority.changed();
}

// Z is held rather than switched: the reader wants every name for as long as
// they are looking for one, and then wants the map back. Holding it says both
// in one gesture, so there is no state left on by mistake and no zoom to reach
// first -- letting go returns the map to whatever it was drawing before.
export function setLabelsHeld(held) {
  if (state.labelsHeld === held) return;
  state.labelsHeld = held;
  elements.labelsHint.textContent = held ? "Z · labels shown" : "Z · hold for labels";
  state.layers.pinLabels.changed();
  state.layers.text.changed();
  // Quiet zone names answer the same key, so the title layers must hear it
  // the moment the pin labels do.
  state.layers.zoneTitles.changed();
  state.layers.zoneTitleDetail.changed();
  // Anything else writing names beside its pins -- the globe -- holds and
  // releases them the same moment the chart does.
  document.dispatchEvent(new Event("atlas:labels"));
}

export function isPriorityPin(pin) {
  return pin === state.selectedPin || pin === state.hoveredPin ||
    pinInGridCell(pin) ||
    (Boolean(state.search) && pin.location.title.toLocaleLowerCase().includes(state.search));
}

export function pinIsHidden(pin) {
  return pin.filteredHidden || pinIsZoneCulled(pin) || pinIsGridCulled(pin) ||
    !onActiveShard(pin.location);
}

// A map split into layers offers one at a time. Anything belonging to another
// layer is elsewhere in the world, not merely filtered out.
export function onActiveShard(item) {
  const shard = state.lens?.shard;
  return !shard || !item.shard || item.shard === shard;
}

export function pinIsZoneCulled(pin) {
  if (!state.highlightedZones.size || pin.passesZoneFilters) return false;
  if (pin === state.selectedPin) return false;
  return !(Boolean(state.search) &&
    pin.location.title.toLocaleLowerCase().includes(state.search));
}

// Descending into a cell narrows the question to what is inside it, the same
// way highlighting a zone does. What lies outside was still being drawn, so the
// answer stayed as crowded as it was before the cell was chosen.
export function pinIsGridCulled(pin) {
  if (!state.gridEnabled || !state.gridCell || pinInGridCell(pin)) return false;
  if (pin === state.selectedPin) return false;
  return !(Boolean(state.search) &&
    pin.location.title.toLocaleLowerCase().includes(state.search));
}

export function pinPriority(category, location) {
  const rarity = Math.max(0, 1_000_000 - Math.min(category.locations.length, 999) * 1000);
  return rarity + (stableRank(location.id) % 1000);
}

export function textDetailRatio(category) {
  if (category.locations.length > 200) return 4;
  if (category.locations.length > 75) return 2.5;
  return 1;
}

export function atMaximumNativeZoom() {
  const zoom = state.engine?.getView().getZoom() || 0;
  return zoom >= (state.lens?.maxZoom || 0) - 0.05;
}
