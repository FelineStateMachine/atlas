// What the two panes are handed.
//
// The viewport host does the loading and the arithmetic once — the payloads,
// the model, the standing set, the ground the cell systems divide — and hands
// both panes the same object. Neither pane fetches, neither pane filters, and
// neither pane can disagree with the other about what is on the map, because
// there is one answer and they are both reading it.

import type { CellSystem, Ground } from "@atlas/analysis";
import type { Lens, TileGrid } from "./data/payload.ts";
import type { Scene } from "./scene/read.ts";
import type { WorldModel } from "./world/model.ts";
import type { Visibility } from "./world/visibility.ts";

/** One world, one lens, one arrangement: everything a pane draws from. */
export interface WorldContext {
  readonly scene: Scene;
  readonly base: string;
  readonly grid: TileGrid;
  readonly model: WorldModel;
  /**
   * The world's title, as the catalog spells it. The model carries the slug
   * -- which is what a URL and a payload name a world by -- and the
   * diagnostics report carries the title, because that is the word a reader
   * is shown.
   */
  readonly worldTitle: string;
  readonly lens: Lens | null;
  readonly lensIndex: number;
  /** The rim markers wear on this world: `light`, `dark`, or none. */
  readonly outset: string;
  readonly ground: Ground;
  /** The cell system in play, and the held cell — both the session's. */
  readonly system: CellSystem | null;
  readonly cell: string;
  readonly subgridVisible: boolean;
  visibility: Visibility;
  /** Whether Z is down. Ephemeral, seam-side, shared by both panes. */
  labelsHeld: boolean;
  /** The feature under the pointer, if any. Ephemeral, chart-side. */
  hovered: string | null;
}

/** A camera as both panes and the session speak it. */
export interface CameraReport {
  readonly world: string;
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
  readonly rotation: number;
}
