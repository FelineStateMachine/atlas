# The included Earth volume

The one `.atlas` file in this directory is the included Earth volume: a real,
ordinary format-v3 bundle the desktop shell embeds (`//go:embed included/*.atlas`
in `main.go`) and installs into the reader's library at startup, through the
same `format/bundle` path an imported bundle takes. A first launch therefore
opens onto a world instead of an empty library, and everything after
installation — opening, rendering, analysis, export, registry ordering — reads
the installed file, never the embedded copy. `atlas serve` and the CLI embed
nothing: they use exactly the registry the operator gives them.

The volume is raster-only: one world (`earth`), one lens (`Blue Marble`), no
collections, no features, and no runtime network dependency of any kind.

## Provenance

| Fact | Value |
|---|---|
| Product | Blue Marble: Next Generation, July 2004 — NASA Earth Observatory's global, cloud-free, true-color base map |
| Product page | <https://science.nasa.gov/earth/earth-observatory/blue-marble-next-generation/base-map/> |
| Source image | <https://assets.science.nasa.gov/content/dam/science/esd/eo/images/bmng/bmng-base/july/world.200407.3x21600x10800.jpg> |
| Source dimensions | 21600 × 10800 |
| Source SHA-256 | `dea8b4dc8a4f93f5f8bce0c8c85a508a178e7901e9ed8e6bf86e6ce7ef6d61e2` |
| First captured | `2026-08-03T14:30:39Z` |
| Credit | NASA Earth Observatory |

NASA imagery is free of copyright. NASA's media guidelines ask that the credit
be preserved, that no NASA logo be used, and that no NASA endorsement be
implied; the bundle's provenance ledger carries the credit, and nothing here
does the other two. No source URL appears inside the bundle — format v3's
runtime-URL prohibition holds for this volume exactly as for any other.

## Derivation

The pinned source is downsampled to the 8192 × 4096 reference level — the top
half of the world square, the shared whole-sphere equirectangular window
(`atlas.geometry.equirect.px = 0,0,8192,4096`,
`atlas.geometry.equirect.deg = -180,90,180,-90`) — through the deterministic
fixed-point Catmull-Rom resampler in `internal/generate/crawl/resample.go`
(`catmull-rom-fixed15`), cut into 256-pixel tiles, and encoded as JPEG at
quality **90** (`internal/generate/crawl/bluemarble.go` is the one place that
setting is spelled). The ordinary tile lane derives every shallower level,
coverage, bounds and stamps from that reference level, and the ordinary compose
lane writes and validates the bundle. The full extent survives uncropped, and
tiles are stored, not deflated, as format v3 requires.

## Regeneration

```sh
make included-earth
```

The first step (`atlas crawl -source nasa-blue-marble earth`) is the only
networked part: it downloads the source image once into the staged archive at
`crawl/blue-marble/fmg-archive`, refuses any digest but the pinned one, and
fetches nothing at all when the source is already there. The remaining steps
derive and compose offline. The same pinned source, capture metadata,
generation policy and tool version produce this file byte for byte, under this
same name — a rebuild of an unchanged archive writes nothing.

Ordinary builds, tests, application startup and first launch never contact
NASA: a release checkout builds from this committed artifact completely
offline.
