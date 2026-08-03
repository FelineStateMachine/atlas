// The application, walked. These specs assert arrangement, not pixels: the
// URL a doorway lands on, the island a page carries, the controls a topbar
// offers, the session the next page remembers. The registry underneath is
// dist/e2e/bundles — the committed corpus, packed by tests/e2e/prep — so
// every expectation below is a fact about bytes in this repository.
import { expect, test, type Page } from "@playwright/test";

// The corpus contract, spelled out rather than discovered, so that a volume
// quietly failing to serve is a red test and not a shorter loop.
const MARS = { slug: "mars", world: "global", title: "Mars", lenses: 2 };
const BEND = { slug: "bend-or", world: "2026-08-02", title: "Bend, Oregon", lenses: 1 };

// island reads the page's own account of the arrangement: the inert JSON
// script node every explorer page carries.
async function island(page: Page) {
  const text = await page.locator("#atlas-session-island").textContent();
  expect(text, "the page carries no session island").toBeTruthy();
  return JSON.parse(text!) as {
    last: string;
    entry: null | { volume: string; world: string; lens: number };
  };
}

test.describe("the doorways", () => {
  test("the root is a doorway to a real world URL", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/v\/[a-z0-9-]+\/[a-z0-9-]+$/);
  });

  test("open names a volume and lands on its world", async ({ page }) => {
    await page.goto(`/open?volume=${MARS.slug}&world=${MARS.world}`);
    await expect(page).toHaveURL(`/v/${MARS.slug}/${MARS.world}`);
  });

  test("the catalog serves every corpus volume", async ({ request }) => {
    const response = await request.get("/data/catalog.json");
    expect(response.ok()).toBeTruthy();
    const catalog = (await response.json()) as {
      volumes: { slug: string; stamp: string; worlds: unknown[] }[];
    };
    const slugs = catalog.volumes.map((v) => v.slug).sort();
    expect(slugs).toEqual([BEND.slug, MARS.slug].sort());
    for (const volume of catalog.volumes) {
      expect(volume.stamp).toMatch(/^[0-9a-f]{64}$/);
      expect(volume.worlds.length).toBeGreaterThan(0);
    }
  });

  test("a volume nobody installed is not found", async ({ page }) => {
    const response = await page.goto("/v/nowhere/nothing");
    expect(response?.status()).toBe(404);
  });
});

test.describe("the explorer page", () => {
  for (const volume of [MARS, BEND]) {
    test(`${volume.slug} arrives arranged`, async ({ page }) => {
      await page.goto(`/v/${volume.slug}/${volume.world}`);

      // The page's own account agrees with its address.
      const state = await island(page);
      expect(state.entry?.volume).toBe(volume.slug);
      expect(state.entry?.world).toBe(volume.world);

      // The topbar's selects say where the reader is and what else there is.
      await expect(page.locator("#volume-select")).toHaveValue(volume.slug);
      await expect(page.locator("#world-select")).toHaveValue(volume.world);
      const lensField = page.locator("#lens-field");
      if (volume.lenses > 1) {
        await expect(lensField).toBeVisible();
        await expect(page.locator("#lens-select option")).toHaveCount(volume.lenses);
      } else {
        await expect(lensField).toBeHidden();
      }

      // The viewport is the seam's: the custom elements are there, and the
      // chart has booted far enough to put a canvas on the page. No tile
      // assertions — the corpus commits no rasters, and surviving that is
      // the render suite's own proof.
      await expect(page.locator("atlas-viewport")).toHaveCount(1);
      await expect(page.locator("atlas-chart")).toHaveCount(1);
      await expect(page.locator("atlas-globe")).toHaveCount(1);
      await expect(page.locator("atlas-chart canvas, #map canvas").first()).toBeAttached({
        timeout: 15_000,
      });

      // The keyboard contract rides every page.
      await expect(page.locator("#atlas-shortcuts")).toBeAttached();
    });
  }

  test("a lens is addressed by name and remembered", async ({ page }) => {
    await page.goto(`/v/${MARS.slug}/${MARS.world}`);
    const options = page.locator("#lens-select option");
    await expect(options).toHaveCount(MARS.lenses);
    const second = await options.nth(1).getAttribute("value");
    expect(second).toBeTruthy();

    await page.locator("#lens-select").selectOption(second!);
    // The session answer swaps the shell and republishes the island.
    await expect
      .poll(async () => (await island(page)).entry?.lens, {
        message: "the island never recorded the lens change",
      })
      .toBe(1);

    // A fresh visit to the same world resumes the same lens.
    await page.goto(`/v/${MARS.slug}/${MARS.world}`);
    expect((await island(page)).entry?.lens).toBe(1);
  });

  test("the last volume is where the root door leads", async ({ page }) => {
    await page.goto(`/v/${MARS.slug}/${MARS.world}`);
    await expect(page.locator("#atlas-shell")).toBeAttached();
    await page.goto("/");
    await expect(page).toHaveURL(`/v/${MARS.slug}/${MARS.world}`);
  });
});
