// The TypeScript half of the guardrails (issue #5 §9).
//
// Same boundaries as golden/depcheck, where they cross into TS: analysis/
// imports no DOM and nothing app-shaped; render/ imports only analysis/ and
// itself; fetch lives in render's data layer; console lives in the log module.
// Most of these rules exist to say "we don't do that here" — the message is
// the point, so every one names the contract and cites the issue section.
//
// This config is not wired into the existing frontend build. The lanes it
// polices (analysis/, render/) arrive in M6; until then it is the written form
// of their boundaries. To run it:
//
//   npm i -D eslint typescript-eslint
//   make lint-lanes
//
// M6 owns three follow-ups this skeleton deliberately leaves open: the import
// specifier for the analysis package (the allowlist below is where it gets
// declared), type-aware linting once tsconfigs exist, and the seam's authored
// line budget as a warning.

import tseslint from "typescript-eslint";

const cite = (violation, section, why) => `${violation} (issue #5 §${section}): ${why}`;

/** Browser globals the analysis lane must not reach for. */
const domGlobals = [
  "document",
  "window",
  "navigator",
  "customElements",
  "HTMLElement",
  "Element",
  "Node",
  "localStorage",
  "sessionStorage",
  "requestAnimationFrame",
  "getComputedStyle",
];

/** Everything that reaches the network. */
const networkGlobals = ["fetch", "XMLHttpRequest", "EventSource", "WebSocket"];

const noDom = domGlobals.map((name) => ({
  name,
  message: cite(
    `analysis/ must not touch the DOM (${name})`,
    "5.4",
    "an analysis is a pure transformation of its declared inputs — a Ground descriptor and a volume, never the page it happens to be running in; that is what lets a system be tested headlessly and reused by a renderer nobody has written yet",
  ),
}));

const noNetwork = (lane, why) =>
  networkGlobals.map((name) => ({
    name,
    message: cite(`${lane} must not reach the network (${name})`, "9", why),
  }));

const consoleRule = {
  selector: "MemberExpression[object.name='console']",
  message: cite(
    "bare console.* is not the log stream",
    "9",
    "the system narrates itself through one leveled, structured stream; the TS log module is the browser end of it, and the headless parity runner captures it — a failing tour step ships its console context for free only if everything goes through the module",
  ),
};

export default tseslint.config(
  {
    // The pre-rewrite frontend keeps its own build and its own rules; these
    // boundaries are for the clean-room lanes.
    ignores: ["frontend/**", "golden/fixtures/**", "**/node_modules/**", "**/dist/**", "**/build/**"],
  },

  {
    files: ["analysis/**/*.ts", "render/**/*.ts"],
    extends: [tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2023,
      sourceType: "module",
    },
    rules: {
      "@typescript-eslint/no-explicit-any": [
        "error",
        { fixToUnknown: false, ignoreRestArgs: false },
      ],
      "no-restricted-syntax": ["error", consoleRule],
    },
  },

  {
    files: ["analysis/**/*.ts"],
    rules: {
      "no-restricted-globals": ["error", ...noDom, ...noNetwork(
        "analysis/",
        "an analysis declares its inputs; fetching them is the consumer's adapter concern, not the lane's",
      )],
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["**/render/**", "render", "render/*", "ol", "ol/*", "globe.gl", "three", "three/*"],
              message: cite(
                "analysis/ must not import the renderer or its libraries",
                "3.2",
                "analysis depends only on its own math and the published attribute vocabulary; renderers adapt its tokens, and no renderer owns a cell rule",
              ),
            },
            {
              group: ["**/internal/**", "**/format/**", "**/golden/**", "htmx*", "**/app/**"],
              message: cite(
                "analysis/ must not import anything app-shaped",
                "3.2",
                "the lane is a TypeScript package a third party could use: app state, session shape and server contracts are not its inputs",
              ),
            },
          ],
        },
      ],
    },
  },

  {
    files: ["render/**/*.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              // Allowlist. M6 declares the analysis package's real specifier
              // here; the pinned dependency surface (OpenLayers, globe.gl /
              // three, s2js) is the rest of it, and it grows only behind a
              // green parity tour.
              group: [
                "*",
                "!./*",
                "!../*",
                "!analysis",
                "!analysis/*",
                "!ol",
                "!ol/*",
                "!globe.gl",
                "!three",
                "!three/*",
                "!s2js",
              ],
              message: cite(
                "render/ imports only analysis/, itself, and its pinned dependency surface",
                "5.5",
                "the seam stands on exactly three published documents — the /data plane, the scene description, the analysis API — which is what makes it rewritable from its contracts alone",
              ),
            },
            {
              group: ["**/internal/**", "**/format/**", "**/golden/**", "**/app/**", "htmx*"],
              message: cite(
                "render/ must not import the application",
                "5.5",
                "data flows one way — server to scene description to seam — and only two things flow back: the atlas:pick event and the debounced camera report",
              ),
            },
          ],
        },
      ],
    },
  },

  {
    // Fetch is a data-layer concern: the seam reads /data and tiles in one
    // place, so the offline invariant and the cache rules have one owner.
    files: ["render/**/*.ts"],
    ignores: ["render/data/**/*.ts"],
    rules: {
      "no-restricted-globals": ["error", ...noNetwork(
        "render/",
        "the seam's network access lives in its data layer, where the /data plane's URL shapes, the immutable cache rules and the zero-copy ATLASLOC unpack already live",
      )],
    },
  },

  {
    // The one module allowed to be the console.
    files: ["analysis/log.ts", "render/log.ts", "**/log/*.ts"],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
);
