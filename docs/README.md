# The documentation

Issue #5 §8 names a documentation end-state and calls it an exit criterion per
milestone rather than residue. This file is the map from that list to what
exists, and — where a document is thinner than the standard it was written to —
the standing note for the close-out audit (§7, M7).

Read in this order to learn the system: [`format.md`](format.md) is the centre,
[`generate.md`](generate.md) and [`enrich.md`](enrich.md) are how a bundle
comes to be, [`app.md`](app.md) is what serves it,
[`render-seam.md`](render-seam.md) and [`analysis.md`](analysis.md) are what
pictures it, and [`decisions/`](decisions/) is why any of it is shaped this way.

## The end-state, and where it stands

| §8 asks for | Where it is | State |
|---|---|---|
| `format.md` — normative `.atlas` v3 spec, sufficient alone to implement a reader | [`format.md`](format.md) | Complete. Twelve sections through the container, the manifest, the world payload, the `ATLASLOC` bytes, stamps and ordering, validation, the registry directory and version history, ending in a checklist for a new reader. |
| `semconv/REGISTRY.md` — generated from `spec/registry.yaml` | [`semconv/REGISTRY.md`](semconv/REGISTRY.md) | Complete, and now literally generated. The agreement test it stood on became `spec/gen`'s up-to-date check; `make golden`'s `semconv-codegen` gate runs it. |
| `generate.md` — pipeline, interchange schema, archive layout, source-authoring guide, determinism contract, curation schemas | [`generate.md`](generate.md) | Complete. §2.7 is the "add a sixth source" walkthrough; §6 is the curation schemas; §8 says what the lane has proven. |
| `enrich.md` — enricher authoring guide, the evidence ethic, the versioned scoring table, ledger vocabulary | [`enrich.md`](enrich.md) | Complete. |
| `analysis.md` — the 18-method contract, coordinate and continuity rules, plan ordering guarantee, system author's checklist | [`analysis.md`](analysis.md) | Complete. §9 is the checklist, in the form of adding a third system. |
| `app.md` — route table, session schema, region/partial map, the state-placement rule, the hostenv contract and the three-host story | [`app.md`](app.md) | Complete. Two of the three hosts are built and §1.4 is the desktop shell's own section; the third, the WASM service worker, is deliberately unscheduled and §11 says so. |
| `render-seam.md` — the complete brief for rewriting `render/` blind | [`render-seam.md`](render-seam.md) | Complete, and self-auditing: §10 is the seam's own list of what is not proven yet, and §11 maps every named behaviour of issue #5 §5.5 to where it lives. |
| `golden.md` — fixture provenance, gates, the waiver process, how to read a parity diff | [`../golden/HARNESS.md`](../golden/HARNESS.md) | **Placed differently, and one section thin.** It sits beside the fixtures it describes rather than under `docs/`, which is a decision close-out should confirm or reverse. Fixture provenance, every gate, the escape hatch and the waiver process are all there. **How to read a parity diff is not** — `parity-compare` is the one gate whose lane does not exist, so there is no diff to describe yet. It lands with the tour. |
| `logging.md` — levels, the shared attribute keys, handler and flag conventions for CLI and browser | [`logging.md`](logging.md) | Complete. The browser half is documented here rather than in a document of its own, which is the right size for one module that shares this one's level names and vocabulary. |
| `decisions/` — short ADRs for the calls this issue makes | [`decisions/`](decisions/) | Complete: the eight calls §8 names, plus eight the execution made that §10 does not record. |

## Beyond the list

Two documents §8 does not name, because the things they describe were built
after it was written:

- [`workbench.md`](workbench.md) — the cartograph successor: its pages, its
  operations, the operation runner as a consumable library, and the carried
  safety properties.
- [`snapshots.md`](snapshots.md) — the reusable scheduled-build workflow a city
  runs over its own data, and the one-time setup it needs.

## What close-out should check

The audit issue #5 §7 puts in M7, stated as questions rather than a checklist,
so it can be answered rather than ticked:

1. **The walkthrough test.** §7's exit criterion is a newcomer building a
   bundle, enriching it, breaking the renderer, deleting it, and starting to
   rewrite — from these documents alone. Nobody has run it. It is the one
   audit item no amount of reading replaces.
2. **`golden.md`.** Move `golden/HARNESS.md` under `docs/`, or declare its
   placement deliberate and link it from here permanently. Either way, the
   parity-diff section is owed once `parity-compare` runs.
3. **The next wave's documents.** `app.md` §11 and `render-seam.md` §10 are
   both honest lists of what is not built. Each item that lands has to move
   from a "not yet" list into a normative section, and the milestone that lands
   it owns that edit. The Wails host did exactly that at archival; the pattern
   is the answer to this item, not a one-off.
4. **The old tree's prose. Answered at archival.** §8's comment policy says the
   reference implementation's comment trail stays readable on the
   `golden-reference` tag and that no new file references it as documentation.
   The tree left the branch for the tag at close-out, and the grep was run: what
   remains is provenance — `golden/fixtures/README.md` and
   `golden/analysis/README.md` name reference-tree paths because that is where
   a fixture's numbers came from, and each says in one line that those paths
   resolve on the tag. No file on this branch cites the old tree's comments as
   an explanation of anything on it.
5. **The parked scheduler.** `snapshots.md` describes a workflow whose first
   step needs a crawler the rewrite has not written. It is marked parked rather
   than deleted, because everything downstream of that step is shipped and
   proven. Close-out should decide whether the crawler is in scope or whether
   the document should say "not planned" instead of "parked".
