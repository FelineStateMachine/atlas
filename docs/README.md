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
| `golden.md` — fixture provenance, gates, the waiver process, how to read a parity diff | [`../golden/HARNESS.md`](../golden/HARNESS.md) | **Complete, and placed differently.** It sits beside the fixtures it describes rather than under `docs/`, which close-out confirmed: a document about how to read a gate belongs with the gate. Fixture provenance, every gate, the escape hatch, the waiver process and the parity diff are all there — `parity-compare` walks all six volumes against their baselines, and §165 is how to read what it prints. |
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
2. **`golden.md`. Placement confirmed, section since delivered.**
   [`../golden/HARNESS.md`](../golden/HARNESS.md) stays beside the fixtures it
   describes; a document about how to read a gate belongs with the gate, and
   the table above is the permanent link this file owes it. The parity-diff
   section was owed only for as long as `parity-compare` had nothing to judge:
   the gate walks all six volumes from a fresh launch, and the harness's
   `parity-compare` section is what a diff means and how a waiver answers one.
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
4. **The old tree's prose. Answered, and the sweep has been run.** §8's comment
   policy says the reference implementation's comment trail stays readable on
   the `golden-reference` tag and that no new file references it as
   documentation. The rule this settles on is one line: **production files
   speak in domain terms; history lives in `decisions/` and on the tag.** The
   sweep took the archaeology out of the files that were only telling it —
   the workbench's operation runner and its policy, the headless host, the
   collator's two orders, the tile stamp, the merge ledger's frozen wording,
   the maturity axes, the cities table, the served stylesheet's own header,
   and the two lane configs — keeping in every case the contract the sentence
   was really stating. What was deliberately left is the other kind: a comment
   that names the old behaviour because the old behaviour is the *specification*
   ("byte-compatible with", "these are the key names", "the order of these
   questions is"), where deleting the name would leave a rule with no authority
   behind it. Provenance stayed where provenance belongs, too:
   `golden/fixtures/README.md`, `golden/analysis/README.md`,
   `golden/parity/*` and the ADRs still name reference-tree paths, because
   that is where a fixture's numbers came from and naming them is the whole
   point of the file.
5. **The parked scheduler. Parked is the right word.**
   [`snapshots.md`](snapshots.md) describes a workflow whose first step needs
   the clean-room ArcGIS/USGS crawler, and
   [`generate.md`](generate.md) §3.2 and §8.2 now agree that this crawler is
   the one outstanding piece of the generate lane — everything downstream of it
   ships, and the city it would feed is composed, drawn, enriched and measured
   from a staged archive today. A crawler that is the last item on a lane's
   list is parked, not abandoned, so the document keeps the word.

## Post-parity follow-ups

Named here so they are findable. The list was written when the tour compared
only counts, cameras and state; it has since been walked with a pointer, a
focus reading and a camera's screenshot, and most of what stood here was
answered by that. What answered it, and what did not, in that order:

- **Pixel and pick coverage exists.** The tour drives a real pointer at a real
  pixel, records where the focus lands, and photographs the pane against a
  committed picture — `golden/parity/SCHEMA.md` §2.1, §6.1, and the pictures in
  `golden/parity/screens/`. The three seam findings that rode on the old blind
  spot are all closed: the outset rim is drawn in the colour its token names
  (`outsetColor`, `render/chart/styles.ts`); the label ladder says what it does
  — `atlas.render.as: text` decides a collection's default label policy and
  its legend toggle, and a text collection draws an ordinary marker, which is
  what the reference did too (`frontend/src/styles.js` on the
  `golden-reference` tag draws a pin for every point collection); and
  `atlas:pick` is consumed by the shell's own hidden form, so a click on either
  canvas opens the card ([`render-seam.md` §5](render-seam.md#5-what-flows-back)).
- **One rendering difference is genuinely open**, and it is now the only one:
  the reference rasterised a marker once per collection into a 64 × 64 canvas
  — the glyph tinted, an outline in the outset colour smeared through a small
  disc to make a halo, the two composited — and drew that image at 31 px, or
  36 px when chosen. The seam draws a vector approximation instead: a filled
  circle with an outset-coloured rim and the collection's icon at 15 px over
  it. It reads correctly and it is not the reference's pixels. Nothing in the
  goldens holds it, because the pictures were captured from the seam.
- **CI does not walk the tour** (`golden/HARNESS.md` records the bargain): a
  runner would need `make static`, a browser and half an hour. That is a
  decision rather than an omission, and the tour runs locally against a green
  `make golden`.
- **The seam is 865 authored lines past its ~3,000 guideline**; `seam-lane`
  warns on every run, as designed (issue #5 §5.5). It was 358 when this line
  was first written; a globe, a grid, a pick path and a keyboard have arrived
  since, so the number is a standing question about the budget rather than a
  regression to fix — and the warning is what keeps the question asked.
