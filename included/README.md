# The included Earth volume

The one `.atlas` file in this directory is the included Earth volume: a real,
ordinary format-v3 bundle the desktop shell embeds (`//go:embed included/*.atlas`
in `main.go`) and installs into the reader's library at startup, through the
same `format/bundle` path an imported bundle takes. A first launch therefore
opens onto a world instead of an empty library, and everything after
installation — opening, rendering, analysis, export, registry ordering — reads
the installed file, never the embedded copy. `atlas serve` and the CLI embed
nothing: they use exactly the registry the operator gives them.

The volume is one world (`earth`) with one lens (`Blue Marble`) and the
lightest cut of ground truth: the primary capitals as a point collection per
continent (202 pins, each linked to the country it stands in), and the country
borders as one quiet area collection (177 areas) — enough to jump somewhere
real and see the legend, pins and ground working. No runtime network
dependency of any kind.

## Provenance

| Fact | Value |
|---|---|
| Product | Blue Marble: Next Generation w/ Topography and Bathymetry, July 2004 — NASA Earth Observatory's global, cloud-free, true-color composite with shaded relief and ocean-floor bathymetry |
| Product page | <https://science.nasa.gov/earth/earth-observatory/blue-marble-next-generation/base-topography-bathymetry/> |
| Source image | <https://assets.science.nasa.gov/content/dam/science/esd/eo/images/bmng/bmng-topography-bathymetry/july/world.topo.bathy.200407.3x21600x10800.jpg> |
| Source dimensions | 21600 × 10800 |
| Source SHA-256 | `d225f1f35a6448a4d1d8f6de6e48f3433e470085b70a35800e64f384f269a7b0` |
| First captured | `2026-08-03T15:27:26Z` |
| Credit | NASA Earth Observatory |
| Features | Natural Earth 1:110m cultural vectors, v5.1.2 — public domain |
| Borders file | <https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_110m_admin_0_countries.geojson> |
| Borders SHA-256 | `6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f` |
| Places file | <https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_110m_populated_places_simple.geojson> |
| Places SHA-256 | `0dbd25c9ad8bd797ddf164b067f563be5c16be2c002254eb594862377963f9dc` |

NASA imagery is free of copyright. NASA's media guidelines ask that the credit
be preserved, that no NASA logo be used, and that no NASA endorsement be
implied; the bundle's provenance ledger carries the credit, and nothing here
does the other two. Natural Earth is in the public domain; its boundaries and
its primary-capital designations are carried exactly as that project draws
them, de facto, with no editorial judgement added here. No source URL appears
inside the bundle — format v3's runtime-URL prohibition holds for this volume
exactly as for any other.

## Derivation

The pinned source is downsampled to the pyramid's deepest level — local zoom
6, a 16384 × 8192 picture the 21600-wide source still fills with real detail —
through the deterministic fixed-point Catmull-Rom resampler in
`internal/generate/crawl/resample.go` (`catmull-rom-fixed15`), cut into
256-pixel tiles, and encoded as JPEG at quality **85**
(`internal/generate/crawl/bluemarble.go` is the one place both settings are
spelled). The world's ground is unchanged by the depth: the raster fills the
top half of the world square, the shared whole-sphere equirectangular window
(`atlas.geometry.equirect.px = 0,0,8192,4096`,
`atlas.geometry.equirect.deg = -180,90,180,-90`), and zoom 6 is simply detail
past the world-pixel resolution, the way every deep pyramid in the corpus is.
The ordinary tile lane folds the reference level and everything shallower down
from the cut and derives coverage, bounds and stamps; the ordinary compose lane
writes and validates the bundle. The full extent survives uncropped, tiles are
stored, not deflated, as format v3 requires, and the finished file is ~21 MiB —
inside the 25 MiB budget the included volume keeps so the executable stays
portable.

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
