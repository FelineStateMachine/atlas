# The corpus

Real data, publicly redistributable, committed. Two volumes and one capture:

- `bundles/bend-or/` — a city volume built from open data: the district
  survey, the national hydrography enrichment, the offline-rasterized
  basemap. It is the deepest pipeline output in the tree — multipart zones,
  described shapes, a national-layer join — and it reproduces byte-for-byte
  from its archive, which is what lets tests hold real output to exact
  expectations.
- `bundles/mars/` — a NASA Trek volume: the sphere, three lenses, the WMTS
  quirks a planetary source brings. Public domain.
- `translators/nasa-trek.doc.json` — one real captured Trek document, so the
  translator's test parses what the source actually serves rather than only
  what we imagined it serves. `nasa-trek.fixture.json` is its summary.

Each bundle directory is a JSON extraction of one build: the manifest, each
world's payload, text and unpacked locations, and a per-pyramid tile
inventory carrying content and decoded-pixel digests. No rasters and no
archives are committed — which is exactly enough to hold a reader to the
format.

## What may live here

Public provenance is the bar. A fixture captured from a commercial game, an
installed library, or anything else that cannot be republished does not go
here — it goes nowhere. Tests that need shapes the corpus lacks build them:
synthetic bundles through the real writer (`format/bundle`'s fixture
builder), synthetic scenes through the render tests' model factories.

`.gitattributes` marks this whole tree `-text`: the files are compared as
bytes, and no checkout may rewrite them.
