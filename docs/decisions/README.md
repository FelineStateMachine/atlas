# Decisions

Short records for the calls this rewrite makes. Each one is context, decision,
consequences — what was true, what was chosen, and what the choice costs. They
are written from the artifacts: the lane documents, the harness, the code, and
[issue #5](https://github.com/FelineStateMachine/atlas/issues/5), which is the
source of truth. Where a record and the issue disagree, the issue wins.

**These are records, not policy.** A decision that changes gets a new record
that supersedes the old one; the old one stays, because the reasoning is the
point. Issue #5 §8's comment policy says the rest: a comment states a
constraint the code cannot show, and "how we got here" prose belongs here or
nowhere.

## The calls issue #5 makes (§8)

| | Decision |
|---|---|
| [1](0001-htmx-4-at-the-center.md) | HTMX 4 owns the discrete state |
| [2](0002-allons-is-removed.md) | allons is removed, and nothing replaces it |
| [3](0003-merge-moves-to-enrich.md) | Cross-source merge moves from generate to enrich |
| [4](0004-source-neutral-interchange-document.md) | The interchange document is Atlas's own schema |
| [5](0005-unbounded-feature-maturity.md) | Maturity is per-feature, additive, unbounded and monotone-gated |
| [6](0006-the-scene-description-seam.md) | The seam is driven by an inert scene description |
| [7](0007-hostenv-portability.md) | One pure handler, everything OS-shaped behind hostenv |
| [8](0008-stamp-identity-is-an-aspiration.md) | A stamp is a rebuild-cost promise, not a content promise |

## The calls execution made

Decisions taken while building the lanes, which issue #5 §10 does not record.

| | Decision |
|---|---|
| [9](0009-the-enrich-compose-adapter-seam.md) | `generate ⊕ enrich` is joined by an adapter in the CLI |
| [10](0010-revision-packing.md) | An enriched build wins the fold by packing two policy revisions |
| [11](0011-one-event-stream.md) | One leveled event stream, and a lane of its own for it |
| [12](0012-scan-at-launch-never-watch.md) | The registry is scanned at launch and on import — never watched |
| [13](0013-assets-and-static-are-two-mounts.md) | `/assets` and `/static` are two different mounts |
| [14](0014-the-depcheck-pragma.md) | A boundary crossing is annotated in place, with a reason |
| [15](0015-the-ground-descriptor.md) | A cell system is handed a `Ground`, not an application |
| [16](0016-uncurated-captures-are-passed-over.md) | A capture a reader cannot answer for is passed over, not refused |
| [17](0017-the-host-follows-redirects.md) | The desktop host follows the application's redirects |
