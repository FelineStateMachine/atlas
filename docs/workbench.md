# The workbench

The workbench is the visual surface beside the Atlas reader. It measures the
installed library and authors exactly one `.atlas-project` selected when the
server starts.

```sh
atlas workbench [-addr 127.0.0.1:6180] [-bundles DIR] [-cache DIR] \
  examples/sample-region.atlas-project
```

The URL is printed on stdout. `-bundles` defaults to the Atlas library and
`-cache` defaults to the shared authoring cache.

## Surface

| Route | Purpose |
|---|---|
| `GET /` | Installed volumes and serving maturity. |
| `GET /volume/{slug}` | One volume's builds, score, semantic contents and provenance. |
| `GET /volume/{slug}/diff` | Build comparison. |
| `GET /project` | The fixed manifest, resolved plan, build controls, run history and artifact handoff. |
| `POST /project/save` | Strictly validate a candidate, then atomically replace the fixed manifest. |
| `POST /project/build` | Start the one native build operation. |
| `GET /project/run` | Replay the latest build rows for live polling or a page reload. |
| `POST /project/open` | Hand the completed file to the operating system to open in Atlas. |
| `GET /project/artifact` | Drag-out/download body for the completed file. |

The old `/sources`, `/operations`, and `/operations/run` routes are gone. Source
selection, protocol settings, semantic mapping and presentation all live in the
manifest; five partially overlapping commands no longer define creation.

## Authority and safety

The command host supplies the absolute manifest, cache, binary and library
paths. No browser field can change them. All state-changing requests are
same-origin checked. Manifest candidates are size-limited, staged beside the
real file, decoded with unknown-field rejection, fully validated, synced and
renamed only after success.

One build runs at a time. Its argv is always:

```text
atlas build --log-json -cache CACHE -bundles LIBRARY [-offline] MANIFEST
```

Arguments are passed directly to the operating system without a shell. The
subprocess belongs to an in-memory supervisor, not the initiating HTTP request.
Closing or reloading the page therefore does not cancel it; the next page load
replays every retained row. A failed or successful result remains visible until
the next build.

The native-open action accepts only a `.atlas` path reported by the build and
contained by the configured library. The drag card publishes the same artifact
through browser drag data (`DownloadURL`, URI and path); clicking the card never
navigates the workbench webview to the file. **Open in Atlas** is an explicit
native handoff.

## Page model

The Project page is a compact projection of the compiler:

```text
feature adapters ─┐
                  ├─ exact requests ─ evidence cache ─ observations ─┐
raster adapters ──┘                                                   │
                                                                      ├─ native volume
typed mappings ───────── semantic feature sets + relationships ──────┤
presentation ─────────── styles + ordered layers + labels ────────────┘
```

It shows request cache state, estimated bytes, the project digest, source
adapters, obligations, semantic-set/layer counts, and output library before any
build begins. The manifest editor is deliberately the durable configuration
rather than a second form model that could drift from it.

## Boundaries

`internal/workbench` imports the native authoring planner for read-only plan
projection and shells out to the same `atlas` executable for builds. The
library/diff pages continue to read installed native volumes. The host injects
the hypermedia runtime and native-open function. OS process execution stays in
the operation runner; native application choice stays in the command host.
