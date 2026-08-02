// The half of the keyboard that is not a route.
//
// The application spells its shortcuts as hx-triggers, because every one of
// them moves discrete state and a route is where discrete state is moved
// (issue #5 §4.1, internal/app/templates/shell.tmpl). What is left over is
// this file, and the line between them is not arbitrary: nothing here writes
// a session, and nothing here would survive being posted.
//
//   HELD, NOT SWITCHED. Z raises every name for as long as the key is down.
//   There is no state to leave on by mistake and no control to find first,
//   and there is nothing to tell the server: the moment the key comes up the
//   map is back.
//   FOCUS. ⌘K reaches the search field, Escape hands the keyboard back from
//   the grid field to the map, and G hands it on to the grid's own field and
//   takes it back again. Where the caret is is not a fact about the volume.
//   WHICH PANE IS UP. The backquote flips chart and sphere, which is the same
//   world seen from a different distance -- the same filters, the same
//   selection, the same camera.
//   ZOOM. +/- step the chart the way the buttons beside it do.
//   AND SUPPRESSION. The webview's own menu offers Reload and Inspect
//   Element, which belong to a browser rather than to a map.
//
// ONE LISTENER PER EVENT, ON THE WINDOW, IN THE CAPTURE PHASE, and all four
// of those words are load-bearing:
//
//   One, because a listener per control is a listener re-added every time a
//   swap carries its control across -- the bug that made one press of the
//   globe toggle flip the panes four times.
//   On the window, because the controls this reads (the grid field, the
//   search field, the panel's fold button) are the application's and are
//   replaced by swaps; every one of them is resolved at the moment a key is
//   pressed rather than held from before.
//   In the capture phase, because the grid field's Escape has to be able to
//   stop the window's own ascend from hearing it, and a window listener that
//   waits for the bubble is too late to stop anything.
//
// Everything is registered against one AbortController, so a viewport that
// leaves the page takes its keyboard with it.

/**
 * What the seam can answer with, as the keyboard asks for it.
 *
 * The viewport implements this. It is an interface rather than the class
 * because the four things below are the whole of what a keystroke may reach
 * for in this lane, and a test can stand in for all of them.
 */
export interface KeyboardHost {
  /** Z, held: every name up while the key is down, the map back when it isn't. */
  holdLabels(down: boolean): void;
  /** The backquote's flip. Called only when the page offers a sphere. */
  flipPane(): void;
  /** One step of zoom, on whichever pane is up. */
  zoomBy(delta: number): void;
  /** Whether the sphere is up. The zoom keys are inert behind it. */
  readonly sphereUp: boolean;
}

/**
 * Wire the seam's keyboard. Returns the way to take it off again.
 *
 * Calling this twice without calling the answer in between would double every
 * shortcut, which is why the viewport keeps the answer in a field and calls it
 * before it wires again.
 */
export function wireKeyboard(host: KeyboardHost): () => void {
  const stop = new AbortController();
  const { signal } = stop;

  window.addEventListener("keydown", (event) => pressed(host, event, signal),
    { capture: true, signal });
  // No guard and no preventDefault on the way up. A key that went down inside
  // a text field still has to be allowed to come up: the alternative is labels
  // stuck on because the reader clicked into the search box mid-hold.
  window.addEventListener("keyup", (event) => {
    if (event.key.toLowerCase() === "z") host.holdLabels(false);
  }, { signal });
  // A window that loses focus with the key down never hears it come back up.
  window.addEventListener("blur", () => host.holdLabels(false), { signal });
  // Text fields keep their menu, where cut and paste are worth having.
  window.addEventListener("contextmenu", (event) => {
    if (editable(event.target)) return;
    event.preventDefault();
  }, { signal });

  return () => stop.abort();
}

/**
 * The ladder, in the reference implementation's own order.
 *
 * Each rung either answers the key and returns or leaves it for the next one,
 * and the order is the meaning: the field before the guard, the guard before
 * everything a reader would rather type than trigger.
 */
function pressed(host: KeyboardHost, event: KeyboardEvent, signal: AbortSignal): void {
  // The grid field speaks for itself first. Everything it does not claim
  // stops here anyway -- it is an input, and the guard below would have
  // stopped it one line later.
  //
  // The field is compared only once it is known to be there. A page with no
  // grid navigator answers `null` for it, and a keystroke aimed at no element
  // in particular answers `null` too, so the unguarded comparison would hand
  // every window-level key to the field's own ladder.
  const field = find("#grid-input");
  if (field !== null && event.target === field) {
    inField(host, event);
    return;
  }
  if (editable(event.target)) return;
  // The map's own keys, heard while the map has the focus. They are checked
  // before the window's because that is where the reference had them -- on
  // the surface rather than on the window -- and a key the surface answers is
  // not offered to anything else.
  if (overMap(event.target) && onMap(host, event)) return;
  // The backquote flips between chart and globe and touches nothing else.
  // Only a map that declared itself a sphere answers it, and the key is
  // swallowed either way: a backquote is not a character this page wants.
  if (event.key === "`" && !event.metaKey && !event.ctrlKey && !event.altKey) {
    if (offersSphere()) {
      event.preventDefault();
      host.flipPane();
    }
    return;
  }
  // G is the application's key -- it posts the grid on and off -- and the
  // keyboard it hands on is this lane's. The keystroke is not answered here,
  // only noted: nothing is swallowed, nothing is moved, and the route the
  // shell declares against this very filter does the rest.
  if (event.key.toLowerCase() === "g" && !event.metaKey && !event.ctrlKey &&
    !event.altKey && !event.repeat) {
    followGrid(signal);
  }
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    const search = find<HTMLInputElement>("#pin-search");
    search?.focus();
    search?.select();
    return;
  }
  // Z is last, and it is the one key that is answered whether or not it
  // changes anything: a held key repeats, and every repeat after the first
  // says what the first one already said.
  if (event.key.toLowerCase() === "z") {
    event.preventDefault();
    if (!event.repeat) host.holdLabels(true);
  }
}

/**
 * Inside the grid field.
 *
 * Space is missing from this list on purpose. It flips the subgrid from in
 * here exactly as it does from the map, but flipping the subgrid is discrete
 * state, so it is a route the application declares against this very field
 * (shell.tmpl) rather than a handler here. The two keys below are not: one
 * moves which pane is up, and one moves the focus.
 */
function inField(host: KeyboardHost, event: KeyboardEvent): void {
  // No hash will ever spell a backquote, so the pane flip reaches in here too.
  if (event.key === "`") {
    event.preventDefault();
    if (offersSphere()) host.flipPane();
    return;
  }
  if (event.key !== "Escape") return;
  // THE stopPropagation IS THE POINT. Escape leaves the field before it
  // leaves the level: the first press hands the keyboard back to the map and
  // the window's ascend never hears it, and the next press -- with the map
  // focused now -- telescopes out as ever.
  event.preventDefault();
  event.stopPropagation();
  find<HTMLElement>("#map")?.focus({ preventScroll: true });
}

/**
 * On the map surface.
 *
 * Escape is missing from this list for the same reason Space is missing from
 * the field's: closing the card is discrete state and the application posts
 * it, from a trigger scoped to this same surface. What is left is the zoom,
 * which is continuous and nobody's business but this lane's.
 *
 * The keys are answered whichever pane is up, and only the chart moves. On
 * the sphere the distance is the buttons' to change -- there is no zoom level
 * to step -- but swallowing the keystroke either way keeps a shortcut from
 * being a shortcut on one pane and a rejection tone on the other.
 */
function onMap(host: KeyboardHost, event: KeyboardEvent): boolean {
  if (event.key === "+" || event.key === "=") {
    if (!host.sphereUp) host.zoomBy(1);
  } else if (event.key === "-") {
    if (!host.sphereUp) host.zoomBy(-1);
  } else {
    return false;
  }
  event.preventDefault();
  return true;
}

/**
 * Follow the G key's own answer, once.
 *
 * The reference hands the keyboard on when the grid opens and takes it back
 * when it closes: the field that takes a hash is focused and selected on the
 * way in -- so "gm6" arrives at m6, the rest of the word being typed landing
 * where a hash is typed -- and the map is focused on the way out, where the
 * shortcuts live.
 *
 * Both halves are focus, so both are this file's. Neither can be done from
 * the keydown: what they follow is the *answer* to the key's post, and until
 * the swap has settled the navigator on screen is still the one from before.
 * So the press arms a listener for one settle and asks the page which way it
 * went -- the navigator says whether it is up, and nothing here has to
 * remember what it was.
 *
 * THE PRESS FOLLOWS ITS OWN SWAP, NOT THE NEXT ONE TO GO PAST. An earlier
 * draft took the first settle of any kind, on the argument that a request
 * already in flight settling first only lands the keystroke a beat early. It
 * does not: the page settles a search, a camera report and a session island
 * without the navigator moving at all, and a press that spent itself on one of
 * those asked a navigator that had not been swapped yet, read it as still shut,
 * and put the keyboard back on the map -- so G opened the grid and left the
 * reader typing into whatever they were typing into before. It was a race, so
 * it was intermittent, which is the worst way for a focus bug to be wrong.
 *
 * The grid's answer is identifiable: every route the key posts renders the
 * navigator's own region (internal/app/session.go, the `grid` concern), so the
 * settle to follow is the one whose target is that region. Anything else is
 * left alone and the listener stays armed for the swap it was waiting for.
 *
 * ONE PRESS AT A TIME. A second press before the first was answered replaces
 * it rather than stacking a second listener, because two listeners would both
 * fire on the one swap and the second would be asking about a page the first
 * has already moved. The signal is the viewport's, so a seam that leaves the
 * page leaves no armed press behind.
 */
let following: (() => void) | null = null;

function followGrid(signal: AbortSignal): void {
  following?.();
  const answered = (event: Event) => {
    const navigator = find<HTMLElement>("#atlas-grid-navigator");
    // A page that renders no navigator can never answer, so the press is
    // spent here rather than left armed: the keyboard belongs on the map,
    // which is where every shortcut that is not a route is heard.
    if (navigator === null) {
      stop();
      find<HTMLElement>("#map")?.focus({ preventScroll: true });
      return;
    }
    const target = event.target;
    // The region itself, or anything the swap left inside it. `contains`
    // answers false for anything that is not a node in it, including the
    // window an event with no element behind it names.
    if (target !== navigator && !navigator.contains(target as Node | null)) return;
    stop();
    if (!navigator.hidden) {
      const field = find<HTMLInputElement>("#grid-input");
      field?.focus({ preventScroll: true });
      field?.select();
      return;
    }
    // The grid went away. The keyboard goes back to the map with it.
    find<HTMLElement>("#map")?.focus({ preventScroll: true });
  };
  const stop = () => {
    window.removeEventListener("htmx:after:settle", answered);
    following = null;
  };
  following = stop;
  window.addEventListener("htmx:after:settle", answered, { signal });
  signal.addEventListener("abort", () => { following = null; }, { once: true });
}

/** Whether the page is offering a sphere, asked of the control that says so. */
function offersSphere(): boolean {
  const toggle = find<HTMLElement>("#globe-toggle");
  return toggle !== null && !toggle.hidden;
}

/** Whether a keystroke landed on the map surface. */
function overMap(target: EventTarget | null): boolean {
  const map = find("#map");
  if (!map) return false;
  return target === map || (target instanceof Element && map.contains(target));
}

/**
 * Whether a keystroke is a reader typing.
 *
 * A select counts, and that is not an oversight: a select answers the space
 * bar and the arrow keys itself, and a shortcut that took them would make the
 * control unusable. Anything that is not an Element at all -- the window,
 * which is what a synthetic key from the parity tour is dispatched at -- is
 * nobody's typing.
 */
function editable(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  if (/^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return true;
  return (target as HTMLElement).isContentEditable === true;
}

/** The application's controls, resolved at the moment they are needed. */
function find<T extends Element>(selector: string): T | null {
  return document.querySelector<T>(selector);
}
