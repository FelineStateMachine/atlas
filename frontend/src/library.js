import { elements, populateSelect } from "./dom.js";
import { state } from "./state.js";
import { selectVolume } from "./navigation.js";
import { readRoute, readSession } from "./session.js";

// The library is the set of installed bundles, and it can change under a
// running application: a file dropped into the bundles directory, replaced
// with a newer build, or deleted. The backend says so with one event, and
// this refetch reconciles the page with whatever the catalog now holds.

let refreshing = null;

export function refreshCatalog() {
  // Bursts collapse into one refetch; the directory watcher already
  // debounces, so a second signal during a refresh means "look again".
  if (!refreshing) {
    refreshing = reconcile().finally(() => { refreshing = null; });
  }
  return refreshing;
}

async function reconcile() {
  const response = await fetch("/data/catalog.json");
  if (!response.ok) return;
  const previous = state.volume;
  state.catalog = await response.json();
  populateSelect(elements.volume, state.catalog.volumes, "title");
  syncEmptyState();

  if (!state.catalog.volumes.length) return;
  const current = previous && state.catalog.volumes.find((volume) => volume.slug === previous.slug);
  if (!current) {
    // Nothing was open, or the open volume's bundle left: land somewhere that
    // still exists, preferring wherever the address bar or the last session
    // points.
    const route = readRoute();
    await selectVolume(
      route.volume || readSession()?.volume || state.catalog.volumes[0].slug,
      route.volume ? route.world : undefined,
    );
    return;
  }
  if (current.stamp !== previous.stamp) {
    // The open volume was replaced by a newer build. Reload the world that is on
    // screen through the new stamped base, carrying the exact arrangement --
    // camera, filters, panels -- across so the swap reads as the map
    // sharpening rather than the application starting over.
    for (const key of state.textByWorld.keys()) {
      if (key.startsWith(previous.base + "/")) state.textByWorld.delete(key);
    }
    const view = state.engine?.getView();
    state.restore = {
      volume: current.slug,
      world: state.world?.slug,
      lens: state.world ? state.world.lenses.indexOf(state.lens) : 0,
      center: view?.getCenter(),
      zoom: view?.getZoom(),
      hidden: [...state.hiddenCategories],
      collapsed: [...state.collapsedSections],
      overviewDocked: state.overviewDocked,
      dockFolded: state.dockFolded,
      dockDismissed: state.dockDismissed,
    };
    await selectVolume(current.slug, state.world?.slug);
    return;
  }
  // Same volume, same build: only the select needed repainting, but the old
  // catalog objects are gone, so the state points at their replacements.
  state.volume = current;
  elements.volume.value = current.slug;
}

export function syncEmptyState() {
  const empty = !state.catalog?.volumes?.length;
  elements.emptyState.hidden = !empty;
  if (state.catalog?.bundlesDir) elements.emptyPath.textContent = state.catalog.bundlesDir;
}

// importBundles asks the backend to raise the native picker and copy the
// chosen files in. The watcher would notice on its own; refreshing here just
// spares the reader the debounce wait, and the first imported volume is opened
// so the action lands somewhere visible.
export async function importBundles() {
  for (const button of [elements.emptyOpen, elements.addBundles]) button.disabled = true;
  try {
    const response = await fetch("/data/bundles/import", { method: "POST" });
    const outcome = await response.json().catch(() => ({}));
    await refreshCatalog();
    const first = outcome.imported?.[0];
    if (first && state.catalog.volumes.some((volume) => volume.slug === first)) {
      await selectVolume(first);
    }
  } finally {
    for (const button of [elements.emptyOpen, elements.addBundles]) button.disabled = false;
  }
}
