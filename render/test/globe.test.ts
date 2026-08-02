// What the sphere raises over a pin, and how it decides.
//
// Five decisions in `globe/element.ts` are contracts rather than taste, and
// every one of them was wrong before this suite existed.
//
// THE CARD IS CUT TO THE NAME. A label is measured in the font it is drawn
// in, and the canvas, the texture and the sprite's aspect all follow that one
// number. A fixed 256x64 canvas clipped every name longer than it, and a
// fixed sprite scale then stretched whatever survived to the same width — so
// two names of very different lengths came out identical, and neither was the
// size it asked for.
//
// THE CAMERA SPENDS THE BUDGET. 180 names is a decision about a sphere; which
// 180 is a decision about a reader. Ordering by collection priority hands
// every card to whichever collection happens to be rarest, wherever in the
// world it stands, and leaves the ground under the camera bare.
//
// THE HORIZON IS ENFORCED BY HAND. Cards and chips draw with the depth test
// off, so nothing stops a name on the far side of the planet shining straight
// through it but the limb test here.
//
// A MATERIAL IS SHARED. Two thousand pins of twenty collections are forty
// materials, cached by marker and selection, never disposed — and the ringed
// variant is a different entry, not a mutation of the plain one.
//
// A REBUILD GIVES BACK WHAT IT MINTED. Dropping a card out of a group is not
// freeing its texture, and the names are rebuilt every time the camera
// settles somewhere new.
//
// Everything below is a pure function of its arguments, over a stub canvas
// that records what was drawn on it: no WebGL, no globe, no fixtures.

import test from "node:test";
import { strict as assert } from "node:assert";
// `three` reads nothing of the page as it loads, so it is imported the plain
// way; `globe.gl`, which the seam pulls in, reads the document as it loads,
// which is why the module under test arrives by hand further down.
import * as THREE from "three";
import { setLevel } from "../log.ts";

setLevel("error");

// ---- the page the sphere is drawn in ---------------------------------

/** One call made on a stub canvas, so a test can say what was drawn. */
interface Op {
  readonly name: string;
  readonly args: readonly unknown[];
}

/** The pitch a font is measured at: enough that a longer name is wider. */
function pitch(font: string): number {
  return Number(/(\d+)px/.exec(font)?.[1] ?? 10) * 0.5;
}

interface StubCanvas {
  width: number;
  height: number;
  readonly ops: Op[];
  getContext(): Record<string, unknown>;
}

function stubCanvas(): StubCanvas {
  const ops: Op[] = [];
  const paper: Record<string, unknown> = {
    font: "10px", textAlign: "", textBaseline: "", lineWidth: 0,
    fillStyle: "", strokeStyle: "", globalAlpha: 1, globalCompositeOperation: "",
    measureText: (text: string) => ({ width: text.length * pitch(String(paper.font)) }),
  };
  for (const name of ["fillRect", "fillText", "strokeText", "drawImage",
    "beginPath", "arc", "stroke", "clearRect"]) {
    paper[name] = (...args: unknown[]) => { ops.push({ name, args }); };
  }
  return {
    width: 0, height: 0, ops,
    getContext: () => paper,
    style: {}, classList: { add: () => {} },
    appendChild: () => {}, insertBefore: () => {},
    setAttribute: () => {}, addEventListener: () => {},
  } as unknown as StubCanvas;
}

const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class {};
host.window = globalThis;
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
host.Image = class { src = ""; crossOrigin = ""; };
host.document = {
  createElement: stubCanvas,
  createElementNS: stubCanvas,
  createTextNode: stubCanvas,
  getElementsByTagName: () => [stubCanvas()],
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener: () => {},
  head: stubCanvas(),
  body: stubCanvas(),
};

// Imported after the page exists: `globe.gl` reads the document as it loads.
const {
  angularDistance, facesCamera, initialsOf, labelCandidates, markerMaterial,
  nameCard, release, wearSkin,
} = await import("../globe/element.ts");
type Placed = import("../globe/element.ts").Placed;

/** The canvas a sprite's texture was drawn on, with what was drawn on it. */
function drawnOn(sprite: THREE.Sprite): StubCanvas {
  const map = (sprite.material as THREE.SpriteMaterial).map;
  assert.ok(map, "the sprite carries a texture");
  return map.image as StubCanvas;
}

const somewhere = { x: 0, y: 0, z: 101 };

// ---- the card is cut to the name -------------------------------------

test("a name's card is measured in the font it is written in", () => {
  const short = nameCard("Elysium", somewhere);
  const canvas = drawnOn(short);
  // "600 26px Inter" measures at 13 per character here, and the card carries
  // 24 pixels of padding around whatever that came to.
  assert.equal(canvas.width, "Elysium".length * 13 + 24, "the canvas is the name plus its padding");
  assert.equal(canvas.height, 40, "and a fixed 40 tall");
  const filled = canvas.ops.filter((op) => op.name === "fillRect");
  assert.deepEqual(filled[0]?.args, [0, 0, canvas.width, 40], "the card is filled edge to edge");
  const written = canvas.ops.find((op) => op.name === "fillText");
  assert.deepEqual(written?.args, ["Elysium", canvas.width / 2, 21], "centred on its own width");
});

test("a longer name gets a wider card and a wider sprite, in proportion", () => {
  const short = nameCard("Elysium", somewhere);
  const long = nameCard("Olympus Mons and the Tharsis Rise", somewhere);
  for (const card of [short, long]) {
    const canvas = drawnOn(card);
    assert.equal(card.scale.y, 0.028, "every card is the same height on screen");
    assert.equal(card.scale.x, (0.028 * canvas.width) / 40, "and as wide as its own canvas");
  }
  assert.ok(long.scale.x > short.scale.x * 2, "the longer name is much the wider");
  // The defect this pins down: one fixed canvas and one fixed scale made
  // these two identical, and clipped the longer one to fit.
  assert.notEqual(short.scale.x, long.scale.x);
});

test("a card floats above its pin and draws over the planet", () => {
  const card = nameCard("Hellas", somewhere);
  const material = card.material as THREE.SpriteMaterial;
  assert.equal(material.depthTest, false, "the card is not clipped by the curve it stands on");
  assert.equal(material.depthWrite, false);
  assert.equal(material.sizeAttenuation, false, "screen-sized, not world-sized");
  assert.equal(material.transparent, true);
  assert.equal(card.renderOrder, 4);
  assert.deepEqual([card.center.x, card.center.y], [0.5, 0], "anchored by its bottom edge");
  assert.deepEqual([card.position.x, card.position.y, card.position.z], [0, 0, 101]);
});

// ---- the camera spends the budget ------------------------------------

test("the great-circle distance is what near means on a sphere", () => {
  assert.equal(angularDistance({ lat: 0, lng: 0 }, { lat: 0, lng: 0 }), 0);
  assert.ok(Math.abs(angularDistance({ lat: 0, lng: 0 }, { lat: 0, lng: 90 }) - 90) < 1e-9);
  assert.ok(Math.abs(angularDistance({ lat: 90, lng: 0 }, { lat: -90, lng: 0 }) - 180) < 1e-9);
  // Two places a hundred and seventy degrees of longitude apart, at the
  // eightieth parallel, are nine degrees apart on the ground. Comparing
  // latitudes and longitudes would call that most of a planet.
  assert.ok(angularDistance({ lat: 80, lng: -85 }, { lat: 80, lng: 85 }) < 35);
});

test("the names raised are the nearest ones, in order, out to the rim", () => {
  const placed: Placed[] = [
    { title: "far side", lat: 0, lng: 179 },
    { title: "third", lat: 0, lng: 30 },
    { title: "over the rim", lat: 0, lng: 86 },
    { title: "first", lat: 0, lng: 2 },
    { title: "at the rim", lat: 0, lng: 84.9 },
    { title: "second", lat: 0, lng: -10 },
  ];
  const raised = labelCandidates({ lat: 0, lng: 0 }, placed, 180);
  assert.deepEqual(
    raised.map((entry) => entry.title),
    ["first", "second", "third", "at the rim"],
    "ascending by distance, and nothing past eighty-five degrees");
});

test("the budget is spent on the nearest names and stops there", () => {
  // Two hundred pins along the equator, the farthest of them still inside the
  // reach, handed over in the worst possible order.
  const placed: Placed[] = [];
  for (let at = 200; at > 0; at--) {
    placed.push({ title: `pin ${at}`, lat: 0, lng: at * 0.4 });
  }
  const raised = labelCandidates({ lat: 0, lng: 0 }, placed, 180);
  assert.equal(raised.length, 180, "the budget is the budget");
  assert.equal(raised[0]?.title, "pin 1", "and it is spent nearest first");
  assert.equal(raised[179]?.title, "pin 180");
  // Which is the whole defect: in the order they arrived, the 180 kept would
  // have been the twenty nearest and a hundred and sixty of the farthest.
  assert.ok(!raised.some((entry) => entry.title === "pin 181"));
});

test("a budget larger than the standing set raises everything inside the reach", () => {
  const placed: Placed[] = [
    { title: "here", lat: 10, lng: 10 },
    { title: "behind", lat: -10, lng: -170 },
  ];
  assert.deepEqual(
    labelCandidates({ lat: 10, lng: 10 }, placed, 180).map((entry) => entry.title),
    ["here"]);
});

// ---- the horizon is enforced by hand ---------------------------------

test("a card on the near side faces the camera and one on the far side does not", () => {
  // A camera three radii out: the limb sits where the cosine of a point's
  // angle from the camera's axis is a third.
  const camera = { x: 0, y: 0, z: 300 };
  assert.equal(facesCamera({ x: 0, y: 0, z: 100 }, camera), true, "straight ahead");
  assert.equal(facesCamera({ x: 0, y: 0, z: -100 }, camera), false, "straight behind");
  assert.equal(facesCamera({ x: 100, y: 0, z: 0 }, camera), false, "square on the rim");
});

test("the limb itself is the far side, and a hair inside it is not", () => {
  const camera = { x: 0, y: 0, z: 300 };
  const at = (cosine: number) => ({
    x: 100 * Math.sqrt(1 - cosine ** 2), y: 0, z: 100 * cosine,
  });
  assert.equal(facesCamera(at(1 / 3), camera), false, "exactly at the limb is not visible");
  assert.equal(facesCamera(at(1 / 3 + 1e-6), camera), true, "a hair inside it is");
  assert.equal(facesCamera(at(1 / 3 - 1e-6), camera), false, "a hair outside it is not");
});

test("the closer the camera comes the less of the planet it can see", () => {
  const anchor = { x: 100 * Math.sin(0.9), y: 0, z: 100 * Math.cos(0.9) };
  assert.equal(facesCamera(anchor, { x: 0, y: 0, z: 400 }), true, "from far out, visible");
  assert.equal(facesCamera(anchor, { x: 0, y: 0, z: 120 }), false, "from close in, over the rim");
});

// ---- the skin is worn, not multiplied away ---------------------------
//
// The sphere is the one surface in this lane whose whole picture can be wrong
// with every number about it right, and this is how it went wrong: globe.gl
// hands out a material tinted black to say "no picture yet", and a Phong
// material multiplies its map by its tint. The seam assigned the map and left
// the tint, so six baselines' worth of globe steps were correct about a black
// planet. The test is the multiplication, stated: after the skin is on, the
// tint must be identity.

test("the sphere's skin is hung on a material that does not tint it away", () => {
  // What globe.gl hands out when nothing has given it an image: the comment in
  // its own source is "Black globe if no image".
  const material = new THREE.MeshPhongMaterial({ color: 0x000000 });
  const skin = new THREE.CanvasTexture(stubCanvas() as unknown as HTMLCanvasElement);
  wearSkin(material, skin);
  assert.equal(material.map, skin, "the skin is not the material's map");
  assert.equal(material.color.getHex(), 0xffffff,
    "the material multiplies its map by its colour, and this colour is not identity" +
    " — a black planet with every count right is what that looks like");
  // `needsUpdate` is write-only on a three material — it bumps `version` and
  // keeps nothing — so the version is what says the shader was recompiled.
  assert.equal(material.version, 1, "the material was not told to recompile");
});

// ---- a material is shared --------------------------------------------

const mark = {
  icon: "", picture: false, color: "#4fb3d5",
  outset: "rgba(7, 9, 7, 0.98)", title: "Impact Craters",
};

test("a collection's marker is built once and shared by every pin wearing it", () => {
  const first = markerMaterial(mark, false);
  assert.equal(markerMaterial(mark, false), first, "the same marker is the same material");
  assert.equal(markerMaterial({ ...mark }, false), first, "keyed by what it draws, not by identity");
  assert.notEqual(markerMaterial({ ...mark, color: "#c8734f" }, false), first,
    "a different colour is a different marker");
  assert.equal(first.depthWrite, false);
  assert.equal(first.sizeAttenuation, false, "a pin keeps its size however close the camera comes");
});

test("a marker with no picture wears its collection's initials in its colour", () => {
  const material = markerMaterial({ ...mark, color: "#123456" }, false);
  const canvas = (material.map as THREE.Texture).image as StubCanvas;
  assert.equal(canvas.width, 80);
  assert.equal(canvas.height, 80);
  const stroked = canvas.ops.find((op) => op.name === "strokeText");
  const filled = canvas.ops.find((op) => op.name === "fillText");
  assert.deepEqual(stroked?.args, ["IC", 40, 41], "the rim goes down first");
  assert.deepEqual(filled?.args, ["IC", 40, 41], "and the colour over it");
  assert.equal(canvas.ops.some((op) => op.name === "arc"), false, "an unchosen pin wears no ring");
});

test("the chosen variant is a second entry in the cache, and it is the ringed one", () => {
  const ringed = markerMaterial(mark, true);
  assert.notEqual(ringed, markerMaterial(mark, false), "chosen and unchosen are two materials");
  assert.equal(markerMaterial(mark, true), ringed, "and the chosen one is cached too");
  const canvas = (ringed.map as THREE.Texture).image as StubCanvas;
  const ring = canvas.ops.find((op) => op.name === "arc");
  assert.deepEqual(ring?.args, [40, 40, 36, 0, Math.PI * 2], "a ring around the whole marker");
  assert.equal(canvas.ops.some((op) => op.name === "stroke"), true);
});

test("a collection's initials are its first two words' first letters", () => {
  assert.equal(initialsOf("Impact Craters"), "IC");
  assert.equal(initialsOf("Landing Sites and Rovers"), "LS");
  assert.equal(initialsOf("Volcanoes"), "V");
  assert.equal(initialsOf(""), "");
});

// ---- a rebuild gives back what it minted ------------------------------

/** Count what a rebuild frees: three dispatches `dispose` on the way out. */
function watch(
  target: THREE.EventDispatcher<{ dispose: object }>, seen: string[], name: string,
): void {
  target.addEventListener("dispose", () => { seen.push(name); });
}

test("releasing the raised names disposes every texture and material they held", () => {
  const group = new THREE.Group();
  const seen: string[] = [];
  for (const title of ["Elysium", "Hellas", "Arsia Mons"]) {
    const card = nameCard(title, somewhere);
    watch((card.material as THREE.SpriteMaterial).map as THREE.Texture, seen, `${title}: texture`);
    watch(card.material as THREE.SpriteMaterial, seen, `${title}: material`);
    group.add(card);
  }
  release(group);
  assert.equal(group.children.length, 0, "the group is empty");
  assert.deepEqual(seen.sort(), [
    "Arsia Mons: material", "Arsia Mons: texture",
    "Elysium: material", "Elysium: texture",
    "Hellas: material", "Hellas: texture",
  ], "and nothing it minted is still held");
});

test("a rebuild frees a mesh's geometry and leaves three's shared sprite quad alone", () => {
  const group = new THREE.Group();
  const seen: string[] = [];
  const mesh = new THREE.Mesh(
    new THREE.BufferGeometry(), new THREE.MeshBasicMaterial());
  watch(mesh.geometry, seen, "mesh geometry");
  const card = nameCard("Elysium", somewhere);
  // Every sprite in the scene shares one quad: disposing it here would take
  // the geometry out from under the pins as well.
  watch(card.geometry, seen, "sprite geometry");
  group.add(mesh, card);
  release(group);
  assert.deepEqual(seen, ["mesh geometry"]);
});

test("the shared marker materials survive a release, because no group owns them", () => {
  const material = markerMaterial({ ...mark, title: "Dunes" }, false);
  const seen: string[] = [];
  watch(material, seen, "marker");
  const group = new THREE.Group();
  group.add(nameCard("Dunes", somewhere));
  release(group);
  assert.deepEqual(seen, [], "a name coming down is not a collection losing its mark");
  assert.equal(markerMaterial({ ...mark, title: "Dunes" }, false), material);
});
