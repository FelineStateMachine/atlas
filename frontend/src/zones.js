import Feature from "ol/Feature.js";
import { createEmpty, extend, getCenter } from "ol/extent.js";
import Point from "ol/geom/Point.js";
import MultiLineString from "ol/geom/MultiLineString.js";
import MultiPolygon from "ol/geom/MultiPolygon.js";
import Polygon from "ol/geom/Polygon.js";

import { anyShapeCollectionVisible, collectionOf, isCollectionHidden } from "./collections.js";
import { elements } from "./dom.js";
import { state } from "./state.js";
import { showFeature } from "./detail.js";
import { collectionID, syncLegendCheckboxes, syncSectionSwitches } from "./legend.js";
import { viewMaxZoom } from "./navigation.js";
import { onActiveShard, refreshPinRendering, updateZonePinFocus } from "./pins.js";
import { labelPolicy } from "./semconv.js";
import { colorFor } from "./theme.js";

export function renderZones() {
  state.renderedShard = state.lens?.shard || 0;
  state.sources.zones.clear();
  state.sources.zoneTitles.clear();
  state.zoneRecords.clear();
  state.highlightedZones.clear();
  state.focusedZoneID = null;
  state.zoneTitleCount = 0;
  syncZoneLayers();

  for (const collection of state.world.collections) {
    if (collection.kind === "point") continue;
    for (const zone of collection.features || []) {
      if (!onActiveShard(zone)) continue;
      // Each shape feature learns its collection's id here, once, so
      // everything downstream -- styles, filters, the label ladder -- can
      // ask the feature alone.
      zone.collectionId = collection.id;
      const zoneExtent = createEmpty();
      const geometries = [];
      let hasGeometry = false;
      for (const rawGeometry of zone.geometry || []) {
        const geometry = projectZoneGeometry(rawGeometry);
        if (!geometry) continue;
        hasGeometry = true;
        geometries.push(geometry);
        extend(zoneExtent, geometry.getExtent());
        state.sources.zones.addFeature(new Feature({
          geometry,
          zone,
          color: colorFor(zone.id),
          child: zone.parent != null,
        }));
      }
      if (!hasGeometry) continue;
      const color = colorFor(zone.id);
      const center = zone.center ? project(zone.center.lat, zone.center.lng) : getCenter(zoneExtent);
      const span = Math.max(zoneExtent[2] - zoneExtent[0], zoneExtent[3] - zoneExtent[1]);
      state.sources.zoneTitles.addFeature(new Feature({
        geometry: new Point(center),
        zone,
        color,
        child: zone.parent != null,
        span,
        priority: (zone.parent == null ? 2_000_000 : 1_000_000) + Math.round(span),
      }));
      state.zoneRecords.set(zone.id, {
        zone,
        collection,
        extent: zoneExtent.slice(),
        geometries,
        color,
      });
    }
  }
  recountZoneTitles();
  renderZoneScrim();
  renderZoneIndex();
}

// Only names that draw on their own crowd the map, so only they press on
// the threshold that starts thinning them. A hundred quiet zones cost the
// spoken ones nothing. Recounted whenever the policy moves -- the legend's
// label toggle changes the answer without rebuilding a single zone.
export function recountZoneTitles() {
  state.zoneTitleCount = state.sources.zoneTitles.getFeatures()
    .filter((feature) => labelPolicy(feature.get("zone")) !== "quiet").length;
}

// Highlighting a zone is a request to look at one place, so everything else
// recedes: the whole map is covered and the zone is cut back out of it.
// Shading only the neighbouring zones demoted them rather than raising the
// zone asked for, and left the unclaimed space between them at full strength.
export function renderZoneScrim() {
  state.sources.zoneScrim.clear();
  if (!state.highlightedZones.size) return;
  const [left, bottom, right, top] = state.projection.getExtent();
  // Rings alternate direction with their depth so the fill counts its way in
  // and out: the map, then the zone as a hole, then any hole of the zone's own
  // back to solid.
  const rings = [orientRing([
    [left, bottom], [right, bottom], [right, top], [left, top], [left, bottom],
  ], true)];
  for (const zoneID of state.highlightedZones) {
    for (const geometry of state.zoneRecords.get(zoneID)?.geometries || []) {
      // A line zone has no ring to cut from the scrim: the highlighted path
      // draws above it instead, at its own weight.
      if (geometry.getType() === "MultiLineString") continue;
      const polygons = geometry.getType() === "MultiPolygon"
        ? geometry.getPolygons()
        : [geometry];
      for (const polygon of polygons) {
        polygon.getLinearRings().forEach((ring, index) => {
          rings.push(orientRing(ring.getCoordinates(), index > 0));
        });
      }
    }
  }
  state.sources.zoneScrim.addFeature(new Feature({ geometry: new Polygon(rings) }));
}

// Twice the signed area of a ring: negative when its points run clockwise.
export function orientRing(ring, clockwise) {
  let area = 0;
  for (let index = 0, prior = ring.length - 1; index < ring.length; prior = index++) {
    area += ring[prior][0] * ring[index][1] - ring[index][0] * ring[prior][1];
  }
  return (area < 0) === clockwise ? ring : ring.slice().reverse();
}

// The feature index under an unfolded shape row: one button per zone, in the
// parent-first order of the ground itself, each container listing its own
// collection's features.
export function renderZoneIndex() {
  for (const container of elements.legend.querySelectorAll(".feature-index")) {
    const fragment = document.createDocumentFragment();
    for (const { zone, depth } of orderedZones(collectionID(container.dataset.featureIndex))) {
      const record = state.zoneRecords.get(zone.id);
      if (!record) continue;
      const button = document.createElement("button");
      button.type = "button";
      button.className = "zone-index-item";
      button.dataset.zone = String(zone.id);
      button.style.setProperty("--zone-color", record.color);
      button.style.setProperty("--zone-depth", String(depth));
      button.setAttribute("aria-pressed", "false");
      button.title = `${zone.title}: click to jump; right-click to toggle highlight`;

      const marker = document.createElement("span");
      marker.className = "zone-index-marker";
      marker.setAttribute("aria-hidden", "true");
      const title = document.createElement("span");
      title.textContent = zone.title;
      button.append(marker, title);
      fragment.append(button);
    }
    container.replaceChildren(fragment);
  }
}

export function orderedZones(collectionId) {
  const zones = [...state.zoneRecords.values()]
    .filter((record) => record.zone.collectionId === collectionId)
    .map((record) => record.zone);
  const zoneIDs = new Set(zones.map((zone) => zone.id));
  const children = new Map();
  for (const zone of zones) {
    const parentID = zoneIDs.has(zone.parent) ? zone.parent : null;
    if (!children.has(parentID)) children.set(parentID, []);
    children.get(parentID).push(zone);
  }
  for (const entries of children.values()) {
    entries.sort((left, right) => left.title.localeCompare(right.title, undefined, {
      numeric: true,
      sensitivity: "base",
    }));
  }

  const ordered = [];
  const visited = new Set();
  const append = (zone, depth) => {
    if (visited.has(zone.id)) return;
    visited.add(zone.id);
    ordered.push({ zone, depth });
    for (const child of children.get(zone.id) || []) append(child, depth + 1);
  };
  for (const zone of children.get(null) || []) append(zone, 0);
  for (const zone of zones) append(zone, 0);
  return ordered;
}

// The four zone layers answer the ledger the same way the pins do: drawn
// while their collection is shown, and away while it is hidden. Hiding also
// withdraws the hidden collection's highlights -- a filter must not keep
// conditioning the map from under a layer the reader has put away.
export function syncZoneLayers() {
  const visible = anyShapeCollectionVisible();
  state.layers.zoneScrim.setVisible(visible);
  state.layers.zones.setVisible(visible);
  state.layers.zoneTitles.setVisible(visible);
  state.layers.zoneTitleDetail.setVisible(visible);
  let pruned = false;
  for (const zoneID of [...state.highlightedZones]) {
    const record = state.zoneRecords.get(zoneID);
    if (!record || isCollectionHidden(collectionOf(record.zone))) {
      state.highlightedZones.delete(zoneID);
      pruned = true;
    }
  }
  if (pruned) {
    updateZonePinFocus();
    renderZoneScrim();
    updateZoneIndexState();
  }
}

export function jumpToZone(zoneID) {
  const record = state.zoneRecords.get(zoneID);
  if (!record) return;
  state.focusedZoneID = zoneID;
  updateZoneIndexState();
  showFeature(record.zone);
  state.engine.getView().fit(record.extent, {
    size: state.engine.getSize(),
    padding: [54, 54, 54, 54],
    maxZoom: viewMaxZoom(state.lens),
    duration: 220,
  });
  elements.sidebar.classList.remove("is-open");
  elements.mobileLegend.setAttribute("aria-expanded", "false");
}

export function toggleZoneHighlight(zoneID) {
  const record = state.zoneRecords.get(zoneID);
  if (!record) return;
  if (state.highlightedZones.has(zoneID)) state.highlightedZones.delete(zoneID);
  else {
    state.highlightedZones.add(zoneID);
    // Highlighting unhides: asking to look at a zone and keeping its layer
    // away cannot both be meant.
    if (state.hiddenCollections.delete(collectionOf(record.zone))) {
      syncLegendCheckboxes();
      syncSectionSwitches();
    }
    syncZoneLayers();
  }
  state.hoveredPin = null;
  updateZonePinFocus();
  refreshPinRendering();
  updateZoneIndexState();
  renderZoneScrim();
  state.layers.zones.changed();
  state.layers.zoneTitles.changed();
  state.layers.zoneTitleDetail.changed();
}

export function updateZoneIndexState() {
  for (const container of elements.legend.querySelectorAll(".feature-index")) {
    container.classList.toggle("has-highlights", state.highlightedZones.size > 0);
  }
  for (const button of elements.legend.querySelectorAll("[data-zone]")) {
    const zoneID = Number(button.dataset.zone);
    const highlighted = state.highlightedZones.has(zoneID);
    button.classList.toggle("is-highlighted", highlighted);
    button.classList.toggle("is-current", state.focusedZoneID === zoneID);
    button.setAttribute("aria-pressed", String(highlighted));
  }
}

export function projectZoneGeometry(rawGeometry) {
  if (!rawGeometry?.coordinates) return null;
  const point = ([longitude, latitude]) => project(latitude, longitude);
  if (rawGeometry.type === "Polygon") {
    return new Polygon(rawGeometry.coordinates.map((ring) => ring.map(point)));
  }
  if (rawGeometry.type === "MultiPolygon") {
    return new MultiPolygon(
      rawGeometry.coordinates.map((polygon) => polygon.map((ring) => ring.map(point))),
    );
  }
  // A path zone is a line and a weight: the geometry stays the line, and the
  // zone's declared stroke width says how wide it is drawn and clicked.
  if (rawGeometry.type === "MultiLineString") {
    return new MultiLineString(rawGeometry.coordinates.map((line) => line.map(point)));
  }
  return null;
}

// A pin arrives as a latitude and longitude in the source tile grid, which is
// not the same grid for every volume: most worlds are cut from a 32-tile square at
// zoom 13, and a map cut from a window of its own carries where that window
// sits. Both describe the same world square, so everything downstream of this
// -- the view, the overview, the geohash grid -- works in units either way.
export function project(latitude, longitude) {
  const grid = mapTileGrid();
  const worldTiles = 2 ** grid.sourceZoom;
  const xTile = ((longitude + 180) / 360) * worldTiles;
  const latitudeRadians = latitude * Math.PI / 180;
  const yTile = (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2 * worldTiles;
  return [
    (xTile - grid.firstTile) * grid.tileSize,
    -(yTile - grid.firstTile) * grid.tileSize,
  ];
}

export function mapTileGrid() {
  const shared = state.volume.tileGrid;
  const own = state.world?.grid;
  return own ? { ...shared, ...own } : shared;
}
