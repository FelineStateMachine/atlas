// What a marker is made of.
//
// THE DEFECT THIS SUITE IS THE RECORD OF: markers were drawn as coloured
// bubbles — a filled disc in the collection's colour, rimmed with the world's
// outset, with a black symbol punched out of the middle. Every collection
// therefore said the same thing at a glance ("a coloured bubble"), and the
// one drawing that told a Shrine from a Landing Site was the small dark shape
// inside it. The mark is supposed to be the symbol itself: the colour is
// carried by the glyph, and what makes it legible over the art beneath is an
// outset cut to the glyph's own shape.
//
// So the composition is the contract, and it is three canvases:
//
//   the TINTED glyph — the symbol, filled with the collection's colour, and
//   only when it is a glyph: a picture already carries its own colours;
//   the OUTLINE — the same symbol, filled with the world's rim colour;
//   the RASTER — the outline stamped in a disc of 37 offsets, and the tinted
//   glyph over the top of it, in that order. Stamping the other way round
//   would leave every symbol looking eroded at its edges.
//
// The canvas is stubbed rather than mocked away: every operation is recorded
// with the state it was performed under, so the tests can say what was drawn,
// in what order, in what colour and under which composite mode. Images are
// parked until a test hands them over, which is the only way to say anything
// about the window before a raster exists.

import test from "node:test";
import { strict as assert } from "node:assert";

// ---- the page a raster is composed on --------------------------------

/** One call made on a stub canvas, with the state it was made under. */
interface Op {
  readonly name: string;
  readonly args: readonly unknown[];
  readonly fill: string;
  readonly mode: string;
  readonly smoothing: boolean | undefined;
}

/** Every canvas the module has minted, in the order it minted them. */
const canvases: StubCanvas[] = [];

class StubCanvas {
  width = 0;
  height = 0;
  readonly ops: Op[] = [];
  readonly paper: Record<string, unknown>;

  constructor() {
    const record = (name: string) => (...args: unknown[]): void => {
      this.ops.push({
        name,
        args,
        fill: String(this.paper.fillStyle),
        mode: String(this.paper.globalCompositeOperation),
        smoothing: this.paper.imageSmoothingEnabled as boolean | undefined,
      });
    };
    this.paper = {
      fillStyle: "",
      globalCompositeOperation: "source-over",
      imageSmoothingEnabled: undefined,
      drawImage: record("drawImage"),
      fillRect: record("fillRect"),
    };
  }

  getContext(): Record<string, unknown> {
    return this.paper;
  }

  toDataURL(): string {
    return `data:image/png;base64,canvas-${canvases.indexOf(this)}`;
  }

  drawn(name: string): readonly Op[] {
    return this.ops.filter((op) => op.name === name);
  }
}

/** One image the seam has asked for and not yet been given. */
interface Parked {
  readonly url: string;
  readonly load: () => void;
  readonly fail: () => void;
}

const parked: Parked[] = [];

class StubImage {
  crossOrigin = "";
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private asked = "";

  get src(): string {
    return this.asked;
  }

  set src(value: string) {
    this.asked = value;
    parked.push({
      url: value,
      load: () => this.onload?.(),
      fail: () => this.onerror?.(),
    });
  }
}

const host = globalThis as unknown as Record<string, unknown>;
host.document = {
  createElement: (): StubCanvas => {
    const canvas = new StubCanvas();
    canvases.push(canvas);
    return canvas;
  },
};
host.Image = StubImage;

const {
  composeMarker, forgetMarkerRasters, initialsOf, legibleIconColor, markerKey,
  markerRaster, markerRasterReady, outsetColor, relativeLuminance, withLightness,
} = await import("../chart/markers.ts");
type MarkerFace = import("../chart/markers.ts").MarkerFace;

const { iconURL } = await import("../data/plane.ts");

const DARK_RIM = outsetColor("dark");

function face(over: Partial<MarkerFace> = {}): MarkerFace {
  return {
    asset: "shrine.svg",
    url: "/data/v/skyrim/abc123abc123/icons/shrine.svg",
    picture: false,
    color: "#4fb3d5",
    outset: DARK_RIM,
    ...over,
  };
}

/** Let the microtasks behind a delivered image run. */
function settle(): Promise<void> {
  return new Promise((resolve) => { setTimeout(resolve, 0); });
}

/** Compose one raster from a fresh page, and hand back the three canvases. */
async function compose(over: Partial<MarkerFace> = {}): Promise<{
  raster: string | null;
  target: StubCanvas;
  tinted: StubCanvas;
  outline: StubCanvas;
}> {
  forgetMarkerRasters();
  canvases.length = 0;
  parked.length = 0;
  const asking = markerRasterReady(face(over));
  parked[0]?.load();
  const raster = await asking;
  const [target, tinted, outline] = canvases;
  assert.ok(target && tinted && outline, "a raster is composed on three canvases");
  return { raster, target, tinted, outline };
}

// ---- the raster ------------------------------------------------------

test("a raster is 64 square, and the glyph sits inset inside it", async () => {
  const { target, tinted } = await compose();
  for (const canvas of canvases) {
    assert.equal(canvas.width, 64);
    assert.equal(canvas.height, 64);
  }
  const [glyph] = tinted.drawn("drawImage");
  assert.deepEqual(glyph?.args.slice(1), [6, 6, 52, 52],
    "six of inset on every side, which is where the halo goes");
  assert.equal(target.width, 64);
});

test("the halo goes down first and the symbol over it", async () => {
  const { target, tinted, outline } = await compose();
  const stamps = target.drawn("drawImage");
  assert.equal(stamps.at(-1)?.args[0], tinted,
    "the tinted glyph is the last thing drawn — a symbol over its rim, not under it");
  for (const stamp of stamps.slice(0, -1)) {
    assert.equal(stamp.args[0], outline, "everything before it is the rim");
  }
  assert.deepEqual(stamps.at(-1)?.args.slice(1), [0, 0], "and it is drawn square on");
});

test("the halo is a disc: thirty-seven stamps, none further than three", async () => {
  const { target, outline } = await compose();
  const stamps = target.drawn("drawImage").filter((op) => op.args[0] === outline);
  assert.equal(stamps.length, 37, "a disc of radius √10, not a 7×7 square of 49");
  const offsets = stamps.map(
    (op): readonly [number, number] => [op.args[1] as number, op.args[2] as number]);
  for (const [x, y] of offsets) {
    assert.ok(x * x + y * y <= 10, `(${x}, ${y}) is inside the disc`);
    assert.ok(Math.abs(x) <= 3 && Math.abs(y) <= 3);
  }
  assert.equal(new Set(offsets.map(String)).size, 37, "every offset is stamped once");
  // The corners are what a square would have added: at three out in both
  // directions the rim would reach 4.2 pixels on the diagonal and 3 on the
  // straight, which reads as a lozenge rather than a halo.
  assert.equal(offsets.some(([x, y]) => Math.abs(x) === 3 && Math.abs(y) === 3), false);
});

test("a glyph is filled with its collection's colour, and smoothed on the way", async () => {
  const { tinted } = await compose({ color: "#c9924b" });
  const [drawn] = tinted.drawn("drawImage");
  assert.equal(drawn?.smoothing, true, "a silhouette is scaled smoothly");
  const [filled] = tinted.drawn("fillRect");
  assert.equal(filled?.mode, "source-in", "the fill keeps the shape and replaces its pixels");
  assert.equal(filled?.fill, "#c9924b");
  assert.deepEqual(filled?.args, [0, 0, 64, 64], "over the whole raster");
});

test("a picture keeps its own colours and its own pixels", async () => {
  const { tinted } = await compose({ picture: true });
  const [drawn] = tinted.drawn("drawImage");
  assert.equal(drawn?.smoothing, false, "a picture is scaled nearest-neighbour");
  assert.equal(tinted.drawn("fillRect").length, 0,
    "flattening a picture to one colour would leave nothing but its outline filled in");
});

test("the rim is the world's outset, whatever the symbol is", async () => {
  for (const picture of [false, true]) {
    const { outline } = await compose({ picture, outset: DARK_RIM });
    const [filled] = outline.drawn("fillRect");
    assert.equal(filled?.mode, "source-in");
    assert.equal(filled?.fill, DARK_RIM, "a picture wears a halo too");
  }
});

test("a composed raster is a data URL of the raster canvas, not of a part of it", async () => {
  const { raster, target } = await compose();
  assert.equal(raster, target.toDataURL());
});

test("a page with no canvas composes nothing rather than throwing", () => {
  const page = host.document;
  host.document = { createElement: () => ({ getContext: () => null }) };
  try {
    assert.equal(composeMarker({} as CanvasImageSource, face()), "");
  } finally {
    host.document = page;
  }
});

// ---- the cache -------------------------------------------------------

test("a raster is keyed by asset, colour and rim — not by collection", () => {
  const key = markerKey(face());
  assert.equal(markerKey(face()), key, "two collections drawn the same way share one raster");
  assert.notEqual(markerKey(face({ color: "#c9924b" })), key);
  assert.notEqual(markerKey(face({ outset: outsetColor("light") })), key);
  assert.notEqual(markerKey(face({ asset: "cave.svg" })), key);
});

test("one asset is fetched once, however many collections ask for it", async () => {
  forgetMarkerRasters();
  parked.length = 0;
  const first = markerRasterReady(face());
  const second = markerRasterReady(face());
  assert.equal(parked.length, 1, "the second ask joins the first rather than fetching again");
  parked[0]?.load();
  assert.equal(await first, await second, "and they are handed the same raster");
  assert.equal(markerRaster(face()), await first, "which is there for a style function to read");
});

test("until the symbol arrives there is no raster to draw", () => {
  forgetMarkerRasters();
  parked.length = 0;
  void markerRasterReady(face());
  assert.equal(markerRaster(face()), null, "the pin wears its initials in the meantime");
});

test("a symbol that never arrives is not asked for again", async () => {
  forgetMarkerRasters();
  parked.length = 0;
  const asking = markerRasterReady(face());
  parked[0]?.fail();
  assert.equal(await asking, null);
  assert.equal(markerRaster(face()), null, "the initials become permanent");
  await markerRasterReady(face());
  assert.equal(parked.length, 1, "a 404 is remembered as firmly as a picture");
});

test("a collection with no artwork asks for nothing at all", async () => {
  forgetMarkerRasters();
  parked.length = 0;
  assert.equal(await markerRasterReady(face({ asset: "", url: "" })), null);
  assert.equal(markerRaster(face({ asset: "", url: "" })), null);
  assert.equal(parked.length, 0);
});

test("the symbol is asked for over CORS, because the raster is exported", async () => {
  forgetMarkerRasters();
  parked.length = 0;
  void markerRasterReady(face());
  // A canvas tainted by an image fetched without CORS cannot be exported at
  // all, and exporting it is the whole of what this module does.
  assert.equal(parked[0]?.url, face().url);
  await settle();
});

// ---- the colour ladder -----------------------------------------------

test("brightness is the reference's own ungamma'd weights", () => {
  assert.equal(relativeLuminance("#000000"), 0);
  assert.equal(relativeLuminance("#ffffff"), 1);
  assert.ok(Math.abs(relativeLuminance("#00ff00") - 0.7152) < 1e-9);
  assert.equal(relativeLuminance("rgb(0, 0, 0)"), 0.5, "a colour we cannot take apart is a shrug");
});

interface LadderCase {
  readonly name: string;
  readonly color: string;
  readonly outset: string;
  readonly kept: boolean;
}

const LADDER: readonly LadderCase[] = [
  {
    name: "a mid colour stands against a dark rim",
    color: "#4fb3d5", outset: "dark", kept: true,
  },
  {
    name: "a near-black colour is lifted off a dark rim",
    color: "#101418", outset: "dark", kept: false,
  },
  {
    name: "a mid colour stands against a light rim",
    color: "#4fb3d5", outset: "light", kept: true,
  },
  {
    name: "a near-white colour is dropped against a light rim",
    color: "#fafafa", outset: "light", kept: false,
  },
  {
    name: "an unknown outset reads light, like everywhere else",
    color: "#fafafa", outset: "sepia", kept: false,
  },
  {
    name: "and a mid colour survives an unknown outset too",
    color: "#4fb3d5", outset: "", kept: true,
  },
];

for (const item of LADDER) {
  test(`legible colour: ${item.name}`, () => {
    const drawn = legibleIconColor(item.color, item.outset);
    assert.equal(drawn === item.color, item.kept);
    if (item.kept) return;
    const lifted = item.outset === "dark";
    assert.ok(lifted
      ? relativeLuminance(drawn) > relativeLuminance(item.color)
      : relativeLuminance(drawn) < relativeLuminance(item.color),
    "the colour moved the way the rim needed it to");
  });
}

test("a lifted colour keeps its hue, so the legend still matches the map", () => {
  // 0.74 and 0.42 are the two lightnesses the reference lifts and drops to.
  assert.equal(withLightness("#0a3d5c", 0.74), "#87caf2");
  assert.equal(withLightness("#0a3d5c", 0.42), "#1580c1");
  assert.equal(withLightness("#808080", 0.74), "#bdbdbd", "grey has no hue to keep");
  assert.equal(withLightness("rgb(1,2,3)", 0.5), "rgb(1,2,3)", "and what cannot be read is kept");
});

// ---- the fallback and the URL ----------------------------------------

test("initials are the first letters of the first two words", () => {
  assert.equal(initialsOf("Impact Craters"), "IC");
  assert.equal(initialsOf("Landing Sites and Rovers"), "LS");
  assert.equal(initialsOf("Volcanoes"), "V");
  assert.equal(initialsOf(""), "");
});

test("an asset path is encoded segment by segment", () => {
  const base = "/data/v/neon-harbor/abc123abc123";
  assert.equal(iconURL(base, "ward.svg"), `${base}/icons/ward.svg`);
  assert.equal(iconURL(base, "signs/Ward 101 (Ext).png"),
    `${base}/icons/signs/Ward%20101%20(Ext).png`,
    "the separators stay separators and everything else goes on the wire encoded");
});
