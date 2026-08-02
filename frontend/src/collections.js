// The filtering side of the coming collections model, spoken early against
// the v2 wire. When the unified format lands, every feature will belong to a
// named collection and highlighting will read AND across collections, OR
// within one: two districts highlighted widens the question, a district and a
// subwatershed narrows it to the ground they share. That logic lives here
// now, while every zone still belongs to one implicit collection -- over a
// single collection the AND is exactly the union the map has always drawn,
// so nothing looks different until a second collection exists to conjoin.

import { state } from "./state.js";

// collectionOf names the collection a zone belongs to. The v2 wire never
// says, so there is exactly one answer; at flag day this reads what the wire
// declares, and nothing else has to learn the new vocabulary.
export function collectionOf(zone) {
  return "zones";
}

// isCollectionHidden is the one question every renderer asks about a
// collection: pins ask by their category's id, shape features ask through
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
