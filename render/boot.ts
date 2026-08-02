// The boot module: the only page JavaScript the seam contributes.
//
// It does three things and nothing else:
//
//   REGISTERS the three custom elements. Until this runs, `<atlas-viewport>`
//   is an unknown tag that renders nothing and breaks nothing — which is what
//   makes the seam deletable rather than merely small.
//
//   OPENS the diagnostics seams the parity harness reads.
//
//   HOOKS the one after-swap rescan issue #5 §4.3 allows. A morph swap can
//   replace the scene node whole; the seam has to be told to look again. That
//   is the entire glue budget: one listener, no `hx-on` anywhere, and no
//   other behaviour on the page belongs to this lane.
//
// Note what is NOT here: no interaction handlers, no route knowledge, no
// session writes. Everything a reader does that is discrete goes to the
// server as an ordinary request, and comes back as a new scene.

import { logger } from "./log.ts";
import { AtlasViewport } from "./viewport.ts";
import { AtlasChart } from "./chart/element.ts";
import { AtlasGlobe } from "./globe/element.ts";
import { expose } from "./diagnostics.ts";

const log = logger("boot");

/** Register the elements. Safe to call twice; the second call does nothing. */
export function register(): void {
  if (customElements.get("atlas-viewport")) return;
  customElements.define("atlas-chart", AtlasChart);
  customElements.define("atlas-globe", AtlasGlobe);
  customElements.define("atlas-viewport", AtlasViewport);
}

/** Every viewport on the page. There is one, but nothing here assumes it. */
function viewports(): AtlasViewport[] {
  return [...document.querySelectorAll("atlas-viewport")]
    .filter((element): element is AtlasViewport => element instanceof AtlasViewport);
}

/**
 * Start the seam.
 *
 * The after-swap hook listens for htmx's own settle event and for a plain
 * custom event, so a page that swaps by some other means — a test, a future
 * framework, a hand-written fetch — has one documented way to say "look
 * again" without this module knowing anything about htmx.
 */
export function boot(): void {
  register();
  const rescan = () => {
    for (const viewport of viewports()) viewport.rescan();
  };
  // Both spellings of the same two events. htmx 4 separates the words with
  // colons -- `htmx:after:swap` -- where htmx 2 camel-cased them, and a seam
  // listening only for the old names is a seam that is never told a swap
  // happened: the scene node is replaced whole, the watcher's observer goes
  // with it, and everything this lane draws quietly stops answering the page.
  // `atlas:rescan` is the plain custom event a page that swaps by some other
  // means can raise without this module knowing anything about htmx.
  const swapped = [
    "htmx:after:swap", "htmx:after:settle",
    "htmx:afterSwap", "htmx:afterSettle",
    "atlas:rescan",
  ];
  for (const name of swapped) document.body.addEventListener(name, rescan);
  for (const viewport of viewports()) expose(viewport);
  log.info("the seam is up", { op: "render", viewports: viewports().length });
}

if (typeof document !== "undefined") {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot, { once: true });
  } else {
    boot();
  }
}
