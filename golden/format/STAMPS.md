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
| `tunic` | `13d5657ed903` | 34 | 33 | 1 | ASPIRATION |
| `cyberpunk-2077` | `e191f1964b71` | 44 | 42 | 2 | ASPIRATION |
| `fallout-new-vegas` | `e6cd7eb1936e` | 93 | 88 | 5 | ASPIRATION |
| `zelda-tears-of-the-kingdom` | `9dc737d9871e` | 98 | 97 | 1 | ASPIRATION |
| `mars` | `68e141f26b1a` | 20 | 18 | 2 | ASPIRATION |

The city fixture is not listed: the committed slot is being re-captured from a
publicly curated city (`golden/fixtures/README.md`). It joins the table with
the same status when it lands.

## What would close it

Nothing in M1 can. Stamp identity becomes assertable when the generate lane
(M2) derives a pyramid from the capture archive and computes its derivation
stamp again: with those values in hand, the missing rows above are filled in
and the sum can be compared to the stamp the golden-reference tree wrote.

That check belongs to the `generate-enrich` gate, which reproduces whole
bundles and therefore reproduces their stamps or does not. When it lands,
each row here moves from ASPIRATION to HELD as its volume's stamp is
reproduced, and this file becomes the record of when each one was closed.
Until then, a fixture that reads ASPIRATION is a fixture whose stamp nobody
has reproduced — which is worth saying out loud, because it is the one
invariant of the format that this gate does not prove.
