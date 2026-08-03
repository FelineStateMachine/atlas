// The diagnostics duty (issue #5 §5.5).
//
// The seam publishes what it knows about itself under one fixed set of key
// names (docs/render-seam.md §8), because a snapshot is diffed key for key
// against the application's half of the same account, and a seam that
// renamed its own diagnostics would be unreadable to every driver of the
// report. This is the fourth contract the lane stands on, and the only one
// that points outward.
//
// THE SPLIT, and why it is where it is. A snapshot is two halves. The
// application's half — what is hidden, what the footer says, what the library
// offers, the saved arrangement — is server state, and it arrives as the JSON
// island `#atlas-session-island` (docs/app.md §6). The seam's half is
// everything only a renderer can answer: where the camera is, how many tiles
// it took, what is standing, what the grid drew, what the sphere built. Two
// halves, one object, and neither half guesses at the other's keys.
//
// THREE GLOBALS, all of them read-only observations:
//
//   `__atlasSeam`      this lane's half, and the canonical entry point.
//   `__atlasGlobe`     the sphere's own counts, and only once it exists —
//                      its mere presence is the answer to "has the sphere
//                      been entered this session".
//   `render_game_to_text()` / `__atlasDebug` / `advanceTime()`
//                      the entry points a headless driver calls. They are
//                      published names, and renaming them would buy nothing.

import { logger } from "./log.ts";
import { COORDINATE_SYSTEM, RASTER_CACHE_SIZE, viewMaxZoom } from "./chart/projection.ts";
import type { DrawnCell } from "./chart/grid.ts";
import type { AtlasViewport } from "./viewport.ts";
import { cellSystems } from "@atlas/analysis";

const log = logger("diagnostics");

/** The seam's half of a diagnostics snapshot. */
export interface SeamSnapshot {
  coordinateSystem: string;
  world: string;
  lens: string;
  zoom: number | null;
  center: [number, number] | null;
  resolution: number | null;
  nativeMaxZoom: number | null;
  maxZoom: number | null;
  interpolate: boolean | null;
  tileStats: { requested: number; loaded: number; errors: number; peakPending: number };
  pins: number;
  eligibleLocations: number;
  domNodes: number;
  canvases: number;
  rasterCacheSize: number;
  labelsHeld: boolean;
  hoveredPin: string | null;
  selectedPin: string | null;
  fitZoom: number | null;
  zones: {
    visible: boolean;
    count: number;
    highlighted: string[];
    focusedPins: number;
  };
  sync: { drawn: number; listable: number };
  grid: {
    enabled: boolean;
    system: string;
    prefix: string;
    maximumDepth: number;
    extent: readonly number[] | null;
    cells: DrawnCell[];
    priorityPins: number;
  };
}

/** Take one reading of the seam. */
export function snapshot(viewport: AtlasViewport): SeamSnapshot {
  const context = viewport.current;
  const chart = viewport.chart;
  const readings = chart?.diagnostics();
  const model = context?.model;
  const visibility = context?.visibility;
  const shapes = visibility?.shapesShown ?? [];
  const selected = context ? model?.feature(context.scene.selected) ?? null : null;
  const hovered = context?.hovered ? model?.pointByID.get(context.hovered) ?? null : null;
  const defaultSystem = cellSystems.systems[0];
  const system = context?.system ?? (context ? defaultSystem ?? null : null);

  return {
    coordinateSystem: COORDINATE_SYSTEM,
    world: context?.worldTitle ?? "",
    lens: context?.lens?.name ?? "",
    zoom: readings?.zoom ?? null,
    center: readings?.center ?? null,
    resolution: readings?.resolution ?? null,
    nativeMaxZoom: context?.lens?.maxZoom ?? null,
    maxZoom: context?.lens ? viewMaxZoom(context.lens) : null,
    interpolate: context?.lens?.interpolate ?? null,
    tileStats: { ...(chart?.tileStats ?? { requested: 0, loaded: 0, errors: 0, peakPending: 0 }) },
    // The registry's own size, and the part of it the filters left standing.
    pins: model?.points.length ?? 0,
    eligibleLocations: visibility?.eligible ?? 0,
    domNodes: document.querySelectorAll("*").length,
    canvases: document.querySelectorAll("canvas").length,
    rasterCacheSize: RASTER_CACHE_SIZE,
    labelsHeld: context?.labelsHeld ?? false,
    hoveredPin: hovered?.title ?? null,
    // A *pin*, and only a pin. The selection is put down when a card opens
    // on ground, so a shape being read about is recorded here as nothing.
    selectedPin: selected && "coordinate" in selected ? selected.title || null : null,
    fitZoom: readings?.fitZoom ?? null,
    // `focused` is deliberately absent: which shape a reader has open is the
    // application's answer, not a renderer's, and the merge below leaves the
    // application's half of `zones` standing rather than writing a null over
    // it.
    zones: {
      visible: shapes.length > 0,
      count: visibility?.shapesHere.length ?? 0,
      highlighted: (visibility?.highlightedShapes ?? []).map((shape) => shape.title),
      focusedPins: visibility?.focusedPins ?? 0,
    },
    // The model's own count of what the map is drawing. What the footer and
    // the dock *say* is the application's half of this key, rendered in Go.
    sync: { drawn: visibility?.drawn ?? 0, listable: visibility?.listable ?? 0 },
    grid: {
      enabled: Boolean(context?.scene.gridSystem),
      system: context?.scene.gridSystem || defaultSystem?.slug || "",
      prefix: context?.cell ?? "",
      maximumDepth: context && system ? system.maxLevel(context.ground) : 0,
      extent: readings?.grid.extent ?? null,
      cells: readings?.grid.cells ?? [],
      priorityPins: visibility?.priorityPins ?? 0,
    },
  };
}

/** Merge the application's half under the seam's, one level deep. */
function mergeHalves(
  app: Record<string, unknown>,
  seam: SeamSnapshot,
): Record<string, unknown> {
  const out: Record<string, unknown> = { ...app };
  for (const [key, value] of Object.entries(seam)) {
    const held = out[key];
    if (isPlain(held) && isPlain(value)) out[key] = { ...held, ...value };
    else out[key] = value;
  }
  return out;
}

function isPlain(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** The window a headless driver reads. Typed here rather than cast at every use. */
interface DiagnosticsWindow {
  __atlasSeam?: { snapshot(): SeamSnapshot; level: string };
  __atlasGlobe?: unknown;
  __atlasDebug?: { snapshot(): SeamSnapshot };
  __atlasAppDiagnostics?: () => Record<string, unknown>;
  render_game_to_text?: () => string;
  advanceTime?: () => void;
}

/**
 * Open the seams a headless driver reads.
 *
 * `__atlasGlobe` is a getter rather than an assignment: its mere presence
 * says "the sphere has been entered at least once this session", so it must
 * not exist before the sphere is built.
 */
export function expose(held: AtlasViewport): void {
  const host = window as unknown as DiagnosticsWindow;
  // The viewport is resolved at every reading rather than captured once. A
  // navigation swaps the whole shell, which replaces the element these seams
  // were opened over -- and a snapshot taken through the old one answers for
  // the volume the reader has left, forever. The element held at boot is the
  // fallback for a page with none on it.
  const live = (): AtlasViewport =>
    document.querySelector<AtlasViewport>("atlas-viewport") ?? held;
  const take = () => snapshot(live());
  host.__atlasSeam = { snapshot: take, level: "1" };
  host.__atlasDebug = { snapshot: take };
  host.advanceTime = () => live().chart?.renderSync();
  // The two halves, merged one level deep.
  //
  // A flat spread would be wrong for the three keys both halves speak to:
  // `sync` is the model's counts from here and the rendered words from the
  // application, `zones` is what is drawn from here and what is open from
  // there. Merging only the top level would let this half's four keys erase
  // the application's four, which is how a snapshot ends up missing the
  // footer's own sentence. Nested objects are merged; everything else, this
  // half wins, because it is the half that watched it happen.
  host.render_game_to_text = () => JSON.stringify(
    mergeHalves(host.__atlasAppDiagnostics?.() ?? {}, take()));
  Object.defineProperty(window, "__atlasGlobe", {
    configurable: true,
    get: () => live().globe?.built ? live().globe?.diagnostics() : undefined,
  });
  log.debug("the diagnostics seams are open", { op: "render" });
}
