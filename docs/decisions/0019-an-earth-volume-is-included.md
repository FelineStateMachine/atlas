# 19. The desktop ships with an included Earth volume

- **Date:** 2026-08-03
- **Status:** accepted
- **Where it is written down:** [app.md](../app.md) §1.4; [generate.md](../generate.md) §2.4; `included/README.md`; `main.go`; `internal/generate/crawl/bluemarble.go`; `internal/generate/sources/nasabluemarble`

## Context

A fresh install opened onto the empty-library card. Everything past that card
assumed the reader could produce or obtain a `.atlas` file, which meant the
whole pipeline stood between a new reader and their first world — and there was
no committed bundle with real rasters anywhere in the repository, so nothing
end to end ever proved the imagery leg of the data plane.

NASA Earth Observatory publishes Blue Marble Next Generation: a global,
cloud-free, true-color composite of the whole planet, free of copyright, at a
size that survives a repository. The format already had everything needed to
carry it — the whole-sphere equirectangular declarations NASA Trek's worlds
make, a registry fold that lets builds sit side by side, and an install path
with validation, versioned naming, atomic staging and idempotency.

## Decision

Ship the desktop application with one included volume: Earth, as a real,
ordinary format-v3 bundle composed from a pinned Blue Marble capture, committed
at `included/` and embedded in the desktop executable. At startup the shell
installs it into the library through `format/bundle`'s own rules, before the
host's first scan, and never touches the embedded copy again.

The capture is pinned — one URL, one SHA-256, one recorded capture time, one
spelled derivation policy — and the crawl that fetches it is the only networked
step, run by hand (`make included-earth`). A digest mismatch is a refusal.
Ordinary builds, tests and every launch are offline; a release checkout builds
from the committed artifact alone.

The included build is ordinary on purpose: nothing anywhere special-cases it.
The application handler does not know built-in volumes exist; `atlas serve` and
the CLI install nothing; a newer Earth build installed later simply wins the
fold, and deleting the installed file brings it back at next launch by the same
idempotent install.

## Consequences

- A first launch opens onto a world, and the empty-library card becomes a
  state most readers never see.
- The repository carries a ~5 MiB binary artifact, regenerable byte for byte
  from the pin by `make included-earth`, with its provenance and recipe beside
  it in `included/README.md`.
- The e2e registry gains its first volume with real rasters, so the data
  plane's imagery leg is proven in a browser rather than only inferred.
- The embedded copy costs every desktop binary its size once; the headless
  host and CLI cost nothing.
- Anything that later enriches Earth — features, layers, regional extraction —
  arrives as ordinary builds and ordinary enrichment, because the included
  volume made no special claims for itself.
