// Behavior-parity tour. Pressing F9 in a running Atlas walks the
// user-reachable surface — selection, search, filters, zones, the geohash
// grid, keyboard shortcuts, the overview, and map switching — records a
// diagnostics snapshot after every step, and posts the log to the
// development build's /parity/result route, which writes it to disk. Two
// runs of the same build must produce identical output for every stable
// field; the refactor gates diff this output across builds. The tour clears
// the saved session before and after so every run starts from the same
// place, and it announces completion with an on-screen badge so a driver
// that can only look at pixels knows when to collect the file.
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const tourQuery = (selector) => document.querySelector(selector);

async function waitForBoot() {
  for (let i = 0; i < 300; i += 1) {
    if (window.__atlasDebug && !tourQuery("#map-loading")?.checkVisibility?.()) break;
    await sleep(100);
  }
}

// A step is settled when tiles stop arriving and the view stops moving. Each
// poll waits through a real animation frame so work the app deferred with
// requestAnimationFrame - the post-selection map fit above all - lands during
// the step that caused it rather than surfacing in a later one.
async function settle() {
  let previous = "";
  for (let i = 0; i < 100; i += 1) {
    await sleep(120);
    await new Promise((resolve) => requestAnimationFrame(resolve));
    window.advanceTime?.();
    const s = window.__atlasDebug.snapshot();
    const key = JSON.stringify([s.zoom, s.center, s.resolution, s.tileStats]);
    if (key === previous) return;
    previous = key;
  }
}

function keydown(key, options = {}) {
  window.dispatchEvent(new KeyboardEvent("keydown", {
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
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

function badge(text, failed = false) {
  let element = tourQuery("#parity-badge");
  if (!element) {
    element = document.createElement("div");
    element.id = "parity-badge";
    element.style.cssText = "position:fixed;top:8px;right:8px;z-index:9999;" +
      "padding:6px 12px;font:700 13px monospace;border-radius:6px;" +
      "color:#fff;pointer-events:none;";
    document.body.append(element);
  }
  element.style.background = failed ? "#b3261e" : "#1a7f37";
  element.textContent = text;
}

async function tour() {
  const steps = [];
  const record = async (name) => {
    await settle();
    steps.push({ name, snapshot: JSON.parse(window.render_game_to_text()) });
  };

  // Start from a virgin session on the first game so every run of the tour
  // begins in the same place regardless of what was explored beforehand.
  await waitForBoot();
  localStorage.clear();
  const gameSelect = tourQuery("#volume-select");
  const firstGame = gameSelect.options[0].value;
  change(gameSelect, firstGame);
  await record("initial");

  // Search, select the first result, read the detail panel, close it.
  type("#pin-search", "a");
  await record("search-a");
  const firstResult = tourQuery(".search-result");
  if (firstResult) {
    firstResult.click();
    await record("search-select-first");
    tourQuery("#close-detail").click();
    await record("detail-closed");
  }
  type("#pin-search", "");
  await record("search-cleared");

  // Category filtering: hide the first category, then solo it, then clear.
  const firstCategory = tourQuery(".category-row input");
  if (firstCategory) {
    firstCategory.click();
    await record("category-hidden");
    firstCategory.click();
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
    firstZone.dispatchEvent(new MouseEvent("contextmenu", {
      bubbles: true, cancelable: true,
    }));
    await record("zone-highlighted");
    firstZone.dispatchEvent(new MouseEvent("contextmenu", {
      bubbles: true, cancelable: true,
    }));
    await record("zone-unhighlighted");
  }

  // The geohash grid: open, descend one character, hide the subgrid,
  // ascend, close.
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

  // The zoom controls, and overview docking while zoomed in — the shelf
  // puts itself away whenever the whole map fits the viewport, so it is only
  // reachable once the view is deeper than the fit.
  tourQuery("#zoom-in").click();
  await record("zoomed-in");
  const dock = tourQuery("#overview-dock");
  if (dock && !tourQuery("#overview-shelf").hidden) {
    dock.click();
    await record("overview-docked");
    dock.click();
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
    change(variant, variant.options[0].value);
    await record("variant-first");
  }
  const mapSelect = tourQuery("#world-select");
  if (mapSelect.options.length > 1) {
    change(mapSelect, mapSelect.options[1].value);
    await record("map-second");
    change(mapSelect, mapSelect.options[0].value);
    await record("map-first");
  }
  if (gameSelect.options.length > 1) {
    change(gameSelect, gameSelect.options[1].value);
    await record("game-second");
    change(gameSelect, firstGame);
    await record("game-first");
  }

  localStorage.clear();
  return { viewport: [window.innerWidth, window.innerHeight], steps };
}

let touring = false;

async function runTour() {
  if (touring) return;
  touring = true;
  badge("parity tour running…");
  try {
    const result = await tour();
    const response = await fetch("/parity/result", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(result, null, 2),
    });
    const path = (await response.text()).trim();
    if (!response.ok) throw new Error(`saving failed: ${response.status} ${path}`);
    badge(`parity tour complete ✓ ${result.steps.length} steps`);
    console.log("parity tour saved to", path);
  } catch (error) {
    badge(`parity tour failed: ${error}`, true);
    console.error("parity tour failed", error);
  } finally {
    touring = false;
  }
}

window.__atlasTour = runTour;
window.addEventListener("keydown", (event) => {
  if (event.key === "F9") runTour();
});
