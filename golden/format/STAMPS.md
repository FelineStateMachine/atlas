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
| `cyberpunk-2077` | `e191f1964b71` | 44 | 42 | 2 | ASPIRATION — multi-source, M3 |
| `fallout-new-vegas` | `e6cd7eb1936e` | 93 | 88 | 5 | **HELD** (M2) |
| `zelda-tears-of-the-kingdom` | `9dc737d9871e` | 98 | 97 | 1 | **HELD** (M2) |
| `mars` | `68e141f26b1a` | 20 | 18 | 2 | **HELD** (M2) |
| `bend-or` | `3610a0f10798` | 6 | 5 | 1 | ASPIRATION — the capture is back, the city lane is not, see below |

## What M2 closed, and how far

Four fixtures now read **HELD**. `golden/pipeline` composes each of them from
the capture archive and the derived tile set and gets back the file the
reference implementation wrote: the same stamp, the same name, the same
SHA-256, the same byte count. That is stamp identity in the strongest sense the
aspiration asked for — the whole bundle, not a proxy for it.

| Fixture | file bytes | reproduction |
| --- | ---: | --- |
| `tunic` | 8,047,414 | byte-identical |
| `fallout-new-vegas` | 23,188,369 | byte-identical |
| `zelda-tears-of-the-kingdom` | 58,031,657 | byte-identical |
| `mars` | 255,455,078 | byte-identical |

Those four hold because the **derived tile set is an input**, the same way the
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
  the interpolation flag, the content bounds, and every captured tile of every
  level with its content hash and format, in the stamp's own sort order — is
  reproduced bit for bit. Feeding the reference implementation's tool hash into
  the clean room's stamp function returns the stamp the tile cache recorded, for
  **all nine pyramids of the four single-source fixtures**. Two plans that agree
  under one tool hash are the same plan.
- **The tiles are identical.** Tunic's pyramid is rebuilt from the frames the
  archive holds and compared against the reference cache: **741 tiles, byte for
  byte**, plus the register entry's zoom range, window, formats, content bounds,
  background colour and per-level coverage bitsets.

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

So the row is **ASPIRATION** rather than BLOCKED, and what it is now waiting on
is a lane rather than a lost input:

- **the archive is an input again.** Rebuilding from it reproduces the committed
  fixture exactly — `dist/bundles/bend-or-20260802-3610a0f10798.atlas`, sha256
  `7a4375d0…`, byte-identical across two runs of the reference pipeline, and the
  derived pyramid stamps the same value both times.
- **the clean room cannot compose it yet.** `internal/generate/sources` has no
  `arcgishub` translator, and the tile deriver renders no basemap, so
  `golden/pipeline` has no bend-or row to fail. The gate's `singleSource` table
  is the honest record of that: four fixtures, not five.

When the city lane lands, this row reads the same way the pyramids do — the
plan, the rasters and the canonical content reproduce, and the stamp does not,
because the stamp is downstream of the clock and of the deriving tool's own
source hash.
