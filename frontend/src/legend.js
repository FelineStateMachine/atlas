import { elements } from "./dom.js";
import { state } from "./state.js";
import { saveSession } from "./session.js";
import { applyPinFilters } from "./pins.js";
import { renderSearchResults } from "./search.js";
import { renderAs } from "./semconv.js";
import { applyCategoryVisual, applyCategoryGlyph, initials } from "./theme.js";
import { formatNumber } from "./util.js";

// Categories drawn as text are labels for the ground itself -- Area, Region,
// Province -- and are read and edited as one set. Gathered here rather than
// left in place, where "Area" sits between Altar and Bank and reads like
// another kind of marker.
export function legendSections(groups) {
  const sections = [];
  const text = [];
  for (const group of groups) {
    const drawn = [];
    for (const category of group.categories) {
      (renderAs(category) === "text" ? text : drawn).push(category);
    }
    if (drawn.length) {
      sections.push({ key: `group-${group.id}`, title: group.title, categories: drawn });
    }
  }
  // Above the pin groups, under the zones: labels and boundaries both say where
  // you are, rather than what is worth going to.
  if (text.length) sections.unshift({ key: "text", title: "Text", categories: text });
  return sections;
}

export function renderLegend() {
  const fragment = document.createDocumentFragment();
  for (const section of state.world.sections) {
    const element = document.createElement("section");
    element.className = "layer-section";
    element.dataset.layerSection = section.key;
    const locations = section.categories.reduce((total, category) => total + category.locations.length, 0);
    element.append(layerHeader(section.key, section.title, locations));
    const toggles = document.createElement("div");
    toggles.className = "category-toggles";
    for (const category of section.categories) toggles.append(categoryToggle(category));
    element.append(toggles);
    fragment.append(element);
  }
  elements.legend.replaceChildren(fragment);
  syncSectionCollapse();
  syncSectionSwitches();
}

// Mirrors the markup of the static zones header so every layer section reads the
// same: disclosure on the left, one switch on the right.
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

export function categoryToggle(category) {
  const isText = renderAs(category) === "text";
  const row = document.createElement("label");
  row.className = "category-row";
  applyCategoryVisual(row, category);
  const checkbox = document.createElement("input");
  checkbox.type = "checkbox";
  checkbox.dataset.category = String(category.id);
  checkbox.checked = !state.hiddenCategories.has(category.id);
  const icon = document.createElement("span");
  if (isText) {
    icon.className = "text-symbol";
    icon.textContent = "Tt";
    icon.title = "Drawn as a text label";
  } else {
    icon.className = "category-icon";
    applyCategoryGlyph(icon, category, initials(category.title));
    icon.title = category.icon || category.title;
  }
  const name = document.createElement("span");
  name.className = "category-name";
  name.textContent = category.title;
  const locations = document.createElement("span");
  locations.className = "category-count";
  locations.textContent = formatNumber(category.locations.length);
  // Overlaid on the count rather than appended: these pills wrap, and a row
  // that grows on hover would shove the one under the cursor somewhere else.
  const only = onlyButton(`Show only ${category.title}`);
  only.dataset.onlyCategory = String(category.id);
  row.append(checkbox, icon, name, only, locations);
  return row;
}

// There is one level of nesting here -- sections hold rows, not more sections --
// so folding by a depth and folding entirely are the same move, and only the
// one exists.
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

export function setAllCategories(visible) {
  state.hiddenCategories.clear();
  if (!visible) {
    for (const group of state.world.groups) {
      for (const category of group.categories) state.hiddenCategories.add(category.id);
    }
  }
  syncLegendCheckboxes();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

export function syncLegendCheckboxes() {
  for (const checkbox of document.querySelectorAll("[data-category]")) {
    checkbox.checked = !state.hiddenCategories.has(Number(checkbox.dataset.category));
  }
}

export function toggleSection(key) {
  const section = state.world.sections.find((item) => item.key === key);
  if (!section) return;
  const hasVisible = section.categories.some((category) => !state.hiddenCategories.has(category.id));
  for (const category of section.categories) {
    if (hasVisible) state.hiddenCategories.add(category.id);
    else state.hiddenCategories.delete(category.id);
  }
  syncLegendCheckboxes();
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
    setAllCategories(true);
    return;
  }
  state.hiddenCategories.clear();
  for (const section of state.world.sections) {
    const wanted = target.section === section.key;
    for (const category of section.categories) {
      if (!wanted && target.category !== category.id) {
        state.hiddenCategories.add(category.id);
      }
    }
  }
  syncLegendCheckboxes();
  applyPinFilters();
  renderSearchResults();
  syncSectionSwitches();
}

// True when what is on screen is already exactly what this target would isolate.
export function isOnly(target) {
  for (const section of state.world.sections) {
    for (const category of section.categories) {
      const wanted = target.section === section.key || target.category === category.id;
      if (wanted === state.hiddenCategories.has(category.id)) return false;
    }
  }
  return true;
}

// Derived rather than remembered, so the chip is right however the state was
// reached -- including by switching categories off one at a time.
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
    for (const category of section.categories) {
      if (state.hiddenCategories.has(category.id)) continue;
      shown++;
      visibleCount++;
      onlyVisible = category;
    }
    if (shown > 0) {
      sectionsShowing++;
      soleSection = shown === section.categories.length ? section : null;
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
      input.checked = section.categories.some((category) => !state.hiddenCategories.has(category.id));
    }
  }
  updateSoloChip();
  saveSession();
}
