(() => {
  "use strict";

  const $ = (selector) => document.querySelector(selector);
  const elements = {
    game: $("#game-select"),
    map: $("#map-select"),
    variant: $("#variant-select"),
    variantField: $("#variant-field"),
    title: $("#map-title"),
    meta: $("#map-meta"),
    legend: $("#legend"),
    search: $("#pin-search"),
    searchResults: $("#search-results"),
    visibleCount: $("#visible-count"),
    viewport: $("#map"),
    world: $("#world"),
    image: $("#map-image"),
    zones: $("#zones"),
    zoneLabels: $("#zone-labels"),
    overlayControls: $("#overlay-controls"),
    textOverlayControls: $("#text-overlay-controls"),
    zoneControl: $("#zone-control"),
    zoneToggle: $("#zone-toggle"),
    zoneCount: $("#zone-count"),
    markers: $("#markers"),
    loading: $("#map-loading"),
    detail: $("#pin-detail"),
    detailTitle: $("#detail-title"),
    detailCategory: $("#detail-category"),
    detailDescription: $("#detail-description"),
    detailID: $("#detail-id"),
    detailCoordinates: $("#detail-coordinates"),
    detailDot: $("#detail-dot"),
    sidebar: $("#sidebar"),
    mobileLegend: $("#mobile-legend"),
    fatal: $("#fatal-error"),
    fatalMessage: $("#fatal-message"),
  };

  const state = {
    catalog: null,
    game: null,
    map: null,
    variant: null,
    hiddenCategories: new Set(),
    pins: [],
    markerPins: [],
    textPins: [],
    eligibleMarkerPins: [],
    eligibleTextPins: [],
    pinByID: new Map(),
    selectedPin: null,
    zoneLabels: [],
    zonesVisible: true,
    annotationGrid: new Map(),
    eligibleLocations: 0,
    renderedMarkers: 0,
    renderedText: 0,
    view: { x: 0, y: 0, scale: 0.1, fitScale: 0.1, minScale: 0.05, maxScale: 4 },
    drag: null,
    search: "",
  };

  const palette = [
    "#d6f36b", "#72d5f4", "#ff9e64", "#df83ff", "#62e6ae",
    "#ff6f91", "#f4d35e", "#8aa9ff", "#e7a56d", "#83d483",
  ];

  async function start() {
    bindEvents();
    try {
      const response = await fetch("/static/catalog.json");
      if (!response.ok) {
        throw new Error(`catalog request returned ${response.status}`);
      }
      state.catalog = await response.json();
      if (!state.catalog.games.length) {
        throw new Error("the embedded catalog contains no maps");
      }
      populateSelect(elements.game, state.catalog.games, "title");
      selectGame(state.catalog.games[0].slug);
    } catch (error) {
      elements.loading.hidden = true;
      elements.fatalMessage.textContent = error instanceof Error ? error.message : String(error);
      elements.fatal.hidden = false;
    }
  }

  function bindEvents() {
    elements.game.addEventListener("change", () => selectGame(elements.game.value));
    elements.map.addEventListener("change", () => selectMap(Number(elements.map.value)));
    elements.variant.addEventListener("change", () => selectVariant(Number(elements.variant.value)));

    const changeCategoryVisibility = (event) => {
      const checkbox = event.target.closest("[data-category]");
      if (!checkbox) return;
      const categoryID = Number(checkbox.dataset.category);
      if (checkbox.checked) state.hiddenCategories.delete(categoryID);
      else state.hiddenCategories.add(categoryID);
      applyPinFilters();
      updateGroupHeadings();
    };
    elements.legend.addEventListener("change", changeCategoryVisibility);
    elements.textOverlayControls.addEventListener("change", changeCategoryVisibility);

    elements.legend.addEventListener("click", (event) => {
      const button = event.target.closest("[data-group]");
      if (!button) return;
      const group = state.map.groups.find((item) => item.id === Number(button.dataset.group));
      if (!group) return;
      const categories = markerCategories(group);
      const hasVisible = categories.some((category) => !state.hiddenCategories.has(category.id));
      for (const category of categories) {
        if (hasVisible) state.hiddenCategories.add(category.id);
        else state.hiddenCategories.delete(category.id);
      }
      syncLegendCheckboxes();
      applyPinFilters();
      updateGroupHeadings();
    });

    $("#show-all").addEventListener("click", () => setAllCategories(true));
    $("#hide-all").addEventListener("click", () => setAllCategories(false));
    elements.zoneToggle.addEventListener("change", () => {
      state.zonesVisible = elements.zoneToggle.checked;
      layoutAnnotations();
    });
    elements.search.addEventListener("input", () => {
      state.search = elements.search.value.trim().toLocaleLowerCase();
      applyPinFilters();
      renderSearchResults();
    });

    elements.searchResults.addEventListener("click", (event) => {
      const result = event.target.closest("[data-location]");
      if (!result) return;
      const pin = state.pinByID.get(Number(result.dataset.location));
      if (pin) revealPin(pin);
    });

    elements.markers.addEventListener("click", (event) => {
      const marker = event.target.closest("[data-location]");
      if (!marker) return;
      event.stopPropagation();
      const pin = state.pinByID.get(Number(marker.dataset.location));
      if (pin) showPin(pin);
    });

    $("#close-detail").addEventListener("click", closeDetail);
    $("#zoom-in").addEventListener("click", () => zoomAt(1.35));
    $("#zoom-out").addEventListener("click", () => zoomAt(1 / 1.35));
    $("#reset-view").addEventListener("click", fitMap);

    elements.viewport.addEventListener("wheel", (event) => {
      event.preventDefault();
      const rect = elements.viewport.getBoundingClientRect();
      zoomAt(Math.exp(-event.deltaY * 0.0012), event.clientX - rect.left, event.clientY - rect.top);
    }, { passive: false });

    elements.viewport.addEventListener("pointerdown", beginDrag);
    elements.viewport.addEventListener("pointermove", moveDrag);
    elements.viewport.addEventListener("pointerup", endDrag);
    elements.viewport.addEventListener("pointercancel", endDrag);
    elements.viewport.addEventListener("dblclick", (event) => {
      if (event.target.closest(".pin, .map-label")) return;
      const rect = elements.viewport.getBoundingClientRect();
      zoomAt(1.6, event.clientX - rect.left, event.clientY - rect.top);
    });

    elements.viewport.addEventListener("keydown", (event) => {
      if (event.key === "+" || event.key === "=") zoomAt(1.3);
      else if (event.key === "-") zoomAt(1 / 1.3);
      else if (event.key === "0") fitMap();
      else if (event.key === "Escape") closeDetail();
      else return;
      event.preventDefault();
    });

    window.addEventListener("keydown", (event) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
        event.preventDefault();
        elements.search.focus();
        elements.search.select();
      }
    });
    window.addEventListener("resize", () => constrainView());

    elements.mobileLegend.addEventListener("click", () => {
      const open = elements.sidebar.classList.toggle("is-open");
      elements.mobileLegend.setAttribute("aria-expanded", String(open));
    });
  }

  function selectGame(slug) {
    state.game = state.catalog.games.find((game) => game.slug === slug) || state.catalog.games[0];
    elements.game.value = state.game.slug;
    populateSelect(elements.map, state.game.maps, "title", "id");
    selectMap(state.game.maps[0].id);
  }

  function selectMap(id) {
    state.map = state.game.maps.find((map) => map.id === id) || state.game.maps[0];
    elements.map.value = String(state.map.id);
    populateSelect(elements.variant, state.map.variants, "name");
    elements.variantField.hidden = state.map.variants.length < 2;
    state.hiddenCategories.clear();
    for (const group of state.map.groups) {
      for (const category of group.categories) {
        if (!category.visible) state.hiddenCategories.add(category.id);
      }
    }
    state.search = "";
    elements.search.value = "";
    elements.searchResults.hidden = true;
    closeDetail();
    renderLegend();
    renderZones();
    buildPins();
    selectVariant(0, true);
    elements.title.textContent = state.map.title;
    elements.meta.textContent = `${state.game.title} · ${formatNumber(state.map.pinCount)} archived locations`;
    elements.sidebar.classList.remove("is-open");
    elements.mobileLegend.setAttribute("aria-expanded", "false");
  }

  function selectVariant(index, resetView = false) {
    state.variant = state.map.variants[index] || state.map.variants[0];
    elements.variant.value = String(state.map.variants.indexOf(state.variant));
    const bounds = activeBounds();
    const size = state.catalog.tileGrid.size;
    elements.image.style.clipPath = `inset(${bounds.y}px ${size - bounds.x - bounds.width}px ${size - bounds.y - bounds.height}px ${bounds.x}px)`;
    elements.loading.hidden = false;
    elements.image.alt = `${state.game.title} — ${state.map.title}, ${state.variant.name} map`;
    elements.image.onload = () => {
      elements.loading.hidden = true;
      if (resetView) fitMap();
      else applyTransform();
    };
    elements.image.onerror = () => {
      elements.loading.hidden = true;
      elements.fatalMessage.textContent = `The embedded map image “${state.variant.image}” could not be decoded.`;
      elements.fatal.hidden = false;
    };
    elements.image.src = `/static/maps/${encodeURIComponent(state.variant.image)}`;
    if (elements.image.complete && elements.image.naturalWidth) {
      elements.loading.hidden = true;
      if (resetView) fitMap();
      else applyTransform();
    }
  }

  function populateSelect(select, items, labelKey, valueKey) {
    select.replaceChildren();
    items.forEach((item, index) => {
      const option = document.createElement("option");
      option.value = valueKey ? String(item[valueKey]) : (item.slug || String(index));
      option.textContent = item[labelKey];
      select.append(option);
    });
  }

  function renderLegend() {
    const fragment = document.createDocumentFragment();
    const overlayFragment = document.createDocumentFragment();
    let textOverlayCount = 0;
    for (const group of state.map.groups) {
      for (const category of textCategories(group)) {
        overlayFragment.append(categoryToggle(category, true));
        textOverlayCount++;
      }

      const categories = markerCategories(group);
      if (!categories.length) continue;
      const section = document.createElement("section");
      section.className = "legend-group";
      section.dataset.groupSection = String(group.id);

      const heading = document.createElement("button");
      heading.type = "button";
      heading.className = "group-heading";
      heading.dataset.group = String(group.id);
      heading.title = `Toggle every category in ${group.title}`;
      const title = document.createElement("span");
      title.textContent = group.title;
      const count = document.createElement("span");
      count.dataset.groupCount = String(group.id);
      heading.append(title, count);
      section.append(heading);

      for (const category of categories) section.append(categoryToggle(category, false));
      fragment.append(section);
    }
    elements.textOverlayControls.replaceChildren(overlayFragment);
    elements.legend.replaceChildren(fragment);
    elements.overlayControls.hidden = !textOverlayCount && !(state.map.zones || []).length;
    updateGroupHeadings();
  }

  function categoryToggle(category, promoted) {
    const row = document.createElement("label");
    row.className = promoted ? "overlay-control" : "category-row";
    applyCategoryVisual(row, category);

    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.dataset.category = String(category.id);
    checkbox.checked = !state.hiddenCategories.has(category.id);

    const check = document.createElement("span");
    check.className = "check";
    check.setAttribute("aria-hidden", "true");

    const icon = document.createElement("span");
    icon.className = promoted ? "text-symbol" : "category-icon";
    if (promoted) icon.textContent = "Tt";
    else applyCategoryGlyph(icon, category, initials(category.title));
    icon.title = promoted ? "Text overlay" : (category.icon || category.title);

    const name = document.createElement("span");
    name.className = "category-name";
    name.textContent = promoted ? `${category.title} titles` : category.title;

    const locations = document.createElement("span");
    locations.className = promoted ? "overlay-count" : "category-count";
    locations.textContent = formatNumber(category.locations.length);

    row.append(checkbox, check, icon, name, locations);
    return row;
  }

  function textCategories(group) {
    return group.categories.filter((category) => category.displayType === "text");
  }

  function markerCategories(group) {
    return group.categories.filter((category) => category.displayType !== "text");
  }

  function buildPins() {
    const fragment = document.createDocumentFragment();
    state.pins = [];
    state.markerPins = [];
    state.textPins = [];
    state.pinByID.clear();
    for (const group of state.map.groups) {
      for (const category of group.categories) {
        for (const location of category.locations) {
          const point = project(location.lat, location.lng);
          const marker = document.createElement("button");
          marker.type = "button";
          marker.className = category.displayType === "text" ? "map-label" : "pin";
          marker.dataset.location = String(location.id);
          marker.style.left = `${point.x}px`;
          marker.style.top = `${point.y}px`;
          applyCategoryVisual(marker, category);
          marker.setAttribute("aria-label", `${location.title}, ${category.title}`);
          marker.title = location.title;
          if (category.displayType === "text") marker.textContent = location.title;
          else applyCategoryGlyph(marker, category, initials(category.title));

          const pin = {
            location,
            category,
            group,
            point,
            marker,
            detailRatio: category.displayType === "text" ? textDetailRatio(category) : 1,
            filteredHidden: false,
          };
          state.pins.push(pin);
          if (category.displayType === "text") state.textPins.push(pin);
          else state.markerPins.push(pin);
          state.pinByID.set(location.id, pin);
          fragment.append(marker);
        }
      }
    }
    state.markerPins.sort((a, b) =>
      a.category.locations.length - b.category.locations.length ||
      stableRank(a.location.id) - stableRank(b.location.id));
    elements.markers.replaceChildren(fragment);
    applyPinFilters();
  }

  function renderZones() {
    const zones = state.map.zones || [];
    state.zonesVisible = true;
    elements.zoneToggle.checked = true;
    elements.zoneControl.hidden = zones.length === 0;
    elements.overlayControls.hidden = !zones.length &&
      !state.map.groups.some((group) => textCategories(group).length);
    elements.zoneCount.textContent = formatNumber(zones.length);
    elements.zones.replaceChildren();
    elements.zoneLabels.replaceChildren();
    state.zoneLabels = [];
    if (!zones.length) return;

    const paths = document.createDocumentFragment();
    const labels = document.createDocumentFragment();
    for (const zone of zones) {
      const projected = projectZone(zone);
      if (!projected.path) continue;
      const color = colorFor(zone.id);
      const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute("d", projected.path);
      path.classList.add("zone-shape");
      if (zone.parentRegionId != null) path.classList.add("is-subregion");
      path.style.setProperty("--zone-color", color);
      paths.append(path);

      const center = zone.center ? project(zone.center.lat, zone.center.lng) : projected.center;
      const label = document.createElement("span");
      label.className = "zone-label";
      if (zone.parentRegionId != null) label.classList.add("is-subregion");
      label.style.left = `${center.x}px`;
      label.style.top = `${center.y}px`;
      label.style.setProperty("--zone-color", color);
      label.textContent = zone.title;
      labels.append(label);
      state.zoneLabels.push({
        element: label,
        child: zone.parentRegionId != null,
        width: projected.bounds.maxX - projected.bounds.minX,
        height: projected.bounds.maxY - projected.bounds.minY,
      });
    }
    elements.zones.append(paths);
    elements.zoneLabels.append(labels);
    layoutAnnotations();
  }

  function projectZone(zone) {
    let path = "";
    const points = [];
    for (const feature of zone.features || []) {
      const polygons = feature.type === "MultiPolygon" ? feature.coordinates : [feature.coordinates];
      for (const polygon of polygons) {
        for (const ring of polygon || []) {
          const projectedRing = ring.map(([longitude, latitude]) => project(latitude, longitude));
          if (!projectedRing.length) continue;
          path += `M${projectedRing.map((point) => `${point.x.toFixed(2)},${point.y.toFixed(2)}`).join("L")}Z`;
          points.push(...projectedRing);
        }
      }
    }
    if (!points.length) {
      return {
        path: "",
        center: { x: 0, y: 0 },
        bounds: { minX: 0, maxX: 0, minY: 0, maxY: 0 },
      };
    }
    const bounds = points.reduce((result, point) => ({
      minX: Math.min(result.minX, point.x),
      maxX: Math.max(result.maxX, point.x),
      minY: Math.min(result.minY, point.y),
      maxY: Math.max(result.maxY, point.y),
    }), { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
    return {
      path,
      center: { x: (bounds.minX + bounds.maxX) / 2, y: (bounds.minY + bounds.maxY) / 2 },
      bounds,
    };
  }

  function project(latitude, longitude) {
    const grid = state.catalog.tileGrid;
    const worldTiles = 2 ** grid.zoom;
    const xTile = ((longitude + 180) / 360) * worldTiles;
    const latitudeRadians = latitude * Math.PI / 180;
    const yTile = (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * worldTiles;
    return {
      x: (xTile - grid.firstTile) * grid.tileSize,
      y: (yTile - grid.firstTile) * grid.tileSize,
    };
  }

  function applyPinFilters() {
    for (const pin of state.pins) {
      const categoryHidden = state.hiddenCategories.has(pin.category.id);
      const searchHidden = state.search && !pin.location.title.toLocaleLowerCase().includes(state.search);
      pin.filteredHidden = Boolean(categoryHidden || searchHidden);
      pin.marker.hidden = pin.filteredHidden;
    }
    state.eligibleLocations = state.pins.filter((pin) => !pin.filteredHidden).length;
    state.eligibleMarkerPins = state.markerPins.filter((pin) => !pin.filteredHidden);
    state.eligibleTextPins = state.textPins.filter((pin) => !pin.filteredHidden);
    layoutAnnotations();
    if (state.selectedPin?.filteredHidden) closeDetail();
  }

  function renderSearchResults() {
    if (!state.search) {
      elements.searchResults.hidden = true;
      elements.searchResults.replaceChildren();
      return;
    }
    const matches = state.pins
      .filter((pin) => !state.hiddenCategories.has(pin.category.id) &&
        pin.location.title.toLocaleLowerCase().includes(state.search))
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

  function setAllCategories(visible) {
    state.hiddenCategories.clear();
    if (!visible) {
      for (const group of state.map.groups) {
        for (const category of group.categories) state.hiddenCategories.add(category.id);
      }
    }
    syncLegendCheckboxes();
    applyPinFilters();
    renderSearchResults();
    updateGroupHeadings();
  }

  function syncLegendCheckboxes() {
    for (const checkbox of document.querySelectorAll("[data-category]")) {
      checkbox.checked = !state.hiddenCategories.has(Number(checkbox.dataset.category));
    }
  }

  function updateGroupHeadings() {
    if (!state.map) return;
    for (const group of state.map.groups) {
      const categories = markerCategories(group);
      const visible = categories.filter((category) => !state.hiddenCategories.has(category.id)).length;
      const count = elements.legend.querySelector(`[data-group-count="${group.id}"]`);
      if (count) count.textContent = `${visible}/${categories.length}`;
    }
  }

  function revealPin(pin) {
    state.hiddenCategories.delete(pin.category.id);
    state.search = "";
    elements.search.value = "";
    elements.searchResults.hidden = true;
    syncLegendCheckboxes();
    applyPinFilters();
    updateGroupHeadings();
    showPin(pin, true);
  }

  function showPin(pin, focus = false) {
    if (state.selectedPin) state.selectedPin.marker.classList.remove("is-selected");
    state.selectedPin = pin;
    layoutAnnotations();
    pin.marker.classList.add("is-selected");
    elements.detailTitle.textContent = pin.location.title;
    elements.detailCategory.textContent = `${pin.group.title} / ${pin.category.title}`;
    elements.detailDescription.textContent = cleanDescription(pin.location.description) || "No description is included in the archive.";
    elements.detailID.textContent = String(pin.location.id);
    elements.detailCoordinates.textContent = `${pin.location.lat.toFixed(6)}, ${pin.location.lng.toFixed(6)}`;
    applyCategoryVisual(elements.detailDot, pin.category);
    applyCategoryGlyph(elements.detailDot, pin.category, initials(pin.category.title));
    elements.detail.hidden = false;

    if (focus) {
      const nextScale = Math.max(state.view.scale, 0.85);
      state.view.scale = Math.min(nextScale, state.view.maxScale);
      state.view.x = elements.viewport.clientWidth / 2 - pin.point.x * state.view.scale;
      state.view.y = elements.viewport.clientHeight / 2 - pin.point.y * state.view.scale;
      constrainView();
    }
  }

  function closeDetail() {
    if (state.selectedPin) state.selectedPin.marker.classList.remove("is-selected");
    state.selectedPin = null;
    elements.detail.hidden = true;
  }

  function fitMap() {
    const bounds = activeBounds();
    const width = elements.viewport.clientWidth;
    const height = elements.viewport.clientHeight;
    if (!width || !height) return;
    const scale = Math.min(width / bounds.width, height / bounds.height);
    state.view.fitScale = scale;
    state.view.minScale = scale * 0.72;
    state.view.scale = scale;
    state.view.x = (width - bounds.width * scale) / 2 - bounds.x * scale;
    state.view.y = (height - bounds.height * scale) / 2 - bounds.y * scale;
    applyTransform();
  }

  function zoomAt(factor, screenX = elements.viewport.clientWidth / 2, screenY = elements.viewport.clientHeight / 2) {
    const previous = state.view.scale;
    const next = clamp(previous * factor, state.view.minScale, state.view.maxScale);
    const worldX = (screenX - state.view.x) / previous;
    const worldY = (screenY - state.view.y) / previous;
    state.view.scale = next;
    state.view.x = screenX - worldX * next;
    state.view.y = screenY - worldY * next;
    constrainView();
  }

  function beginDrag(event) {
    if (event.button !== 0 || event.target.closest(".pin, .map-label, .zoom-controls, .pin-detail")) return;
    elements.viewport.setPointerCapture(event.pointerId);
    state.drag = {
      id: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      viewX: state.view.x,
      viewY: state.view.y,
    };
    elements.viewport.classList.add("is-dragging");
  }

  function moveDrag(event) {
    if (!state.drag || state.drag.id !== event.pointerId) return;
    state.view.x = state.drag.viewX + event.clientX - state.drag.startX;
    state.view.y = state.drag.viewY + event.clientY - state.drag.startY;
    constrainView();
  }

  function endDrag(event) {
    if (!state.drag || state.drag.id !== event.pointerId) return;
    state.drag = null;
    elements.viewport.classList.remove("is-dragging");
  }

  function constrainView() {
    const bounds = activeBounds();
    const scaledWidth = bounds.width * state.view.scale;
    const scaledHeight = bounds.height * state.view.scale;
    const width = elements.viewport.clientWidth;
    const height = elements.viewport.clientHeight;
    const marginX = Math.min(width * 0.38, scaledWidth * 0.38);
    const marginY = Math.min(height * 0.38, scaledHeight * 0.38);
    const offsetX = bounds.x * state.view.scale;
    const offsetY = bounds.y * state.view.scale;
    const contentX = state.view.x + offsetX;
    const contentY = state.view.y + offsetY;

    if (scaledWidth <= width) state.view.x = (width - scaledWidth) / 2 - offsetX;
    else state.view.x = clamp(contentX, width - scaledWidth - marginX, marginX) - offsetX;
    if (scaledHeight <= height) state.view.y = (height - scaledHeight) / 2 - offsetY;
    else state.view.y = clamp(contentY, height - scaledHeight - marginY, marginY) - offsetY;
    applyTransform();
  }

  function activeBounds() {
    const size = state.catalog?.tileGrid.size || 8192;
    return state.variant?.bounds || { x: 0, y: 0, width: size, height: size };
  }

  function applyTransform() {
    const { x, y, scale } = state.view;
    elements.world.style.transform = `translate3d(${x}px, ${y}px, 0) scale(${scale})`;
    // Annotations live in map coordinates but counter-scale into fixed CSS-pixel overlays.
    elements.world.style.setProperty("--viewport-overlay-scale", String(1 / scale));
    layoutAnnotations();
  }

  function layoutAnnotations() {
    state.annotationGrid.clear();
    updateTextLabelVisibility();
    updateZoneVisibility();
    updateMarkerVisibility();
    updateVisibleCount();
  }

  function updateTextLabelVisibility() {
    const fitScale = state.view.fitScale || state.view.scale;
    const candidates = [...state.eligibleTextPins]
      .sort((a, b) =>
        a.category.locations.length - b.category.locations.length ||
      stableRank(a.location.id) - stableRank(b.location.id));
    let rendered = 0;
    for (const pin of state.textPins) pin.marker.hidden = true;
    for (const pin of candidates) {
      const lacksDetail = state.view.scale < fitScale * pin.detailRatio;
      const selected = pin === state.selectedPin;
      if (lacksDetail && !selected) continue;
      const screen = screenPoint(pin.point);
      const width = Math.min(196, pin.location.title.length * 8.4 + 18);
      const shown = reserveAnnotation(screen.x, screen.y, width, 30, selected);
      pin.marker.hidden = !shown;
      if (shown) rendered++;
    }
    state.renderedText = rendered;
  }

  function updateZoneVisibility() {
    elements.zones.hidden = !state.zonesVisible;
    elements.zoneLabels.hidden = !state.zonesVisible;
    const showSubregions = state.view.scale >= state.view.fitScale * 3;
    const minimumSpan = state.zoneLabels.length > 100 ? 76 :
      state.zoneLabels.length > 40 ? 52 : 0;
    const candidates = [...state.zoneLabels].sort((a, b) =>
      Number(a.child) - Number(b.child) ||
      Math.max(b.width, b.height) - Math.max(a.width, a.height) ||
      a.element.textContent.localeCompare(b.element.textContent));
    for (const label of state.zoneLabels) label.element.hidden = true;
    for (const label of candidates) {
      const screenSpan = Math.max(label.width, label.height) * state.view.scale;
      const lacksDetail = minimumSpan > 0 && screenSpan < minimumSpan;
      if (!state.zonesVisible || lacksDetail || (label.child && !showSubregions)) continue;
      const point = {
        x: Number.parseFloat(label.element.style.left),
        y: Number.parseFloat(label.element.style.top),
      };
      const screen = screenPoint(point);
      const width = Math.min(220, label.element.textContent.length * 8.7 + 20);
      label.element.hidden = !reserveAnnotation(screen.x, screen.y, width, 30);
    }
  }

  function updateMarkerVisibility() {
    let rendered = 0;
    for (const pin of state.eligibleMarkerPins) {
      const selected = pin === state.selectedPin;
      const screen = screenPoint(pin.point);
      const shown = Boolean(state.search) || selected ||
        reserveAnnotation(screen.x, screen.y, 34, 34, selected);
      pin.marker.hidden = !shown;
      if (shown) rendered++;
    }
    state.renderedMarkers = rendered;
  }

  function screenPoint(point) {
    return {
      x: state.view.x + point.x * state.view.scale,
      y: state.view.y + point.y * state.view.scale,
    };
  }

  function reserveAnnotation(centerX, centerY, width, height, force = false) {
    const viewportWidth = elements.viewport.clientWidth;
    const viewportHeight = elements.viewport.clientHeight;
    const box = {
      left: centerX - width / 2 - 3,
      right: centerX + width / 2 + 3,
      top: centerY - height / 2 - 3,
      bottom: centerY + height / 2 + 3,
    };
    if (box.right < 0 || box.left > viewportWidth || box.bottom < 0 || box.top > viewportHeight) {
      return false;
    }

    const cellSize = 48;
    const minX = Math.floor(box.left / cellSize);
    const maxX = Math.floor(box.right / cellSize);
    const minY = Math.floor(box.top / cellSize);
    const maxY = Math.floor(box.bottom / cellSize);
    if (!force) {
      const checked = new Set();
      for (let x = minX; x <= maxX; x++) {
        for (let y = minY; y <= maxY; y++) {
          for (const existing of state.annotationGrid.get(`${x}:${y}`) || []) {
            if (checked.has(existing)) continue;
            checked.add(existing);
            if (box.left < existing.right && box.right > existing.left &&
              box.top < existing.bottom && box.bottom > existing.top) {
              return false;
            }
          }
        }
      }
    }
    for (let x = minX; x <= maxX; x++) {
      for (let y = minY; y <= maxY; y++) {
        const key = `${x}:${y}`;
        const entries = state.annotationGrid.get(key) || [];
        entries.push(box);
        state.annotationGrid.set(key, entries);
      }
    }
    return true;
  }

  function updateVisibleCount() {
    const rendered = state.renderedMarkers + state.renderedText;
    const eligible = state.eligibleLocations;
    if (rendered < eligible) {
      elements.visibleCount.textContent =
        `${formatNumber(eligible)} enabled · ${formatNumber(rendered)} drawn`;
      return;
    }
    elements.visibleCount.textContent =
      `${formatNumber(eligible)} ${eligible === 1 ? "location" : "locations"} visible`;
  }

  function textDetailRatio(category) {
    if (category.locations.length > 200) return 4;
    if (category.locations.length > 75) return 2.5;
    return 1;
  }

  function stableRank(value) {
    return (Math.imul(Number(value) || 0, 2654435761) >>> 0);
  }

  function colorFor(id) {
    const value = Math.abs(Number(id) || 0);
    return palette[(value * 2654435761 >>> 0) % palette.length];
  }

  function applyCategoryVisual(element, category) {
    element.style.setProperty("--pin-color", category.color || colorFor(category.id));
  }

  function applyCategoryGlyph(element, category, fallback) {
    element.classList.remove("has-source-icon");
    element.style.removeProperty("--pin-icon");
    element.textContent = "";
    if (!category.iconAsset) {
      element.textContent = fallback;
      return;
    }
    const path = category.iconAsset
      .split("/")
      .map((segment) => encodeURIComponent(segment))
      .join("/");
    element.style.setProperty("--pin-icon", `url("/static/icons/${path}")`);
    element.classList.add("has-source-icon");
  }

  function initials(value) {
    return value.split(/\s+/).slice(0, 2).map((part) => part[0] || "").join("");
  }

  function cleanDescription(value) {
    return String(value || "")
      .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
      .replace(/[*_`>#]/g, "")
      .replace(/\r/g, "")
      .replace(/\n{3,}/g, "\n\n")
      .trim();
  }

  function formatNumber(value) {
    return new Intl.NumberFormat().format(value);
  }

  function clamp(value, minimum, maximum) {
    return Math.min(maximum, Math.max(minimum, value));
  }

  start();
})();
