# 8. A stamp is a rebuild-cost promise, not a content promise

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §6, §10 decision 7;
  [golden/format/STAMPS.md](../../golden/format/STAMPS.md),
  [format.md](../format.md) §8

## Context

The format's first invariant is determinism: same archive in, same stamp, same
file name, on any machine. The rewrite therefore raises an obvious question —
must a clean-room implementation reproduce the reference implementation's
stamps? Issue #5 §6 is careful about it: canonical-content equality is
**mandatory**, stamp identity is tracked as an **aspiration per fixture**.

Measuring why produced two independent answers.

- A stamp covers a pyramid's **derivation stamp**, and that value is written
  nowhere in the `.atlas` file. Every other part is recoverable from the
  bundle; that one is out of reach by construction, not by omission.
- A derivation stamp deliberately covers **the deriving code's own source
  hash**, so that changing how a level is reduced invalidates every pyramid.
  The exact consequence: two derivers that write byte-identical tiles still
  stamp differently, because they are different tools. Shipping the reference
  implementation's source verbatim is the only way around it, and that is what
  a clean room is not.
- The `bend-or` re-crawl measured the same thing from the other direction: two
  builds of one city, identical in all 2,320 entries but `atlas.json`,
  differing only where the clock is — and therefore differing in the stamp and
  the file name.

## Decision

**A stamp is a rebuild-cost promise — "nothing that made this has moved" — and
never a content promise.** Cross-implementation reproducibility is proven where
it is observable: at the tiles and at the composed bundle's canonical content.

The gate asserts proxies that would fail for anything that could move a stamp:
manifest byte-parity, canonical-content equality, filename derivation, every
recoverable part hash, and the accounting itself — a test that builds the stamp
from every part it can and asserts that what is missing is exactly the
pyramids.

## Consequences

- `golden/format/STAMPS.md` carries a per-fixture tracking table with a stated
  status, so the aspiration cannot quietly stay impossible — or quietly become
  possible while nobody checks.
- Four fixtures read HELD because the derived tile set is an input the same way
  the capture archive is; the deriver itself is proven in two halves (the plan
  is identical under a substituted tool hash; the tiles are byte-identical).
- Putting derivation stamps into the manifest would fix the gap and restamp
  every bundle in every library. Format evolution waits until parity passes, so
  it is not done here.
