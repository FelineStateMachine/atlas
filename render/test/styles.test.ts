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
// THE TEXT FORM. A point collection curated `atlas.render.as: text` is
// written on the map rather than pinned to it: one style, which is its name,
// and no marker under it. This departs from the reference on purpose — the
// reference drew a marker for every point collection, so a text collection
// wore its symbol and its name at once — and the cases below are the whole of
// the rule: no mark, a centred name in the collection's own colour and rim, a
// step of growth under the pointer, and the marker back again for the one
// reader who silences the collection by hand, so that nothing can be made to
// vanish from the map while its box is still ticked.
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
import { Styles, curatedPointPolicy, featureColor } from "../chart/styles.ts";
import type { ShapeRecord } from "../world/model.ts";
import {
  OUTSET_COLORS, forgetMarkerRasters, legibleIconColor, outsetColor,
} from "../chart/markers.ts";

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
  color: "#2c3f52",
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

/**
 * A text collection that was given artwork anyway, which really happens.
 *
 * The captured game volumes shipped region collections carrying a glyph
 * (`province.svg` and the like) *and* a text curation: the curation's word
 * about the form wins over the artwork it happens to have, or the change
 * would do nothing at all on the volumes that asked for it.
 */
const LETTERED: Collection = {
  id: 7, title: "Provinces", kind: "point", visible: true,
  iconAsset: "province.svg", color: "#2c3f52",
  attrs: { "atlas.render.as": "text" },
};
const COLLECTIONS = [PLAIN, SPOKEN, QUIETED, TEXT, SHRINES, DAEDRIC, LETTERED];

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
  assert.equal(parked.filter((one) => one.url.endsWith("shrine.svg")).length, 1,
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
  assert.equal(built.pin(pin(QUIETED), false)[0]?.getText()?.getText(), "C", "Caves");
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

// ---- the text form -----------------------------------------------------

/** The colour a text mark is written in: the collection's, off the rim. */
const TEXT_COLOR = legibleIconColor("#2c3f52", "dark");

test("a collection curated as text draws its name and nothing else", () => {
  // THE RULE, in the reader's words: "the map will display text as just the
  // text by default rather than 'A - Wellspring of Wisdom'". The initials and
  // the always-on name were two drawings of one place.
  const built = waiting();
  assert.deepEqual(built.pin(pin(TEXT), false), [], "no mark of any kind under the name");
  const mark = built.pinLabel(pin(TEXT, "7", "Wellspring of Wisdom"), false);
  assert.ok(mark, "and the name is what there is");
  assert.equal(mark?.getText()?.getText(), "Wellspring of Wisdom");
  assert.equal(mark?.getImage(), null, "no disc, no icon");
});

test("artwork does not buy a text collection a marker back", async () => {
  // A region collection carrying a glyph and curated as text: the curation
  // decides the form, or the rule changes nothing where it was asked for.
  // The raster still composes -- the reader may yet ask for markers -- and
  // it is still not drawn.
  const built = waiting();
  for (const one of parked) one.load();
  await settle();
  assert.deepEqual(built.pin(pin(LETTERED), false), []);
  assert.ok(built.pinLabel(pin(LETTERED, "7", "Necluda"), false));
});

test("the text mark is the text-symbol's own face, in the collection's colour", () => {
  const built = waiting();
  const text = built.pinLabel(pin(TEXT, "7", "Necluda"), false)?.getText();
  assert.equal(text?.getFont(),
    '900 11px "Arial Narrow", "Roboto Condensed", ui-sans-serif, system-ui, sans-serif');
  assert.equal(text?.getFill()?.getColor(), TEXT_COLOR,
    "the collection's own colour, taken off the rim it is drawn against");
  assert.equal(text?.getStroke()?.getColor(), OUTSET_COLORS.dark);
  assert.equal(text?.getStroke()?.getWidth(), 3);
});

test("a text mark stands on its place rather than beside it", () => {
  // The label's sixteen pixels are the room a marker takes up. With no marker
  // there is nothing to stand off from, and a province written below the
  // point it names is a province in the wrong place.
  const built = waiting();
  assert.equal(built.pinLabel(pin(TEXT), false)?.getText()?.getOffsetY(), 0);
});

test("an ordinary label still stands off from the marker it belongs to", () => {
  const built = styles({ overrides: overrides(SPOKEN) });
  assert.equal(built.pinLabel(pin(SPOKEN), false)?.getText()?.getOffsetY(), 16);
});

for (const attended of ["hovered", "selected"] as const) {
  test(`a ${attended} text mark grows a step, as a marker does`, () => {
    const id = "7";
    const built = attended === "hovered"
      ? styles({}, false, id)
      : styles({ selected: id });
    const text = built.pinLabel(pin(TEXT, id), attended === "selected")?.getText();
    assert.equal(text?.getFont(),
      '900 13px "Arial Narrow", "Roboto Condensed", ui-sans-serif, system-ui, sans-serif');
    assert.equal(text?.getStroke()?.getWidth(), 4);
  });
}

test("the text mark of the open card is drawn over everything", () => {
  const built = styles({ selected: "7" });
  assert.equal(built.pinLabel(pin(TEXT, "7"), true)?.getZIndex(), 20_000_000);
  const beside = built.pinLabel(pin(TEXT, "8"), true);
  assert.equal(beside?.getZIndex(), 9_100_000, "a promoted one is placed, not chosen");
  assert.equal(beside?.getText()?.getDeclutterMode(), "none");
  assert.equal(built.pinLabel(pin(TEXT, "9"), false)?.getZIndex(), 42);
  assert.equal(built.pinLabel(pin(TEXT, "9"), false)?.getText()?.getDeclutterMode(), "declutter",
    "and one in the crowd gives way to a rarer collection's, as any name does");
});

test("a reader who silences a text collection gets its markers back", () => {
  // THE ONE JUDGEMENT THE RULE NEEDS. Silencing a collection silences its
  // names, and for a collection whose name is its only form that would erase
  // the feature outright: a box still ticked, a place still counted, and
  // nothing on the map. So the override chooses between the two forms a point
  // collection can take -- which is what the reference's own key called that
  // toggle -- rather than between a form and nothing.
  const built = waiting({ overrides: overrides(TEXT, "quiet") });
  assert.equal(built.pinLabel(pin(TEXT), false), null, "the names are silenced");
  const mark = built.pin(pin(TEXT), false)[0];
  assert.equal(mark?.getText()?.getText(), "R", "and the ordinary marker stands in their place");
  assert.equal(built.pin(pin(TEXT), false).length, 1);
});

test("a place with no name to be written as keeps its marker", () => {
  // The other half of the same rule: there is no text to draw a nameless
  // feature as, and a feature drawn as nothing is a feature the reader can
  // neither see nor point at.
  const built = waiting();
  const nameless = pin(TEXT, "7", "");
  assert.equal(built.pinLabel(nameless, false), null);
  assert.equal(built.pin(nameless, false)[0]?.getText()?.getText(), "R");
});

test("a silenced text collection still answers the pointer, and with a marker beside", () => {
  // Hover and selection speak over the silencing for every other collection;
  // the marker is the form either way, so the name comes back beside it.
  const id = "7";
  const built = waiting({ overrides: overrides(TEXT, "quiet") }, "dark");
  assert.equal(built.pin(pin(TEXT, id), false).length, 1);
  const hovering = styles({ overrides: overrides(TEXT, "quiet") }, false, id);
  const label = hovering.pinLabel(pin(TEXT, id), false);
  assert.equal(label?.getText()?.getOffsetY(), 16, "an ordinary name, beside an ordinary mark");
});

test("everything else keeps the mark it had", async () => {
  const built = waiting();
  parked[0]?.load();
  await settle();
  assert.equal(built.pin(pin(SHRINES), false).length, 1);
  assert.ok(built.pin(pin(SHRINES), false)[0]?.getImage(), "artwork collections are unchanged");
  assert.equal(built.pin(pin(PLAIN), false).length, 1);
  assert.equal(built.pin(pin(SPOKEN), false)[0]?.getText()?.getText(), "C", "Cities");
});

// ---- the shapes' own colours ------------------------------------------

/** One zone, standing wherever; only its id and collection matter here. */
function zone(collection: Collection, id: string): ShapeRecord {
  return {
    id, title: "", subtitle: "", collection, kind: "area", shard: 0,
    lines: [], holes: [], center: null, feature: { id } as never,
  };
}

test("a shape wears its feature's colour, which is the index's own", () => {
  // The server's colorFor (internal/app/legend.go): Knuth's multiplicative
  // hash over the id, mod the ten-colour wheel. By hand: 1496244488 ·
  // 2654435761 mod 2^32 mod 10 = 2, and 39191589 lands on 1 -- so the two
  // zones wear the wheel's third and second colours, exactly as their index
  // rows do, and an id that is not a number wears the wheel's first.
  assert.equal(featureColor("1496244488"), "#82b56a");
  assert.equal(featureColor("39191589"), "#c9924b");
  assert.equal(featureColor("zoning-b1"), "#4fb3d5");

  // A zoning collection is one row with one declared colour, but B-1 and
  // O-1 are different grounds: the map draws each in its feature's colour,
  // never the collection's.
  const zoning: Collection = {
    id: 9, title: "Zoning", kind: "area", visible: true, color: "#00ff00",
  };
  const built = styles({});
  const first = built.area(zone(zoning, "1496244488"), false);
  const second = built.area(zone(zoning, "39191589"), false);
  assert.equal(first.getStroke()?.getColor(), "#82b56a");
  assert.equal(second.getStroke()?.getColor(), "#c9924b");
  assert.notEqual(first.getStroke()?.getColor(), zoning.color);

  // A path is the same rule at a different width.
  const trails: Collection = { id: 4, title: "Trails", kind: "path", visible: true };
  const [, ink] = built.path({ ...zone(trails, "1496244488"), kind: "path" }, 2, false);
  assert.equal(ink?.getStroke()?.getColor(), "#82b56a");
});
