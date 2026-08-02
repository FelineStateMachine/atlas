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

## Re-running the capture

    golden/capture/capture.sh                 # writes golden/fixtures
    golden/capture/capture.sh /tmp/compare    # writes somewhere else, to diff

The script builds a working registry holding exactly the fixture set,
measures it, runs the headless application against it, records the data
plane, and stops the server. It writes nothing outside the fixtures
directory and its own temporary directory, redirects `HOME` for the headless
run so no other checkout's `inspector.url` is disturbed, and takes whatever
port is free so a running application is never in the way.

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

**`arcgis-hub` is not captured here.** Its archived capture is present, but
`arcgismap.Translate` refuses a city it is not curated for, and the only
curated city on the capturing machine is the withheld one below. See
`FIXTURES.json`.

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
shell and the two shell assets the headless host answers for. `catalog.json`
is the catalog body as served.

Two values are machine-specific and are replaced: the `Date` header, and the
`bundlesDir` the catalog reports. Nothing else is normalized.

Worth knowing before reading it: **the data plane does not serve ranges.**
Tiles are stored uncompressed so that it could, and §2 of the rewrite issue
describes them as served by byte range, but the router sets a length and
copies the entry — a `Range` request is answered `200` with the whole body
and no `Accept-Ranges`. The transcript records that, so the rewrite either
keeps the behavior or changes it on purpose.

## The withheld city fixture

The fixture set calls for a city — basemap lens, curated municipal layers,
national hydrography with HUC12 membership. The only city curated on the
capturing machine is registered from `internal/arcgismap/cities_local.go`,
which git ignores deliberately, because it is a personal location. Its
fixtures are therefore captured into **`golden/fixtures/private/`**, which
git also ignores, and its name appears in no committed file.

What that costs, written down so it is not discovered later: no committed
fixture exercises path or area collections, inline geometry, label policy,
standard icons resolved through the conventions, HUC12 membership in the
packed `member` column, or the `DescribedPct` defect. When a bundle exists
for one of the publicly curated cities — `bend-or`, `redondo-beach-ca` — the
city fixture should be re-captured from it and committed, and this paragraph
deleted.
