// One answer to what the reader is looking at.
//
// Three surfaces used to work this out for themselves. The canvas drew what
// survived every filter; the footer counted what the legend allowed; the dock
// listed what the search left. So highlighting one district could cull sixty
// features off the map while the panel beside it went on offering all sixty,
// and the two numbers on screen disagreed by a factor of four with nothing to
// say which was the map's. Every surface asks here now, and a filter that
// lands on one of them lands on all of them.

import { isCollectionHidden } from "./collections.js";
import { pinIsHidden } from "./features.js";
import { state } from "./state.js";

// Every point feature the chart is drawing: its collection shown, the search
// satisfied, and standing inside whatever ground or cell is holding the view.
export function visiblePins() {
  return state.features.filter((pin) => !pinIsHidden(pin));
}

// Every shape feature the chart is drawing: its collection shown, and on the
// lens's own shard, which is all the registry holds. Highlighting narrows
// which points stand rather than which ground is drawn, so it asks nothing
// here.
export function visibleZones() {
  return [...state.zoneRecords.values()]
    .filter((record) => !isCollectionHidden(record.zone.collectionId));
}

// What the map is drawing, counted: features of every kind, one number.
export function drawnFeatureCount() {
  return visiblePins().length + visibleZones().length;
}

// The part of it a list can name, alphabetically. Shape features answer the
// search here rather than on the canvas -- searching for a place has never
// taken the ground out from under it -- and one the archive left untitled is
// on the map without being in any index of it.
export function listableFeatures() {
  return [
    ...visiblePins().map((pin) => ({ title: pin.location.title, pin })),
    ...visibleZones()
      .filter((record) => record.zone.title &&
        (!state.search || record.zone.title.toLocaleLowerCase().includes(state.search)))
      .map((record) => ({ title: record.zone.title, record })),
  ].sort((a, b) => a.title.localeCompare(b.title));
}

// Whether anything but the reader's own search is narrowing the list: a
// collection put away, ground highlighted, a cell holding the view. The dock
// flags itself with this, so the count dropping always has a reason on screen
// beside it.
export function filtersApply() {
  return state.hiddenCollections.size > 0 || state.highlightedZones.size > 0 ||
    Boolean(state.gridEnabled && state.gridCell);
}
