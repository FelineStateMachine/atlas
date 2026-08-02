// Pixels, read and compared, with nothing installed to do it.
//
// The tour was a count, a flag and a string all the way down (SCHEMA.md §7),
// and the one thing it could never say was what the page looked like. The
// screenshot steps say it: a PNG per named step, taken by the driver off the
// live pane, compared against a committed one. This file is the two things
// that needs and the harness did not have -- a PNG reader and a verdict.
//
// NO DEPENDENCY, DELIBERATELY. The repository's npm workspaces exist for the
// two TypeScript lanes; a golden gate that made the harness carry an image
// library would put a third reason in the lock file for something the
// standard library can already do. A PNG is a zlib stream with five filters
// over it, `node:zlib` inflates it, and the sixty lines below unfilter it.
// The subset read is the subset Chromium writes: 8 bits a channel,
// non-interlaced, greyscale or truecolour, with or without alpha. Anything
// else throws by name rather than being guessed at.

import { inflateSync } from "node:zlib";

const SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

/** Channels per pixel, by PNG colour type. Types 3 (palette) is unsupported. */
const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 };

/**
 * Decode one PNG into straight RGBA bytes.
 *
 * Answers `{ width, height, data }` where `data` is `width * height * 4`
 * bytes, row-major, no padding — the same shape `getImageData` answers, so a
 * reader who knows `checkCanvas` in the tour knows this too.
 */
export function decodePNG(bytes) {
  if (!bytes.subarray(0, 8).equals(SIGNATURE)) throw new Error("not a PNG");
  let at = 8;
  let header = null;
  const parts = [];
  while (at + 8 <= bytes.length) {
    const length = bytes.readUInt32BE(at);
    const kind = bytes.toString("ascii", at + 4, at + 8);
    const body = bytes.subarray(at + 8, at + 8 + length);
    at += 12 + length;
    if (kind === "IHDR") {
      header = {
        width: body.readUInt32BE(0),
        height: body.readUInt32BE(4),
        depth: body[8],
        colour: body[9],
        interlace: body[12],
      };
    } else if (kind === "IDAT") {
      parts.push(Buffer.from(body));
    } else if (kind === "IEND") {
      break;
    }
  }
  if (!header) throw new Error("a PNG with no header chunk");
  if (header.depth !== 8) throw new Error(`a PNG at ${header.depth} bits a channel`);
  if (header.interlace !== 0) throw new Error("an interlaced PNG");
  const channels = CHANNELS[header.colour];
  if (!channels) throw new Error(`a PNG of colour type ${header.colour}`);

  const raw = inflateSync(Buffer.concat(parts));
  const { width, height } = header;
  const stride = width * channels;
  const lines = Buffer.alloc(height * stride);
  // Unfiltering: each row carries one filter byte and is reconstructed from
  // the row above it and the pixel to its left, which is the whole of the PNG
  // filter algebra.
  for (let row = 0; row < height; row += 1) {
    const filter = raw[row * (stride + 1)];
    const from = row * (stride + 1) + 1;
    const to = row * stride;
    for (let index = 0; index < stride; index += 1) {
      const value = raw[from + index];
      const left = index >= channels ? lines[to + index - channels] : 0;
      const up = row > 0 ? lines[to - stride + index] : 0;
      const upLeft = row > 0 && index >= channels ? lines[to - stride + index - channels] : 0;
      let restored = value;
      if (filter === 1) restored = value + left;
      else if (filter === 2) restored = value + up;
      else if (filter === 3) restored = value + ((left + up) >> 1);
      else if (filter === 4) restored = value + paeth(left, up, upLeft);
      else if (filter !== 0) throw new Error(`a PNG row filtered ${filter}`);
      lines[to + index] = restored & 0xff;
    }
  }

  const data = new Uint8Array(width * height * 4);
  for (let pixel = 0; pixel < width * height; pixel += 1) {
    const source = pixel * channels;
    const target = pixel * 4;
    if (channels <= 2) {
      const grey = lines[source];
      data[target] = grey;
      data[target + 1] = grey;
      data[target + 2] = grey;
      data[target + 3] = channels === 2 ? lines[source + 1] : 255;
    } else {
      data[target] = lines[source];
      data[target + 1] = lines[source + 1];
      data[target + 2] = lines[source + 2];
      data[target + 3] = channels === 4 ? lines[source + 3] : 255;
    }
  }
  return { width, height, data };
}

function paeth(left, up, upLeft) {
  const estimate = left + up - upLeft;
  const toLeft = Math.abs(estimate - left);
  const toUp = Math.abs(estimate - up);
  const toCorner = Math.abs(estimate - upLeft);
  if (toLeft <= toUp && toLeft <= toCorner) return left;
  return toUp <= toCorner ? up : upLeft;
}

/**
 * What one picture is, in four numbers that survive being written down.
 *
 * `distinct` is the tour's own `checkCanvas` question asked of a picture
 * rather than of a canvas: how many colours are on it, coarsened to 4 bits a
 * channel so that a gradient is not mistaken for detail. A page that drew
 * nothing answers 1. The middle half is sampled rather than the whole,
 * because every pane on this page wears chrome in its corners and a corner
 * button is a second colour on an otherwise empty rectangle.
 */
export function describe({ width, height, data }) {
  const left = Math.floor(width / 4);
  const top = Math.floor(height / 4);
  const right = Math.max(left + 1, Math.floor((width * 3) / 4));
  const bottom = Math.max(top + 1, Math.floor((height * 3) / 4));
  const seen = new Set();
  let red = 0;
  let green = 0;
  let blue = 0;
  let counted = 0;
  for (let y = top; y < bottom; y += 1) {
    for (let x = left; x < right; x += 1) {
      const at = (y * width + x) * 4;
      red += data[at];
      green += data[at + 1];
      blue += data[at + 2];
      counted += 1;
      seen.add(((data[at] >> 4) << 8) | ((data[at + 1] >> 4) << 4) | (data[at + 2] >> 4));
    }
  }
  const mean = (total) => Math.round((total / Math.max(1, counted)) * 10) / 10;
  return { width, height, distinct: seen.size, mean: [mean(red), mean(green), mean(blue)] };
}

/**
 * The threshold, and the argument for it.
 *
 * Two numbers rather than one, because the two ways a screenshot moves are
 * different in kind and a single figure has to be loose enough for the
 * harmless one, which makes it blind to the other.
 *
 *   `mean` — the average absolute difference per colour channel over every
 *   pixel. Text rendered a subpixel over, a tile decoded by a different
 *   version of the same decoder, an easing that finished a frame earlier:
 *   these move a great many pixels by a very little. A build that drew the
 *   sphere black, lost its sprites, or smoothed a pixel-art raster moves the
 *   average by whole numbers.
 *
 *   `differing` — the fraction of pixels that moved more than `NOISE` in any
 *   channel. Antialiasing along an edge moves a few pixels a lot, which the
 *   average forgives and this does not have to: a label that moved is a
 *   fraction of a percent of the picture, and a district that changed colour
 *   is not.
 *
 * THE NUMBERS ARE MEASURED, not guessed. Two fresh-launch walks of tunic,
 * taken one after the other, produced six pairs of pictures that were
 * *identical* — mean 0, nothing moved at all. The tour settles every step
 * before it is recorded and a settled page draws the same frame twice, so the
 * noise this has to tolerate on the machine that captures the baselines is
 * none. The room below is for the machine that did not capture them: a
 * different Chromium, a different font stack, a GPU that antialiases
 * differently. Against a deliberate mismatch it was checked the other way —
 * a 60×60 patch recoloured (a district drawn wrong, a part not drawn) moves
 * 0.76% of the picture and fails; a whole picture three shades darker (a
 * texture lost, a gamma changed) moves the mean to 2.33 and fails; a
 * thousandth of the pixels moved 30 apiece (edges, a label a pixel over)
 * passes.
 */
export const THRESHOLD = { mean: 0.5, differing: 0.002 };
const NOISE = 12;

/** Compare two decoded pictures. Answers the verdict and the two numbers. */
export function comparePixels(baseline, candidate) {
  if (baseline.width !== candidate.width || baseline.height !== candidate.height) {
    return {
      ok: false,
      reason: `${baseline.width}×${baseline.height} → ${candidate.width}×${candidate.height}`,
      mean: null,
      differing: null,
    };
  }
  const count = baseline.width * baseline.height;
  let total = 0;
  let moved = 0;
  for (let pixel = 0; pixel < count; pixel += 1) {
    const at = pixel * 4;
    const red = Math.abs(baseline.data[at] - candidate.data[at]);
    const green = Math.abs(baseline.data[at + 1] - candidate.data[at + 1]);
    const blue = Math.abs(baseline.data[at + 2] - candidate.data[at + 2]);
    total += red + green + blue;
    if (Math.max(red, green, blue) > NOISE) moved += 1;
  }
  const mean = total / (count * 3);
  const differing = moved / count;
  const ok = mean <= THRESHOLD.mean && differing <= THRESHOLD.differing;
  return {
    ok,
    reason: ok ? "" : `mean ${mean.toFixed(2)} (≤ ${THRESHOLD.mean}),` +
      ` ${(differing * 100).toFixed(2)}% of pixels moved (≤ ${THRESHOLD.differing * 100}%)`,
    mean: Math.round(mean * 100) / 100,
    differing: Math.round(differing * 10000) / 10000,
  };
}
