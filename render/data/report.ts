// The camera's whisper upward.
//
// It is the one thing that leaves this lane besides a pick, and -- like the
// pick -- it leaves as a DOM event rather than as a request. The application
// renders a form for it and the fields it wants filled; this fills them and
// says so; the page's own hypermedia runtime posts it and applies the answer.
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
// on the next launch and nothing else.

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
