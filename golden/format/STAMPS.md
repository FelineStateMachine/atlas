# Stamp identity: an aspiration, per fixture

Issue #5 §6 asks the format gate for two different things and is careful to
say they are different: **canonical-content equality is mandatory, stamp
identity is tracked as an aspiration per fixture.** This file is that
tracking, and the reason the second one is not an assertion.

## Why a stamp cannot be recomputed from a bundle

A build's stamp is an order-independent SHA-256 over named part hashes
(`docs/format.md` §8.1). Five kinds of part go into it:

| Part | Hash of | Recoverable from the bundle? |
| --- | --- | --- |
| `worlds/<slug>.json` | the encoded world payload | yes — read the entry, hash it |
| `worlds/<slug>.bin` | the packed locations | yes |
| `worlds/<slug>.text` | the encoded text payload | yes |
| `icons/<name>` | the icon's bytes | yes |
| `atlas.json` | the encoded manifest, with `version.stamp` and `version.createdAt` blank | yes — blank the two fields and re-encode |
| `tiles/<pyramid>` | **the pyramid's own derivation stamp** | **no** |

The last row is the whole difficulty. A pyramid's part hash is not a hash of
the tiles in the archive; it is the stamp the tile pipeline computed when it
derived the pyramid, which already names the source rasters and the tool that
cut them. That value is written nowhere in the `.atlas` file. A reader holding
a finished bundle has every other part in hand and cannot invert SHA-256 for
the rest, so the sum is out of reach by construction, not by omission.

This is a property of format v3 as frozen, not a defect to fix here: putting
the derivation stamps into the manifest would restamp every bundle in every
library, and format evolution waits until parity passes (§2, decision 7).

## What is enforced instead

The proxies below are asserted on every run of the gate, and each of them
would fail for any change that could move a stamp:

- **Manifest byte-parity.** In registry mode `MarshalManifest` must reproduce
  the archive's `atlas.json` byte for byte. The manifest's encoded bytes *are*
  a stamped part, so a schema that has moved by one byte is caught exactly
  where it would have changed the stamp.
- **Canonical-content equality.** Always-on, the committed manifest extraction
  parses into `format/bundle` types and re-encodes to the same canonical
  bytes.
- **Filename derivation.** `VersionedFileName` must produce the name the
  fixture build actually carries — the short stamp and the capture-day rule,
  end to end.
- **Every recoverable part hash.** In registry mode each world payload, packed
  payload and text payload must hash to the value `volume.json` recorded, and
  the icon and tile inventories must match name for name and hash for hash. If
  a stamp could be recomputed, these are the inputs it would be recomputed
  from, and they are all pinned.
- **The accounting itself.** `TestStampPartsAreReproducibleExceptThePyramids`
  builds the stamp out of every part it can and asserts that what is missing
  is exactly the pyramids. If a future format writes the derivation stamps
  into the archive, or a pyramid appears that nothing accounts for, that test
  says so — rather than the aspiration quietly staying impossible, or quietly
  becoming possible while nobody checks it.

## The tracking table

Counts are the stamp's parts: one `atlas.json`, three per world, one per icon,
one per pyramid. `go test -v ./golden/format/` prints the same figures against
a real library, so the table can be checked rather than believed.

| Fixture | stamp12 | Parts | Recomputable | Pyramid parts | Stamp identity |
| --- | --- | ---: | ---: | ---: | --- |
| `tunic` | `13d5657ed903` | 34 | 33 | 1 | **HELD** (M2) |
| `cyberpunk-2077` | `e191f1964b71` | 44 | 42 | 2 | WAIVED — enriched build, see below |
| `fallout-new-vegas` | `e6cd7eb1936e` | 93 | 88 | 5 | **HELD** (M2) |
| `zelda-tears-of-the-kingdom` | `9dc737d9871e` | 98 | 97 | 1 | **HELD** (M2) |
| `mars` | `68e141f26b1a` | 20 | 18 | 2 | **HELD** (M2) |
| `bend-or` | `3610a0f10798` | 6 | 5 | 1 | **HELD** (M2) — the first city, see below |

## What the merged volume closed, and what it cannot

`cyberpunk-2077` now rebuilds through `generate ⊕ enrich` — `atlas enrich` over
the two archived captures, into an empty registry — and everything a bundle *is*
comes back identical: the world payload with its whole merge ledger, the packed
locations, the deferred prose, all 38 icons, both pyramids' 17,507 tiles, and the
archive's entry order, byte for byte.

Measured against the reference build itself, that comes to: **17,549 archive
entries, in identical order, of which exactly one differs** — `atlas.json`, in
two fields of one object. Not the tile grid, not the volume, not a world's
counts or its capture time; `version.revision` and the `version.stamp` that
follows from it. The two files are the same length to the byte.

Its stamp cannot be identical, and the reason is a rule rather than a defect.
Issue #5 §5.3 requires an enrich write to bump the build revision past the
serving build's so the registry fold deterministically serves the enriched build;
`docs/enrich.md` §2 does that by packing the enrich policy into the high field,
so the reference's revision 9 becomes 109. The revision rides the manifest, the
manifest is a stamped part, and the stamp names the file. One rule, three
consequences, all of them downstream of a decision the issue makes on purpose.

That is the `enriched-build-revision` waiver. The gate does not shrug at it: it
asserts the *shape* of the difference — the capture time is unmoved, the revision
is exactly this lane's bump of the fixture's own, the stamp differs, the file
name follows — so a second, unrelated divergence could not hide inside the first.
Retiring the waiver would mean either giving up the fold-winning rule or
restamping every bundle in every library.

The volume's two pyramid parts are also unrecoverable for the reason every
pyramid part is, and one of them is the corpus's only **warped** pyramid: the IGN
raster resampled into the Piggyback world. Its plan half is proven the same way
every other plan half is, and proves more while doing it — the stamp covers the
alignment itself, six coefficients to nine decimal places, so reproducing it says
the anchors, the name matching, the trimming, the least-squares fit and the
target zoom all came out identical from the captures alone. Its 1,365 tiles
rebuild byte for byte.

## What M2 closed, and how far

Every single-source fixture now reads **HELD**. `golden/pipeline` composes each
of them from the capture archive and the derived tile set and gets back the file
the reference implementation wrote: the same stamp, the same name, the same
SHA-256, the same byte count. That is stamp identity in the strongest sense the
aspiration asked for — the whole bundle, not a proxy for it.

| Fixture | file bytes | reproduction |
| --- | ---: | --- |
| `tunic` | 8,047,414 | byte-identical |
| `fallout-new-vegas` | 23,188,369 | byte-identical |
| `zelda-tears-of-the-kingdom` | 58,031,657 | byte-identical |
| `mars` | 255,455,078 | byte-identical |
| `bend-or` | 13,642,843 | byte-identical |

Those five hold because the **derived tile set is an input**, the same way the
capture archive is: the pyramids composition reads carry the stamps the
reference tool wrote, and composition folds them in without reading a raster.
The question that leaves open is whether the clean-room deriver would have
produced those same pyramids and those same stamps, and that is the next
section.

## The one thing a clean-room deriver cannot reproduce

A derivation stamp covers **the deriving code's own source hash** (see
`docs/generate.md` §4.2, the stamp's field order). That is deliberate and
load-bearing: changing how a level is reduced, or where the content bounds
land, must invalidate every pyramid, and a stamp that watched only the archive
would quietly keep serving the old derivation.

The consequence is exact and unavoidable: **two derivers that write
byte-identical tiles still stamp differently, because they are different
tools.** Clean-room stamp identity for a pyramid is impossible by construction,
not by omission. Nothing short of shipping the reference implementation's
source verbatim could produce its tool hash, and shipping it verbatim is what a
clean room is not.

So the deriver is proven in two halves, in `golden/pipeline/derive_test.go`:

- **The plan is identical.** Everything the stamp covers except the tool hash —
  the frame, the deepest complete level, the deepest usable level, the encoding,
  the interpolation flag, the content bounds, the alignment where there is one,
  and every captured tile of every level with its content hash and format, in the
  stamp's own sort order — is reproduced bit for bit. Feeding the reference
  implementation's tool hash into the clean room's stamp function returns the
  stamp the tile cache recorded, for **all ten pyramids of the five
  single-source fixtures and for cyberpunk's warped variant**. Two plans that
  agree under one tool hash are the same plan.
- **The tiles are identical.** Three whole pyramids are rebuilt from what the
  archive holds and compared against the reference cache, one per path a level's
  pixels can take: tunic's **741 tiles** for the copied path, a captured level
  carried through and reduced; the city's **2,316** for the drawn one, where the
  deepest level is rasterized from the city's own vectors and every one of its
  4,096 tiles is held against the hash the capture witnessed; and cyberpunk's
  warped variant's **1,365** for the fitted one, rebuilt from the archive *and
  the fit*, including the level the resampler renders and every level folded
  down from it. All come back byte for byte, along with each register entry's
  zoom range, window, formats, content bounds, background colour and per-level
  coverage bitsets.

What that adds up to: a fresh archive derived by the clean-room deriver would
produce the same rasters and the same volumes, under different pyramid stamps
and therefore different bundle stamps. The bundles would be canonically
identical and byte-identical in everything except the twelve hex digits in their
names. That is the honest ceiling, and it is a property of the format's
incrementality mechanism rather than of this rewrite.

**The decision this implies** (for `docs/decisions/`): a derivation stamp is a
*rebuild-cost* promise — "nothing that made this has moved" — and never a
*content* promise. Cross-implementation reproducibility is proven at the tiles
and at the composed bundle, which is where it is observable.

## The city fixture

`bend-or` was **BLOCKED**, for a reason no gate could lift: its bundle was built
during M0 from a live crawl of Bend, Oregon's ArcGIS Hub, and that capture
archive was not kept. The committed
`golden/fixtures/translators/arcgis-hub.doc.json` was the reference tree's
*output* for a capture whose *input* no longer existed, and a fixture nothing
can be rebuilt from is a fixture nothing can be measured against.

The archive is back. The city was re-crawled on **the same day the first build
answered to**, 2026-08-02, from the same eleven datasets and the same three
endpoints, into the archive the pipeline keeps —
`games/arcgis-hub-bend-oregon-34950069941/maps/2026-08-02-35604576620`. The
repository stages it at `crawl/bend-or/fmg-archive`, a one-game archive of the
shape `tools/generate` reads, so the city can be rebuilt without walking a
library-sized capture.

What came back is worth stating precisely, because it is stronger than the
re-crawl had any right to be. **Every entry of the rebuilt bundle but one hashes
exactly as the lost build's did** — all 2,320 of them: the world payload, the
packed locations, the deferred prose, the icon, and all 2,316 tiles of the
basemap the renderer draws from the city's own vector data. The city's open data
had not moved between the two crawls, and the offline render is deterministic
over it. The one entry that differs is `atlas.json`, in exactly three fields:

| field | first build | rebuild |
| --- | --- | --- |
| `version.createdAt` | `2026-08-02T09:09:17.410769Z` | `2026-08-02T10:45:19.464241Z` |
| `worlds[0].updatedAt` | `2026-08-02T09:09:17.410769Z` | `2026-08-02T10:45:19.464241Z` |
| `version.stamp` | `f0feba1cd00c…` | `3610a0f10798…` |

That is the whole difference, and it is the one the old row predicted:
`capturedAt` is first-seen, a volume's `createdAt` is capture-derived by the
format's own invariant, the manifest's encoded bytes are a stamped part, and so
the stamp and the file name move after the clock. **The stamp is a rebuild-cost
promise, not a content promise** — the same decision the pyramids force, arrived
at from the other direction, and here it is measured rather than argued: two
builds of the same city, 13,642,843 bytes each, identical everywhere the city is
and different only where the clock is.

The row is now **HELD**, and it is the strongest row in the table, because the
city is the only fixture whose rasters the clean room *makes* rather than reads.

- **The archive is an input.** `golden/pipeline` reads it, translates it, derives
  the pyramid and composes the volume, and gets back the committed fixture
  exactly: `bend-or-20260802-3610a0f10798.atlas`, 13,642,843 bytes, sha256
  `7a4375d0…`, with `createdAt` and the revision reproduced along with the stamp.
- **The pixels are the clean room's own.** A city has no tile server, so its
  deepest level is drawn from the vectors the city publishes (`docs/generate.md`
  §4.4). The clean-room renderer draws all **4,096 tiles** of that level and
  every one of them hashes to what the reference implementation's renderer wrote
  into the archive — a byte-for-byte agreement between two independent
  rasterizers over the same geometry, checked by the deriver on every run rather
  than only in a suite. 2,316 survive the background omission into the pyramid,
  and all 2,316 match the reference cache.
- **The plan is identical too.** The city's pyramid stamps to the value the tile
  cache recorded, `624a4e0c…`, when the reference implementation's tool hash is
  substituted for the clean room's — the tenth of ten.

So the city closes both halves at once, which no other fixture does: the four
game and planet fixtures hold their stamps while standing on rasters somebody
else derived, and the city holds its stamp while standing on rasters it drew.

**What is still true is the ceiling.** This row holds because the derived tile
set is an input carrying the reference tool's stamps. A pyramid the clean-room
deriver stamps for itself stamps differently — its `ToolStamp` now covers the
renderer as well, which is exactly right and exactly why it cannot match — so a
library built end to end by the clean room would carry the same tiles and the
same canonical content under different twelve-hex names. That is the property
§"The one thing a clean-room deriver cannot reproduce" describes, unchanged by
this row: the stamp is a rebuild-cost promise, and cross-implementation
reproducibility is proven where it is observable, at the tiles and at the
composed bundle.

The old prediction that the city could never reproduce its `createdAt` was true
of the *lost* archive and is no longer true of this one. `capturedAt` is
first-seen and travels in the archive, so the re-crawl's clock is now a fact on
disk rather than a moment that has passed — which is what makes this the first
city to hold, and the whole reason the archive was worth crawling back.
