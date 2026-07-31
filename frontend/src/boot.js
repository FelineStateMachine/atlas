import { elements, populateSelect } from "./dom.js";
import { state } from "./state.js";
import { bindUIEvents } from "./events.js";
import { initializeMap } from "./engine.js";
import { readRoute, readSession } from "./session.js";
import { selectGame } from "./navigation.js";
import { exposeDiagnostics } from "./diagnostics.js";

export async function start() {
  bindUIEvents();
  try {
    const response = await fetch("/static/catalog.json");
    if (!response.ok) throw new Error(`catalog request returned ${response.status}`);
    state.catalog = await response.json();
    if (!state.catalog.games.length) throw new Error("the embedded catalog contains no maps");
    initializeMap();
    populateSelect(elements.game, state.catalog.games, "title");
    const route = readRoute();
    const session = readSession();
    // An address naming somewhere else was typed on purpose, so it wins and
    // arrives clean; anything else resumes where the reader left off.
    const resuming = session &&
      (!route.game || (route.game === session.game && route.map === session.map));
    state.restore = resuming ? session : null;
    await selectGame(
      route.game || session?.game || state.catalog.games[0].slug,
      route.map || session?.map,
    );
    state.restore = null;
    exposeDiagnostics();
    requestAnimationFrame(() => elements.viewport.focus({ preventScroll: true }));
  } catch (error) {
    elements.loading.hidden = true;
    elements.fatalMessage.textContent = error instanceof Error ? error.message : String(error);
    elements.fatal.hidden = false;
  }
}
