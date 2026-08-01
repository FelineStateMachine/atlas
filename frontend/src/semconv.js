// The viewer's side of the semantic conventions: the attribute keys a bundle
// may speak, read leniently. An attribute the payload declares wins; a
// payload from before the conventions falls back to the legacy field that
// used to carry the meaning; a key this build has never heard of is simply
// ignored, because a bundle is never refused over vocabulary.

// renderAs answers how a category draws: "pin" or "text". This is the one
// display rule the viewer holds, spelled once.
export function renderAs(category) {
  const declared = category.attrs?.["atlas.render.as"];
  if (declared) return declared;
  return category.displayType === "text" ? "text" : "pin";
}

// mapSurface answers what the map's raster pictures. A map that says
// nothing is a plane, which every map was until the planets arrived.
export function mapSurface(map) {
  return map?.attrs?.["atlas.geometry.surface"] || "plane";
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
  };
}
