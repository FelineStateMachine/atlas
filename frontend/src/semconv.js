// The viewer's side of the semantic conventions: the attribute keys a bundle
// may speak, read leniently. An attribute the payload declares wins; a
// payload from before the conventions falls back to the legacy field that
// used to carry the meaning; a key this build has never heard of is simply
// ignored, because a bundle is never refused over vocabulary.

import { collectionOf } from "./collections.js";
import { state } from "./state.js";

// labelPolicy answers whether a zone's name draws on its own or waits to be
// asked: "always" or "quiet". The reader's per-collection override wins,
// then the collection's declared word, then the zone's own. The v2 wire has
// no collections to speak, so today the zone answers for its collection --
// and silence means "always", which is what every bundle from before the
// key already meant.
export function labelPolicy(zone, collection) {
  return state.labelOverrides.get(collection?.id ?? collectionOf(zone)) ??
    collection?.attrs?.["atlas.label.policy"] ??
    zone?.attrs?.["atlas.label.policy"] ??
    "always";
}

// renderAs answers how a category draws: "pin" or "text". This is the one
// display rule the viewer holds, spelled once.
export function renderAs(category) {
  const declared = category.attrs?.["atlas.render.as"];
  if (declared) return declared;
  return category.displayType === "text" ? "text" : "pin";
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
