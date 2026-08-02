import { isCollectionHidden } from "./collections.js";
import { elements } from "./dom.js";
import { saveSession } from "./session.js";
import { state } from "./state.js";
import { applyCategoryVisual, applyCategoryGlyph, initials } from "./theme.js";
import { formatNumber } from "./util.js";

// The dock lists at most this many rows. Every annotation still renders on
// the canvas; the list is a shortlist, and it says when it is one.
const dockListLimit = 100;

// The dock's results pane: every feature that survives the current search and
// collection filters, alphabetically, capped -- named shape features listed
// beside the locations, since ground is as findable as anything standing on
// it. With a search underway the list is exactly the matches; with none it is
// a browsable index of what the legend has left visible.
export function renderSearchResults() {
  if (!elements.searchResults) return;
  const eligible = [
    ...state.features
      .filter((pin) => !pin.filteredHidden)
      .map((pin) => ({ title: pin.location.title, pin })),
    ...[...state.zoneRecords.values()]
      .filter((record) => record.zone.title &&
        !isCollectionHidden(record.zone.collectionId) &&
        (!state.search || record.zone.title.toLocaleLowerCase().includes(state.search)))
      .map((record) => ({ title: record.zone.title, record })),
  ].sort((a, b) => a.title.localeCompare(b.title));
  const shown = eligible.slice(0, dockListLimit);
  const fragment = document.createDocumentFragment();
  if (!shown.length) {
    const empty = document.createElement("p");
    empty.className = "search-empty";
    empty.textContent = state.search
      ? "No visible features match."
      : "Nothing is enabled. Show a collection to list its features.";
    fragment.append(empty);
  }
  for (const entry of shown) {
    fragment.append(entry.pin ? pinResult(entry.pin) : zoneResult(entry.record));
  }
  if (eligible.length > shown.length) {
    const note = document.createElement("p");
    note.className = "dock-note";
    note.textContent = `First ${shown.length} of ${formatNumber(eligible.length)} — ` +
      "search or filter to narrow the list.";
    fragment.append(note);
  }
  elements.searchResults.replaceChildren(fragment);
  updateDockReadout(eligible.length);
  updateDockRail();
}

function pinResult(pin) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "search-result";
  if (state.selectedPin === pin) button.classList.add("is-selected");
  button.dataset.location = String(pin.location.id);
  applyCategoryVisual(button, pin.category);
  const dot = document.createElement("span");
  dot.className = "result-dot";
  applyCategoryGlyph(dot, pin.category, initials(pin.category.title));
  const copy = document.createElement("span");
  const title = document.createElement("strong");
  title.textContent = pin.location.title;
  const category = document.createElement("small");
  category.textContent = [pin.category.title, pin.category.group].filter(Boolean).join(" · ");
  copy.append(title, category);
  button.append(dot, copy);
  return button;
}

// A shape feature's row wears a swatch of the zone's own colour where a pin
// wears its category glyph, and jumps the way its legend row does.
function zoneResult(record) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "search-result";
  if (state.selectedZone === record.zone) button.classList.add("is-selected");
  button.dataset.zone = String(record.zone.id);
  const dot = document.createElement("span");
  dot.className = "result-dot";
  dot.style.setProperty("background", record.color);
  const copy = document.createElement("span");
  const title = document.createElement("strong");
  title.textContent = record.zone.title;
  const collection = document.createElement("small");
  collection.textContent = record.collection?.title || "Zone";
  copy.append(title, collection);
  button.append(dot, copy);
  return button;
}

// Folded, the dock is a rail with one word on it, and that word read the same
// whether or not a location was open: the map had a pin picked out and the
// only panel saying which one was the one just put away. The rail carries the
// name until the pin is closed, so folding the panel costs the view of the map
// and nothing else.
const railNameLimit = 28;

export function updateDockRail() {
  const title = state.selectedPin?.location.title || "";
  // The rail runs the height of the window, and a long name would run past the
  // bottom of it. Cut it where the reader can still recognise the place.
  const shown = title.length > railNameLimit
    ? `${title.slice(0, railNameLimit - 1).trimEnd()}…`
    : title;
  elements.dockRailName.textContent = shown ? ` · ${shown}` : "";
  elements.dockRailName.hidden = !shown;
}

function updateDockReadout(count) {
  elements.dockCount.textContent =
    `${formatNumber(count)} ${count === 1 ? "feature" : "features"}`;
  if (state.search) {
    elements.dockFlag.textContent = `“${state.search}”`;
    elements.dockFlag.hidden = false;
  } else if (state.hiddenCollections.size > 0) {
    elements.dockFlag.textContent = "filtered";
    elements.dockFlag.hidden = false;
  } else {
    elements.dockFlag.hidden = true;
  }
}

// The panel starts out of the way, because the map is the reason the window is
// open, and comes out by itself the first time it has something to say -- a pin
// opened, or a search with results in it. Once, and only until the reader has
// answered the question by putting it away: after that it stays where they put
// it and the map is theirs.
export function revealDock() {
  if (!state.dockFolded || state.dockDismissed) return;
  setDockFolded(false);
}

// The reader working the control themselves is what settles it. Folding it by
// hand is the answer that stops it opening again; unfolding by hand withdraws
// that answer, so a panel put away and brought back is once more a panel that
// keeps up with what is selected.
export function foldDockByHand(folded) {
  state.dockDismissed = folded;
  setDockFolded(folded);
}

// Put away rather than closed: the rail keeps the count's place and the way
// back. Like the overview, the choice is remembered with the volume.
export function setDockFolded(folded, remember = true) {
  state.dockFolded = folded;
  elements.dock.classList.toggle("is-folded", folded);
  elements.dockFold.setAttribute("aria-expanded", String(!folded));
  const label = folded ? "Bring the panel back (⌘⌥B)" : "Put the panel away (⌘⌥B)";
  elements.dockFold.setAttribute("aria-label", label);
  elements.dockFold.setAttribute("title", label);
  if (remember) saveSession();
}
