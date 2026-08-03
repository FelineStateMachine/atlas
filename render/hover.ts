// Which legend row the pointer is over.
//
// The carried stylesheet reveals two controls on rest -- the isolate
// crosshair (`assets/css/layer-header.css`) and the label-policy toggle
// (`assets/css/category-rows.css`) -- and it reveals them under
// `.category-row.is-hovered` / `.layer-header.is-hovered` rather than under
// `:hover`. Nothing in the clean-room build ever wrote that class, so both
// controls sat at `opacity: 0` for the whole life of the page: implemented,
// wired to their routes, and unreachable by pointer.
//
// THE CLASS IS NOT A REPLACEMENT FOR `:hover`, IT IS A CORRECTION OF IT. The
// reference tracked the pointer's position on purpose, and the CSS says so in
// a comment that names this function. A list scrolls under a still cursor --
// the wheel over the legend, a swap that shortened a section, the keyboard --
// and the browser does not reliably revise `:hover` when the ground moves
// under a pointer that did not. The row that was under the cursor kept
// offering its actions to a pointer that had long since left it. Asking where
// the pointer actually is answers correctly in both cases, which is why the
// answer is recomputed on scroll as well as on movement.
//
// WHY THIS LANE. Where the pointer is is continuous, ephemeral interaction
// state (issue #5 §4.1): it survives no swap, belongs in no session, and
// posting it would be posting a mouse. The application renders the rows and
// owns the stylesheet that reads the mark; this lane writes the mark, the way
// it wires the zoom buttons and the navigator's field without owning either.
//
// WHAT A SWAP DOES TO IT is the whole of the mechanism below. `#atlas-legend`
// is an `outerMorph` region, so every filtering move can replace the list
// whole -- and htmx's morph *preserves* a node whose id matches, so it can
// also hand back the very same container, and rows carrying the mark they
// wore before the swap. One rule covers both: every pass clears every mark in
// the container before it writes one, and a rescan is a pass.

/**
 * The three things a mark may land on: a collection's row, a section's head,
 * or a row of a feature index -- which reveals an action of its own (ask after
 * this ground and no other) and is therefore the same kind of thing to the
 * pointer, however differently it is built.
 */
const ROWS = ".category-row, .layer-header, .zone-index-row";

/** The class the carried stylesheet reads. */
const MARK = "is-hovered";

/**
 * The pointer over the legend, and the row it is over.
 *
 * The container is a selector rather than an element, for the same reason the
 * scene node is (`scene/observe.ts`): a swap replaces it, and what stays true
 * across the swap is the name, never the node.
 *
 * A stopped tracker is spent -- the controller it registered everything
 * against cannot be re-armed -- so an element coming back to the page mints a
 * new one, exactly as it re-wires its keyboard.
 */
export class RowHover {
  private readonly selector: string;
  private readonly leaving = new AbortController();
  // THE SET IS PER TRACKER, and that is the difference between this and the
  // viewport's module-level `wired`. There, a listener is never taken off, so
  // the fact that a control has one has to outlive every element. Here the
  // listeners leave with the tracker, so a container remembered past its
  // tracker's life would be a container nobody is listening to -- the seam
  // reconnected, the legend still and dead. Weak either way, so a list that
  // leaves the page leaves this with it.
  private readonly wired = new WeakSet<Element>();
  private pointerAt: { x: number; y: number } | null = null;

  constructor(selector = "#layers") {
    this.selector = selector;
  }

  /** Wire whatever is under the selector now. */
  start(): void {
    this.rescan();
  }

  /**
   * The after-swap hook: wire the container on screen, and say again what is
   * true of it.
   *
   * Both halves matter. A swap that replaced the list hands back a container
   * this has never listened to; a swap that morphed it in place hands back the
   * same container carrying marks from before, on rows that may not even be
   * under the pointer any more. The mark pass is unconditional for the second
   * case, and it runs before the wiring guard so it also covers the first.
   */
  rescan(): void {
    const layers = this.layers();
    if (!layers) return;
    this.mark();
    if (this.wired.has(layers)) return;
    this.wired.add(layers);
    const { signal } = this.leaving;
    layers.addEventListener("pointermove", (event: PointerEvent) => {
      this.pointerAt = { x: event.clientX, y: event.clientY };
      this.mark();
    }, { passive: true, signal });
    layers.addEventListener("pointerleave", () => {
      this.pointerAt = null;
      this.mark();
    }, { signal });
    // Passive, and deliberately not throttled: the work is one hit test and a
    // class, and a mark that lands a frame late is a control that flickers on
    // a row the reader has already left.
    layers.addEventListener("scroll", () => this.mark(), { passive: true, signal });
  }

  /** Give the listeners back. A stopped tracker marks nothing again. */
  stop(): void {
    this.pointerAt = null;
    this.leaving.abort();
  }

  /**
   * Clear every mark in the list, then mark the row the pointer is over.
   *
   * Clearing first is what makes this idempotent, and idempotence is what
   * makes it safe to call from a swap: however the page arrived at the marks
   * it is wearing -- a previous pass, a morph that carried them across, a row
   * that moved out from under the cursor -- the answer afterwards is the one
   * the pointer's own position gives.
   *
   * The containment check is not redundant with the listener's scope. The hit
   * test is asked of the whole document, and a menu, a card or the dock drawn
   * over the legend answers it with an element of its own; only a row this
   * list actually holds may be marked.
   */
  private mark(): void {
    const layers = this.layers();
    if (!layers) return;
    for (const marked of [...layers.querySelectorAll(`.${MARK}`)]) {
      marked.classList.remove(MARK);
    }
    const at = this.pointerAt;
    if (!at) return;
    const under = document.elementFromPoint(at.x, at.y);
    const row = under?.closest(ROWS) ?? null;
    if (row && layers.contains(row)) row.classList.add(MARK);
  }

  /** The list as it is on the page now, never as it was when this was made. */
  private layers(): HTMLElement | null {
    return document.querySelector<HTMLElement>(this.selector);
  }
}
