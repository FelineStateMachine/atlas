import Feature from "ol/Feature.js";
import Polygon from "ol/geom/Polygon.js";

import { geohashAlphabet, geohashMaxDepth } from "./constants.js";
import { closeDetail } from "./detail.js";
import { elements } from "./dom.js";
import { activeExtent, viewMaxZoom } from "./navigation.js";
import { refreshPrioritySource } from "./pins.js";
import { state } from "./state.js";

// What the grid divides is the ground the map covers, which is not the window
// its raster is cut from: a piece of a sheet keeps a margin so the title drawn
// beside it survives the crop. On a big map that margin is nothing; on a small
// one it is a fifth of the width, and the cells over it are cells the map never
// gets -- b naming blank sheet while m carries what b should have held.
export function gridExtent() {
  const surface = state.variant?.surface;
  if (!surface) return activeExtent();
  return [
    surface.x,
    -(surface.y + surface.height),
    surface.x + surface.width,
    -surface.y,
  ];
}

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
}

export function updateGridHint() {
  if (!state.gridEnabled) {
    elements.gridHint.textContent = "G · grid off";
    return;
  }
  // Compact while it is carrying a hash: the mode, the place and the state of
  // the subdivision are one reading, not three separate ones.
  elements.gridHint.textContent = `G-${state.gridPrefix || "root"}` +
    (state.subgridVisible ? "" : " no subgrid");
}

export function selectGridPrefix(prefix) {
  const maximum = gridMaxDepth();
  const normalized = [...prefix.toLocaleLowerCase()]
    .filter((character) => geohashAlphabet.includes(character))
    .slice(0, maximum)
    .join("");
  const changed = normalized !== state.gridPrefix;
  state.gridPrefix = normalized;
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
    maxZoom: viewMaxZoom(state.variant),
    duration: 180,
  });
}

export function ascendGrid() {
  if (!state.gridEnabled) return;
  if (!state.gridPrefix) {
    toggleGrid(false);
    return;
  }
  selectGridPrefix(state.gridPrefix.slice(0, -1));
}

export function renderGrid() {
  if (!state.sources) return;
  state.sources.grid.clear();
  state.sources.gridContext.clear();
  elements.gridNavigator.hidden = !state.gridEnabled;
  updateGridHint();
  if (!state.gridEnabled || !state.variant) return;

  const maximum = gridMaxDepth();
  elements.gridInput.maxLength = maximum;
  elements.gridInput.value = state.gridPrefix;
  elements.gridBack.title = state.gridPrefix ? "Back one geohash level" : "Close geohash grid";

  renderGridContext();
  if (state.gridPrefix.length >= maximum) {
    addGridFeature(state.gridPrefix, currentGridExtent(), "leaf");
    return;
  }
  // The cell the reader is inside, outlined rather than tiled. It is the one
  // part of the grid that survives putting the grid away: what is on screen is
  // still a chosen place, and a boundary says so where a bare map does not.
  if (state.gridPrefix) {
    addGridFeature(state.gridPrefix, currentGridExtent(), "scope");
  }
  for (const character of geohashAlphabet) {
    const hash = state.gridPrefix + character;
    addGridFeature(hash, geohashExtent(hash), "child");
  }
}

export function renderGridContext() {
  for (let depth = 0; depth < state.gridPrefix.length; depth++) {
    const parent = state.gridPrefix.slice(0, depth);
    const selected = state.gridPrefix.slice(0, depth + 1);
    for (const character of geohashAlphabet) {
      const hash = parent + character;
      if (hash === selected) continue;
      addGridFeature(hash, geohashExtent(hash), "neighbor", state.gridPrefix.length - depth);
    }
  }
}

export function addGridFeature(hash, extent, role, contextDistance = 0) {
  const [minimumX, minimumY, maximumX, maximumY] = extent;
  const count = state.pins.filter((pin) => {
    if (pin.filteredHidden) return false;
    const [x, y] = pin.coordinate;
    return x >= minimumX && x <= maximumX && y >= minimumY && y <= maximumY;
  }).length;
  const feature = new Feature({
    geometry: new Polygon([[
      [minimumX, minimumY],
      [minimumX, maximumY],
      [maximumX, maximumY],
      [maximumX, minimumY],
      [minimumX, minimumY],
    ]]),
    gridCell: { hash, extent, role, count, contextDistance },
    priority: role === "neighbor"
      ? -contextDistance * 100 + geohashAlphabet.indexOf(hash[hash.length - 1])
      : geohashAlphabet.indexOf(hash[hash.length - 1]),
  });
  const source = role === "neighbor" ? state.sources.gridContext : state.sources.grid;
  source.addFeature(feature);
}

export function currentGridExtent() {
  return geohashExtent(state.gridPrefix);
}

export function geohashExtent(hash) {
  const extent = [...gridExtent()];
  let splitX = true;
  for (const character of hash) {
    const value = geohashAlphabet.indexOf(character);
    if (value < 0) continue;
    for (const mask of [16, 8, 4, 2, 1]) {
      if (splitX) {
        const middle = (extent[0] + extent[2]) / 2;
        if (value & mask) extent[0] = middle;
        else extent[2] = middle;
      } else {
        const middle = (extent[1] + extent[3]) / 2;
        if (value & mask) extent[1] = middle;
        else extent[3] = middle;
      }
      splitX = !splitX;
    }
  }
  return extent;
}

export function gridMaxDepth() {
  return geohashMaxDepth;
}

export function pinInGridCell(pin) {
  if (!state.gridEnabled || !state.gridPrefix) return false;
  const extent = currentGridExtent();
  const [x, y] = pin.coordinate;
  return x >= extent[0] && x <= extent[2] && y >= extent[1] && y <= extent[3];
}
