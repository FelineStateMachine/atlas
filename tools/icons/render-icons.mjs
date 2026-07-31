#!/usr/bin/env node
// Renders a game's category glyphs into the FMG archive as standalone SVGs.
//
// MapGenie ships its category icons as an icon font referenced from a
// stylesheet, so the glyph for "police_station" is a codepoint rather than a
// file. This resolves that indirection: the stylesheet gives key -> codepoint,
// the SVG font gives codepoint -> outline, and each pairing is written to
// <archive>/games/<game-dir>/icons/<key>.svg, which is where tools/generate
// looks for them.
//
// It runs once per game rather than per tile, which is why it stays a small
// script instead of moving into the Go crawler.
//
//   node tools/icons/render-icons.mjs --archive ../gamemap/fmg-archive --game skyrim
//
// Without --game, every game already present in the archive is refreshed.

import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

import { cropPNG, decodePNG, encodePNG, isBlank } from "./png.mjs";

const API = "https://mapgenie.io/api/v1";
const CDN = "https://cdn.mapgenie.io";
// The CDN answers 403 to requests that do not look like a browser.
// Used when a category carries no legend colour of its own; light enough to
// stay legible on the dark map.
const FALLBACK_COLOR = "#c8ccbd";
const USER_AGENT =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

const parseArguments = (argv) => {
  const options = { archive: "../gamemap/fmg-archive", game: null };
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--archive") options.archive = argv[++index];
    else if (flag === "--game") options.game = argv[++index];
    else if (flag === "--help" || flag === "-h") options.help = true;
    else throw new Error(`unknown argument ${flag}`);
  }
  return options;
};

let lastRequest = 0;
// A single game needs only a handful of requests, but they are still spaced
// out so a loop over every game stays a light load.
const fetchPolitely = async (url, { optional = false } = {}) => {
  const wait = Math.max(0, lastRequest + 150 - Date.now());
  if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
  lastRequest = Date.now();

  const response = await fetch(url, {
    headers: { "User-Agent": USER_AGENT, Accept: "*/*" },
  });
  if (!response.ok) {
    if (optional && (response.status === 403 || response.status === 404)) return null;
    throw new Error(`HTTP ${response.status} from ${url}`);
  }
  return response;
};

const fetchText = async (url, options) => {
  const response = await fetchPolitely(url, options);
  return response && response.text();
};

const fetchJSON = async (url, options) => {
  const response = await fetchPolitely(url, options);
  return response && response.json();
};

const fetchBuffer = async (url, options) => {
  const response = await fetchPolitely(url, options);
  return response && Buffer.from(await response.arrayBuffer());
};

const cssColor = (value) => {
  if (!value) return null;
  const rgb = /rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i.exec(value);
  if (rgb) {
    return `#${[1, 2, 3]
      .map((index) => Number(rgb[index]).toString(16).padStart(2, "0"))
      .join("")}`;
  }
  return /#[0-9a-f]{3,8}/i.exec(value)?.[0] ?? null;
};

// An icon is either a single glyph, `.icon-<key>:before`, or several stacked
// layers, `.icon-<key> .path1:before` .. `.pathN:before`, each with its own
// colour. Both forms are collected per key, in the order the layers paint.
const readIconLayers = (css) => {
  const icons = new Map();
  const layerAt = (key, order) => {
    if (!icons.has(key)) icons.set(key, []);
    const layers = icons.get(key);
    const existing = layers.find((layer) => layer.order === order);
    if (existing) return existing;
    const layer = { order };
    layers.push(layer);
    return layer;
  };

  const rule =
    /\.icon-([a-z0-9_-]+)(?:\s+\.path(\d+))?:before\s*\{([^}]*)\}/gi;
  for (const match of css.matchAll(rule)) {
    const [, key, pathNumber, body] = match;
    const codepoint = /content:\s*"\\([0-9a-fA-F]+)"/.exec(body)?.[1];
    if (!codepoint) continue;
    const layer = layerAt(key, pathNumber ? Number(pathNumber) : 0);
    layer.codepoint = codepoint.toLowerCase();
    layer.color = cssColor(/color:\s*([^;]+)/i.exec(body)?.[1]);
    const opacity = /opacity:\s*([0-9.]+)/i.exec(body)?.[1];
    if (opacity) layer.opacity = Number(opacity);
  }

  for (const layers of icons.values()) layers.sort((a, b) => a.order - b.order);
  return icons;
};

const readFontURL = (css) => {
  const match = css.match(/url\(['"]?([^'")]*\.svg[^'")]*)['"]?\)/i);
  if (!match) return null;
  return new URL(match[1].replace(/#.*$/, ""), `${CDN}/css/themes/icons/`).href;
};

// Some games publish one marker strip that CSS windows into, rather than a
// font: `[class^="icon-"] { background: url(sprite.png); background-size: 15px }`
// with `.icon-<key> { background-position: 0 -Npx }` naming each row.
const readSprite = (css) => {
  const source = /\[class\^?[*]?="icon-"\][^{]*\{([^}]*)\}/i.exec(css)?.[1];
  if (!source) return null;
  const url = /url\(['"]?([^'")]+\.png[^'")]*)['"]?\)/i.exec(source)?.[1];
  if (!url) return null;
  const size = Number(/background-size:\s*([0-9.]+)px/i.exec(source)?.[1] ?? 0);
  const cell = Number(/width:\s*([0-9.]+)px/i.exec(source)?.[1] ?? size);
  if (!cell) return null;

  const offsets = new Map();
  const rule = /\.icon-([a-z0-9_-]+)\s*\{[^}]*background-position:\s*[^;]*?(-?[0-9.]+)px\s*;?[^}]*\}/gi;
  for (const match of css.matchAll(rule)) {
    // A key repeated in the strip keeps its first row; later ones are spares.
    if (!offsets.has(match[1])) offsets.set(match[1], Math.abs(Number(match[2])));
  }
  if (offsets.size === 0) return null;
  return {
    url: new URL(url, `${CDN}/css/themes/icons/`).href,
    cell,
    displayWidth: size || cell,
    offsets,
  };
};

// An SVG font is XML, so the glyphs can be read directly rather than through a
// font parser: each carries its codepoint and its outline.
const readGlyphs = (svg) => {
  const glyphs = new Map();
  const element = /<glyph\b([^>]*)\/?>/gi;
  for (const match of svg.matchAll(element)) {
    const attributes = match[1];
    const unicode = /unicode="&#x([0-9a-fA-F]+);"/.exec(attributes);
    const outline = /\sd="([^"]*)"/.exec(attributes);
    if (!unicode || !outline || !outline[1].trim()) continue;
    glyphs.set(unicode[1].toLowerCase(), outline[1]);
  }
  const face = /<font-face\b([^>]*)/i.exec(svg)?.[1] ?? "";
  const number = (name, fallback) =>
    Number(new RegExp(`${name}="(-?[0-9.]+)"`).exec(face)?.[1] ?? fallback);
  return {
    glyphs,
    unitsPerEm: number("units-per-em", 1024),
    ascent: number("ascent", 960),
  };
};

// Font outlines are y-up from the baseline; SVG is y-down from the top corner,
// so the glyph is flipped about the ascent line to sit in a square viewBox.
//
// Layers paint in order. Atlas draws these as background images, where nothing
// outside the file can tint them and `currentColor` has no inherited value to
// resolve against, so a layer without its own colour needs the legend colour
// carried on the root element rather than left to the page.
const renderSVG = (key, layers, unitsPerEm, ascent, defaultColor) => {
  const paths = layers
    .map(({ outline, color, opacity }) => {
      const fill = color ? `fill="${color}"` : `fill="currentColor"`;
      const alpha = opacity === undefined ? "" : ` fill-opacity="${opacity}"`;
      return `    <path ${fill}${alpha} d="${outline}"/>`;
    })
    .join("\n");
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${unitsPerEm} ${unitsPerEm}" ` +
    `color="${defaultColor}" role="img">\n` +
    `  <title>${key}</title>\n` +
    `  <g transform="translate(0, ${ascent}) scale(1, -1)">\n` +
    `${paths}\n` +
    `  </g>\n` +
    `</svg>\n`
  );
};

const renderGame = async (archiveRoot, game, directory) => {
  const css = await fetchText(`${CDN}/css/themes/icons/${game.slug}-icons.css`, {
    optional: true,
  });
  if (!css) {
    console.log(`  ${game.slug}: no icon stylesheet published`);
    return 0;
  }
  const icons = readIconLayers(css);
  const fontURL = readFontURL(css);
  const sprite = fontURL ? null : readSprite(css);
  if ((!fontURL || icons.size === 0) && !sprite) {
    console.log(`  ${game.slug}: stylesheet carries no usable glyph mapping`);
    return 0;
  }
  const font = fontURL ? readGlyphs(await fetchText(fontURL)) : null;
  const strip = sprite ? decodePNG(await fetchBuffer(sprite.url)) : null;

  // Which glyphs matter, and what colour the legend gives them, comes from the
  // maps themselves.
  const usages = new Map();
  for (const reference of game.maps ?? []) {
    const full = await fetchJSON(`${API}/maps/${reference.id}/full`, { optional: true });
    if (!full) continue;
    for (const group of full.groups ?? []) {
      for (const category of group.categories ?? []) {
        if (!category.icon) continue;
        if (!usages.has(category.icon)) usages.set(category.icon, []);
        usages.get(category.icon).push({
          categoryId: category.id,
          categoryTitle: category.title,
          groupId: group.id,
          legendColor: (category.color ?? "").replace(/^#/, "") || undefined,
          mapId: full.id,
        });
      }
    }
  }

  const iconDirectory = path.join(archiveRoot, directory, "icons");
  await mkdir(iconDirectory, { recursive: true });

  const index = [];
  let missing = 0;
  for (const [key, uses] of [...usages].sort(([a], [b]) => a.localeCompare(b))) {
    const legend = uses.find((use) => use.legendColor)?.legendColor;
    const defaultColor = legend ? `#${legend.toLowerCase()}` : FALLBACK_COLOR;

    if (strip) {
      const offset = sprite.offsets.get(key);
      if (offset === undefined) {
        missing += 1;
        continue;
      }
      // Offsets are in displayed pixels; the strip may be published at a
      // multiple of that, so they are scaled to source pixels before cutting.
      const scale = strip.width / sprite.displayWidth;
      const cell = Math.round(sprite.cell * scale);
      const top = Math.round(offset * scale);
      if (top + cell > strip.height) {
        missing += 1;
        continue;
      }
      const slice = cropPNG(strip, 0, top, Math.min(cell, strip.width), cell);
      if (isBlank(slice)) {
        missing += 1;
        continue;
      }
      const png = encodePNG(slice);
      await writeFile(path.join(iconDirectory, `${key}.png`), png);
      index.push({
        contentHash: createHash("sha256").update(png).digest("hex"),
        defaultColor,
        file: `${key}.png`,
        key,
        spriteOffset: offset,
        sourceCssUrl: `${CDN}/css/themes/icons/${game.slug}-icons.css`,
        sourceSpriteUrl: sprite.url,
        usages: uses,
      });
      continue;
    }

    const drawn = (icons.get(key) ?? [])
      .map((layer) => ({ ...layer, outline: font.glyphs.get(layer.codepoint) }))
      .filter((layer) => layer.outline);
    if (drawn.length === 0) {
      missing += 1;
      continue;
    }
    const svg = renderSVG(key, drawn, font.unitsPerEm, font.ascent, defaultColor);
    await writeFile(path.join(iconDirectory, `${key}.svg`), svg);
    index.push({
      codepoint: drawn.map((layer) => layer.codepoint).join("+"),
      contentHash: createHash("sha256").update(svg).digest("hex"),
      defaultColor,
      file: `${key}.svg`,
      key,
      sourceCssUrl: `${CDN}/css/themes/icons/${game.slug}-icons.css`,
      sourceFontUrl: fontURL,
      usages: uses,
    });
  }
  await writeFile(
    path.join(iconDirectory, "index.json"),
    `${JSON.stringify(index, null, 2)}\n`
  );
  console.log(
    `  ${game.slug}: ${index.length} icons written` +
      (missing ? ` · ${missing} referenced but not in the font` : "")
  );
  return index.length;
};

const main = async () => {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) {
    console.log(
      "usage: render-icons.mjs [--archive PATH] [--game SLUG]\n" +
        "Renders category glyphs into <archive>/games/<game>/icons/."
    );
    return;
  }

  const archiveRoot = path.resolve(options.archive);
  const archiveFile = JSON.parse(
    await readFile(path.join(archiveRoot, "archive.json"), "utf8")
  );
  const directories = new Map(
    (archiveFile.games ?? []).map((entry) => [entry.id, entry.directory])
  );

  const games = await fetchJSON(`${API}/games`);
  const wanted = games.filter((game) =>
    options.game
      ? game.slug === options.game || String(game.id) === options.game
      : directories.has(game.id)
  );
  if (wanted.length === 0) {
    throw new Error(
      options.game
        ? `no game matches ${options.game}`
        : "archive.json lists no games"
    );
  }

  console.log(`Rendering icons for ${wanted.length} game(s)`);
  let total = 0;
  for (const game of wanted) {
    const directory = directories.get(game.id);
    if (!directory) {
      console.log(`  ${game.slug}: not in this archive, skipping`);
      continue;
    }
    total += await renderGame(archiveRoot, game, directory);
  }
  console.log(`${total} icons rendered`);
};

main().catch((error) => {
  console.error("render-icons:", error.message);
  process.exit(1);
});
