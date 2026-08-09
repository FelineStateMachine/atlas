// Earth authoring enhancement.
//
// The application renders the complete form and owns every route. This module
// contributes only the two facts a browser camera can answer: which local
// world rectangle is visible, and which rectangle a pointer drew. Both are
// converted into the WGS84 fields the server already rendered.

import type { TileGrid } from "./data/payload.ts";
import type { AtlasChart } from "./chart/element.ts";
import type { AtlasViewport } from "./viewport.ts";

export type Extent = readonly [number, number, number, number];

// Six decimal places are the authored form contract. Keeping the clamp on
// that same precision prevents a whole-Earth camera from rounding one unit
// past the native Web Mercator boundary.
const MAX_MERCATOR_LATITUDE = 85.051128;

/** Convert local y-up Atlas pixels into a WGS84 west/south/east/north box. */
export function worldExtentToWGS84(extent: Extent, grid: TileGrid): [number, number, number, number] | null {
  const zoom = grid.sourceZoom;
  const tileSize = grid.tileSize;
  const originX = grid.originX ?? grid.firstTile;
  const originY = grid.originY ?? grid.firstTile;
  const scale = tileSize * 2 ** zoom;
  if (![...extent, zoom, tileSize, originX, originY, scale].every(Number.isFinite) || tileSize <= 0 || scale <= 0) return null;
  const west = longitude(originX * tileSize + extent[0], scale);
  const east = longitude(originX * tileSize + extent[2], scale);
  const north = latitude(originY * tileSize - extent[3], scale);
  const south = latitude(originY * tileSize - extent[1], scale);
  const bounds = [west, south, east, north] as const;
  if (!bounds.every(Number.isFinite) || west >= east || south >= north) return null;
  return bounds.map((value) => rounded(value)) as [number, number, number, number];
}

function longitude(pixel: number, scale: number): number {
  return Math.max(-180, Math.min(180, pixel / scale * 360 - 180));
}

function latitude(pixel: number, scale: number): number {
  const mercator = Math.PI - 2 * Math.PI * pixel / scale;
  const degrees = Math.atan(Math.sinh(mercator)) * 180 / Math.PI;
  return Math.max(-MAX_MERCATOR_LATITUDE, Math.min(MAX_MERCATOR_LATITUDE, degrees));
}

function rounded(value: number): number { return Number(value.toFixed(6)); }

let drawing = false;
let start: { x: number; y: number } | null = null;
const wired = new WeakSet<EventTarget>();

/** Wire the enhancement onto whichever mint controls are currently rendered. */
export function wireMinting(): void {
  const create = document.querySelector<HTMLElement>("#atlas-create");
  if (create && !wired.has(create)) {
    wired.add(create);
    create.addEventListener("click", () => window.setTimeout(useCurrentView, 220));
  }
  const current = document.querySelector<HTMLButtonElement>("[data-mint-current-view]");
  if (current && !wired.has(current)) {
    wired.add(current);
    current.addEventListener("click", useCurrentView);
  }
  const draw = document.querySelector<HTMLButtonElement>("[data-mint-draw]");
  if (draw && !wired.has(draw)) {
    wired.add(draw);
    draw.addEventListener("click", () => setDrawing(!drawing));
  }
  const detail = document.querySelector<HTMLInputElement>('input[name="detail"]');
  if (detail && !wired.has(detail)) {
    wired.add(detail);
    detail.addEventListener("input", () => writeDetail(detail));
  }
  const chart = document.querySelector<AtlasChart>("atlas-chart");
  if (chart && !wired.has(chart)) {
    wired.add(chart);
    chart.addEventListener("pointerdown", beginZone, { capture: true });
  }
}

function useCurrentView(): void {
  const chart = document.querySelector<AtlasChart>("atlas-chart");
  const panel = document.querySelector<HTMLElement>("#atlas-mint");
  if (!chart) return;
  const chartBox = chart.getBoundingClientRect();
  const panelBox = panel?.getBoundingClientRect();
  const visibleWidth = panelBox && panelBox.left > chartBox.left
    ? Math.min(chartBox.width, panelBox.left - chartBox.left)
    : chartBox.width;
  const extent = chart.extentForPixels([0, 0, visibleWidth, chartBox.height]) ?? chart.visibleExtent();
  if (!extent) return;
  fillBounds(extent);
  showZone({ left: chartBox.left, top: chartBox.top, right: chartBox.left + visibleWidth, bottom: chartBox.bottom });
}

function beginZone(event: PointerEvent): void {
  if (!drawing || event.button !== 0) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  start = { x: event.clientX, y: event.clientY };
  showZone({ left: event.clientX, top: event.clientY, right: event.clientX, bottom: event.clientY });
  window.addEventListener("pointermove", moveZone, { capture: true });
  window.addEventListener("pointerup", finishZone, { capture: true, once: true });
  window.addEventListener("pointercancel", cancelZone, { capture: true, once: true });
}

function moveZone(event: PointerEvent): void {
  if (!start) return;
  event.preventDefault();
  showZone({ left: start.x, top: start.y, right: event.clientX, bottom: event.clientY });
}

function finishZone(event: PointerEvent): void {
  window.removeEventListener("pointermove", moveZone, { capture: true });
  window.removeEventListener("pointercancel", cancelZone, { capture: true });
  if (!start) return;
  event.preventDefault();
  const chart = document.querySelector<AtlasChart>("atlas-chart");
  const box = chart?.getBoundingClientRect();
  const width = Math.abs(event.clientX - start.x);
  const height = Math.abs(event.clientY - start.y);
  if (chart && box && width >= 12 && height >= 12) {
    const extent = chart.extentForPixels([
      Math.min(start.x, event.clientX) - box.left,
      Math.min(start.y, event.clientY) - box.top,
      Math.max(start.x, event.clientX) - box.left,
      Math.max(start.y, event.clientY) - box.top,
    ]);
    if (extent) fillBounds(extent);
  }
  start = null;
  setDrawing(false);
}

function cancelZone(): void {
  window.removeEventListener("pointermove", moveZone, { capture: true });
  window.removeEventListener("pointerup", finishZone, { capture: true });
  start = null;
  setDrawing(false);
}

function fillBounds(extent: Extent): void {
  const viewport = document.querySelector<AtlasViewport>("atlas-viewport");
  const bounds = viewport?.current ? worldExtentToWGS84(extent, viewport.current.grid) : null;
  if (!bounds) return;
  ["west", "south", "east", "north"].forEach((name, index) => {
    const field = document.querySelector<HTMLInputElement>(`#atlas-mint input[name="${name}"]`);
    if (field) field.value = String(bounds[index] ?? "");
  });
  window.dispatchEvent(new CustomEvent("atlas:mint-area"));
}

function showZone(rect: { left: number; top: number; right: number; bottom: number }): void {
  const zone = document.querySelector<HTMLElement>("#atlas-mint-zone");
  const panel = document.querySelector<HTMLElement>(".map-panel");
  if (!zone || !panel) return;
  const base = panel.getBoundingClientRect();
  zone.hidden = false;
  zone.dataset.visible = "true";
  zone.style.left = `${Math.min(rect.left, rect.right) - base.left}px`;
  zone.style.top = `${Math.min(rect.top, rect.bottom) - base.top}px`;
  zone.style.width = `${Math.abs(rect.right - rect.left)}px`;
  zone.style.height = `${Math.abs(rect.bottom - rect.top)}px`;
  zone.style.right = "auto";
  zone.style.bottom = "auto";
}

function setDrawing(value: boolean): void {
  drawing = value;
  const button = document.querySelector<HTMLButtonElement>("[data-mint-draw]");
  button?.setAttribute("aria-pressed", String(value));
  document.querySelector(".map-panel")?.classList.toggle("mint-drawing", value);
}

function writeDetail(field: HTMLInputElement): void {
  const output = document.querySelector<HTMLOutputElement>("[data-mint-detail]");
  if (output) output.value = `z${field.value}`;
}
