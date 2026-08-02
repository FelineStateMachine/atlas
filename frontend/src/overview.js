import { overviewTargetSize } from "./constants.js";
import { elements } from "./dom.js";
import { aimGlobe, globeCamera } from "./globe.js";
import { activeExtent, releaseCameraReturn, settleView, tileExists } from "./navigation.js";
import { equirectMapping } from "./semconv.js";
import { saveSession } from "./session.js";
import { state } from "./state.js";
import { clamp } from "./util.js";

export function renderOverview() {
  const lens = state.lens;
  const canvas = elements.overviewCanvas;
  const context = canvas.getContext("2d");
  const extent = activeExtent();
  const world = state.volume.tileGrid.size;
  const tileSize = state.volume.tileGrid.tileSize;
  const width = extent[2] - extent[0];
  const height = extent[3] - extent[1];

  let level = 0;
  while (level < lens.fullZoom &&
    (Math.max(width, height) / world) * tileSize * (2 ** level) < overviewTargetSize) {
    level++;
  }
  const scale = (tileSize * (2 ** level)) / world;
  canvas.width = Math.max(1, Math.round(width * scale));
  canvas.height = Math.max(1, Math.round(height * scale));
  context.clearRect(0, 0, canvas.width, canvas.height);
  if (lens.background) {
    context.fillStyle = lens.background;
    context.fillRect(0, 0, canvas.width, canvas.height);
  }

  const run = ++state.overviewRun;
  const format = lens.formats[level];
  const first = Math.floor((extent[0] / world) * (2 ** level));
  const last = Math.ceil((extent[2] / world) * (2 ** level));
  const top = Math.floor((-extent[3] / world) * (2 ** level));
  const bottom = Math.ceil((-extent[1] / world) * (2 ** level));
  for (let y = top; y < bottom; y++) {
    for (let x = first; x < last; x++) {
      if (!format || !tileExists(lens, level, x, y)) continue;
      const image = new Image();
      image.onload = () => {
        if (state.overviewRun !== run) return;
        context.drawImage(
          image,
          x * tileSize - extent[0] * scale,
          y * tileSize + extent[3] * scale,
          tileSize,
          tileSize,
        );
      };
      image.src = `${state.volume.base}/tiles/${encodeURIComponent(lens.tiles)}/${level}/${x}/${y}.${format}`;
    }
  }
  updateOverviewViewport();
}

// Hidden while the whole map is on screen: a locator that says "all of it"
// tells the reader nothing they cannot already see.
export function updateOverviewViewport() {
  // On the globe half the planet is always out of sight, so the locator is
  // always worth having: a reticle at the point the camera faces, on the
  // same flat chart the corner has always drawn.
  const camera = globeCamera();
  if (camera) {
    const mapping = equirectMapping(state.world);
    if (!mapping || !state.lens) return;
    const extent = activeExtent();
    const [worldX, worldY] = mapping.toWorld(camera.lat, camera.lng);
    const canvas = elements.overviewCanvas;
    const across = clamp((worldX - extent[0]) / (extent[2] - extent[0]), 0, 1);
    const down = clamp((worldY + extent[3]) / (extent[3] - extent[1]), 0, 1);
    state.overviewKey = "";
    elements.overviewShelf.hidden = false;
    const box = elements.overviewViewport.style;
    const size = 22;
    box.left = `${across * canvas.width - size / 2}px`;
    box.top = `${down * canvas.height - size / 2}px`;
    box.width = `${size}px`;
    box.height = `${size}px`;
    return;
  }
  const view = state.engine?.getView();
  if (!view || !state.lens) return;
  const extent = activeExtent();
  const visible = view.calculateExtent(state.engine.getSize());
  const width = extent[2] - extent[0];
  const height = extent[3] - extent[1];
  // Fitting the map lands on its extent to within a fraction of a pixel, so an
  // exact comparison reports the whole map as not quite visible and leaves a
  // locator on screen that marks the entire map.
  const slack = (view.getResolution() || 0) * 4;
  const covered =
    visible[0] <= extent[0] + slack && visible[1] <= extent[1] + slack &&
    visible[2] >= extent[2] - slack && visible[3] >= extent[3] - slack;
  const key = covered ? "hidden" : visible.map(Math.round).join(",");
  if (key === state.overviewKey) return;
  state.overviewKey = key;
  // The handle goes with it: there is nothing to send for while the whole map
  // is already on screen.
  elements.overviewShelf.hidden = covered;
  if (covered) return;

  const canvas = elements.overviewCanvas;
  const left = clamp((visible[0] - extent[0]) / width, 0, 1);
  const right = clamp((visible[2] - extent[0]) / width, 0, 1);
  const upper = clamp((extent[3] - visible[3]) / height, 0, 1);
  const lower = clamp((extent[3] - visible[1]) / height, 0, 1);
  const box = elements.overviewViewport.style;
  box.left = `${left * canvas.width}px`;
  box.top = `${upper * canvas.height}px`;
  // A floor keeps the rectangle findable when the view is deep enough that a
  // true-to-scale box would be a couple of pixels.
  box.width = `${Math.max(8, (right - left) * canvas.width)}px`;
  box.height = `${Math.max(8, (lower - upper) * canvas.height)}px`;
}

// Put away rather than switched off: the overview keeps drawing, and the handle
// it leaves behind is the whole of the way back. A reader who wants the corner
// of their map usually wants it for that map's whole session, so the choice is
// remembered with the rest of what a volume is left in.
export function setOverviewDocked(docked, remember = true) {
  state.overviewDocked = docked;
  // A sweep in progress ends where it is rather than following the panel out.
  if (docked && state.overviewPointer !== null) {
    state.overviewPointer = null;
    settleView();
  }
  elements.overviewShelf.classList.toggle("is-docked", docked);
  elements.overviewDock.setAttribute("aria-expanded", String(!docked));
  const label = docked ? "Bring the overview back" : "Put the overview away";
  elements.overviewDock.setAttribute("aria-label", label);
  elements.overviewDock.title = label;
  if (remember) saveSession();
}

// The press that opens a gesture eases across, so a plain click reads as a
// move rather than a cut. Everything after it lands at once: a view that eased
// its way to each new position would trail behind the hand steering it.
export function overviewNavigate(event, ease) {
  // Steering from the corner is steering: whatever view a panel's jump was
  // holding is the reader's to give up here as much as on the map itself.
  releaseCameraReturn();
  const rect = elements.overviewCanvas.getBoundingClientRect();
  const extent = activeExtent();
  const fraction = (value, low, high) => clamp((value - low) / (high - low), 0, 1);
  // The same press steers whichever pane is showing: the chart's window, or
  // the globe turned to face the point under the finger.
  if (state.globeActive) {
    const mapping = equirectMapping(state.world);
    if (!mapping) return;
    const worldX = extent[0] + fraction(event.clientX, rect.left, rect.right) * (extent[2] - extent[0]);
    const worldY = -extent[3] + fraction(event.clientY, rect.top, rect.bottom) * (extent[3] - extent[1]);
    const [lat, lng] = mapping.toLatLng(worldX, worldY);
    aimGlobe(lat, lng, ease);
    return;
  }
  const view = state.engine?.getView();
  if (!view || !state.lens) return;
  const center = [
    extent[0] + fraction(event.clientX, rect.left, rect.right) * (extent[2] - extent[0]),
    extent[3] - fraction(event.clientY, rect.top, rect.bottom) * (extent[3] - extent[1]),
  ];
  if (ease) {
    view.animate({ center, duration: 180 });
    return;
  }
  view.cancelAnimations();
  view.setCenter(center);
}
