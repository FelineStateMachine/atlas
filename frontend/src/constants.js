// Zones, grid cells, and categories without an archive color draw from this
// wheel. Mid-tone hues anchored on the identity's cerulean and earths: bright
// enough to read over game tiles under the white outset, muted enough not to
// shout over the neutral chrome.
export const palette = [
  "#4fb3d5", "#c9924b", "#82b56a", "#c96a6a", "#9581cc",
  "#4bc9a9", "#d4b04a", "#6a92c9", "#b08a5a", "#8fb3a2",
];

export const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz";
export const geohashMaxDepth = 3;
export const overzoomLevels = 2;

// A fragment rather than a path: the window has no address bar to make a path
// worth reading, the app can be mounted under a workspace prefix that a pushed
// path would navigate out of, and a fragment cannot 404. Slash-separated
// because both slugs contain dashes.
// The window is reopened far more often than it is refreshed, and reopening it
// to a default view discards work the reader did to reach where they were. The
// whole arrangement is kept: which map, which layer, where the view sits, what
// is filtered out and which groups are folded.
export const sessionKey = "atlas.session";

// The overview is drawn once per variant from the shallowest pyramid level big
// enough to read, then only the viewport rectangle moves.
export const overviewTargetSize = 168;

export const outsetColors = {
  light: "rgba(255, 255, 255, 0.96)",
  dark: "rgba(7, 9, 7, 0.98)",
};
