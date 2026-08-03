# The documentation

These documents describe the repository as it stands. History, where it is
needed, is a pointer rather than a chapter: the pre-rewrite tree is archived
on the `golden-reference` tag (mirrored at `archive/golden-reference`), the
calls the rewrite made are dated records under [`decisions/`](decisions/), and
the behavioral differences accepted against the archived reference are
[decision 18](decisions/0018-divergences-from-the-reference.md).

Read in this order to learn the system: [`format.md`](format.md) is the centre,
[`generate.md`](generate.md) and [`enrich.md`](enrich.md) are how a bundle
comes to be, [`app.md`](app.md) is what serves it,
[`render-seam.md`](render-seam.md) and [`analysis.md`](analysis.md) are what
pictures it, and [`decisions/`](decisions/) is why any of it is shaped this way.

## The documents

| Document | What it is |
|---|---|
| [`format.md`](format.md) | Normative `.atlas` v3 spec, sufficient alone to implement a reader: the container, the manifest, the world payload, the `ATLASLOC` bytes, stamps and ordering, validation, the registry directory, and a checklist for a new reader. |
| [`semconv/REGISTRY.md`](semconv/REGISTRY.md) | The attribute vocabulary, generated from `spec/registry.yaml`; `spec/gen`'s own test holds the committed copy up to date. |
| [`generate.md`](generate.md) | The generate lane: the interchange document, the sources and their gates, the capture archive, tile derivation, composition, curation. §2.7 is the "add a source" walkthrough. |
| [`enrich.md`](enrich.md) | The enrich lane: the enricher interface, the contribution format, the ledger vocabulary, the curated queue, the maturity score. |
| [`analysis.md`](analysis.md) | The cell-system contract: the eighteen methods, coordinates and continuity, the `Ground` descriptor, the plan order, the style tokens. §9 is the "add a third system" checklist. |
| [`app.md`](app.md) | The hypermedia application: routes, the session record, regions and partials, the state island, the hostenv contract and the three-host story. |
| [`render-seam.md`](render-seam.md) | The brief for rewriting `render/` blind: the scene description, the standing set, the chart, the globe, the diagnostics duty. §10.1 names what the document does not carry. |
| [`workbench.md`](workbench.md) | The operator's surface: its pages, its operations, the operation runner as a consumable library, and the safety properties. |
| [`snapshots.md`](snapshots.md) | The reusable scheduled-build workflow a city runs over its own data, and the one-time setup it needs. |
| [`testing.md`](testing.md) | The whole test surface: `make test`, `make test-e2e`, `make corpus-smoke`, what tests are made of, where they live, and the bar for a new one. |
| [`stamps.md`](stamps.md) | What a build stamp is a promise of, why it cannot be recomputed from a bundle, and what is enforced instead. |
| [`logging.md`](logging.md) | The one event stream: levels, the shared attribute keys, handler and flag conventions for the CLI and the browser. |
| [`decisions/`](decisions/) | Dated records of the calls that shaped all of the above, each with its context and consequences. |
