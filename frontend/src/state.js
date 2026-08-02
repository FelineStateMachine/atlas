export const state = {
  catalog: null,
  volume: null,
  world: null,
  lens: null,
  engine: null,
  projection: null,
  layers: null,
  sources: null,
  // One hide-set across every kind of collection, keyed by the numeric ids
  // the wire declares. Nothing downstream cares which kind an id names; it
  // only asks isCollectionHidden.
  hiddenCollections: new Set(),
  collapsedSections: new Set(),
  // Which area and path rows have their feature index unfolded in the legend.
  expandedCollections: new Set(),
  // The point-feature registry: one pin record per packed location, built by
  // buildFeatures. Shape features keep their own registry in zoneRecords
  // below -- that name (and highlightedZones, focusedZoneID with it) stays as
  // it is because the diagnostics snapshot and the parity baselines grew up
  // around the zone spelling, and the records it holds really are the zone
  // layers' to draw.
  features: [],
  featureByID: new Map(),
  selectedPin: null,
  selectedZone: null,
  hoveredPin: null,
  labelsHeld: false,
  gridEnabled: false,
  // Which cell system divides the world, by slug; geohash until chosen
  // otherwise, and reset with the world so no world boots into a system it
  // does not offer.
  gridSystem: "geohash",
  // Whether the reader is looking at the planet instead of the chart: only
  // a world that declares itself a sphere can turn this on.
  globeActive: false,
  // Whether the cells are drawn, which is a separate question from whether one
  // of them is holding the view to a place.
  subgridVisible: true,
  gridCell: "",
  zoneRecords: new Map(),
  highlightedZones: new Set(),
  focusedZoneID: null,
  // The reader's word over a collection's label policy, keyed by collection
  // id. Written by the legend's label toggle, read by the policy ladder.
  labelOverrides: new Map(),
  search: "",
  fitZoom: 0,
  zoneTitleCount: 0,
  eligibleLocations: 0,
  tileRun: 0,
  tileStats: { requested: 0, loaded: 0, errors: 0, peakPending: 0 },
  overviewRun: 0,
  overviewKey: "",
  overviewPointer: null,
  overviewDocked: false,
  dockFolded: true,
  // Whether the reader has put the panel away themselves. Until they have, it
  // comes out on its own the first time there is something in it to read.
  dockDismissed: false,
  renderedShard: 0,
  worldRun: 0,
  textByWorld: new Map(),
  restore: null,
  settling: false,
  styleCache: new Map(),
  markerIcons: new Map(),
};
