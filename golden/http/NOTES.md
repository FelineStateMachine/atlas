# Reading the HTTP replay

`replay_test.go` is the `http-replay` gate: every exchange in
`golden/fixtures/http/transcript.json` asked again of `internal/app` and held
to the recorded answer — status, the recorded header set in both directions,
and the body by length and SHA-256, after the capture's own two
normalizations (the `Date` header, and the library directory the catalog
reports).

## The two modes

    go test ./golden/http/...                      # synthesized
    ATLAS_REGISTRY_DIR=<bundles dir> go test ./golden/http/...   # + registry

**synthesized** builds a volume store out of the committed fixture manifests.
It has manifests and no archives, so it answers the catalog and every refusal
and nothing else — which is enough to check catalog composition *byte for
byte* on a machine with no bundles, CI included: field names, field order,
volume ordering by title, the stamped base URL, `bundlesDir`. That is 9 of the
43 exchanges. The rest are skipped with the reason printed, never counted as
passes.

**registry** links the fixture builds named in `FIXTURES.json` into a
directory of their own — the way `golden/capture/capture.sh` builds the
registry it records from — and serves them through the real OS host. Every
exchange replays, bodies included: 40 of 43 byte-identical, 3 waived.

The files come from two places, for the same reason the capture script reads
two. The games and the planet are installed, and `ATLAS_REGISTRY_DIR` names
that library. The public city was **built for the fixture** and is read from
where it was built — `ATLAS_GOLDEN_CITY_DIR`, `dist/bundles` by default. The
test does not carry a list of which is which: a fixture volume whose
`FIXTURES.json` entry has a `builtFor` block is one the library never held,
and that is the rule it follows.

A library that has moved past the fixture set (a fixture build deleted, a
newer build of a fixture volume installed, a city bundle not built in this
checkout) makes the mode skip with the file it could not find and the
convention that would supply it, rather than replaying against a different
library and reporting a difference that is really a fixture-provenance
question.

## The waived three

`/`, `/static/app.css` and `/static/app.js` are entries in
`golden/waivers.json`. The first is the old hash-routed shell, which the
rewrite replaces with a page per world by design; the other two are the
reference implementation's built frontend, which is M6's lane. Each still
carries a reduced assertion, written next to the waiver in `replay_test.go` —
a waiver never means "unchecked".

## Byte ranges: a deliberate non-improvement, for now

The transcript records a `Range: bytes=0-99` request — against the city's
first tile, since the sampling walks the catalog and `bend-or` sorts first —
answered **200 with the whole body and no `Accept-Ranges`**. Tiles are stored
uncompressed in the container precisely so that a server *could* answer a
range out of the archive without inflating anything, and issue #5 §2 describes
them as "served by byte range" — but the reference implementation sets a
length and copies the entry, and the recording is what the gate compares
against.

So `internal/app` does not serve ranges either, and
`internal/app/data.go` says so where a reader will meet it. Serving them would
be a strictly better data plane and a red golden: the recorded exchange would
answer 206 with 100 bytes. That is a change to make on purpose — a waiver in
the same commit, with a reason — when something actually wants ranges. It is
not a change to make by accident, which is what a test asserting the current
behaviour is for (`TestContentDoesNotServeRanges` in `internal/app`).

## When the fixture set moves

The transcript is derived from what is served — the catalog names the volumes,
each volume's first world payload names its lenses and its icons — so a volume
joining the set re-spells exchanges that were already pinned. That is what
happened when the public city landed: `bend-or` sorts first by title, so the
range request and the eight refusals moved onto its world and its `.png`
tiles, and five new exchanges joined for the city's own payloads. 38 exchanges
became 43.

The answer to that is a re-capture from the reference tree, never an edit:
build the six-volume registry the way `capture.sh` does, run the reference
app headless against it, and run `golden/capture/http` at it. Everything the
gate then compares is the oracle's own answer to a question that had not been
asked before.
