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
    toDataURL: () => "data:image/png;base64,raster",
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
// Images arrive of their own accord here: what the sphere's tests are about
// is what it does with a raster once it has one, and the window before that
// is `markers.test.ts`'s.
host.Image = class {
  crossOrigin = "";
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private asked = "";

  get src(): string {
    return this.asked;
  }

  set src(value: string) {
    this.asked = value;
    setTimeout(() => this.onload?.(), 0);
  }
};
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
  altitudeForZoom, angularDistance, boundsOf, cellBoundary, densifyRing, detailBlock,
  detailLevel, facesCamera, fillRows, holdControls, initialsOf, labelCandidates,
  markerMaterial, nameCard, release, ringFill, ringLatLng, tileGeometry, wearSkin,
  zoneMesh, zoneOutlines, zoomForAltitude,
} = await import("../globe/element.ts");
type Placed = import("../globe/element.ts").Placed;

/**
 * The sphere the seam draws on, without a WebGL context under it.
 *
 * `getCoords` is globe.gl's own spelling of a latitude and a longitude at an
 * altitude given as a fraction of the radius, and it is the only thing any of
 * the geometry below asks of a globe — so the stub is that one function, and
 * every assertion about where a vertex landed is an assertion about the seam's
 * arithmetic rather than about three's.
 */
const sphere = {
  getCoords(lat: number, lng: number, altitude = 0): { x: number; y: number; z: number } {
    const radius = 100 * (1 + altitude);
    const phi = (lat * Math.PI) / 180;
    const theta = (lng * Math.PI) / 180;
    return {
      x: radius * Math.cos(phi) * Math.cos(theta),
      y: radius * Math.sin(phi),
      z: radius * Math.cos(phi) * Math.sin(theta),
    };
  },
} as unknown as import("globe.gl").GlobeInstance;

/** How far a vertex sits from the middle of the planet. */
function radiiOf(mesh: THREE.Object3D): number[] {
  const drawn = mesh as THREE.Mesh;
  const position = drawn.geometry.getAttribute("position");
  const radii: number[] = [];
  for (let at = 0; at < position.count; at++) {
    radii.push(Math.hypot(position.getX(at), position.getY(at), position.getZ(at)));
  }
  return radii;
}

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
  assert.equal(card.renderOrder, 5, "over the grid (1, 2, 4) and over the pin it names (3)");
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
  asset: "", url: "", picture: false, color: "#4fb3d5",
  outset: "rgba(7, 9, 7, 0.98)", title: "Impact Craters",
};

/** Let the microtasks behind a delivered image run. */
function settle(): Promise<void> {
  return new Promise((resolve) => { setTimeout(resolve, 0); });
}

test("a collection's marker is built once and shared by every pin wearing it", () => {
  const first = markerMaterial(mark, false);
  assert.equal(markerMaterial(mark, false), first, "the same marker is the same material");
  assert.equal(markerMaterial({ ...mark }, false), first, "keyed by what it draws, not by identity");
  assert.notEqual(markerMaterial({ ...mark, color: "#c8734f" }, false), first,
    "a different colour is a different marker");
  assert.equal(first.depthWrite, false);
  assert.equal(first.sizeAttenuation, false, "a pin keeps its size however close the camera comes");
  // THE DEFECT THIS PINS DOWN. A pin's sprite is screen-sized around a point
  // half a unit off the skin, so at the limb most of that square stands over
  // ground nearer the camera than its own anchor: tested against the depth
  // buffer, the planet swallowed the buried half and the reader saw icons sunk
  // to the waist in the rim. An icon is drawn on top of the raster, and the
  // far side is kept out by the horizon instead (`cull`).
  assert.equal(first.depthTest, false, "a pin is not clipped by the ground it stands on");
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

test("the sphere wears the chart's raster rather than composing one of its own", async () => {
  const material = markerMaterial(
    { ...mark, asset: "shrine.svg", url: "/icons/shrine.svg", color: "#123456" }, false);
  await settle();
  await settle();
  const canvas = (material.map as THREE.Texture).image as StubCanvas;
  assert.equal(canvas.width, 80, "the sphere's own canvas is still 80 square");
  const drawn = canvas.ops.filter((op) => op.name === "drawImage");
  assert.equal(drawn.length, 1, "one image: the finished symbol");
  assert.deepEqual(drawn[0]?.args.slice(1), [8, 8, 64, 64],
    "the 64-square raster, inset by eight so the ring has somewhere to go");
  // The tint and the halo are composed into the raster the chart hands over,
  // and a sphere that tinted it a second time would be deciding for itself
  // what a Shrine looks like -- which is how the two panes drifted apart.
  assert.equal(canvas.ops.some((op) => op.name === "fillRect"), false);
  assert.equal(canvas.ops.some((op) => op.name === "fillText"), false,
    "and it is a symbol, not initials");
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

// ---- the grid follows the curve ---------------------------------------
//
// THE DEFECT, stated as arithmetic. A cell's ring is four corners, and at the
// root of a plan two of them are forty-five degrees apart. Joined by a
// straight line in three dimensions that is a chord, and a chord across 45° of
// a hundred-unit sphere passes seven units *inside* it — so a fill built from
// the corners alone was a sheet drawn through the crust, and the boundary over
// it went the same way. The bigger the cell the deeper it sank, which is why
// the dimmed neighbourhood was the part that "did not render": it was drawn,
// underground. Densifying by span is the whole fix, and the tests below are
// the two clamps and the lift that make it one.

test("a ring is subdivided by the span of its own segments", () => {
  // Forty-five degrees at two degrees a step, and the last vertex of a
  // segment belongs to the next one: an open loop, never a repeated point.
  const quarter = densifyRing([[0, 0], [0, 45]], 1);
  assert.equal(quarter.length, 23, "ceil(45 / 2) steps across the segment");
  assert.deepEqual(quarter[0], [0, 0], "starting where the segment starts");
  assert.ok(Math.abs((quarter.at(-1)?.[1] ?? 0) - (45 * 22) / 23) < 1e-9,
    "and stopping short of its end");
});

test("the subdivision has a floor and a ceiling, and the caller sets the floor", () => {
  const hair: [number, number][] = [[0, 0], [0, 0.5]];
  assert.equal(densifyRing(hair, 1).length, 1, "a fill wants at least one step");
  assert.equal(densifyRing(hair, 4).length, 4,
    "a boundary wants four: it is what a reader sees the curve of");
  // A pole cell's ring walks the whole planet, and 180 steps of a single
  // segment is a boundary nobody can see the difference in.
  assert.equal(densifyRing([[0, -180], [0, 180]], 1).length, 48, "capped at 48 a segment");
});

test("a fill's rings of triangles are counted by span, between two and twenty-four", () => {
  assert.equal(fillRows(0), 2, "even the smallest cell gets two");
  assert.equal(fillRows(45), 8, "ceil(45 / 6)");
  assert.equal(fillRows(360), 24, "and no cell gets more than twenty-four");
});

test("every vertex of a fill is lifted back onto the sphere it is drawn on", () => {
  // The root plan's own cells: 45 degrees of longitude, 45 of latitude.
  const ring: [number, number][] = [[0, 0], [45, 0], [45, 45], [0, 45], [0, 0]];
  const corners = boundsOf(ring);
  assert.ok(corners);
  const mesh = ringFill(sphere, ring, corners, [22.5, 22.5], { color: "#050810", opacity: 0.3 });
  assert.ok(mesh, "the cell has a sheet");
  const radii = radiiOf(mesh);
  const wanted = 100.32;
  // The vertices are stored as 32-bit floats, so "on the surface" is to a
  // ten-thousandth of a unit; the defect this pins down was seven whole units.
  for (const radius of radii) {
    assert.ok(Math.abs(radius - wanted) < 1e-4,
      `a vertex at ${radius} is not on the surface — the sheet sags into the planet`);
  }
  // Which is the defect measured: the chord between two corners 45 apart, and
  // the straight walk from the middle out to them, both cut deep inside.
  assert.ok(radii.length > 5 * 4, "and there are far more vertices than the ring had corners");
});

test("a fill is drawn under the boundary and over the tiles, writing no depth", () => {
  const ring: [number, number][] = [[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]];
  const corners = boundsOf(ring);
  assert.ok(corners);
  const mesh = ringFill(sphere, ring, corners, [5, 5], { color: "#4fb3d5", opacity: 0.14 });
  assert.ok(mesh);
  const material = mesh.material as THREE.MeshBasicMaterial;
  assert.equal(mesh.renderOrder, 1,
    "over the tiles (0), under the boundary (2), the pins (3), the chip (4) and the cards (5)");
  assert.equal(material.transparent, true);
  assert.equal(material.opacity, 0.14);
  assert.equal(material.depthWrite, false, "a sheet that wrote depth would erase its own boundary");
  assert.equal(material.side, THREE.DoubleSide);
  assert.equal(material.color.getHexString(), "4fb3d5");
});

// ---- the boundary carries its weight ----------------------------------

const square: [number, number][] = [[0, 0], [20, 0], [20, 20], [0, 20], [0, 0]];

test("a boundary is drawn at the width its role is weighed at", () => {
  // The role table of `gridTheme.widths`, which WebGL's native line ignores
  // outright: every one of these came out a hairline, and the hierarchy the
  // weights encode was invisible on the sphere while the chart drew it.
  for (const width of [2.5, 1.8, 1.4, 1]) {
    const loop = cellBoundary(sphere, square, { color: "#ffffff", opacity: 1, widthPx: width },
      { width: 1280, height: 800 });
    assert.ok(loop);
    const material = loop.material;
    assert.equal(material.linewidth, width, "the width is the token's, honestly drawn");
    assert.deepEqual([material.resolution.x, material.resolution.y], [1280, 800],
      "a width in pixels is measured against the window it is drawn in");
    assert.equal(loop.renderOrder, 2);
    assert.equal(material.depthWrite, false);
  }
});

test("a boundary follows the curve and closes on itself", () => {
  const loop = cellBoundary(sphere, square, { color: "#c9924b", opacity: 0.44, widthPx: 1 },
    { width: 900, height: 600 });
  assert.ok(loop);
  // Ten steps a side at twenty degrees, and the first point repeated to close.
  const positions = loop.geometry.getAttribute("instanceStart");
  assert.equal(positions.count, 40, "four sides of ten segments, closed");
  for (let at = 0; at < positions.count; at++) {
    const radius = Math.hypot(
      positions.getX(at), positions.getY(at), positions.getZ(at));
    assert.ok(Math.abs(radius - 100.45) < 1e-4, "and every vertex of it is on the surface");
  }
  assert.equal(loop.material.opacity, 0.44);
});

test("a boundary's geometry and material are given back on release", () => {
  const group = new THREE.Group();
  const loop = cellBoundary(sphere, square, { color: "#ffffff", opacity: 1, widthPx: 2.5 },
    { width: 1280, height: 800 });
  assert.ok(loop);
  const seen: string[] = [];
  watch(loop.geometry, seen, "line geometry");
  watch(loop.material, seen, "line material");
  group.add(loop);
  release(group);
  assert.equal(group.children.length, 0);
  assert.deepEqual(seen.sort(), ["line geometry", "line material"],
    "a boundary is rebuilt every time the camera changes what fits: leaking one leaks dozens");
});

// ---- the ring is the sphere's, not the sheet's -------------------------

test("a ring that carried its longitudes past the antimeridian is left alone", () => {
  // The mapping a world declares: the whole equirect window, 8192 by 4096.
  const mapping = {
    toLatLng: (x: number, y: number): [number, number] => [(y / 4096) * 180, (x / 8192) * 360 - 180],
    toWorld: (): [number, number] => [0, 0],
  };
  const ring = ringLatLng([[7680, 0], [8704, 0], [8704, -1024], [7680, -1024], [7680, 0]], mapping);
  const corners = boundsOf(ring);
  assert.ok(corners);
  assert.ok(corners.east > 180,
    "the loop stays continuous: the sphere is periodic and a cut would tear the cell in two");
  assert.equal(ring.length, 5, "and it is still one ring");
});

// ---- the pyramid under the camera --------------------------------------
//
// The worked case both tests below share: the camera at altitude 0.68 over
// 0,0 draws 81 tiles of level 5. Both numbers fall out of the two functions
// below and nothing else — the altitude reads as zoom 3.878, rounds to 4,
// and one deeper is 5; and at level 5 a nine-by-nine block is the widest
// reach that stays inside a 96-tile budget.

test("the level is one deeper than the distance reads as, capped by the capture", () => {
  const ceiling = 8;
  const table: [number, number][] = [
    [2.5, 3], // the whole disc reads as zoom 2
    [0.68, 5], // the worked case: 3.878 rounds to 4, and one deeper is 5
    [0.08, 8], // pressed against the nearest altitude
  ];
  for (const [altitude, wanted] of table) {
    assert.equal(detailLevel(zoomForAltitude(altitude, ceiling), 6),
      Math.min(wanted, 6), `at altitude ${altitude}`);
  }
  assert.equal(detailLevel(5.4, 6), 6, "5.4 rounds to 5 and one deeper is 6");
  assert.equal(detailLevel(5.6, 6), 6, "and the capture's own floor is the ceiling");
  assert.equal(detailLevel(2, 4), 3);
});

test("the neighbourhood is the block the horizon asks for, pulled into its budget", () => {
  const wanted = detailBlock({ lat: 0, lng: 0, altitude: 0.68 }, 5, 96);
  assert.equal(wanted.length, 81,
    "the widest block under the budget: reach 6 is 169 tiles, 5 is 121, and 4 is 81 ≤ 96");
  const columns = new Set(wanted.map(([x]) => x));
  const rows = new Set(wanted.map(([, y]) => y));
  assert.equal(columns.size, 9, "nine columns");
  assert.equal(rows.size, 9, "by nine rows");
  assert.ok([...columns].every((x) => x >= 0 && x < 32), "wrapped in longitude: the sphere closes");
});

test("the block is clipped at the poles and wrapped at the seam", () => {
  const pole = detailBlock({ lat: 88, lng: 0, altitude: 0.68 }, 5, 96);
  assert.ok(pole.length < 81, "there is no row above the pole to ask for");
  assert.ok(pole.every(([, y]) => y >= 0 && y < 16));
  const seam = detailBlock({ lat: 0, lng: 179, altitude: 0.68 }, 5, 96);
  assert.equal(seam.length, 81);
  assert.ok(seam.some(([x]) => x === 0) && seam.some(([x]) => x === 31),
    "a camera on the antimeridian reads tiles from both ends of the pyramid");
});

test("a shallower level has room for a wider reach", () => {
  // The budget is a count of tiles, not of degrees: at level 3 the whole
  // horizon is three tiles across, the block is seven columns wide and nothing
  // has to be given up — and the four rows are every row the pyramid has.
  const wanted = detailBlock({ lat: 0, lng: 0, altitude: 2.5 }, 3, 96);
  assert.equal(wanted.length, 28);
  assert.equal(new Set(wanted.map(([x]) => x)).size, 7);
  assert.equal(new Set(wanted.map(([, y]) => y)).size, 4, "clipped to the pyramid's own rows");
});

// ---- a tile is draped, not composited ----------------------------------
//
// The base skin is one 4096-pixel canvas of the whole equirect window, which
// is level four exactly. A level-six tile composited into it is a 256-pixel
// square drawn 64 wide — three quarters of the capture thrown away before
// anything is drawn, which is what made the sphere read two levels shallower
// than the chart at the same camera. Draped as its own mesh, a tile covers its
// own ground at its own pitch.

test("a tile is draped on a grid that follows the curve, at the pyramid's own radius", () => {
  const geometry = tileGeometry(sphere, -180, 90, 11.25);
  const position = geometry.getAttribute("position");
  assert.equal(position.count, 81, "nine by nine points: an eight-segment grid");
  assert.equal(geometry.getIndex()?.count, 8 * 8 * 6, "two triangles a square");
  for (let at = 0; at < position.count; at++) {
    const radius = Math.hypot(position.getX(at), position.getY(at), position.getZ(at));
    assert.ok(Math.abs(radius - 100.2) < 1e-4,
      "just off the skin, under everything the grid draws");
  }
  const uv = geometry.getAttribute("uv");
  assert.deepEqual([uv.getX(0), uv.getY(0)], [0, 1], "the tile's top-left is the texture's");
  assert.deepEqual([uv.getX(80), uv.getY(80)], [1, 0], "and its bottom-right likewise");
});

test("the wheel is held to the same clamps as every other door", () => {
  // globe.gl's own controls would dolly the camera to a hair above the skin
  // -- under the detail shell and the pins, where every face is a back face
  // and the pane renders black -- and out to a hundred radii. holdControls
  // gives the wheel the same two rails steer, changeZoom and the flip
  // already ride: the pairing's own clamps, read back here through the
  // exported conversion at a zoom past either end (the sphere's radius in
  // globe.gl's units is 100).
  const controls = { minDistance: 0, maxDistance: Infinity };
  holdControls(controls);
  assert.equal(controls.minDistance, 100 * (1 + altitudeForZoom(999)));
  assert.equal(controls.maxDistance, 100 * (1 + altitudeForZoom(-999)));
  // The nearest stand keeps the camera outside everything drawn off the
  // skin: the detail tiles at radius 100.2 and the pins above them.
  assert.ok(controls.minDistance > 100.6, `camera can reach ${controls.minDistance}`);
  // And the farthest keeps the planet bigger than a dot.
  assert.ok(controls.maxDistance <= 1000, `camera can leave to ${controls.maxDistance}`);
});

// ---- zones on the sphere ---------------------------------------------
//
// The chart has always drawn a world's area and path features; the sphere
// grew its zone layer when the included Earth became the first volume to put
// areas on a spherical world. What is contractual is small and exact: which
// outlines a shape draws (holes ride their areas, paths stay open), and that
// every vertex of the landed mesh sits on its own radius rather than on a
// chord through the planet -- the same promise the grid's boundaries make.

type ShapeRecord = import("../world/model.ts").ShapeRecord;

/** The identity mapping: a ground whose world units are already degrees. */
const degrees = {
  toLatLng: (x: number, y: number): [number, number] => [y, x],
  toWorld: (lat: number, lng: number): [number, number] => [lng, lat],
};

function zoneShape(overrides: Partial<ShapeRecord>): ShapeRecord {
  return {
    id: "9", title: "Vale", subtitle: "", kind: "area", shard: 0,
    collection: { id: 1, title: "Countries", kind: "area", visible: true },
    lines: [], holes: [], center: null, feature: { id: 9, title: "Vale", geometry: [] },
    ...overrides,
  } as ShapeRecord;
}

// A square area in world coordinates (y negative-down), with one hole.
const zoneOuter = [[10, -10], [20, -10], [20, -20], [10, -20], [10, -10]] as [number, number][];
const zoneHole = [[12, -12], [14, -12], [14, -14], [12, -14], [12, -12]] as [number, number][];

test("an area's holes ride beside its outer rings, closed", () => {
  const outlines = zoneOutlines(zoneShape({ lines: [zoneOuter], holes: [[zoneHole]] }));
  assert.equal(outlines.length, 2);
  assert.deepEqual(outlines.map((held) => held.close), [true, true]);
  assert.equal(outlines[0]?.ring, zoneOuter);
  assert.equal(outlines[1]?.ring, zoneHole);
});

test("a path's lines stay open", () => {
  const walk = [[0, 0], [30, -5]] as [number, number][];
  const outlines = zoneOutlines(zoneShape({ kind: "path", lines: [walk], holes: [[]] }));
  assert.deepEqual(outlines.map((held) => held.close), [false]);
});

test("every landed zone vertex sits on its own radius, not on a chord", () => {
  const mesh = zoneMesh(sphere, zoneShape({ lines: [zoneOuter], holes: [[zoneHole]] }),
    degrees, { color: "#88aaff", opacity: 0.85, widthPx: 1.4 }, { width: 800, height: 600 });
  assert.ok(mesh, "a shape with ground drew nothing");
  const start = mesh.geometry.getAttribute("instanceStart");
  assert.ok(start.count > 8, "the segments were not densified");
  const radius = Math.hypot(start.getX(0), start.getY(0), start.getZ(0));
  for (let at = 0; at < start.count; at++) {
    const held = Math.hypot(start.getX(at), start.getY(at), start.getZ(at));
    assert.ok(Math.abs(held - radius) < 0.01,
      `vertex ${at} sits at radius ${held}, the surface is at ${radius}`);
  }
});

test("a shape with no ground draws nothing on the sphere", () => {
  const mesh = zoneMesh(sphere, zoneShape({ lines: [], holes: [] }),
    degrees, { color: "#88aaff", opacity: 0.85, widthPx: 1.4 }, { width: 800, height: 600 });
  assert.equal(mesh, null);
});
