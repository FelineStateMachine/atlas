import { sessionKey } from "./constants.js";
import { state } from "./state.js";

// Kept per game rather than as a single place, so wandering off to another
// game and coming back does not cost the reader where they were in this one.
export function saveSession() {
  // Nothing is written while a map is being swapped in: the arrangement is
  // half the old one and half the new until it settles.
  if (!state.map || !state.variant || state.settling) return;
  const view = state.engine?.getView();
  try {
    const stored = allSessions();
    stored.last = state.game.slug;
    stored.games[state.game.slug] = {
      game: state.game.slug,
      map: state.map.slug,
      variant: state.map.variants.indexOf(state.variant),
      center: view?.getCenter(),
      zoom: view?.getZoom(),
      hidden: [...state.hiddenCategories],
      collapsed: [...state.collapsedSections],
      overviewDocked: state.overviewDocked,
      dockFolded: state.dockFolded,
      dockDismissed: state.dockDismissed,
    };
    localStorage.setItem(sessionKey, JSON.stringify(stored));
  } catch {
    // A browsing session that cannot be written is not worth failing over.
  }
}

export function allSessions() {
  try {
    const stored = JSON.parse(localStorage.getItem(sessionKey) || "null");
    if (stored && stored.games) return stored;
  } catch {
    // Falls through to a fresh record.
  }
  return { last: "", games: {} };
}

export function readSession(gameSlug) {
  const stored = allSessions();
  const wanted = gameSlug || stored.last;
  const entry = wanted && stored.games[wanted];
  return entry && entry.game && entry.map ? entry : null;
}

export function readRoute() {
  const [game, map] = decodeURIComponent(location.hash.replace(/^#\/?/, ""))
    .split("/")
    .map((part) => part.trim());
  return { game, map };
}

export function writeRoute() {
  if (!state.game || !state.map) return;
  const route = `#${state.game.slug}/${state.map.slug}`;
  if (location.hash === route) return;
  // Replaced rather than pushed: this records a location, not a trail.
  history.replaceState(null, "", route);
}
