# Atlas authoring

Atlas creation has one public input and one public operation:

```text
sample-region.atlas-project
        │
        ▼
atlas build ── plan ── capture ── observe ── assemble ── compile ── install
        │           shared content cache                         │
        └────────────────────────────────────────────────────────┘
                                      immutable native .atlas
```

The manifest is configuration, not a project directory. Source bytes are held
once in a shared content-addressed cache; successful outputs are held once in
the Atlas library. Moving the manifest preserves request identity because
remote locators remain authored and local locators resolve relative to the
manifest.

## Commands

```sh
# Inspect the exact source requests and output without fetching or writing.
go run ./cmd/atlas build -plan-json examples/sample-region.atlas-project

# Build and atomically install a native vNext volume.
go run ./cmd/atlas build examples/sample-region.atlas-project

# Rebuild only from already captured evidence.
go run ./cmd/atlas build -offline examples/sample-region.atlas-project

# Rebuild from the exact evidence selected by an earlier Atlas or receipt.
go run ./cmd/atlas build -replay previous-build.atlas examples/sample-region.atlas-project

# Edit, inspect, build, and hand the result to Atlas from the workbench.
go run ./cmd/atlas workbench examples/sample-region.atlas-project
```

`-cache` selects the evidence cache and `-bundles` selects the Atlas library.
Their defaults are the application-owned cache and library.

## Contract

- `atlas-project/v2` is an intentional hard break. Top-level `feature-sets`
  declare stable semantic membership, one homogeneous `point`, `path`, or
  `area` geometry family, typed property contracts, and relationship contracts.
  Semantic types remain domain language rather than geometry labels.
- Feature adapters acquire evidence; source mappings only bind identity, title,
  geometry transforms, property paths, and relationship paths to a declared
  FeatureSet. An adapter cannot redefine a set's title, semantic type, geometry,
  field types, or relationship meaning.
- The YAML decoder is strict. Unknown keys, duplicate identities, incomplete
  mappings, broken presentation references, and invalid raster plans fail.
- Adapters describe acquisition; declarative mappings describe meaning.
  Built-ins are `geojson`, `arcgis-feature-service`, `ogc-api-features`,
  `raster-file`, explicit-window `xyz`, and REST-template `wmts`.
- Planning performs no network or filesystem writes. Every request has a stable
  binding identity plus a source-independent acquisition identity that includes
  adapter version, portable locator, media type, and tile coordinates. The plan
  reports validated cache presence, estimated bytes, and obligations; identical
  acquisitions may be shared across projects without merging their semantics.
- Every plan resolves stable refusal budgets for request bytes, total evidence,
  request count, raster tile count, and raster pixel count. The manifest may
  state smaller or deliberately larger positive limits. Planning rejects tile
  explosions before enumeration; capture rejects at `limit + 1`; offline and
  exact replay apply the same limits. Atlas never truncates to fit a budget.
- Capture envelopes retain request identity, body hash, media type, timestamp,
  licence, and attribution. Changed bodies append history instead of replacing
  evidence. Cache reads verify the envelope, body length, and body SHA-256;
  repeated or concurrent bytes retain one first-seen timestamp.
- Observations retain source-native identity and evidence. Assembly begins from
  the declared contracts, so empty sets and optional fields remain explicit in
  the native result. It merges only canonical identities declared by the
  manifest; earlier sources win conflicts and every contributor remains in
  provenance. Required properties and relationships are checked after fusion,
  allowing one source to complete another. Property contracts retain bool,
  int64, float64, string, bytes, and stable-ID kinds; relationships name their
  target FeatureSet and canonical feature identity.
- A local raster image is deterministically expanded into the declared native
  pyramid. Explicit XYZ sources retain authored windows. Authored supporting
  assets are captured, packed, and may be referenced directly by styles.
- Presentation is compiled directly into native styles and layers. There is no
  legacy JSON document or enrichment projection between authored intent and the
  packed schema.
- Publishing writes from the already validated file handle, syncs the staged
  file, commits without replacement, syncs the library directory, and fully
  validates the installed result. An existing stamp-named file must itself be
  valid and byte-identical. Rebuilding the same project from the same evidence
  produces the same bytes and returns the existing file; corrupt or colliding
  targets fail loudly.
- Every build embeds an executable v3 evidence receipt. Each selected capture
  carries the adapter and version, exact hashes and media metadata, and its
  licence/attribution obligation. `-replay` selects those
  exact immutable capture hashes even when newer evidence exists. Mapping and
  presentation changes may deliberately replay the same acquisitions, while a
  changed locator, query, media type, or adapter version is refused as drift.

## Workbench

The workbench receives one manifest path at startup. Browser requests cannot
choose another file, cache, command, or output directory.

The Project page shows the resolved build graph, cache readiness, estimated
input, licence/attribution obligations, manifest editor, and the build log. A
build belongs to the workbench process rather than the POST request, so reloads
do not cancel it. When it completes, **Open in Atlas** performs a native file
handoff; the artifact card also supports dragging the `.atlas` onto an Atlas
window.

The compiled inspector is derived only from the validated `.atlas`, never from
the manifest. It shows the carried/runtime schema boundary, worlds, coordinate
space, explicit FeatureSet contracts (including empty sets), paged feature
facts, geometry, relationships, provenance, raster/presentation semantics,
assets, and the integrity-protected evidence/legal receipt.

## Production envelope

The current native container is intentionally bounded: a strict entry/reference
budget, exact ZIP-entry role bijection, Store-only table/blob references,
bounded bootstrap/schema/table/blob reads, deterministic validation order, and
streamed opaque-blob hashing. These are refusal boundaries, not claims that the
current single feature pack and per-tile ZIP layout scale without limit. The
next storage-generation break for genuinely massive worlds is partitioned
feature blocks plus deterministic raster shard packs and range/page APIs.

Release publication runs correctness and browser acceptance first, smokes a
packaged CLI by building Sample Region, generates checksums for the exact asset
set, uploads to a draft, verifies every remote asset name, and only then makes
the release public. Both CLI and desktop binaries answer `--version` from the
release tag.

The former `crawl`, `tiles`, `compose`, `enrich`, and `translate` commands and
the Sources/Operations workbench pages are no longer public interfaces. Their
code remains only as migration and corpus-test material while existing fixtures
are retired.
