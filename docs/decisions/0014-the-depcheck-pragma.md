# 14. A boundary crossing is annotated in place, with a reason

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** [golden/HARNESS.md](../../golden/HARNESS.md);
  issue #5 §9, §10 decision 13; `golden/depcheck/report.go`

## Context

The project's architectural contracts are encoded as static analysis: the lane
import matrix, the stdlib-only rule for `format/`, hostenv purity, network
confinement, semconv discipline. Rules that cannot be escaped get weakened
instead — the rule is loosened until the one honest exception fits, and then it
no longer catches the dishonest ones. Rules that can be escaped silently are
worse.

## Decision

A boundary crossing that is genuinely correct is annotated **in place**:

```go
resp, err := http.Get(u) //depcheck:allow netconfine the crawl politeness probe runs before the archive exists
```

The pragma sits on the offending line or the line above it, names one rule (or
`all`), and **must carry a written reason**. A pragma naming an unknown rule,
or carrying no reason, is itself a finding — it would otherwise suppress
nothing while reading as though it did.

It is the source-level twin of a `golden/waivers.json` entry, and it is read
the same way: **as a cost**, visible where the crossing is rather than in a
config file nobody opens.

## Consequences

- Every rule keeps its strictness. The exception is local, named and explained,
  and it shows up in a diff of the file that takes it.
- The pragma checker runs inside each analyzer's pass, so a typo'd rule name is
  caught by the pass it was addressed to rather than by a separate lint.
- One pragma exists in the tree today —
  `internal/generate/compose/compose_test.go` suppressing `semconvlit`, because
  the point of that test case is a key the registry does not know and which by
  definition has no constant. A second one appearing is a thing to notice, not
  a thing to prevent.
- Grepping `//depcheck:allow` is the standing inventory of accepted crossings,
  the same way printing the waiver file on every harness run is the standing
  inventory of accepted divergences.
