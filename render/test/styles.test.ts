// Which names the chart says out loud, and what its markers are rimmed with.
//
// Two decisions in `chart/styles.ts` are contracts rather than taste, and
// both were quietly wrong before this suite existed.
//
// THE LABEL LADDER. A point collection's names are curated, overridden and
// revealed in that order — the reader's override wins, then the producer's
// `atlas.label.policy`, then the kind's own default, which for points is
// silence unless the curation asked for text. Over the policy sit exactly two
// states: the pin being pointed at and the pin whose card is open. Promotion
// is not one of them, and that is the whole of the defect this suite pins
// down: a search promotes every pin it matches so the declutterer cannot drop
// one, and a renderer that read promotion as "say its name" answered a search
// for a place with ten copies of the same word piled on one another.
//
// THE OUTSET. `atlas.icon.outset` arrives as a token — `light` or `dark` —
// and a marker is rimmed with the colour that token names, never with the
// word itself.
//
// THE MARK. A marker is the collection's symbol carrying the collection's
// colour, with an outset cut to the symbol's own shape: never a coloured
// bubble with a symbol punched out of it, which is what this pane drew until
// the reader said so. What that raster is made of is `markers.ts`'s and is
// tested there; what is here is what the chart does with it — the size, the
// stacking, and the initials it draws in the window before it exists.
//
// The ladder is a pure function of a collection, a scene and a pin, so those
// cases are table-driven over a synthetic world: no fixtures, no map. The
// mark needs a canvas and an image, and both are stubbed by hand below —
// images are parked until a test hands them over, which is the only way to
// say anything about that window.

import test from "node:test";
import { strict as assert } from "node:assert";
import type Icon from "ol/style/Icon.js";
import { EMPTY_SCENE } from "../scene/read.ts";
import type { LabelPolicy, Scene } from "../scene/read.ts";
import { WorldModel } from "../world/model.ts";
import type { PointRecord } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import type { Collection, TileGrid } from "../data/payload.ts";
import { Styles, curatedPointPolicy } from "../chart/styles.ts";
import { OUTSET_COLORS, forgetMarkerRasters, outsetColor } from "../chart/markers.ts";

// ---- the page a raster is composed on --------------------------------

/** One image the seam has asked for and not yet been given. */
interface Parked {
  readonly url: string;
  readonly load: () => void;
}

const parked: Parked[] = [];

const host = globalThis as unknown as Record<string, unknown>;
host.document = {
  createElement: () => ({
    width: 0,
    height: 0,
    getContext: () => ({ drawImage: () => {}, fillRect: () => {} }),
    toDataURL: () => "data:image/png;base64,raster",
  }),
};
// OpenLayers asks whether an icon's image is an `HTMLImageElement` before it
// decides how to size it, so the stub has to be one.
host.HTMLImageElement = class {
  // The raster this stands for is 64 square, which is what the sizes below
  // are read against: an icon drawn at 31 is that raster at 31/64.
  width = 64;
  height = 64;
};
host.Image = class extends (host.HTMLImageElement as new () => object) {
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  crossOrigin = "";
  private asked = "";

  get src(): string {
    return this.asked;
  }

  set src(value: string) {
    this.asked = value;
    // A data URL is the composed raster coming back to be measured, not an
    // asset being fetched: it is not something a test hands over.
    if (value.startsWith("data:")) return;
    parked.push({ url: value, load: () => this.onload?.() });
  }
};

/** Let the microtasks behind a delivered image run. */
function settle(): Promise<void> {
  return new Promise((resolve) => { setTimeout(resolve, 0); });
}

const grid: TileGrid = { sourceZoom: 5, firstTile: 0, tileSize: 256, size: 8192 };

/** Four point collections, one for each rung the ladder can start on. */
const PLAIN: Collection = { id: 1, title: "Settlements", kind: "point", visible: true };
const SPOKEN: Collection = {
  id: 2, title: "Cities", kind: "point", visible: true,
  attrs: { "atlas.label.policy": "always" },
};
const QUIETED: Collection = {
  id: 3, title: "Caves", kind: "point", visible: true,
  attrs: { "atlas.label.policy": "quiet" },
};
const TEXT: Collection = {
  id: 4, title: "Regions", kind: "point", visible: true,
  attrs: { "atlas.render.as": "text" },
};

/**
 * Two collections drawn the same way, which is one raster.
 *
 * Shrine and Daedric Shrine: one asset in one colour, so the symbol is
 * composed once and only one of them ever asked for it.
 */
const SHRINES: Collection = {
  id: 5, title: "Shrines", kind: "point", visible: true,
  iconAsset: "shrine.svg", color: "#4fb3d5",
};
const DAEDRIC: Collection = {
  id: 6, title: "Daedric Shrines", kind: "point", visible: true,
  iconAsset: "shrine.svg", color: "#4fb3d5",
};
const COLLECTIONS = [PLAIN, SPOKEN, QUIETED, TEXT, SHRINES, DAEDRIC];

/** One pin, standing wherever; only its title, id and collection matter here. */
function pin(collection: Collection, id = "7", title = "Goodsprings"): PointRecord {
  return {
    id, index: 0, title, collection,
    coordinate: [0, 0], member: 0, shard: 0, priority: 42,
  };
}

function overrides(collection: Collection, policy?: LabelPolicy) {
  const map = new Map<string, LabelPolicy>();
  if (policy) map.set(String(collection.id), policy);
  return map;
}

/** The chart's styles over an empty world, with the scene the case describes. */
function styles(
  scene: Partial<Scene>,
  labelsHeld = false,
  hovered: string | null = null,
  outset = "dark",
  repaint?: () => void,
): Styles {
  const model = new WorldModel(
    "world", { lenses: [], collections: COLLECTIONS }, grid, null);
  const full: Scene = { ...EMPTY_SCENE, ...scene };
  const built = new Styles({
    visibility: new Visibility(model, full, 0, null, hovered),
    scene: full,
    labelsHeld,
    hovered,
    outset,
    iconURL: (asset: string) => `/icons/${asset}`,
    ...(repaint ? { repaint } : {}),
  });
  built.learn(COLLECTIONS);
  return built;
}

/** A world whose symbols have not arrived, and the ask that stands parked. */
function waiting(
  scene: Partial<Scene> = {}, outset = "dark", repaint?: () => void,
): Styles {
  forgetMarkerRasters();
  parked.length = 0;
  return styles(scene, false, null, outset, repaint);
}

// ---- the ladder -------------------------------------------------------

interface LadderCase {
  readonly name: string;
  readonly collection: Collection;
  readonly override?: LabelPolicy;
  readonly held?: boolean;
  readonly hovered?: boolean;
  readonly selected?: boolean;
  readonly promoted?: boolean;
  readonly labelled: boolean;
}

const LADDER: readonly LadderCase[] = [
  // The producer's word, with nobody touching anything.
  { name: "a curated-always collection speaks unasked", collection: SPOKEN, labelled: true },
  { name: "a plain pin collection waits to be asked", collection: PLAIN, labelled: false },
  { name: "a curated-quiet collection waits too", collection: QUIETED, labelled: false },
  {
    name: "a collection curated as text speaks unasked",
    collection: TEXT, labelled: true,
  },

  // Z reveals what is optional.
  { name: "Z reveals a plain collection's names", collection: PLAIN, held: true, labelled: true },
  {
    name: "Z reveals a curated-quiet collection's names",
    collection: QUIETED, held: true, labelled: true,
  },

  // The reader's override wins, and Z never overrules a silencing.
  {
    name: "an override of quiet silences a collection curated always",
    collection: SPOKEN, override: "quiet", labelled: false,
  },
  {
    name: "Z does not revive what the reader silenced by hand",
    collection: QUIETED, override: "quiet", held: true, labelled: false,
  },
  {
    name: "Z does not revive a silenced text collection either",
    collection: TEXT, override: "quiet", held: true, labelled: false,
  },
  {
    name: "an override of always speaks with no key held",
    collection: PLAIN, override: "always", labelled: true,
  },

  // Promotion places a label; it never asks for one.
  {
    name: "a searched pin is promoted and not labelled",
    collection: PLAIN, promoted: true, labelled: false,
  },
  {
    name: "a searched pin of a silenced collection is still not labelled",
    collection: SPOKEN, override: "quiet", promoted: true, labelled: false,
  },
  {
    name: "a promoted pin of a speaking collection keeps its name",
    collection: SPOKEN, promoted: true, labelled: true,
  },

  // Pointing at a pin, and opening its card, speak over everything.
  { name: "the hovered pin is named", collection: PLAIN, hovered: true, labelled: true },
  { name: "the selected pin is named", collection: PLAIN, selected: true, labelled: true },
  {
    name: "the hovered pin is named even where the reader silenced its collection",
    collection: QUIETED, override: "quiet", hovered: true, labelled: true,
  },
  {
    name: "the selected pin is named even where the reader silenced its collection",
    collection: QUIETED, override: "quiet", selected: true, labelled: true,
  },
];

for (const item of LADDER) {
  test(`pin label: ${item.name}`, () => {
    const id = "7";
    const style = styles(
      {
        selected: item.selected ? id : "",
        overrides: overrides(item.collection, item.override),
      },
      item.held ?? false,
      item.hovered ? id : null,
    ).pinLabel(pin(item.collection, id), item.promoted ?? false);
    assert.equal(style !== null, item.labelled);
    if (style) assert.equal(style.getText()?.getText(), "Goodsprings");
  });
}

test("a pin with no name draws none, however loudly its collection speaks", () => {
  const built = styles({ selected: "7" }, true);
  assert.equal(built.pinLabel(pin(SPOKEN, "7", ""), true), null);
});

test("a search that matches ten places labels none of them", () => {
  // The defect as it was seen: ten pins matching one word, every one of them
  // promoted past the declutterer, and a pile of names over a single town.
  const built = styles({ search: "goodsprings", overrides: overrides(PLAIN) });
  const drawn = Array.from({ length: 10 }, (_, i) => pin(PLAIN, String(i)))
    .map((record) => built.pinLabel(record, true))
    .filter((style) => style !== null);
  assert.equal(drawn.length, 0);
});

test("promotion still decides where a label is drawn and whether it declutters", () => {
  const built = styles({ overrides: overrides(SPOKEN) });
  const plain = built.pinLabel(pin(SPOKEN), false);
  const promoted = built.pinLabel(pin(SPOKEN), true);
  assert.equal(plain?.getZIndex(), 42);
  assert.equal(plain?.getText()?.getDeclutterMode(), "declutter");
  assert.equal(promoted?.getZIndex(), 9_100_000);
  assert.equal(promoted?.getText()?.getDeclutterMode(), "none");
});

test("a collection curated as text draws an ordinary marker", () => {
  // `atlas.render.as` survives only as the default its policy falls back to:
  // the text-only symbol it used to name was never drawn.
  const built = waiting();
  assert.equal(built.pin(pin(TEXT), false).length, built.pin(pin(PLAIN), false).length);
  assert.ok(built.pin(pin(TEXT), false)[0]?.getText(), "which is a mark like any other");
});

test("the curated default of a point collection is silence unless it is text", () => {
  assert.equal(curatedPointPolicy(PLAIN), "quiet");
  assert.equal(curatedPointPolicy(SPOKEN), "always");
  assert.equal(curatedPointPolicy(QUIETED), "quiet");
  assert.equal(curatedPointPolicy(TEXT), "always");
});

// ---- the outset -------------------------------------------------------

test("an outset token names a colour, and anything but dark reads light", () => {
  assert.equal(outsetColor("dark"), OUTSET_COLORS.dark);
  assert.equal(outsetColor("light"), OUTSET_COLORS.light);
  assert.equal(outsetColor(""), OUTSET_COLORS.light);
  assert.equal(outsetColor("sepia"), OUTSET_COLORS.light);
});

test("a marker is rimmed with the outset's colour, never with its token", () => {
  for (const [token, color] of [["dark", OUTSET_COLORS.dark], ["light", OUTSET_COLORS.light],
    ["", OUTSET_COLORS.light]] as const) {
    const built = waiting({}, token);
    const text = built.pin(pin(PLAIN), false)[0]?.getText();
    assert.equal(text?.getStroke()?.getColor(), color);
  }
});

// ---- the mark ---------------------------------------------------------

test("a marker is the symbol itself: no disc, no second style", async () => {
  // THE DEFECT, as the reader saw it: "icons are rendered as coloured bubbles
  // with an outset and a black symbol inside the bubble". A pin was a filled
  // circle plus an untinted 15px icon over it; it is one style now, and that
  // style is the symbol.
  const built = waiting();
  parked[0]?.load();
  await settle();
  const marks = built.pin(pin(SHRINES), false);
  assert.equal(marks.length, 1, "one style, not a disc and a glyph over it");
  const icon = marks[0]?.getImage() as Icon | undefined;
  assert.ok(icon, "and the style's image is the composed raster");
  assert.equal(icon?.getSrc(), "data:image/png;base64,raster");
  assert.equal(marks[0]?.getText(), null, "with no separate glyph beside it");
});

test("one symbol is composed for every collection drawn that way", async () => {
  const built = waiting();
  assert.equal(parked.length, 1,
    "Shrine and Daedric Shrine are one asset in one colour, so one raster");
  parked[0]?.load();
  await settle();
  const shrine = built.pin(pin(SHRINES), false)[0]?.getImage() as Icon | undefined;
  const daedric = built.pin(pin(DAEDRIC), false)[0]?.getImage() as Icon | undefined;
  assert.equal(shrine?.getSrc(), daedric?.getSrc());
});

test("the chosen pin is the larger mark, and the one drawn over the rest", async () => {
  const built = waiting({ selected: "7" });
  parked[0]?.load();
  await settle();
  const chosen = built.pin(pin(SHRINES, "7"), true)[0];
  const beside = built.pin(pin(SHRINES, "8"), false)[0];
  // The raster is 64 square, so an icon drawn at 31 is that raster scaled by
  // 31/64: the scale is the size, said the way OpenLayers says it before the
  // image it will measure has loaded.
  assert.deepEqual((chosen?.getImage() as Icon).getScale(), [36 / 64, 36 / 64]);
  assert.deepEqual((beside?.getImage() as Icon).getScale(), [31 / 64, 31 / 64]);
  assert.equal(chosen?.getZIndex(), 20_000_000, "the open card's pin is over everything");
  assert.equal(beside?.getZIndex(), 42, "and every other pin keeps its place in the crowd");
});

test("a promoted pin that is not the chosen one is not grown", async () => {
  // A search promotes every pin it matched so the declutterer cannot drop
  // one; growing all of those would say a hundred places had been chosen.
  const built = waiting({ selected: "7" });
  parked[0]?.load();
  await settle();
  const promoted = built.pin(pin(SHRINES, "9"), true)[0];
  assert.deepEqual((promoted?.getImage() as Icon).getScale(), [31 / 64, 31 / 64]);
  assert.equal(promoted?.getZIndex(), 42);
});

test("until the symbol arrives the pin wears its collection's initials", () => {
  const built = waiting();
  const text = built.pin(pin(SHRINES), false)[0]?.getText();
  assert.equal(text?.getText(), "S", "the first letters of the first two words");
  assert.equal(text?.getFont(), "900 13px Inter, ui-sans-serif, system-ui, sans-serif");
  assert.equal(text?.getFill()?.getColor(), "#4fb3d5", "in the collection's own colour");
  assert.equal(text?.getStroke()?.getColor(), OUTSET_COLORS.dark);
  assert.equal(text?.getStroke()?.getWidth(), 3);
  assert.equal(built.pin(pin(SHRINES), false)[0]?.getImage(), null, "and no disc under it");
});

test("the chosen pin's initials are the larger of the two as well", () => {
  const built = waiting({ selected: "7" });
  const text = built.pin(pin(SHRINES, "7"), true)[0]?.getText();
  assert.equal(text?.getFont(), "900 15px Inter, ui-sans-serif, system-ui, sans-serif");
  assert.equal(text?.getStroke()?.getWidth(), 4);
});

test("a collection with no artwork wears its initials for good", async () => {
  const built = waiting();
  parked[0]?.load();
  await settle();
  const text = built.pin(pin(PLAIN), false)[0]?.getText();
  assert.equal(text?.getText(), "S", "Settlements");
  assert.equal(built.pin(pin(TEXT), false)[0]?.getText()?.getText(), "R", "Regions");
});

test("a colour that would vanish into the rim is taken off it", () => {
  // The ladder is `markers.ts`'s and is tested there; what is checked here is
  // that the chart climbs it — a near-black collection on a world with dark
  // rims is drawn in a lifted shade of itself rather than in the dark.
  const DIM: Collection = {
    id: 9, title: "Caverns", kind: "point", visible: true, color: "#0d1014",
  };
  const model = new WorldModel(
    "world", { lenses: [], collections: [DIM] }, grid, null);
  const scene: Scene = { ...EMPTY_SCENE };
  const built = new Styles({
    visibility: new Visibility(model, scene, 0, null, null),
    scene,
    labelsHeld: false,
    hovered: null,
    outset: "dark",
    iconURL: (asset: string) => asset,
  });
  built.learn([DIM]);
  assert.notEqual(built.pin(pin(DIM), false)[0]?.getText()?.getFill()?.getColor(), "#0d1014");
  assert.equal(built.color(DIM), "#0d1014",
    "and everything else the collection draws keeps the colour it declared");
});

test("a symbol arriving drops every mark and asks the pane to draw again", async () => {
  let pokes = 0;
  const built = waiting({}, "dark", () => { pokes++; });
  const shrine = built.pin(pin(SHRINES), false)[0];
  const daedric = built.pin(pin(DAEDRIC), false)[0];
  assert.ok(shrine?.getText() && daedric?.getText(), "both cached their initials");
  parked[0]?.load();
  await settle();
  assert.equal(pokes, 1, "once per raster, not once per collection wearing it");
  assert.ok(built.pin(pin(SHRINES), false)[0]?.getImage(), "the collection that asked is redrawn");
  assert.ok(built.pin(pin(DAEDRIC), false)[0]?.getImage(),
    "and so is the one that cached its initials while the raster was loading");
});

test("a mark is built once per collection and state, not once per pin", () => {
  const built = waiting();
  const first = built.pin(pin(SHRINES, "1"), false)[0];
  assert.equal(built.pin(pin(SHRINES, "2"), false)[0], first,
    "two thousand pins of twenty collections are forty styles");
  assert.notEqual(built.pin(pin(PLAIN, "3"), false)[0], first);
});
