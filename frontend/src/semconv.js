// The viewer's side of the semantic conventions: the attribute keys a bundle
// may speak, read leniently. An attribute the payload declares wins; a
// payload from before the conventions falls back to the legacy field that
// used to carry the meaning; a key this build has never heard of is simply
// ignored, because a bundle is never refused over vocabulary.

import { collectionFor } from "./collections.js";
import { state } from "./state.js";

// The registry's vocabularies, mirrored: what shape of thing a collection
// holds, and whether names draw unasked.
export const geometryKinds = Object.freeze({ point: "point", path: "path", area: "area" });
export const labelPolicies = Object.freeze({ always: "always", quiet: "quiet" });

// labelPolicy answers whether a zone's name draws on its own or waits to be
// asked: "always" or "quiet". The reader's per-collection override wins,
// then the collection's declared word, then the kind's own default -- areas
// speak unasked, which is what every map before the key already did, and
// paths wait.
export function labelPolicy(zone, collection) {
  const declared = collection ?? collectionFor(zone);
  const override = state.labelOverrides.get(declared?.id);
  if (override) return override;
  const curated = declared?.attrs?.["atlas.label.policy"];
  if (curated) return curated;
  return declared?.kind === geometryKinds.path ? labelPolicies.quiet : labelPolicies.always;
}

// renderAs answers how a point collection draws: "pin" or "text". This is
// the one display rule the viewer holds, spelled once; a collection saying
// nothing is markers, and since the v3 wire every producer says. Text is a
// capability of an ordinary point collection, not a kind of its own, so the
// reader's override outranks the curation the same way label policy does.
export function renderAs(collection) {
  return state.renderOverrides.get(collection?.id) ??
    (collection.attrs?.["atlas.render.as"] || "pin");
}

// The labels the detail card gives a feature's own attributes. Anything in
// the reserved rendering namespaces is machinery rather than material, and
// the geographic pair already has rows of its own.
const attributeLabels = { "atlas.hydro.huc12": "HUC-12" };
const attributeHidden = new Set(["atlas.geo.lat", "atlas.geo.lon"]);
const reservedPrefixes = ["atlas.render.", "atlas.label.", "atlas.stroke.", "atlas.geometry."];

// featureAttributeRows turns a feature's attributes into the rows its card
// shows: curated label where one exists, the raw key where none does, and
// nothing at all for machinery.
export function featureAttributeRows(attrs) {
  const rows = [];
  for (const [key, value] of Object.entries(attrs || {})) {
    if (attributeHidden.has(key)) continue;
    if (reservedPrefixes.some((prefix) => key.startsWith(prefix))) continue;
    rows.push({ label: attributeLabels[key] || key, value });
  }
  return rows.sort((left, right) => left.label.localeCompare(right.label));
}

// worldSurface answers what the map's raster pictures. A map that says
// nothing is a plane, which every map was until the planets arrived.
export function worldSurface(map) {
  return map?.attrs?.["atlas.geometry.surface"] || "plane";
}

// geoMapping reads whichever flattening a map declares -- equirectangular
// or Web-Mercator -- and answers with the same two functions either way:
// world pixels to true degrees and back. Consumers that only need "where
// on the planet is this pixel" ask here; the globe keeps asking for
// equirect specifically, because only that projection drapes a sphere
// without resampling.
export function geoMapping(map) {
  const equirect = equirectMapping(map);
  if (equirect) return equirect;
  return mercatorMapping(map);
}

// mercatorMapping is the Web-Mercator reader: x linear in longitude, y
// linear in the projected latitude asinh(tan phi), which is what a
// real-world tile window actually is.
export function mercatorMapping(map) {
  const attrs = map?.attrs || {};
  if (attrs["atlas.geometry.projection"] !== "mercator") return null;
  const px = (attrs["atlas.geometry.mercator.px"] || "").split(",").map(Number);
  const deg = (attrs["atlas.geometry.mercator.deg"] || "").split(",").map(Number);
  if (px.length !== 4 || deg.length !== 4 || px.some(Number.isNaN) || deg.some(Number.isNaN)) {
    return null;
  }
  const [x, y, w, h] = px;
  const [west, north, east, south] = deg;
  if (!w || !h || north === south || west === east) return null;
  const project = (lat) => Math.asinh(Math.tan((lat * Math.PI) / 180));
  const unproject = (value) => (Math.atan(Math.sinh(value)) * 180) / Math.PI;
  const top = project(north);
  const bottom = project(south);
  return {
    toLatLng(worldX, worldY) {
      return [
        unproject(top - ((worldY - y) / h) * (top - bottom)),
        west + ((worldX - x) / w) * (east - west),
      ];
    },
    toWorld(lat, lng) {
      return [
        x + ((lng - west) / (east - west)) * w,
        y + ((top - project(lat)) / (top - bottom)) * h,
      ];
    },
  };
}

// equirectMapping reads the flattening a spherical map declares: the raster
// window it fills in world pixels and the ground that window pictures in
// degrees. It returns an inverter from world pixels back to true latitude
// and longitude -- the whole story any reader needs to stand a packed pin
// on the sphere -- or null when the declaration is absent or malformed,
// which a validated bundle never is.
export function equirectMapping(map) {
  const attrs = map?.attrs || {};
  if (attrs["atlas.geometry.projection"] !== "equirect") return null;
  const px = (attrs["atlas.geometry.equirect.px"] || "").split(",").map(Number);
  const deg = (attrs["atlas.geometry.equirect.deg"] || "").split(",").map(Number);
  if (px.length !== 4 || deg.length !== 4 || px.some(Number.isNaN) || deg.some(Number.isNaN)) {
    return null;
  }
  const [x, y, w, h] = px;
  const [west, north, east, south] = deg;
  if (!w || !h || north === south || west === east) return null;
  return {
    toLatLng(worldX, worldY) {
      return [
        north - ((worldY - y) / h) * (north - south),
        west + ((worldX - x) / w) * (east - west),
      ];
    },
    toWorld(lat, lng) {
      return [
        x + ((lng - west) / (east - west)) * w,
        y + ((north - lat) / (north - south)) * h,
      ];
    },
  };
}
