import { sessionKey } from "./constants.js";
import { state } from "./state.js";

// Kept per volume rather than as a single place, so wandering off to another
// volume and coming back does not cost the reader where they were in this one.
export function saveSession() {
  // Nothing is written while a world is being swapped in: the arrangement is
  // half the old one and half the new until it settles.
  if (!state.world || !state.lens || state.settling) return;
  const view = state.engine?.getView();
  try {
    const stored = allSessions();
    stored.last = state.volume.slug;
    stored.volumes[state.volume.slug] = {
      volume: state.volume.slug,
      world: state.world.slug,
      lens: state.world.lenses.indexOf(state.lens),
      center: view?.getCenter(),
      zoom: view?.getZoom(),
      hidden: [...state.hiddenCollections],
      collapsed: [...state.collapsedSections],
      expanded: [...state.expandedCollections],
      labels: [...state.labelOverrides],
      render: [...state.renderOverrides],
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
    if (stored && stored.volumes) return stored;
  } catch {
    // Falls through to a fresh record.
  }
  return { last: "", volumes: {} };
}

export function readSession(volumeSlug) {
  const stored = allSessions();
  const wanted = volumeSlug || stored.last;
  const entry = wanted && stored.volumes[wanted];
  return entry && entry.volume && entry.world ? entry : null;
}

export function readRoute() {
  const [volume, world] = decodeURIComponent(location.hash.replace(/^#\/?/, ""))
    .split("/")
    .map((part) => part.trim());
  return { volume, world };
}

export function writeRoute() {
  if (!state.volume || !state.world) return;
  const route = `#${state.volume.slug}/${state.world.slug}`;
  if (location.hash === route) return;
  // Replaced rather than pushed: this records a location, not a trail.
  history.replaceState(null, "", route);
}
