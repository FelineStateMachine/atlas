// `<atlas-viewport>` — the host of the two panes, and the seam's whole edge.
//
// It does four things and delegates everything else:
//
//   READS THE SCENE. One watcher over `#atlas-viewport-state`, surviving the
//   morph swaps that replace it (`scene/observe.ts`).
//   LOADS. The world payload, the packed locations, through the one data
//   module. Payloads are kept per build, so switching worlds back and forth
//   costs nothing after the first visit.
//   RECONCILES. Model, standing set, ground, cell system: computed once and
//   handed to both panes, so the chart and the sphere cannot disagree.
//   HOLDS THE EPHEMERAL. Z, the hover, which pane is up, and the camera —
//   the continuous half of issue #5 §4.1, none of which is the session's.
//
// Two things leave: the pick, as a DOM event the page's glue submits, and the
// settled camera, debounced. Nothing else, ever.

import { applicableSystems, cellSystems } from "@atlas/analysis";
import type { CellSystem } from "@atlas/analysis";
import { logger } from "./log.ts";
import { DataPlane } from "./data/plane.ts";
import { reportCamera } from "./data/report.ts";
import type { Catalog, Lens, TileGrid } from "./data/payload.ts";
import { SceneWatcher } from "./scene/observe.ts";
import type { Scene, SceneChange } from "./scene/read.ts";
import { WorldModel, worldGrid } from "./world/model.ts";
import { Visibility } from "./world/visibility.ts";
import type { WorldContext } from "./context.ts";
import { AtlasChart } from "./chart/element.ts";
import { AtlasGlobe } from "./globe/element.ts";
import { cellTest } from "./chart/grid.ts";

const log = logger("viewport");

export class AtlasViewport extends HTMLElement {
  private readonly plane = new DataPlane();
  private watcher: SceneWatcher | null = null;
  private catalog: Catalog | null = null;
  private readonly worlds = new Map<string, Promise<WorldModel>>();
  private context: WorldContext | null = null;
  private generation = 0;
  private globeUp = false;

  connectedCallback(): void {
    const selector = this.getAttribute("state-src") || "#atlas-viewport-state";
    this.watcher = new SceneWatcher(selector, (scene, change) => {
      void this.apply(scene, change);
    });
    this.chart?.attach(this.plane, (camera) => {
      const scene = this.watcher?.scene;
      if (!scene?.volume) return;
      void reportCamera({ volume: scene.volume, world: scene.world, ...camera });
    });
    this.wireKeys();
    this.wireGlobeToggle();
    this.watcher.start();
  }

  disconnectedCallback(): void {
    this.watcher?.stop();
  }

  /** The one after-swap hook: re-resolve the scene node and re-read it. */
  rescan(): void {
    this.watcher?.rescan();
    this.wireGlobeToggle();
  }

  /** The panes, looked up rather than held: a morph may not touch them, but
   * an application that renders one and not the other is still a page. */
  get chart(): AtlasChart | null {
    return this.querySelector("atlas-chart");
  }

  get globe(): AtlasGlobe | null {
    return this.querySelector("atlas-globe");
  }

  /** What the seam publishes about itself. Read by the diagnostics module. */
  get current(): WorldContext | null {
    return this.context;
  }

  get sphereUp(): boolean {
    return this.globeUp;
  }

  // ---- reconciling ----------------------------------------------------

  private async apply(scene: Scene, change: SceneChange): Promise<void> {
    if (!scene.volume || !scene.base || !scene.world) return;
    const mine = ++this.generation;
    try {
      const grid = await this.tileGrid(scene);
      if (!grid) return;
      const model = await this.model(scene, grid);
      // A scene that moved again while a payload was in flight wins: the last
      // thing the reader asked for is the thing to draw.
      if (mine !== this.generation) return;

      const lens = model.payload.lenses[this.lensIndex(model, scene)] ?? null;
      const ground = model.ground(lens);
      const system = this.system(scene, ground);
      const test = system ? cellTest(ground, system, scene.gridCell) : null;
      const held = this.context;
      const context: WorldContext = {
        scene,
        base: scene.base,
        grid: worldGrid(grid, model.payload),
        model,
        lens,
        lensIndex: this.lensIndex(model, scene),
        outset: outsetOf(model),
        ground,
        system,
        cell: scene.gridCell,
        subgridVisible: scene.subgrid > 0,
        visibility: new Visibility(model, scene, lens?.shard ?? 0, test,
          change.world ? null : held?.hovered ?? null),
        labelsHeld: change.world ? false : held?.labelsHeld ?? false,
        hovered: change.world ? null : held?.hovered ?? null,
      };
      this.context = context;
      this.chart?.show(context);
      if (AtlasGlobe.offers(model.payload.attrs ?? {})) this.globe?.show(context);
    } catch (error) {
      log.error("the scene could not be drawn", {
        op: "render", volume: scene.volume, world: scene.world, error: String(error),
      });
    }
  }

  /** Recompute the standing set and restyle, without touching the payloads. */
  private refresh(): void {
    const context = this.context;
    if (!context) return;
    const test = context.system
      ? cellTest(context.ground, context.system, context.cell)
      : null;
    context.visibility = new Visibility(
      context.model, context.scene, context.lens?.shard ?? 0, test, context.hovered);
    this.chart?.restyle();
    this.globe?.update();
  }

  private lensIndex(model: WorldModel, scene: Scene): number {
    const named = model.payload.lenses.findIndex((lens: Lens) => lens.name === scene.lens);
    if (named >= 0) return named;
    return Math.min(Math.max(scene.lensIndex, 0), Math.max(0, model.payload.lenses.length - 1));
  }

  private system(scene: Scene, ground: ReturnType<WorldModel["ground"]>): CellSystem | null {
    if (!scene.gridSystem) return null;
    const offered = applicableSystems(ground);
    return offered.find((candidate) => candidate.slug === scene.gridSystem) ??
      cellSystems.get(scene.gridSystem) ?? null;
  }

  private async tileGrid(scene: Scene): Promise<TileGrid | null> {
    this.catalog ??= await this.plane.catalog();
    const volume = this.catalog.volumes.find((entry) => entry.slug === scene.volume);
    if (!volume) {
      log.warn("the catalog does not list the volume the page is about", {
        op: "render", volume: scene.volume,
      });
      return null;
    }
    return volume.tileGrid;
  }

  private model(scene: Scene, grid: TileGrid): Promise<WorldModel> {
    const key = `${scene.base}/${scene.world}`;
    const held = this.worlds.get(key);
    if (held) return held;
    const building = (async () => {
      const [payload, table] = await Promise.all([
        this.plane.world(scene.base, scene.world),
        this.plane.locations(scene.base, scene.world),
      ]);
      return new WorldModel(scene.world, payload, worldGrid(grid, payload), table);
    })();
    this.worlds.set(key, building);
    return building;
  }

  // ---- the ephemeral half ---------------------------------------------

  /**
   * Z is held, not switched.
   *
   * The reader wants every name for as long as they are looking for one, and
   * then wants the map back. Holding says both in one gesture: there is no
   * state left on by mistake and no control to find first.
   */
  private wireKeys(): void {
    const held = (down: boolean) => (event: KeyboardEvent) => {
      if (event.key !== "z" && event.key !== "Z") return;
      const context = this.context;
      if (!context || context.labelsHeld === down) return;
      context.labelsHeld = down;
      this.chart?.restyle();
      this.globe?.update();
      const hint = document.querySelector("#labels-hint");
      if (hint) hint.textContent = down ? "Z · labels shown" : "Z · hold for labels";
    };
    window.addEventListener("keydown", held(true));
    window.addEventListener("keyup", held(false));
  }

  /**
   * Which pane is up is seam-local.
   *
   * The application renders the toggle — it knows whether the world declares
   * a sphere — and the seam wires it, because flipping panes moves no
   * discrete state: it is the same world, the same filters and the same
   * camera, seen from a different distance.
   */
  private wireGlobeToggle(): void {
    const toggle = document.querySelector<HTMLButtonElement>("#globe-toggle");
    if (!toggle || toggle.dataset["atlasWired"] === "yes") return;
    toggle.dataset["atlasWired"] = "yes";
    toggle.addEventListener("click", () => this.flip());
  }

  private flip(): void {
    const globe = this.globe;
    const chart = this.chart;
    const toggle = document.querySelector<HTMLButtonElement>("#globe-toggle");
    if (!globe || !chart) return;
    const size = { width: this.clientWidth, height: this.clientHeight };
    if (this.globeUp) {
      const camera = globe.leave(size);
      if (camera) chart.goTo(camera.x, camera.y, camera.zoom, camera.rotation);
      chart.hidden = false;
      this.globeUp = false;
    } else {
      const camera = chart.camera();
      chart.hidden = true;
      if (camera) globe.enter(camera, size);
      this.globeUp = true;
    }
    toggle?.setAttribute("aria-pressed", String(this.globeUp));
    this.refresh();
  }
}

function outsetOf(model: WorldModel): string {
  return model.payload.attrs?.["atlas.icon.outset"] ?? "";
}
