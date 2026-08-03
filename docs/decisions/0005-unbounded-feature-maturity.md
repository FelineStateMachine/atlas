# 5. Maturity is per-feature, additive, unbounded and monotone-gated

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §5.3, §10 decision 5;
  [enrich.md](../enrich.md)

## Context

The reference implementation measures a bundle's maturity as percentages
against denominators it has to invent. Denominators produce two problems at
once: a ceiling — once a volume reads 100 % there is nothing left to earn, so
adding good data cannot be rewarded — and arithmetic that can be wrong, which
is exactly the known `DescribedPct` above 100 % defect.

## Decision

The score is **per feature, additive, unbounded, and monotone**. Each feature
earns points for a quality it verifiably has: a name; prose past a substance
threshold; resolved geo coordinates; a membership attribute; a resolved icon on
its collection; geometry substance, log-scaled; cross-source corroboration from
the merge ledger. Collections earn for declared conventions, worlds for
geometry declarations and lens depth, and scores sum upward to the volume.

**No denominators, no ceilings.** Percentages survive only as diagnostics.

Monotonicity is a build gate: an enrichment build whose score declines fails.
The gate compares under the same scoring-table version — a re-weighting is a
new version, not a mass failure — and a decrease that comes from removing data
that was *wrong* is ledgered and permitted.

## Consequences

- The `DescribedPct` defect is retired by construction rather than fixed.
- The five absolute measurement axes and the whole merge-ledger reporting carry
  over as the score's breakdown, so nothing that was measurable stops being
  measurable.
- The scoring table is versioned data. Changing a weight is a visible decision
  with a version number, not a silent re-baseline.
- The workbench headlines score deltas between builds: "what did this
  enrichment actually add" is a number the pipeline already computed.
- The gate rewards good data and never punishes corrections, which is the
  ethic it exists to encode.
