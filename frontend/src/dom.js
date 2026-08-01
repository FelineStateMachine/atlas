export const $ = (selector) => document.querySelector(selector);
export const elements = {
  game: $("#game-select"),
  map: $("#map-select"),
  variant: $("#variant-select"),
  variantField: $("#variant-field"),
  legend: $("#legend"),
  search: $("#pin-search"),
  searchResults: $("#dock-results"),
  dock: $("#dock"),
  dockCount: $("#dock-count"),
  dockFlag: $("#dock-flag"),
  dockFold: $("#dock-fold"),
  dockRailName: $("#dock-rail-name"),
  visibleCount: $("#visible-count"),
  viewport: $("#map"),
  layers: $("#layers"),
  zoneToggle: $("#zone-toggle"),
  zoneCount: $("#zone-count"),
  zoneIndex: $("#zone-index"),
  zoneList: $("#zone-list"),
  loading: $("#map-loading"),
  labelsHint: $("#labels-hint"),
  gridHint: $("#grid-hint"),
  soloChip: $("#solo-chip"),
  overview: $("#overview"),
  overviewShelf: $("#overview-shelf"),
  overviewDock: $("#overview-dock"),
  overviewCanvas: $("#overview-canvas"),
  overviewViewport: $("#overview-viewport"),
  gridNavigator: $("#grid-navigator"),
  gridInput: $("#grid-input"),
  gridBack: $("#grid-back"),
  subgridToggle: $("#subgrid-toggle"),
  detail: $("#pin-detail"),
  detailTitle: $("#detail-title"),
  detailCategory: $("#detail-category"),
  detailDescription: $("#detail-description"),
  detailID: $("#detail-id"),
  detailCoordinates: $("#detail-coordinates"),
  detailCell: $("#detail-cell"),
  detailCellField: $("#detail-cell-field"),
  detailLinks: $("#detail-links"),
  detailDot: $("#detail-dot"),
  sidebar: $("#sidebar"),
  mobileLegend: $("#mobile-legend"),
  fatal: $("#fatal-error"),
  fatalMessage: $("#fatal-message"),
};

export function populateSelect(select, items, labelKey, valueKey) {
  select.replaceChildren();
  items.forEach((item, index) => {
    const option = document.createElement("option");
    option.value = valueKey ? String(item[valueKey]) : (item.slug || String(index));
    option.textContent = item[labelKey];
    select.append(option);
  });
}
