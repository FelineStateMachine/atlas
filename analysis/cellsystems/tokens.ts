// The grid's one visual vocabulary, as pure tokens.
//
// Issue #5 §5.4: outputs are renderer-neutral artifacts — plans and pure style
// tokens. These numbers live in the analysis lane because both projections
// must render from the same ones or they drift apart: line weight encodes the
// hierarchy the way an MGRS grid weighs its own, the chosen path draws white
// over per-cell palette accents, and the label is a small chip spelling the
// address with its prefix faint and its final character bright.
//
// Nothing here is a colour a renderer chose. The chart adapts these into
// ol/style and the globe into materials and sprites; neither holds a width or
// a colour of its own.

/**
 * The accent wheel. Grid cells, zones, and categories without an archive
 * colour draw from it: mid-tone hues anchored on the identity's cerulean and
 * earths, bright enough to read over world tiles and muted enough not to shout
 * over the neutral chrome.
 */
export const palette: readonly string[] = [
  "#4fb3d5", "#c9924b", "#82b56a", "#c96a6a", "#9581cc",
  "#4bc9a9", "#d4b04a", "#6a92c9", "#b08a5a", "#8fb3a2",
];

/** The role a cell plays in one plan. */
export type CellRole = "neighbor" | "scope" | "child" | "leaf";

/** Every weight, alpha and chip the grid draws with. */
export interface GridTheme {
  readonly labelSizePx: number;
  readonly neighborLabelSizePx: number;
  readonly labelInsetPx: number;
  readonly labelFont: string;
  readonly prefixAlpha: number;
  readonly chip: string;
  readonly neighborChip: string;
  readonly lineWhite: string;
  readonly widths: Readonly<Record<CellRole | "scopeBare", number>>;
  readonly childLineAlpha: number;
  readonly neighborLineAlpha: number;
  readonly leafFillAlpha: number;
  readonly childFillAlpha: number;
  readonly neighborTextAlpha: number;
  readonly dimColor: string;
  readonly dimBase: number;
  readonly dimStep: number;
  readonly dimCap: number;
}

export const gridTheme: GridTheme = {
  labelSizePx: 11,
  neighborLabelSizePx: 10,
  labelInsetPx: 5,
  labelFont: "ui-monospace, SFMono-Regular, Menlo, monospace",
  prefixAlpha: 0.55,
  chip: "rgba(12, 15, 22, 0.76)",
  neighborChip: "rgba(8, 11, 18, 0.88)",
  lineWhite: "#ffffff",
  widths: { leaf: 2.5, scope: 2.5, scopeBare: 1.8, child: 1.4, neighbor: 1 },
  childLineAlpha: 0.82,
  neighborLineAlpha: 0.44,
  leafFillAlpha: 0.14,
  childFillAlpha: 0.055,
  neighborTextAlpha: 0.72,
  dimColor: "#050810",
  dimBase: 0.3,
  dimStep: 0.06,
  dimCap: 0.52,
};

/** The accent a cell wears everywhere it is drawn. */
export function paletteColor(index: number): string {
  const color = palette[((index % palette.length) + palette.length) % palette.length];
  // The wheel is a non-empty constant, so this is unreachable; the fallback
  // exists because `noUncheckedIndexedAccess` is right to insist.
  return color ?? gridTheme.lineWhite;
}
