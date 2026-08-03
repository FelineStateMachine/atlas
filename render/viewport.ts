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
// Three things leave, all of them as DOM events the page's glue submits: the
// pick, the cell a surface was pointed at, and the settled camera, debounced.
// Nothing else, ever.

import { KEY_ICON_OUTSET } from "@atlas/analysis/semconv/keys";
import { applicableSystems, cellSystems, geohashCellAt } from "@atlas/analysis";
import type { CellSystem, Coordinate, Ground, LocatedCell } from "@atlas/analysis";
import { logger } from "./log.ts";
import { wireKeyboard } from "./keys.ts";
import { RowHover } from "./hover.ts";
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
  // How to take the keyboard off again. Held rather than forgotten because a
  // custom element can be disconnected and reconnected -- a swap that moved
  // it, a test that reused it -- and window listeners do not leave with it.
  private unkey: (() => void) | null = null;
  // And the same for the legend's pointer: which row it is over is continuous
  // state nobody posts, and the tracker's listeners live and die with this
  // element (`hover.ts`).
  private hover: RowHover | null = null;

  connectedCallback(): void {
    const selector = this.getAttribute("state-src") || "#atlas-viewport-state";
    this.watcher = new SceneWatcher(selector, (scene, change) => {
      void this.apply(scene, change);
    });
    this.chart?.attach(this.plane, (camera) => {
      const scene = this.watcher?.scene;
      if (!scene?.volume) return;
      reportCamera({ volume: scene.volume, world: scene.world, ...camera });
    });
    this.unkey?.();
    this.unkey = wireKeyboard(this);
    this.hover?.stop();
    this.hover = new RowHover();
    this.hover.start();
    this.wireGlobeToggle();
    this.wireZoom();
    this.wireGridInput();
    // The sphere's camera, written where a camera can be read: the corner
    // locator's rectangle, which is the one form the globe's view ever takes
    // outside its own scene graph.
    const globe = this.globe;
    if (globe) globe.onCamera = (pov) => this.locate(pov);
    this.watcher.start();
  }

  disconnectedCallback(): void {
    this.watcher?.stop();
    this.unkey?.();
    this.unkey = null;
    this.hover?.stop();
    this.hover = null;
  }

  /** The one after-swap hook: re-resolve the scene node and re-read it. */
  rescan(): void {
    this.watcher?.rescan();
    this.wireGlobeToggle();
    this.wireZoom();
    // The navigator is a region and a swap replaces it whole, so the field the
    // last swap left behind is not the field on screen.
    this.wireGridInput();
    // The legend is a region too, and the one that moves most: every filtering
    // move re-renders it. The list on screen may be a new node with no
    // listener on it, or the same node carrying a mark from before the swap on
    // a row the pointer is no longer over -- one pass answers both.
    this.hover?.rescan();
    // The footer's sentence is half the application's and half this lane's,
    // and a swap that re-rendered the legend has just put the application's
    // half back on screen. A concern that touches the legend and not the
    // scene node -- folding a section, unfolding every one of them -- moves
    // nothing this seam watches, so nothing would otherwise tell it to
    // finish the sentence again.
    // ...unless there is no window to count over, which is the chart's case
    // behind the sphere: the sentence on screen is then the last true thing it
    // said, and that is what the reader is left reading.
    if (!this.sphereUp) this.chart?.writeCount();
    // And the same duty for the corner: the shelf hides itself when the whole
    // map is on screen, and a swap that re-rendered it has just un-hidden it.
    this.chart?.redrawOverview();
    // The open card is the same shape of duty as the footer's sentence: the
    // application renders the row and leaves it empty, and only this lane can
    // say what a point's address is.
    this.writeCell();
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
    // BEFORE ANYTHING IS FETCHED, because a sphere left standing over a world
    // that has none is the whole of the defect: the reader switches volumes,
    // the page comes back a plane, and the planet on screen goes on wearing
    // the skin of the world they left. The application says which surface this
    // world declares in the scene itself (`data-surface`), so the pane can be
    // put right in the same tick the scene moved rather than a payload later
    // -- and if the payload never lands, the sphere is still down.
    if (scene.surface !== "sphere") this.dropSphere();
    const mine = ++this.generation;
    try {
      const grid = await this.tileGrid(scene);
      if (!grid) return;
      const worldTitle = this.catalog?.volumes
        .find((entry) => entry.slug === scene.volume)?.worlds
        .find((world) => world.slug === scene.world)?.title ?? scene.world;
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
        worldTitle,
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
      // The world's own answer, which is the one that decides: a world may
      // declare a sphere and still flatten in a way no globe can invert, and
      // only the payload knows. Asked before the chart is shown, so a chart
      // coming back up is measured against a pane that is already on screen.
      const sphere = AtlasGlobe.offers(model.payload.attrs ?? {});
      if (!sphere) this.dropSphere();
      this.chart?.show(context);
      if (sphere) this.globe?.show(context);
      // The card's rows are written from the model, so they cannot be written
      // before there is one: a rescan runs the moment the swap lands, and the
      // payload it is about may still be in flight. This is the same duty
      // asked again on the other side of the wait.
      this.writeCell();
    } catch (error) {
      log.error("the scene could not be drawn", {
        op: "render", volume: scene.volume, world: scene.world, error: String(error),
      });
    }
  }

  /**
   * Recompute what stands and repaint both panes, without touching the
   * payloads.
   *
   * `recount` is what tells a filter from a pane flip. A filter changed what
   * is drawn and the footer owes a new sentence; flipping panes changed
   * nothing about the world and the sentence on screen is still the last true
   * thing the chart said about it -- writing it again would only ask a pane
   * with no window how much of the map it can see.
   */
  private refresh({ recount = true } = {}): void {
    const context = this.context;
    if (!context) return;
    // WRAPPED, because everything inside it asks a cell system a question and
    // a cell system is allowed to refuse. S2 throws outright on a ground with
    // no invertible flattening rather than shrugging -- `appliesTo` is the
    // question a caller was supposed to ask first (analysis/cellsystems/s2.ts)
    // -- and the guard in `system` below is the asking. This is the second
    // fence: a refusal that got past it costs one repaint and a line in the
    // log, and never the map. A reader whose grid is wrong can still read
    // their world; a reader whose repaint threw has a page that answers
    // nothing at all.
    try {
      const test = context.system
        ? cellTest(context.ground, context.system, context.cell)
        : null;
      context.visibility = new Visibility(
        context.model, context.scene, context.lens?.shard ?? 0, test, context.hovered);
      this.chart?.restyle();
      if (recount) this.chart?.writeCount();
      this.globe?.update();
    } catch (error) {
      log.error("the arrangement could not be repainted", {
        op: "render", volume: context.scene.volume, world: context.scene.world,
        system: context.system?.slug ?? "", cell: context.cell, error: String(error),
      });
    }
  }

  private lensIndex(model: WorldModel, scene: Scene): number {
    const named = model.payload.lenses.findIndex((lens: Lens) => lens.name === scene.lens);
    if (named >= 0) return named;
    return Math.min(Math.max(scene.lensIndex, 0), Math.max(0, model.payload.lenses.length - 1));
  }

  /**
   * The system dividing this world, and only if it can.
   *
   * THE FALLBACK WAS THE BUG. A system named in the session that this ground
   * does not offer used to be looked up in the registry and handed back
   * anyway, which is the one answer that cannot be given: the systems are
   * exact rather than approximate, and one asked about a ground it refused
   * throws on the first question the grid puts to it. So the offered set is
   * the whole of the answer -- `applicableSystems` is `appliesTo` over the
   * registry -- and a name outside it draws no grid and says so once.
   *
   * The server refuses such a value too (internal/app/session.go, applyGrid),
   * and that is not a reason to trust it here: a record written by an older
   * build, or a hand-edited session, arrives at this lane all the same, and
   * the pane that would throw is this one.
   */
  private system(scene: Scene, ground: ReturnType<WorldModel["ground"]>): CellSystem | null {
    if (!scene.gridSystem) return null;
    const offered = applicableSystems(ground);
    const found = offered.find((candidate) => candidate.slug === scene.gridSystem);
    if (found) return found;
    log.warn("the session names a system this world cannot be divided by", {
      op: "render", volume: scene.volume, world: scene.world, system: scene.gridSystem,
      offered: offered.map((candidate) => candidate.slug).join(","),
      known: cellSystems.get(scene.gridSystem) !== undefined,
    });
    return null;
  }

  /**
   * The open card's cell addresses.
   *
   * WHOSE HALF THIS IS. Where a point stands is the application's -- the card
   * renders the coordinates out of the payload -- and *what that place is
   * called* is the analysis lane's, which the server has no access to. So the
   * card is rendered with the row present and empty, hidden, and this fills
   * it: the same division `writeCount` makes over the footer's sentence, and
   * the same lifecycle, because the card is an outerMorph region and every
   * swap replaces it whole. There is nothing to keep in step -- a swap takes
   * the last answer away with the node it was written on, and the rescan that
   * follows writes this one.
   *
   * GEOHASH IS THE FIRST ROW AND IT IS FIXED. The address shown is the one at
   * depth 3, not at whatever depth the grid currently stands: the row says
   * where this point *is*, and a reader descending the navigator did not ask
   * for every open card to be re-spelled behind them. Every other system this
   * ground offers adds a row of its own, in registry order, out of the one
   * question the contract has for a point (`locate`).
   *
   * The rows are inert text. Nothing here is a control, and the reference's
   * were not either.
   */
  private writeCell(): void {
    const context = this.context;
    const field = document.querySelector<HTMLElement>("#detail-cell-field");
    const value = document.querySelector<HTMLElement>("#detail-cell");
    // The strays go first and unconditionally. Every other row on the card is
    // the server's and leaves with the swap that replaced it; these are this
    // lane's, and a card that has just become a shape's -- or nobody's -- must
    // not keep the last point's second address under it.
    for (const row of [...document.querySelectorAll("[data-cell-system-row]")]) row.remove();
    // No card on the page at all, which is most of the application's states.
    if (!field || !value) return;
    // THE LIVE SCENE, not the reconciled one. A rescan runs the moment the
    // swap lands and the reconcile it starts is a payload away; the context
    // still names the selection *before* this swap, and writing that here
    // would put the last point's address on the card now open about another.
    // The model is the same model either way -- a selection does not change
    // the world -- and a selection this one has never heard of resolves to
    // nothing, which is a hidden row until the reconcile fills it.
    const selected = this.watcher?.scene.selected ?? context?.scene.selected ?? "";
    const point = context?.model.pointByID.get(selected) ?? null;
    if (!context || !point) {
      value.textContent = "";
      field.hidden = true;
      return;
    }
    const address = geohashCellAt(context.ground, point.coordinate);
    value.textContent = address;
    field.hidden = address === "";
    if (!address) return;
    let after: Element = field;
    for (const system of applicableSystems(context.ground)) {
      if (system.slug === "geohash") continue;
      const located = this.addressOf(system, context.ground, point.coordinate);
      if (!located) continue;
      const row = cellRow(system.slug, located.label, located.value);
      after.after(row);
      after = row;
    }
  }

  /**
   * One system's name for a point, or nothing.
   *
   * Fenced for the reason every call into a system on this element is fenced:
   * the systems are exact rather than approximate, and S2 asked about a ground
   * with no invertible flattening throws outright. `applicableSystems` above
   * is the asking that should make this unreachable; this is the second fence,
   * and it costs a row and a line on the stream rather than the card.
   */
  private addressOf(
    system: CellSystem, ground: Ground, at: Coordinate,
  ): LocatedCell | null {
    try {
      return system.on(ground).locate(at);
    } catch (error) {
      log.warn("the system cannot name the place this card is about", {
        op: "render", system: system.slug, error: String(error),
      });
      return null;
    }
  }

  /**
   * The navigator's field, kept spellable.
   *
   * THE SERVER STAYS AUTHORITATIVE. What a cell address is, and whether the
   * text has become one, is decided where the session is written; this is
   * display assist and nothing more -- the field shows the reader only what
   * their system could keep of what they typed, so a capital or a stray
   * character does not sit in the box looking like part of an address.
   *
   * IN THE CAPTURE PHASE, so the value is normalized before the route bound to
   * this same field reads it: one keystroke, one posted value, and the two
   * halves of the page agree about what was typed. It never stops the event --
   * the field's own `input` trigger is the application's and must fire exactly
   * as it did -- and it rewrites nothing when there is nothing to rewrite,
   * which is every keystroke that was already an address.
   */
  private wireGridInput(): void {
    const field = document.querySelector<HTMLInputElement>("#grid-input");
    if (!field || wired.has(field)) return;
    wired.add(field);
    // The live viewport, not this one: a swap can carry a control across while
    // the element it was wired to is no longer the element on the page.
    field.addEventListener("input", () => {
      (document.querySelector<AtlasViewport>("atlas-viewport") ?? this).normalizeCell(field);
    }, { capture: true });
  }

  /**
   * Rewrite what is in the field as the active system spells it, keeping the
   * caret where the reader left it.
   *
   * The caret is counted rather than kept: how many characters of what stands
   * *before* it survive normalization is where it belongs afterwards, which is
   * the one arithmetic that holds however many characters were dropped and
   * wherever in the text they were. Typing at the end -- which is nearly all
   * typing -- lands it at the end, as it would have anyway.
   *
   * Public because the listener above resolves the live element and calls it.
   */
  normalizeCell(field: HTMLInputElement): void {
    const context = this.context;
    if (!context?.system) return;
    const raw = field.value;
    let on;
    try {
      on = context.system.on(context.ground);
    } catch (error) {
      log.warn("the active system cannot spell an address on this ground", {
        op: "render", system: context.system.slug, error: String(error),
      });
      return;
    }
    const shown = on.normalizeInput(raw);
    // Nothing to say. This is the path the recorded tours take -- "m" is
    // already what geohash keeps of "m" -- so the field, the event and the
    // caret are all left exactly as they were found.
    if (shown === raw) return;
    const caret = field.selectionStart ?? raw.length;
    const before = on.normalizeInput(raw.slice(0, caret)).length;
    field.value = shown;
    field.setSelectionRange?.(before, before);
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
   *
   * Which key does this, when a repeat counts and what happens when the window
   * loses focus mid-hold all belong to `keys.ts`; what is here is the half only
   * this element can do -- the flag both panes read, and the sentence in the
   * corner that says the flag is up.
   */
  holdLabels(down: boolean): void {
    const context = this.context;
    if (!context || context.labelsHeld === down) return;
    context.labelsHeld = down;
    this.chart?.restyle();
    this.globe?.update();
    const hint = document.querySelector("#labels-hint");
    if (hint) hint.textContent = down ? "Z · labels shown" : "Z · hold for labels";
  }

  /**
   * One step of zoom, wherever the ask came from.
   *
   * The buttons beside the map and the +/- keys are the same interaction, and
   * which pane is up decides what a step means: a zoom level on the chart, a
   * halving of distance on the sphere. Public because both callers are
   * outside -- the button handlers may outlive an instance, and the keyboard
   * is a module.
   */
  zoomBy(delta: number): void {
    if (this.globeUp) this.globe?.changeZoom(delta);
    else this.chart?.nudgeZoom(delta);
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
    if (!toggle) return;
    // Which pane is up is this lane's state, and the application renders the
    // button that says so -- with `aria-pressed="false"`, because the server
    // has no way of knowing. So a swap that re-renders the topbar puts the
    // button back to "chart" while the sphere is still on screen, and the
    // next press reads as a first press. The state is re-asserted onto the
    // control after every swap, which is the price of the control belonging
    // to one side and the state to the other.
    toggle.setAttribute("aria-pressed", String(this.globeUp));
    if (wired.has(toggle)) return;
    wired.add(toggle);
    // The handler resolves the live viewport rather than closing over this
    // one. A morph swap can carry a control across intact while the element it
    // was wired to is no longer the element on the page, and a toggle bound to
    // a viewport nobody is looking at flips nothing and reports nothing.
    toggle.addEventListener("click", () => {
      (document.querySelector<AtlasViewport>("atlas-viewport") ?? this).flipPane();
    });
  }

  /** Swap the panes. Public because the toggle's handler may outlive an
   * instance and has to be able to reach whichever one is live. */
  flipPane(): void {
    const globe = this.globe;
    const chart = this.chart;
    const toggle = document.querySelector<HTMLButtonElement>("#globe-toggle");
    if (!globe || !chart) return;
    const size = { width: this.clientWidth, height: this.clientHeight };
    if (this.globeUp) {
      const camera = globe.leave();
      if (camera) chart.goTo(camera.x, camera.y, camera.zoom, camera.rotation);
      chart.hidden = false;
      this.globeUp = false;
      // THE CAMERA IS CHECKED WHERE IT FRONTS, and this is the second half of
      // the boundary the sphere's write-back is the first half of. Behind the
      // sphere the chart went on being moved by things that do not need a
      // window -- a filter, a lens, a cell descended into -- and the fit a
      // descent makes there is made against a window of no size, so it lands
      // at the deepest level the lens has whatever the reader was looking at.
      // While the pane was away that was nobody's business. It becomes the
      // reader's here, and it is the *only* moment it can be asked without
      // moving what the recorded globe steps pin: a sphere handing back
      // nothing at all (`leave` refusing a camera that is not a place) leaves
      // the chart standing on that camera, and this is what looks at it.
      chart.front();
      // Over the chart the camera is in the snapshot proper, so the locator
      // goes back to reading the map rather than being told.
      chart.locate(null);
    } else {
      const camera = chart.camera();
      chart.hidden = true;
      // Which pane is up is set *before* the sphere comes up, because coming
      // up is the sphere's first camera event and the corner locator refuses
      // to follow a globe that is not up yet. Told afterwards, the mark was
      // right on a first entry -- where the tiles arriving raised more events
      // -- and stale on every entry after it.
      this.globeUp = true;
      // Retiring a sphere forgets its camera callback along with everything
      // else, so a globe going up re-learns where its camera reports -- a
      // wire asserted only at connect time is a wire a retire has cut.
      globe.onCamera = (pov) => this.locate(pov);
      if (camera) globe.enter(camera, size);
    }
    toggle?.setAttribute("aria-pressed", String(this.globeUp));
    this.refresh({ recount: false });
  }

  /**
   * Put the sphere down, because this world has none.
   *
   * THE PANE IS SEAM STATE AND THE WORLD IS NOT. Which pane is up survives a
   * swap on purpose — it is continuous interaction state, and a filter must
   * not drop the reader back to the chart — but a world change is exactly the
   * moment that stops being right: the new world may be a game map, and the
   * sphere standing over it belongs to the world before. Left alone, that is
   * what the reader saw: Night City opened while a planet went on turning,
   * still wearing Mars's skin, with only the toggle quietly hiding itself to
   * say the offer had been withdrawn.
   *
   * So the chart comes back up, the toggle is put down, and the sphere is
   * retired — the same teardown a disconnect does, plus the context, because
   * this element is not about that world any more. No camera is carried back:
   * a camera is a place on a particular world, and the sphere's was a place on
   * the one being left (`AtlasGlobe.retire`).
   *
   * Idempotent, and it has to be: it is asked on every scene change a plane
   * world sees, and only the first one has anything to put down.
   */
  private dropSphere(): void {
    if (this.globeUp) {
      this.globeUp = false;
      const chart = this.chart;
      if (chart) {
        chart.hidden = false;
        // The same fronting duty as `flipPane`, and owed more here: no camera
        // is handed back at all on this path, so whatever the chart was left
        // standing on behind the sphere is what the reader is about to be
        // looking through.
        chart.front();
        // Over the chart the camera is in the snapshot proper, so the locator
        // goes back to reading the map rather than being told.
        chart.locate(null);
      }
      document.querySelector<HTMLButtonElement>("#globe-toggle")
        ?.setAttribute("aria-pressed", "false");
    }
    this.globe?.retire();
  }

  /**
   * The globe's camera, marked on the chart's own surface.
   *
   * A point, not an extent. On the sphere half the world is always out of
   * sight, so the honest thing for the corner to say is where the camera is
   * looking; the locator draws a fixed mark there, which is why every
   * recorded globe step carries a 22-pixel box that never changes size
   * however close the camera comes.
   */
  private locate(pov: { lat: number; lng: number; altitude: number }): void {
    const globe = this.globe;
    if (!globe || !this.globeUp) return;
    const camera = globe.cameraOf(pov);
    if (!camera) return;
    this.chart?.locate([camera.x, camera.y]);
  }

  /**
   * The zoom controls.
   *
   * Zoom is continuous interaction state, so it is the seam's (issue #5
   * §4.1), and the two buttons are the application's chrome the way the globe
   * toggle is: it renders them because they belong beside the map, and this
   * lane wires them because pressing one moves nothing discrete. Which pane
   * is up decides what a press means -- a zoom level on the chart, a halving
   * of distance on the sphere.
   */
  private wireZoom(): void {
    for (const [id, delta] of [["#zoom-in", 1], ["#zoom-out", -1]] as const) {
      const button = document.querySelector<HTMLButtonElement>(id);
      if (!button || wired.has(button)) continue;
      wired.add(button);
      button.addEventListener("click", () => {
        (document.querySelector<AtlasViewport>("atlas-viewport") ?? this).zoomBy(delta);
      });
    }
  }
}

/**
 * The controls this lane has already wired.
 *
 * WHY A SET AND NOT A MARK ON THE ELEMENT. It was a `data-` attribute, and
 * that is the one place a mark cannot be kept: a morph rewrites an element's
 * attributes from the server's markup, which has never heard of it, so the
 * mark was rubbed off every swap while the listener it recorded stayed
 * attached. The next rescan wired the same button again, and again, until one
 * press of the globe toggle flipped the panes four times and left them where
 * they started — reachable by hand, unreachable at the end of a tour, and the
 * parity of a count deciding which. Held here, the fact belongs to the lane
 * that owns it and no swap can reach it.
 *
 * Weak, so a control that leaves the page leaves this with it.
 */
const wired = new WeakSet<Element>();

/**
 * One system's row for the open card, in the card's own shape.
 *
 * A bare `<div><dt>…</dt><dd>…</dd></div>`, which is what every other row in
 * that list is: the carried assets/css/pin-detail.css lays the definition list
 * out from the divs and asks nothing of them, so a row this lane inserts and a
 * row the server rendered are the same row to the stylesheet. The mark is how
 * the next pass finds it again, and it is the only thing that distinguishes
 * them.
 *
 * Text, not markup: a cell address is a value, and `textContent` is the one
 * way to put a value on a page without asking what is in it.
 */
function cellRow(slug: string, label: string, value: string): HTMLElement {
  const row = document.createElement("div");
  row.setAttribute("data-cell-system-row", slug);
  const term = document.createElement("dt");
  term.textContent = label;
  const said = document.createElement("dd");
  said.textContent = value;
  row.append(term, said);
  return row;
}

function outsetOf(model: WorldModel): string {
  return model.payload.attrs?.[KEY_ICON_OUTSET] ?? "";
}
