export const state = {
  catalog: null,
  volume: null,
  world: null,
  lens: null,
  engine: null,
  projection: null,
  layers: null,
  sources: null,
  hiddenCategories: new Set(),
  collapsedSections: new Set(),
  pins: [],
  pinByID: new Map(),
  selectedPin: null,
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
  zonesVisible: true,
  zoneRecords: new Map(),
  highlightedZones: new Set(),
  focusedZoneID: null,
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
  mapRun: 0,
  textByMap: new Map(),
  restore: null,
  settling: false,
  styleCache: new Map(),
  markerIcons: new Map(),
};
