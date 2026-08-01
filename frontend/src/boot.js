import { elements, populateSelect } from "./dom.js";
import { state } from "./state.js";
import { bindUIEvents } from "./events.js";
import { readRoute, readSession } from "./session.js";
import { selectGame } from "./navigation.js";
import { refreshCatalog, syncEmptyState } from "./library.js";
import { exposeDiagnostics } from "./diagnostics.js";

export async function start() {
  bindUIEvents();
  try {
    const response = await fetch("/data/catalog.json");
    if (!response.ok) throw new Error(`catalog request returned ${response.status}`);
    state.catalog = await response.json();
    populateSelect(elements.game, state.catalog.games, "title");
    syncEmptyState();
    // The library can change while the application is open -- a bundle
    // dropped in, replaced, or removed -- and the backend says so here.
    // Outside the desktop shell there is no event bridge, and the catalog
    // simply stays as fetched.
    window.runtime?.EventsOn?.("atlas:catalog-changed", () => {
      void refreshCatalog();
    });
    if (state.catalog.games.length) {
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
    } else {
      // An empty library is a fresh install, not a failure: the empty state
      // is already showing, and the first bundle to arrive opens on its own.
      elements.loading.hidden = true;
    }
    exposeDiagnostics();
    requestAnimationFrame(() => elements.viewport.focus({ preventScroll: true }));
  } catch (error) {
    elements.loading.hidden = true;
    elements.fatalMessage.textContent = error instanceof Error ? error.message : String(error);
    elements.fatal.hidden = false;
  }
}
