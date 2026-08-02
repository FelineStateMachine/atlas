// The behaviour-parity tour, re-pointed at the rewritten application.
//
// This is `frontend/parity/tour.js` with one half replaced and one half
// untouched, and the split is the whole point of the exercise:
//
//   THE STEPS ARE UNTOUCHED. Every step name, every order, every interaction
//   and every self-check below is the reference tour's, character for
//   character where the DOM allowed it. A step is a stable identifier and
//   renaming one is a breaking change to the contract
//   (`golden/parity/SCHEMA.md` §2), so none is renamed, dropped or added.
//
//   THE READING IS RE-POINTED. The page underneath is a different page: the
//   panes are `<atlas-chart>` and `<atlas-globe>` rather than `#map` and
//   `#globe`, the locator's shelf is `#atlas-overview`, the arrangement is a
//   server record published as a JSON island rather than a `localStorage`
//   key, and the empty-library card is a page of its own rather than a hidden
//   region. Each of those is a different way of asking the same question, and
//   every one of them is written down where it is asked.
//
// WHAT MAY AND MAY NOT MOVE. The values compared are golden. A read may be
// re-pointed at the element that now carries the fact; a read may not be
// re-defined to produce whatever the new build happens to say. Where a value
// genuinely cannot be equal -- the reference's hash route, the ordinal a lens
// used to be addressed by -- the difference is declared in
// `golden/waivers.json` with a reason, never absorbed here.
//
// The tour is injected by `run.mjs` rather than built into the application:
// it is harness code, and the application under test is required to carry no
// client JavaScript of its own beyond the seam's boot module (docs/app.md
// §4.4). It publishes `window.__atlasAppDiagnostics`, which is the hook the
// seam merges its own half into (docs/render-seam.md §8.1) -- the server's
// contribution to that half is the island, and the rest is read off the
// rendered page, which is exactly the split issue #5 §6 asks for.

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const tourQuery = (selector) => document.querySelector(selector);


// ---- the application's half of the snapshot ---------------------------

// The island: the arrangement the page was rendered from, in the golden's own
// key names (docs/app.md §6). It is re-read on every snapshot because a swap
// replaces it, and it is parsed leniently because a page with no island is a
// page with no volume open rather than a broken one.
function islandOf() {
  try {
    return JSON.parse(tourQuery("#atlas-session-island")?.textContent || "null") ||
      { last: null, entry: null };
  } catch {
    return { last: null, entry: null };
  }
}

const textOf = (selector) => tourQuery(selector)?.textContent?.trim() ?? "";
const shown = (element) => Boolean(element) && !element.hidden;

// The words the application renders about what the map is drawing. The seam
// answers `drawn` and `listable` from the model; these are the same facts as
// they reached the screen, which is where a surface that forgot to recount
// shows up.
function syncOf() {
  const flag = tourQuery("#dock-flag");
  return {
    footerText: textOf("#visible-count"),
    dockText: textOf("#dock-count"),
    dockFlag: shown(flag) ? flag.textContent.trim() : null,
    dockRows: document.querySelectorAll("#dock-results .search-result").length,
    searching: Boolean(tourQuery("#pin-search")?.value),
  };
}

// The chrome's own state, read where it is rendered rather than where it is
// stored: a build whose model moved and whose page did not is wrong, and this
// is where it shows.
function uiOf(island) {
  const entry = island.entry;
  const chip = tourQuery("#solo-chip");
  const detail = tourQuery("#atlas-detail");
  return {
    sidebarCollapsed: Boolean(tourQuery("#atlas-shell")?.classList.contains("sidebar-collapsed")),
    detailOpen: Boolean(detail && detail.children.length > 0),
    detailTitle: textOf("#detail-title") || null,
    searchQuery: tourQuery("#pin-search")?.value ?? "",
    searchResultsVisible: shown(tourQuery("#dock-results")),
    soloChip: shown(chip) ? chip.textContent.trim() : null,
    visibleCountText: textOf("#visible-count"),
    overviewDocked: entry?.overviewDocked ?? false,
    dockFolded: entry?.dockFolded ?? false,
    dockDismissed: entry?.dockDismissed ?? false,
    subgridVisible: tourQuery("#subgrid-toggle")?.getAttribute("aria-pressed") === "true",
  };
}

// The application's half, published under the name the seam merges. The seam
// owns everything only a renderer can know and wins every key it answers;
// these are the keys it cannot.
window.__atlasAppDiagnostics = () => {
  const island = islandOf();
  const volumeSelect = tourQuery("#volume-select");
  return {
    volume: volumeSelect?.selectedOptions?.[0]?.textContent?.trim() ?? "",
    filters: {
      hiddenCategories: [...(island.entry?.hidden ?? [])],
      collapsedSections: [...(island.entry?.collapsed ?? [])],
    },
    ui: uiOf(island),
    sync: syncOf(),
    // Which shape the reader has open. The seam answers what is drawn; this
    // answers what is being read, which is the session's business.
    zones: { focused: focusedZone() },
  };
};

// The ground the reader last went to, by title.
//
// RE-POINTED, and the re-pointing is the whole of what this read is about.
// The reference held a `focusedZoneID` that a jump set and only a rebuilt
// ground cleared -- closing the card put the selection down and left the index
// still marking where the reader had been -- and it published that id here.
// Reading the card instead would answer null the moment a card closes, which
// is a different question from the one the baselines recorded.
//
// The new page carries the same fact where the reference carried it on screen:
// the row of the feature index the jump marked as current. So that is what is
// asked. Nothing is redefined -- the value is still the title of the ground
// the reader went to -- only the element carrying it has changed.
function focusedZone() {
  const current = document.querySelector("#legend .zone-index-item.is-current");
  return current?.querySelector("span:last-child")?.textContent?.trim() || null;
}

// ---- what the harness observes for itself -----------------------------

async function waitForBoot() {
  for (let i = 0; i < 300; i += 1) {
    if (window.__atlasDebug && !tourQuery("#map-loading")?.checkVisibility?.()) break;
    await sleep(100);
  }
}

// What the harness can see of the pane the reader is looking at. The chart
// says where it stands through the diagnostics object; the globe says so
// through the seam it opens for exactly this -- counts of what it has drawn,
// never a handle on it -- and through the overview's reticle, which is the
// globe's camera written out in pixels on a surface the chart shares. A build
// with no globe at all still answers every key here, so the shape of a step
// never depends on which pane happens to be up.
//
// RE-POINTED: the two panes are custom elements now (`<atlas-chart>`,
// `<atlas-globe>`) rather than two divs, and the locator's shelf is the
// region container `#atlas-overview`. The questions asked of them -- is this
// one hidden, is that one there at all -- are the reference tour's questions.
function paneOf() {
  const toggle = tourQuery("#globe-toggle");
  const seam = window.__atlasGlobe;
  const box = tourQuery("#overview-viewport")?.style;
  const firstTile = seam ? [...seam.detail.tiles.keys()].sort()[0] : undefined;
  return {
    globeOffered: toggle ? !toggle.hidden : false,
    globeActive: globeActive(toggle),
    chartHidden: Boolean(tourQuery("atlas-chart")?.hidden),
    globeHidden: tourQuery("atlas-globe") ? Boolean(tourQuery("atlas-globe").hidden) : true,
    globeBuilt: Boolean(seam),
    detailLens: seam ? seam.detail.lens || "" : "",
    detailTiles: seam ? seam.detail.tiles.size : 0,
    detailZoom: firstTile ? Number(firstTile.split("/")[0]) : null,
    gridCells: seam?.grid.group ? seam.grid.group.children.length : 0,
    gridCell: seam ? seam.grid.cell ?? null : null,
    gridFitKey: seam ? seam.grid.fitKey || "" : "",
    labelKey: seam ? seam.labels.key || "" : "",
    labelSprites: seam?.labels.group ? seam.labels.group.children.length : 0,
    pinSprites: seam ? seam.sprites.size : 0,
    visibleSprites: seam
      ? [...seam.sprites.values()].filter((sprite) => sprite.visible).length
      : 0,
    overviewShelfHidden: Boolean(tourQuery("#atlas-overview")?.hidden),
    reticle: globeActive(toggle) && box
      ? [box.left, box.top, box.width, box.height].map(pixel).join(" ")
      : "",
  };
}

function globeActive(toggle) {
  return toggle?.getAttribute("aria-pressed") === "true";
}

function pixel(value) {
  const number = Number.parseFloat(value);
  return Number.isFinite(number) ? Math.round(number) : 0;
}

// The library as the chrome offers it: which volumes, worlds and lenses are
// on the selects, and whether the two ways into an import are open.
//
// RE-POINTED: the empty-library card is a page of its own now rather than a
// region hidden inside the explorer, so on an explorer page there is no
// `#empty-state` element at all. An absent card is a card that is not being
// shown, which is the same fact the reference recorded as `hidden`, so that
// is what is recorded -- the question is unchanged and the element carrying
// the answer is not there to be asked.
function libraryOf() {
  const values = (selector) => [...(tourQuery(selector)?.options || [])].map((o) => o.value);
  const emptyState = tourQuery("#empty-state");
  return {
    volumes: values("#volume-select"),
    volumeValue: tourQuery("#volume-select")?.value ?? null,
    worlds: values("#world-select"),
    worldValue: tourQuery("#world-select")?.value ?? null,
    lenses: values("#lens-select"),
    lensValue: tourQuery("#lens-select")?.value ?? null,
    lensFieldHidden: Boolean(tourQuery("#lens-field")?.hidden),
    emptyStateHidden: emptyState ? Boolean(emptyState.hidden) : true,
    addBundlesDisabled: Boolean(tourQuery("#add-bundles")?.disabled),
    emptyOpenDisabled: Boolean(tourQuery("#empty-open")?.disabled),
  };
}

// The label-policy ladder as it was actually rendered. Each toggle says
// whether its collection's names are speaking; a surface that flipped the
// model and forgot to repaint the button shows up here.
function labelsOf() {
  const speaking = [];
  const silent = [];
  for (const button of document.querySelectorAll("[data-label-toggle]")) {
    const id = button.dataset.labelToggle;
    (button.getAttribute("aria-pressed") === "true" ? speaking : silent).push(id);
  }
  speaking.sort();
  silent.sort();
  return { speaking, silent };
}

// The saved arrangement.
//
// RE-POINTED: the reference wrote it to `localStorage` under
// `atlas.session.v3` and the rewrite keeps it on the server, one record per
// volume, published back to the page as the JSON island. Where it lives is
// the application's business; what it contains is not, and the island is
// rendered in the golden's own key names and roundings for exactly this read
// (docs/app.md §6). The rounding the reference did here is therefore already
// done, and doing it twice would be harmless but dishonest about where it
// happened.
function sessionOf() {
  const stored = islandOf();
  if (!stored.entry) return { last: stored.last ?? null, entry: null };
  const entry = stored.entry;
  return {
    last: stored.last ?? null,
    entry: {
      volume: entry.volume,
      world: entry.world,
      lens: entry.lens,
      center: entry.center ?? null,
      zoom: entry.zoom ?? null,
      hidden: [...(entry.hidden || [])],
      collapsed: [...(entry.collapsed || [])],
      expanded: [...(entry.expanded || [])],
      labels: [...(entry.labels || [])],
      overviewDocked: entry.overviewDocked ?? null,
      dockFolded: entry.dockFolded ?? null,
      dockDismissed: entry.dockDismissed ?? null,
    },
  };
}

function observe() {
  return {
    pane: paneOf(),
    library: libraryOf(),
    labels: labelsOf(),
    session: sessionOf(),
    route: location.hash,
  };
}

// A step is settled when tiles stop arriving, the view stops moving, and the
// page has stopped answering. The pane's own readings are part of the key: on
// the globe the chart's view never moves, so a step that flew the camera
// across the planet would otherwise read as settled before any of it had
// happened.
//
// RE-POINTED: an interaction is a round trip now, so the rendered words are
// part of the key too -- a step is not over until the partial that answers it
// has landed.
async function settle() {
  let previous = "";
  let stable = 0;
  for (let i = 0; i < 150; i += 1) {
    await sleep(120);
    await new Promise((resolve) => requestAnimationFrame(resolve));
    window.advanceTime?.();
    const s = window.__atlasDebug.snapshot();
    const pane = paneOf();
    const key = JSON.stringify([
      s.zoom, s.center, s.resolution, s.tileStats,
      pane.detailTiles, pane.detailZoom, pane.gridCells, pane.gridFitKey,
      pane.labelKey, pane.labelSprites, pane.visibleSprites, pane.reticle,
      syncOf(), islandOf(),
    ]);
    if (key === previous && reported(s)) {
      stable += 1;
      if (stable >= 2) return;
    } else {
      stable = 0;
    }
    previous = key;
  }
}

/**
 * Whether the camera the chart is standing at has reached the record.
 *
 * The reference kept the arrangement in the browser, so the camera was in it
 * the moment the view stopped moving. The rewrite keeps it on the server: the
 * seam reports a settled camera once, debounced, and the record comes back as
 * the state island. A step is therefore not over when the view stops -- it is
 * over when the page has finished saying so, and this is that question asked
 * of the two halves at once.
 *
 * A world with no camera reported at all is answered true rather than waited
 * on: not every step of every volume has a chart standing under it.
 */
function reported(snapshot) {
  const entry = islandOf().entry;
  if (!entry || !snapshot || snapshot.zoom == null || !snapshot.center) return true;
  if (entry.center === null || entry.zoom === null) return false;
  return Math.abs(entry.zoom - Number(snapshot.zoom.toFixed(3))) < 1e-9 &&
    entry.center[0] === Math.round(snapshot.center[0]) &&
    entry.center[1] === Math.round(snapshot.center[1]);
}

function keydown(key, options = {}) {
  window.dispatchEvent(new KeyboardEvent("keydown", {
    key, bubbles: true, cancelable: true, ...options,
  }));
}

// A key pressed AT something, rather than at the window.
//
// The tour's own shortcuts are dispatched at the window because that is where
// a keystroke with no focused control is heard, and the application's
// shortcuts listen there (internal/app/templates/shell.tmpl). A reader typing
// into a field is the other case entirely: their keystroke starts at the
// field and travels up, so a shortcut that does not ask where the key came
// from hears it. That is a real difference in the event and it can only be
// exercised by dispatching where the reader's own key would land.
function keydownAt(selector, key, options = {}) {
  tourQuery(selector)?.dispatchEvent(new KeyboardEvent("keydown", {
    key, bubbles: true, cancelable: true, ...options,
  }));
}

function keyup(key, options = {}) {
  window.dispatchEvent(new KeyboardEvent("keyup", {
    key, bubbles: true, cancelable: true, ...options,
  }));
}

function viewportKeydown(key) {
  tourQuery("#map").dispatchEvent(new KeyboardEvent("keydown", {
    key, bubbles: true, cancelable: true,
  }));
}

function change(select, value) {
  select.value = value;
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

function type(selector, value) {
  const input = tourQuery(selector);
  if (!input) return;
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

// One filter, three surfaces: the canvas draws it, the footer counts it, the
// dock lists it. These checks catch a single build where they never agreed in
// the first place. Read straight off the rendered text, so a surface that
// forgot to recount fails here even though the model behind it is right.
const countPattern = /^([\d,]+) of ([\d,]+) features? in view$/;
const dockPattern = /^([\d,]+) features?$/;
const readCount = (text) => Number(text.replace(/,/g, ""));

function checkSync(name, snapshot) {
  const sync = snapshot.sync;
  const problems = [];
  const complain = (message) => problems.push(`${name}: ${message}`);

  if (sync.footerText === "No features shown") {
    if (sync.drawn !== 0) complain(`footer says nothing is shown while ${sync.drawn} features draw`);
  } else {
    const footer = countPattern.exec(sync.footerText);
    if (!footer) complain(`footer text "${sync.footerText}" is not a count`);
    else {
      const inView = readCount(footer[1]);
      const shownCount = readCount(footer[2]);
      if (shownCount !== sync.drawn) {
        complain(`footer counts ${shownCount} features shown, the map draws ${sync.drawn}`);
      }
      if (inView > shownCount) complain(`footer has ${inView} of ${shownCount} in view`);
    }
  }

  const dock = dockPattern.exec(sync.dockText);
  if (!dock) complain(`dock text "${sync.dockText}" is not a count`);
  else if (readCount(dock[1]) !== sync.listable) {
    complain(`dock counts ${readCount(dock[1])} features, ${sync.listable} are listable`);
  }
  const expectedRows = Math.min(sync.listable, 100);
  if (sync.dockRows !== expectedRows) {
    complain(`dock shows ${sync.dockRows} rows for ${sync.listable} listable features`);
  }
  if (sync.listable > sync.drawn) {
    complain(`dock can list ${sync.listable} features from a map drawing ${sync.drawn}`);
  }
  return problems;
}

/**
 * Whether what the map says it is drawing is actually on screen.
 *
 * The tour is a count, a flag and a string all the way down, and it was
 * possible for every one of them to be right while the page showed an empty
 * rectangle: the application's own backdrop was painted over the pane, so the
 * renderer drew a world nobody could see and every field in this file agreed
 * that it had. This is deliberately not a pixel comparison -- there is no
 * golden to compare against and there should not be. It asks two questions a
 * blank page answers wrongly: is the pane's own canvas the thing at the
 * middle of the map, and does it have anything but one flat colour on it.
 *
 * It runs once, on the first step, because it is about how the page is put
 * together rather than about what the reader just did.
 */
function checkCanvas(name, snapshot) {
  if (snapshot.sync.drawn === 0) return [];
  const pane = tourQuery(snapshot.pane.globeActive ? "atlas-globe" : "atlas-chart");
  const canvas = pane?.querySelector("canvas");
  if (!canvas) return [`${name}: the pane has no canvas to draw on`];
  const box = canvas.getBoundingClientRect();
  const problems = [];
  const middle = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
  if (middle !== canvas && !canvas.contains(middle)) {
    problems.push(`${name}: ${middle?.tagName}#${middle?.id} covers the map's own canvas`);
  }
  try {
    const paper = canvas.getContext("2d", { willReadFrequently: true });
    if (paper) {
      const width = Math.min(canvas.width, 160);
      const height = Math.min(canvas.height, 160);
      const pixels = paper.getImageData(
        Math.max(0, (canvas.width - width) / 2),
        Math.max(0, (canvas.height - height) / 2), width, height).data;
      const seen = new Set();
      for (let i = 0; i < pixels.length; i += 4) {
        seen.add(`${pixels[i]},${pixels[i + 1]},${pixels[i + 2]}`);
        if (seen.size > 4) break;
      }
      if (seen.size <= 1) {
        problems.push(`${name}: the map draws ${snapshot.sync.drawn} features onto one flat colour`);
      }
    }
  } catch {
    // A tainted or WebGL canvas cannot be read; the covering check stands.
  }
  return problems;
}

function cameraOf(snapshot) {
  return {
    center: (snapshot.center || []).map((value) => Math.round(value)),
    zoom: Number((snapshot.zoom || 0).toFixed(3)),
  };
}

function sameCamera(a, b) {
  return JSON.stringify(cameraOf(a)) === JSON.stringify(cameraOf(b));
}

// Reconciling with whatever the catalog now holds.
//
// RE-POINTED: the reference opened `__atlasDebug.refreshCatalog()` for the
// harness. The rewrite has no such seam and needs none -- the page already
// re-reads its own URL whenever the library moves under it (docs/app.md §5),
// so the harness raises the event the application listens for rather than
// calling into it. That drives the real reconcile through the real wiring,
// which is more than the reference's hook did.
async function refreshCatalog() {
  const response = await fetch("/data/catalog.json", { cache: "no-store" });
  await response.json();
  tourQuery("#atlas-events")?.dispatchEvent(new CustomEvent("catalog", { bubbles: true }));
  await sleep(400);
}

async function tour(options = {}) {
  const steps = [];
  const problems = [];
  const shots = [];
  const complain = (message) => problems.push(message);
  const record = async (name, extra = null) => {
    // A walk that stalls should say where. The runner reads these back off
    // the page rather than off a console, so a step that never settles is
    // reported as the step it was rather than as a timeout with no name.
    // Where the walk is, for whoever is watching from outside. A tour of
    // sixty steps takes minutes, and the step a stall happened on is the only
    // useful thing to know about a stall.
    window.__atlasTourAt = name;
    await settle();
    // `extra` is how the three new step kinds say what they drove: the aim a
    // pick took, the element a keystroke left the focus on, the picture a
    // screenshot step wrote. It is merged into the snapshot rather than kept
    // beside it so that it is compared like everything else -- and it is
    // passed per step rather than added to `observe()` so that the steps the
    // reference captured keep the exact shape they were captured in.
    const snapshot = { ...JSON.parse(window.render_game_to_text()), ...observe(), ...(extra ?? {}) };
    steps.push({ name, snapshot });
    problems.push(...checkSync(name, snapshot));
    if (steps.length === 1) problems.push(...checkCanvas(name, snapshot));
    return snapshot;
  };

  // A baseline is captured per volume, so the caller names which volume this
  // run is about; with nothing named it is the first on offer, as ever.
  await waitForBoot();
  const gameSelect = tourQuery("#volume-select");
  const offered = [...gameSelect.options].map((option) => option.value);
  const firstGame = offered.includes(options.volume) ? options.volume : offered[0];
  change(gameSelect, firstGame);
  await record("initial");

  // Search, select the first result, read the detail panel, close it.
  type("#pin-search", "a");
  await record("search-a");
  const firstResult = tourQuery(".search-result");
  if (firstResult) {
    firstResult.click();
    await record("search-select-first");
    tourQuery("#close-detail")?.click();
    await record("detail-closed");
  }
  type("#pin-search", "");
  await record("search-cleared");

  // Category filtering: hide the first category, then solo it, then clear.
  const firstCategory = tourQuery(".category-row input");
  if (firstCategory) {
    firstCategory.click();
    await record("category-hidden");
    tourQuery(".category-row input").click();
    await record("category-restored");
  }
  const firstOnly = tourQuery(".only-button");
  if (firstOnly) {
    firstOnly.click();
    await record("solo-first");
    const chip = tourQuery("#solo-chip");
    if (chip && !chip.hidden) chip.click();
    await record("solo-cleared");
  }

  // Section folding and the bulk visibility actions.
  const firstSection = tourQuery("#legend .layer-title");
  if (firstSection) {
    firstSection.click();
    await record("section-folded");
    tourQuery("#expand-all").click();
    await record("sections-unfolded");
  }
  tourQuery("#hide-all").click();
  await record("all-hidden");
  tourQuery("#show-all").click();
  await record("all-shown");

  // Zones: jump to the first zone, highlight it, clear the highlight.
  const firstZone = tourQuery(".zone-index-item");
  if (firstZone) {
    firstZone.click();
    await record("zone-jumped");
    rightClick(tourQuery(".zone-index-item"));
    await record("zone-highlighted");
    rightClick(tourQuery(".zone-index-item"));
    await record("zone-unhighlighted");
  }

  // The unified legend's own states: a shape collection folds and unfolds its
  // feature index, hides and returns, isolates, and flips its label policy.
  const shapeExpand = tourQuery("[data-expand-collection]");
  if (shapeExpand) {
    shapeExpand.click();
    await record("collection-folded");
    tourQuery("[data-expand-collection]").click();
    await record("collection-unfolded");
  }
  const shapeRowAt = (index) => {
    const rows = [...document.querySelectorAll(".category-row")]
      .filter((row) => row.querySelector("[data-expand-collection]"));
    return rows[index] ?? rows[0] ?? null;
  };
  const shapeIndex = shapeRowAt(1) === shapeRowAt(0) ? 0 : 1;
  if (shapeRowAt(shapeIndex)) {
    shapeRowAt(shapeIndex).querySelector("input[data-collection]").click();
    await record("collection-hidden");
    shapeRowAt(shapeIndex).querySelector("input[data-collection]").click();
    await record("collection-restored");
    const only = shapeRowAt(shapeIndex).querySelector("[data-only-collection]");
    if (only) {
      only.click();
      await record("collection-solo");
      const soloChip = tourQuery("#solo-chip");
      if (soloChip && !soloChip.hidden) soloChip.click();
      await record("collection-solo-cleared");
    }
  }
  const labelToggle = tourQuery("[data-label-toggle]");
  if (labelToggle) {
    const id = labelToggle.dataset.labelToggle;
    labelToggle.click();
    await record("labels-flipped");
    tourQuery(`[data-label-toggle="${CSS.escape(id)}"]`)?.click();
    await record("labels-curated");
  }
  // A point collection's labels answer the same toggle.
  const pointLabelID = [...document.querySelectorAll(".category-row")]
    .filter((row) => !row.querySelector("[data-expand-collection]"))
    .map((row) => row.querySelector("[data-label-toggle]"))
    .find(Boolean)?.dataset.labelToggle;
  if (pointLabelID) {
    const pointToggle = () => tourQuery(`[data-label-toggle="${CSS.escape(pointLabelID)}"]`);
    pointToggle().click();
    await record("point-labels-flipped");
    pointToggle()?.click();
    await record("point-labels-curated");
  }

  // Highlights across two collections read AND.
  const indexIDs = [...document.querySelectorAll(".feature-index")]
    .map((index) => index.querySelector(".zone-index-item")?.dataset.zone)
    .filter(Boolean);
  if (indexIDs.length > 1) {
    const near = indexIDs[0];
    const far = indexIDs[indexIDs.length - 1];
    const zone = (id) => tourQuery(`.zone-index-item[data-zone="${CSS.escape(id)}"]`);
    rightClick(zone(near));
    rightClick(zone(far));
    await record("and-highlighted");
    keydown("z");
    await record("labels-held-highlighted");
    keyup("z");
    rightClick(zone(near));
    rightClick(zone(far));
    await record("and-cleared");
  }

  // The legend, the map and the dock under one filter, then the camera's
  // round trip through the panel beside the map.
  const syncZoneID = tourQuery(".zone-index-item")?.dataset.zone;
  if (syncZoneID) {
    const syncZone = () => tourQuery(`.zone-index-item[data-zone="${CSS.escape(syncZoneID)}"]`);
    rightClick(syncZone());
    await record("filter-highlight-dock");
    const before = await record("filter-dock-before-jump");
    const rows = [...document.querySelectorAll("#dock-results .search-result")];
    const row = rows[rows.length - 1];
    if (row) {
      row.click();
      const jumped = await record("dock-jumped");
      const moved = !sameCamera(before, jumped);
      tourQuery("#close-detail")?.click();
      const returned = await record("dock-returned");
      if (moved && !sameCamera(before, returned)) {
        problems.push("dock-returned: closing the card left the camera where the jump put it");
      }
    }
    const steered = [...document.querySelectorAll("#dock-results .search-result")].pop();
    if (steered) {
      steered.click();
      await record("dock-jumped-again");
      tourQuery("#zoom-in").click();
      const chosen = await record("dock-steered");
      tourQuery("#close-detail")?.click();
      const kept = await record("dock-kept-view");
      if (!sameCamera(chosen, kept)) {
        problems.push("dock-kept-view: closing the card overrode a view the reader steered to");
      }
      tourQuery("#zoom-out").click();
      await record("dock-steer-undone");
    }
    rightClick(syncZone());
    await record("filter-highlight-cleared");
  }

  // The geohash grid: open, descend one character, hide the subgrid, ascend,
  // close.
  keydown("g");
  await record("grid-open");
  type("#grid-input", "m");
  await record("grid-descended");
  keydown(" ");
  await record("subgrid-hidden");
  keydown(" ");
  keydown("Escape");
  await record("grid-ascended");
  keydown("Escape");
  await record("grid-closed");

  // Label and chrome shortcuts.
  keydown("z");
  await record("labels-on");
  keyup("z");
  await record("labels-off");
  keydown("b", { metaKey: true });
  await record("sidebar-collapsed");
  keydown("b", { metaKey: true });
  await record("sidebar-restored");

  // The zoom controls, and overview docking while zoomed in.
  tourQuery("#zoom-in").click();
  await record("zoomed-in");
  const dock = tourQuery("#overview-dock");
  if (dock && !tourQuery("#atlas-overview").hidden) {
    dock.click();
    await record("overview-docked");
    tourQuery("#overview-dock").click();
    await record("overview-undocked");
  }
  tourQuery("#zoom-out").click();
  await record("zoomed-out");
  viewportKeydown("Escape");
  await record("viewport-escape");

  // Variant, map, and game switching, then return to the start.
  const variant = tourQuery("#lens-select");
  if (variant.options.length > 1) {
    change(variant, variant.options[1].value);
    await record("variant-second");
    change(tourQuery("#lens-select"), tourQuery("#lens-select").options[0].value);
    await record("variant-first");
  }
  const mapSelect = tourQuery("#world-select");
  if (mapSelect.options.length > 1) {
    change(mapSelect, mapSelect.options[1].value);
    await record("map-second");
    change(tourQuery("#world-select"), tourQuery("#world-select").options[0].value);
    await record("map-first");
  }
  const otherGame = offered.find((slug) => slug !== firstGame);
  if (otherGame) {
    change(tourQuery("#volume-select"), otherGame);
    await record("game-second");
    change(tourQuery("#volume-select"), firstGame);
    await record("game-first");
  }

  await globePane(record, complain);
  await libraryFlow(record, complain);
  await labelLadder(record, complain);

  // The three kinds the reference tour could not see with (SCHEMA.md §2.1).
  // They are appended, and they are asked for: a walk that did not ask keeps
  // exactly the step list the six committed baselines hold, which is what
  // lets this file change without every baseline having to.
  if (options.extended) {
    const shot = shotTaker(shots, complain);
    await pickSteps(record, complain);
    await keySteps(record, complain);
    await screenSteps(record, complain, shot);
  }

  return {
    volume: firstGame,
    viewport: [window.innerWidth, window.innerHeight],
    problems,
    steps,
    // The screenshots this walk asked the driver for, in the order it asked.
    // The pictures themselves are files; this is the list of them, and it is
    // what `compare.mjs` looks for a committed twin of.
    shots,
  };
}

function rightClick(element) {
  element?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }));
}

// The globe pane.
async function globePane(record, complain) {
  const before = await record("globe-offered");
  if (!before.pane.globeOffered || !tourQuery("#globe-toggle")) return;
  // Re-queried at every press rather than held: a control on this page can be
  // carried across a swap or replaced by one, and a reference taken before a
  // step is a reference to whatever the page used to be.
  const toggle = () => tourQuery("#globe-toggle");

  toggle().click();
  const entered = await record("globe-entered");
  if (!entered.pane.globeActive || entered.pane.globeHidden || !entered.pane.chartHidden) {
    complain("globe-entered: the toggle reads pressed but the panes did not swap");
  }
  tourQuery("#zoom-in").click();
  await record("globe-zoomed-in");
  tourQuery("#zoom-in").click();
  tourQuery("#zoom-in").click();
  const deep = await record("globe-zoomed-deep");
  if (deep.pane.detailTiles === 0) {
    complain("globe-zoomed-deep: no pyramid tiles arrived under a camera past the skin's depth");
  }
  keydown("z");
  await record("globe-labels-held");
  keyup("z");
  const released = await record("globe-labels-released");
  if (released.pane.labelSprites !== 0) {
    complain("globe-labels-released: the sphere kept its names after Z was let go");
  }
  const lensSelect = tourQuery("#lens-select");
  if (lensSelect && lensSelect.options.length > 1) {
    change(lensSelect, lensSelect.options[1].value);
    const swapped = await record("globe-lens-second");
    if (swapped.pane.detailLens === deep.pane.detailLens) {
      complain("globe-lens-second: the sphere kept the pyramid of the lens it left");
    }
    change(tourQuery("#lens-select"), tourQuery("#lens-select").options[0].value);
    await record("globe-lens-first");
  }
  keydown("g");
  await record("globe-grid-open");
  type("#grid-input", "m");
  await record("globe-grid-descended");
  keydown("Escape");
  keydown("Escape");
  await record("globe-grid-closed");
  const collectionID = tourQuery("#legend input[data-collection]")?.dataset.collection;
  if (collectionID) {
    const checkbox = () => tourQuery(`#legend input[data-collection="${CSS.escape(collectionID)}"]`);
    const lit = await record("globe-collection-shown");
    checkbox().click();
    const hidden = await record("globe-collection-hidden");
    if (hidden.pane.visibleSprites >= lit.pane.visibleSprites) {
      complain("globe-collection-hidden: the sphere kept every pin the chart just lost");
    }
    checkbox().click();
    const restored = await record("globe-collection-restored");
    if (restored.pane.visibleSprites !== lit.pane.visibleSprites) {
      complain("globe-collection-restored: the sphere did not get its pins back");
    }
  }
  const row = [...document.querySelectorAll("#dock-results .search-result")][0];
  if (row) {
    row.click();
    await record("globe-selected");
    tourQuery("#close-detail")?.click();
    await record("globe-detail-closed");
  }
  tourQuery("#zoom-out").click();
  tourQuery("#zoom-out").click();
  tourQuery("#zoom-out").click();
  await record("globe-zoomed-out");
  toggle().click();
  const left = await record("globe-left");
  if (left.pane.globeActive || !left.pane.globeHidden || left.pane.chartHidden) {
    complain("globe-left: the toggle reads unpressed but the panes did not swap back");
  }
  if (left.pane.detailTiles !== 0) {
    complain("globe-left: a put-away globe kept the neighborhood of tiles under its camera");
  }
  const parked = await record("globe-parked");
  toggle().click();
  await record("globe-reentered");
  toggle().click();
  const returned = await record("globe-returned");
  if (!sameCamera(parked, returned)) {
    complain("globe-returned: a flip to the sphere and back moved the chart's camera");
  }
}

// The library: adding a volume, and reconciling with whatever the catalog now
// holds. The picker is native and cannot be raised without a window, so a
// headless run exercises the refusal rather than the choosing.
async function libraryFlow(record, complain) {
  const before = await record("library-initial");
  const button = tourQuery("#add-bundles");
  if (button) {
    button.click();
    const asked = await record("import-refused");
    if (asked.library.volumes.join() !== before.library.volumes.join()) {
      complain("import-refused: a refused import changed the volumes on offer");
    }
    if (asked.library.addBundlesDisabled) {
      complain("import-refused: the import button was left disabled");
    }
  }
  await refreshCatalog();
  const same = await record("catalog-reconciled");
  if (same.volume !== before.volume || same.world !== before.world) {
    complain("catalog-reconciled: reconciling an unchanged catalog moved the reader");
  }
  if (!sameCamera(before, same)) {
    complain("catalog-reconciled: reconciling an unchanged catalog moved the camera");
  }
  const collectionID = tourQuery("#legend input[data-collection]")?.dataset.collection;
  if (collectionID) {
    const checkbox = () => tourQuery(`#legend input[data-collection="${CSS.escape(collectionID)}"]`);
    checkbox().click();
    const filtered = await record("catalog-reconcile-filtered");
    await refreshCatalog();
    const after = await record("catalog-reconciled-filtered");
    if (after.sync.drawn !== filtered.sync.drawn ||
      JSON.stringify(after.filters) !== JSON.stringify(filtered.filters)) {
      complain("catalog-reconciled-filtered: the reconcile spent the reader's filter");
    }
    checkbox().click();
    await record("catalog-reconcile-cleared");
  }
}

// The label-policy ladder.
async function labelLadder(record, complain) {
  const initial = await record("label-ladder-initial");
  const ids = [...document.querySelectorAll("[data-label-toggle]")]
    .map((button) => button.dataset.labelToggle);
  if (ids.length === 0) return;
  const toggle = (id) => tourQuery(`[data-label-toggle="${CSS.escape(id)}"]`);

  const id = ids.find((each) => toggle(each)?.getAttribute("aria-pressed") === "true") ?? ids[0];
  const overridden = (snapshot) =>
    snapshot.session.entry?.labels.some((pair) => pair.startsWith(`${id}=`)) ?? false;

  toggle(id).click();
  const flipped = await record("label-override-set");
  if (!overridden(flipped)) {
    complain("label-override-set: the flip disagreed with the curation and was not recorded");
  }
  keydown("z");
  const held = await record("label-silenced-held");
  if (held.labels.speaking.includes(id) !== flipped.labels.speaking.includes(id)) {
    complain("label-silenced-held: Z overrode a policy the reader chose");
  }
  keyup("z");
  await record("label-silenced-released");

  toggle(id).click();
  const restored = await record("label-override-dropped");
  if (overridden(restored)) {
    complain("label-override-dropped: flipping back to the curated word left an override behind");
  }
  if (JSON.stringify(restored.labels) !== JSON.stringify(initial.labels)) {
    complain("label-override-dropped: the ladder did not come back to where it started");
  }

  for (const each of ids) toggle(each)?.click();
  await record("label-ladder-all-flipped");
  for (const each of ids) toggle(each)?.click();
  const back = await record("label-ladder-restored");
  if (JSON.stringify(back.labels) !== JSON.stringify(initial.labels)) {
    complain("label-ladder-restored: turning the whole ladder over and back did not restore it");
  }
  if (back.session.entry?.labels.length) {
    complain("label-ladder-restored: overrides that agree with the curation were kept");
  }
}

// ---- the extended half: picks, keys and pictures -----------------------
//
// Three step kinds, and one sentence for why each exists. The tour walked the
// whole application through its controls and could not have seen a click on
// the canvas go nowhere, a shortcut fire while the reader was typing, or a
// sphere rendered black -- not because those were hard to reach, but because
// nothing in the log was a pixel, a pointer or a focus (SCHEMA.md §7).
//
//   PICKS drive real pointer events at a real feature's pixel. What is aimed
//   at is worked out through the pane's own OpenLayers map, and only the aim:
//   the click itself goes in as `pointerdown`/`pointerup` on the map's
//   viewport, travels through OpenLayers' own hit detection, the seam's
//   `singleclick` handler, the pick form the page renders and the route
//   behind it. Every part of that path is the application's.
//
//   KEYS extend the tour's synthesis past the four it had. Two things are new
//   in kind: what the *focus* is afterwards, which is half of what a shortcut
//   is for, and a key dispatched AT a text field rather than at the window,
//   which is the only way to ask whether a shortcut can hear a reader typing.
//
//   PICTURES are taken by the driver, not by the page: a WebGL sphere cannot
//   be read back through a 2D context, and the tour's own `checkCanvas` says
//   so by falling silent exactly where the sphere is. The driver screenshots
//   the pane and the comparison happens outside the browser.
//
// THE REACH-IN, declared. `document.querySelector("atlas-chart").map` is the
// pane's own OpenLayers map, and a harness that asks it where a feature is
// drawn is doing arithmetic the pane would otherwise make it repeat. Nothing
// is *done* through it: no selection is made, no camera is moved, no event is
// raised on it. A build that renamed it loses the aim, and the steps say so
// rather than passing quietly.

const CLICK_MARGIN = 64;
const HIT_TOLERANCE = 4;

function chartMap() {
  const chart = tourQuery("atlas-chart");
  const map = chart && !chart.hidden ? chart.map : null;
  return map && typeof map.getPixelFromCoordinate === "function" ? map : null;
}

/** What the pane would resolve at a pixel: a feature id, or nothing. */
function resolveAt(map, pixel) {
  let found = null;
  map.forEachFeatureAtPixel(pixel, (feature) => {
    const record = feature.get("record");
    if (!record) return false;
    found = record.id;
    return true;
  }, { hitTolerance: HIT_TOLERANCE });
  return found;
}

function insideWindow(map, pixel) {
  const box = map.getViewport().getBoundingClientRect();
  return Boolean(pixel) && pixel[0] > CLICK_MARGIN && pixel[1] > CLICK_MARGIN &&
    pixel[0] < box.width - CLICK_MARGIN && pixel[1] < box.height - CLICK_MARGIN;
}

/**
 * A feature to aim at: the first, by id, that the pane resolves as itself.
 *
 * "As itself" is the whole of the choosing. Pins overlap, and a pin clicked
 * where another one is drawn on top of it is a correct pick of the wrong
 * feature -- which would make this step's recorded value a property of the
 * camera rather than of the build. Sorting by id makes the choice the same on
 * every run over the same view.
 */
function aimAt(map, source, at) {
  if (!source) return null;
  const candidates = [];
  for (const feature of source.getFeatures()) {
    const record = feature.get("record");
    const geometry = feature.getGeometry();
    if (!record || !geometry) continue;
    const place = at(geometry);
    if (!place) continue;
    const pixel = map.getPixelFromCoordinate(place);
    if (!insideWindow(map, pixel)) continue;
    candidates.push({
      id: record.id, title: record.title ?? "", kind: record.kind ?? "point", pixel,
    });
  }
  candidates.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  for (const candidate of candidates) {
    if (resolveAt(map, candidate.pixel) === candidate.id) return candidate;
  }
  return null;
}

const pointOf = (geometry) => geometry.getType() === "Point"
  ? geometry.getCoordinates().slice(0, 2)
  : null;

const groundOf = (geometry) => {
  const type = geometry.getType();
  if (type === "Polygon") return geometry.getInteriorPoint().getCoordinates().slice(0, 2);
  if (type === "MultiPolygon") {
    const points = geometry.getInteriorPoints();
    return points ? points.getCoordinates()[0]?.slice(0, 2) ?? null : null;
  }
  if (type === "LineString") return geometry.getCoordinateAt(0.5);
  return null;
};

/**
 * Ground with nothing on it: the first lattice point the pane resolves as
 * empty, over a coarse lattice and then a fine one.
 *
 * The second pass is for the city. A volume with eight thousand features and
 * a river system drawn across it can have no gap on a 37-pixel lattice at all,
 * and a miss that cannot be aimed is a step that quietly does not happen --
 * which is exactly the kind of silence this whole exercise is about. The
 * caller says so out loud when both passes come back empty.
 */
function emptyPixel(map) {
  const box = map.getViewport().getBoundingClientRect();
  for (const step of [37, 13]) {
    for (let y = CLICK_MARGIN; y < box.height - CLICK_MARGIN; y += step) {
      for (let x = CLICK_MARGIN; x < box.width - CLICK_MARGIN; x += step) {
        if (resolveAt(map, [x, y]) === null) return [x, y];
      }
    }
  }
  return null;
}

/**
 * One real click, as a browser sends one.
 *
 * OpenLayers builds its `singleclick` out of `pointerdown`, `pointerup` and
 * the quarter-second it waits to see whether a second click is coming, so a
 * synthesized `click` alone reaches nothing. The wait is the reason this is
 * slow and the reason it is honest.
 */
async function clickPixel(map, pixel) {
  const surface = map.getViewport();
  const box = surface.getBoundingClientRect();
  const shared = {
    bubbles: true, cancelable: true, pointerId: 1, pointerType: "mouse",
    isPrimary: true, button: 0, clientX: box.left + pixel[0], clientY: box.top + pixel[1],
  };
  surface.dispatchEvent(new PointerEvent("pointermove", { ...shared, buttons: 0 }));
  surface.dispatchEvent(new PointerEvent("pointerdown", { ...shared, buttons: 1 }));
  await sleep(40);
  surface.dispatchEvent(new PointerEvent("pointerup", { ...shared, buttons: 0 }));
  surface.dispatchEvent(new MouseEvent("click", { ...shared, buttons: 0 }));
  await sleep(400);
}

/**
 * What a pick step records about its own aim.
 *
 * `under` is read *before* the click and is the aim's witness: what the pane
 * itself says is at that pixel. For a pin it is the pin, by construction; for
 * a miss it is nothing, by construction; for a cell it is whatever feature
 * happens to stand inside the cell, which is the whole question that step
 * asks. It is deliberately not read afterwards: a pick moves the camera, so
 * an after-reading would record where the view went rather than what was
 * clicked.
 */
const aimRecord = (aimed, under) => ({
  pick: {
    at: aimed ? aimed.id : null,
    title: aimed ? aimed.title : null,
    kind: aimed ? aimed.kind : null,
    under,
  },
});

async function pickSteps(record, complain) {
  // A wide view: the aim wants features on screen, and where the walk left
  // the camera is nobody's contract.
  tourQuery("#zoom-out")?.click();
  tourQuery("#zoom-out")?.click();
  await record("pick-ready");
  const map = chartMap();
  if (!map) {
    complain("pick-ready: the chart pane opened no map to aim through");
    return;
  }

  const pin = aimAt(map, pinSource(), pointOf);
  if (pin) {
    const under = resolveAt(map, pin.pixel);
    await clickPixel(map, pin.pixel);
    const picked = await record("pick-a-pin", aimRecord(pin, under));
    if (!picked.ui.detailOpen) {
      complain("pick-a-pin: a click on a pin opened no card");
    } else if (picked.ui.detailTitle !== pin.title) {
      complain(`pick-a-pin: clicking ${pin.title} opened the card on ${picked.ui.detailTitle}`);
    }
  } else {
    complain("pick-ready: no pin stood clear inside the window to aim at");
  }

  // A miss, with the card open, and the decision it pins down: a click on
  // nothing is not a pick and therefore is not a *deselection* either. The
  // reference kept the card; a build that closes it on every stray click has
  // changed what a click means, and no count in this file would notice.
  // The city has nowhere to miss into at any camera, and the reason is not
  // density: its ground *covers the window*. A watershed is a feature and a
  // click inside one resolves it, so on a volume whose areas tile the map
  // there is no pixel that resolves nothing however far in the camera goes.
  // Where the window is covered, the drawing is taken away instead -- every
  // collection hidden, through the control the tour already presses -- and put
  // back before the group ends. Hiding leaves the card open, which is what
  // makes it usable here: the question is still whether a click that resolves
  // nothing puts the reader's selection down.
  let empty = emptyPixel(map);
  let hidden = false;
  if (!empty && pin) {
    tourQuery("#hide-all")?.click();
    await settle();
    empty = emptyPixel(map);
    hidden = Boolean(empty);
  }
  if (!empty && pin) {
    complain("pick-missed: nowhere in the window was clear of features to miss with," +
      " so the step the card's survival is pinned by did not happen");
  }
  if (empty && pin) {
    const under = resolveAt(map, empty);
    await clickPixel(map, empty);
    const missed = await record("pick-missed", aimRecord(null, under));
    if (!missed.ui.detailOpen) {
      complain("pick-missed: a click on empty ground closed the card the reader had open");
    }
  }
  if (hidden) tourQuery("#show-all")?.click();
  tourQuery("#close-detail")?.click();
  await record("pick-cleared");

  const ground = aimAt(map, shapeSource(), groundOf);
  if (ground) {
    const under = resolveAt(map, ground.pixel);
    await clickPixel(map, ground.pixel);
    const picked = await record("pick-a-shape", aimRecord(ground, under));
    if (!picked.ui.detailOpen) {
      complain("pick-a-shape: a click on ground opened no card");
    } else if (picked.ui.detailTitle !== ground.title) {
      complain(`pick-a-shape: clicking ${ground.title} opened the card on ${picked.ui.detailTitle}`);
    }
    tourQuery("#close-detail")?.click();
    await record("pick-shape-cleared");
  }

  // In grid mode a cell is the nearer thing. A reader who turned the grid on
  // is navigating by cells, and a click inside one telescopes into it; the
  // feature under the pointer is what they get when the grid is off.
  keydown("g");
  const opened = await record("pick-grid-open");
  const cell = gridTarget(map, opened);
  if (cell) {
    const under = resolveAt(map, cell.pixel);
    await clickPixel(map, cell.pixel);
    const descended = await record("pick-in-grid", {
      pick: { at: cell.hash, title: null, kind: "cell", under },
    });
    if (descended.grid.prefix !== cell.hash) {
      complain(`pick-in-grid: a click inside cell ${cell.hash} left the grid holding` +
        ` "${descended.grid.prefix}"`);
    }
    if (descended.ui.detailOpen) {
      complain("pick-in-grid: a click meant for a cell opened a feature's card instead");
    }
  }
  keydown("Escape");
  keydown("Escape");
  await record("pick-grid-closed");
}

// The two vector sources a pick can land in, by the names the pane keeps them
// under. Read rather than reached through: see the reach-in note above.
const pinSource = () => tourQuery("atlas-chart")?.sources?.pins ?? null;
const shapeSource = () => tourQuery("atlas-chart")?.sources?.zones ?? null;

/**
 * A cell to click into: the held level's first cell by hash, centre on screen.
 *
 * The cells come from the published diagnostics rather than from the pane's
 * features -- the grid is one of the things the seam already writes down
 * (SCHEMA.md §3.1) -- and only the pixel is asked of the map.
 */
function gridTarget(map, snapshot) {
  const cells = (snapshot.grid?.cells ?? [])
    .filter((cell) => cell.role !== "neighbor" && cell.hash)
    .sort((a, b) => (a.hash < b.hash ? -1 : 1));
  for (const cell of cells) {
    const [minX, minY, maxX, maxY] = cell.extent;
    const pixel = map.getPixelFromCoordinate([(minX + maxX) / 2, (minY + maxY) / 2]);
    if (insideWindow(map, pixel)) return { hash: cell.hash, pixel };
  }
  return null;
}

// ---- keys, and where they leave the reader -----------------------------

/**
 * Where the focus is, and whether what is in it is ready to be typed over.
 *
 * A shortcut that opens a field and does not put the cursor in it has done
 * half its job, and the half it skipped is the half the reader notices. The
 * ids are the page's own, so this reads as a sentence in the log:
 * `focus.active: "pin-search"`.
 */
function focusRecord() {
  const element = document.activeElement;
  const id = element ? element.id || element.tagName.toLowerCase() : null;
  const selected = element && "selectionStart" in element && typeof element.value === "string"
    ? element.selectionStart === 0 && element.selectionEnd === element.value.length &&
      element.value.length > 0
    : false;
  return { focus: { active: id, selected } };
}

async function keySteps(record, complain) {
  // ⌘K — the shortcut the search field advertises with a <kbd> and does not
  // answer. Focused *and* selected: a reader who reaches for search a second
  // time means to replace what is in it, not to append to it.
  type("#pin-search", "harbour");
  await record("key-search-primed");
  keydown("k", { metaKey: true });
  const searching = await record("key-search-focus", focusRecord());
  if (searching.focus.active !== "pin-search") {
    complain(`key-search-focus: ⌘K left the focus on ${searching.focus.active}`);
  } else if (!searching.focus.selected) {
    complain("key-search-focus: ⌘K focused the search field without selecting what was in it");
  }
  type("#pin-search", "");
  await record("key-search-cleared");

  // G opens the grid; the field it opens is where the reader's next keystroke
  // is meant to go.
  keydown("g");
  const grid = await record("key-grid-open", focusRecord());
  if (!grid.grid.enabled) complain("key-grid-open: G did not turn the grid on");
  if (grid.focus.active !== "grid-input") {
    complain(`key-grid-open: G left the focus on ${grid.focus.active}`);
  }

  // The Escape dance. From inside the field, the first Escape is about the
  // field -- it gives the map back the keyboard -- and only the second one is
  // about the grid. A single Escape that ascends a level is the reader
  // losing a cell for putting their hands down.
  type("#grid-input", "m");
  // The cell the field actually took, rather than the one that was typed: not
  // every ground divides into a cell called "m", and the question this step
  // asks is whether Escape *moved* it, whatever it was.
  const held = (await record("key-grid-descended")).grid.prefix;
  tourQuery("#grid-input")?.focus();
  keydown("Escape");
  const once = await record("key-escape-once", focusRecord());
  if (once.focus.active !== "map") {
    complain(`key-escape-once: the first Escape left the focus on ${once.focus.active}`);
  }
  if (once.grid.prefix !== held) {
    complain(`key-escape-once: the first Escape ascended out of "${held}" to` +
      ` "${once.grid.prefix}" while the reader was still in the field`);
  }
  keydown("Escape");
  const twice = await record("key-escape-twice", focusRecord());
  if (twice.grid.prefix === once.grid.prefix && once.grid.prefix !== "") {
    complain("key-escape-twice: the second Escape ascended nothing");
  }

  // ⌘G cycles the cell system, and the cell the reader is holding is carried
  // across to the same ground in the system it lands in.
  type("#grid-input", "m");
  const before = await record("key-cell-system-before");
  keydown("g", { metaKey: true });
  const cycled = await record("key-cell-system-cycled");
  if (cycled.grid.system === before.grid.system) {
    complain(`key-cell-system-cycled: ⌘G left the system at ${before.grid.system}`);
  } else if (!cycled.grid.prefix) {
    complain("key-cell-system-cycled: the cycle dropped the cell the reader was holding");
  }
  keydown("Escape");
  keydown("Escape");
  await record("key-grid-closed");

  // Z, held and let go. This one the tour already had and it is repeated here
  // beside the rest so that the keyboard's own group reads as one thing.
  keydown("z");
  const raised = await record("key-labels-held", focusRecord());
  if (!raised.labelsHeld) complain("key-labels-held: Z down did not raise the names");
  keyup("z");
  const released = await record("key-labels-released", focusRecord());
  if (released.labelsHeld) complain("key-labels-released: Z up did not put the names down");

  // A reader typing is not pressing shortcuts. The key goes in AT the field,
  // which is where a reader's own key starts, and the grid must not hear it.
  const gridBefore = (await record("key-typing-before")).grid.enabled;
  tourQuery("#pin-search")?.focus();
  type("#pin-search", "g");
  keydownAt("#pin-search", "g");
  const typed = await record("key-typing-not-a-shortcut", focusRecord());
  if (typed.grid.enabled !== gridBefore) {
    complain("key-typing-not-a-shortcut: typing g into the search field turned the grid" +
      ` ${typed.grid.enabled ? "on" : "off"}`);
  }
  if (typed.focus.active !== "pin-search") {
    complain(`key-typing-not-a-shortcut: typing took the focus to ${typed.focus.active}`);
  }
  type("#pin-search", "");
  await record("key-typing-cleared");
}

// ---- pictures ----------------------------------------------------------

/**
 * The handshake with the driver.
 *
 * The page cannot take its own picture: a WebGL canvas has nothing to read
 * back and a composited pane is more than its canvas anyway. So the walk asks
 * -- it publishes what it wants shot and waits -- and the runner, which is
 * already watching this page once a second to see where the walk has got to,
 * takes it with the browser's own screenshot and says so.
 *
 * A walk asked to take pictures with nobody serving them says which step it
 * was waiting on rather than hanging: `run.mjs --extended` is the only thing
 * that serves them.
 */
function shotTaker(shots, complain) {
  return async (name, selector, { nonBlank = true } = {}) => {
    const request = { name, selector, file: `${name}.png`, nonBlank };
    window.__atlasShotTaken = "";
    window.__atlasShotFailed = "";
    window.__atlasShotWant = request;
    for (let waited = 0; waited < 300; waited += 1) {
      await sleep(100);
      if (window.__atlasShotTaken === name) {
        shots.push(request);
        return true;
      }
      if (window.__atlasShotFailed === name) {
        const why = window.__atlasShotError || "no reason given";
        complain(`${name}: the screenshot failed — ${why}`);
        return false;
      }
    }
    window.__atlasShotWant = null;
    complain(`${name}: nobody was serving screenshots (run.mjs --extended does)`);
    return false;
  };
}

async function screenSteps(record, complain, shot) {
  const chart = "atlas-chart";

  // Pictures are taken from a known state, not from wherever the keyboard
  // group left the page. The walk is deterministic either way -- two runs of
  // one build photograph the same frame -- but a chart picture with the grid
  // navigator sitting across it is a picture of two things, and the one it
  // was taken for is the one underneath.
  for (let tries = 0; tries < 4; tries += 1) {
    if (!JSON.parse(window.render_game_to_text()).grid.enabled) break;
    keydown("Escape");
    await settle();
  }
  tourQuery("#close-detail")?.click();
  const ready = await record("screen-ready");
  if (ready.grid.enabled) complain("screen-ready: the grid would not close before the pictures");

  // The chart as the volume opens: the one picture that would have caught the
  // backdrop painted over the pane, and the one every other chart picture is
  // read against by eye.
  await settle();
  await shot("screen-chart", chart);
  await record("screen-chart", { screen: { file: "screen-chart.png", element: chart } });

  // The panel beside the map, out and folded away. Its own region, because
  // the thing worth seeing is the rail's width and the rows in it.
  const dockFold = tourQuery("#dock-fold");
  if (dockFold) {
    await settle();
    await shot("screen-dock-open", "#atlas-dock");
    await record("screen-dock-open",
      { screen: { file: "screen-dock-open.png", element: "#atlas-dock" } });
    tourQuery("#dock-fold").click();
    await settle();
    // The one picture that is *supposed* to be a flat colour: folded, the
    // panel is a rail forty pixels across with its label written down the
    // side, and the middle half of it is one shade. The colour count is a
    // floor for panes that draw a world, not for chrome.
    await shot("screen-dock-folded", "#atlas-dock", { nonBlank: false });
    await record("screen-dock-folded",
      { screen: { file: "screen-dock-folded.png", element: "#atlas-dock" } });
    tourQuery("#dock-fold").click();
    await record("screen-dock-unfolded");
  }

  // Names, held up. A label ladder that draws nothing is a count of sprites
  // that says everything is fine.
  keydown("z");
  await settle();
  await shot("screen-labels-held", chart);
  await record("screen-labels-held",
    { screen: { file: "screen-labels-held.png", element: chart } });
  keyup("z");
  await record("screen-labels-released");

  // Past the lens's own depth, where the raster is either smoothed or kept
  // square. A build that lost `interpolate` looks identical in every count in
  // this file and different in exactly one picture.
  const opening = await record("screen-zoom-ready");
  if (opening.nativeMaxZoom !== null) {
    for (let press = 0; press < 4; press += 1) tourQuery("#zoom-in")?.click();
    await settle();
    await shot("screen-raster-deep", chart);
    await record("screen-raster-deep",
      { screen: { file: "screen-raster-deep.png", element: chart } });
    for (let press = 0; press < 4; press += 1) tourQuery("#zoom-out")?.click();
    await record("screen-raster-back");
  }

  // Out past the edge of the drawn world, where what shows is the background
  // the lens declares rather than the lens.
  for (let press = 0; press < 3; press += 1) tourQuery("#zoom-out")?.click();
  await settle();
  await shot("screen-outside-bounds", chart);
  await record("screen-outside-bounds",
    { screen: { file: "screen-outside-bounds.png", element: chart } });

  // Ground, drawn whole. On a volume whose districts are multipart this is
  // the picture that says whether the second part was drawn at all.
  const firstZone = tourQuery(".zone-index-item");
  if (firstZone) {
    firstZone.click();
    await settle();
    await shot("screen-ground", chart);
    await record("screen-ground", { screen: { file: "screen-ground.png", element: chart } });
    tourQuery("#close-detail")?.click();
    await record("screen-ground-cleared");
  }

  // The sphere: its skin, and its names.
  const toggle = tourQuery("#globe-toggle");
  if (toggle && !toggle.hidden) {
    toggle.click();
    await settle();
    await shot("screen-globe", "atlas-globe");
    await record("screen-globe", { screen: { file: "screen-globe.png", element: "atlas-globe" } });
    keydown("z");
    await settle();
    await shot("screen-globe-labels", "atlas-globe");
    await record("screen-globe-labels",
      { screen: { file: "screen-globe-labels.png", element: "atlas-globe" } });
    keyup("z");
    tourQuery("#globe-toggle").click();
    await record("screen-globe-left");
  }
}

window.__atlasTour = tour;
