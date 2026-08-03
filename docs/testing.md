# Testing

One rule above the others: **a gate that did not judge may not say PASS.**
Every required test's inputs are committed to this repository or built by
the run itself. No required test skips, reads an `ATLAS_*` variable, or
looks in the home directory. CI enforces the first half of that
structurally: Go tests run through `tools/testgate`, which fails the run if
any test skipped, whatever the exit code said.

History, where it is needed, is a pointer rather than a chapter: the
pre-rewrite tree and its capture programs are archived on the
`golden-reference` tag (mirrored at `archive/golden-reference`), and the
behavioral differences accepted against it are
[decision 18](decisions/0018-divergences-from-the-reference.md).

## The layers

**`make test`** is the whole required surface, and what CI runs on
ubuntu, macOS and Windows (spelled step by step in
`.github/workflows/ci.yml`, because the Windows runner has no make):

- `go vet ./...`, then `go run ./tools/testgate ./...` — every Go test in
  the module, skip-proof, with `-race` on the platform where a C toolchain
  is free.
- `npm run lane` and `npm run seam-lane` — each TypeScript lane's boundary
  rules, `tsc --strict`, and its own suite.
- `go run ./tools/depcheck` — the import matrix, hostenv purity, network
  confinement, semconv discipline.

**`make test-e2e`** is the application in a real browser. The run builds
every one of its own prerequisites: the seam (`make static`), a registry
packed from the committed corpus (`tests/e2e/prep`), and the server
Playwright starts itself. The specs assert arrangement — URLs, the state
island, the topbar's controls, session memory — never pixels.

**`make corpus-smoke`** is the maintainer's deep check and deliberately not
a CI gate: it walks a real installed library (`-bundles`, else
`$ATLAS_BUNDLES_DIR`, else the application's data directory) and holds
every current-format bundle to the reader's invariants. It compares no
stamps, no hashes and no content.

## What tests are made of

**Stated bundles.** The default. A test that needs a bundle writes one
through `format/bundle`'s real writer from a few lines of Go (see
`format/bundle/fixture_test.go`, `internal/workbench/bundles_test.go`) —
nothing opaque is committed, and a bundle a test measures is a bundle the
application could open.

**The corpus** (`testdata/corpus/`). Real extractions, kept because their
provenance is public: bend-or is city open data, mars is NASA Trek. They
give the format, render, island and e2e suites real bytes with real depth —
multipart zones, described shapes, a sphere with two lenses — at the price
of about 14 MB. Public provenance is the bar for adding anything here; see
its README.

**Hand-derived vectors** (`analysis/testdata/cells/`). The cell-system
contract, written down rather than recorded, and read by both
implementations of the arithmetic: the TypeScript lane's conformance suite
and the Go twin's (`tests/cells`). Until the planned unification deletes
one copy, these vectors are what keep the two from drifting.

**Property tests** for what no single example can pin down: containment,
hierarchy, carry and plan-order invariants over invented locations in the
analysis lane; determinism in the pipeline (build twice, byte-identical);
fuzzing where bytes cross a trust boundary (`format/bundle`'s reader).

## Where tests live

Tests live with their package. Two trees exist because some tests cannot:

- `tests/` — Go tests that need `os` against packages that may not
  (`hostenv` forbids `internal/app` the filesystem): `tests/island` drives
  the application's routes over the corpus, `tests/cells` reads the shared
  vectors, `tests/e2e` is the browser suite, `tests/corpus` the shared
  packer.
- `tools/` — the enforcement commands: `depcheck`, `testgate`,
  `corpussmoke`.

## The bar for a new test

Ask one question: what breaks if this test is deleted? A test that anchors
a number nobody can recompute, reads a path only one machine has, or skips
when the world is not to its liking does not clear it. State the bundle,
derive the expectation by hand, and if the inputs cannot be committed, the
check belongs behind `make corpus-smoke` — named as a maintainer's check,
not dressed as a gate.
