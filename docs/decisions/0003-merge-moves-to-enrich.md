# 3. Cross-source merge moves from generate to enrich

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §5.2, §5.3, §10 decision 4;
  [generate.md](../generate.md), [enrich.md](../enrich.md)

## Context

In the reference implementation, folding several sources' readings of one
volume together happens **inside composition**. That places the merge — with
its affine fits, its match radii, its per-key attribute policy and its ledger —
in the middle of the lane whose job is to turn one source's capture into one
volume. Composition therefore has to know what a donor is, and the generate
lane cannot be reasoned about, tested, or replaced without the merge coming
along.

## Decision

**Generate is single-source.** What one source said travels through untouched.
Folding readings together is an enricher — the first of the curated queue — and
the composed multi-source result is `generate ⊕ enrich`.

Correctness is defined at the composed-bundle level: the goldens check the
result of running both lanes, not the boundary between them.

## Consequences

- The two lanes never import each other (issue #5 §3.2, enforced by
  `golden/depcheck`), which is what forces the adapter seam of
  [ADR 9](0009-the-enrich-compose-adapter-seam.md).
- Only a merge ledger ever names a source, so the source-neutral rule
  ([ADR 4](0004-source-neutral-interchange-document.md)) has exactly one
  sanctioned exception and it is a provenance record.
- An enriched build must win the registry fold deterministically, which is
  [ADR 10](0010-revision-packing.md), which is in turn why every merged fixture
  carries the `enriched-build-revision` waiver: the reference tree wrote a
  plain policy revision, so the manifest — and therefore the stamp and the file
  name — cannot match by construction.
- Merge becomes replaceable and separately testable: it sees translated worlds
  and returns contributions, and everything it decided is in a ledger the gate
  audits.
