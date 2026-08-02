// The mark a collection wears, composed once and worn by both panes.
//
// A MARKER IS A SYMBOL, NOT A BUBBLE. The collection's colour is carried by
// the symbol itself — the glyph is a silhouette and it is filled with that
// colour — and what keeps it legible over whatever art the world is drawn on
// is an outset: the same silhouette, filled with the world's rim colour and
// stamped in a small disc around the symbol, so the mark wears a halo cut to
// its own shape. A disc behind the glyph with a black symbol punched out of
// it says the same thing about every collection and hides the one drawing
// that told them apart.
//
// THE RASTER is 64×64 and is composed once per asset, colour and rim, then
// handed to both panes: OpenLayers draws it as an `Icon`, three draws it into
// a sprite's texture. Two panes composing their own would be two answers to
// "what is a Shrine", and the halo — the expensive half — would have to be
// written twice.
//
// A GLYPH IS TINTED AND A PICTURE IS NOT. A glyph is a monochrome silhouette
// whose colour is the reader's to assign; a picture already carries its own,
// and flattening it to one colour would leave nothing but its outline filled
// in. `atlas.icon.kind` is curation's word on which of the two arrived.
//
// THE COLOUR LADDER is here for the same reason the halo is: a colour close
// to the rim it is drawn against leaves the mark with nothing to stand
// against, so a colour that collides with the outset is taken to a lighter or
// darker shade of the same hue. The hue survives, which is what keeps the
// legend and the map agreeing about what a collection looks like.

import { KEY_ICON_KIND } from "@atlas/analysis/semconv/keys";
import type { Collection } from "../data/payload.ts";

const LIGHT_OUTSET = "rgba(255, 255, 255, 0.96)";

/** The rim a world's markers wear to stay legible against its art. */
export const OUTSET_COLORS: Readonly<Record<string, string>> = {
  light: LIGHT_OUTSET,
  dark: "rgba(7, 9, 7, 0.98)",
};

/**
 * The colour a declared outset names.
 *
 * `atlas.icon.outset` is curation's word about the art a world is drawn on —
 * a dark rim on a pale map, a pale rim on a dark one — and it arrives as the
 * token, not the colour. Anything that is not exactly `dark` reads as light,
 * which is the reference's own rule and the reason an unset or misspelled
 * outset still draws a legible marker rather than none.
 */
export function outsetColor(outset: string): string {
  return OUTSET_COLORS[outset] ?? LIGHT_OUTSET;
}

/**
 * A colour that can be seen against the rim it will be drawn with.
 *
 * A dark rim needs a mark bright enough to sit inside it and a light rim
 * needs one dark enough. Rather than rim it the other way — which would put a
 * pale halo on a world that asked for dark ones — the colour itself is taken
 * to a lighter or darker shade of the same hue, so the collection is still
 * recognisably its own colour wherever else it is drawn.
 */
export function legibleIconColor(color: string, outset: string): string {
  const dark = outset === "dark";
  const luminance = relativeLuminance(color);
  if (dark ? luminance > 0.3 : luminance < 0.88) return color;
  return withLightness(color, dark ? 0.74 : 0.42);
}

/**
 * The same hue and saturation at a different lightness.
 *
 * Ungamma'd, deliberately: this is the reference's own arithmetic, and the
 * numbers the ladder above is calibrated against are these ones. Anything
 * that is not a six-digit hex is handed back untouched — a collection may
 * declare `rgb()` or a keyword, and a colour we cannot take apart is better
 * drawn as it was asked for than guessed at.
 */
export function withLightness(color: string, lightness: number): string {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return color;
  const [r = 0, g = 0, b = 0] = [0, 2, 4].map(
    (at) => Number.parseInt(hex.slice(at, at + 2), 16) / 255);
  const high = Math.max(r, g, b);
  const low = Math.min(r, g, b);
  const light = (high + low) / 2;
  const chroma = high - low;
  const saturation = chroma === 0 ? 0 : chroma / (1 - Math.abs(2 * light - 1));
  let hue = 0;
  if (chroma !== 0) {
    if (high === r) hue = ((g - b) / chroma) % 6;
    else if (high === g) hue = (b - r) / chroma + 2;
    else hue = (r - g) / chroma + 4;
    hue *= 60;
    if (hue < 0) hue += 360;
  }
  const c = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = lightness - c / 2;
  const [rr, gg, bb] =
    hue < 60 ? [c, x, 0] : hue < 120 ? [x, c, 0] : hue < 180 ? [0, c, x] :
    hue < 240 ? [0, x, c] : hue < 300 ? [x, 0, c] : [c, 0, x];
  const channel = (value: number): string =>
    Math.round((value + m) * 255).toString(16).padStart(2, "0");
  return `#${channel(rr)}${channel(gg)}${channel(bb)}`;
}

/** How bright a colour reads, on the reference's ungamma'd weights. */
export function relativeLuminance(color: string): number {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return 0.5;
  const channel = (at: number): number => Number.parseInt(hex.slice(at, at + 2), 16) / 255;
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4);
}

/** A collection's name, cut down to what fits inside a marker. */
export function initialsOf(title: string): string {
  return title.split(/\s+/).slice(0, 2).map((part) => part[0] ?? "").join("");
}

/**
 * Whether a collection's artwork is a picture rather than a glyph.
 *
 * `atlas.icon.kind` names what a file suffix used to imply, and `iconPicture`
 * is the manifest's own copy of the same fact.
 */
export function iconIsPicture(collection: Collection): boolean {
  return Boolean(collection.iconPicture || collection.attrs?.[KEY_ICON_KIND] === "picture");
}

/** Everything a raster is composed from, which is everything it is keyed by. */
export interface MarkerFace {
  /** The asset path, or "" for a collection with no artwork at all. */
  readonly asset: string;
  /** Where that asset lives, already resolved against the build's base. */
  readonly url: string;
  readonly picture: boolean;
  /** The collection's colour, already taken through the ladder above. */
  readonly color: string;
  /** The rim's colour, already resolved from the world's outset token. */
  readonly outset: string;
}

/**
 * What two faces have to agree on to share a raster.
 *
 * Not the collection: one asset drawn in one colour with one rim is one
 * image, and a world that gives Shrine and Daedric Shrine the same artwork
 * and the same accent composes it once.
 */
export function markerKey(face: MarkerFace): string {
  return `${face.asset}:${face.color}:${face.outset}`;
}

// A raster is 64 wide with the glyph inset 6 on every side, which leaves the
// halo its 3 pixels of reach plus a pixel of air. The disc is every offset
// within a radius of √10 — a rounder rim than a 7×7 square, and 37 stamps
// rather than 49.
const RASTER = 64;
const INSET = 6;
const GLYPH = 52;
const HALO = 3;
const HALO_REACH = 10;

/**
 * The raster itself: a haloed, tinted symbol on a transparent field.
 *
 * The halo goes down first and the symbol last, so the rim is behind the
 * mark rather than over it — stamping the other way around would leave every
 * glyph looking eroded at its edges.
 */
export function composeMarker(source: CanvasImageSource, face: MarkerFace): string {
  const canvas = surface();
  const paper = canvas?.getContext("2d");
  // A page with no canvas draws no marker; the initials fallback covers it.
  if (!canvas || !paper) return "";
  const tinted = silhouette(source, face.picture ? "" : face.color, !face.picture);
  const outline = silhouette(source, face.outset);
  if (!tinted || !outline) return "";
  for (let y = -HALO; y <= HALO; y++) {
    for (let x = -HALO; x <= HALO; x++) {
      if (x * x + y * y <= HALO_REACH) paper.drawImage(outline, x, y);
    }
  }
  paper.drawImage(tinted, 0, 0);
  return canvas.toDataURL("image/png");
}

/**
 * The glyph on its own canvas, filled with one colour when it is asked for.
 *
 * `source-in` keeps the drawn shape and replaces its pixels, which is what
 * turns a monochrome symbol into a coloured one without knowing anything
 * about the colour it arrived in. Smoothing is only ever spoken about for the
 * tint: a picture is scaled nearest-neighbour so its own pixels survive, a
 * glyph is smoothed because it is about to become a silhouette anyway.
 */
function silhouette(
  source: CanvasImageSource, fill: string, smoothing?: boolean,
): HTMLCanvasElement | null {
  const canvas = surface();
  const paper = canvas?.getContext("2d");
  if (!canvas || !paper) return null;
  if (smoothing !== undefined) paper.imageSmoothingEnabled = smoothing;
  paper.drawImage(source, INSET, INSET, GLYPH, GLYPH);
  if (fill) {
    paper.globalCompositeOperation = "source-in";
    paper.fillStyle = fill;
    paper.fillRect(0, 0, RASTER, RASTER);
  }
  return canvas;
}

function surface(): HTMLCanvasElement | null {
  const canvas = document.createElement("canvas");
  canvas.width = RASTER;
  canvas.height = RASTER;
  return canvas;
}

/** Rasters that have finished, whether they arrived or failed. */
const composed = new Map<string, string | null>();
/** Rasters that have been asked for, so no asset is ever fetched twice. */
const asked = new Map<string, Promise<string | null>>();

/**
 * The raster for a face if it is already composed, and nothing if it is not.
 *
 * The synchronous half, for a style function: a style is evaluated inside a
 * render and cannot wait for an image. What a pane draws while it waits — and
 * for a collection whose artwork never arrives — is the initials fallback,
 * which is also what a build with no icons at all draws.
 */
export function markerRaster(face: MarkerFace): string | null {
  if (!face.asset) return null;
  return composed.get(markerKey(face)) ?? null;
}

/**
 * The raster for a face, composing it if this is the first ask.
 *
 * A failure is remembered as permanently as a success: a collection whose
 * artwork 404s wears its initials for the life of the page rather than asking
 * again on every repaint.
 */
export function markerRasterReady(face: MarkerFace): Promise<string | null> {
  if (!face.asset || !face.url) return Promise.resolve(null);
  const key = markerKey(face);
  const held = asked.get(key);
  if (held) return held;
  const work = new Promise<string | null>((resolve) => {
    const source = new Image();
    // The raster is exported as a data URL, and a canvas tainted by an image
    // fetched without CORS cannot be exported at all — so a cross-origin
    // build's icons are asked for in the one way that can be composed.
    source.crossOrigin = "anonymous";
    source.onload = (): void => { resolve(composeMarker(source, face) || null); };
    source.onerror = (): void => { resolve(null); };
    source.src = face.url;
  }).then((raster) => {
    composed.set(key, raster);
    return raster;
  });
  asked.set(key, work);
  return work;
}

/** Forget every composed raster. For tests, which compose their own. */
export function forgetMarkerRasters(): void {
  composed.clear();
  asked.clear();
}
