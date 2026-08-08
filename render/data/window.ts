import type { Camera } from "../scene/read.ts";

/** A settled camera key precise enough that materially different bboxes differ. */
export function demandWindowKey(camera: Camera | null): string {
  if (!camera) return "fit";
  return `${camera.zoom.toFixed(2)}/${camera.x.toFixed(2)}/${camera.y.toFixed(2)}`;
}

/** Keep one demanded window per world; a pan replaces rather than accumulates. */
export function replaceDemandWindow<T>(cache: Map<string, T>, prefix: string, key: string, value: T): void {
  for (const held of cache.keys()) {
    if (held.startsWith(prefix) && held !== key) cache.delete(held);
  }
  cache.set(key, value);
}
