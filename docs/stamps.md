# Stamps: what they promise, and what they cannot

Two different things could be asked of a build's stamp, and they are
different on purpose (issue #5 §6,
[decision 8](decisions/0008-stamp-identity-is-an-aspiration.md)):
**canonical-content equality is mandatory; stamp identity is an aspiration.**
This file is why the aspiration cannot be an assertion — what a stamp can
promise, what it cannot by construction, and what is enforced instead.

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

This is a property of format v3 as frozen, not a defect to fix: putting the
derivation stamps into the manifest would restamp every bundle in every
library.

## The second ceiling: two derivers never stamp alike

A derivation stamp covers **the deriving code's own source hash**
(`docs/generate.md` §4.2). That is deliberate and load-bearing: changing how a
level is reduced, or where the content bounds land, must invalidate every
pyramid, and a stamp that watched only the archive would quietly keep serving
the old derivation.

The consequence is exact and unavoidable: **two derivers that write
byte-identical tiles still stamp differently, because they are different
tools.** A library built end to end by two implementations carries the same
tiles and the same canonical content under different twelve-hex names.

Both ceilings resolve the same way: a stamp is a **rebuild-cost** promise —
"nothing that made this has moved" — and never a *content* promise.
Reproducibility is proven where it is observable, at the tiles and at the
composed bundle.

## What is enforced instead

The proxies below run on every `make test`, and each of them would fail for
any change that could move a stamp:

- **The stamp arithmetic itself.** `format/bundle/stamp_test.go` holds the
  part-hash format, the order independence, the short form, the capture-day
  rule and `VersionedFileName` to stated vectors.
- **Manifest byte-parity.** `TestCorpusManifestsReEncodeExactly`
  (`format/bundle/corpus_test.go`): each committed corpus manifest parses into
  `format/bundle` types and re-encodes to the committed bytes. The manifest's
  encoded bytes *are* a stamped part, so a schema that has moved by one byte
  is caught exactly where it would have changed the stamp.
- **Identity derivation, end to end.** `TestCorpusManifestsDeriveTheirIdentity`:
  the file name, short stamp and capture day each corpus build actually
  carries are re-derived from its manifest alone.
- **Every recoverable payload.** `TestCorpusLocationsRepackToTheRecordedPayload`
  packs each world's unpacked locations back into the recorded payload bytes,
  both directions; `TestCorpusInventoriesAgreeWithTheirHeaders` holds the
  per-pyramid tile inventories to what the manifests promise.
- **The registry fold.** `TestCorpusManifestsFoldAsALibrary`: the ordering a
  stamp participates in — capture time, policy revision, stamp, locator —
  folds the corpus builds the way a library would.

What the committed corpus cannot carry is a whole `.atlas` archive — rasters
and all, they are hundreds of megabytes — so the checks that need a real file
in hand (part hashes against the wire, entry order, tiles stored uncompressed)
live in `make corpus-smoke`, the maintainer's non-gating walk of an installed
library ([testing.md](testing.md)).

## Where the measurements live

The corpus extractions under `testdata/corpus/bundles/` record two real
builds — the proof city and the planet — file name, stamp, counts, payload
hashes and tile inventories, which is what the tests above measure against.
The pre-rewrite reproduction measurements, fixture by fixture, are archived
with the tree that took them on the `golden-reference` tag; the divergences
accepted from them are
[decision 18](decisions/0018-divergences-from-the-reference.md).
