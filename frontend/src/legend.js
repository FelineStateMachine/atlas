import { elements } from "./dom.js";
import { state } from "./state.js";
import { saveSession } from "./session.js";
import { applyPinFilters } from "./features.js";
import { renderSearchResults } from "./search.js";
import { curatedLabelPolicy, labelPolicy } from "./semconv.js";
import { recountZoneTitles, syncZoneLayers } from "./areas.js";
import { applyCategoryVisual, applyCategoryGlyph, initials } from "./theme.js";
import { formatNumber } from "./util.js";

// The sidebar is one legend tree: shape collections, text labels, and pin
// groups are all sections of the same list, each holding rows of the same
// shape. Sections come from the wire's group strings, in order of first
// appearance; an ungrouped shape collection files under the viewer's own
// Zones section, which keeps the pre-collapse behaviour a place to land.
export function legendSections(collections) {
  const sections = [];
  const at = new Map();
  const place = (key, title, collection) => {
    if (!at.has(key)) {
      at.set(key, sections.length);
      sections.push({ key, title, collections: [] });
    }
    sections[at.get(key)].collections.push(collection);
  };
  const zones = [];
  for (const collection of collections) {
    if (collection.kind !== "point" && !collection.group) zones.push(collection);
    else place(`group-${collection.group}`, collection.group, collection);
  }
  // Text is how a point collection draws, not what it is: a collection of
  // cities rendered as floating names sits in its own group beside the
  // markers, wearing the toggle that flips it either way.
  if (zones.length) sections.unshift({ key: "zones", title: "Zones", collections: zones });
  return sections;
}

// A collection either packs its members as pin locations or carries them
// inline as shape features; either way the row's count is how many there are.
export function collectionKind(collection) {
  return collection.kind || "point";
}

export function collectionCount(collection) {
  return (collection.features || collection.locations || []).length;
}

// Collection ids ride the DOM as strings, but point categories are known
// everywhere by number. One reader turns the attribute back into the id the
// state sets actually hold.
export function collectionID(value) {
  return /^\d+$/.test(value) ? Number(value) : value;
}

export function findCollection(collectionID) {
  for (const section of state.world?.sections || []) {
    for (const collection of section.collections) {
      if (collection.id === collectionID) return collection;
    }
  }
  return null;
}

export function renderLegend() {
  const fragment = document.createDocumentFragment();
  for (const section of state.world.sections) {
    const element = document.createElement("section");
    element.className = "layer-section";
    element.dataset.layerSection = section.key;
    const members = section.collections.reduce((total, collection) => total + collectionCount(collection), 0);
    element.append(layerHeader(section.key, section.title, members));
    const toggles = document.createElement("div");
    toggles.className = "category-toggles";
    for (const collection of section.collections) {
      toggles.append(collectionRow(collection));
      // A shape row unfolds into its feature index. The container is laid
      // down empty here and filled by renderZoneIndex once the zone records
      // exist -- the legend renders before the zones do.
      if (collectionKind(collection) !== "point") {
        const index = document.createElement("div");
        index.className = "feature-index";
        index.dataset.featureIndex = String(collection.id);
        toggles.append(index);
      }
    }
    element.append(toggles);
    fragment.append(element);
  }
  elements.legend.replaceChildren(fragment);
  syncSectionCollapse();
  syncCollectionExpansion();
  syncSectionSwitches();
}

// Mirrors the markup every layer section reads the same by: disclosure on the
// left, one switch on the right.
export function layerHeader(key, title, count) {
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
export function onlyButton(label) {
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

// One row for every kind of collection: checkbox, an icon cell -- the
// category's glyph for points, an unfolding chevron for shapes -- the name,
// the isolate target, the label-policy toggle where names are drawn on the
// ground, and the count. The columns are fixed so every row lines up whether
// or not it uses them all.
export function collectionRow(collection) {
  const kind = collectionKind(collection);
  const row = document.createElement("label");
  row.className = "category-row";
  const checkbox = document.createElement("input");
  checkbox.type = "checkbox";
  checkbox.dataset.collection = String(collection.id);
  checkbox.checked = !state.hiddenCollections.has(collection.id);
  let icon;
  if (kind !== "point") {
    icon = document.createElement("button");
    icon.type = "button";
    icon.className = "collection-expand";
    icon.dataset.expandCollection = String(collection.id);
    icon.setAttribute("aria-label", `Unfold the ${collection.title} index`);
    icon.innerHTML =
      '<svg class="collection-chevron" viewBox="0 0 16 16" aria-hidden="true"><path d="m6 4 4 4-4 4"/></svg>' +
      (kind === "path"
        ? '<svg class="collection-kind" viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 12.5c3-6 8-2 11-9"/></svg>'
        : '<svg class="collection-kind collection-kind-area" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 4.2 12.6 11.6H3.4z"/></svg>');
  } else {
    icon = document.createElement("span");
    icon.className = "category-icon";
    applyCategoryVisual(row, collection);
    applyCategoryGlyph(icon, collection, initials(collection.title));
    icon.title = collection.icon || collection.title;
  }
  const name = document.createElement("span");
  name.className = "category-name";
  name.textContent = collection.title;
  // Overlaid on the count rather than appended: these pills wrap, and a row
  // that grows on hover would shove the one under the cursor somewhere else.
  const only = onlyButton(`Show only ${collection.title}`);
  only.dataset.onlyCollection = String(collection.id);
  // Areas draw their names on the ground, and whether they speak unasked is
  // the reader's to override. A point collection's override is which shape
  // it takes at all -- markers, or the floating names text categories used
  // to be set apart for. Paths keep the column as space, so the count stays
  // a column no row disagrees about.
  let labels;
  if (kind === "area") {
    labels = document.createElement("button");
    labels.type = "button";
    labels.className = "label-toggle";
    labels.dataset.labelToggle = String(collection.id);
    labels.innerHTML =
      '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 3.5h9M8 3.5v9"/></svg>';
    syncLabelToggle(labels, collection);
  } else if (kind === "point") {
    labels = document.createElement("button");
    labels.type = "button";
    labels.className = "label-toggle render-toggle";
    labels.dataset.labelToggle = String(collection.id);
    labels.textContent = "Tt";
    syncLabelToggle(labels, collection);
  } else {
    labels = document.createElement("span");
    labels.className = "label-toggle-spacer";
  }
  const count = document.createElement("span");
  count.className = "category-count";
  count.textContent = formatNumber(collectionCount(collection));
  row.append(checkbox, icon, name, only, labels, count);
  return row;
}

// There is one level of nesting per move here -- sections hold rows, rows may
// hold a feature index -- so folding by a depth and folding entirely are the
// same move, and only the one exists.
export function setAllSectionsCollapsed(collapsed) {
  state.collapsedSections.clear();
  if (collapsed) {
    for (const button of elements.layers.querySelectorAll("[data-section]")) {
      state.collapsedSections.add(button.dataset.section);
    }
  }
  syncSectionCollapse();
}

export function syncSectionCollapse() {
  for (const button of elements.layers.querySelectorAll("[data-section]")) {
    const collapsed = state.collapsedSections.has(button.dataset.section);
    button.setAttribute("aria-expanded", String(!collapsed));
    button.closest(".layer-section").classList.toggle("is-collapsed", collapsed);
  }
  saveSession();
}

export function toggleCollectionExpanded(collectionID) {
  if (state.expandedCollections.has(collectionID)) state.expandedCollections.delete(collectionID);
  else state.expandedCollections.add(collectionID);
  syncCollectionExpansion();
}

export function syncCollectionExpansion() {
  for (const button of elements.legend.querySelectorAll("[data-expand-collection]")) {
    const expanded = state.expandedCollections.has(collectionID(button.dataset.expandCollection));
    button.setAttribute("aria-expanded", String(expanded));
    button.closest(".category-row").classList.toggle("is-expanded", expanded);
  }
  for (const index of elements.legend.querySelectorAll(".feature-index")) {
    index.hidden = !state.expandedCollections.has(collectionID(index.dataset.featureIndex));
  }
  saveSession();
}

// The label toggle is a two-state affair around the effective policy: press
// it and the collection's names flip to the other word; if the other word is
// what the producer curated anyway, the override has nothing left to say and
// is dropped rather than stored.
export function toggleLabelPolicy(id) {
  const collection = findCollection(id);
  if (!collection) return;
  const flipped = labelPolicy(null, collection) === "always" ? "quiet" : "always";
  if (flipped === curatedLabelPolicy(collection)) state.labelOverrides.delete(collection.id);
  else state.labelOverrides.set(collection.id, flipped);
  syncLabelToggles();
  if (collection.kind === "point") {
    // A point collection's names ride the pin-label layer.
    state.layers.pinLabels.changed();
  } else {
    // The crowd-thinning threshold counts spoken names only, and the toggle
    // just changed which names speak; ground names are drawn by the title
    // layers, at either zoom depth.
    recountZoneTitles();
    state.layers.zoneTitles.changed();
    state.layers.zoneTitleDetail.changed();
  }
  saveSession();
}

// One toggle, one language: the button says what pressing it would do,
// "Hide X labels" while the names are speaking and "Display X labels" while
// they wait, whatever the kind of collection behind it.
function syncLabelToggle(button, collection) {
  const speaking = labelPolicy(null, collection) === "always";
  const help = `${speaking ? "Hide" : "Display"} ${collection.title} labels`;
  button.dataset.label = help;
  button.setAttribute("aria-label", help);
  button.title = help;
  button.setAttribute("aria-pressed", String(speaking));
}

export function syncLabelToggles() {
  for (const button of elements.legend.querySelectorAll("[data-label-toggle]")) {
    const collection = findCollection(collectionID(button.dataset.labelToggle));
    if (collection) syncLabelToggle(button, collection);
  }
}

export function setAllCollections(visible) {
  state.hiddenCollections.clear();
  if (!visible) {
    for (const section of state.world.sections) {
      for (const collection of section.collections) state.hiddenCollections.add(collection.id);
    }
  }
  syncLegendCheckboxes();
  syncZoneLayers();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

export function syncLegendCheckboxes() {
  for (const checkbox of document.querySelectorAll("[data-collection]")) {
    checkbox.checked = !state.hiddenCollections.has(collectionID(checkbox.dataset.collection));
  }
}

export function toggleSection(key) {
  const section = state.world.sections.find((item) => item.key === key);
  if (!section) return;
  const hasVisible = section.collections.some((collection) => !state.hiddenCollections.has(collection.id));
  for (const collection of section.collections) {
    if (hasVisible) state.hiddenCollections.add(collection.id);
    else state.hiddenCollections.delete(collection.id);
  }
  syncLegendCheckboxes();
  syncZoneLayers();
  applyPinFilters();
  syncSectionSwitches();
}

// Isolating is the common request of a long legend: "just the Korok Seeds",
// out of a hundred and sixty categories. Everything else is hidden rather than
// remembered, so Show all is the single, obvious way back.
export function showOnly(target) {
  if (!state.world) return;
  // Asking to isolate what is already isolated means the reader is done with
  // it, so the same control lets them back out.
  if (isOnly(target)) {
    setAllCollections(true);
    return;
  }
  state.hiddenCollections.clear();
  for (const section of state.world.sections) {
    const wanted = target.section === section.key;
    for (const collection of section.collections) {
      if (!wanted && target.collection !== collection.id) {
        state.hiddenCollections.add(collection.id);
      }
    }
  }
  syncLegendCheckboxes();
  syncZoneLayers();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

// True when what is on screen is already exactly what this target would isolate.
export function isOnly(target) {
  for (const section of state.world.sections) {
    for (const collection of section.collections) {
      const wanted = target.section === section.key || target.collection === collection.id;
      if (wanted === state.hiddenCollections.has(collection.id)) return false;
    }
  }
  return true;
}

// Derived rather than remembered, so the chip is right however the state was
// reached -- including by switching collections off one at a time.
export function updateSoloChip() {
  const chip = elements.soloChip;
  if (!state.world) {
    chip.hidden = true;
    return;
  }
  let onlyVisible = null;
  let visibleCount = 0;
  let soleSection = null;
  let sectionsShowing = 0;
  for (const section of state.world.sections) {
    let shown = 0;
    for (const collection of section.collections) {
      if (state.hiddenCollections.has(collection.id)) continue;
      shown++;
      visibleCount++;
      onlyVisible = collection;
    }
    if (shown > 0) {
      sectionsShowing++;
      soleSection = shown === section.collections.length ? section : null;
    }
  }
  let label = "";
  if (visibleCount === 1 && onlyVisible) label = onlyVisible.title;
  else if (sectionsShowing === 1 && soleSection) label = soleSection.title;
  chip.hidden = !label;
  chip.textContent = label ? `only: ${label}` : "";
  chip.title = label ? `Showing only ${label} — click to show everything` : "";
}

export function syncSectionSwitches() {
  if (!state.world) return;
  for (const section of state.world.sections) {
    const input = elements.legend.querySelector(`input[data-section-toggle="${section.key}"]`);
    if (input) {
      input.checked = section.collections.some((collection) => !state.hiddenCollections.has(collection.id));
    }
  }
  updateSoloChip();
  saveSession();
}
