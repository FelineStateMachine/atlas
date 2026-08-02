// The one styling of a grid cell, as pure tokens.
//
// What its boundary weighs, what fills it, what its corner label says. The
// chart adapts these into ol/style and the globe into materials and sprites;
// neither holds a colour or a width of its own, so the two projections read as
// one instrument (issue #5 §5.4: renderers adapt tokens; no renderer owns a
// cell rule).
//
// Two questions come from the renderer's side rather than the plan's:
// whether the subdivision is showing (session state), and whether the label
// fits where it would be drawn (a measurement only a renderer can make). A
// child cell too small for its label draws **nothing at all** — the
// subdivision appears at the size it can be read, on both panes at the same
// visual moment — which is why null is a real answer here.

import type { CellSystem } from "./contract.ts";
import type { Ground } from "./ground.ts";
import type { PlanCell } from "./plan.ts";
import { gridTheme, paletteColor } from "./tokens.ts";

/** The two things only the renderer knows. */
export interface VisualOptions {
  /** Whether the held cell's subdivision is showing. */
  readonly subgridVisible: boolean;
  /** Whether the label fits where this cell would carry it. */
  readonly labelled: boolean;
}

export interface LineTokens {
  readonly color: string;
  readonly opacity: number;
  readonly widthPx: number;
}

export interface FillTokens {
  readonly color: string;
  readonly opacity: number;
}

export interface LabelTokens {
  /** The faint cut: everything before the principal character. */
  readonly prefix: string;
  /** The bright cut: the principal digit of the address. */
  readonly final: string;
  readonly color: string;
  readonly textAlpha: number;
  readonly chip: string;
  readonly sizePx: number;
}

export interface CellVisual {
  readonly line: LineTokens;
  readonly fill: FillTokens | null;
  readonly label: LabelTokens | null;
  /** The held cell with its subdivision showing: an outline and nothing else. */
  readonly bare: boolean;
}

/**
 * The style tokens for one plan cell, or null when the cell draws nothing.
 *
 * Pure: the same cell, options and ground always answer the same tokens, and
 * no token is a renderer's unit but the pixel widths, which both panes agree
 * on because they read them from here.
 */
export function gridCellVisual(
  ground: Ground,
  system: CellSystem,
  cell: PlanCell,
  { subgridVisible, labelled }: VisualOptions,
): CellVisual | null {
  const { role, contextDistance } = cell;
  if (role === "child" && (!labelled || !subgridVisible)) return null;
  const on = system.on(ground);
  const color = paletteColor(on.colorKey(cell.hash));
  const chosen = role === "leaf" || role === "scope";
  const bare = role === "scope" && subgridVisible;

  const line: LineTokens = {
    color: chosen ? gridTheme.lineWhite : color,
    opacity: role === "neighbor"
      ? gridTheme.neighborLineAlpha
      : role === "child" ? gridTheme.childLineAlpha : 1,
    widthPx: bare ? gridTheme.widths.scopeBare : gridTheme.widths[role],
  };

  let fill: FillTokens | null = null;
  if (role === "neighbor") {
    fill = {
      color: gridTheme.dimColor,
      opacity: Math.min(
        gridTheme.dimCap,
        gridTheme.dimBase + contextDistance * gridTheme.dimStep,
      ),
    };
  } else if (role === "leaf") {
    fill = { color, opacity: gridTheme.leafFillAlpha };
  } else if (role === "child") {
    fill = { color, opacity: gridTheme.childFillAlpha };
  }

  let label: LabelTokens | null = null;
  if (labelled && !bare) {
    const { context, principal } = on.label(cell.hash);
    label = {
      prefix: context,
      final: principal,
      color: chosen ? gridTheme.lineWhite : color,
      textAlpha: role === "neighbor" ? gridTheme.neighborTextAlpha : 1,
      chip: role === "neighbor" ? gridTheme.neighborChip : gridTheme.chip,
      sizePx: role === "neighbor" ? gridTheme.neighborLabelSizePx : gridTheme.labelSizePx,
    };
  }
  return { line, fill, label, bare };
}
