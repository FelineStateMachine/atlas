// One answer to what the reader is looking at.
//
// Three surfaces used to work this out for themselves — the canvas drew what
// survived every filter, the footer counted what the legend allowed, the dock
// listed what the search left — and one highlight could cull sixty features
// off the map while the panel beside it went on offering all sixty. The
// application counts its own half in Go now, and this is the seam's half: one
// object, computed once per scene, that the chart, the globe, the picks and
// the diagnostics all read. A filter that lands on one of them lands on all.
//
// THE RULES, in the order they apply to a point:
//
//   SHARD.      A world split into layers offers one at a time. Anything
//               belonging to another layer is *elsewhere in the world*, not
//               merely filtered out, so it is not drawn, not counted, and not
//               picked.
//   FILTERED.   Its collection is hidden, or the search does not name it.
//   HIGHLIGHTS. Highlighted shapes conjoin across collections and unite
//               within one: a pin stands only where every collection holding
//               a highlight claims it.
//   CELL.       A held grid cell narrows the question to what is inside it,
//               exactly the way a highlight does.
//
// The last two have the same bypass, and it is deliberate: the selected
// feature never disappears out from under the card that is open about it, and
// a searched-for name is what the reader asked to see. Highlighting is a way
// of reading the map, not a way of losing your place.

import type { Scene } from "../scene/read.ts";
import type { Coordinate, Line, PointRecord, ShapeRecord, WorldModel } from "./model.ts";

/** Whether a coordinate lies in a cell of whatever system is in play. */
export type CellTest = ((at: Coordinate) => boolean) | null;

/** What one point's standing is, in the terms the rules are written in. */
export interface PointStanding {
  readonly filteredHidden: boolean;
  readonly passesHighlights: boolean;
  readonly inCell: boolean;
  readonly searched: boolean;
  readonly hidden: boolean;
  /** Drawn above everything: selected, hovered, searched, or inside the cell. */
  readonly promoted: boolean;
}

/** The standing of everything, for one scene. */
export class Visibility {
  readonly points: readonly PointStanding[];
  readonly shapesShown: readonly ShapeRecord[];
  readonly eligible: number;
  readonly drawnPoints: number;
  readonly drawn: number;
  readonly listable: number;
  readonly highlightedShapes: readonly ShapeRecord[];
  readonly focusedPins: number;
  readonly priorityPins: number;

  readonly model: WorldModel;
  readonly scene: Scene;
  readonly shard: number;

  constructor(
    model: WorldModel,
    scene: Scene,
    shard: number,
    inCell: CellTest,
    hovered: string | null = null,
  ) {
    this.model = model;
    this.scene = scene;
    this.shard = shard;
    const search = scene.search.toLocaleLowerCase();
    const highlighted = model.shapes.filter((shape) => scene.highlighted.has(shape.id));
    this.highlightedShapes = highlighted;
    const groups = new Map<number, ShapeRecord[]>();
    for (const shape of highlighted) {
      const held = groups.get(shape.collection.id);
      if (held) held.push(shape);
      else groups.set(shape.collection.id, [shape]);
    }

    const standings: PointStanding[] = [];
    let eligible = 0;
    let focused = 0;
    let priority = 0;
    for (const point of model.points) {
      const onShard = onActiveShard(point.shard, shard);
      const searched = Boolean(search) && point.title.toLocaleLowerCase().includes(search);
      const filteredHidden = scene.hidden.has(String(point.collection.id)) ||
        (Boolean(search) && !searched);
      const selected = point.id === scene.selected;
      const passesHighlights = groups.size === 0
        ? false
        : passes(groups, point.coordinate);
      const cell = inCell ? inCell(point.coordinate) : true;
      const spared = selected || searched;
      const culled = (groups.size > 0 && !passesHighlights && !spared) ||
        (Boolean(inCell) && !cell && !spared);
      const hidden = !onShard || filteredHidden || culled;
      const promoted = !hidden &&
        (selected || point.id === hovered || searched || (Boolean(inCell) && cell));
      standings.push({ filteredHidden, passesHighlights, inCell: cell, searched, hidden, promoted });
      if (!hidden) eligible++;
      if (!filteredHidden && passesHighlights) focused++;
      if (!filteredHidden && inCell && cell) priority++;
    }
    this.points = standings;
    this.eligible = eligible;
    this.drawnPoints = eligible;
    this.focusedPins = focused;
    this.priorityPins = scene.gridCell ? priority : 0;

    // A shape answers the legend and the shard and nothing else. Highlighting
    // narrows which points stand rather than which ground is drawn, and
    // searching for a place has never taken the ground out from under it.
    this.shapesShown = model.shapes.filter((shape) =>
      !scene.hidden.has(String(shape.collection.id)) && onActiveShard(shape.shard, shard));
    this.drawn = eligible + this.shapesShown.length;
    this.listable = eligible + this.shapesShown.filter((shape) =>
      shape.title && (!search || shape.title.toLocaleLowerCase().includes(search))).length;
  }

  /** The standing of one point, by its place in the model's registry. */
  at(index: number): PointStanding {
    return this.points[index] ?? {
      filteredHidden: true, passesHighlights: false, inCell: false,
      searched: false, hidden: true, promoted: false,
    };
  }

  /** Every point the chart is drawing, in the model's own order. */
  *standing(): Generator<PointRecord> {
    for (let i = 0; i < this.model.points.length; i++) {
      const point = this.model.points[i];
      if (point && !this.at(i).hidden) yield point;
    }
  }
}

/** Whether a feature on this shard belongs to the layer the lens draws. */
export function onActiveShard(featureShard: number, lensShard: number): boolean {
  return !lensShard || !featureShard || featureShard === lensShard;
}

function passes(groups: ReadonlyMap<number, ShapeRecord[]>, at: Coordinate): boolean {
  for (const shapes of groups.values()) {
    if (!shapes.some((shape) => shapeContains(shape, at))) return false;
  }
  return true;
}

/**
 * Containment with a unit of grace.
 *
 * A pin dropped on a boundary was put there to mean the place, and exact
 * point-in-polygon arithmetic would flip it out over the width of the line it
 * stands on. A path claims what lies within its own declared ground width for
 * the same reason: a trail is a line and a weight.
 */
export function shapeContains(shape: ShapeRecord, at: Coordinate): boolean {
  const grace = shape.kind === "path"
    ? Math.max(1, Number(shape.collection.attrs?.["atlas.stroke.width_px"] ?? 0) / 2)
    : 1;
  for (let i = 0; i < shape.lines.length; i++) {
    const outer = shape.lines[i];
    if (!outer) continue;
    if (shape.kind === "area") {
      if (inRing(outer, at)) {
        const holes = shape.holes[i] ?? [];
        if (!holes.some((hole) => inRing(hole, at))) return true;
      }
    }
    if (nearLine(outer, at, grace)) return true;
  }
  return false;
}

/** The even-odd crossing test, which is also how the scrims cut their holes. */
function inRing(ring: Line, [x, y]: Coordinate): boolean {
  let inside = false;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const a = ring[i];
    const b = ring[j];
    if (!a || !b) continue;
    const crosses = a[1] > y !== b[1] > y &&
      x < ((b[0] - a[0]) * (y - a[1])) / (b[1] - a[1]) + a[0];
    if (crosses) inside = !inside;
  }
  return inside;
}

function nearLine(line: Line, at: Coordinate, grace: number): boolean {
  const limit = grace * grace;
  for (let i = 1; i < line.length; i++) {
    const a = line[i - 1];
    const b = line[i];
    if (!a || !b) continue;
    if (segmentDistanceSquared(a, b, at) <= limit) return true;
  }
  return false;
}

function segmentDistanceSquared(a: Coordinate, b: Coordinate, at: Coordinate): number {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const length = dx * dx + dy * dy;
  const t = length === 0 ? 0 : Math.min(1, Math.max(0,
    ((at[0] - a[0]) * dx + (at[1] - a[1]) * dy) / length));
  const x = a[0] + t * dx - at[0];
  const y = a[1] + t * dy - at[1];
  return x * x + y * y;
}
