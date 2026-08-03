// The pins on the sphere: what they are drawn over, and what hides them.
//
// Two defects the reader found, and one number that must not move.
//
// AN ICON IS DRAWN ON TOP OF THE RASTER. A pin's sprite is screen-sized
// around a point half a unit off the skin, so at the limb most of that square
// stands over ground *nearer* the camera than its own anchor. Tested against
// the depth buffer, the planet swallowed the buried half: the reader's
// screenshot of Mars is a rim of icons sunk to the waist. So the depth test
// comes off, as it already had for the cards and the chips.
//
// AND THE FAR SIDE MUST NOT SHINE THROUGH. Depth was doing one honest job as
// well as the dishonest one, and `cull` takes it over: a pin past the limb is
// ground nobody can see.
//
// THE STANDING COUNT IS THE FILTERS' ANSWER. `visibleSprites` in six recorded
// tours is a count of `sprite.visible` — 2048 at every Mars step, which is the
// registry with nothing filtered out — and it means "what the legend left",
// never "what the camera is pointing at". So the horizon is spelled with
// layers: the camera draws layer 0 and nothing else, and a pin over the rim
// steps off it without touching a boolean somebody else owns.
//
// The last section is the other half of the reader's report: a world change
// under a standing sphere. What the pane does about it is `viewport.test.ts`'s
// — here it is only the sphere's own half, that another world is another skin
// and another set of pins rather than the last world's, redrawn.

import test from "node:test";
import { strict as assert } from "node:assert";
import * as THREE from "three";
import {
  KEY_GEOMETRY_EQUIRECT_DEG,
  KEY_GEOMETRY_EQUIRECT_PX,
  KEY_GEOMETRY_PROJECTION,
  KEY_GEOMETRY_SURFACE,
} from "@atlas/analysis/semconv/keys";
import { setLevel } from "../log.ts";
import type { WorldContext } from "../context.ts";

setLevel("error");

// ---- the page the sphere is drawn in ---------------------------------

/** A stub node: enough of one for `globe.gl` to load over. */
function stubNode(): Record<string, unknown> {
  return {
    width: 0, height: 0, style: {}, classList: { add: () => {} },
    getContext: () => ({
      font: "10px", textAlign: "", textBaseline: "", lineWidth: 0,
      fillStyle: "", strokeStyle: "", globalAlpha: 1,
      measureText: (text: string) => ({ width: text.length * 8 }),
      fillRect: () => {}, fillText: () => {}, strokeText: () => {},
      drawImage: () => {}, beginPath: () => {}, arc: () => {}, stroke: () => {},
      clearRect: () => {},
    }),
    appendChild: () => {}, insertBefore: () => {},
    setAttribute: () => {}, addEventListener: () => {},
  };
}

const host = globalThis as unknown as Record<string, unknown>;
host.HTMLElement = class {};
host.window = globalThis;
host.requestAnimationFrame = (): number => 0;
host.cancelAnimationFrame = (): void => {};
host.Image = class { src = ""; crossOrigin = ""; };
host.document = {
  createElement: stubNode, createElementNS: stubNode, createTextNode: stubNode,
  getElementsByTagName: () => [stubNode()], querySelector: () => null,
  querySelectorAll: () => [], addEventListener: () => {},
  head: stubNode(), body: stubNode(),
};

// Imported after the page exists: `globe.gl` reads the document as it loads.
const { AtlasGlobe } = await import("../globe/element.ts");

// ---- a sphere with pins standing on it -------------------------------

/**
 * The globe, as much of one as a pin needs.
 *
 * `getCoords` is globe.gl's own spelling of a latitude and a longitude at an
 * altitude given as a fraction of the radius, so longitude 0 stands at
 * +x — which is where the camera below is parked, and therefore what "the
 * near side" means in every assertion here.
 */
function stubGlobe(at = { x: 300, y: 0, z: 0 }) {
  const camera = { position: at };
  return {
    getCoords(lat: number, lng: number, altitude = 0) {
      const radius = 100 * (1 + altitude);
      const phi = (lat * Math.PI) / 180;
      const theta = (lng * Math.PI) / 180;
      return {
        x: radius * Math.cos(phi) * Math.cos(theta),
        y: radius * Math.sin(phi),
        z: radius * Math.cos(phi) * Math.sin(theta),
      };
    },
    camera: () => camera,
    pointOfView: () => ({ lat: 0, lng: 0, altitude: 2.5 }),
    scene: () => new THREE.Scene(),
  };
}

/** The sphere's own fields, so a test can put a globe where a build would. */
interface Inside {
  globe: unknown;
  skin: unknown;
  lensKey: string;
}

function inside(element: object): Inside {
  return element as unknown as Inside;
}

/**
 * The declared window: the whole planet, 3600 by 1800 world pixels.
 *
 * Which makes a place easy to name — world x 1800 is longitude 0 and world y
 * -900 is the equator — and the second world below declares a different one,
 * because a window is a world's own declaration and that is the whole reason
 * a skin cannot be carried across.
 */
function attrs(width: number): Record<string, string> {
  return {
    [KEY_GEOMETRY_SURFACE]: "sphere",
    [KEY_GEOMETRY_PROJECTION]: "equirect",
    [KEY_GEOMETRY_EQUIRECT_PX]: `0,0,${width},${width / 2}`,
    [KEY_GEOMETRY_EQUIRECT_DEG]: "-180,90,180,-90",
  };
}

/** A pin somewhere on the planet, in the world's own coordinates. */
function point(id: string, x: number, y: number) {
  return { id, title: id, coordinate: [x, y], collection: { id: 1 } };
}

/** Longitude 0, facing the camera; longitude 180, behind the planet. */
const NEAR = point("elysium", 1800, -900);
const FAR = point("hellas", 3600, -900);

interface Standing {
  hidden: ReadonlySet<string>;
}

/**
 * A world as the sphere is handed one: the payload's declaration, the pins,
 * and a standing set the filters answer through.
 */
function world(slug: string, points: ReturnType<typeof point>[], standing: Standing, width = 3600) {
  return {
    base: "/data/mars",
    outset: "light",
    lens: null,
    cell: "",
    labelsHeld: false,
    subgridVisible: false,
    system: null,
    scene: { selected: "", gridSystem: "" },
    model: {
      slug,
      payload: { attrs: attrs(width), lenses: [], collections: [] },
      points,
      collections: [{ id: 1, title: "Impact Craters", kind: "point", visible: true }],
    },
    visibility: {
      at: (index: number) => ({ hidden: standing.hidden.has(points[index]?.id ?? "") }),
    },
  } as unknown as WorldContext;
}

/** A skin that says what it was told, without compositing anything. */
function stubSkin() {
  const retargeted: unknown[] = [];
  return {
    retargeted,
    retarget: (window: unknown) => { retargeted.push(window); },
    base: () => Promise.resolve(),
    clearDetail: () => {},
  };
}

/** A sphere standing over one world, with its pins built. */
function showing(context: WorldContext, globe = stubGlobe()) {
  const element = new AtlasGlobe();
  inside(element).globe = globe;
  inside(element).skin = stubSkin();
  element.show(context);
  return element;
}

/** The camera's own layers: what a renderer will and will not draw. */
const CAMERA = new THREE.Layers();

function drawn(sprite: THREE.Object3D): boolean {
  return sprite.layers.test(CAMERA);
}

function pin(element: InstanceType<typeof AtlasGlobe>, id: string): THREE.Sprite {
  const sprite = element.seam.sprites.get(id);
  assert.ok(sprite, `${id} is standing`);
  return sprite as THREE.Sprite;
}

const nothing: Standing = { hidden: new Set() };

// ---- an icon is drawn on top of the raster ----------------------------

test("a pin draws over the ground it stands on, and under the writing", () => {
  const element = showing(world("mars", [NEAR], nothing));
  const sprite = pin(element, "elysium");
  const material = sprite.material as THREE.SpriteMaterial;
  assert.equal(material.depthTest, false, "the planet no longer buries the limb pins");
  assert.equal(material.depthWrite, false);
  assert.equal(material.sizeAttenuation, false, "screen-sized, not world-sized");
  assert.equal(sprite.renderOrder, 3,
    "over the tiles (0), the fills (1) and the boundaries (2); under the chips (4) and cards (5)");
});

// ---- and the far side does not shine through --------------------------

test("a pin past the limb is taken off the layer the camera draws", () => {
  const element = showing(world("mars", [NEAR, FAR], nothing));
  assert.equal(drawn(pin(element, "elysium")), true, "the near side is drawn");
  assert.equal(drawn(pin(element, "hellas")), false, "the far side is not");
});

test("the planet turning brings the far side back, and nothing else moves", () => {
  const element = showing(world("mars", [NEAR, FAR], nothing));
  const near = pin(element, "elysium");
  const far = pin(element, "hellas");
  const before = [near.visible, far.visible, near.scale.x, far.scale.x];

  // The reader turns the planet: the camera goes round to face longitude 180.
  const turned = showing(world("mars", [NEAR, FAR], nothing),
    stubGlobe({ x: -300, y: 0, z: 0 }));
  assert.equal(drawn(pin(turned, "elysium")), false);
  assert.equal(drawn(pin(turned, "hellas")), true);
  assert.deepEqual(
    [pin(turned, "elysium").visible, pin(turned, "hellas").visible,
      pin(turned, "elysium").scale.x, pin(turned, "hellas").scale.x],
    before,
    "the horizon moved and neither the standing flag nor the size did");
});

// ---- the standing count is the filters' answer ------------------------

test("what is standing is what the legend left, wherever the camera points", () => {
  // What a diagnostics reading of the pane is: the size of the map, and how
  // many of the sprites in it are `visible`. On Mars both numbers are 2048
  // -- the whole registry, nothing filtered -- even with the camera pointed
  // so that half the planet is turned away.
  const both = showing(world("mars", [NEAR, FAR], nothing));
  const standing = (element: InstanceType<typeof AtlasGlobe>): number =>
    [...element.seam.sprites.values()].filter((sprite) => sprite.visible).length;
  assert.equal(both.seam.sprites.size, 2, "every pin the world has, built once");
  assert.equal(standing(both), 2,
    "and every one of them standing, though one is behind the planet");

  // A filter, which is the one thing that may move that number.
  const filtered = showing(world("mars", [NEAR, FAR], { hidden: new Set(["elysium"]) }));
  assert.equal(filtered.seam.sprites.size, 2, "the map is the registry, not the standing set");
  assert.equal(standing(filtered), 1, "and a hidden collection is one fewer standing");
});

test("a pin a filter lets back in is held to the horizon before it is drawn", () => {
  // The order that made this worth a test: `update` writes `visible` from the
  // filters, and a sprite born or restored on the camera's layer would shine
  // through the planet until the reader next moved it.
  const context = world("mars", [FAR], { hidden: new Set(["hellas"]) });
  const element = showing(context);
  assert.equal(pin(element, "hellas").visible, false, "filtered out to begin with");

  const shown = context as unknown as { visibility: { at: () => { hidden: boolean } } };
  shown.visibility = { at: () => ({ hidden: false }) };
  element.update();
  assert.equal(pin(element, "hellas").visible, true, "the filter let it back in");
  assert.equal(drawn(pin(element, "hellas")), false, "and the planet is still in front of it");
});

// ---- another world is another sphere ----------------------------------

test("a world change rebuilds the pins and retargets the skin", () => {
  const element = showing(world("mars", [NEAR], nothing));
  const skin = inside(element).skin as ReturnType<typeof stubSkin>;
  inside(element).lensKey = "/data/mars/mars#viking-mdim";

  element.show(world("phobos", [FAR], nothing, 1800));
  assert.deepEqual([...element.seam.sprites.keys()], ["hellas"],
    "the pins of the world being left are down, and the new world's are up");
  assert.deepEqual(skin.retargeted, [
    { x: 0, y: 0, width: 3600, height: 1800 },
    { x: 0, y: 0, width: 1800, height: 900 },
  ], "the skin was given each world's own window of world pixels, in turn");
  assert.equal(inside(element).lensKey, "",
    "and the lens memo went with it, so the skin is composited again");
});

test("the same world shown again rebuilds nothing", () => {
  // Which is what makes a filter a filter: `show` is called on every scene
  // change, and two thousand sprites and a four-megapixel composite are not
  // what turning a collection off asked for.
  const element = showing(world("mars", [NEAR], nothing));
  const skin = inside(element).skin as ReturnType<typeof stubSkin>;
  const sprite = pin(element, "elysium");
  skin.retargeted.length = 0;
  element.show(world("mars", [NEAR], nothing));
  assert.equal(pin(element, "elysium"), sprite, "the same sprite, not a new one");
  assert.deepEqual(skin.retargeted, [], "and the skin was left alone");
});
