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
// The ladder is a pure function of a collection, a scene and a pin, so these
// are table-driven over a synthetic world: no fixtures, no canvas, no map.

import test from "node:test";
import { strict as assert } from "node:assert";
import type Circle from "ol/style/Circle.js";
import { EMPTY_SCENE } from "../scene/read.ts";
import type { LabelPolicy, Scene } from "../scene/read.ts";
import { WorldModel } from "../world/model.ts";
import type { PointRecord } from "../world/model.ts";
import { Visibility } from "../world/visibility.ts";
import type { Collection, TileGrid } from "../data/payload.ts";
import { OUTSET_COLORS, Styles, curatedPointPolicy, outsetColor } from "../chart/styles.ts";

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
const COLLECTIONS = [PLAIN, SPOKEN, QUIETED, TEXT];

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
    iconURL: (asset: string) => asset,
  });
  built.learn(COLLECTIONS);
  return built;
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
  const built = styles({});
  assert.equal(built.pin(pin(TEXT), false).length, built.pin(pin(PLAIN), false).length);
  assert.ok(built.pin(pin(TEXT), false)[0]?.getImage());
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
    const built = styles({}, false, null, token);
    const image = built.pin(pin(PLAIN), false)[0]?.getImage() as Circle | undefined;
    assert.equal(image?.getStroke()?.getColor(), color);
  }
});
