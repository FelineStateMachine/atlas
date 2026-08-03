# Atlas

Atlas is an offline map explorer built around a file format. A world — a game's
map, a city's open data, Mars — travels as one self-contained `.atlas` bundle:
its manifest, its features, its raster tile pyramids and its icons in a single
zip archive. Drop a bundle into the library and the volume appears; drop in a
newer build of the same volume and it takes over. No sidecars, no CDN, no
runtime network of any kind, ever.

Two things ship from this repository:

- **`atlas`** — the pipeline and the server. `crawl` captures what a publisher
  serves, `tiles` derives pyramids, `compose` writes bundles, `enrich` folds
  several readings of a volume together, `measure` scores them, `workbench`
  serves the operator's pages, `serve` serves the application headlessly, and
  `dev` is the loop for working on it.
- **Atlas** — the desktop application: the same application in a window.

```sh
go build ./cmd/atlas          # the CLI
make desktop                  # the desktop app (macOS; see .github/workflows/release.yml)
make serve-static             # the application over HTTP, with the seam mounted
```

The desktop application ships with one included volume — Earth, NASA's Blue
Marble base map as an ordinary `.atlas` bundle — installed into the library at
first launch, so a fresh install opens onto a world
([`included/README.md`](included/README.md) carries the provenance and the
regeneration recipe).

The library lives under the application's own data directory —
`~/Library/Application Support/dev.felinestatemachine.atlas/bundles` on macOS,
`%AppData%\dev.felinestatemachine.atlas\bundles` on Windows,
`~/.config/dev.felinestatemachine.atlas/bundles` on Linux. `ATLAS_BUNDLES_DIR`
points either the CLI or the desktop app somewhere else; `atlas compose
-bundles DIR` writes a registry elsewhere without touching the library at all.

## Released builds

Tagged releases carry the CLI for five targets and the desktop application for
Windows (x64), macOS (Apple Silicon) and Linux (x64). Two platform notes:

- **macOS**: the app is unsigned. After unzipping, clear the quarantine with
  `xattr -dr com.apple.quarantine Atlas.app`, or approve it under System
  Settings → Privacy & Security → "Open Anyway".
- **Linux**: the binary links GTK 3 and WebKitGTK 4.1 at runtime
  (`libgtk-3-0`, `libwebkit2gtk-4.1-0`) — present on Ubuntu 24.04+, Debian 13,
  and recent Fedora.

## The shape of it

```
format/          THE CENTRE. The .atlas container, manifest, ATLASLOC codec,
                 stamps, registry, validation, and the semantic conventions.
                 Pure Go, standard library only, importable by anyone.
internal/
  generate/      Capture and composition: crawl, five sources, tiles, compose.
  enrich/        Merge, national layers, standard icons, lenses, maturity.
  app/           The hypermedia application: one pure http.Handler, HTMX 4.
  workbench/     Scores, build diffs, source cards, pipeline operations.
analysis/        TypeScript: the cell systems (geohash, S2) behind one contract.
render/          TypeScript: the rendering seam. Deletable, and deleted in the
                 sense that matters — nothing imports it, and the application
                 builds, serves and works with its assets absent.
testdata/        The committed corpus: real extractions with public
                 provenance (see testdata/corpus/README.md).
tests/, tools/   The test trees that cannot live with their packages, and
                 the enforcement commands — depcheck, which enforces every
                 boundary named above, testgate and corpussmoke.
main.go          The desktop shell: ~300 lines of host wiring around the
                 handler, and the whole of what a window costs.
```

## Documentation

[`docs/`](docs/) is the system, written down. Read in this order:
[`format.md`](docs/format.md) is the centre;
[`generate.md`](docs/generate.md) and [`enrich.md`](docs/enrich.md) are how a
bundle comes to be; [`app.md`](docs/app.md) is what serves it;
[`render-seam.md`](docs/render-seam.md) and
[`analysis.md`](docs/analysis.md) are what pictures it;
[`workbench.md`](docs/workbench.md) is the operator's view;
[`logging.md`](docs/logging.md) is how everything narrates itself;
[`testing.md`](docs/testing.md) is how it is judged; and
[`decisions/`](docs/decisions/) is why any of it is shaped this way.
[`docs/README.md`](docs/README.md) is the map.

## Verification

```sh
npm ci
make test        # vet, every Go test skip-proof, both TypeScript lanes, depcheck
make test-e2e    # the application in a real browser, over the committed corpus
```

`make test` is the whole required surface, and it is what CI runs on Linux,
macOS and Windows (`.github/workflows/ci.yml`).
[`docs/testing.md`](docs/testing.md) is the map: the layers, what tests are
made of, where they live, and the bar for a new one.

## History

Atlas was rewritten from a clean room in 2026 ([issue
#5](https://github.com/FelineStateMachine/atlas/issues/5)). The implementation
it replaced is archived, checkout-able, whole and working, at the tag
**`golden-reference`** (mirrored at `archive/golden-reference`), and the
behavioral differences accepted against it are [decision
18](docs/decisions/0018-divergences-from-the-reference.md). Nothing on this
branch imports it — `tools/depcheck` refuses the edge — and no file here cites
its comments as documentation.
