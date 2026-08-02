// The filtering side of the collections model. Every feature belongs to a
// named collection, and highlighting reads AND across collections, OR within
// one: two districts highlighted widens the question, a district and a
// subwatershed narrows it to the ground they share.

import { state } from "./state.js";

// collectionOf names the collection a shape feature belongs to: the id it
// was stamped with when its collection was rendered.
export function collectionOf(zone) {
  return zone?.collectionId;
}

// collectionFor answers with the collection itself, for anything that needs
// its attributes -- the label ladder, the stroke width -- rather than its id.
export function collectionFor(zone) {
  return state.world?.collectionsById?.get(zone?.collectionId);
}

// anyShapeCollectionVisible says whether any ground is drawn at all: the
// zone layers stay up while one shape collection is shown, and each feature's
// own style answers for its collection from there.
export function anyShapeCollectionVisible() {
  return (state.world?.collections || []).some(
    (collection) => collection.kind !== "point" && !isCollectionHidden(collection.id),
  );
}

// isCollectionHidden is the one question every renderer asks about a
// collection: pins ask by their collection's id, shape features ask through
// collectionOf. Nothing outside the legend touches the set directly.
export function isCollectionHidden(collectionID) {
  return state.hiddenCollections.has(collectionID);
}

// groupByCollection buckets highlighted zone records under their collections,
// which is the shape the filter thinks in: each bucket a question, its
// members the acceptable answers.
export function groupByCollection(records) {
  const groups = new Map();
  for (const record of records) {
    const key = collectionOf(record.zone);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(record);
  }
  return groups;
}

// passesZoneFilters answers whether a coordinate survives the highlights: it
// must lie inside at least one highlighted zone of every collection holding
// any. Within a collection the zones are alternatives; across collections
// they are conditions.
export function passesZoneFilters(groups, coordinate) {
  for (const records of groups.values()) {
    const inside = records.some((record) =>
      record.geometries.some((geometry) => geometryContainsCoordinate(geometry, coordinate)));
    if (!inside) return false;
  }
  return true;
}

// Containment with a pixel of grace: a pin dropped on a zone's border was put
// there to mean the zone, and exact point-in-polygon arithmetic would flip it
// out over the width of the line it stands on.
export function geometryContainsCoordinate(geometry, coordinate) {
  if (geometry.intersectsCoordinate(coordinate)) return true;
  const closest = geometry.getClosestPoint(coordinate);
  const x = closest[0] - coordinate[0];
  const y = closest[1] - coordinate[1];
  return x * x + y * y <= 1;
}
