# 10. An enriched build wins the fold by packing two policy revisions

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** [enrich.md](../enrich.md) §2; issue #5 §5.3,
  §10 decision 16; `internal/enrich/revision.go`

## Context

An enriched build must be the one a library serves, deterministically, without
mutating anything. The registry's ordering is creation time, then policy
revision, then stamp, then locator. Creation time is the newest capture the
build was made from and **never the build clock** — a format invariant the
enrich lane does not get to touch. So an enrichment of the same captures ties
on creation time with the plain build beside it, and the revision has to
decide.

## Decision

The revision is one integer carrying two policy numbers:

```
revision = enrichPolicy × RevisionSpan + generatePolicy      RevisionSpan = 100
```

A plain single-source build writes its generate policy revision alone — the
same number with an enrich policy of zero. `9` is generate policy 9 unenriched;
`109` is generate policy 9 enriched under enrich policy 1.

Three properties chose it:

- **Deterministic.** A pure function of two compiled-in constants. Nothing
  scans the library, nothing reads a clock. A mechanism that read the serving
  build's revision off disk and added one would make the stamp depend on which
  machine built it, breaking the format's first invariant.
- **Total.** Every enriched build of one capture outranks every plain build of
  it, with no tie for the stamp to break arbitrarily.
- **Honest about which axis wins.** Packing two axes into one integer makes one
  dominant; this picks enrich, because within one set of captures an enriched
  build is a *superset* of the plain one, and serving the plain build would
  lose data the library already holds.

`BuildRevision` refuses a generate revision that does not fit the span rather
than wrapping into the next enrich band.

## Consequences

- Widening the span is a deliberate restamp of every enriched build, not an
  accident.
- A generate policy change reaches an enriched volume when the pipeline is
  re-run — which is how a policy change reaches a merged volume anyway, since
  enrichment is a pipeline stage and not a separate ritual.
- The `enriched-build-revision` waiver follows directly: the reference tree
  merged inside composition and wrote the plain revision `9`, so an enriched
  build's manifest — and therefore its stamp and its file name — cannot match.
  It is a stamp difference by construction, and the canonical content stays
  unwaived. See [ADR 8](0008-stamp-identity-is-an-aspiration.md).
