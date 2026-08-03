// What leaves this lane: a settled camera, a pick, and a cell.
//
// They are the only three, and they leave the same way -- as a DOM event
// rather than as a request. The application renders a form for each and the
// fields it wants filled; this fills them and says so; the page's own
// hypermedia runtime posts it and applies the answer.
//
// THAT SPLIT IS DELIBERATE and it is the reason this module makes no network
// call at all. A report the seam posted for itself would throw the answer
// away, and the answer is the state island carrying the camera that was just
// written -- the server's half of the joint diagnostics (issue #5 §6), which
// would then be a render behind the reader forever. Data flows one way
// (docs/render-seam.md §1.1); this is the page carrying a value the seam
// happens to be the only one who can compute, which is exactly what the
// labels hint and the corner locator already are.
//
// A page that renders no such form -- a host that mounted the seam over some
// other chrome -- simply loses the report, which costs a reader their place
// on the next launch, or a click that selects nothing, and nothing else.

import { logger } from "../log.ts";

const log = logger("data");

/** A camera as the session stores it, keyed by the world it belongs to. */
export interface CameraPost {
  readonly volume: string;
  readonly world: string;
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
  readonly rotation: number;
}

/** Report a settled camera. Never throws: the page is not the report's. */
export function reportCamera(camera: CameraPost): void {
  const form = document.querySelector("#atlas-camera");
  if (!form) return;
  const fill = (id: string, value: string) => {
    const field = document.querySelector<HTMLInputElement>(id);
    if (field) field.value = value;
  };
  fill("#atlas-camera-world", camera.world);
  fill("#atlas-camera-x", String(camera.x));
  fill("#atlas-camera-y", String(camera.y));
  fill("#atlas-camera-zoom", String(camera.zoom));
  fill("#atlas-camera-rotation", String(camera.rotation));
  window.dispatchEvent(new CustomEvent("atlas:camera", { bubbles: false }));
  log.debug("the camera settled", {
    op: "session", volume: camera.volume, world: camera.world, zoom: camera.zoom,
  });
}

/**
 * A hit, as the pane that resolved it describes it.
 *
 * `kind` rides in the event and posts no field. The session stores an identity
 * and looks the rest up itself, so a form field for the kind would be the seam
 * telling the application something it already knows -- but a consumer of the
 * event that wants to tell a pin from an area (a status line, a cursor) should
 * not have to ask, so it travels.
 */
export interface PickPost {
  readonly feature: string;
  readonly kind: string;
}

/** Report a pick off the canvas. Never throws: the page is not the report's. */
export function reportPick(pick: PickPost): void {
  // A MISS DOES NOT POST. Clicking open water is not a request to close what
  // the reader is reading: the card is put away by Escape and by its own
  // button, and by nothing else. An empty feature posted here would clear the
  // selection on every stray click, which is a thing the reference never did.
  if (!pick.feature) return;
  const form = document.querySelector("#atlas-pick");
  if (!form) return;
  const field = document.querySelector<HTMLInputElement>("#atlas-pick-feature");
  if (field) field.value = pick.feature;
  window.dispatchEvent(new CustomEvent("atlas:pick", {
    bubbles: false, detail: { feature: pick.feature, kind: pick.kind },
  }));
  log.debug("a pick off the canvas", {
    op: "session", feature: pick.feature, kind: pick.kind,
  });
}

/**
 * Report a cell chosen off a surface -- the chart's canvas, or the sphere.
 *
 * The second form, and the same arrangement as the first: the panes resolve a
 * pixel into an address, which is a question only they can answer, and the
 * address is the session's to keep. What comes back is the whole scene the new
 * cell implies -- the navigator's field, the dock's narrowed list, the state
 * island the panes redraw from -- so this cannot be a request the seam makes
 * for itself any more than a pick can.
 *
 * AN EMPTY ADDRESS IS NOT THE ROOT HERE. It is a place -- the field posts it
 * on the way out, and Escape telescopes to it -- but neither of this
 * function's callers can mean it: the canvas only ever names a cell it drew,
 * and the sphere's descent answers `""` for "there is nothing below this",
 * which is the floor of the telescope rather than the top of it. So an empty
 * cell is a miss, and a miss says nothing, exactly as a pick's does.
 */
export function reportGridPick(cell: string): void {
  if (!cell) return;
  const form = document.querySelector("#atlas-grid-pick");
  if (!form) return;
  const field = document.querySelector<HTMLInputElement>("#atlas-grid-pick-cell");
  if (field) field.value = cell;
  window.dispatchEvent(new CustomEvent("atlas:grid-pick", {
    bubbles: false, detail: { cell },
  }));
  log.debug("a cell chosen off a surface", { op: "session", cell });
}
