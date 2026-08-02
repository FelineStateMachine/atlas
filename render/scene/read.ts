// The scene description, read.
//
// `#atlas-viewport-state` is an inert node the server renders and the seam
// observes: data attributes for the singular facts, `<data>` children for the
// three lists (docs/app.md §4). It is the whole of what the application tells
// the seam, and **data flows one way** — nothing here writes to it, and the
// only things that ever travel the other way are the pick event and the
// debounced camera report.
//
// This module is deliberately a pure function of a node-shaped thing. It
// takes the smallest structural interface that a real `Element` satisfies, so
// the reader can be tested over a synthetic node in Node with no DOM at all,
// and so nothing about parsing a scene depends on being in a browser.
//
// Reading is lenient in the format's own spirit: a missing attribute is its
// empty value, a malformed number is as good as absent, and an unknown
// attribute is ignored. A scene that arrives half-written must draw the half
// it can rather than throw a page away.

/** The smallest shape a scene can be read out of. `Element` satisfies it. */
export interface AttributeSource {
  getAttribute(name: string): string | null;
}

/** The state node, structurally. `Element` satisfies this too. */
export interface StateNode extends AttributeSource {
  querySelectorAll(selectors: string): Iterable<AttributeSource>;
}

/** The camera the session handed back, in world coordinates. */
export interface Camera {
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
  readonly rotation: number;
}

/** A reader-set label policy, keyed by collection id as a string. */
export type LabelPolicy = "always" | "quiet";

/** Everything the application says about what to picture. */
export interface Scene {
  readonly volume: string;
  readonly base: string;
  readonly world: string;
  readonly lens: string;
  readonly lensIndex: number;
  readonly surface: "plane" | "sphere";
  readonly selected: string;
  readonly search: string;
  readonly gridSystem: string;
  readonly gridCell: string;
  readonly subgrid: number;
  readonly hidden: ReadonlySet<string>;
  readonly highlighted: ReadonlySet<string>;
  readonly overrides: ReadonlyMap<string, LabelPolicy>;
  readonly camera: Camera | null;
}

/** The scene of a page with no volume open: everything empty, nothing drawn. */
export const EMPTY_SCENE: Scene = {
  volume: "", base: "", world: "", lens: "", lensIndex: 0, surface: "plane",
  selected: "", search: "", gridSystem: "", gridCell: "", subgrid: 0,
  hidden: new Set(), highlighted: new Set(), overrides: new Map(), camera: null,
};

function attribute(node: AttributeSource, name: string): string {
  return node.getAttribute(name) ?? "";
}

function whole(node: AttributeSource, name: string): number {
  const value = Number(attribute(node, name));
  return Number.isFinite(value) ? value : 0;
}

function values(node: StateNode, className: string): string[] {
  const found: string[] = [];
  for (const child of node.querySelectorAll(`data.${className}`)) {
    const value = child.getAttribute("value");
    if (value) found.push(value);
  }
  return found;
}

/**
 * The camera attribute: `x,y,zoom,rotation`.
 *
 * Absent until the seam has reported one (docs/app.md §6.1), which is the
 * difference between "open this volume where you left it" and "open it at the
 * fit". Anything that does not parse as four numbers is treated as absent,
 * because a camera half-read is a camera pointed somewhere nobody chose.
 */
export function readCamera(value: string): Camera | null {
  if (!value) return null;
  const parts = value.split(",").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isFinite(part))) return null;
  const [x, y, zoom, rotation] = parts as [number, number, number, number];
  return { x, y, zoom, rotation };
}

/** The scene as one node says it. */
export function readScene(node: StateNode | null): Scene {
  if (!node) return EMPTY_SCENE;
  const overrides = new Map<string, LabelPolicy>();
  for (const entry of values(node, "label-override")) {
    const cut = entry.lastIndexOf("=");
    if (cut <= 0) continue;
    const policy = entry.slice(cut + 1);
    if (policy === "always" || policy === "quiet") {
      overrides.set(entry.slice(0, cut), policy);
    }
  }
  return {
    volume: attribute(node, "data-volume"),
    base: attribute(node, "data-base"),
    world: attribute(node, "data-world"),
    lens: attribute(node, "data-lens"),
    lensIndex: whole(node, "data-lens-index"),
    surface: attribute(node, "data-surface") === "sphere" ? "sphere" : "plane",
    selected: attribute(node, "data-selected"),
    search: attribute(node, "data-search"),
    gridSystem: attribute(node, "data-grid-system"),
    gridCell: attribute(node, "data-grid-cell"),
    subgrid: whole(node, "data-subgrid"),
    hidden: new Set(values(node, "hidden-collection")),
    highlighted: new Set(values(node, "highlighted-feature")),
    overrides,
    camera: readCamera(attribute(node, "data-camera")),
  };
}

function sameSet(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}

function sameMap(
  a: ReadonlyMap<string, LabelPolicy>,
  b: ReadonlyMap<string, LabelPolicy>,
): boolean {
  if (a.size !== b.size) return false;
  for (const [key, value] of a) if (b.get(key) !== value) return false;
  return true;
}

/**
 * What moved between two scenes.
 *
 * The seam reconciles rather than rebuilds: a swap that only turned a
 * collection off must not refetch a payload, and a swap that changed the
 * world must not keep the pins of the last one. Naming the differences is how
 * that stays honest — every consumer asks this rather than comparing
 * attributes for itself.
 */
export interface SceneChange {
  readonly volume: boolean;
  readonly world: boolean;
  readonly lens: boolean;
  readonly filters: boolean;
  readonly selection: boolean;
  readonly grid: boolean;
  readonly camera: boolean;
  readonly any: boolean;
}

/** Compare two scenes, field by field, in the terms the seam acts on. */
export function sceneChange(was: Scene, now: Scene): SceneChange {
  const volume = was.volume !== now.volume || was.base !== now.base;
  const world = volume || was.world !== now.world;
  const lens = world || was.lens !== now.lens || was.lensIndex !== now.lensIndex;
  const filters = !sameSet(was.hidden, now.hidden) ||
    !sameSet(was.highlighted, now.highlighted) ||
    !sameMap(was.overrides, now.overrides) ||
    was.search !== now.search;
  const selection = was.selected !== now.selected;
  const grid = was.gridSystem !== now.gridSystem || was.gridCell !== now.gridCell ||
    was.subgrid !== now.subgrid;
  const camera = was.camera?.x !== now.camera?.x || was.camera?.y !== now.camera?.y ||
    was.camera?.zoom !== now.camera?.zoom || was.camera?.rotation !== now.camera?.rotation;
  return {
    volume, world, lens, filters, selection, grid, camera,
    any: volume || world || lens || filters || selection || grid || camera ||
      was.surface !== now.surface,
  };
}
