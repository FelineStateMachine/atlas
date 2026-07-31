export const palette = [
  "#d6f36b", "#72d5f4", "#ff9e64", "#df83ff", "#62e6ae",
  "#ff6f91", "#f4d35e", "#8aa9ff", "#e7a56d", "#83d483",
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
