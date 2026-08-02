// Watching the scene node across morph swaps.
//
// `#atlas-viewport-state` is an `outerMorph` region: an interaction that moves
// it replaces the node **whole**, so a listener bound to the old node is bound
// to a node no longer in the page. Two mechanisms together survive that:
//
//   A MutationObserver on the node itself catches attribute and child changes
//   when a morph patches it in place, which is what htmx does when the shape
//   has not changed.
//
//   A re-resolve by selector catches the replacement, which is what happens
//   when it has. The boot module calls `rescan()` after every swap — the one
//   after-swap hook issue #5 §4.3 allows — and this class notices the node
//   under its selector is a different element and re-attaches.
//
// Everything downstream sees the same thing either way: one callback carrying
// the new scene and what moved. That is the whole point of reconciling on a
// described scene rather than on events — the seam never has to know which
// interaction produced a change, only what the change was.

import { logger } from "../log.ts";
import type { Scene, SceneChange, StateNode } from "./read.ts";
import { EMPTY_SCENE, readScene, sceneChange } from "./read.ts";

const log = logger("scene");

/** What a consumer is told when the scene moves. */
export type SceneListener = (scene: Scene, change: SceneChange) => void;

/**
 * The live scene: what the node says now, and a callback when it changes.
 *
 * The selector is the contract, not the element: the node is looked up fresh
 * on every rescan, so a swap that replaced it is a re-attach rather than a
 * lost seam.
 */
export class SceneWatcher {
  private node: Element | null = null;
  private observer: MutationObserver | null = null;
  private held: Scene = EMPTY_SCENE;

  private readonly selector: string;
  private readonly listener: SceneListener;
  private readonly root: ParentNode;

  constructor(selector: string, listener: SceneListener, root: ParentNode = document) {
    this.selector = selector;
    this.listener = listener;
    this.root = root;
  }

  /** The scene as last read. */
  get scene(): Scene {
    return this.held;
  }

  /** Attach to whatever is under the selector now, and read it. */
  start(): void {
    this.rescan();
  }

  /** Stop watching. The seam keeps its last scene; it simply stops hearing. */
  stop(): void {
    this.observer?.disconnect();
    this.observer = null;
    this.node = null;
  }

  /**
   * Re-resolve the node and re-read the scene.
   *
   * Called after every swap. It is cheap and idempotent by design: reading a
   * scene is parsing a handful of attributes, and an unchanged scene tells
   * nobody anything.
   */
  rescan(): void {
    const found = this.root.querySelector(this.selector);
    if (found !== this.node) {
      this.observer?.disconnect();
      this.node = found;
      if (found) {
        this.observer = new MutationObserver(() => this.reread());
        this.observer.observe(found, {
          attributes: true, childList: true, subtree: true,
        });
        log.debug("the scene node is being watched", { op: "render" });
      } else {
        this.observer = null;
      }
    }
    this.reread();
  }

  private reread(): void {
    const now = readScene(this.node as StateNode | null);
    const change = sceneChange(this.held, now);
    if (!change.any) return;
    this.held = now;
    log.debug("the scene moved", {
      op: "render", volume: now.volume, world: now.world, lens: now.lens,
      moved: Object.entries(change)
        .filter(([key, moved]) => moved && key !== "any")
        .map(([key]) => key),
    });
    this.listener(now, change);
  }
}
