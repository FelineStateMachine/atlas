// The sphere's skin, and the one rule a pass that can be interrupted owes.
//
// `Skin` composites the base skin into one texture, once per lens and never
// again. The pass walks tiles one image at a time and awaits each, so it can
// be standing between two awaits when the lens changes or the sphere is put
// away -- and the whole of this file is the consequence of that: a pass that
// was cancelled must leave the texture asking for its composite again rather
// than claiming to hold it.
//
// The defect these tests are the record of: a key committed before the paint
// meant an abandoned pass was remembered as painted, so nothing ever asked
// for it again and the sphere came up black and stayed black.
//
// Tiles are delivered by hand here. `Image` is stubbed so that every request
// is parked until a test says it may arrive, which is the only way to be
// standing between two awaits when the interruption lands.

import test from "node:test";
import { strict as assert } from "node:assert";
import type { Lens, TileGrid } from "../data/payload.ts";
import { setLevel } from "../log.ts";

setLevel("error");

// ---- the host the skin is composited in ------------------------------

/** One image the seam has asked for and not yet been given. */
interface Parked {
  readonly url: string;
  readonly arrive: () => void;
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
    parked.push({ url: value, arrive: () => this.onload?.() });
  }
}

/** Everything drawn onto the texture, in order, by the URL it came from. */
const drawn: string[] = [];
/** Which parked image a `drawImage` belongs to: the one just handed over. */
let arriving = "";

const paper = {
  imageSmoothingEnabled: false,
  clearRect: () => { drawn.length = 0; },
  drawImage: () => { drawn.push(arriving); },
};

const canvas = { width: 0, height: 0, getContext: () => paper };

(globalThis as unknown as { document: unknown }).document = {
  createElement: () => canvas,
};
(globalThis as unknown as { Image: unknown }).Image = StubImage;

// Imported after the host exists: the module itself touches neither, but a
// reader should not have to know that to trust the file.
const { Skin } = await import("../globe/texture.ts");

/** Let the microtasks behind a delivered image run to the next request. */
function settle(): Promise<void> {
  return new Promise((resolve) => { setTimeout(resolve, 0); });
}

/** Hand over every image asked for, and every image those ask for in turn. */
async function deliverAll(): Promise<void> {
  for (let guard = 0; guard < 200 && parked.length; guard++) {
    const next = parked.shift();
    if (!next) break;
    arriving = next.url;
    next.arrive();
    await settle();
  }
}

// ---- the pyramid the tests composite ---------------------------------

const grid: TileGrid = { sourceZoom: 0, firstTile: 0, tileSize: 256, size: 1024 };

function lensNamed(tiles: string): Lens {
  return {
    name: tiles,
    tiles,
    minZoom: 0,
    maxZoom: 4,
    // `fullZoom: 1` puts the base level at 1, which is four tiles of the
    // declared window -- enough for "interrupted half way" to mean something
    // and few enough to deliver one at a time.
    fullZoom: 1,
    sourceZoom: 0,
    formats: ["png"],
    interpolate: true,
    bounds: { x: 0, y: 0, width: 1024, height: 1024 },
  };
}

const alpha = lensNamed("alpha");
const beta = lensNamed("beta");

function urls(tag: string) {
  return (z: number, x: number, y: number): string => `${tag}/${z}/${x}/${y}`;
}

function fresh() {
  parked.length = 0;
  drawn.length = 0;
  return new Skin({ x: 0, y: 0, width: 1024, height: 1024 }, grid);
}

const nothing = () => {};

/** The four tiles of the base level, in the order the window walks them. */
function baseTiles(tag: string): string[] {
  return [`${tag}/1/0/0`, `${tag}/1/0/1`, `${tag}/1/1/0`, `${tag}/1/1/1`];
}

// ---- the skin is composited once, and kept --------------------------

test("the base skin arrives whole", async () => {
  const skin = fresh();
  const painting = skin.base("v1", alpha, urls("alpha"), nothing);
  await settle();
  assert.equal(parked.length, 1, "the base pass is suspended on its first tile");
  await deliverAll();
  await painting;
  assert.deepEqual(drawn, baseTiles("alpha"), "the whole base skin arrived");
  assert.equal(skin.lens, "alpha");
});

test("a base skin that did arrive is not composited a second time", async () => {
  const skin = fresh();
  const first = skin.base("v1", alpha, urls("alpha"), nothing);
  await deliverAll();
  await first;
  assert.equal(drawn.length, 4);

  // Leave, and come back: `show` asks for the same lens again, and the skin
  // is on the texture and is left there. Recompositing it is the expensive
  // half of coming back, and this is why coming back does not pay it.
  await skin.base("v1", alpha, urls("alpha"), nothing);
  assert.equal(parked.length, 0, "no tile was asked for");
  assert.equal(drawn.length, 4, "and nothing was drawn again");
});

test("a base skin cancelled by a whole-skin clear is asked for again", async () => {
  const skin = fresh();
  const abandoned = skin.base("v1", alpha, urls("alpha"), nothing);
  await settle();
  skin.clear();
  await deliverAll();
  await abandoned;
  assert.deepEqual(drawn, [], "the cancelled pass drew nothing after the clear");

  // The key was never committed, so the very same request repaints.
  const retry = skin.base("v1", alpha, urls("alpha"), nothing);
  await deliverAll();
  await retry;
  assert.deepEqual(drawn, baseTiles("alpha"));
});

test("a newer base skin supersedes the one in flight, and it is the one kept", async () => {
  const skin = fresh();
  const stale = skin.base("v1", alpha, urls("alpha"), nothing);
  await settle();
  const current = skin.base("v1", beta, urls("beta"), nothing);
  await deliverAll();
  await Promise.all([stale, current]);
  assert.deepEqual(drawn, baseTiles("beta"), "only the newer lens is on the texture");
  assert.equal(skin.lens, "beta");

  // The lens that was abandoned is not remembered as painted: going back to
  // it composites it, rather than trusting a texture it never finished.
  const back = skin.base("v1", alpha, urls("alpha"), nothing);
  await deliverAll();
  await back;
  assert.deepEqual(drawn, baseTiles("alpha"));
});
