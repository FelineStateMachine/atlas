// The camera's whisper upward.
//
// This is the *only* thing the seam ever sends the application that is not a
// DOM event, and it lives in the data layer because that is where the network
// lives (issue #5 §9). It is a form post, because that is what the rest of
// the page speaks and a camera is not special enough to invent a wire format
// for; it is debounced by the caller; and it is answered `204`, because
// swapping anything in response to a settling camera would fight the reader's
// own hand (docs/app.md §4.3).
//
// A failed report costs a reader their place on the next launch and nothing
// else, so it is logged and dropped rather than retried into a queue.

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
export async function reportCamera(camera: CameraPost): Promise<void> {
  const body = new URLSearchParams({
    volume: camera.volume,
    world: camera.world,
    x: String(camera.x),
    y: String(camera.y),
    zoom: String(camera.zoom),
    rotation: String(camera.rotation),
  });
  try {
    const response = await fetch("/session/view", {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body,
    });
    if (response.status !== 204 && !response.ok) {
      log.warn("the camera report was refused", {
        op: "session", volume: camera.volume, world: camera.world,
        status: response.status,
      });
      return;
    }
    log.debug("the camera settled", {
      op: "session", volume: camera.volume, world: camera.world, zoom: camera.zoom,
    });
  } catch (error) {
    log.warn("the camera report did not reach the application", {
      op: "session", volume: camera.volume, error: String(error),
    });
  }
}
