// `<atlas-globe>` — the sphere, for a world that declares one.
//
// A world is a ground and a lens is a picture of it; where the ground is a
// sphere and the picture is equirectangular, the picture can be draped back
// onto the shape it came off. That is the whole of this pane. It is offered
// only where `atlas.geometry.surface` says `sphere` and the flattening
// inverts, and everything it draws it reads from the same model and the same
// standing set the chart does — one filter, both panes, the same moment.
//
// FOUR BUDGETS, each of them a decision rather than a limit:
//
//   The base skin is one texture, composited once per lens.
//   The detail is composited into that same texture, under the camera only.
//   Names are raised only while Z is held, and at most 180 of them: past that
//   a sphere is a word cloud with a planet behind it. WHICH 180 is the
//   camera's answer and not the legend's — the nearest names to what is being
//   looked at, out to the rim and no further.
//   Sprites are built once per pin and afterwards only shown, hidden or
//   re-dressed, so a filter costs a boolean per pin rather than a rebuild —
//   and the dressing itself is one shared material per collection.
//
// THE HORIZON is enforced by hand. Cards and chips turn the depth test off,
// because a screen-sized card anchored on the ground would otherwise lose its
// lower half to the planet's own curve; `cull` puts the silhouette back, so
// nothing on the far side shines through. The pins keep their depth test and
// are left alone by it, which is what keeps the standing count a reading of
// the filters rather than of where the camera is pointing.
//
// THE CAMERA ROUND TRIP is the pane's one contract with the chart. A flip to
// the sphere and straight back must land the chart's camera exactly where it
// was: the pairing below is invertible arithmetic, and the view the globe was
// handed is handed back unchanged unless the reader actually moved it.

import * as THREE from "three";
import Globe from "globe.gl";
import type { GlobeInstance } from "globe.gl";
import {
  KEY_GEOMETRY_EQUIRECT_DEG,
  KEY_GEOMETRY_EQUIRECT_PX,
  KEY_GEOMETRY_SURFACE,
  KEY_ICON_KIND,
} from "@atlas/analysis/semconv/keys";
import {
  cellPlan, cellRings, cellSystems, equirectMapping, gridCellVisual, gridTheme,
} from "@atlas/analysis";
import type { GeoMapping, PlanCell } from "@atlas/analysis";
import { logger } from "../log.ts";
import type { WorldContext } from "../context.ts";
import type { Attrs, Collection } from "../data/payload.ts";
import { reportPick } from "../data/report.ts";
import { viewMaxZoom } from "../chart/projection.ts";
import { collectionColor, outsetColor } from "../chart/styles.ts";
import { Skin } from "./texture.ts";

const log = logger("globe");

// THE PAIRING, calibrated against the recorded tours rather than guessed.
//
// The two cameras answer to one another as a power law anchored at one point:
// the whole disc at altitude 2.5 reads like the whole chart at zoom 2, and
// each halving of altitude reads like one more zoom. It is deliberately *not*
// a field-of-view calculation -- an earlier draft of this seam derived the
// altitude from the resolution, the viewport height and the declared degree
// span, which is defensible arithmetic and reproduces none of the recorded
// numbers. `golden/parity/mars/tour.json` settles it: `globe-left` records a
// chart zoom of 1.3219 = 2 + log2(2.5 / 4) after the camera has been pushed
// out to the farthest distance, and `globe-labels-held` records an altitude
// of 0.68 = 2.5 / 2^(1.8826 - 2) / 4 after the zoom buttons have halved it
// twice. Both fall out of the four constants below and nothing else does.
const WHOLE_DISC_ALTITUDE = 2.5;
const WHOLE_CHART_ZOOM = 2;

// The camera keeps a respectful distance: never through the skin -- a camera
// inside the sphere sees the world inside out -- and never so far the planet
// is a dot. The clamps are load-bearing, not hygiene: the farthest is what
// `globe-left`'s recorded zoom is a reading of.
const NEAREST_ALTITUDE = 0.08;
const FARTHEST_ALTITUDE = 4;

/** The level the base skin is woven at; detail is only ever deeper. */
const TEXTURE_ZOOM = 4;

/**
 * The sphere globe.gl draws, in its own units. Only one thing reads it: the
 * ground-per-pixel a chip's fit is measured against, which is the sphere's
 * answer to the chart's resolution.
 */
const GLOBE_RADIUS = 100;

/** Tiles the neighbourhood under the camera may hold at once. */
const DETAIL_TILE_BUDGET = 96;

/** Names raised over the sphere at once. */
const LABEL_BUDGET = 180;

/**
 * How far from what the camera faces a name may still be raised, in degrees.
 *
 * The budget says how many names; this says *which*. Past 85° a pin is on the
 * rim or behind it, and a card raised there is either edge-on or on ground
 * nobody can see -- so it takes a place in the budget away from a name the
 * reader is actually looking at. Collection priority decided this before, and
 * priority knows nothing about where the camera is standing.
 */
const LABEL_REACH_DEG = 85;

/** Where a name's card floats: just off the skin, as a fraction of the radius. */
const LABEL_ALTITUDE = 0.006;

/** A card's height on screen, and the canvas it is written on. */
const LABEL_HEIGHT = 0.028;
const CARD_FONT = "600 26px Inter, system-ui, sans-serif";
const CARD_TALL = 40;
const CARD_PAD = 24;

/** A marker's canvas, and the pitch its initials are cut at. */
const MARKER_SIZE = 80;
const MARKER_FONT = "900 26px Inter, system-ui, sans-serif";

/** How big a pin stands on screen, plain and chosen, and how far off the skin. */
const PIN_SIZE = 0.045;
const PIN_SELECTED_SIZE = 0.08;
const PIN_ALTITUDE = 0.005;

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

/** How far out a chart zoom stands the camera. */
export function altitudeForZoom(zoom: number): number {
  const safe = Number.isFinite(zoom) ? zoom : WHOLE_CHART_ZOOM;
  return clamp(
    WHOLE_DISC_ALTITUDE / 2 ** (safe - WHOLE_CHART_ZOOM),
    NEAREST_ALTITUDE, FARTHEST_ALTITUDE);
}

/** How close in a distance reads on the chart. The inverse, with its ceiling. */
export function zoomForAltitude(altitude: number, ceiling: number): number {
  const safe = Number.isFinite(altitude)
    ? Math.max(altitude, NEAREST_ALTITUDE / 2)
    : WHOLE_DISC_ALTITUDE;
  return clamp(WHOLE_CHART_ZOOM + Math.log2(WHOLE_DISC_ALTITUDE / safe), 0, ceiling);
}

/** The camera as the chart speaks it. */
export interface ChartCamera {
  x: number;
  y: number;
  zoom: number;
  rotation: number;
}

/** The equirect window a world declares, in world pixels and degrees. */
export interface Equirect {
  readonly px: readonly [number, number, number, number];
  readonly deg: readonly [number, number, number, number];
  readonly mapping: GeoMapping;
}

/** The declared flattening, or null when this world is not a sphere. */
export function equirectOf(attrs: Attrs): Equirect | null {
  const mapping = equirectMapping({ attrs });
  const px = quad(attrs[KEY_GEOMETRY_EQUIRECT_PX]);
  const deg = quad(attrs[KEY_GEOMETRY_EQUIRECT_DEG]);
  if (!mapping || !px || !deg) return null;
  return { px, deg, mapping };
}

function quad(value: string | undefined): readonly [number, number, number, number] | null {
  const parts = (value ?? "").split(",").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isFinite(part))) return null;
  return parts as [number, number, number, number];
}

/** What the harness reads off the sphere: counts, never handles. */
export interface GlobeSeam {
  detail: { lens: string; tiles: Map<string, true> };
  grid: { group: THREE.Group | null; cell: string | null; fitKey: string };
  labels: { key: string; group: THREE.Group | null };
  sprites: Map<string, THREE.Object3D>;
}

export class AtlasGlobe extends HTMLElement {
  private globe: GlobeInstance | null = null;
  private skin: Skin | null = null;
  private equirect: Equirect | null = null;
  private context: WorldContext | null = null;
  private texture: THREE.CanvasTexture | null = null;
  private readonly pins = new THREE.Group();
  private readonly labels = new THREE.Group();
  private readonly cells = new THREE.Group();
  private readonly sprites = new Map<string, THREE.Object3D>();
  private worldKey = "";
  private lensKey = "";
  private given: { lat: number; lng: number; altitude: number } | null = null;
  private handed: ChartCamera | null = null;
  private labelKey = "";
  private fitKey = "";
  /** The cell the grid was last drawn over: null while no grid is open. */
  private heldCell: string | null = null;
  /** The feature a card was last open about, so a change in it can turn the planet. */
  private selected = "";
  /** Told when the camera moves, so the corner locator can follow it. */
  onCamera: ((pov: { lat: number; lng: number; altitude: number }) => void) | null = null;

  /** The seam the parity harness reads, in the golden shape. */
  readonly seam: GlobeSeam = {
    detail: { lens: "", tiles: new Map() },
    grid: { group: null, cell: null, fitKey: "" },
    labels: { key: "", group: null },
    sprites: this.sprites,
  };

  /** Whether this world can be a sphere at all. */
  static offers(attrs: Attrs): boolean {
    return attrs[KEY_GEOMETRY_SURFACE] === "sphere" && equirectOf(attrs) !== null;
  }

  /** Whether the sphere exists yet: entered at least once this session. */
  get built(): boolean {
    return this.globe !== null;
  }

  /**
   * Take the world.
   *
   * Nothing is built until the sphere is actually entered — a WebGL context,
   * a texture and two thousand meshes are not what a reader looking at a
   * chart asked for. `globeBuilt` in the baselines is exactly this
   * distinction, so it is a real one and not an optimisation.
   */
  show(context: WorldContext): void {
    this.context = context;
    this.equirect = equirectOf(context.model.payload.attrs ?? {});
    if (!this.equirect || !this.globe) return;
    const worldKey = `${context.base}/${context.model.slug}`;
    if (worldKey !== this.worldKey) {
      this.worldKey = worldKey;
      this.lensKey = "";
      this.buildSprites(context);
    }
    this.openLens(context);
    this.update();
  }

  private openLens(context: WorldContext): void {
    const lensKey = `${this.worldKey}#${context.lens?.tiles ?? ""}`;
    if (lensKey === this.lensKey || !context.lens) return;
    this.lensKey = lensKey;
    // A different pyramid: the neighbourhood under the camera belonged to the
    // one being left and is dropped rather than re-drawn. It comes back when
    // the camera next moves, which is exactly what the recorded tour saw.
    this.skin?.clearDetail();
    void this.skin?.base(
      context.base, context.lens,
      (z, x, y) => this.tileURL(z, x, y),
      () => this.refresh());
    this.seam.detail.lens = context.lens.tiles;
  }

  /** A filter moved, or the held key: show and hide, never rebuild. */
  update(): void {
    const context = this.context;
    if (!context) return;
    if (context.scene.selected !== this.selected) {
      this.selected = context.scene.selected;
      // The ring is worn whether or not anybody is looking: a selection made
      // over the chart is what the sphere comes up already dressed for.
      this.restyle();
      this.face(this.selected);
    }
    // A sphere nobody is looking at shows and hides nothing. Everything below
    // is caught up in one call the moment it comes back up, and until then a
    // filter moving on the chart is not the sphere's business.
    if (this.hidden) return;
    context.model.points.forEach((point, index) => {
      const sprite = this.sprites.get(point.id);
      if (sprite) sprite.visible = !context.visibility.at(index).hidden;
    });
    this.drawLabels();
    this.drawGrid();
  }

  /** Come up, taking the chart's camera with you. Builds on first entry. */
  enter(camera: ChartCamera, viewport: { width: number; height: number }): void {
    this.hidden = false;
    const context = this.context;
    if (!this.globe && context) {
      this.build(context);
      this.worldKey = `${context.base}/${context.model.slug}`;
      this.buildSprites(context);
      this.openLens(context);
      this.update();
    }
    this.globe?.width(this.clientWidth || viewport.width);
    this.globe?.height(this.clientHeight || viewport.height);
    const pov = this.povOf(camera);
    if (!pov) return;
    this.handed = camera;
    this.given = pov;
    this.globe?.pointOfView(pov, 0);
    // Everything a filter moved while the sphere was down, caught up in one
    // call now that there is somebody looking.
    this.update();
    this.refreshDetail();
    this.onCamera?.(pov);
    log.info("the sphere is up", { op: "render", world: this.context?.model.slug, ...pov });
  }

  /**
   * Go away, and hand the chart a camera back.
   *
   * If the reader never moved the sphere, the camera handed back is the one
   * it was given — the same numbers, not numbers that round-trip to within a
   * float of them. If they did move it, the pairing below is inverted
   * honestly and the chart lands where the sphere was looking.
   *
   * The pane's size is not asked for: the pairing between a distance and a
   * zoom is a property of the two cameras, not of the window they are seen
   * through (§7, calibrated against the recorded tours).
   */
  leave(): ChartCamera | null {
    this.hidden = true;
    const pov = this.globe?.pointOfView();
    // A globe nobody is looking at holds no pyramid tiles: the skin under it
    // stays, because it is one texture and recompositing it is the expensive
    // half of coming back.
    this.skin?.clearDetail();
    // The name of the pyramid stays. It is the record of what the sphere read
    // while it was up, not a live handle on anything, and the baselines carry
    // it through the pane being put away.
    if (!pov || !this.given || !this.handed) return null;
    const unmoved = Math.abs(pov.lat - this.given.lat) < 1e-9 &&
      Math.abs(pov.lng - this.given.lng) < 1e-9 &&
      Math.abs(pov.altitude - this.given.altitude) < 1e-9;
    return unmoved ? this.handed : this.cameraOf(pov);
  }

  /** The globe's own rounding of its camera, non-empty only while Z is down. */
  diagnostics(): GlobeSeam {
    return this.seam;
  }

  // ---- the pairing ----------------------------------------------------

  /** The chart's camera as a point of view. */
  povOf(camera: ChartCamera): { lat: number; lng: number; altitude: number } | null {
    const equirect = this.equirect;
    if (!equirect) return null;
    const [lat, lng] = equirect.mapping.toLatLng(camera.x, -camera.y);
    return { lat, lng, altitude: altitudeForZoom(camera.zoom) };
  }

  /** A point of view as the chart's camera. The inverse of `povOf`, exactly. */
  cameraOf(pov: { lat: number; lng: number; altitude: number }): ChartCamera | null {
    const equirect = this.equirect;
    const context = this.context;
    if (!equirect || !context) return null;
    const [x, y] = equirect.mapping.toWorld(pov.lat, pov.lng);
    return { x, y: -y, zoom: zoomForAltitude(pov.altitude, this.ceiling()), rotation: 0 };
  }

  /** How deep the chart is willing to go, which is the pairing's own ceiling. */
  private ceiling(): number {
    const lens = this.context?.lens;
    return lens ? viewMaxZoom(lens) : WHOLE_CHART_ZOOM;
  }

  /**
   * One press of a zoom control, on the sphere.
   *
   * A press is one halving or doubling of the distance, read off the camera
   * where it stands rather than off a target this pane remembers -- two
   * presses in one tick therefore move the camera once, which is what the
   * recorded tour did and what its `globe-labels-held` altitude records.
   */
  changeZoom(delta: number): void {
    const globe = this.globe;
    if (!globe) return;
    const pov = globe.pointOfView();
    const standing = Number.isFinite(pov.altitude) ? pov.altitude : WHOLE_DISC_ALTITUDE;
    globe.pointOfView(
      { altitude: clamp(standing / 2 ** delta, NEAREST_ALTITUDE, FARTHEST_ALTITUDE) }, 180);
  }

  /** Where the camera is looking, for the corner locator. */
  facing(): { lat: number; lng: number } | null {
    const pov = this.globe?.pointOfView();
    return pov ? { lat: pov.lat, lng: pov.lng } : null;
  }

  // ---- building -------------------------------------------------------

  private build(context: WorldContext): void {
    const equirect = this.equirect;
    if (!equirect) return;
    const [x, y, width, height] = equirect.px;
    this.skin = new Skin({ x, y, width, height }, context.grid);
    this.texture = new THREE.CanvasTexture(this.skin.canvas);
    this.texture.colorSpace = THREE.SRGBColorSpace;

    this.globe = new Globe(this, { animateIn: false })
      .backgroundColor("#0b0e12")
      .showAtmosphere(true)
      .atmosphereColor("#7ea6c8")
      .atmosphereAltitude(0.12);
    const material = this.globe.globeMaterial() as THREE.MeshPhongMaterial;
    material.map = this.texture;
    material.needsUpdate = true;

    const scene = this.globe.scene();
    scene.add(this.pins);
    scene.add(this.labels);
    scene.add(this.cells);
    this.seam.labels.group = this.labels;
    this.seam.grid.group = this.cells;
    this.seam.detail.tiles = this.skin.tiles;

    this.buildSprites(context);
    this.globe.controls().addEventListener("change", () => this.moved());
    this.globe.onGlobeClick(({ lat, lng }) => this.pick(lat, lng));
  }

  /**
   * One sprite per pin, built once and afterwards only shown, hidden or
   * re-dressed.
   *
   * A pin on the sphere wears what the same pin wears on the chart: its
   * collection's mark in its collection's colour, rimmed with the world's
   * declared outset. Two thousand identical cyan beads say only "something is
   * here", which is the one thing a reader can already see.
   *
   * The materials are shared and cached per collection, so this mints two
   * thousand *sprites* and a handful of textures -- and nothing here is
   * disposed on a rebuild, because none of it belongs to a pin.
   */
  private buildSprites(context: WorldContext): void {
    this.pins.clear();
    this.sprites.clear();
    const equirect = this.equirect;
    const globe = this.globe;
    if (!equirect || !globe) return;
    const marks = markersOf(context);
    for (const point of context.model.points) {
      const [lat, lng] = equirect.mapping.toLatLng(point.coordinate[0], -point.coordinate[1]);
      const chosen = point.id === context.scene.selected;
      const mark = marks.get(point.collection.id) ?? BARE_MARKER;
      const sprite = new THREE.Sprite(markerMaterial(mark, chosen));
      const size = chosen ? PIN_SELECTED_SIZE : PIN_SIZE;
      sprite.scale.set(size, size, 1);
      const at = globe.getCoords(lat, lng, PIN_ALTITUDE);
      sprite.position.set(at.x, at.y, at.z);
      sprite.userData = {
        id: point.id, lat, lng, title: point.title, owner: point.collection.id,
      };
      this.pins.add(sprite);
      this.sprites.set(point.id, sprite);
    }
  }

  /**
   * Dress the standing sprites for the selection.
   *
   * The chosen pin grows and takes its white ring; the one it replaced settles
   * back among the rest. Both materials come out of the same cache the build
   * drew from, so a selection costs two lookups rather than two thousand
   * canvases.
   */
  private restyle(): void {
    const context = this.context;
    if (!context) return;
    const marks = markersOf(context);
    for (const held of this.sprites.values()) {
      const sprite = held as THREE.Sprite;
      if (!sprite.isSprite) continue;
      const stood = held.userData as { id?: string; owner?: number };
      const mark = marks.get(stood.owner ?? -1) ?? BARE_MARKER;
      const chosen = stood.id === context.scene.selected;
      sprite.material = markerMaterial(mark, chosen);
      const size = chosen ? PIN_SELECTED_SIZE : PIN_SIZE;
      sprite.scale.set(size, size, 1);
    }
  }

  /**
   * The names, while Z is held.
   *
   * The key is the sphere's own rounding of its camera — latitude, longitude,
   * altitude, the system and the cell being held — and it is the one written
   * form of the globe's camera the harness can read. Letting Z go drops every
   * name, and the key with it.
   */
  private drawLabels(): void {
    const context = this.context;
    const globe = this.globe;
    if (!context || !globe) return;
    const pov = globe.pointOfView();
    // The rounding is the contract: whole degrees and two decimals of
    // altitude, which is what every recorded `globe-labels-held` is written
    // in ("0:0:0.68:geohash:"). Finer rounding would make the key move where
    // the baseline says it stands still.
    const key = context.labelsHeld ? [
      String(Math.round(pov.lat)), String(Math.round(pov.lng)), pov.altitude.toFixed(2),
      // The system, whether or not a grid is open: which system *would*
      // divide this world is a property of the world, and the key the
      // baselines record carries it over a closed grid ("0:0:0.68:geohash:").
      context.system?.slug ?? cellSystems.systems[0]?.slug ?? "", context.cell,
    ].join(":") : "";
    if (key === this.labelKey) return;
    this.labelKey = key;
    this.seam.labels.key = key;
    // Every rebuild mints a card canvas per name and a texture over it. The
    // group being emptied is not the texture being freed, and a camera settling
    // through a flight rebuilds these dozens of times.
    release(this.labels);
    if (!context.labelsHeld) return;
    // WHICH names, not which collections. The budget is what a sphere can
    // carry; the camera is what decides who spends it.
    for (const near of labelCandidates(pov, this.standing(), LABEL_BUDGET)) {
      const at = globe.getCoords(near.lat, near.lng, LABEL_ALTITUDE);
      this.labels.add(nameCard(near.title, at));
    }
    // Cards are born visible, and a rebuild with the camera already elsewhere
    // must not leave the far side's names shining through the planet.
    this.cull();
  }

  /** The pins a name could be raised over: standing, placed and named. */
  private *standing(): Generator<Placed> {
    for (const held of this.sprites.values()) {
      if (!held.visible) continue;
      const stood = held.userData as Partial<Placed>;
      if (stood.title === undefined || stood.lat === undefined) continue;
      if (stood.lng === undefined) continue;
      yield { title: stood.title, lat: stood.lat, lng: stood.lng };
    }
  }

  /**
   * The horizon, enforced by hand.
   *
   * Cards and chips are drawn with the depth test off, because a screen-sized
   * card anchored on the ground loses its lower half to the planet's own curve
   * at any glancing angle. The price of that is a card on the far side of the
   * world shining straight through it, so the silhouette is applied here
   * instead: a point at the limb sits where the cosine of its angle from the
   * camera's axis equals the radius over the distance, and anything past that
   * is ground nobody can see.
   *
   * Only sprites, and only the two groups that turned their depth test off.
   * The pins keep theirs, so the sphere occludes them itself -- and the count
   * of standing sprites the baselines record stays a count of what the filters
   * left, never a reading of where the camera happens to be pointing.
   */
  private cull(): void {
    const camera = this.globe?.camera().position;
    if (!camera) return;
    for (const group of [this.cells, this.labels]) {
      for (const child of group.children) {
        if (!(child as THREE.Sprite).isSprite) continue;
        child.visible = facesCamera(child.position, camera);
      }
    }
  }

  /** The grid, from the same plan and the same tokens the chart draws. */
  private drawGrid(): void {
    const context = this.context;
    const globe = this.globe;
    const equirect = this.equirect;
    if (!context || !globe || !equirect) return;
    // Cleared *and released*, by the same call the names are released with.
    release(this.cells);
    // The fit key is not cleared when the grid closes. It is the last frame
    // the camera was flown to hold a cell, and the baselines carry it through
    // the grid closing, the pane being left, and the volume being reopened --
    // a record of where the reader was taken, not a live flag.
    if (!context.system || !context.scene.gridSystem) {
      // No grid at all is *nothing held*, which is a different answer from a
      // grid open over the whole ground and nothing chosen in it — and it is
      // the answer that makes the next descent a change worth turning for.
      this.seam.grid.cell = null;
      return;
    }
    this.seam.grid.cell = context.cell;
    // One cell, up to three objects, in the order the chart paints them: the
    // tint or the dim on the ground, the boundary over it, and the address on
    // a card in the corner. Which of the three a cell gets is the analysis
    // lane's answer and not this pane's -- the same `gridCellVisual` the chart
    // asks, handed the one thing only a renderer knows, which is whether the
    // address would fit inside the cell as the camera currently sees it.
    for (const cell of planOf(context)) {
      const rings = cellRings(context.ground, cell);
      const corners = cornersOf(rings, equirect.mapping);
      if (!corners) continue;
      const visual = gridCellVisual(context.ground, context.system, cell, {
        subgridVisible: context.subgridVisible,
        labelled: this.labelFits(cell, corners),
      });
      if (!visual) continue;
      if (visual.fill) {
        const [cx, cy] = context.system.on(context.ground).center(cell.hash);
        const [lat, lng] = equirect.mapping.toLatLng(cx, -cy);
        const fill = this.ringFill(rings, [lat, lng], visual.fill);
        if (fill) this.cells.add(fill);
      }
      for (const ring of rings) {
        const points = ring.map(([x, y]) => {
          const [lat, lng] = equirect.mapping.toLatLng(x, -y);
          const at = globe.getCoords(lat, lng, 0.01);
          return new THREE.Vector3(at.x, at.y, at.z);
        });
        if (points.length < 2) continue;
        this.cells.add(new THREE.Line(
          new THREE.BufferGeometry().setFromPoints(points),
          new THREE.LineBasicMaterial({
            color: visual.line.color, transparent: true, opacity: visual.line.opacity,
          }),
        ));
      }
      if (visual.label) this.cells.add(this.cellChip(visual.label, corners));
    }
    // The frame the camera was flown to hold the cell, which is what makes a
    // held cell reproducible between runs rather than wherever the reader was
    // when they chose it.
    // The fit key is a reading of depth, not of place: the system, the cell,
    // and the chart zoom the camera's distance reads as, in half steps. That
    // is what the baselines record ("geohash:m:7"), and it is what decides
    // when a chip could newly fit inside a cell's footprint.
    // Descending holds a cell, and holding it turns the planet: a cell chosen
    // on the far side of the world is a cell nobody can see. Only a *change*
    // of held cell turns the camera — opening the grid over the ground the
    // reader is already looking at moves nothing.
    if (this.heldCell !== null && this.heldCell !== context.cell) this.frameCell();
    this.heldCell = context.cell;
    // Chips are born visible, like the name cards, and are held to the same
    // horizon for the same reason.
    this.cull();
  }

  /**
   * Redraw the grid when the camera has moved far enough in depth that an
   * address could newly fit inside a cell, or stop fitting.
   *
   * The fit key is that reading of depth -- the system, the cell, and the
   * chart zoom the camera's distance reads as, in half steps -- and it is
   * written here and nowhere else, because it is a fact about where the camera
   * came to rest and a rebuild is not a camera move. It is never cleared: the
   * baselines carry it through the grid closing and the pane being left, as a
   * record of where the reader was taken.
   */
  private regridWhenFitChanges(): void {
    const context = this.context;
    const globe = this.globe;
    if (!context || !globe || this.hidden || !context.scene.gridSystem) return;
    const depth = Math.round(zoomForAltitude(globe.pointOfView().altitude, this.ceiling()) * 2);
    const key = `${context.scene.gridSystem}:${context.cell}:${depth}`;
    if (key === this.fitKey) return;
    this.fitKey = key;
    this.seam.grid.fitKey = key;
    this.drawGrid();
  }

  /**
   * Whether a cell's address fits inside the cell as the camera sees it.
   *
   * The chart asks this of an extent over a resolution; the sphere has no
   * resolution, so the ground-per-pixel is worked out from the camera's
   * distance and the field of view, and the answer feeds the same gate.
   */
  private labelFits(cell: PlanCell, corners: Corners): boolean {
    const globe = this.globe;
    if (!globe) return false;
    const size = cell.role === "neighbor"
      ? gridTheme.neighborLabelSizePx
      : gridTheme.labelSizePx;
    const pov = globe.pointOfView();
    const altitude = Math.max(pov.altitude, NEAREST_ALTITUDE / 2);
    const field = (50 * Math.PI) / 180;
    const groundPerPixel = (2 * altitude * GLOBE_RADIUS * Math.tan(field / 2)) /
      Math.max(1, this.clientHeight || 1);
    const degreePixels = ((GLOBE_RADIUS * Math.PI) / 180) / groundPerPixel;
    const width = Math.abs(corners.east - corners.west) * degreePixels;
    const height = Math.abs(corners.north - corners.south) * degreePixels;
    return measure(cell.hash, `900 ${size}px ${gridTheme.labelFont}`) + 9 <= width &&
      size + 6 <= height;
  }

  /**
   * A cell's tint or dim, laid on the ground.
   *
   * A fan of triangles from the cell's own middle out to its ring, each spoke
   * subdivided so the sheet follows the curve instead of sagging under it. The
   * spokes are walked on the sphere itself rather than in degrees, where an
   * unwrapped ring and a wrapped centre sit hundreds of degrees apart and a
   * pole cell's middle has no honest longitude at all.
   */
  private ringFill(
    rings: readonly (readonly (readonly [number, number])[])[],
    centre: readonly [number, number],
    fill: { color: string; opacity: number },
  ): THREE.Mesh | null {
    const globe = this.globe;
    const equirect = this.equirect;
    const ring = rings[0];
    if (!globe || !equirect || !ring || ring.length < 3) return null;
    const edge = ring.map(([x, y]) => {
      const [lat, lng] = equirect.mapping.toLatLng(x, -y);
      const at = globe.getCoords(lat, lng, 0.008);
      return [at.x, at.y, at.z] as [number, number, number];
    });
    const middle = globe.getCoords(centre[0], centre[1], 0.008);
    const radius = Math.hypot(middle.x, middle.y, middle.z) || 1;
    const positions: number[] = [middle.x, middle.y, middle.z];
    const rows = 4;
    for (let row = 1; row <= rows; row++) {
      const t = row / rows;
      for (const [x, y, z] of edge) {
        const px = middle.x + (x - middle.x) * t;
        const py = middle.y + (y - middle.y) * t;
        const pz = middle.z + (z - middle.z) * t;
        const lift = radius / (Math.hypot(px, py, pz) || 1);
        positions.push(px * lift, py * lift, pz * lift);
      }
    }
    const count = edge.length;
    const indices: number[] = [];
    for (let at = 0; at < count - 1; at++) indices.push(0, 1 + at, 2 + at);
    for (let row = 1; row < rows; row++) {
      const inner = 1 + (row - 1) * count;
      const outer = 1 + row * count;
      for (let at = 0; at < count - 1; at++) {
        indices.push(inner + at, outer + at, outer + at + 1);
        indices.push(inner + at, outer + at + 1, inner + at + 1);
      }
    }
    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute("position", new THREE.Float32BufferAttribute(positions, 3));
    geometry.setIndex(indices);
    return new THREE.Mesh(geometry, new THREE.MeshBasicMaterial({
      color: fill.color, transparent: true, opacity: fill.opacity,
      side: THREE.DoubleSide, depthWrite: false,
    }));
  }

  /**
   * A cell's address, on a card in its bottom-right corner.
   *
   * The prefix faint and the final character bright, exactly as the chart
   * writes it, and anchored by its own corner so it stays put on screen while
   * the planet turns under it.
   */
  private cellChip(
    label: { prefix: string; final: string; color: string; textAlpha: number;
      chip: string; sizePx: number },
    corners: Corners,
  ): THREE.Sprite {
    const globe = this.globe;
    const scale = 2;
    const size = label.sizePx * scale;
    const canvas = document.createElement("canvas");
    const paper = canvas.getContext("2d");
    const prefixFont = `500 ${size}px ${gridTheme.labelFont}`;
    const finalFont = `900 ${size}px ${gridTheme.labelFont}`;
    const pad = gridTheme.labelInsetPx * scale;
    let prefixWidth = 0;
    let finalWidth = size;
    if (paper) {
      paper.font = prefixFont;
      prefixWidth = label.prefix ? paper.measureText(label.prefix).width : 0;
      paper.font = finalFont;
      finalWidth = paper.measureText(label.final).width;
    }
    canvas.width = Math.ceil(prefixWidth + finalWidth + pad * 2);
    canvas.height = Math.ceil(size + pad * 1.4);
    if (paper) {
      paper.fillStyle = label.chip;
      paper.fillRect(0, 0, canvas.width, canvas.height);
      paper.textBaseline = "middle";
      const middle = canvas.height / 2 + scale;
      if (label.prefix) {
        paper.font = prefixFont;
        paper.fillStyle = label.color;
        paper.globalAlpha = gridTheme.prefixAlpha * label.textAlpha;
        paper.fillText(label.prefix, pad, middle);
      }
      paper.font = finalFont;
      paper.globalAlpha = label.textAlpha;
      paper.fillStyle = label.color;
      paper.fillText(label.final, pad + prefixWidth, middle);
      paper.globalAlpha = 1;
    }
    const texture = new THREE.CanvasTexture(canvas);
    texture.colorSpace = THREE.SRGBColorSpace;
    const sprite = new THREE.Sprite(new THREE.SpriteMaterial({
      map: texture, depthTest: false, depthWrite: false,
      transparent: true, sizeAttenuation: false,
    }));
    const insetLat = Math.abs(corners.north - corners.south) * 0.02;
    const insetLng = Math.abs(corners.east - corners.west) * 0.02;
    const at = globe?.getCoords(corners.south + insetLat, corners.east - insetLng, 0.02);
    if (at) sprite.position.set(at.x, at.y, at.z);
    const viewport = Math.max(1, this.clientHeight || 1);
    const height = canvas.height / scale / viewport;
    sprite.scale.set((height * canvas.width) / canvas.height, height, 1);
    sprite.center.set(1, 0);
    return sprite;
  }

  /**
   * Turn the planet to hold the chosen cell, the way the chart fits its view
   * when the navigator descends.
   *
   * The camera aims where the *system* says the cell's middle is — a pole
   * cap's middle is the pole, which no corner of its frame would have named —
   * and stands off by the cell's own span, so a cell fills the window whatever
   * depth it was reached at.
   */
  private frameCell(): void {
    const context = this.context;
    const globe = this.globe;
    const equirect = this.equirect;
    if (!context?.system || !globe || !equirect) return;
    const chosen = planOf(context)
      .find((cell) => cell.role === "scope" || cell.role === "leaf");
    if (!chosen) return;
    const [x, y] = context.system.on(context.ground).center(chosen.hash);
    const [lat, lng] = equirect.mapping.toLatLng(x, -y);
    let north = -90;
    let south = 90;
    let east = -180;
    let west = 180;
    for (const ring of cellRings(context.ground, chosen)) {
      for (const [ringX, ringY] of ring) {
        const [ringLat, ringLng] = equirect.mapping.toLatLng(ringX, -ringY);
        north = Math.max(north, ringLat);
        south = Math.min(south, ringLat);
        east = Math.max(east, ringLng);
        west = Math.min(west, ringLng);
      }
    }
    const span = Math.max(east - west, north - south);
    const altitude = clamp(span / 45, NEAREST_ALTITUDE, FARTHEST_ALTITUDE);
    globe.pointOfView({ lat, lng, altitude }, 400);
  }

  /**
   * Face the feature a card has just opened on.
   *
   * A ring drawn on the far side of a planet selects nothing anyone can see,
   * so a selection made while the sphere is up turns it — keeping the reader's
   * distance, because they chose a place and not a depth. A selection made
   * over the chart is remembered and not acted on: coming up to the sphere
   * afterwards lands on the chart's camera, which is where the reader was.
   */
  private face(id: string): void {
    const globe = this.globe;
    if (!globe || this.hidden || !id) return;
    const stood = this.sprites.get(id)?.userData as
      { lat?: number; lng?: number } | undefined;
    if (stood?.lat === undefined || stood.lng === undefined) return;
    globe.pointOfView(
      { lat: stood.lat, lng: stood.lng, altitude: globe.pointOfView().altitude }, 600);
  }

  private moved(): void {
    this.refreshDetail();
    if (this.context?.labelsHeld) this.drawLabels();
    this.regridWhenFitChanges();
    // The horizon moved with the camera even where nothing was rebuilt: the
    // memo on the label key and the one on the fit key both stand still
    // through a turn of the planet, and the silhouette does not.
    this.cull();
    const pov = this.globe?.pointOfView();
    if (pov && this.onCamera) this.onCamera(pov);
  }

  /**
   * Ask for the neighbourhood the camera is over.
   *
   * The level is the one whose tiles are about a quarter of what is on
   * screen: shallower and the deep capture is wasted, deeper and the sphere
   * asks for a hundred tiles to picture a continent.
   */
  private refreshDetail(): void {
    const context = this.context;
    const globe = this.globe;
    const equirect = this.equirect;
    if (!context?.lens || !globe || !equirect || !this.skin) return;
    const pov = globe.pointOfView();
    // The level the distance asks for, spoken through the pairing: one step
    // deeper than the chart zoom the camera reads as. Nothing is draped until
    // that is deeper than the skin already is -- a sphere seen whole is the
    // base skin and nothing else, which is what makes "past the base skin's
    // depth, tiles actually arrive" a statement the tour can check rather
    // than a description of every frame.
    const z = Math.min(
      Math.round(zoomForAltitude(pov.altitude, this.ceiling())) + 1,
      context.lens.maxZoom);
    if (z <= Math.max(TEXTURE_ZOOM, this.skin.baseLevel(context.lens))) {
      this.skin.clearDetail();
      return;
    }
    // How much ground the camera can possibly see: the horizon angle at this
    // altitude, padded by one tile so tiles arrive before their ground does,
    // then pulled in until the neighbourhood fits its budget.
    //
    // The block is named in tiles rather than measured in pixels. A tile is a
    // whole square of latitude and longitude at this level, so the camera
    // faces exactly one of them and the neighbourhood is the (2·reach+1)²
    // square around it: wrapped in longitude, because the sphere closes, and
    // clipped in latitude, because it does not.
    const columns = 2 ** z;
    const rows = columns / 2;
    const span = 360 / columns;
    const horizon = (Math.acos(1 / (1 + pov.altitude)) * 180) / Math.PI + span;
    let reach = Math.ceil(horizon / span);
    while ((2 * reach + 1) ** 2 > DETAIL_TILE_BUDGET && reach > 1) reach -= 1;
    const centreColumn = Math.floor(((((pov.lng + 180) % 360) + 360) % 360) / span);
    const centreRow = Math.floor((90 - pov.lat) / span);
    const wanted: [number, number][] = [];
    for (let down = -reach; down <= reach; down++) {
      const row = centreRow + down;
      if (row < 0 || row >= rows) continue;
      for (let across = -reach; across <= reach; across++) {
        wanted.push([(((centreColumn + across) % columns) + columns) % columns, row]);
      }
    }
    void this.skin.detail(
      context.lens, z, wanted,
      (level, x, y) => this.tileURL(level, x, y),
      () => this.refresh());
  }

  private refresh(): void {
    if (this.texture) this.texture.needsUpdate = true;
  }

  private tileURL(z: number, x: number, y: number): string | null {
    const context = this.context;
    const lens = context?.lens;
    if (!context || !lens) return null;
    const extension = lens.formats[z - lens.minZoom];
    if (!extension) return null;
    return `${context.base}/tiles/${lens.tiles}/${z}/${x}/${y}.${extension}`;
  }

  private pick(lat: number, lng: number): void {
    const context = this.context;
    const equirect = this.equirect;
    if (!context || !equirect) return;
    let best: { id: string; distance: number } | null = null;
    for (const point of context.visibility.standing()) {
      const [plat, plng] = equirect.mapping.toLatLng(point.coordinate[0], -point.coordinate[1]);
      const distance = (plat - lat) ** 2 + (plng - lng) ** 2;
      if (!best || distance < best.distance) best = { id: point.id, distance };
    }
    // Nothing near enough is a miss, and a miss says nothing at all.
    if (!best || best.distance > 4) return;
    // The same road the chart's picks take: the page renders the form, this
    // fills it and says so (data/report.ts). The sphere draws pins and only
    // pins, so the kind is settled here.
    reportPick({ feature: best.id, kind: "point" });
  }
}

/**
 * The cells to draw: the analysis lane's plan, in its frozen order, exactly
 * as the chart asks for it. Two panes, one plan, no second opinion.
 */
/** A cell's footprint in degrees, which is what a chip is placed by. */
interface Corners {
  readonly north: number;
  readonly south: number;
  readonly east: number;
  readonly west: number;
}

function cornersOf(
  rings: readonly (readonly (readonly [number, number])[])[],
  mapping: GeoMapping,
): Corners | null {
  let north = -Infinity;
  let south = Infinity;
  let east = -Infinity;
  let west = Infinity;
  for (const ring of rings) {
    for (const [x, y] of ring) {
      const [lat, lng] = mapping.toLatLng(x, -y);
      north = Math.max(north, lat);
      south = Math.min(south, lat);
      east = Math.max(east, lng);
      west = Math.min(west, lng);
    }
  }
  return Number.isFinite(north) ? { north, south, east, west } : null;
}

/** One shared canvas for measuring text: measuring is not drawing. */
let ruler: CanvasRenderingContext2D | null | undefined;

function measure(text: string, font: string): number {
  ruler ??= document.createElement("canvas").getContext("2d");
  if (!ruler) return text.length * 8;
  ruler.font = font;
  return ruler.measureText(text).width;
}

function planOf(context: WorldContext): PlanCell[] {
  if (!context.system) return [];
  return cellPlan(context.ground, context.system, context.cell);
}

/** A pin the camera could raise a name over. */
export interface Placed {
  readonly title: string;
  readonly lat: number;
  readonly lng: number;
}

/**
 * The great-circle separation of two places, in degrees.
 *
 * Which is what "near what the camera is looking at" means on a sphere: not
 * the difference of two latitudes and two longitudes, which calls the whole
 * arctic a neighbourhood.
 */
export function angularDistance(
  a: { lat: number; lng: number }, b: { lat: number; lng: number },
): number {
  const rad = Math.PI / 180;
  const inner = Math.sin(a.lat * rad) * Math.sin(b.lat * rad) +
    Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.cos((a.lng - b.lng) * rad);
  return (Math.acos(clamp(inner, -1, 1)) * 180) / Math.PI;
}

/**
 * The names worth raising: the nearest ones to what the camera faces, within
 * the reach and inside the budget.
 *
 * The budget is a decision about a sphere -- past a couple of hundred names a
 * planet is a word cloud -- and the ordering is a decision about a *reader*.
 * Spending it in collection order hands every card to whichever collection
 * happens to be rarest, wherever in the world it stands, and leaves the ground
 * under the camera bare.
 */
export function labelCandidates(
  pov: { lat: number; lng: number },
  standing: Iterable<Placed>,
  budget: number,
): Placed[] {
  const near: { placed: Placed; distance: number }[] = [];
  for (const placed of standing) {
    const distance = angularDistance(pov, placed);
    if (distance > LABEL_REACH_DEG) continue;
    near.push({ placed, distance });
  }
  near.sort((a, b) => a.distance - b.distance);
  return near.slice(0, budget).map((entry) => entry.placed);
}

/**
 * Whether a point on the sphere is on the side of it the camera can see.
 *
 * The limb is where the cosine of a point's angle from the camera's own axis
 * equals the radius over the camera's distance: nearer the axis than that and
 * the point is facing us, past it and the planet is in the way.
 */
export function facesCamera(
  anchor: { x: number; y: number; z: number },
  camera: { x: number; y: number; z: number },
  radius = GLOBE_RADIUS,
): boolean {
  const distance = Math.hypot(camera.x, camera.y, camera.z) || 1;
  const reach = Math.hypot(anchor.x, anchor.y, anchor.z) || 1;
  const facing = (anchor.x * camera.x + anchor.y * camera.y + anchor.z * camera.z) /
    (reach * distance);
  return facing > radius / distance;
}

/**
 * One name, on a card floated above its pin.
 *
 * The card is cut to the name: the text is measured in the font it will be
 * drawn in, and the canvas, the texture and the sprite's own aspect all follow
 * that one number. A fixed canvas clips every name longer than it and pads
 * every name shorter, and a fixed sprite scale then stretches whatever
 * survived to the same width regardless -- so "Olympus Mons" and "Tharsis
 * Tholus" came out the same size and neither was the size it asked for.
 */
export function nameCard(
  title: string, at: { x: number; y: number; z: number },
): THREE.Sprite {
  const width = Math.ceil(measure(title, CARD_FONT)) + CARD_PAD;
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = CARD_TALL;
  const paper = canvas.getContext("2d");
  if (paper) {
    paper.font = CARD_FONT;
    paper.textAlign = "center";
    paper.textBaseline = "middle";
    paper.fillStyle = "rgba(10, 13, 17, 0.78)";
    paper.fillRect(0, 0, width, CARD_TALL);
    paper.fillStyle = "#e6ebf0";
    paper.fillText(title, width / 2, 21);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  const sprite = new THREE.Sprite(new THREE.SpriteMaterial({
    // No depth test: a screen-sized card anchored on the ground loses its
    // lower half to the planet's curve at any glancing angle. `cull` puts the
    // horizon back by hand.
    map: texture, depthTest: false, depthWrite: false,
    sizeAttenuation: false, transparent: true,
  }));
  sprite.position.set(at.x, at.y, at.z);
  sprite.renderOrder = 4;
  sprite.scale.set((LABEL_HEIGHT * width) / CARD_TALL, LABEL_HEIGHT, 1);
  // Anchored at its bottom edge, so the card floats above the marker rather
  // than covering it.
  sprite.center.set(0.5, 0);
  return sprite;
}

/** What a collection's marker is, as the sphere has to draw it. */
export interface Marker {
  /** Where the collection's picture lives, or "" when it has none. */
  readonly icon: string;
  readonly picture: boolean;
  readonly color: string;
  /** The rim, already resolved from the world's declared outset token. */
  readonly outset: string;
  readonly title: string;
}

/**
 * What a pin wears when its collection is not in the world's own list.
 *
 * Which cannot happen through the model, and is a shared cache entry rather
 * than a fresh material anyway: a marker nobody can account for is still not
 * two thousand canvases nobody disposes.
 */
const BARE_MARKER: Marker = {
  icon: "", picture: false, color: "#4fb3d5", outset: outsetColor("light"), title: "",
};

/** Every collection's marker, in payload order, which is palette order. */
function markersOf(context: WorldContext): Map<number, Marker> {
  const outset = outsetColor(context.outset);
  const marker = (collection: Collection, ordinal: number): [number, Marker] => [
    collection.id,
    {
      icon: collection.iconAsset ? `${context.base}/icons/${collection.iconAsset}` : "",
      picture: Boolean(collection.iconPicture ||
        collection.attrs?.[KEY_ICON_KIND] === "picture"),
      color: collectionColor(collection, ordinal),
      outset,
      title: collection.title,
    },
  ];
  return new Map(context.model.collections.map(marker));
}

/**
 * One sprite material per marker and selection state, cached and shared.
 *
 * A material is what a pin *is*, not what a pin *has*: two thousand pins of
 * twenty collections are forty materials, and a filter or a selection swaps
 * which of the forty a sprite points at. The cache is therefore never
 * disposed -- entries outlive every sprite that wore them, which is the whole
 * point of them.
 */
export function markerMaterial(marker: Marker, selected: boolean): THREE.SpriteMaterial {
  const key = [marker.icon, marker.color, marker.outset, selected ? "ringed" : "plain"]
    .join(":");
  const held = markers.get(key);
  if (held) return held;
  // Pins keep one size on screen however close the camera comes, the way the
  // chart draws its markers: world-sized sprites become dinner plates from low
  // altitude.
  const material = new THREE.SpriteMaterial({ depthWrite: false, sizeAttenuation: false });
  markers.set(key, material);
  const dress = (image: HTMLImageElement | null): void => {
    material.map = markerTexture(marker, selected, image);
    material.needsUpdate = true;
  };
  if (!marker.icon) dress(null);
  else {
    const image = new Image();
    image.crossOrigin = "anonymous";
    image.onload = () => dress(image);
    // A collection whose picture never arrives wears its initials, which is
    // what a build with no icons at all draws.
    image.onerror = () => dress(null);
    image.src = marker.icon;
  }
  return material;
}

const markers = new Map<string, THREE.SpriteMaterial>();

/** The 80×80 canvas a marker is drawn on: a picture, or a name's initials. */
function markerTexture(
  marker: Marker, selected: boolean, image: HTMLImageElement | null,
): THREE.CanvasTexture {
  const canvas = document.createElement("canvas");
  canvas.width = MARKER_SIZE;
  canvas.height = MARKER_SIZE;
  const paper = canvas.getContext("2d");
  if (paper && image) {
    paper.drawImage(image, 8, 8, 64, 64);
    // A glyph is a silhouette the reader tints; a picture carries its own
    // colour, and flattening it would leave nothing but its outline filled in.
    if (!marker.picture) {
      paper.globalCompositeOperation = "source-in";
      paper.fillStyle = marker.color;
      paper.fillRect(0, 0, MARKER_SIZE, MARKER_SIZE);
      paper.globalCompositeOperation = "source-over";
    }
  } else if (paper) {
    const short = initialsOf(marker.title);
    paper.font = MARKER_FONT;
    paper.textAlign = "center";
    paper.textBaseline = "middle";
    paper.lineWidth = 6;
    paper.strokeStyle = marker.outset;
    paper.strokeText(short, 40, 41);
    paper.fillStyle = marker.color;
    paper.fillText(short, 40, 41);
  }
  if (paper && selected) {
    paper.beginPath();
    paper.arc(40, 40, 36, 0, Math.PI * 2);
    paper.lineWidth = 5;
    paper.strokeStyle = "#ffffff";
    paper.stroke();
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** A collection's name, cut down to what fits inside a marker. */
export function initialsOf(title: string): string {
  return title.split(/\s+/).slice(0, 2).map((part) => part[0] ?? "").join("");
}

/**
 * Drop a group's children and give the GPU back what they were holding.
 *
 * Dropping an object out of a group is not freeing what it owns: a texture
 * lives until something says so, and both the names and the grid mint one per
 * card on every rebuild. The grid is redrawn whenever the camera moves far
 * enough in depth to change what fits, and the names whenever it settles
 * anywhere new -- over one flight to a cell that is dozens of rebuilds of a
 * couple of hundred objects each.
 *
 * A sprite's geometry is three's own, shared by every sprite in the scene, so
 * it is the one thing here that is not this group's to give back.
 */
export function release(group: THREE.Group): void {
  for (const child of [...group.children]) {
    group.remove(child);
    const drawn = child as THREE.Mesh & {
      isSprite?: boolean;
      material?: THREE.Material & { map?: THREE.Texture | null };
    };
    if (!drawn.isSprite) drawn.geometry?.dispose?.();
    drawn.material?.map?.dispose?.();
    drawn.material?.dispose?.();
  }
}
