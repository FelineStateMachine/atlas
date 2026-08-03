// The e2e harness: the real application, in a real browser, over a registry
// packed minutes ago from the committed corpus.
//
// `make test-e2e` is the way in: it builds the seam, runs tests/e2e/prep to
// pack dist/e2e/bundles, and then hands over to Playwright, which starts
// `atlas serve` itself and waits for it to answer. Every prerequisite is
// built by the run — there is nothing optional, nothing machine-local, and
// therefore no "not judged" outcome. A missing seam or an empty registry is
// a red run.
//
// The corpus bundles carry payloads and no rasters (no tiles are committed),
// so the map's imagery answers 404 and the specs assert arrangement — pages,
// island, controls — rather than pixels. The renderer is built to survive
// missing tiles; that resilience has its own unit suite in render/test.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  // One worker: every spec walks the same host, and the host owns per-volume
  // session records that concurrent walks would fight over.
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:43117",
    trace: "retain-on-failure",
  },
  webServer: {
    command:
      "go run ./cmd/atlas serve -addr 127.0.0.1:43117 -bundles dist/e2e/bundles -static dist/static",
    cwd: "../..",
    url: "http://127.0.0.1:43117/",
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
