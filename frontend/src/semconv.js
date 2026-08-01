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
