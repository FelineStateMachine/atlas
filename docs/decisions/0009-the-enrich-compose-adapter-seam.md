# 9. `generate ⊕ enrich` is joined by an adapter in the CLI

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** [enrich.md](../enrich.md) §1; issue #5 §3.2,
  §5.3

## Context

[ADR 3](0003-merge-moves-to-enrich.md) makes the composed multi-source result
`generate ⊕ enrich`, and issue #5 §3.2 forbids the two lanes to import each
other. So the ⊕ has to be performed somewhere, by something, in a vocabulary
that belongs to neither lane. Three shapes were possible.

- **Enrich composes.** Enrich would import `generate/compose`. Rejected — and
  the forbidding is not bureaucratic: it is what keeps either lane replaceable
  without the other.
- **Enrich emits doc-level additions the CLI feeds back.** Workable, but it
  would make every enricher learn the interchange document's schema.
- **A shared model in `format/`.** Rejected: a volume under enrichment is not a
  format concept. The format describes a written bundle; this model describes a
  build in progress, with a grid to measure distances in and a ledger that is
  not final yet.

## Decision

The ⊕ is performed by the one binary that holds both lanes. `cmd/atlas` owns a
mechanical adapter in each direction:

```
atlas enrich
  ├─ generate:  archive ──▶ doc.Document, one per source reading
  ├─ cmd/atlas: doc.Document ──▶ enrich.Volume            (adapt.go, a copy)
  ├─ enrich:    the curated queue runs, contributions are applied
  ├─ cmd/atlas: enrich.Volume ──▶ doc.Document + ledger   (adapt.go, a copy)
  └─ generate:  compose.Compose(document, ledger, revision) ──▶ a .atlas file
```

Enrich keeps its own model and its own vocabulary; an enricher never learns the
document schema. The **ledger does not travel through the document** — a
document is what one source has to say, a ledger is provenance about a
composition — so the accounts reach the payload through
`compose.Options.Ledger`, already serialized and spliced in verbatim.

## Consequences

- `TestAdaptationRoundTrips` holds the adapter to identity: a document adapted
  and adapted back is the same document, byte for byte. That test is also the
  mechanical form of the lane's fifth law — an empty queue cannot produce a
  different build, because it cannot produce a different document.
- Composition never learns what a matched pair or a held feature is, which is
  what lets the ledger vocabulary belong wholly to the enrich lane.
- The adapter is duplication, deliberately: two models that carry the same
  information because both are shaped by what the format needs. The cost is a
  copy; the purchase is two lanes that can be rewritten independently.
