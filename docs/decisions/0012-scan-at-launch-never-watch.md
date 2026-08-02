# 12. The registry is scanned at launch and on import — never watched

- **Date:** 2026-08-02
- **Status:** accepted
- **Where it is written down:** issue #5 §2, §4.6, §10 decision 15;
  [app.md](../app.md) §1.1, [format.md](../format.md)

## Context

The library is a directory of immutable `.atlas` files. Something has to decide
which build of a volume serves, and something has to notice when the directory
changes. The reference implementation watches the directory. A watcher is a
long-lived OS resource with platform-specific behaviour, it fires on partial
writes, and it makes "what is the library right now" a question with a racy
answer.

## Decision

**Scan at launch, rescan on import, never watch.** A file dropped into the
library from outside appears at the next launch. The user's expectation is
"open Atlas and see my registry", not a live file watcher.

The fold itself — newest capture, then policy revision, then stamp, then
locator; newest-per-slug wins — is **pure and lives in `format/bundle`**.
Touching the filesystem is a `hostenv` concern
([ADR 7](0007-hostenv-portability.md)), so a host only ever has to produce
descriptors.

`index.json` is always derived from the files. New builds land beside old ones;
nothing ever mutates a published bundle.

## Consequences

- `VolumeStore.Rescan()` returns the volumes whose **serving build moved**,
  which is exactly what the SSE stream needs to announce: a refreshed volume
  selector, a refreshed library card, and a full-refresh directive when the
  current volume's serving stamp moves.
- An import (or a pipeline install performed through the workbench) is the only
  in-session trigger, and it is one the application already knows about.
- `Location()` is a label, not a path the handler takes apart — it is what the
  catalog reports as `bundlesDir` and what the empty-library page shows.
- The pure fold is testable without a filesystem, and it is the same code the
  CLI's install path uses, so the desktop app and the pipeline can never
  disagree about which build is current.
