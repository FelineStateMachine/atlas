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
//   a sphere is a word cloud with a planet behind it.
//   Sprites are built once per pin and afterwards only shown or hidden, so a
//   filter costs a boolean per pin rather than a rebuild.
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
} from "@atlas/analysis/semconv/keys";
import { cellPlan, cellRings, equirectMapping } from "@atlas/analysis";
import type { GeoMapping, PlanCell } from "@atlas/analysis";
import { logger } from "../log.ts";
import type { WorldContext } from "../context.ts";
import type { Attrs } from "../data/payload.ts";
import { Skin } from "./texture.ts";

const log = logger("globe");

/** The altitude a whole hemisphere sits at: globe.gl's own default distance. */
const ALTITUDE_AT_FULL = 2.5;

/** Names raised over the sphere at once. */
const LABEL_BUDGET = 180;

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
    const pov = this.povOf(camera, viewport.height);
    if (!pov) return;
    this.handed = camera;
    this.given = pov;
    this.globe?.pointOfView(pov, 0);
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
   */
  leave(viewport: { width: number; height: number }): ChartCamera | null {
    this.hidden = true;
    const pov = this.globe?.pointOfView();
    // A globe nobody is looking at holds no pyramid tiles: the skin under it
    // stays, because it is one texture and recompositing it is the expensive
    // half of coming back.
    this.skin?.clearDetail();
    this.seam.detail.lens = "";
    if (!pov || !this.given || !this.handed) return null;
    const unmoved = Math.abs(pov.lat - this.given.lat) < 1e-9 &&
      Math.abs(pov.lng - this.given.lng) < 1e-9 &&
      Math.abs(pov.altitude - this.given.altitude) < 1e-9;
    return unmoved ? this.handed : this.cameraOf(pov, viewport.height);
  }

  /** The globe's own rounding of its camera, non-empty only while Z is down. */
  diagnostics(): GlobeSeam {
    return this.seam;
  }

  // ---- the pairing ----------------------------------------------------

  /** The chart's camera as a point of view. */
  povOf(camera: ChartCamera, height: number): { lat: number; lng: number; altitude: number } | null {
    const equirect = this.equirect;
    const context = this.context;
    if (!equirect || !context) return null;
    const [lat, lng] = equirect.mapping.toLatLng(camera.x, -camera.y);
    const resolution = context.grid.size / context.grid.tileSize / 2 ** camera.zoom;
    const degrees = (resolution * height / equirect.px[3]) *
      Math.abs(equirect.deg[1] - equirect.deg[3]);
    return { lat, lng, altitude: (degrees / 180) * ALTITUDE_AT_FULL };
  }

  /** A point of view as the chart's camera. The inverse of `povOf`, exactly. */
  cameraOf(
    pov: { lat: number; lng: number; altitude: number },
    height: number,
  ): ChartCamera | null {
    const equirect = this.equirect;
    const context = this.context;
    if (!equirect || !context) return null;
    const [x, y] = equirect.mapping.toWorld(pov.lat, pov.lng);
    const degrees = (pov.altitude / ALTITUDE_AT_FULL) * 180;
    const resolution = (degrees / Math.abs(equirect.deg[1] - equirect.deg[3])) *
      equirect.px[3] / height;
    const zoom = Math.log2(context.grid.size / context.grid.tileSize / resolution);
    return { x, y: -y, zoom, rotation: 0 };
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

  /** One sprite per pin, built once and afterwards only shown or hidden. */
  private buildSprites(context: WorldContext): void {
    this.pins.clear();
    this.sprites.clear();
    const equirect = this.equirect;
    const globe = this.globe;
    if (!equirect || !globe) return;
    for (const point of context.model.points) {
      const [lat, lng] = equirect.mapping.toLatLng(point.coordinate[0], -point.coordinate[1]);
      const sprite = new THREE.Mesh(
        new THREE.SphereGeometry(0.6, 6, 6),
        new THREE.MeshBasicMaterial({ color: 0x4fb3d5 }),
      );
      const at = globe.getCoords(lat, lng, 0.005);
      sprite.position.set(at.x, at.y, at.z);
      sprite.userData = { id: point.id, lat, lng };
      this.pins.add(sprite);
      this.sprites.set(point.id, sprite);
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
    if (!context.labelsHeld) {
      this.labels.clear();
      this.labelKey = "";
      this.seam.labels.key = "";
      return;
    }
    const pov = globe.pointOfView();
    const key = [
      pov.lat.toFixed(2), pov.lng.toFixed(2), pov.altitude.toFixed(3),
      context.scene.gridSystem, context.cell,
    ].join(":");
    if (key === this.labelKey) return;
    this.labelKey = key;
    this.seam.labels.key = key;
    this.labels.clear();
    const standing = [...context.visibility.standing()]
      .sort((a, b) => a.priority - b.priority)
      .slice(0, LABEL_BUDGET);
    const equirect = this.equirect;
    if (!equirect) return;
    for (const point of standing) {
      const [lat, lng] = equirect.mapping.toLatLng(point.coordinate[0], -point.coordinate[1]);
      const sprite = new THREE.Sprite(new THREE.SpriteMaterial({
        map: textTexture(point.title), depthTest: false, transparent: true,
      }));
      const at = globe.getCoords(lat, lng, 0.02);
      sprite.position.set(at.x, at.y, at.z);
      sprite.scale.set(18, 5, 1);
      this.labels.add(sprite);
    }
  }

  /** The grid, from the same plan and the same tokens the chart draws. */
  private drawGrid(): void {
    const context = this.context;
    const globe = this.globe;
    const equirect = this.equirect;
    if (!context || !globe || !equirect) return;
    this.cells.clear();
    this.seam.grid.cell = context.cell || null;
    if (!context.system || !context.scene.gridSystem) {
      this.seam.grid.fitKey = "";
      return;
    }
    for (const cell of planOf(context)) {
      for (const ring of cellRings(context.ground, cell)) {
        const points = ring.map(([x, y]) => {
          const [lat, lng] = equirect.mapping.toLatLng(x, -y);
          const at = globe.getCoords(lat, lng, 0.01);
          return new THREE.Vector3(at.x, at.y, at.z);
        });
        if (points.length < 2) continue;
        this.cells.add(new THREE.Line(
          new THREE.BufferGeometry().setFromPoints(points),
          new THREE.LineBasicMaterial({ color: 0x9fd3e6, transparent: true, opacity: 0.7 }),
        ));
      }
    }
    // The frame the camera was flown to hold the cell, which is what makes a
    // held cell reproducible between runs rather than wherever the reader was
    // when they chose it.
    if (context.cell) {
      const pov = globe.pointOfView();
      this.fitKey = `${context.scene.gridSystem}:${context.cell}@${pov.lat.toFixed(2)},${pov.lng.toFixed(2)}`;
      this.seam.grid.fitKey = this.fitKey;
    } else {
      this.seam.grid.fitKey = "";
    }
  }

  private moved(): void {
    this.refreshDetail();
    if (this.context?.labelsHeld) this.drawLabels();
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
    const degrees = (pov.altitude / ALTITUDE_AT_FULL) * 180;
    const [, , w, h] = equirect.px;
    const span = Math.abs(equirect.deg[1] - equirect.deg[3]);
    const worldHeight = (degrees / span) * h;
    const wanted = Math.round(Math.log2(context.grid.size / Math.max(1, worldHeight))) + 2;
    // Nothing is draped until the camera asks for more than the skin already
    // has. A sphere seen whole is the base skin and nothing else, which is
    // what makes "past the base skin's depth, tiles actually arrive" a
    // statement the tour can check rather than a description of every frame.
    if (wanted <= this.skin.baseLevel(context.lens)) {
      this.skin.clearDetail();
      return;
    }
    const z = Math.min(context.lens.maxZoom, wanted);
    const [cx, cy] = equirect.mapping.toWorld(pov.lat, pov.lng);
    const width = (worldHeight * w) / h;
    void this.skin.detail(
      context.lens, z,
      { x: cx - width / 2, y: cy - worldHeight / 2, width, height: worldHeight },
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
    if (!best || best.distance > 4) return;
    this.dispatchEvent(new CustomEvent("atlas:pick", {
      bubbles: true, composed: true, detail: { feature: best.id, kind: "point" },
    }));
  }
}

/**
 * The cells to draw: the analysis lane's plan, in its frozen order, exactly
 * as the chart asks for it. Two panes, one plan, no second opinion.
 */
function planOf(context: WorldContext): PlanCell[] {
  if (!context.system) return [];
  return cellPlan(context.ground, context.system, context.cell);
}

/** A name as a texture, which is the cheapest sprite a label can be. */
function textTexture(text: string): THREE.CanvasTexture {
  const canvas = document.createElement("canvas");
  canvas.width = 256;
  canvas.height = 64;
  const paper = canvas.getContext("2d");
  if (paper) {
    paper.font = "600 28px ui-sans-serif, system-ui, sans-serif";
    paper.textAlign = "center";
    paper.textBaseline = "middle";
    paper.lineWidth = 6;
    paper.strokeStyle = "rgba(6, 9, 14, 0.86)";
    paper.strokeText(text, 128, 32);
    paper.fillStyle = "#f2f5f9";
    paper.fillText(text, 128, 32);
  }
  return new THREE.CanvasTexture(canvas);
}
