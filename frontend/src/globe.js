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

import { overzoomLevels } from "./constants.js";
import { showPin } from "./detail.js";
import { elements } from "./dom.js";
import { equirectMapping, mapSurface } from "./semconv.js";
import { state } from "./state.js";
import { markerIconKey } from "./styles.js";
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
const detail = { group: null, tiles: new Map(), key: "", variant: "" };
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
  const offered = mapSurface(state.map) === "sphere" && equirectMapping(state.map) !== null;
  elements.globeToggle.hidden = !offered;
  leaveGlobe();
  if (!offered && globe) {
    clearDetailTiles();
    detail.group = null;
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

function altitudeForZoom(zoom) {
  return clamp(wholeDiscAltitude / 2 ** (zoom - wholeChartZoom), 0.08, 4);
}

function zoomForAltitude(altitude) {
  const ceiling = (state.variant?.maxZoom ?? wholeChartZoom) + overzoomLevels;
  return clamp(wholeChartZoom + Math.log2(wholeDiscAltitude / altitude), 0, ceiling);
}

function leaveGlobe() {
  // Whatever the globe was facing, the chart opens on: the same place at a
  // comparable closeness, so the flip reads as turning a page, not losing
  // one. A map change lands here too, but its mapping no longer answers
  // and the new map's view is left alone.
  const mapping = equirectMapping(state.map);
  const view = state.engine?.getView();
  if (globe && state.globeActive && mapping && view) {
    const pov = globe.pointOfView();
    const [worldX, worldY] = mapping.toWorld(pov.lat, pov.lng);
    view.cancelAnimations();
    view.setCenter([worldX, -worldY]);
    view.setZoom(zoomForAltitude(pov.altitude));
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
  const mapping = equirectMapping(state.map);
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
      });
    document.addEventListener("atlas:selection", syncSelection);
    document.addEventListener("atlas:filters", syncFilters);
    detail.group = new Group();
    globe.scene().add(detail.group);
  }
  resizeGlobe();

  // The globe opens facing what the chart was showing, as close as the
  // chart was: the flip keeps the reader's place in both directions.
  const view = state.engine?.getView();
  const center = view?.getCenter();
  if (center) {
    const [lat, lng] = mapping.toLatLng(center[0], -center[1]);
    globe.pointOfView({ lat, lng, altitude: altitudeForZoom(view.getZoom() ?? wholeChartZoom) });
  }

  const variant = state.variant || state.map.variants[0];
  const key = `${state.game.stamp}:${state.map.slug}:${variant.tiles}`;
  if (texturedFor !== key) {
    texturedFor = key;
    const texture = await composeTexture(variant);
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
  document.dispatchEvent(new Event("atlas:globe-camera"));
}

// updateDetailTiles keeps the pyramid under the camera: past the base
// skin's depth, the level the altitude asks for is fetched tile by tile
// around the point being faced -- the same tiles the chart reads, each one
// a perfect square of latitude and longitude draped at its place. Away
// from the camera the base skin carries on; this layer only ever holds the
// neighborhood being looked at.
function updateDetailTiles() {
  if (!globe || !detail.group) return;
  const variant = state.globeActive ? state.variant || state.map.variants[0] : null;
  if (!variant) {
    clearDetailTiles();
    return;
  }
  if (detail.variant !== variant.tiles) {
    clearDetailTiles();
    detail.variant = variant.tiles;
  }
  const pov = globe.pointOfView();
  const zoom = Math.min(
    Math.round(zoomForAltitude(pov.altitude)) + 1,
    variant.maxZoom,
  );
  if (zoom <= textureZoom || !variant.formats[zoom]) {
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
  const key = `${variant.tiles}:${[...wanted].sort().join(",")}`;
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
    const mesh = tileMesh(variant, z, column, row);
    detail.tiles.set(name, mesh);
    detail.group.add(mesh);
  }
}

// tileMesh drapes one pyramid tile at its place on the sphere: a grid of
// points through the same latitude-longitude spelling the pins stand by,
// wearing the tile image once it arrives.
function tileMesh(variant, zoom, column, row) {
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
  const url = `${state.game.base}/tiles/${variant.tiles}/${zoom}/${column}/${row}.${variant.formats[zoom]}`;
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
    const size = selected ? 9 : 5;
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

// syncFilters shows and hides sprites as the legend and the search decide,
// the same visibility the chart draws from.
function syncFilters() {
  if (!globe || !state.globeActive) return;
  for (const [pin, sprite] of sprites) {
    sprite.visible = !pin.filteredHidden && !state.hiddenCategories.has(pin.category.id);
  }
}

// spriteFor stands one pin up as a billboard wearing its category's icon --
// the same haloed, tinted raster the chart composes -- so what a marker is
// reads the same on the sphere as on the sheet.
function spriteFor(pin) {
  const sprite = new Sprite(material(pin.category, pin === state.selectedPin));
  const size = pin === state.selectedPin ? 9 : 5;
  sprite.scale.set(size, size, 1);
  // Born dressed: the renderer creates sprites on its own tick, after any
  // sweep that ran at hand-over, so the filters must already be worn.
  sprite.visible = !pin.filteredHidden && !state.hiddenCategories.has(pin.category.id);
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
  held = new SpriteMaterial({ depthWrite: false });
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
// it: another raster variant chosen, a category switched in the legend.
export function refreshGlobe() {
  if (state.globeActive) void enterGlobe();
}

// resizeGlobe keeps the canvas the size of its pane; globe.gl sizes once at
// construction and must be told when the pane moves.
export function resizeGlobe() {
  if (!globe || elements.globe.hidden) return;
  globe.width(elements.globe.clientWidth).height(elements.globe.clientHeight);
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
async function composeTexture(variant) {
  const zoom = Math.min(textureZoom, variant.maxZoom);
  const format = variant.formats[zoom];
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
      const url = `${state.game.base}/tiles/${variant.tiles}/${zoom}/${x}/${y}.${format}`;
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
