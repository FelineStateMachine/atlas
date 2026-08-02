# The golden harness

The clean-room rewrite of issue #5 has one definition of done: the current
implementation's observable behavior. This directory is the instrument that
measures against it — captured fixtures, the gates that compare candidate to
capture, and the guardrails that refuse an architecture violation before a
fixture ever gets the chance to disagree.

Everything here answers to [issue #5](https://github.com/FelineStateMachine/atlas/issues/5),
§6 (the harness) and §9 (guardrails as code). Where this file and the issue
disagree, the issue wins.

## Running it

```sh
make golden          # every gate, in order; skips what has no lane yet
make depcheck        # the Go guardrails alone
make golden-all      # attempt every gate, including the unready ones
make lint-lanes      # the TypeScript guardrails (needs M6's lanes + eslint)
go test ./golden/... # the harness's own tests
```

`make golden` is the single entrypoint, and it is what CI runs
(`.github/workflows/golden.yml`, ubuntu, on main and the clean-room branches).
A run prints one line per gate, then the waiver file, then a count:

```
  PASS  format-roundtrip  ok  github.com/FelineStateMachine/atlas/golden/format  0.35s
  SKIP  generate-enrich   awaiting M2+M3 — the pipeline lanes: …
  PASS  analysis-vectors  9 grounds, 178 vectors in 8 families, 28 plans over …
  PASS  http-replay       ok  github.com/FelineStateMachine/atlas/golden/http
  PASS  depcheck          depcheck: 5 rules over …

waivers: 2 accepted divergences from the goldens (golden/waivers.json)
  WAIVED  app-shell-page   http-replay/GET /: …
  WAIVED  seam-assets      http-replay/GET /static/app.css, …

6 suites: 4 passed, 2 skipped, 0 failed
```

A run where everything skips is green. That is deliberate: the harness lands
before the lanes it judges, and its skip lines are the running list of what
nobody has proven yet.

One environment variable deepens a run rather than changing it:

```sh
ATLAS_REGISTRY_DIR=~/Library/Application\ Support/dev.felinestatemachine.atlas/bundles make golden
```

`format-roundtrip` then opens the real `.atlas` files the fixtures were
extracted from and checks the extractions against them, instead of standing on
the committed extractions alone; `http-replay` likewise runs at whatever depth
the machine allows — catalog composition and every refusal on any machine, the
recorded bodies too when the variable names a bundles directory holding the
fixture builds (see `golden/http/NOTES.md`). Both modes must pass; CI runs the
first.

Four gates run today. `depcheck`, `format-roundtrip` and `http-replay` need
only Go; `analysis-vectors` runs on plain node and needs the cell math's own
dependencies — `npm --prefix frontend ci` — because the implementation it
drives until M6 is the current tree's module, which imports s2js and
OpenLayers. The workflow installs them; no browser and no bundler is involved.

## The gates

The order below is the order the rewrite builds in — format, pipeline, app,
seam — and the order the harness runs.

| Gate | Milestone | What it checks |
| --- | --- | --- |
| `format-roundtrip` | M1 | A fixture bundle read and rewritten by `format/bundle` is canonically identical. Canonical-content equality is mandatory; stamp identity is tracked per fixture as an aspiration (issue #5 §6). Runs today. |
| `generate-enrich` | M2+M3 | `generate ⊕ enrich` reproduces the composed bundle fixtures. Correctness is defined at the composed-bundle level, which is why the internal interchange shape is free to differ from the old tree's (§5.1). |
| `analysis-vectors` | M0 | The hand-derived geohash and S2 goldens and every recorded cell plan, byte-exact, compared **positionally** — plan emission order is frozen (§5.4). Runs today, against the current systems; M6 re-points it at `analysis/cellsystems` by changing one import. |
| `parity-compare` | M5+M6 | The ~45-step tour, extended into its blind spots, re-pointed at the new app. Diagnostics are emitted jointly: server session state as a JSON island plus seam state, under the golden key names. |
| `http-replay` | M5 | Recorded catalog and sampled `/data` responses, replayed with their headers. The data plane is byte-compatible with today because the seam and the goldens both consume it (§4.2). Runs today, in two modes; the app plane's three exchanges are waived and reduced, not skipped. |
| `depcheck` | M0 | The lane boundaries, as static analysis. Runs today. |

Each unready gate declares the file that will run it (`golden/parity/compare.mjs`,
`golden/pipeline/reproduce_test.go`, …). The milestone that lands a lane flips
its gate on by writing that file; nothing about the harness needs editing but
the `ready` flag. `make golden-all` attempts them regardless, which is the way
to watch a new gate go red before you make it green.

## format-roundtrip

`golden/format` runs in two modes, and the split is what makes the gate both
enforceable in CI and honest about what CI can see.

**Always-on**, with no library present, it stands on the committed extractions
alone. Each canonicalized manifest parses into `format/bundle` types and
re-encodes to the same canonical bytes; the identity derived from it names the
file `FIXTURES.json` says the build carries; each world's unpacked locations
pack again to the byte count and SHA-256 the extraction recorded; the
extractions reassemble into an archive that `Reader.Validate` accepts, which is
how the offline scan, the per-kind counts, the geometry rules and every
`format/semconv` rule get run against real captured payloads with no `.atlas`
in sight; and the five manifests fold as a library whose derived index still
speaks the legacy `games`/`maps` wire keys.

**Registry mode**, with `ATLAS_REGISTRY_DIR` set, opens each fixture build by
name and holds this package to the bytes on disk: the manifest re-encodes byte
for byte (those bytes are a stamped part), every recorded part hash still
matches, every payload canonicalizes to its committed extraction, the packed
payloads unpack to the committed locations, the icon and tile inventories match
name for name and hash for hash, every tile is stored uncompressed, and the
whole bundle validates. A directory holding none of the fixture builds skips —
it is not the library they came from. A directory holding some of them fails on
the rest, because a partial library is a broken oracle rather than a smaller
one.

**Stamps are not asserted.** One class of stamped part — a tile pyramid's
derivation stamp — is not recoverable from a finished bundle, so the sum cannot
be recomputed by a reader. `golden/format/STAMPS.md` tracks the aspiration per
fixture, names the proxies that are enforced instead, and says what in M2 would
close it.

## analysis-vectors

`analysis-vectors` is the one gate that did not wait for its lane, because it
did not have to: the cell systems already exist as the oracle, so the vectors
judge an implementation from the day they are captured and M6 inherits a gate
that has been green — and therefore honest — the whole way. What each family
pins, what a ground carries, and where the one-line switch lives are in
[`analysis/README.md`](analysis/README.md).

## generate-enrich

**Half-built, and it stays skipped.** Its contract is
`generate ⊕ enrich` over *every* bundle fixture, merge included, and the enrich
lane does not exist — so the merged, split-sheet, lens-sharded and city
fixtures cannot be reproduced by anything yet. `golden/pipeline` exists and runs
the single-source half as an ordinary test: it composes the plain-MapGenie
fixture from archived captures and holds it against the committed extractions,
byte for byte as things stand. Those tests read the capture archive and the
derived tile set, which are deliberately not in git, so they skip unless
`ATLAS_ARCHIVE_DIR` and `ATLAS_TILES_INDEX` — or the repository's own gitignored
`crawl/` and `tiles/` — are present. The gate's `ready` flag flips when
enrichment can answer for the rest. See `docs/generate.md` §8.

## depcheck

`golden/depcheck` is a `go/analysis` multichecker — the machinery `go vet` is
built on — carrying five rules:

- **`laneimports`** — the import matrix of §3.2. `format/` is stdlib-only;
  `generate/` and `enrich/` see `format/` and never each other, the app, the
  workbench, or the harness; `app/` sees `format/` and its own packages;
  `workbench/` sees `format/` plus `enrich/maturity` and shells out for the
  rest; nothing imports `render/`. Every lane may import `internal/logging` —
  one event stream narrates the whole system (§9) — and `internal/logging`
  imports no lane back.
- **`cleanroom`** — no clean-room lane imports the pre-rewrite tree (§1). The
  old packages are the oracle, not the base. `golden/` is exempt: capturing
  from the old tree is its job.
- **`hostenv`** — no `os`, `os/exec`, `path/filepath`, `syscall`, or host
  toolkit outside `internal/app/hostenv` (§3.3). This is the rule that keeps
  the handler pure and the third host — a WASM service worker — reachable.
- **`netconfine`** — outbound HTTP only in `internal/generate/crawl` (§2, §9).
  Two checks: the format and pipeline lanes may not import `net/http` at all,
  and the *outbound* half of `net/http` (`Get`, `Client`, `DefaultTransport`,
  `NewRequest`, …) is reported anywhere else, so the app can serve HTTP
  without being able to fetch it.
- **`semconvlit`** — an `atlas.*` key spelled as a string literal outside
  `format/semconv` (§9). Producers are strict, and the strictness lives in the
  registry.

Every message names the contract and cites the issue section, because a
violation should teach the boundary rather than merely block it. That is §9's
stated reason: the working style here involves agents writing code, and the
best boundary feedback is an immediate mechanical "we don't do that here".

**Scope.** With no argument, depcheck analyzes only the clean-room roots of
§3.1 that exist on disk (`format`, `internal/{generate,enrich,app,workbench}`,
`internal/logging`, `cmd/atlas`, `golden`). The old tree is neither loaded nor judged, and a rule
about a lane nobody has written yet passes by having nothing to say. Pass a
pattern to override: `go run ./golden/depcheck ./format/...`.

**The escape hatch.** A boundary crossing that is genuinely correct is
annotated in place:

```go
resp, err := http.Get(u) //depcheck:allow netconfine the crawl politeness probe runs before the archive exists
```

The pragma sits on the offending line or the line above it, names one rule (or
`all`), and must carry a written reason — an unexplained pragma is itself a
finding. It is the source-level twin of a waiver, and it is read the same way:
as a cost.

The TypeScript half of the same boundaries lives in `/eslint.config.mjs`
(analysis touches no DOM and nothing app-shaped; render imports only analysis
and itself; fetch in the data layer; no bare `console.*` outside the log
module). It is not wired into the existing frontend build and waits on M6's
lanes.

## Waivers

**Goldens are never edited to match the candidate.** This is the harness's one
absolute rule. A golden that has been adjusted until the new code agrees with
it has stopped being an oracle; it is a transcript of whatever the new code
happened to do.

Every accepted difference is an entry in `golden/waivers.json` with a written
reason:

```json
[
  {
    "id": "merge-ledger-order",
    "suite": "generate-enrich",
    "fixture": "cyberpunk-2077",
    "reason": "The old merge emitted ledger rows in map order; the new one sorts by donor id. Same rows, stable order, and the sorted form is what the workbench wanted anyway.",
    "added": "2026-08-02"
  }
]
```

The process:

1. A gate goes red. **Read the diff before doing anything else** — the default
   assumption is that the candidate is wrong, because the golden is a recording
   of software that works.
2. If the difference is a defect in the candidate, fix the candidate.
3. If the difference is genuinely better or genuinely unavoidable, add a waiver
   in the same commit as the change that causes it, with a reason a reviewer
   can weigh. Known defects carried deliberately from the old tree (§6 records
   several) are waivers too — annotated, not silently reproduced.
4. Never step 4. There is no path where the fixture changes.

The harness prints every waiver on every run and fails outright on one with no
reason. Non-emptiness is the point: the file is a standing bill.

Fixture *provenance* — what each fixture is, when it was captured, from which
build — belongs to `golden/capture/` and, at the end, to `docs/golden.md`
(§8). This file is about the gates.

## Scope in time

The harness is the rewrite's correctness instrument, not the project's
permanent test strategy (§6). Once parity passes on every fixture volume and
the `golden-reference` tag is archived, the goldens become one regression layer
among ordinary tests — self-captured, amended through reviewed diffs — and the
never-edit-to-match rule retires with the oracle it protected.

The guardrails do not retire. They enforce an architecture the project keeps
after the rewrite is done, and each milestone adds its lane's rules as part of
its exit gate.
