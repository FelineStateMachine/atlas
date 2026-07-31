import { elements } from "./dom.js";
import { state } from "./state.js";
import { applyCategoryVisual, applyCategoryGlyph, initials } from "./theme.js";

export function renderSearchResults() {
  if (!state.search) {
    elements.searchResults.hidden = true;
    elements.searchResults.replaceChildren();
    return;
  }
  const matches = state.pins
    .filter((pin) => !state.hiddenCategories.has(pin.category.id) &&
      pin.location.title.toLocaleLowerCase().includes(state.search))
    .sort((a, b) => a.location.title.localeCompare(b.location.title))
    .slice(0, 20);
  const fragment = document.createDocumentFragment();
  if (!matches.length) {
    const empty = document.createElement("p");
    empty.className = "search-empty";
    empty.textContent = "No visible locations match.";
    fragment.append(empty);
  }
  for (const pin of matches) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "search-result";
    button.dataset.location = String(pin.location.id);
    applyCategoryVisual(button, pin.category);
    const dot = document.createElement("span");
    dot.className = "result-dot";
    applyCategoryGlyph(dot, pin.category, initials(pin.category.title));
    const copy = document.createElement("span");
    const title = document.createElement("strong");
    title.textContent = pin.location.title;
    const category = document.createElement("small");
    category.textContent = `${pin.category.title} · ${pin.group.title}`;
    copy.append(title, category);
    button.append(dot, copy);
    fragment.append(button);
  }
  elements.searchResults.replaceChildren(fragment);
  elements.searchResults.hidden = false;
}
