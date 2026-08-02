# The application

**Status: in progress (M5).** This document specifies the hypermedia
application: the URL surface, the session record, the partial and event
envelopes, and the contract between the handler and whatever host it is
mounted in. Sections marked **next wave** name what is deliberately a stub
today, so nobody mistakes a placeholder for a decision.

The implementation is `internal/app`, its host seam is
`internal/app/hostenv`, and the headless host is `atlas serve`
(`cmd/atlas/serve.go`). Where this document and the code disagree, take it as
a defect in one of them and say so.

---

## 1. One handler, three hosts

The application is **one pure `http.Handler`**. It touches no filesystem,
builds no path, opens no dialog, and reads no environment. Everything
OS-shaped arrives through three small interfaces:

```go
type Hostenv interface {
    Volumes()  VolumeStore
    Sessions() SessionStore
    PickFile(ctx context.Context) (io.ReadCloser, string, error)
}
```

That is the whole host contract. It exists so the same handler can be mounted
by three different hosts:

| Host | Status | What it supplies |
|---|---|---|
| **Wails webview** | next wave | The desktop window; `PickFile` is the native dialog; the library is the application's data directory. |
| **`atlas serve`** | built | Plain HTTP, no window. `PickFile` refuses with `ErrNotAvailable`. The dev loop, CI, and the parity harness. |
| **WASM service worker** | not scheduled | Go compiled to `js/wasm`; the stores back onto OPFS. Nothing is built for it; the discipline is what keeps it reachable. |

The rule is enforced mechanically: `golden/depcheck`'s `hostenv` analyzer
fails any import of `os`, `os/exec`, `path/filepath`, `syscall`, or a window
toolkit from `internal/app` outside `internal/app/hostenv`. The OS
implementations live in `internal/app/hostenv/oshost`, so a host that is not
an operating system links none of them.

### 1.1 `VolumeStore`

```go
Volumes() []Volume                              // the serving build of each volume
Rescan() ([]string, error)                      // → the volumes whose serving build moved
Install(name string, content io.Reader) (Installed, error)
Location() string                               // a label: where the library lives
```

The registry model is **scan at launch, rescan on import — never watch** (issue
#5 §2, decision 15). A file dropped into the library from outside appears at
the next launch. The fold that decides which build of a volume serves is
`format/bundle`'s and is pure — newest capture, then policy revision, then
stamp, then locator — so a host only ever has to produce descriptors.

`Location()` is a label, not a path the handler takes apart: it is what the
catalog reports as `bundlesDir` and what the empty-library page shows.

### 1.2 `SessionStore`

```go
Load(name string) ([]byte, error)   // hostenv.ErrNoSession when absent
Save(name string, data []byte) error
Names() ([]string, error)
```

Bytes under a name. What the bytes mean is §3 of this document, which is what
lets a file, an OPFS handle, and a test's map all be the same store. Names are
held to `hostenv.ValidName` — lowercase, digits, `-`, `_`, `.`, no separators —
before they reach any store.

### 1.3 `PickFile`

Returns the chosen file as a stream, the name it was chosen under, and an
error. Three answers are distinct and must stay so: a file, `ErrNoSelection`
(the reader cancelled), and `ErrNotAvailable` (this host has no picker). An
import that cannot happen and an import nobody wanted are different things to
say.

---

## 2. Routes

### 2.1 The data plane — byte-compatible with the reference implementation

```
GET /data/catalog.json                                  no-store
GET /data/v/{slug}/{stamp12}/{worlds|tiles|icons}/...   private, max-age=31536000, immutable
```

The seam and the goldens both read this plane, so its shape is not this
rewrite's to change. `golden/http` replays the recorded transcript against it
(see `golden/http/NOTES.md`).

**The catalog** is composed at the moment it is asked for and never cached:

```json
{"volumes":[{"slug":"…","title":"…","stamp":"<64 hex>","base":"/data/v/<slug>/<stamp12>",
             "tileGrid":{…},"worlds":[…]}],
 "bundlesDir":"…"}
```

Volumes are listed **by title**. `worlds` is the manifest's world list
verbatim. `base` carries the twelve-hex short stamp, which is the whole cache
story: the URL names exactly one build, so everything under it is immutable,
and the moment a newer build takes the slug over the old URL is a 404 — which
is the client's cue to refetch the catalog.

**Content** is served out of the archive with a `Content-Length`, a
`Content-Type` from a fixed extension table (`.json .text .bin .jpg .png
.webp .svg`), and nothing else. Everything not admitted by that table, not
under `worlds/`, `tiles/` or `icons/`, or not carrying the serving stamp, is a
plain `404`.

**Byte ranges are not served.** Tiles are stored uncompressed so that they
could be, and issue #5 §2 describes them that way, but the recorded transcript
shows the reference implementation answering a `Range` request `200` with the
whole body and no `Accept-Ranges`. Byte-compatibility with the recording wins;
changing it is a deliberate act with a waiver in the same commit.
`TestContentDoesNotServeRanges` is what keeps it from happening by accident.

### 2.2 The app plane

| Route | Answers |
|---|---|
| `GET /` | 302 to the last volume's world; the library card when nothing is installed. |
| `GET /v/{volume}/{world}` | The whole explorer page, server rendered, in its remembered state. Also records that this is where the reader is. |
| `GET /fragments/detail/{id}?volume=` | One feature's card. |
| `POST /session/{concern}` | The partial set for the regions that concern touches (§4). |
| `POST /bundles/import` | Streamed progress rows; picks, installs, rescans, announces. |
| `GET /events?volume=` | The SSE stream (§5). |
| `GET /static/{path...}` | Whatever static tree the host mounted; `404` when it mounted none. |

Real URLs replace the reference implementation's hash routing: a world can be
bookmarked, reloaded, and linked to. `/` is a doorway, not a second name for
the explorer.

A `GET` of an explorer page writes the session's `world` and the last-volume
pointer. That is deliberate: arriving at a URL is a choice whether it was
clicked, typed, or restored by the browser, and the session follows the
address bar rather than the other way round.

---

## 3. The session record

**The state-placement rule (issue #5 §4.1):** discrete application state lives
on the server and in URLs; continuous interaction state lives in the seam. The
camera is the single, deliberate exception — it reports upward once per settle,
so a volume reopens where the reader left it.

One record per volume, named `volume.<slug>.json`, plus `app.json` holding the
last volume. `SessionSchema` versions the shape; a record written under a
schema this build does not know is passed over rather than half-read, which
costs a reader their layout once and never corrupts it.

```json
{
  "schema": 1,
  "volume": "tunic",
  "stamp": "13d5657ed903",
  "world": "world",
  "lens": "Default",
  "hidden":    ["12", "31"],
  "collapsed": ["quests"],
  "labels":    {"31": "always"},
  "solo":      ["hydro"],
  "search":    "shrine",
  "dock":     {"open": true, "section": "counts"},
  "detail":   {"open": true},
  "grid":     {"system": "geohash", "cell": "9q5c", "subgrid": 2},
  "sidebar":  {"open": true},
  "selected": "1849",
  "cameras":  {"world": {"x": 4096, "y": 4096, "zoom": 3.5, "rotation": 0, "at": "2026-08-02T09:12:44Z"}},
  "updatedAt": "2026-08-02T09:12:44Z"
}
```

Notes that are contract, not style:

- `hidden`, `collapsed` and `solo` are **sorted sets**, so a record is stable
  to diff and two paths to the same state produce the same bytes.
- `stamp` is the serving build the record was last written against. A record
  behind the serving build is still read — slugs outlive builds — and the
  difference is what a stamp-move refresh is about.
- `cameras` is keyed by world slug, because a camera belongs to a ground.
- The server stores the camera and hands it back. It never reasons about it.

---

## 4. The partial envelope

Every `/session/*` POST answers with an `<hx-partial>` set covering **exactly**
the regions that interaction touches — never a page-wide refresh standing in
for knowing what moved.

```html
<hx-partial target="#atlas-legend" swap="innerMorph">…</hx-partial>
<hx-partial target="#atlas-dock"   swap="innerMorph">…</hx-partial>
```

| Region | Element | Swap |
|---|---|---|
| `shell` | `#atlas-shell` | `innerMorph` |
| `topbar` | `#atlas-topbar` | `innerMorph` |
| `legend` | `#atlas-legend` | `innerMorph` |
| `dock` | `#atlas-dock` | `innerMorph` |
| `detail` | `#atlas-detail` | `innerMorph` |
| `grid-navigator` | `#atlas-grid-navigator` | `innerMorph` |
| `overview` | `#atlas-overview` | `innerMorph` |
| `viewport` | `#atlas-viewport-state` | `outerMorph` |
| `empty-state` | `#atlas-shell` | `innerMorph` |
| `import` | `#atlas-import` | `beforeend` |

Morph swaps are what let scroll position, focus, and open `<details>` survive a
re-render. **The viewport's own elements are morph-skipped**: a swap replaces
the inert `#atlas-viewport-state` node beside them and never reaches inside
`<atlas-viewport>`, because tearing down a WebGL context mid-gesture is not a
re-render.

### 4.1 The concern table

One route per concern, each declaring the regions its answer covers.

| `POST /session/…` | Fields | Regions |
|---|---|---|
| `world` | `world` | topbar, legend, dock, overview, viewport |
| `lens` | `lens` | topbar, viewport |
| `collections` | `collection`+`visible`, or `hidden` (repeated) | legend, dock, viewport |
| `sections` | `section`, `open` | legend |
| `labels` | `collection`, `policy` (empty clears) | legend, viewport |
| `solo` | `domain`+`on`, or `solo` (repeated) | topbar, legend, viewport |
| `search` | `q` | legend, dock, viewport |
| `dock` | `open`, `section` | dock |
| `select` | `feature` | detail, dock, viewport |
| `grid` | `system`, `cell`, `subgrid` | grid-navigator, dock, viewport |
| `view` | `world`, `x`, `y`, `zoom`, `rotation` | — answers `204` |
| `sidebar` | `open` | shell |

Every request carries `volume`. A volume that is not installed is a `404`; a
malformed slug or a missing required field is a `400`.

`view` is the camera report: the seam's one debounced upward whisper, answered
with `204 No Content`, because swapping anything in response to a settling
camera would fight the reader's own hand. The other upward flow is the pick:
the seam resolves a canvas hit and submits the *identity* through an ordinary
`POST /session/select`.

---

## 5. The events stream

`GET /events?volume={slug}` is Server-Sent Events. The connection opens with a
`: atlas` comment, so a client knows it is live rather than merely accepted.
Two event names, and no more:

```
event: catalog
data: <hx-partial target="#atlas-topbar" swap="innerMorph">…</hx-partial>

event: refresh
data: <hx-partial target="#atlas-shell" swap="innerMorph" src="/v/tunic/world"></hx-partial>
```

- **`catalog`** — the library's composition moved. Carries the regions that
  list it: the volume selector, and the library card when the library has
  emptied. Sent to every connection.
- **`refresh`** — the one directive. The volume this connection is watching now
  serves a different build, every URL under its old stamp is gone, and the page
  has to be fetched whole rather than patched. Sent **only** to connections
  watching that volume, which is why a connection names what it is watching.

There is no event for "a file appeared in the library directory", because
nothing watches it. An import is what triggers a rescan, and the rescan is what
produces these events.

---

## 6. Logging

`internal/logging`, per [`logging.md`](logging.md). The application speaks
`op=` values `scan`, `serve`, `install`, `session`, `events`, `catalog`,
`render`, and uses `volume`, `world`, `stamp` and `path` from the shared
vocabulary. `atlas serve` prints its address to **stdout** — a script starts
the host and reads where it landed — and everything else to the stream on
stderr.

---

## 7. Next wave

Named here so a stub is never mistaken for a decision.

- **Templates.** `internal/app/templates` holds one file per region, and today
  each is a container with the right id and the data already flowing into it.
  The chrome, the legend tree, the label ladders and the dock readout are the
  templates wave. What must not drift is the *names*: region names, element
  ids, and the swap table of §4.
- **Display logic in Go.** Legend algebra, AND-across/OR-within filtering,
  label policy and semconv reading all run in Go before a template is handed
  anything (issue #5 §4.5). None of it exists yet; when it does, it belongs
  beside `view.go`, not in a template.
- **The Wails host.** ~150 lines: `wails.Run` with this handler as the asset
  server, an `fs.Sub` mount for the seam bundle, and the native dialog behind
  `PickFile`. No Wails runtime JS in the page; events are SSE.
- **`atlas dev`.** Template hot-reload, headless HTTP, and the seam's
  `esbuild --watch` under one command.
- **Diagnostics for the parity tour.** The tour compares server session state
  as a JSON island against seam state, under the golden key names. The island
  is not rendered yet.
- **Detail fragments.** `/fragments/detail/{id}` answers with the region stub;
  reading prose, links and attributes out of `worlds/<slug>.text` is the
  templates wave's.
