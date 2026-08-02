// The globe: a map that declares itself a sphere can be looked at as one.
// The renderer is globe.gl -- three.js with the community's globe work
// already done -- fed entirely from the bundle: the texture is composited
// from the map's own equirectangular tiles, which drape a sphere with no
// reprojection at all (u is x over width, v is y over height, exactly), and
// every pin's position comes from running its packed synthetic coordinates
// backward through the mapping the map declares. Pins stand off the surface
// as billboards wearing the same haloed category icons the chart draws, the
// selected one ringed and raised so it reads as chosen, and a click opens
// the same card it opens on the chart. The chart never goes away: this is a
// second way of seeing the same bundle, a toggle apart.
import Globe from "globe.gl";
import {
  BufferGeometry,
  Float32BufferAttribute,
  Group,
  Mesh,
  MeshBasicMaterial,
  Sprite,
  SpriteMaterial,
  SRGBColorSpace,
  TextureLoader,
} from "three";

import { Line2 } from "three/examples/jsm/lines/Line2.js";
import { LineGeometry } from "three/examples/jsm/lines/LineGeometry.js";
import { LineMaterial } from "three/examples/jsm/lines/LineMaterial.js";

import { isCollectionHidden } from "./collections.js";
import { gridTheme, overzoomLevels } from "./constants.js";
import { showPin } from "./detail.js";
import { elements } from "./dom.js";
import { activeSystem } from "./cellsystems/index.js";
import {
  gridCellPlan,
  gridCellVisual,
  pinInGridCell,
  selectGridCell,
} from "./grid.js";
import { equirectMapping, worldSurface } from "./semconv.js";
import { state } from "./state.js";
import { markerIconKey, measureLabel } from "./styles.js";
import { categoryColor, iconOutsetColor, initials } from "./theme.js";
import { clamp } from "./util.js";
import { project } from "./zones.js";

// textureZoom picks the pyramid level the sphere wears everywhere: deep
// enough to read from orbit, shallow enough that one canvas holds the whole
// planet (level 4 is a 4096x2048 texture). Closer than that, the detail
// layer below takes over with the pyramid's own tiles.
const textureZoom = 4;

// The sphere three-globe draws has radius 100, and its objects place a
// latitude and longitude with this exact spelling; the detail tiles use the
// same one, so they land on the pixel the base skin and the pins already
// agree on. Tiles float just off the skin, well under the pins.
const globeRadius = 100;
const detailRadius = globeRadius + 0.2;
const detailSegments = 8;
const detailTileBudget = 96;
// Screen-relative sprite sizes: with attenuation off, scale is a fraction
// of the viewport rather than a length on the sphere.
const spriteSize = 0.045;
const spriteSelectedSize = 0.08;

function surfacePoint(lat, lng, radius) {
  const phi = ((90 - lat) * Math.PI) / 180;
  const theta = ((90 - lng) * Math.PI) / 180;
  return [
    radius * Math.sin(phi) * Math.cos(theta),
    radius * Math.cos(phi),
    radius * Math.sin(phi) * Math.sin(theta),
  ];
}

let globe = null;
let texturedFor = "";
// The detail layer: pyramid tiles standing just off the base skin wherever
// the camera is close enough to want them, keyed by level/column/row.
const detail = { group: null, tiles: new Map(), key: "", lens: "" };
// The geohash grid drawn on the sphere: cell boundaries and their letters,
// rebuilt from the same plan the chart tiles its cells from. fitKey coarsens
// the camera so the grid rebuilds only when a zoom change could move the
// fit gate, not on every tick of a drag.
const grid = { group: null, cell: null, fitKey: "" };
// The names held up while Z is down: label sprites over the pins nearest
// the camera, rebuilt as the view settles and dropped on release.
const labels = { group: null, key: "" };
const labelBudget = 180;
// The pins the standing sprites were built from. The renderer creates
// sprites lazily on its own tick, so nothing here may assume they exist
// the moment the data is handed over -- each sprite is born already
// dressed for the current filters and selection, and the sweeps below only
// re-dress the ones that are standing.
let pinsBuilt = null;
// The sprites standing on the sphere right now, by pin, so a selection or
// filter change restyles them in place instead of rebuilding two thousand.
const sprites = new Map();
const placed = new Map();
const materials = new Map();

// syncGlobe is called whenever a map arrives or leaves: it puts the toggle
// up only for maps that declare a sphere with a mapping the viewer can
// invert, and always returns the reader to the chart, which is where every
// map opens.
export function syncGlobe() {
  const offered = worldSurface(state.world) === "sphere" && equirectMapping(state.world) !== null;
  elements.globeToggle.hidden = !offered;
  leaveGlobe();
  if (!offered && globe) {
    clearDetailTiles();
    detail.group = null;
    grid.group = null;
    grid.cell = null;
    labels.group = null;
    labels.key = "";
    globe._destructor();
    globe = null;
    texturedFor = "";
    pinsBuilt = null;
    elements.globe.replaceChildren();
  }
}

export function toggleGlobe() {
  if (state.globeActive) leaveGlobe();
  else void enterGlobe();
}

// The two cameras answer to one another through the declared mapping, so
// flipping panes keeps the reader's place: the point faced and roughly how
// close. The scale pairing is a rule of thumb -- the whole disc at altitude
// 2.5 reads like the whole chart at zoom 2, and each halving of altitude
// reads like one more zoom -- which is all "the same spot" needs.
const wholeDiscAltitude = 2.5;
const wholeChartZoom = 2;

// The camera keeps a respectful distance: never through the skin -- a
// camera inside the sphere sees the world inside out and its altitude goes
// negative, which once poisoned every conversion downstream -- and never so
// far the planet is a dot.
const nearestAltitude = 0.08;
const farthestAltitude = 4;

function altitudeForZoom(zoom) {
  const safe = Number.isFinite(zoom) ? zoom : wholeChartZoom;
  return clamp(wholeDiscAltitude / 2 ** (safe - wholeChartZoom), nearestAltitude, farthestAltitude);
}

function zoomForAltitude(altitude) {
  const safe = Number.isFinite(altitude) ? Math.max(altitude, nearestAltitude / 2) : wholeDiscAltitude;
  const ceiling = (state.lens?.maxZoom ?? wholeChartZoom) + overzoomLevels;
  return clamp(wholeChartZoom + Math.log2(wholeDiscAltitude / safe), 0, ceiling);
}

// changeGlobeZoom is the zoom buttons' reading on the sphere: one press is
// one halving or doubling of the distance, inside the same bounds the
// wheel is held to.
export function changeGlobeZoom(delta) {
  if (!globe || !state.globeActive) return;
  const pov = globe.pointOfView();
  const altitude = clamp(
    (Number.isFinite(pov.altitude) ? pov.altitude : wholeDiscAltitude) / 2 ** delta,
    nearestAltitude,
    farthestAltitude,
  );
  globe.pointOfView({ altitude }, 180);
}

function leaveGlobe() {
  // Whatever the globe was facing, the chart opens on: the same place at a
  // comparable closeness, so the flip reads as turning a page, not losing
  // one. A map change lands here too, but its mapping no longer answers
  // and the new map's view is left alone.
  const mapping = equirectMapping(state.world);
  const view = state.engine?.getView();
  if (globe && state.globeActive && mapping && view) {
    const pov = globe.pointOfView();
    const [worldX, worldY] = mapping.toWorld(pov.lat, pov.lng);
    // Nothing unfinite may reach the chart's view: a broken number here
    // once blacked out both panes with no way back.
    if (Number.isFinite(worldX) && Number.isFinite(worldY)) {
      view.cancelAnimations();
      view.setCenter([worldX, -worldY]);
      view.setZoom(zoomForAltitude(pov.altitude));
    }
  }
  state.globeActive = false;
  elements.globe.hidden = true;
  elements.globeToggle.setAttribute("aria-pressed", "false");
  elements.viewport.hidden = false;
  // The neighborhood of tiles under the camera goes back to the pyramid;
  // the base skin is all a put-away globe keeps.
  clearDetailTiles();
  // The overview goes back to marking the chart's window; the memo that
  // spares it redundant work must not spare it this change.
  state.overviewKey = "";
  document.dispatchEvent(new Event("atlas:globe-camera"));
}

// globeCamera says where the globe is looking, for the overview's reticle;
// nowhere, when the reader is on the chart.
export function globeCamera() {
  if (!globe || !state.globeActive) return null;
  const pov = globe.pointOfView();
  return { lat: pov.lat, lng: pov.lng };
}

// aimGlobe turns the globe to face a place, keeping the reader's distance.
export function aimGlobe(lat, lng, ease) {
  if (!globe) return;
  const altitude = globe.pointOfView().altitude;
  globe.pointOfView({ lat, lng, altitude }, ease ? 180 : 0);
}

async function enterGlobe() {
  const mapping = equirectMapping(state.world);
  if (!mapping) return;
  state.globeActive = true;
  elements.viewport.hidden = true;
  elements.globe.hidden = false;
  elements.globeToggle.setAttribute("aria-pressed", "true");
  state.overviewKey = "";

  if (!globe) {
    globe = Globe({ animateIn: false })(elements.globe)
      .backgroundColor("#0b0e12")
      .showAtmosphere(true)
      .atmosphereColor("#8a6f5b")
      .atmosphereAltitude(0.12)
      .objectLat((point) => point.lat)
      .objectLng((point) => point.lng)
      .objectAltitude((point) => (point.pin === state.selectedPin ? 0.03 : 0.014))
      .objectThreeObject((point) => spriteFor(point.pin))
      .objectLabel((point) => point.pin.location.title)
      .onObjectClick((point) => showPin(point.pin))
      .onObjectHover((point) => {
        elements.globe.style.cursor = point ? "pointer" : "";
      })
      .onZoom(() => {
        document.dispatchEvent(new Event("atlas:globe-camera"));
        updateDetailTiles();
        rebuildGlobeLabels();
        regridWhenFitChanges();
        cullHiddenLabels();
      });
    document.addEventListener("atlas:selection", syncSelection);
    document.addEventListener("atlas:filters", syncFilters);
    // A grid change moves both the boundaries and which pins are held to
    // the chosen cell -- and which names Z is holding up.
    document.addEventListener("atlas:grid", () => {
      rebuildGlobeGrid();
      syncFilters();
      rebuildGlobeLabels();
    });
    document.addEventListener("atlas:labels", rebuildGlobeLabels);
    // A cell chosen on the sphere is chosen the way the chart chooses it:
    // the point pressed names its next-deeper cell through the same
    // reverse-halving the navigator types.
    globe.onGlobeClick(({ lat, lng }) => {
      if (!state.gridEnabled) return;
      const held = equirectMapping(state.world);
      if (!held) return;
      const [worldX, worldY] = held.toWorld(lat, lng);
      const target = activeSystem().descendTarget(state.gridCell, [worldX, -worldY]);
      if (target) selectGridCell(target);
    });
    // The camera stays outside the skin: closer than the nearest altitude
    // the world turns inside out, and the numbers it produces poison every
    // pane they touch.
    const controls = globe.controls();
    controls.minDistance = globeRadius * (1 + nearestAltitude);
    controls.maxDistance = globeRadius * (1 + farthestAltitude);
    detail.group = new Group();
    globe.scene().add(detail.group);
    grid.group = new Group();
    globe.scene().add(grid.group);
    labels.group = new Group();
    globe.scene().add(labels.group);
    // A window on the globe's working parts for the parity harness and a
    // plain browser: counts, never control.
    window.__atlasGlobe = { detail, grid, labels, sprites };
  }
  resizeGlobe();

  // The globe opens facing what the chart was showing, as close as the
  // chart was: the flip keeps the reader's place in both directions.
  const view = state.engine?.getView();
  const center = view?.getCenter();
  if (center && Number.isFinite(center[0]) && Number.isFinite(center[1])) {
    const [lat, lng] = mapping.toLatLng(center[0], -center[1]);
    globe.pointOfView({ lat, lng, altitude: altitudeForZoom(view.getZoom()) });
  } else {
    globe.pointOfView({ lat: 10, lng: 0, altitude: wholeDiscAltitude });
  }

  const lens = state.lens || state.world.lenses[0];
  const key = `${state.volume.stamp}:${state.world.slug}:${lens.tiles}`;
  if (texturedFor !== key) {
    texturedFor = key;
    const texture = await composeTexture(lens);
    if (texture && texturedFor === key) {
      globe.globeImageUrl(texture);
    }
  }
  if (pinsBuilt !== state.pins) {
    pinsBuilt = state.pins;
    sprites.clear();
    placed.clear();
    globe.objectsData(spherePins(mapping));
  }
  syncFilters();
  restyleSelection();
  updateDetailTiles();
  rebuildGlobeGrid();
  rebuildGlobeLabels();
  document.dispatchEvent(new Event("atlas:globe-camera"));
}

// rebuildGlobeGrid redraws the geohash cells on the sphere as the globe's
// adapter over gridCellVisual: the same tokens the chart styles from become
// fat-line boundaries, translucent fill quads, and corner chip sprites, so
// the two projections read as one instrument. Descending into a cell also
// turns the globe to frame it, the way the chart fits its view.
function rebuildGlobeGrid() {
  if (!globe || !grid.group) return;
  for (const child of [...grid.group.children]) {
    grid.group.remove(child);
    child.geometry?.dispose();
    child.material?.map?.dispose();
    child.material?.dispose();
  }
  const mapping = state.globeActive ? equirectMapping(state.world) : null;
  if (!mapping || !state.gridEnabled) {
    grid.cell = null;
    return;
  }
  const system = activeSystem();
  for (const cell of gridCellPlan()) {
    const ringLL = ringLatLng(cell.ring, mapping);
    const corners = ringBounds(ringLL);
    const visual = gridCellVisual(cell, {
      subgridVisible: state.subgridVisible,
      labelled: gridLabelFits(cell, corners),
    });
    if (!visual) continue;
    if (visual.fill) {
      const centerLL = pointLatLng(system.center(cell.hash), mapping);
      grid.group.add(ringFill(ringLL, centerLL, visual.fill));
    }
    grid.group.add(cellBoundary(ringLL, visual.line));
    if (visual.label) grid.group.add(cellChip(visual.label, corners));
  }
  if (grid.cell !== null && grid.cell !== state.gridCell) {
    frameGridCell(mapping);
  }
  grid.cell = state.gridCell;
  // Chips are born visible; a rebuild with the camera already elsewhere
  // must not leave far-side chips shining through the planet.
  cullHiddenLabels();
}

// ringLatLng lands a system's world-pixel ring on the sphere. The ring is
// continuous by contract, so a loop that crossed the antimeridian simply
// carries longitudes past 180 -- the trigonometry underneath is periodic
// and drapes it where it belongs, which is why the globe never needs the
// splitting the chart does.
function ringLatLng(ring, mapping) {
  return ring.map(([x, y]) => mapping.toLatLng(x, -y));
}

function pointLatLng(point, mapping) {
  return mapping.toLatLng(point[0], -point[1]);
}

// densifyRing subdivides each ring segment by its span, returning the open
// loop the fill fans from.
function densifyRing(ringLL) {
  const open = [];
  for (let at = 0; at < ringLL.length - 1; at++) {
    const [fromLat, fromLng] = ringLL[at];
    const [toLat, toLng] = ringLL[at + 1];
    const span = Math.max(Math.abs(toLat - fromLat), Math.abs(toLng - fromLng));
    const steps = clamp(Math.ceil(span / 2), 1, 48);
    for (let step = 0; step < steps; step++) {
      const t = step / steps;
      open.push([fromLat + (toLat - fromLat) * t, fromLng + (toLng - fromLng) * t]);
    }
  }
  return open;
}

// ringBounds is the cell's frame in degrees, for the fit gate, the chip
// anchor, and the camera.
function ringBounds(ringLL) {
  let north = -Infinity;
  let south = Infinity;
  let west = Infinity;
  let east = -Infinity;
  for (const [lat, lng] of ringLL) {
    north = Math.max(north, lat);
    south = Math.min(south, lat);
    west = Math.min(west, lng);
    east = Math.max(east, lng);
  }
  return { north, south, west, east };
}

// cellScreenPx estimates how many pixels a cell spans on screen: degrees of
// ground per pixel fall out of the camera's distance and field of view. It
// is the globe's answer to the chart's extent-over-resolution, feeding the
// same fit gate.
function cellScreenPx(corners) {
  const pov = globe.pointOfView();
  const altitude = Math.max(pov.altitude, nearestAltitude / 2);
  const fovRadians = (50 * Math.PI) / 180;
  const groundPerPx =
    (2 * altitude * globeRadius * Math.tan(fovRadians / 2)) /
    Math.max(1, elements.globe.clientHeight || 1);
  const degreePx = ((globeRadius * Math.PI) / 180) / groundPerPx;
  return {
    width: Math.abs(corners.east - corners.west) * degreePx,
    height: Math.abs(corners.north - corners.south) * degreePx,
  };
}

// gridLabelFits is labelFitsCell spoken in the globe's terms: the chip --
// measured in the same fixed pitch the chart measures -- must fit inside
// the cell's projected footprint.
function gridLabelFits(cell, corners) {
  const neighbor = cell.role === "neighbor";
  const size = neighbor ? gridTheme.neighborLabelSizePx : gridTheme.labelSizePx;
  const font = `900 ${size}px ${gridTheme.labelFont}`;
  const px = cellScreenPx(corners);
  return measureLabel(cell.hash, font) + 9 <= px.width && size + 6 <= px.height;
}

// ringFill lays a cell's tint or dim on the ground: a fan of triangles
// from the cell's own centre out to its ring, each spoke subdivided so the
// sheet follows the curve instead of sagging under it, just off the detail
// tiles and under the boundary lines. The spokes are walked on the sphere
// itself -- chord-lerp between the centre's vector and each ring vector,
// re-projected to the surface -- never in lat/lng, where an unwrapped ring
// and a wrapped centre sit hundreds of degrees apart and a pole cell's
// centre has no honest longitude at all; either smears the sheet around
// the planet.
function ringFill(ringLL, centerLL, fill) {
  const radius = detailRadius + 0.12;
  // The fan needs spokes as dense as the boundary's steps: with only the
  // ring's own corners, the sheet between spokes sags below the ground and
  // the fill opens into almond-shaped gaps.
  const open = densifyRing(ringLL);
  const bounds = ringBounds(ringLL);
  const span = Math.max(bounds.east - bounds.west, bounds.north - bounds.south);
  const rows = clamp(Math.ceil(span / 6), 2, 24);
  const center = surfacePoint(centerLL[0], centerLL[1], radius);
  const edges = open.map(([lat, lng]) => surfacePoint(lat, lng, radius));
  const positions = [...center];
  const indices = [];
  for (let row = 1; row <= rows; row++) {
    const t = row / rows;
    for (const edge of edges) {
      const x = center[0] + (edge[0] - center[0]) * t;
      const y = center[1] + (edge[1] - center[1]) * t;
      const z = center[2] + (edge[2] - center[2]) * t;
      const lift = radius / (Math.hypot(x, y, z) || 1);
      positions.push(x * lift, y * lift, z * lift);
    }
  }
  const count = open.length;
  const at = (row, index) => 1 + (row - 1) * count + (index % count);
  for (let index = 0; index < count; index++) {
    indices.push(0, at(1, index), at(1, index + 1));
  }
  for (let row = 1; row < rows; row++) {
    for (let index = 0; index < count; index++) {
      indices.push(at(row, index), at(row + 1, index), at(row, index + 1));
      indices.push(at(row, index + 1), at(row + 1, index), at(row + 1, index + 1));
    }
  }
  const geometry = new BufferGeometry();
  geometry.setAttribute("position", new Float32BufferAttribute(positions, 3));
  geometry.setIndex(indices);
  const material = new MeshBasicMaterial({
    color: fill.color,
    transparent: true,
    opacity: fill.opacity,
    depthWrite: false,
    side: 2,
  });
  const mesh = new Mesh(geometry, material);
  mesh.renderOrder = 1;
  return mesh;
}

// rebuildGlobeLabels raises names over the pins while Z is held: the ones
// nearest what the camera faces, within a budget -- a planet of two
// thousand names at once is noise, and the chart's own flood is bounded by
// its window the same way. Rebuilt as the camera settles, dropped on
// release.
function rebuildGlobeLabels() {
  if (!globe || !labels.group) return;
  const wanted = state.globeActive && state.labelsHeld;
  const pov = wanted ? globe.pointOfView() : null;
  const key = wanted
    ? `${Math.round(pov.lat)}:${Math.round(pov.lng)}:${pov.altitude.toFixed(2)}:${state.gridSystem}:${state.gridCell}`
    : "";
  if (key === labels.key) return;
  labels.key = key;
  for (const child of [...labels.group.children]) {
    labels.group.remove(child);
    child.material?.map?.dispose();
    child.material?.dispose();
  }
  if (!wanted) return;

  const nearby = [];
  for (const [pin, sprite] of sprites) {
    if (!sprite.visible) continue;
    const stood = placed.get(pin);
    if (!stood) continue;
    const distance = angularDistance(pov, stood);
    if (distance > 85) continue;
    nearby.push({ pin, stood, distance });
  }
  nearby.sort((a, b) => a.distance - b.distance);
  for (const { pin, stood } of nearby.slice(0, labelBudget)) {
    labels.group.add(labelSprite(pin, stood));
  }
  cullHiddenLabels();
}

// cullHiddenLabels enforces the horizon that the label sprites' materials
// no longer test for: a card whose anchor has slipped past the planet's
// silhouette goes invisible instead of shining through it. A point at the
// limb sits where the cosine of its angle from the camera's axis equals
// radius over distance; anything beyond that is the far side.
function cullHiddenLabels() {
  if (!globe) return;
  const camera = globe.camera().position;
  const distance = camera.length() || 1;
  const horizon = globeRadius / distance;
  for (const group of [grid.group, labels.group]) {
    if (!group) continue;
    for (const child of group.children) {
      if (!child.isSprite) continue;
      const anchor = child.position;
      const reach = anchor.length() || 1;
      const facing =
        (anchor.x * camera.x + anchor.y * camera.y + anchor.z * camera.z) /
        (reach * distance);
      child.visible = facing > horizon;
    }
  }
}

// angularDistance is the great-circle separation in degrees, which is what
// "near what the camera faces" means on a sphere.
function angularDistance(a, b) {
  const rad = Math.PI / 180;
  const inner =
    Math.sin(a.lat * rad) * Math.sin(b.lat * rad) +
    Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.cos((a.lng - b.lng) * rad);
  return (Math.acos(clamp(inner, -1, 1)) * 180) / Math.PI;
}

// labelSprite writes one pin's name on a small card floated above its
// marker, screen-sized like the marker itself.
function labelSprite(pin, stood) {
  const title = pin.location.title;
  const canvas = document.createElement("canvas");
  const context = canvas.getContext("2d");
  const font = "600 26px Inter, system-ui, sans-serif";
  context.font = font;
  const width = Math.ceil(context.measureText(title).width) + 24;
  canvas.width = width;
  canvas.height = 40;
  context.font = font;
  context.textAlign = "center";
  context.textBaseline = "middle";
  context.fillStyle = "rgba(10, 13, 17, 0.78)";
  context.fillRect(0, 0, width, 40);
  context.fillStyle = "#e6ebf0";
  context.fillText(title, width / 2, 21);
  const material = new SpriteMaterial({
    // No depth test: a screen-sized card anchored on the ground loses its
    // lower half to the planet's own curve at any glancing angle. The
    // horizon is enforced by hand in cullHiddenLabels instead.
    depthTest: false,
    depthWrite: false,
    sizeAttenuation: false,
    transparent: true,
  });
  new TextureLoader().load(canvas.toDataURL("image/png"), (texture) => {
    material.map = texture;
    material.needsUpdate = true;
  });
  const sprite = new Sprite(material);
  sprite.position.set(...surfacePoint(stood.lat, stood.lng, detailRadius + 0.4));
  sprite.renderOrder = 4;
  const height = 0.028;
  sprite.scale.set((height * width) / 40, height, 1);
  // Anchored at its bottom edge -- the far end of center's 0..1 contract --
  // the card floats above the marker instead of covering it.
  sprite.center.set(0.5, 0);
  return sprite;
}

// cellBoundary drapes a cell's frame at its token weight. WebGL's plain
// lines are one pixel whatever they ask for, so the frame is a fat line --
// Line2 -- whose width is spoken in the same pixels the chart strokes.
// The ring arrives pre-tessellated by its system; each segment is further
// subdivided by its own span so long geohash edges still follow the curve.
function cellBoundary(ringLL, line) {
  const positions = [];
  for (let at = 0; at < ringLL.length - 1; at++) {
    const [fromLat, fromLng] = ringLL[at];
    const [toLat, toLng] = ringLL[at + 1];
    const span = Math.max(Math.abs(toLat - fromLat), Math.abs(toLng - fromLng));
    const steps = clamp(Math.ceil(span / 2), 4, 48);
    for (let step = 0; step < steps; step++) {
      const t = step / steps;
      positions.push(...surfacePoint(
        fromLat + (toLat - fromLat) * t,
        fromLng + (toLng - fromLng) * t,
        detailRadius + 0.25,
      ));
    }
  }
  positions.push(...positions.slice(0, 3));
  const geometry = new LineGeometry();
  geometry.setPositions(positions);
  const material = new LineMaterial({
    color: line.color,
    transparent: true,
    opacity: line.opacity,
    linewidth: line.widthPx,
    depthWrite: false,
  });
  material.resolution.set(elements.globe.clientWidth || 1, elements.globe.clientHeight || 1);
  const loop = new Line2(geometry, material);
  loop.computeLineDistances();
  loop.renderOrder = 2;
  return loop;
}

// cellChip writes a cell's address on a small card in its bottom-right
// corner -- the bounding-box convention -- with the prefix faint and the
// final character bright, exactly as the chart writes it.
function cellChip(label, corners) {
  const scaleUp = 2;
  const size = label.sizePx * scaleUp;
  // One size for both cuts: weight and alpha carry the faintness, and the
  // baseline holds, because mixed sizes on one line wobble the address.
  const prefixFont = `500 ${size}px ${gridTheme.labelFont}`;
  const finalFont = `900 ${size}px ${gridTheme.labelFont}`;
  const canvas = document.createElement("canvas");
  const context = canvas.getContext("2d");
  context.font = prefixFont;
  const prefixWidth = label.prefix ? context.measureText(label.prefix).width : 0;
  context.font = finalFont;
  const finalWidth = context.measureText(label.final).width;
  const pad = 5 * scaleUp;
  canvas.width = Math.ceil(prefixWidth + finalWidth + pad * 2);
  canvas.height = size + pad * 1.4;
  context.fillStyle = label.chip;
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.textBaseline = "middle";
  const middle = canvas.height / 2 + scaleUp;
  if (label.prefix) {
    context.font = prefixFont;
    context.fillStyle = label.color;
    context.globalAlpha = gridTheme.prefixAlpha * label.textAlpha;
    context.fillText(label.prefix, pad, middle);
  }
  context.font = finalFont;
  context.globalAlpha = label.textAlpha;
  context.fillStyle = label.color;
  context.fillText(label.final, pad + prefixWidth, middle);
  context.globalAlpha = 1;

  const material = new SpriteMaterial({
    // Same bargain as the name cards: no depth test, horizon by hand.
    depthTest: false,
    depthWrite: false,
    transparent: true,
    sizeAttenuation: false,
  });
  new TextureLoader().load(canvas.toDataURL("image/png"), (texture) => {
    material.map = texture;
    material.needsUpdate = true;
  });
  const sprite = new Sprite(material);
  // Just inside the corner, anchored by its own bottom-right, fixed on
  // screen like the chart's chips.
  const insetLat = Math.abs(corners.north - corners.south) * 0.02;
  const insetLng = Math.abs(corners.east - corners.west) * 0.02;
  sprite.position.set(...surfacePoint(
    corners.south + insetLat,
    corners.east - insetLng,
    detailRadius + 0.35,
  ));
  const viewport = Math.max(1, elements.globe.clientHeight || 1);
  const heightScale = canvas.height / scaleUp / viewport;
  sprite.scale.set((heightScale * canvas.width) / canvas.height, heightScale, 1);
  sprite.center.set(1, 0);
  sprite.renderOrder = 3;
  return sprite;
}

// frameGridCell turns the globe to hold the chosen cell, the way the chart
// fits its view when the navigator descends.
function frameGridCell(mapping) {
  const plan = gridCellPlan();
  const chosen = plan.find((cell) => cell.role === "scope" || cell.role === "leaf");
  if (!chosen) return;
  // The camera aims where the system says the cell's middle is -- a pole
  // cap's middle is the pole, which no frame corner would have named.
  const [lat, lng] = pointLatLng(activeSystem().center(chosen.hash), mapping);
  const bounds = ringBounds(ringLatLng(chosen.ring, mapping));
  const span = Math.max(bounds.east - bounds.west, bounds.north - bounds.south);
  const altitude = clamp(span / 45, nearestAltitude, farthestAltitude);
  globe.pointOfView({ lat, lng, altitude }, 400);
}

// updateDetailTiles keeps the pyramid under the camera: past the base
// skin's depth, the level the altitude asks for is fetched tile by tile
// around the point being faced -- the same tiles the chart reads, each one
// a perfect square of latitude and longitude draped at its place. Away
// from the camera the base skin carries on; this layer only ever holds the
// neighborhood being looked at.
function updateDetailTiles() {
  if (!globe || !detail.group) return;
  const lens = state.globeActive ? state.lens || state.world.lenses[0] : null;
  if (!lens) {
    clearDetailTiles();
    return;
  }
  if (detail.lens !== lens.tiles) {
    clearDetailTiles();
    detail.lens = lens.tiles;
  }
  const pov = globe.pointOfView();
  const zoom = Math.min(
    Math.round(zoomForAltitude(pov.altitude)) + 1,
    lens.maxZoom,
  );
  if (zoom <= textureZoom || !lens.formats[zoom]) {
    clearDetailTiles();
    detail.key = "";
    return;
  }

  const columns = 2 ** zoom;
  const rows = columns / 2;
  const span = 360 / columns;
  // How much ground the camera can possibly see: the horizon angle at this
  // altitude, padded a little so tiles arrive before their ground does.
  const horizon = (Math.acos(1 / (1 + pov.altitude)) * 180) / Math.PI + span;
  const centerColumn = Math.floor((((pov.lng + 180) % 360) + 360) % 360 / span);
  const centerRow = Math.floor((90 - pov.lat) / span);
  let reach = Math.ceil(horizon / span);
  while ((2 * reach + 1) ** 2 > detailTileBudget && reach > 1) reach--;

  const wanted = new Set();
  for (let dr = -reach; dr <= reach; dr++) {
    const row = centerRow + dr;
    if (row < 0 || row >= rows) continue;
    for (let dc = -reach; dc <= reach; dc++) {
      const column = (((centerColumn + dc) % columns) + columns) % columns;
      wanted.add(`${zoom}/${column}/${row}`);
    }
  }
  const key = `${lens.tiles}:${[...wanted].sort().join(",")}`;
  if (key === detail.key) return;
  detail.key = key;

  for (const [name, mesh] of detail.tiles) {
    if (wanted.has(name)) continue;
    detail.group.remove(mesh);
    disposeTile(mesh);
    detail.tiles.delete(name);
  }
  for (const name of wanted) {
    if (detail.tiles.has(name)) continue;
    const [z, column, row] = name.split("/").map(Number);
    const mesh = tileMesh(lens, z, column, row);
    detail.tiles.set(name, mesh);
    detail.group.add(mesh);
  }
}

// tileMesh drapes one pyramid tile at its place on the sphere: a grid of
// points through the same latitude-longitude spelling the pins stand by,
// wearing the tile image once it arrives.
function tileMesh(lens, zoom, column, row) {
  const span = 360 / 2 ** zoom;
  const west = -180 + column * span;
  const north = 90 - row * span;
  const positions = [];
  const uvs = [];
  const indices = [];
  for (let i = 0; i <= detailSegments; i++) {
    const lat = north - (i / detailSegments) * span;
    for (let j = 0; j <= detailSegments; j++) {
      const lng = west + (j / detailSegments) * span;
      positions.push(...surfacePoint(lat, lng, detailRadius));
      uvs.push(j / detailSegments, 1 - i / detailSegments);
    }
  }
  const stride = detailSegments + 1;
  for (let i = 0; i < detailSegments; i++) {
    for (let j = 0; j < detailSegments; j++) {
      const corner = i * stride + j;
      indices.push(corner, corner + stride, corner + 1);
      indices.push(corner + 1, corner + stride, corner + stride + 1);
    }
  }
  const geometry = new BufferGeometry();
  geometry.setAttribute("position", new Float32BufferAttribute(positions, 3));
  geometry.setAttribute("uv", new Float32BufferAttribute(uvs, 2));
  geometry.setIndex(indices);

  const material = new MeshBasicMaterial();
  const mesh = new Mesh(geometry, material);
  // Invisible until its picture arrives: a black square teaches nothing.
  mesh.visible = false;
  const url = `${state.volume.base}/tiles/${lens.tiles}/${zoom}/${column}/${row}.${lens.formats[zoom]}`;
  new TextureLoader().load(url, (texture) => {
    texture.colorSpace = SRGBColorSpace;
    material.map = texture;
    material.needsUpdate = true;
    mesh.visible = true;
  });
  return mesh;
}

function clearDetailTiles() {
  if (!detail.group) return;
  for (const mesh of detail.tiles.values()) {
    detail.group.remove(mesh);
    disposeTile(mesh);
  }
  detail.tiles.clear();
  detail.key = "";
}

function disposeTile(mesh) {
  mesh.geometry.dispose();
  mesh.material.map?.dispose();
  mesh.material.dispose();
}

// restyleSelection dresses the standing sprites for the current selection:
// the chosen pin grows, takes its ring, lifts a little higher off the
// ground; the one it replaced settles back among the rest.
function restyleSelection() {
  if (!globe) return;
  for (const [pin, sprite] of sprites) {
    const selected = pin === state.selectedPin;
    sprite.material = material(pin.category, selected);
    const size = selected ? spriteSelectedSize : spriteSize;
    sprite.scale.set(size, size, 1);
  }
  // Altitude rides the accessor, so re-declaring it reseats the two that
  // moved along with everyone else -- cheap at this scale.
  globe.objectAltitude((point) => (point.pin === state.selectedPin ? 0.03 : 0.014));
}

// syncSelection answers a selection made while the globe is up: restyle,
// and turn to face the chosen pin, because a ring on the far side of a
// planet selects nothing anyone can see. Entering the globe restyles
// without turning -- the camera there belongs to the chart's view.
function syncSelection() {
  if (!globe || !state.globeActive) return;
  restyleSelection();
  const stood = placed.get(state.selectedPin);
  if (stood) {
    const altitude = globe.pointOfView().altitude;
    globe.pointOfView({ lat: stood.lat, lng: stood.lng, altitude }, 600);
  }
}

// pinShown is the one visibility rule the sphere holds a pin to: the
// legend's and search's filters, and the chosen geohash cell, exactly as
// the chart holds them.
function pinShown(pin) {
  if (pin.filteredHidden || isCollectionHidden(pin.category.id)) return false;
  if (state.gridEnabled && state.gridCell && !pinInGridCell(pin)) return false;
  return true;
}

// syncFilters shows and hides sprites as the legend and the search decide,
// the same visibility the chart draws from.
function syncFilters() {
  if (!globe || !state.globeActive) return;
  for (const [pin, sprite] of sprites) {
    sprite.visible = pinShown(pin);
  }
}

// spriteFor stands one pin up as a billboard wearing its category's icon --
// the same haloed, tinted raster the chart composes -- so what a marker is
// reads the same on the sphere as on the sheet.
function spriteFor(pin) {
  const sprite = new Sprite(material(pin.category, pin === state.selectedPin));
  const size = pin === state.selectedPin ? spriteSelectedSize : spriteSize;
  sprite.scale.set(size, size, 1);
  // Born dressed: the renderer creates sprites on its own tick, after any
  // sweep that ran at hand-over, so the filters must already be worn.
  sprite.visible = pinShown(pin);
  sprites.set(pin, sprite);
  return sprite;
}

// material caches one sprite material per category and selection state. The
// icon raster is the chart's own when it has arrived; a category still
// waiting, or one with no artwork at all, wears its initials the same way
// the chart's fallback draws them.
function material(category, selected) {
  const key = `${markerIconKey(category)}:${selected ? "ringed" : "plain"}`;
  let held = materials.get(key);
  if (held) return held;
  // Pins keep one size on screen however close the camera is, the way the
  // chart draws its markers -- world-sized sprites become dinner plates at
  // low altitude.
  held = new SpriteMaterial({ depthWrite: false, sizeAttenuation: false });
  materials.set(key, held);
  const icon = state.markerIcons.get(markerIconKey(category));
  const finish = (image) => {
    const canvas = document.createElement("canvas");
    canvas.width = 80;
    canvas.height = 80;
    const context = canvas.getContext("2d");
    if (image) {
      context.drawImage(image, 8, 8, 64, 64);
    } else {
      context.font = "900 26px Inter, system-ui, sans-serif";
      context.textAlign = "center";
      context.textBaseline = "middle";
      context.lineWidth = 6;
      context.strokeStyle = iconOutsetColor();
      context.strokeText(initials(category.title), 40, 41);
      context.fillStyle = categoryColor(category);
      context.fillText(initials(category.title), 40, 41);
    }
    if (selected) {
      context.beginPath();
      context.arc(40, 40, 36, 0, Math.PI * 2);
      context.lineWidth = 5;
      context.strokeStyle = "#ffffff";
      context.stroke();
    }
    new TextureLoader().load(canvas.toDataURL("image/png"), (texture) => {
      held.map = texture;
      held.needsUpdate = true;
    });
  };
  if (icon) {
    const image = new Image();
    image.onload = () => finish(image);
    image.onerror = () => finish(null);
    image.src = icon;
  } else {
    finish(null);
  }
  return held;
}

// refreshGlobe re-enters the globe when what it shows has moved underneath
// it: another raster lens chosen, a category switched in the legend.
export function refreshGlobe() {
  if (state.globeActive) void enterGlobe();
}

// resizeGlobe keeps the canvas the size of its pane; globe.gl sizes once at
// construction and must be told when the pane moves. The grid follows: its
// line widths and chips are spoken in pixels of this very pane.
export function resizeGlobe() {
  if (!globe || elements.globe.hidden) return;
  globe.width(elements.globe.clientWidth).height(elements.globe.clientHeight);
  rebuildGlobeGrid();
}

// regridWhenFitChanges redraws the grid only when the camera has moved far
// enough in depth that a label could newly fit or stop fitting.
function regridWhenFitChanges() {
  if (!globe || !state.globeActive || !state.gridEnabled) return;
  const pov = globe.pointOfView();
  const key = `${state.gridSystem}:${state.gridCell}:${Math.round(zoomForAltitude(pov.altitude) * 2)}`;
  if (key === grid.fitKey) return;
  grid.fitKey = key;
  rebuildGlobeGrid();
}

// spherePins stands every visible pin on the planet: packed synthetic
// coordinates, through the viewer's own projection to world pixels, then
// backward through the declared mapping to true latitude and longitude.
function spherePins(mapping) {
  const points = [];
  for (const pin of state.pins) {
    const [x, negY] = project(pin.location.lat, pin.location.lng);
    const [lat, lng] = mapping.toLatLng(x, -negY);
    points.push({ lat, lng, pin });
    placed.set(pin, { lat, lng });
  }
  return points;
}

// composeTexture stitches the sphere's skin from the bundle's own tiles at
// one fixed level: the full width of the pyramid and the top half of its
// rows, which is the whole planet by the declared mapping. The result rides
// as a data URL, so the globe asks the app for nothing it has not already
// been given.
async function composeTexture(lens) {
  const zoom = Math.min(textureZoom, lens.maxZoom);
  const format = lens.formats[zoom];
  if (!format) return null;
  const columns = 2 ** zoom;
  const rows = columns / 2;
  const canvas = document.createElement("canvas");
  canvas.width = columns * 256;
  canvas.height = rows * 256;
  const context = canvas.getContext("2d");
  const jobs = [];
  for (let y = 0; y < rows; y++) {
    for (let x = 0; x < columns; x++) {
      const url = `${state.volume.base}/tiles/${lens.tiles}/${zoom}/${x}/${y}.${format}`;
      jobs.push(loadTile(url).then((image) => {
        if (image) context.drawImage(image, x * 256, y * 256, 256, 256);
      }));
    }
  }
  await Promise.all(jobs);
  return canvas.toDataURL("image/jpeg", 0.92);
}

function loadTile(url) {
  return new Promise((resolve) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => resolve(null);
    image.src = url;
  });
}
