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
volume ordering by title, the stamped base URL, `bundlesDir`. The exchanges
that need real archive content are skipped with the reason printed, never
counted as passes.

**registry** links the fixture builds named in `FIXTURES.json` into a
directory of their own — the way `golden/capture/capture.sh` built the
registry it recorded from — and serves them through the real OS host. Every
exchange replays, bodies included. On the machine this was written on that is
35 of 38 exchanges byte-identical, 3 waived.

A library that has moved past the fixture set (a fixture build deleted, a
newer build of a fixture volume installed) makes the mode skip with the file
it could not find, rather than replaying against a different library and
reporting a difference that is really a fixture-provenance question.

## The waived three

`/`, `/static/app.css` and `/static/app.js` are entries in
`golden/waivers.json`. The first is the old hash-routed shell, which the
rewrite replaces with a page per world by design; the other two are the
reference implementation's built frontend, which is M6's lane. Each still
carries a reduced assertion, written next to the waiver in `replay_test.go` —
a waiver never means "unchecked".

## Byte ranges: a deliberate non-improvement, for now

The transcript records a `Range: bytes=0-99` request answered **200 with the
whole body and no `Accept-Ranges`**. Tiles are stored uncompressed in the
container precisely so that a server *could* answer a range out of the archive
without inflating anything, and issue #5 §2 describes them as "served by byte
range" — but the reference implementation sets a length and copies the entry,
and the recording is what the gate compares against.

So `internal/app` does not serve ranges either, and
`internal/app/data.go` says so where a reader will meet it. Serving them would
be a strictly better data plane and a red golden: the recorded exchange would
answer 206 with 100 bytes. That is a change to make on purpose — a waiver in
the same commit, with a reason — when something actually wants ranges. It is
not a change to make by accident, which is what a test asserting the current
behaviour is for (`TestContentDoesNotServeRanges` in `internal/app`).
