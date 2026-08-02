import { worldText } from "./catalog.js";
import { applicableSystems } from "./cellsystems/index.js";
import { geohashCellAt } from "./cellsystems/geohash.js";
import { collectionFor, geometryContainsCoordinate } from "./collections.js";
import { elements } from "./dom.js";
import { syncLegendCheckboxes, syncSectionSwitches } from "./legend.js";
import { viewMaxZoom } from "./navigation.js";
import { applyPinFilters, onActiveShard, refreshPrioritySource } from "./features.js";
import { renderSearchResults, revealDock } from "./search.js";
import { featureAttributeRows, geoMapping } from "./semconv.js";
import { state } from "./state.js";
import { applyCategoryVisual, applyCategoryGlyph, colorFor, initials } from "./theme.js";
import { cleanDescription, formatNumber } from "./util.js";

export function revealPin(pin) {
  state.hiddenCollections.delete(pin.category.id);
  syncLegendCheckboxes();
  applyPinFilters();
  syncSectionSwitches();
  showPin(pin, true);
}

export function showPin(pin, focus = false) {
  state.selectedPin = pin;
  state.selectedZone = null;
  elements.detailCoordinatesField.hidden = false;
  // Anything else drawing the selection -- the globe -- hears about it the
  // same moment the chart's layers do.
  document.dispatchEvent(new Event("atlas:selection"));
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.pinLabels.changed();
  state.layers.text.changed();
  state.layers.textDetail.changed();
  state.layers.priority.changed();
  elements.detailTitle.textContent = pin.location.title;
  elements.detailCategory.textContent =
    [pin.category.group, pin.category.title].filter(Boolean).join(" / ");
  elements.detailDescription.textContent = "";
  elements.detailLinks.hidden = true;
  clearFeatureRows();
  fillPinText(pin);
  elements.detailID.textContent = String(pin.location.id);
  elements.detailCoordinates.textContent =
    `${pin.location.lat.toFixed(6)}, ${pin.location.lng.toFixed(6)}`;
  // A latitude of this map's own is hard to hold in the head and harder to
  // compare, so the pin also names the cell it stands in -- the same three
  // characters the grid draws and the navigator takes, which say where a place
  // is by saying which part of the map it is in. A pin off the ground the grid
  // divides has no cell to name, and says nothing rather than the nearest one.
  const cell = geohashCellAt(pin.coordinate);
  elements.detailCell.textContent = cell;
  elements.detailCellField.hidden = !cell;
  // Every other system that can divide this map speaks for the point in
  // its own address, each layer owning the attribute it mints: one row
  // per system, after the geohash row the markup already keeps.
  for (const stray of elements.detail.querySelectorAll("[data-cell-system-row]")) {
    stray.remove();
  }
  for (const system of applicableSystems(state.world)) {
    if (system.slug === "geohash") continue;
    const row = system.locate(pin.coordinate, state.world);
    if (!row) continue;
    const field = document.createElement("div");
    field.dataset.cellSystemRow = system.slug;
    const term = document.createElement("dt");
    term.textContent = row.label;
    const value = document.createElement("dd");
    value.textContent = row.value;
    field.append(term, value);
    elements.detailCellField.after(field);
  }
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
      zoom: Math.min(viewMaxZoom(state.lens), Math.max(view.getZoom() || 0, 4)),
      duration: 220,
    });
  }
}

// showFeature opens the panel on a shape feature: its title and collection,
// its prose when the archive carries any -- fetched the same lazy way a
// pin's is -- its measure on the ground where the world declares a geographic
// mapping, and whatever attributes it carries in its own right. Ground has
// no single coordinate or cell, so those rows step aside.
export function showFeature(zone) {
  state.selectedPin = null;
  state.selectedZone = zone;
  document.dispatchEvent(new Event("atlas:selection"));
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.priority.changed();
  // A quiet zone speaks its name for as long as it is the one being read
  // about, so selection repaints the chips.
  state.layers.zoneTitles.changed();
  state.layers.zoneTitleDetail.changed();
  const collection = collectionFor(zone);
  elements.detailTitle.textContent = zone.title;
  elements.detailCategory.textContent =
    [collection?.group, collection?.title].filter(Boolean).join(" / ") ||
    zone.subtitle || "Zone";
  elements.detailDescription.textContent = "";
  elements.detailLinks.hidden = true;
  elements.detailLinks.replaceChildren();
  elements.detailID.textContent = String(zone.id);
  elements.detailCoordinatesField.hidden = true;
  elements.detailCellField.hidden = true;
  for (const stray of elements.detail.querySelectorAll("[data-cell-system-row]")) {
    stray.remove();
  }
  const origin = state.world?.merged?.find((account) => account.origin)?.source || "";
  elements.detailSource.textContent = origin;
  elements.detailSourceField.hidden = !origin;
  elements.detailDot.textContent = "";
  elements.detailDot.removeAttribute("style");
  elements.detailDot.style.setProperty("background", colorFor(zone.id));
  clearFeatureRows();
  renderFeatureRows([...featureMeasureRows(zone, collection), ...featureAttributeRows(zone.attrs)]);
  if (zone.hasText) void fillZoneText(zone);
  else elements.detailDescription.textContent = "No description is included in the archive.";
  elements.detail.hidden = false;
  revealDock();
  renderSearchResults();
}

// featureMeasureRows says what a feature's ground amounts to, in the units of
// the planet -- so only where the world declares a mapping back to one. An
// area gets its extent and how many visible locations stand inside it; a path
// gets its length.
function featureMeasureRows(zone, collection) {
  const record = state.zoneRecords.get(zone.id);
  if (!record || !collection) return [];
  const rows = [];
  const mapping = geoMapping(state.world);
  if (collection.kind === "area") {
    if (mapping) {
      const km2 = record.geometries.reduce((sum, geometry) => sum + geometryAreaKm2(geometry, mapping), 0);
      if (km2 > 0) rows.push({ label: "Area", value: formatKm2(km2) });
    }
    const inside = state.features.filter((pin) =>
      !pin.filteredHidden && onActiveShard(pin.location) &&
      record.geometries.some((geometry) => geometryContainsCoordinate(geometry, pin.coordinate))).length;
    rows.push({ label: "Locations inside", value: formatNumber(inside) });
  }
  if (collection.kind === "path" && mapping) {
    const km = record.geometries.reduce((sum, geometry) => sum + geometryLengthKm(geometry, mapping), 0);
    if (km > 0) rows.push({ label: "Length", value: formatKm(km) });
  }
  return rows;
}

// The measures below flatten small grounds locally: latitude and longitude
// come back through the world's declared mapping, and a city-sized ring is
// near enough planar that the shoelace over locally scaled kilometres is the
// honest figure.
const kmPerDegreeLat = 110.574;

function toLatLng(mapping, [x, y]) {
  return mapping.toLatLng(x, -y);
}

function ringAreaKm2(ring, mapping) {
  const points = ring.map((position) => toLatLng(mapping, position));
  if (points.length < 3) return 0;
  const midLat = points.reduce((sum, point) => sum + point[0], 0) / points.length;
  const kmPerDegreeLng = 111.320 * Math.cos((midLat * Math.PI) / 180);
  let doubled = 0;
  for (let index = 0, prior = points.length - 1; index < points.length; prior = index++) {
    const [latA, lngA] = points[prior];
    const [latB, lngB] = points[index];
    doubled += lngA * kmPerDegreeLng * latB * kmPerDegreeLat -
      lngB * kmPerDegreeLng * latA * kmPerDegreeLat;
  }
  return Math.abs(doubled) / 2;
}

function geometryAreaKm2(geometry, mapping) {
  const polygons = geometry.getType() === "MultiPolygon"
    ? geometry.getPolygons()
    : geometry.getType() === "Polygon" ? [geometry] : [];
  let total = 0;
  for (const polygon of polygons) {
    polygon.getLinearRings().forEach((ring, index) => {
      const area = ringAreaKm2(ring.getCoordinates(), mapping);
      total += index === 0 ? area : -area;
    });
  }
  return Math.max(0, total);
}

function geometryLengthKm(geometry, mapping) {
  const lines = geometry.getType() === "MultiLineString"
    ? geometry.getCoordinates()
    : geometry.getType() === "LineString" ? [geometry.getCoordinates()] : [];
  let total = 0;
  for (const line of lines) {
    for (let index = 1; index < line.length; index++) {
      const [latA, lngA] = toLatLng(mapping, line[index - 1]);
      const [latB, lngB] = toLatLng(mapping, line[index]);
      const midLat = ((latA + latB) / 2) * (Math.PI / 180);
      const dx = (lngB - lngA) * 111.320 * Math.cos(midLat);
      const dy = (latB - latA) * kmPerDegreeLat;
      total += Math.hypot(dx, dy);
    }
  }
  return total;
}

function formatKm2(km2) {
  return `${km2 >= 10 ? formatNumber(Math.round(km2)) : km2.toFixed(2)} km²`;
}

function formatKm(km) {
  if (km < 1) return `${Math.round(km * 1000)} m`;
  return `${km >= 10 ? formatNumber(Math.round(km)) : km.toFixed(1)} km`;
}

// The card's own rows -- a feature's measures and attributes -- live in the
// same definition list the fixed rows do, marked so the next selection can
// sweep them.
function clearFeatureRows() {
  for (const stray of elements.detail.querySelectorAll("[data-feature-row]")) {
    stray.remove();
  }
}

function renderFeatureRows(rows) {
  let anchor = elements.detailSourceField;
  for (const row of rows) {
    const field = document.createElement("div");
    field.dataset.featureRow = "";
    const term = document.createElement("dt");
    term.textContent = row.label;
    const value = document.createElement("dd");
    value.textContent = row.value;
    field.append(term, value);
    anchor.after(field);
    anchor = field;
  }
}

// The words belonging to a zone arrive with the same text payload a pin's
// do, and the same rule holds: a zone closed or changed while they were on
// the way is left alone.
async function fillZoneText(zone) {
  const text = await worldText();
  if (state.selectedZone !== zone) return;
  const entry = text[String(zone.id)] || {};
  elements.detailDescription.textContent =
    cleanDescription(entry.d) || "No description is included in the archive.";
}

// pinSource reads a pin's provenance out of the map's merge account: a pin in
// a source-titled group or adopted into a native category came from that
// source, a pin another source matched is the origin's word corroborated, and
// every other pin is the origin's alone. The index is built once per map and
// thrown away with it.
function pinSource(pin) {
  const merged = state.world?.merged;
  if (!merged?.length) return "";
  const origin = merged.find((account) => account.origin)?.source || "";
  if (state.pinSourceIndex?.world !== state.world) {
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
    state.pinSourceIndex = { world: state.world, byID };
  }
  for (const account of merged) {
    if (!account.origin && pin.category.group === account.source) return account.source;
  }
  return state.pinSourceIndex.byID.get(pin.location.id) || origin;
}

// The words belonging to a pin are fetched the first time one is opened, so a
// map that is only ever looked at never pays for them. The panel opens on what
// is already known and fills in when they arrive; a pin closed or changed in
// the meantime is left alone.
export async function fillPinText(pin) {
  const text = await worldText();
  if (state.selectedPin !== pin) return;
  const entry = text[String(pin.location.id)] || {};
  pin.location.description = entry.d || "";
  pin.location.links = entry.l || [];
  elements.detailDescription.textContent =
    cleanDescription(entry.d) || "No description is included in the archive.";
  // A point feature's attributes travel with its text, so its card rows --
  // the HUC-12 of a pin that knows its subwatershed -- arrive with the words.
  clearFeatureRows();
  renderFeatureRows(featureAttributeRows(entry.a));
  renderDetailLinks(pin);
}

// The source wrote these as mapgenie URLs. They are rebuilt as in-app jumps so
// they still work with no network, and dropped when the target is not on this
// map.
export function renderDetailLinks(pin) {
  const links = (pin.location.links || []).filter((link) => state.featureByID.has(link.locationId));
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
  const hadSelection = state.selectedPin !== null || state.selectedZone !== null;
  state.selectedPin = null;
  state.selectedZone = null;
  document.dispatchEvent(new Event("atlas:selection"));
  if (state.sources) {
    refreshPrioritySource();
    state.layers.pins.changed();
    state.layers.pinLabels.changed();
    state.layers.text.changed();
    state.layers.textDetail.changed();
    state.layers.priority.changed();
    // A quiet zone that spoke while selected falls silent again.
    state.layers.zoneTitles.changed();
    state.layers.zoneTitleDetail.changed();
  }
  elements.detail.hidden = true;
  if (hadSelection) renderSearchResults();
}
