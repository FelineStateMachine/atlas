# 4. The interchange document is Atlas's own schema

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §5.1, §10 decision 6;
  [generate.md](../generate.md) §1

## Context

The reference implementation's interchange document is `mgdoc` — MapGenie's
shape. Every other translator impersonates it: a source that has nothing to do
with MapGenie must first pretend to be MapGenie in order to be composed. The
capture archive inherited the same flavour in its naming. That is a data
source's vocabulary sitting at the centre of a format that is supposed to be
source-neutral.

## Decision

**No data source's shape, vocabulary, or name may appear outside its own
`internal/generate/sources/<name>/` directory.**

`internal/generate/doc` defines the Atlas interchange document, designed
backwards from what composition needs — worlds, lenses, collections, features,
geometry, attributes, provenance — with no source's shape privileged. MapGenie
becomes an ordinary translator into it.

The capture archive is a source-neutral concept the generate lane owns and
documents: content-addressed captures, per-source id-space bits, the same-slug
policy. The **existing on-disk archive is kept verbatim as input data** — years
of captured history are data, not code — read through its legacy layout;
nothing new inherits its naming.

## Consequences

- A document is produced and consumed inside one run. It is not an archive
  format, nothing is expected to read one back in five years, and `atlas
  translate` prints one so the two halves of the lane can be reasoned about
  separately.
- The translator fixtures pin *source semantics*; they are reference material
  and regression tripwires, not byte gates, because equivalence is judged at
  the composed-bundle level.
- Curation moves out of code into reviewed data files, and a client that speaks
  a standard protocol is parameterized by data — so a new dataset on a known
  protocol is a configuration entry rather than a new client.
- Each source's registry entry carries license and attribution, which the
  workbench's source cards surface.
