// Enough PNG to cut one icon out of a marker sprite sheet.
//
// Some games publish category icons as a single strip that CSS windows into
// with background-position, rather than as a font. Slicing that strip is the
// only raster work the icon step needs, and Node's zlib covers it, so it stays
// dependency-free rather than pulling in an image library for one operation.
//
// Only what MapGenie actually serves is handled: 8-bit non-interlaced images.

import zlib from "node:zlib";

const SIGNATURE = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);

const crcTable = (() => {
  const table = new Int32Array(256);
  for (let index = 0; index < 256; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
    }
    table[index] = value;
  }
  return table;
})();

const crc32 = (buffer) => {
  let value = -1;
  for (const byte of buffer) value = crcTable[(value ^ byte) & 0xff] ^ (value >>> 8);
  return (value ^ -1) >>> 0;
};

const chunk = (type, body) => {
  const header = Buffer.alloc(8);
  header.writeUInt32BE(body.length, 0);
  header.write(type, 4, "ascii");
  const trailer = Buffer.alloc(4);
  trailer.writeUInt32BE(crc32(Buffer.concat([header.subarray(4), body])), 0);
  return Buffer.concat([header, body, trailer]);
};

const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 };

// Each row is filtered against the one above it, so decoding is sequential.
const unfilter = (raw, width, height, bytesPerPixel) => {
  const stride = width * bytesPerPixel;
  const out = Buffer.alloc(height * stride);
  let source = 0;
  for (let y = 0; y < height; y += 1) {
    const filter = raw[source];
    source += 1;
    const row = y * stride;
    const above = row - stride;
    for (let index = 0; index < stride; index += 1) {
      const value = raw[source + index];
      const left = index >= bytesPerPixel ? out[row + index - bytesPerPixel] : 0;
      const up = y > 0 ? out[above + index] : 0;
      const upLeft =
        y > 0 && index >= bytesPerPixel ? out[above + index - bytesPerPixel] : 0;
      let restored;
      switch (filter) {
        case 0:
          restored = value;
          break;
        case 1:
          restored = value + left;
          break;
        case 2:
          restored = value + up;
          break;
        case 3:
          restored = value + ((left + up) >> 1);
          break;
        case 4: {
          const estimate = left + up - upLeft;
          const dLeft = Math.abs(estimate - left);
          const dUp = Math.abs(estimate - up);
          const dUpLeft = Math.abs(estimate - upLeft);
          restored =
            value +
            (dLeft <= dUp && dLeft <= dUpLeft ? left : dUp <= dUpLeft ? up : upLeft);
          break;
        }
        default:
          throw new Error(`unsupported PNG row filter ${filter}`);
      }
      out[row + index] = restored & 0xff;
    }
    source += stride;
  }
  return out;
};

export const decodePNG = (buffer) => {
  if (!buffer.subarray(0, 8).equals(SIGNATURE)) throw new Error("not a PNG");
  let offset = 8;
  let header = null;
  let palette = null;
  let alpha = null;
  const data = [];

  while (offset < buffer.length) {
    const length = buffer.readUInt32BE(offset);
    const type = buffer.toString("ascii", offset + 4, offset + 8);
    const body = buffer.subarray(offset + 8, offset + 8 + length);
    offset += 12 + length;

    if (type === "IHDR") {
      header = {
        width: body.readUInt32BE(0),
        height: body.readUInt32BE(4),
        depth: body[8],
        colorType: body[9],
        interlace: body[12],
      };
    } else if (type === "PLTE") palette = Buffer.from(body);
    else if (type === "tRNS") alpha = Buffer.from(body);
    else if (type === "IDAT") data.push(Buffer.from(body));
    else if (type === "IEND") break;
  }

  if (!header) throw new Error("PNG has no header");
  if (header.depth !== 8) throw new Error(`unsupported PNG bit depth ${header.depth}`);
  if (header.interlace !== 0) throw new Error("interlaced PNG is not supported");

  const { width, height, colorType } = header;
  const raw = zlib.inflateSync(Buffer.concat(data));

  // Everything is normalised to RGBA so the caller only deals with one layout.
  const rgba = Buffer.alloc(width * height * 4);
  if (colorType === 3) {
    if (!palette) throw new Error("indexed PNG has no palette");
    const indices = unfilter(raw, width, height, 1);
    for (let pixel = 0; pixel < width * height; pixel += 1) {
      const entry = indices[pixel];
      rgba[pixel * 4] = palette[entry * 3];
      rgba[pixel * 4 + 1] = palette[entry * 3 + 1];
      rgba[pixel * 4 + 2] = palette[entry * 3 + 2];
      rgba[pixel * 4 + 3] = alpha && entry < alpha.length ? alpha[entry] : 255;
    }
    return { width, height, rgba };
  }

  const channels = CHANNELS[colorType];
  if (!channels) throw new Error(`unsupported PNG colour type ${colorType}`);
  const pixels = unfilter(raw, width, height, channels);
  for (let pixel = 0; pixel < width * height; pixel += 1) {
    const source = pixel * channels;
    const target = pixel * 4;
    if (colorType === 0 || colorType === 4) {
      const grey = pixels[source];
      rgba[target] = grey;
      rgba[target + 1] = grey;
      rgba[target + 2] = grey;
      rgba[target + 3] = colorType === 4 ? pixels[source + 1] : 255;
    } else {
      rgba[target] = pixels[source];
      rgba[target + 1] = pixels[source + 1];
      rgba[target + 2] = pixels[source + 2];
      rgba[target + 3] = colorType === 6 ? pixels[source + 3] : 255;
    }
  }
  return { width, height, rgba };
};

export const encodePNG = ({ width, height, rgba }) => {
  const stride = width * 4;
  const raw = Buffer.alloc(height * (stride + 1));
  for (let y = 0; y < height; y += 1) {
    raw[y * (stride + 1)] = 0; // no filtering; these images are tiny
    rgba.copy(raw, y * (stride + 1) + 1, y * stride, (y + 1) * stride);
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = 6; // RGBA
  return Buffer.concat([
    SIGNATURE,
    chunk("IHDR", header),
    chunk("IDAT", zlib.deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
};

export const cropPNG = (image, x, y, width, height) => {
  const rgba = Buffer.alloc(width * height * 4);
  for (let row = 0; row < height; row += 1) {
    const source = ((y + row) * image.width + x) * 4;
    image.rgba.copy(rgba, row * width * 4, source, source + width * 4);
  }
  return { width, height, rgba };
};

// A slice that is entirely transparent is a gap in the sprite, not an icon.
export const isBlank = ({ rgba }) => {
  for (let index = 3; index < rgba.length; index += 4) {
    if (rgba[index] !== 0) return false;
  }
  return true;
};
