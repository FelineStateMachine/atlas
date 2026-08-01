import { outsetColors, palette } from "./constants.js";
import { state } from "./state.js";

export function iconOutsetColor() {
  return outsetColors[state.map?.iconOutset === "dark" ? "dark" : "light"];
}

// A marker is drawn in its category's colour and edged with the map's outset,
// so a colour close to that outset leaves the marker with nothing to stand
// against. Rather than edge it the other way -- which would put a pale rim on
// a map that asked for dark ones -- the colour itself is taken to a lighter or
// darker variant of the same hue, so the legend still matches the map.
export function legibleIconColor(color) {
  const dark = state.map?.iconOutset === "dark";
  const luminance = relativeLuminance(color);
  if (dark ? luminance > 0.3 : luminance < 0.88) return color;
  return withLightness(color, dark ? 0.74 : 0.42);
}

export function withLightness(color, lightness) {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return color;
  const [r, g, b] = [0, 2, 4].map((at) => parseInt(hex.slice(at, at + 2), 16) / 255);
  const high = Math.max(r, g, b);
  const low = Math.min(r, g, b);
  const light = (high + low) / 2;
  let hue = 0;
  const chroma = high - low;
  const saturation = chroma === 0 ? 0 : chroma / (1 - Math.abs(2 * light - 1));
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
  const channel = (value) =>
    Math.round((value + m) * 255).toString(16).padStart(2, "0");
  return `#${channel(rr)}${channel(gg)}${channel(bb)}`;
}

export function relativeLuminance(color) {
  const hex = String(color).replace("#", "");
  if (hex.length !== 6) return 0.5;
  const channel = (offset) => parseInt(hex.slice(offset, offset + 2), 16) / 255;
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4);
}

export function colorFor(id) {
  const value = Math.abs(Number(id) || 0);
  return palette[(Math.imul(value, 2654435761) >>> 0) % palette.length];
}

export function hexToRGBA(value, alpha) {
  const hex = String(value || "#ffffff").replace("#", "");
  const expanded = hex.length === 3
    ? hex.split("").map((character) => character + character).join("")
    : hex.slice(0, 6).padEnd(6, "f");
  const number = Number.parseInt(expanded, 16);
  return `rgba(${(number >> 16) & 255}, ${(number >> 8) & 255}, ${number & 255}, ${alpha})`;
}

export function applyCategoryVisual(element, category) {
  element.style.setProperty("--pin-color", categoryColor(category));
}

// One colour for a category wherever it is drawn, so the legend and the map
// agree on what it looks like.
export function categoryColor(category) {
  return legibleIconColor(category.color || colorFor(category.id));
}

export function applyCategoryGlyph(element, category, fallback) {
  element.classList.remove("has-source-icon");
  element.style.removeProperty("--pin-icon");
  element.textContent = "";
  if (!category.iconAsset) {
    element.textContent = fallback;
    return;
  }
  element.style.setProperty("--pin-icon", `url("${iconURL(category.iconAsset)}")`);
  element.classList.add("has-source-icon");
}

export function iconURL(asset) {
  const path = asset.split("/").map((segment) => encodeURIComponent(segment)).join("/");
  return `${state.game.base}/icons/${path}`;
}

export function initials(value) {
  return value.split(/\s+/).slice(0, 2).map((part) => part[0] || "").join("");
}
