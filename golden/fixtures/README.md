# The golden fixtures

These files are the current implementation's observable behavior, extracted.
They are the reference the clean-room rewrite is measured against, and they
are read-only in the direction that matters: **a golden is never edited to
match a candidate.** An accepted difference goes in `golden/waivers.json`
with a written reason; a corrected golden goes through a re-capture from the
reference tree, and says so in its commit.

## Provenance

Everything here was captured from the tree tagged **`golden-reference`**,
commit **`b8d28888`** on `main` of `github.com/FelineStateMachine/atlas`,
using the programs in `golden/capture/` — which are part of that same tree
and read `.atlas` bundles through `internal/bundle`, so a fixture is what the
application itself would see, not what a separate reader made of the file.

The inputs are two directories outside the repository:

- the installed `.atlas` library, `~/Library/Application
  Support/dev.felinestatemachine.atlas/bundles` — read only, never written;
- the crawl archive, `../gamemap/fmg-archive`, in the layout
  `tools/crawl/archive.go` writes and `tools/generate` reads.

The library holds many builds of each volume. The fixture is the build that
would *serve* — the winner of `internal/bundle`'s newest-per-slug fold — and
that build's stamp is recorded in `FIXTURES.json`, so a later capture that
picks a different build announces itself rather than sliding past.

One volume comes from neither input: **`bend-or`**, the city, was built for
this fixture rather than found installed. See *The city fixture* below.

## Re-running the capture

    golden/capture/capture.sh                 # writes golden/fixtures
    golden/capture/capture.sh /tmp/compare    # writes somewhere else, to diff

The script builds a working registry holding exactly the fixture set,
measures it, runs the headless application against it, records the data
plane, and stops the server. It writes nothing outside the fixtures
directory and its own temporary directory, redirects `HOME` for the headless
run so no other checkout's `inspector.url` is disturbed, and takes whatever
port is free so a running application is never in the way.

The city is not in the library, so the script looks for it where it was
built — `dist/bundles` by default, `ATLAS_GOLDEN_CITY_DIR` otherwise — and
refuses to run without it rather than measure a set one volume short.
`ATLAS_GOLDEN_CITY_ARCHIVE` points the translator capture at the city's own
crawl archive; without it the `arcgis-hub` fixture is left as committed.

Individual captures, if one of them is what changed:

    go run ./golden/capture/survey                        # classify the library
    go run ./golden/capture/survey -paths tunic           # which file serves a slug
    go run ./golden/capture/bundles -out golden/fixtures tunic
    go run ./golden/capture/translators -out golden/fixtures
    go run ./golden/capture/measure -bundles <dir> -out golden/fixtures
    go run ./golden/capture/http -base http://127.0.0.1:PORT -out golden/fixtures

## What is here, and what each fixture pins

### `FIXTURES.json`

The fixture set itself: which volume stands for which shape, how that
classification was verified, and each volume's serving stamp, file hash and
counts. Read this first.

### `bundles/<slug>/`

One directory per fixture volume.

| file | pins |
| --- | --- |
| `volume.json` | the build's identity, its trait classification, the counts the manifest promises, a SHA-256 per archive part, and a hash over the archive's entry order |
| `manifest.json` | `atlas.json`, canonicalized |
| `worlds/<world>.payload.json` | `worlds/<world>.json`, canonicalized: lenses, the ordered collections array, attrs, merge ledger |
| `worlds/<world>.text.json` | `worlds/<world>.text`, canonicalized: the deferred prose, links and per-feature attrs |
| `worlds/<world>.locations.json` | `worlds/<world>.bin` — the `ATLASLOC` v3 packed payload — unpacked to one row per point, in packed order, `owner` still indexing the collections array |
| `icons.json` | every icon by name, weight and content hash, plus a rollup over all of them |
| `tiles/<pyramid>.json` | every tile of one pyramid: name, weight, CRC-32, content SHA-256, decoded width and height, and a digest of the decoded pixels — plus one content rollup and one pixel rollup for the pyramid |

The **pixel digest** is the load-bearing part of a tile inventory. It is
SHA-256 over `"<width>x<height>\n"` and the decoded image flattened to
non-alpha-premultiplied RGBA. A tile re-encoded by a different library, or at
a different compression level, has different bytes and the same picture, and
only this digest can tell that apart from a tile whose picture actually
moved. Rasters themselves are never committed.

### `translators/`

One archived capture per source, handed through the same chain of
`MaybeTranslate` calls `tools/generate` uses, in the same order, and the
resulting interchange document canonicalized. `<source>.fixture.json` names
the capture by content hash, records whether the translator passed it through
untouched, and counts the document; `<source>.doc.json` is the document.

The archived captures themselves are not committed — they are named by hash,
which is enough to prove two runs read the same input.

These pin *behavior*, not shape. The rewrite's interchange document may be
spelled differently and equivalence is judged at the composed bundle; what
these files catch is a source's semantics quietly moving — an id space that
stops being stable, a projection that shifts, a category that stops being
declared.

**`arcgis-hub` is captured from its own archive.** `arcgismap.Translate`
refuses a city it is not curated for, and the archive beside the library
holds only the withheld city, so this fixture is translated from the
`bend-or` archive the city volume was built from — the same program, pointed
with `-archive` at that archive. See *The city fixture* below.

### `measure/`

`maturity.txt` is the report `tools/maturity` prints over a registry holding
exactly the committed fixture set — verbatim, defects included.
`<slug>.build.json` is the same measurement structured, with the figures the
reports derive. `NOTES.md` annotates what is wrong with the output; nothing
in the output itself is corrected.

### `http/`

`transcript.json` is every recorded request and its answer: status, response
headers, body length and body hash, with small text bodies quoted. It covers
the catalog, a payload, a packed payload, a text payload, an icon and a tile
for every fixture volume, a range request, eight refusals, the application
shell and the two shell assets the headless host answers for — 43 exchanges.
`catalog.json` is the catalog body as served.

The sampling is derived from what is served rather than from a list kept in
step by hand, so the city joining the set moved it: the volumes are walked in
the catalog's order, and `bend-or` sorts first, which is why the range request
and the eight refusals are now spelled against the city's own world and its
`.png` tiles. That is a re-capture from the reference tree, not an edit.

Two values are machine-specific and are replaced: the `Date` header, and the
`bundlesDir` the catalog reports. Nothing else is normalized.

Worth knowing before reading it: **the data plane does not serve ranges.**
Tiles are stored uncompressed so that it could, and §2 of the rewrite issue
describes them as served by byte range, but the router sets a length and
copies the entry — a `Range` request is answered `200` with the whole body
and no `Accept-Ranges`. The transcript records that, so the rewrite either
keeps the behavior or changes it on purpose.

## The city fixture

The fixture set calls for a city — a basemap lens, curated municipal layers,
national hydrography with HUC12 membership — and for a while it had none it
could commit. The only city installed on the capturing machine is registered
from `internal/arcgismap/cities_local.go`, which git ignores deliberately,
because it is a personal location; its fixtures go to
`golden/fixtures/private/`, which git also ignores, and its name appears in
no committed file.

The city slot is now filled publicly. `bend-or` — the City of Bend, Oregon —
is one of the two proof cities curated in `internal/arcgismap`'s own table,
open data published by the city itself, and nothing about it needs
withholding. No bundle for it existed, so one was built by the reference
tree's pipeline, unchanged since the `golden-reference` tag, into a directory
outside the library:

    go run ./tools/crawl -arcgis bend-or -archive <archive>
    go run ./tools/tiles -source <archive> -output <tiles>
    go run ./tools/generate -source <archive-parent> \
        -tiles <tiles>/index.json -bundles <dir>

    go run ./golden/capture/bundles -dir <dir> -out golden/fixtures bend-or
    go run ./golden/capture/translators -archive <archive> -out golden/fixtures
    go run ./tools/maturity -bundles <six-volume-registry> \
        >golden/fixtures/measure/maturity.txt
    go run ./golden/capture/measure -bundles <six-volume-registry> \
        -out golden/fixtures

The crawl is the only stage that touches the network: the city's hub
(`data.bendoregon.gov`, through the ArcGIS Hub download API with
`spatialRefId=4326`) and the two USGS services on `hydro.nationalmap.gov`
that every curated city is enriched from by bounding box. The basemap is not
fetched from anywhere — it is rendered from the city's own vector data by
`internal/basemap`, so the pyramid is reproducible offline from the archived
capture alone.

**The capture day is part of the identity.** A city versions by crawl day:
the day is the world's slug, it is in the basemap's tile-set path, and it
sets the bundle's version. This fixture is the day `2026-08-02`. Re-crawling
on another day builds another world, not this one, and the hub's data will
have moved besides — so a re-capture of this fixture means re-reading the
same archived capture, not re-crawling.

What the city fixture pins that nothing else in the set does: 104 paths and
44 areas with inline geometry, 88 shape features carrying
`atlas.hydro.huc12` from the subwatershed membership join, four national
layers declaring `atlas.label.policy=quiet`, a standard icon resolved by
convention (`atlas.icon.std=maki/monument` → `icons/std--maki-monument.svg`),
and the `DescribedPct` defect, which reads **235%** here — 153 described
entries over 65 pins, because described shapes defer their prose into the
same `.text` payload the pins do.

Two things it does *not* pin, so nobody goes looking for them:

- the packed `member` column is zero for every row. `member` is the id of
  the *area feature containing a point*, and it is filled from a
  translator's `region_id`; the ArcGIS translator assigns none, so its pins
  are unowned. HUC12 membership is not carried there — it rides on the
  shape features as an attribute and a sentence in the `.text` payload.
- `http/transcript.json` predates this fixture and does not sample it. See
  `FIXTURES.json`'s `http.cityGap` for what re-recording would cost.
