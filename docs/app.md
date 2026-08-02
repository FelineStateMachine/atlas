# The application

**Status: built (M5, with the desktop host landing in M7).** This document
specifies the hypermedia application: the URL surface, the session record, the
partial and event envelopes, the region and template system, the state island,
and the contract between the handler and whatever host it is mounted in. §11
is the standing list of what is named rather than built, so nobody mistakes a
placeholder for a decision.

The implementation is `internal/app`, its host seam is
`internal/app/hostenv`, and the two hosts are the desktop shell at the module
root (`main.go`) and `atlas serve` (`cmd/atlas/serve.go`). Where this document
and the code disagree, take it as a defect in one of them and say so.

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
| **Wails webview** | built | The desktop window; `PickFile` is the native dialog; the library is the application's data directory. `main.go` + `redirects.go` + `hostenv/wailshost`. |
| **`atlas serve`** | built | Plain HTTP, no window. `PickFile` refuses with `ErrNotAvailable`. The dev loop, CI, and the parity harness. |
| **WASM service worker** | not scheduled | Go compiled to `js/wasm`; the stores back onto OPFS. Nothing is built for it; the discipline is what keeps it reachable. |

The rule is enforced mechanically: `golden/depcheck`'s `hostenv` analyzer
fails any import of `os`, `os/exec`, `path/filepath`, `syscall`, or a window
toolkit from `internal/app` outside `internal/app/hostenv`. The OS
implementations live in `internal/app/hostenv/oshost`, so a host that is not
an operating system links none of them. The two host entries — `cmd/atlas` and
the desktop shell at the module root — are where the machine is *supposed* to
be reached, and are exempt by sitting outside the lane the rule is written
about; depcheck's scope carries the root package for the rest of its rules.

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
say — and one of the two things to say is nothing. A host with no picker is
refused out loud, because a reader who pressed the button is owed a reason; a
reader who closed their own dialog is told nothing at all, and the row that
was standing by is taken away again (§4.1).

### 1.4 The desktop shell

The shell at the module root is host wiring and nothing else (issue #5 §3.4).
It resolves two directories, opens an `oshost` over them with a native picker
attached, and hands the handler to `wails.Run` as the asset server:

```go
host, _ := oshost.New(oshost.Options{
    BundlesDir: library, SessionsDir: sessions, Pick: window.Pick,
})
wails.Run(&options.App{
    AssetServer: &assetserver.Options{Handler: followRedirects(handler)},
    OnStartup:   window.Opened,
})
```

Four things about it are worth knowing.

**Where the library is.** `$ATLAS_DATA_DIR`, else `os.UserConfigDir()` +
`dev.felinestatemachine.atlas` — the identifier the pre-rewrite shell used, so
a reader's existing library is found where it always was. `bundles/` under it
holds the volumes and `sessions/` the session records. `$ATLAS_BUNDLES_DIR`
moves the library alone, which is what a development run points at a freshly
composed `dist/bundles`. The headless host reads the same two variables
(`cmd/atlas/serve.go`).

**Where the seam is.** `//go:embed static` — the root `static/` directory,
holding the seam's built bundle as `app.js`. `make static` puts the same bytes
in `dist/static`, which is what a `-static` mount is pointed at; the shell
embeds its copy, so the shipped application is one file with no sidecars. A
build that skipped `make static` embeds only that directory's README:
`/static/app.js` answers `404`, `<atlas-viewport>` renders nothing, and
everything else works. The stylesheet system is not there — it is
`internal/app/assets`, embedded by the application itself and served from
`/assets`, so deleting the seam costs a page one script tag, not its chrome.

**Redirects are followed by the host.** A Wails page is served over a custom
URL scheme, and a scheme task has no way to express a redirect: WebKit hands
the `302` to the page as if it were a document, and the reader is looking at
the two words Go writes in a redirect body. `redirects.go` walks the
application's own doorways instead — GET only, `Location` values naming a path
only, five hops at most, streaming answers passed straight through — so the
handler goes on redirecting and `atlas serve` goes on serving those redirects
to clients that follow them. The HTTP goldens judge what the handler sends,
which is unchanged.

**No Wails runtime JavaScript in the page.** Nothing is bound, no bindings
JSON is generated, and the page hears that the library moved over the
application's own SSE stream (§5). Wails' asset server injects its runtime into
a `200 text/html` answer at a path ending in `/`; this application answers `/`
with a redirect whenever it has a library at all, so the only page that can
ever carry the injection is the empty-library doorway, which uses none of it.
Every explorer page is at `/v/…` and is served exactly as the headless host
serves it.

The build is a plain `go build -tags "desktop,production"`, not `wails build`:
the Wails CLI's job is scaffolding and driving a Vite frontend, and this tree
serves its own pages — which is also why there is no `wails.json`. `make
desktop` is the macOS recipe, and `.github/workflows/release.yml` carries all
three platforms.

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
| `GET /open?volume=&world=` | 302 to the explorer page those two name. The doorway the topbar's two selects go through. |
| `GET /v/{volume}/{world}` | The whole explorer page, server rendered, in its remembered state. Also records that this is where the reader is. |
| `GET /fragments/detail/{id}?volume=` | One feature's card. |
| `POST /session/{concern}` | The partial set for the regions that concern touches (§4). |
| `POST /bundles/import` | One progress row, streamed through its states (§4.1); picks, installs, rescans, announces. |
| `GET /events?volume=` | The SSE stream (§5). |
| `GET /assets/{app.css,htmx.js}` | The application's own chrome, out of the binary. |
| `GET /static/{path...}` | Whatever static tree the host mounted; `404` when it mounted none. |

Real URLs replace the reference implementation's hash routing: a world can be
bookmarked, reloaded, and linked to. `/` is a doorway, not a second name for
the explorer.

A `GET` of an explorer page writes the session's `world` and the last-volume
pointer. That is deliberate: arriving at a URL is a choice whether it was
clicked, typed, or restored by the browser, and the session follows the
address bar rather than the other way round. Arriving on a *different* world
also clears what belonged to the old one — the selection, the highlights, the
search — and re-derives the arrangement (§3.1).

**`/open` exists because a `<select>` cannot build a path out of its own
value.** The volume and the world are both in the address, so the two crumbs
name what they want and the server redirects to it; after the redirect the
reader is at the real URL, which is the only place the explorer lives. It is
one route rather than client-side glue, which is the same trade the rest of
this document makes.

**`/assets` is a different mount from `/static`, and the difference is the
deletability principle in the URL space.** `/static` is whatever tree a host
handed over — the seam's built bundle, which `make static` puts where a
`-static` mount wants it ([render-seam.md](render-seam.md) §9) — and answers
`404` when a host handed over nothing. `/assets` is the part of the page that is the
application's own: the stylesheet system and the vendored hypermedia runtime,
compiled into the binary (`internal/app/assets`). A build with no seam serves
a complete, styled, interactive page.

Both assets are vendored rather than linked. The offline invariant is not only
about bundles: an atlas opens on a machine with no network, forever, and a
page that fetches its runtime from a CDN is a page that does not.

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
  "schema": 2,
  "volume": "bend-or",
  "stamp": "3610a0f10798",
  "world": "2026-08-02",
  "lens": "Basemap",
  "hidden":      ["1496244488"],
  "collapsed":   ["zones"],
  "expanded":    ["1951802496", "253393030"],
  "highlighted": ["277390785"],
  "arranged":    true,
  "labels":      {"39191589": "quiet"},
  "search":      "mill",
  "dock":     {"open": true, "dismissed": false, "section": "counts"},
  "detail":   {"open": true},
  "grid":     {"system": "geohash", "cell": "9q5c", "subgrid": 2},
  "sidebar":  {"collapsed": false},
  "overview": {"docked": false},
  "selected": "277390785",
  "cameras":  {"2026-08-02": {"x": 4096, "y": -4096, "zoom": 1.34, "rotation": 0, "at": "2026-08-02T09:12:44Z"}},
  "updatedAt": "2026-08-02T09:12:44Z"
}
```

Notes that are contract, not style:

- `hidden`, `collapsed`, `expanded` and `highlighted` are **sorted sets**, so a
  record is stable to diff and two paths to the same state produce the same
  bytes. They are stored as strings — collection and feature ids ride the DOM
  as strings — and the island writes them back as the numbers the payload
  declares (§6).
- `lens` is the lens's **name**, not its index. A name outlives a build's
  ordering; the island renders the index because that is what the golden
  baselines record.
- `focused` is the ground the reader last *went to* from a list, and it is not
  `selected`: closing the card puts the selection down and leaves the feature
  index still marking where the reader has been. It is set only by a row that
  says `focus=1` — a feature index row, a panel row, a link inside a card —
  because a pick off the canvas is a reader who was already there, and it is
  cleared when the ground itself is rebuilt: a world opened, a split sheet's
  layer swapped, the volume come back to from another one.
- `sidebar` and `overview` are spelled as the thing a reader *does* to them —
  collapsed, docked — so the zero value is the ordinary page.
- `dock.dismissed` is the reader having folded the panel by hand. Until they
  have, the panel comes out on its own the first time it has something to say;
  after that it stays where they put it.
- `stamp` is the serving build the record was last written against. A record
  behind the serving build is still read — slugs outlive builds — and the
  difference is what a stamp-move refresh is about.
- `cameras` is keyed by world slug, because a camera belongs to a ground. The
  server stores it and hands it back. It never reasons about it.

**Schema 2** is the templates wave. It gained `expanded`, `highlighted`,
`arranged` and `overview`, renamed `sidebar.open` to `sidebar.collapsed`, and
lost `solo`: isolating turned out to be a move on the hide set rather than a
state of its own, and the chip is derived from what is hidden so that it is
right however that set was reached — including by switching rows off one at a
time.

### 3.1 The arrangement a world opens with

Three of the sets have non-empty defaults that the *world* supplies, not the
reader:

| Set | Default | Why |
|---|---|---|
| `hidden` | every collection the payload marks `visible: false` | the producer's own curation |
| `collapsed` | `["zones"]` | shape collections are a navigation aid, not the primary filter surface, so their section folds and pin groups stay above the fold |
| `expanded` | every ungrouped shape collection | their feature indexes are there the moment the section is opened, and in the DOM for anything reaching for a feature without unfolding first |

`arranged` is what tells a fresh record from an arranged one. Without it an
empty hide set — a reader who asked to see everything — is the same bytes as a
record nobody has touched, and the next request would put the curation back.

---

## 4. Regions, templates, and the partial envelope

### 4.1 The regions

Eleven regions, one template file each (`internal/app/templates/<region>.tmpl`),
mirroring the one-file-per-region stylesheet system in
`internal/app/assets/css`. A region's template renders that region's own
container, so a first paint and a swap produce the same bytes.

| Region | Element | Swap | What it is |
|---|---|---|---|
| `shell` | `#atlas-shell` | `outerMorph` | the whole page frame |
| `topbar` | `#atlas-topbar` | `innerMorph` | volume, world, lens, import |
| `legend` | `#atlas-legend` | `outerMorph` | search field, toolbar, tree, footer count |
| `dock` | `#atlas-dock` | `outerMorph` | count, flag, shortlist, and the card inside it |
| `detail` | `#atlas-detail` | `outerMorph` | one feature's card |
| `grid-navigator` | `#atlas-grid-navigator` | `outerMorph` | cell system, held cell, subgrid |
| `overview` | `#atlas-overview` | `outerMorph` | the corner locator's chrome |
| `viewport` | `#atlas-viewport-state` | `outerMorph` | the inert state node the seam reads |
| `island` | `#atlas-session-island` | `outerHTML` | the session record the seam reads, as JSON |
| `empty-state` | `#atlas-shell` | `innerMorph` | the library card |
| `import` | `#atlas-import` | `outerMorph` | the import that is happening, as one row |

**Nearly every region is an *outer* morph**, and that is the reading that
matches what the templates render: the element is morphed onto itself, which
keeps the first-paint bytes and the swap bytes identical and is the only swap
that can set an attribute on the region's own container. Three states are
spelled exactly that way — the sidebar's collapsed class on the shell, the
navigator's `hidden`, the detail card's `hidden`. The exceptions each have a
reason: the topbar's container carries nothing a swap moves; the island is a
`<script>` node with nothing inside worth preserving, so it is replaced rather
than morphed, because morphing one leaves its text where it was.
`internal/app/partials.go` is the table this one mirrors.

**An import is one row, not a log.** The response is a stream — a hundred
megabytes takes long enough that silence reads as failure — but it is a stream
of *states of the same row*: the handler renders the whole region again, and
flushes it, for every state a run reaches, and the morph carries one row from
"choosing a bundle" to "installed" in place. The row's element id carries the
run's number, so a second import is a different element rather than the first
one's leftovers. Three things follow, and they are the point of the
arrangement: a cancelled picker is **silent** (its last render holds no row at
all, and the section renders itself `hidden`), a run that ended well marks its
last row and the stylesheet fades it out on a delay, and a refusal stays until
the next import replaces it — it is the only account of what went wrong, and
nothing else on the page will say it. An append swap, which is what this was,
made every state of every run a line of its own that outlived the run, the
import, and everything short of a full page render.

**The legend is one region because it is one answer.** Every filtering move
changes the rows, the isolate chip and the footer count together; three
regions would be three ways for them to disagree, which is the exact defect
the reference implementation carried until every surface was made to ask one
place.

**The card is nested inside the dock and is still its own region.** That is
where it belongs on screen, and `select` moves the card and the list together,
so a dock render carries the card with it and the two cannot disagree about
what is open.

`viewport.tmpl` defines two templates. `viewport` is the partial — the state
node alone, because that node is what an interaction moves. `viewport-surface`
is the shell's alone and is never swapped.

### 4.2 The envelope

Every `/session/*` POST answers with an `<hx-partial>` set covering **exactly**
the regions that interaction touches — never a page-wide refresh standing in
for knowing what moved.

```html
<hx-partial hx-target="#atlas-legend" hx-swap="innerMorph">…</hx-partial>
<hx-partial hx-target="#atlas-dock"   hx-swap="innerMorph">…</hx-partial>
```

The target and swap are spelled `hx-target` and `hx-swap`: htmx 4 reads an
`<hx-partial>` through the same attribute vocabulary it reads everything else
through, and an unprefixed `target` is silently no target at all. The region
names and the element ids either side of it are the same as they ever were.

Morph swaps are what let scroll position, focus, and open `<details>` survive a
re-render. **The seam's surfaces are morph-skipped**: `<atlas-viewport>`, the
map pane and the overview's canvas holder carry `hx-morph-skip-children`, htmx
4's own spelling of "touch my attributes, never my internals". Tearing down a
WebGL context mid-gesture is not a re-render.

### 4.3 The concern table

One route per concern, each declaring the regions its answer covers. Reading
the regions column downward is the fastest way to see what the application
thinks is coupled to what.

| `POST /session/…` | Fields | Regions |
|---|---|---|
| `world` | `world` | topbar, legend, dock, overview, viewport |
| `lens` | `lens` (the lens's name) | topbar, legend, dock, overview, viewport |
| `collections` | `collection`+`visible`, or `section`, or `all`=`show`\|`hide`, or `hidden` (repeated) | legend, dock, viewport |
| `sections` | `section`+`open`, or `all`=`fold`\|`unfold` | legend |
| `expand` | `collection`+`open`, or `all`=`fold`\|`unfold` | legend |
| `labels` | `collection` + `flip`, or `collection`+`policy` (empty clears) | legend, viewport |
| `solo` | `collection` or `section`; neither means show everything | legend, dock, viewport |
| `search` | `q` | legend, dock, viewport |
| `highlight` | `feature` (+ optional `on`), or `all`=`clear` | legend, dock, viewport |
| `dock` | `open`, `byHand`, `section` | dock |
| `select` | `feature` (empty closes), `focus` | legend, dock, detail, viewport |
| `grid` | `system`, `cell`, `subgrid` | grid-navigator, dock, viewport |
| `overview` | `docked` | overview |
| `sidebar` | `open` | shell |
| `view` | `world`, `x`, `y`, `zoom`, `rotation` | — answers `204` |

Every request carries `volume`, declared once on the shell as
`hx-vals:inherited`. A volume that is not installed is a `404`; a malformed
slug or a missing required field is a `400`.

`view` is the camera report: the seam's one debounced upward whisper, answered
with `204 No Content`, because swapping anything in response to a settling
camera would fight the reader's own hand. The other upward flow is the pick:
the seam resolves a canvas hit and submits the *identity* through an ordinary
`POST /session/select`.

**`labels` is spelled as a flip, not a destination.** The policy turns over,
and if the other word is what the producer curated anyway the override has
nothing left to say and is dropped rather than stored. That is what keeps a
ladder turned over and back from leaving overrides behind it — and it is why
the route needs the world, not just the id.

### 4.4 Template rules

- **One file per region**, named for the region.
- **No `hx-on`.** Every interaction is an `hx-*` attribute naming a route. The
  only glue on the page is the seam's boot module.
- **Explicit inheritance.** `hx-vals:inherited` and `hx-swap:inherited` are
  declared once, on the shell, and nothing else inherits.
- **Templates render; they do not decide.** Every display decision runs in Go
  first (§7). A template that needs an `if` about what a collection *means* is
  a decision in the wrong place.
- **`hx-*` lives in templates only.** The routes, the region names, the partial
  envelope and the viewport state node are all framework-neutral, so replacing
  HTMX would be an afternoon in one directory rather than an architecture
  event.

---

## 5. The events stream

`GET /events?volume={slug}` is Server-Sent Events. The connection opens with a
`: atlas` comment, so a client knows it is live rather than merely accepted.
Two event names, and no more:

```
event: catalog
data: <hx-partial hx-target="#atlas-topbar" hx-swap="innerMorph">…</hx-partial>

event: refresh
data: <hx-partial hx-target="#atlas-shell" hx-swap="innerMorph" hx-get="/v/tunic/world"></hx-partial>
```

- **`catalog`** — the library's composition moved. Carries the regions that
  list it. Sent to every connection.
- **`refresh`** — the one directive. The volume this connection is watching now
  serves a different build, every URL under its old stamp is gone, and the page
  has to be fetched whole rather than patched. Sent **only** to connections
  watching that volume.

The page takes the plainer road of re-fetching its own URL on either event: a
page that re-reads where it is cannot end up half-patched, and both events mean
the library moved under it. The bodies stay in the contract for a client that
wants the cheaper path.

There is no event for "a file appeared in the library directory", because
nothing watches it. An import is what triggers a rescan, and the rescan is what
produces these events.

---

## 6. The state island

The explorer page carries the session it was rendered from as an inert JSON
script node:

```html
<script type="application/json" id="atlas-session-island">{…}</script>
```

It exists for one reason. Issue #5 §6 asks the rewritten application to publish
"server session state as a JSON island … matching golden key names", so the
parity tour can diff the application's account of the arrangement against the
seam's. **These are the key names** — the ones the reference implementation
wrote to `localStorage` under `atlas.session.v3`, documented in
`golden/parity/SCHEMA.md` §3.2. Where the arrangement is stored is this
application's business and has changed completely; what it contains is not.

```json
{"last": "bend-or",
 "entry": {"volume": "bend-or", "world": "2026-08-02", "lens": 0,
           "center": [4096, -4096], "zoom": 1.34,
           "hidden": [], "collapsed": ["zones"],
           "expanded": [39191589, 50985093, …],
           "labels": ["39191589=quiet"],
           "overviewDocked": false, "dockFolded": true, "dockDismissed": false}}
```

Shape notes: ids are emitted as the numbers a payload declares them as but
sorted as the strings they ride the DOM as, which is what the baselines record;
`labels` is the override ledger, one `"<collection>=<policy>"` per entry,
sorted; `lens` is the index; `center` is rounded to whole world units and
`zoom` to three decimals, exactly as the harness rounds what it reads, so a
baseline and an island are diffable without a normalizer in between.

`golden/island` is the gate. It drives session POST sequences derived from the
baselines — twenty-seven steps across all six fixture volumes, over the legend,
solo, search, label-policy, lens, grid and overview concerns — and holds the
island to each step's `session` object key for key, in both directions: a key
the baseline has and the island lacks is a hole, and a key the island invents
is an invention.

### 6.1 What is seam-side, and why

Two of the twelve keys **cannot be produced server-side**, and are not forced
to be:

- **`center`** and **`zoom`** are the chart's camera. They are arrived at by
  fitting a raster to a window and depend on the viewport's size, the lens's
  own depth, and the fit the seam computes. No amount of session state produces
  them. The server's only honest relationship with them is the one issue #5
  §4.1 describes: the seam reports a settled camera upward, debounced, at
  `POST /session/view`, and the server stores it and hands it back. Until the
  seam has reported one they are `null`. The gate posts the baseline's own
  camera and then checks the round trip, so what is proved about these two is
  the echo, not the origin.

**The grid cull is not one of them, and was for a while.** A held cell narrows
what stands exactly the way a highlight does, and the count above the panel has
to be the count the map is drawing — so the server has to answer "is this
feature inside the held cell" without asking the seam, which the third upward
flow §5 of [render-seam.md](render-seam.md) forbids would be the only other
way. `internal/app/cells` is the smallest piece of one system's arithmetic that
answers it: the recursive halving and nothing else, gated against the cell
extents every parity baseline records. The cost — a second copy of arithmetic
`analysis/cellsystems` also holds — is written down in that package's own
comment, and the file is the first thing a Go twin of the analysis lane would
delete.

One thing still belongs to a lane, and is recorded here rather than papered
over:

- **The footer's "in view" half.** The reference implementation's footer read
  "N of M features in view", and N is the count inside the camera's extent —
  seam-side by construction. The server renders M; refining it to the full
  sentence is the seam's, once there is a camera to ask.

---

## 7. Display logic

All of it runs in Go, once, before a template is handed anything (issue #5
§4.5). It lives in four files beside the handler, and reads the conventions
only through `format/semconv` — an `atlas.*` string literal outside the
registry is a depcheck failure, not a review comment.

| File | What it decides |
|---|---|
| `world.go` | the payload model: collections in order, points unpacked from `ATLASLOC`, shape rings projected onto the world square, the parent chain, containment |
| `legend.go` | the tree — sections, rows, counts, the label ladder, the feature index order, isolating, the solo chip, the opening arrangement |
| `filter.go` | what stands: the hide set, the search, the AND-across/OR-within highlight filter, the shard, the counts and the words above them |
| `view.go` | the card, the dock, the grid and overview chrome, the viewport state node |

Three rules worth stating because they are easy to get subtly wrong, all three
extracted from the reference implementation's behaviour:

1. **Highlighting reads AND across collections, OR within one.** Two districts
   highlighted widens the question; a district and a subwatershed narrows it to
   the ground they share. The city fixture is the test bed: nine shape
   collections, and highlighting one boundary plus one waterbody takes the
   drawn count from 213 to 148, because none of the 65 annotated places stands
   in both. Two answers are exempt from the cull — the feature the reader has
   open, and one they are searching for by name — because both were asked for
   rather than merely drawn.
2. **A search narrows points and never the ground.** Searching for a place has
   never taken the ground out from under it, so a shape answers the search in
   the list and not on the canvas.
3. **Containment is boundary-inclusive with a pixel of grace.** A pin dropped
   on a zone's border was put there to mean the zone, and exact point-in-polygon
   arithmetic would flip it out over the width of the line it stands on.

The label ladder is the one place a convention helper had to move. The
reference implementation gave a point collection curated as `atlas.render.as =
text` a speaking default, because floating names are labels a producer pinned
on rather than a different kind of thing to draw; `semconv.LabelPolicy` now
says the same, so the rule lives in the registry rather than in a viewer.

### 7.1 The stylesheet system

`internal/app/assets/css` is the reference implementation's token-first,
one-file-per-region system, carried as an asset (issue #5 §9) and unedited. The
whole difference between it and this application's markup lives in one file,
`chrome.css`: the region containers carry ids of their own, the seam's custom
elements need somewhere to stand, and the import row is new — including the
delayed fade that takes it away when a run ended well, which is an animation
and not a script. The list is shorter than it was: the detail card used to be
patched here, keyed off `:empty`, because a region rendered inside-out could
not be given an attribute; it is an outerMorph region now (§4.1) and renders
its own `hidden`, so the carried `pin-detail.css` works verbatim and there is
nothing to say. The visual identity is unchanged — neutral dark chrome,
palette as accents.

---

## 8. The development loop

```sh
atlas dev                       # 127.0.0.1:7433, chrome read from the working copy
atlas dev -bundles DIR -root .  # against another library
atlas dev -seam-watch           # and the seam's bundler beside it
```

`atlas dev` is `atlas serve` with two additions and no third. Templates and
stylesheets are read from the working copy instead of the binary and re-parsed
the moment a file is written, so a template edit is one refresh away rather
than one rebuild away; writes are coalesced over 60 ms, because editors write a
file three times per save. A tree that will not parse is reported and the last
set that parsed keeps serving — an unbalanced `{{if}}` halfway through an edit
is a normal thing to type, and taking the page down over it would make the loop
worse than a rebuild.

**The watching lives in `cmd/`, and nowhere else.** `internal/app` is one pure
`http.Handler` that touches no filesystem; what it exposes is
`templates.Reload(fs.FS)` and `assets.Reload(fs.FS)`, and building an `fs.FS`
out of a directory is the command's business. That is the hostenv rule
surviving having a development loop.

`-seam-watch` runs `npm run watch` in `render/` beside the server, streaming
the bundler's output into the same event stream. The seam landed in M6, so the
flag does its work; a tree with no `render/package.json` — the deletability
principle exercised — is told so at `info` and the server carries on, because a
missing seam is a served page with one script tag fewer, not a failed run. The
flag's own usage line still describes the stub it was: a defect in the code,
not in this paragraph.

---

## 9. Logging

`internal/logging`, per [`logging.md`](logging.md). The application speaks
`op=` values `scan`, `serve`, `install`, `session`, `events`, `catalog`,
`render`, and uses `volume`, `world`, `stamp` and `path` from the shared
vocabulary. `atlas serve` and `atlas dev` print their address to **stdout** — a
script starts the host and reads where it landed — and everything else to the
stream on stderr.

---

## 10. The pragmatism clause

Issue #5 §4.3: HTMX ownership is a means, not a loyalty test. If an interaction
proves laggy or awkward as a round trip, it is re-classified by editing the
issue's interaction inventory — never patched with ad-hoc client code. Three
things are worth watching, recorded here so the edit has evidence behind it
rather than a feeling:

1. **Search-as-you-type.** Debounced 150 ms on the client attribute and
   answered with three region partials. On the city (213 features) and the
   games (up to 1,400) the round trip is comfortable; on a volume an order of
   magnitude larger the legend partial is the expensive half, because it
   re-renders every row to move one count. If it ever bites, the fix inside
   this architecture is a narrower region — a `legend-count` region — not a
   client-side filter.
2. **The feature index.** A shape row's index is rendered whether or not the
   row is unfolded, which is what the reference implementation did and what
   keeps a feature reachable by name without unfolding first. It is also the
   largest thing in the legend partial.
3. **The grid navigator's text field.** Typing a cell address round-trips per
   keystroke like the search. It is a short string and a cheap region, but it
   is the interaction most likely to feel like typing through a straw, and it
   is the first candidate for the hybrid bucket if it does.

None of the three has been re-classified. They are listed because the clause is
worth nothing without a habit of writing down what it would apply to.

---

## 11. What landed, and what is still named rather than built

Named here so a stub is never mistaken for a decision — and so that a wave
which lands moves out of the list rather than sitting in it as folklore.

**Landed since this section was written:**

- **The Wails host** (§1.4). `wails.Run` with this handler as the asset server,
  the seam's built tree embedded and mounted at `/static`, and the native
  dialog behind `PickFile`. No Wails runtime JS in the page; events are SSE.
  The one thing the plan did not anticipate is that the webview's transport
  cannot carry a redirect, so the host follows the application's own doorways
  itself.
- **The seam.** `render/` landed in M6. A build without it still serves:
  `/static` answers `404`, an undefined `<atlas-viewport>` renders nothing, and
  every non-viewport interaction works — the deletability principle,
  demonstrated in the build order and again in the shipping binary. The M7
  close-out walked it: with `render/` removed from the working copy,
  `go build ./... && go test ./...` and `golden/depcheck` stay green, the page
  serves whole, and search, the legend, solo, sections and the dock all answer.
- **The grid cull** (§6.1). `internal/app/cells` answers it server-side, which
  is what lets the panel's count and the map's drawing be one number.

**Still named rather than built:**

- **The WASM service-worker host.** §1's third row. Nothing is built for it;
  the hostenv discipline is what keeps it reachable.
- **The footer's "in view" half.** §6.1.

The seam's own list of what it has not proven is `docs/render-seam.md` §10, and
it is the one to read for anything below the `<atlas-viewport>` boundary.
