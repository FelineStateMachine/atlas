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
| `generate.md` — pipeline, interchange schema, archive layout, source-authoring guide, determinism contract, curation schemas | [`generate.md`](generate.md) | Complete. §2.7 is the "add a sixth source" walkthrough; §6 is the curation schemas; §8 says what the lane has proven. The close-out audit corrected §3's archive layout, added §7.1 — where the staged archives and tile sets actually are, which was written down nowhere a reader looks — and retired §8.2's wait for a city that had already arrived. |
| `enrich.md` — enricher authoring guide, the evidence ethic, the versioned scoring table, ledger vocabulary | [`enrich.md`](enrich.md) | Complete. |
| `analysis.md` — the 18-method contract, coordinate and continuity rules, plan ordering guarantee, system author's checklist | [`analysis.md`](analysis.md) | Complete. §9 is the checklist, in the form of adding a third system. |
| `app.md` — route table, session schema, region/partial map, the state-placement rule, the hostenv contract and the three-host story | [`app.md`](app.md) | Complete. Two of the three hosts are built and §1.4 is the desktop shell's own section; the third, the WASM service worker, is deliberately unscheduled and §11 says so. The close-out audit moved three notes written before M6 out of the "not yet" voice: the seam landed, `-seam-watch` runs the bundler, and the grid cull is `internal/app/cells`. |
| `render-seam.md` — the complete brief for rewriting `render/` blind | [`render-seam.md`](render-seam.md) | **Complete for the contracts, and honest about the numbers.** §11 maps every named behaviour of issue #5 §5.5 to where it lives, §10 is what is not proven, and §10.1 — written by the close-out audit that actually attempted the blind read — is what a rewriter would still have to take out of `render/`: the drawn dimensions, the hit tolerance, what `members` counts, and the explicit resolution ladder. Two of its entries are contract rather than cosmetic and are the next thing this document owes. |
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

## What close-out checked

The audit issue #5 §7 puts in M7, stated as questions rather than a checklist,
so it can be answered rather than ticked. **The walkthrough was run.** What
follows is what it found; each item now carries an answer rather than an
instruction.

1. **The walkthrough test. Run, and it passes with a limp.** §7's exit
   criterion is a newcomer building a bundle, enriching it, breaking the
   renderer, deleting it, and starting to rewrite — from these documents alone.
   Every step of it worked from the documents: `tunic` composed from the staged
   captures and reproduced the fixture byte for byte, `enrich` said *nothing to
   add* over it and merged `cyberpunk-2077` to the ledger `enrich.md` §9
   predicts, `measure` scored to the point table in §6.1, `serve` came up with
   the seam mounted, `render/` deleted whole left `go build`, `go test` and
   `depcheck` green and every non-viewport interaction answering, the
   `registry.yaml` loop moved its three artifacts and refused an unknown
   checker, and the workbench streamed an operation and refused a foreign
   origin. The limp is that four of those steps needed a document fixed on the
   way past — where the archives are, what the archive layout is, what the
   `-seam-watch` flag does, which of two contradictory globe formulas is real
   — and those fixes are in this tree's history. The blind rewrite is the one
   step that does **not** pass: see item 3.
2. **`golden.md`. Placement confirmed, section still owed.**
   [`../golden/HARNESS.md`](../golden/HARNESS.md) stays beside the fixtures it
   describes; a document about how to read a gate belongs with the gate, and
   the table above is the permanent link this file owes it. The parity-diff
   section is still owed and still blocked on the same thing — `parity-compare`
   is the one gate that skips, and there is no diff to describe until it runs.
3. **The next wave's documents. The pattern held; one list grew.** `app.md`
   §11 lost the seam and the grid cull to §6.1 and §1.4 as they landed, which
   is the habit this item asked for. `render-seam.md` gained
   [§10.1](render-seam.md#101-what-this-document-does-not-give-you), which is
   the close-out's honest answer to the blind-rewrite standard: the document is
   sufficient to build a *correct* seam and not sufficient to build one whose
   diagnostics equal a baseline. Two of its entries — `atlas.icon.outset` and
   `atlas.render.as` — are semantic-conventions keys whose meaning is normative
   and whose current reading in the seam is not, and they are the next thing
   that document owes.
4. **The old tree's prose. Nearly answered.** §8's comment policy says the
   reference implementation's comment trail stays readable on the
   `golden-reference` tag and that no new file references it as documentation.
   The grep was re-run at close-out over the whole tree. Almost everything that
   remains is provenance in the places provenance belongs —
   `golden/fixtures/README.md`, `golden/analysis/README.md`,
   `golden/parity/*` and the ADRs, each naming reference-tree paths because
   that is where a fixture's numbers came from. **One file is not that.**
   `internal/workbench/oprunner`'s package comment cites `cmd/cartograph`'s own
   file names as the origin of the safety properties, which is "how we got
   here" prose in shipped source — §8 puts that in `decisions/` or nowhere, and
   [`workbench.md`](workbench.md) already carries the same account properly. A
   handful of one-line history notes sit in the same grey area
   (`cmd/atlas/serve.go`, `internal/workbench/workbench.go`, `package.json`,
   `eslint.config.mjs`, `.github/workflows/snapshot.yml`); each would be
   allowed verbatim in `docs/` or `golden/`, and the policy is scoped by where
   a file lives. `eslint.config.mjs`'s note is additionally stale: it says the
   lanes arrive in M6 and that the config is not wired, and both have since
   stopped being true.
5. **The parked scheduler. Parked is the right word.**
   [`snapshots.md`](snapshots.md) describes a workflow whose first step needs
   the clean-room ArcGIS/USGS crawler, and
   [`generate.md`](generate.md) §3.2 and §8.2 now agree that this crawler is
   the one outstanding piece of the generate lane — everything downstream of it
   ships, and the city it would feed is composed, drawn, enriched and measured
   from a staged archive today. A crawler that is the last item on a lane's
   list is parked, not abandoned, so the document keeps the word.
