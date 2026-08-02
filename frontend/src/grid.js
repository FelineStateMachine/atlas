// The grid controller: the telescoping interactions -- open, descend,
// ascend, divide, hold pins to the chosen cell -- spoken against whichever
// cell system divides this map. The systems themselves live in
// cellsystems/, each a pure object keeping one contract; this file owns
// the state, the navigator, and the plan both renderers draw from, and it
// no longer knows what a geohash is.
import Feature from "ol/Feature.js";
import MultiPolygon from "ol/geom/MultiPolygon.js";
import Polygon from "ol/geom/Polygon.js";

import {
  activeSystem,
  applicableSystems,
  clipRingX,
  equivalentCell,
  surfaceExtent,
} from "./cellsystems/index.js";
import { gridTheme, palette } from "./constants.js";
import { closeDetail } from "./detail.js";
import { elements } from "./dom.js";
import { viewMaxZoom } from "./navigation.js";
import { refreshPrioritySource } from "./features.js";
import { state } from "./state.js";

export function handleGridKey(event) {
  if (event.key.toLocaleLowerCase() === "g") {
    event.preventDefault();
    toggleGrid();
    // The grid is opened to go somewhere, so the field that takes a hash is
    // ready for the rest of what is being typed: "gm6" arrives at m6. Closing
    // hands the keyboard back to the map, where the shortcuts live.
    if (state.gridEnabled) {
      elements.gridInput.focus();
      elements.gridInput.select();
    } else {
      elements.viewport.focus({ preventScroll: true });
    }
    return true;
  }
  // Space is about what the chosen cell is divided into, not where the reader
  // is: the cell stays chosen and the pins outside it stay put away, while the
  // subdivision inside it gets out of the way of the map underneath.
  // The button answers its own space bar, and would otherwise be pressed twice.
  if (event.key === " " && state.gridEnabled && event.target !== elements.subgridToggle) {
    event.preventDefault();
    setSubgridVisible(!state.subgridVisible);
    return true;
  }
  if (event.key === "Escape" && state.gridEnabled) {
    event.preventDefault();
    ascendGrid();
    return true;
  }
  return false;
}

export function toggleGrid(enabled = !state.gridEnabled) {
  state.gridEnabled = enabled;
  // Opening the grid divides the cell again. A reader who put the subgrid away
  // and then closed the grid altogether is starting over, not resuming.
  if (enabled) setSubgridVisible(true);
  renderGrid();
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.text.changed();
  state.layers.priority.changed();
}

// Dividing the chosen cell and being held to it are two questions. The
// subdivision is how a reader picks the next place; once picked, the mesh over
// the map is often the last thing they want to look at, and until now the only
// way to be rid of it was to give up the scope it was holding. Nothing is
// really hidden: the cell keeps its boundary and the ancestors keep theirs.
export function setSubgridVisible(visible) {
  state.subgridVisible = visible;
  state.layers.grid.changed();
  state.layers.gridContext.changed();
  elements.subgridToggle.setAttribute("aria-pressed", String(visible));
  elements.subgridToggle.setAttribute("aria-label",
    visible ? "Hide the subgrid" : "Show the subgrid");
  updateGridHint();
  document.dispatchEvent(new Event("atlas:grid"));
}

export function updateGridHint() {
  if (!state.gridEnabled) {
    elements.gridHint.textContent = "G · grid off";
    return;
  }
  // Compact while it is carrying a hash: the mode, the place and the state of
  // the subdivision are one reading, not three separate ones.
  elements.gridHint.textContent = `G-${state.gridCell || "root"}` +
    (state.subgridVisible ? "" : " no subgrid");
}

export function selectGridCell(raw) {
  const system = activeSystem();
  const id = system.parseInput(system.normalizeInput(raw));
  if (id === null) return;
  const changed = id !== state.gridCell;
  state.gridCell = id;
  if (!state.gridEnabled) state.gridEnabled = true;
  renderGrid();
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.text.changed();
  state.layers.priority.changed();
  if (!changed) return;
  closeDetail();
  state.engine.getView().fit(currentGridExtent(), {
    size: state.engine.getSize(),
    padding: [52, 52, 52, 52],
    maxZoom: viewMaxZoom(state.lens),
    duration: 180,
  });
}

export function ascendGrid() {
  if (!state.gridEnabled) return;
  if (!state.gridCell) {
    toggleGrid(false);
    return;
  }
  selectGridCell(activeSystem().parent(state.gridCell));
}

export function renderGrid() {
  if (!state.sources) return;
  state.sources.grid.clear();
  state.sources.gridContext.clear();
  elements.gridNavigator.hidden = !state.gridEnabled;
  updateGridHint();
  syncGridSystemControl();
  if (state.gridEnabled && state.lens) {
    const system = activeSystem();
    elements.gridInput.maxLength = system.inputLength(state.world);
    elements.gridInput.value = state.gridCell;
    elements.gridBack.title = state.gridCell
      ? `Back one ${system.slug} level`
      : `Close ${system.slug} grid`;
    for (const cell of gridCellPlan()) {
      addGridFeature(cell);
    }
  }
  // Anything else drawing the grid -- the globe -- redraws from the same
  // plan the same moment the chart does.
  document.dispatchEvent(new Event("atlas:grid"));
}

// syncGridSystemControl keeps the navigator honest about which systems can
// divide this map: one small button wearing the current system's mark,
// cycling to the next on click -- and nothing at all when geohash is the
// only voice. The full name lives on the field label beside it.
function syncGridSystemControl() {
  const systems = applicableSystems(state.world);
  const active = activeSystem();
  elements.gridSystemName.textContent = active.name;
  elements.gridSystem.hidden = systems.length < 2;
  if (systems.length < 2) return;
  const next = systems[(systems.indexOf(active) + 1) % systems.length];
  elements.gridSystem.textContent = active.short;
  elements.gridSystem.title = `Dividing by ${active.name} · ⌘G for ${next.name}`;
  elements.gridSystem.setAttribute("aria-label",
    `Cell system: ${active.name}. Switch to ${next.name}`);
}

// cycleGridSystem steps to the next system that can divide this map.
export function cycleGridSystem() {
  const systems = applicableSystems(state.world);
  if (systems.length < 2) return;
  const at = systems.indexOf(activeSystem());
  setGridSystem(systems[(at + 1) % systems.length].slug);
}

// setGridSystem changes which system divides the map. A chosen place
// survives the change: the new hierarchy's cell over the old cell's center
// at the nearest precision, so switching dividers re-addresses the ground
// under the reader rather than sending them back to the root. The view
// stays put -- the new cell covers roughly the ground already on screen.
export function setGridSystem(slug) {
  if (slug === state.gridSystem) return;
  const from = activeSystem();
  const held = state.gridCell;
  state.gridSystem = slug;
  state.gridCell = held ? equivalentCell(from, activeSystem(), held, state.world) : "";
  renderGrid();
  refreshPrioritySource();
  state.layers.pins.changed();
  state.layers.text.changed();
  state.layers.priority.changed();
}

// gridCellColor is the accent a cell wears everywhere it is drawn, chosen
// by the system so siblings differ and a cell keeps its color at every
// depth it appears.
export function gridCellColor(id) {
  return palette[activeSystem().colorKey(id)];
}

// gridCellVisual is the one styling of a grid cell, as pure tokens: what
// its boundary weighs, what fills it, and what its corner label says. The
// chart adapts these into ol/style and the globe into materials and
// sprites; neither holds a color or width of its own, so the projections
// read as one instrument. A child cell too small for its label returns
// nothing at all -- the subdivision appears at the size it can be read,
// on both panes at the same visual moment.
export function gridCellVisual(cell, { subgridVisible, labelled }) {
  const { role, contextDistance } = cell;
  if (role === "child" && (!labelled || !subgridVisible)) return null;
  const color = gridCellColor(cell.hash);
  const chosen = role === "leaf" || role === "scope";
  const bare = role === "scope" && subgridVisible;

  const line = {
    color: chosen ? gridTheme.lineWhite : color,
    opacity: role === "neighbor"
      ? gridTheme.neighborLineAlpha
      : role === "child" ? gridTheme.childLineAlpha : 1,
    widthPx: bare
      ? gridTheme.widths.scopeBare
      : gridTheme.widths[role],
  };

  let fill = null;
  if (role === "neighbor") {
    fill = {
      color: gridTheme.dimColor,
      opacity: Math.min(gridTheme.dimCap, gridTheme.dimBase + contextDistance * gridTheme.dimStep),
    };
  } else if (role === "leaf") {
    fill = { color, opacity: gridTheme.leafFillAlpha };
  } else if (role === "child") {
    fill = { color, opacity: gridTheme.childFillAlpha };
  }

  let label = null;
  if (labelled && !bare) {
    const { context, principal } = activeSystem().label(cell.hash);
    label = {
      prefix: context,
      final: principal,
      color: chosen ? gridTheme.lineWhite : color,
      textAlpha: role === "neighbor" ? gridTheme.neighborTextAlpha : 1,
      chip: role === "neighbor" ? gridTheme.neighborChip : gridTheme.chip,
      sizePx: role === "neighbor" ? gridTheme.neighborLabelSizePx : gridTheme.labelSizePx,
    };
  }
  return { line, fill, label, bare };
}

// gridCellPlan is the one account of which cells the grid shows: the chosen
// cell outlined, its subdivision, and the dimmed neighbors of every
// ancestor. The chart tiles it as polygons and the globe drapes it as
// boundaries, both reading this. Emission order is part of the contract --
// the parity harness compares it positionally.
export function gridCellPlan() {
  const system = activeSystem();
  const cells = [];
  const chain = ancestorChain(system, state.gridCell);
  for (let depth = 0; depth < chain.length - 1; depth++) {
    const selected = chain[depth + 1];
    for (const id of system.children(chain[depth])) {
      if (id === selected) continue;
      cells.push(planCell(system, id, "neighbor", chain.length - 1 - depth));
    }
  }
  if (state.gridCell && system.level(state.gridCell) >= system.maxLevel(state.world)) {
    cells.push(planCell(system, state.gridCell, "leaf", 0));
    return cells;
  }
  // The cell the reader is inside, outlined rather than tiled. It is the one
  // part of the grid that survives putting the grid away: what is on screen is
  // still a chosen place, and a boundary says so where a bare map does not.
  if (state.gridCell) {
    cells.push(planCell(system, state.gridCell, "scope", 0));
  }
  for (const id of system.children(state.gridCell)) {
    cells.push(planCell(system, id, "child", 0));
  }
  return cells;
}

// ancestorChain walks from the root down to the chosen cell, root first.
function ancestorChain(system, id) {
  const chain = [id];
  let held = id;
  while (held) {
    held = system.parent(held);
    chain.unshift(held);
  }
  return chain;
}

function planCell(system, id, role, contextDistance) {
  return {
    hash: id,
    extent: system.bbox(id),
    ring: system.ring(id),
    pole: system.poleContained(id),
    childIndex: system.childIndex(id),
    role,
    contextDistance,
  };
}

export function addGridFeature(cell) {
  const system = activeSystem();
  const count = state.features.filter((pin) =>
    !pin.filteredHidden && system.contains(cell.hash, pin.coordinate)).length;
  const feature = new Feature({
    geometry: gridGeometry(cell),
    // Exactly these five keys: the parity harness serializes this object,
    // and the id keeps the field name `hash` whatever system minted it.
    gridCell: {
      hash: cell.hash,
      extent: cell.extent,
      role: cell.role,
      count,
      contextDistance: cell.contextDistance,
    },
    priority: cell.role === "neighbor"
      ? -cell.contextDistance * 100 + cell.childIndex
      : cell.childIndex,
  });
  const source = cell.role === "neighbor" ? state.sources.gridContext : state.sources.grid;
  source.addFeature(feature);
}

// gridGeometry tiles one plan cell for the chart. A system's ring is a
// closed, continuous loop; most rings lie within the surface and become a
// plain polygon -- every geohash ring does, byte for byte as before. A
// cell holding a pole circles it, so its loop closes along the pole's own
// edge of the picture; and a ring that stayed continuous across the
// antimeridian is drawn twice, clipped to the surface as it lies and
// shifted a world over, so the one cell appears as its two pieces.
function gridGeometry(cell) {
  const surface = surfaceExtent();
  let ring = cell.ring;
  if (cell.pole) {
    // The whole loop, closing point included: a pole cell's walk ends a
    // world over from where it began, and the closure is what spans the
    // last tessellation step. Dropping it left a sliver of ground the
    // polygon never covered, one step wide, at the walk's own longitude.
    const poleY = cell.pole === "north" ? surface[3] : surface[1];
    ring = [
      ...ring,
      [ring[ring.length - 1][0], poleY],
      [ring[0][0], poleY],
      ring[0],
    ];
  }
  const inside = ring.every(([x]) => x >= surface[0] && x <= surface[2]);
  if (inside) return new Polygon([ring]);
  const width = surface[2] - surface[0];
  const pieces = [];
  for (const shift of [0, -width, width]) {
    const clipped = clipRingX(
      ring.map(([x, y]) => [x + shift, y]),
      surface[0],
      surface[2],
    );
    if (clipped.length >= 4) pieces.push([clipped]);
  }
  return new MultiPolygon(pieces);
}

export function currentGridExtent() {
  return activeSystem().bbox(state.gridCell);
}

export function gridMaxLevel() {
  return activeSystem().maxLevel(state.world);
}

export function pinInGridCell(pin) {
  if (!state.gridEnabled || !state.gridCell) return false;
  return activeSystem().contains(state.gridCell, pin.coordinate);
}
