# The workbench

**Status: built (M4).** This document specifies the workbench: the pages it
serves, where their facts come from, the operation-runner library, the safety
properties operations are held to, and the feature-by-feature account of the
reference implementation's `cmd/cartograph` that this milestone's exit criterion
asks for.

The implementation is `internal/workbench`, the runner is
`internal/workbench/oprunner`, and the host is `atlas workbench`
(`cmd/atlas/workbench.go`). Where this document and the code disagree, take it
as a defect in one of them and say so.

The [application](app.md) serves the build of every volume a reader should
have and asks no questions. The workbench answers the questions: what each build
is worth, what moved between two of them, what the collection owes the people
whose work it carries, and what the pipeline should do next.

---

## 1. Running it

```
atlas workbench [-addr 127.0.0.1:6180] [-bundles DIR] [-archive DIR] [-tiles DIR]
```

| Flag | What it is | Without it |
|---|---|---|
| `-addr` | Where the workbench listens. | `127.0.0.1:6180`. |
| `-bundles` | The registry of `.atlas` files every measurement page is about. | The application's own library. |
| `-archive` | The capture archive root operations read and write. | Crawl, tiles, compose and enrich say they cannot run. |
| `-tiles` | The derived tile set directory. | Tiles, compose and enrich say they cannot run. |

The listening URL is printed on stdout as product output; the event stream goes
to stderr like every other subcommand ([logging](logging.md)).

Reading the library is free and happens on every page load. **Nothing runs on a
schedule or at startup.** An operation exists only between a submitted form and
its subprocess's exit.

## 2. The pages

| Route | Page | What it says |
|---|---|---|
| `GET /` | Library | Every volume, headlined by its serving build's score, with the movement against the build before it. Bundles that will not measure are listed as warnings rather than failing the page. |
| `GET /volume/{slug}` | Measurement | The serving score and how it moved; then every build in full — per-world score breakdown, the five absolute axes as diagnostics, and the whole of every ledger. |
| `GET /volume/{slug}/diff?a=&b=` | Diff | Two builds side by side, headlined by the score delta and its verdict; per-world deltas, axis deltas, the features added and removed, and matched-pair stability. |
| `GET /sources` | Sources | One card per capture source: licence, attribution, id space, and whether it is crawled from here. |
| `GET /operations` | Operations | What may be run, what each operation needs, and what this workbench was pointed at. |
| `POST /operations/run` | — | One operation, streamed back as HTML rows. |
| `GET /assets/{path...}` | — | `workbench.css`, and `htmx.js` when a host handed a runtime over. |

**Measurement first.** Every page leads with the score, because the score is the
only number anything gates on ([enrich](enrich.md), the monotonicity gate). The
five axes the reference tree measured — annotation, cartography, structure,
icons, conventions — are printed under it as diagnostics and are labelled as
such on the page. Percentages survive there and nowhere else.

### The score and its movement

A volume's movement is `maturity.Compare(previous, serving)`: the same
comparison the build gate reads, printed rather than enforced. Three things
come with it and are all rendered:

- **Comparability.** Two builds scored under different point-table versions
  conclude nothing, and the page says so instead of showing a delta.
- **The allowance.** A decline is permitted up to what the later build's ledger
  accounts for in corrections; the reasons are printed.
- **The verdict** on a diff — *richer*, *corrected*, *poorer*, *unmoved*, or
  *not comparable* — is that arithmetic in one word.

### Where the facts come from

Two sources, and neither is a copy.

**Scores** are `internal/enrich/maturity` reading the registry directory
directly. The directory is re-read on every request — a build installed by an
operation is on the page at the next load, with no watcher and no cache to
invalidate by hand — and scoring is memoised per file by size and modification
time, the same test the format's registry uses to tell an untouched file from
one rewritten under its name.

**Source registry entries** arrive as **data, handed to the handler at
construction** by `atlas workbench`. The workbench may not import the generate
lane (issue #5 §3.2), and `cmd/` is the one place allowed to wire every lane, so
the wiring reads each source's own `Describe()` and the crawl registry's own
`Usage()` and passes the result to `workbench.New`. Two alternatives were
weighed and refused:

- an `atlas sources -json` subcommand the workbench shells out for — it puts a
  second copy of every licence on a wire, and adds a subprocess between a page
  and a fact that is already in the binary;
- a curation data file restating the entries — a licence that exists twice can
  be wrong in one of them.

Wiring passes the registry entry itself, once. A source added to the generate
lane appears on the card wall with no second edit anywhere.

## 3. Operations

An operation is the `atlas` binary invoked exactly as a person at a terminal
would invoke it. The workbench does not link a pipeline lane: it **shells out to
the lane CLIs** (issue #5 §3.1), which is what keeps a lane's work inside its
lane while the page that starts it imports nothing but the format and the score.

The binary is this process's own executable (`os.Executable`), so an operation
runs the same build of the pipeline that is serving the page. No repository
checkout is involved.

| Operation | Needs | argv |
|---|---|---|
| `crawl` | archive | `atlas crawl --log-json -archive A -source NAME TARGET` |
| `tiles` | archive, tile set | `atlas tiles --log-json -archive A -output T` |
| `compose` | archive, tile index, registry | `atlas compose --log-json -archive A -tiles T/index.json -bundles R [volume]` |
| `enrich` | archive, tile index, registry | `atlas enrich --log-json -archive A -tiles T/index.json -bundles R [volume]` |
| `measure` | registry | `atlas measure --log-json -bundles R [volume]` |

An operation whose targets are not all configured is not offered: the card says
what is missing instead of letting a person find out by pressing a button.

Three request-borne values exist, and each is validated before an argv does:
the operation name (a table lookup), the source name (a registry lookup, plus a
crawler must exist for it), and the target and volume slug (`ValidTarget`,
below). Nothing composes a shell command; argv is a slice and the operating
system gets it as one.

## 4. The operation runner

`internal/workbench/oprunner` is a small library the workbench consumes. It is
not workbench-internal code (issue #5 §5.6): the safety properties are the
interesting part, they are testable without a page around them, and a second
consumer of the pipeline gets them by importing rather than by copying.

```go
type Operation struct { Name, Dir string; Argv []string }
func (o Operation) Validate() error
func (o Operation) Command() string

type Row struct {
    Seq     int      // 1, 2, 3… in arrival order across both streams
    Kind    Kind     // command | event | output | result
    Stream  string   // stdout | stderr, for the rows the subprocess spoke
    Level   string   // INFO, WARN… on a parsed record
    Time    string   // the record's own timestamp, verbatim
    Message string
    Attrs   []Attr   // documented vocabulary first, then the rest, alphabetically
    Failed  bool     // an ERROR record, or a result that did not succeed
}

type Runner struct{ /* the one operation slot; zero value ready */ }
func (r *Runner) Acquire(name string) (release func(), ok bool)
func (r *Runner) Busy() string
func (r *Runner) Run(ctx context.Context, op Operation, emit func(Row) error) error
func (r *Runner) Serve(w http.ResponseWriter, req *http.Request, op Operation, render RowWriter)
func Stream(ctx context.Context, op Operation, emit func(Row) error) error

func CheckOrigin(r *http.Request) error
func ValidTarget(target string, pair bool) error
```

**Rows are data, not markup.** `atlas` writes slog records to stderr and JSON
when asked, so the runner parses each line into a `Row` carrying level, message
and the documented attribute vocabulary of [logging](logging.md); a line that is
not a record is carried verbatim, because a tool's own words are worth more than
a runner's opinion of them. Rendering a row as HTML is the consumer's business,
which is what keeps the framework vocabulary in templates (issue #5 §4.3). The
workbench's `RowWriter` is one template, `op-row.tmpl`.

Every run reads the same way: one **command** row, then the subprocess's own
**event** and **output** rows in arrival order, then exactly one **result** row.
A program that will not start, a program that exits non-zero, and a program that
succeeds all end in a result row — the difference is `Failed`.

### The safety properties, carried verbatim

These are the reference implementation's, named because they are the contract:

1. **Origin-checked POSTs.** A browser sends `Origin` on any cross-site POST and
   omits it on an ordinary same-origin form submission. A present one must agree
   with the host the request arrived at, scheme included; a foreign one is
   **403** and nothing is planned, let alone run.
2. **One operation at a time.** The slot is a mutex; a second submission is
   **409** with the name of what is running, never a queue. Both refusals are
   decided before the first row goes out, because once a body is streaming the
   status line is spent.
3. **Target validation.** `ValidTarget` admits lowercase letters, digits, `-`,
   `_`, `.`, and — for a source addressed as two slugs — exactly one interior
   slash. A leading dash is refused so a target can never be read as a flag. A
   bad target is **400**.
4. **An operation dies with its request.** The subprocess runs under the
   request's context: a page abandoned mid-operation stops its operation, so
   nothing crawls on with nobody watching. An emit that fails — the page went
   away — ends the run the same way.
5. **A strict content security policy** on every page:
   `default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self';
   form-action 'self'; base-uri 'none'; frame-ancestors 'none'`. Nothing loads
   from anywhere but this server, nothing but this server's forms may be
   submitted to, and no page may be framed — a page that operates a pipeline is
   exactly the page that must not be reachable through somebody else's document.
6. **Pages are rendered into a buffer first**, so a template error is a clean
   500 rather than half a page.

## 5. Hypermedia

The workbench is pure HTMX and has no seam. Real URLs for every view, ordinary
links and forms, and **one** swap: the operations console, which grows a row at
a time (`hx-post` + `hx-target="#op-log"` + `hx-swap="beforeend"`) — the
streaming swap of issue #5 §4.3, the same shape the application's import stream
takes. No `hx-on`; the `hx-*` vocabulary lives in `templates/` and nowhere else.

The runtime is **not** vendored here. htmx is vendored once, with the
application's assets, and reaches this handler as bytes through
`Options.Runtime`: one vendored copy, one licence file, and no import edge
between two lanes that must not depend on each other. `atlas workbench` — which
may import both — hands it over.

A workbench mounted without a runtime is a working workbench. Every page is
plain HTML, every form is an ordinary POST, and an operation still streams: the
browser renders the rows as they arrive instead of a swap appending them. The
stylesheet is the identity's token system (carried verbatim from the
application's assets) plus one file of this surface's own.

---

## 6. Cartograph feature parity

M4's exit criterion is "cartograph feature parity", and this enumeration is the
evidence. Every user-visible feature of the reference implementation's
`cmd/cartograph` — read off its code and its tests — appears below with one of
three verdicts:

- **carried** — the same capability, here;
- **replaced** — the capability is subsumed by the score, or by a mechanism this
  architecture already has;
- **dropped** — deliberately not carried, with the reason.

**Tally: 58 features enumerated — 43 carried, 9 replaced by the score or by a
mechanism this architecture already has, 6 deliberately dropped.**

### The collection page → the library page

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 1 | Registry directory printed | carried | Same, with the point-table version beside it. |
| 2 | One row per volume, linking to its page | carried | Same. |
| 3 | Bundles that fail to measure listed as warnings | carried | Same; one bad file never takes the page down. (Smoke-tested against a real library holding format v1 bundles the clean-room reader refuses.) |
| 4 | Builds count | carried | Same. |
| 5 | "Figures are the serving build's" note | carried | Same, plus "the score is the measurement; everything beside it is a diagnostic". |
| 6 | Serving build = the registry's fold | carried | `bundle.Newer` over descriptors built from scores; a test holds the first build to `maturity.Serving`. |
| 7 | Measurements cached by size and modification time | carried | Same test, same reason. |
| 8 | Pin count as the headline figure | replaced | The **score** is the headline; the feature count is a diagnostic column. |
| 9 | `DescribedPct` column | replaced | Described count with its share beside it, over the right denominator — the >100 % defect is retired by construction (see [enrich](enrich.md)). |
| 10 | Unique raster MB | carried | Diagnostic column. |
| 11 | Lens/layer count | carried | Diagnostic column. |
| 12 | Merged-source badges | carried | "Readings" badges, from the serving build's ledger. |
| 13 | Depth column | replaced | Moved to the measurement page's cartography axis; the library table leads with the score. |
| 14 | Icon-coverage column | replaced | Moved to the measurement page's icons axis, for the same reason. |
| — | *(new)* Score movement against the previous build | — | The delta headline of issue #5 §5.6. |

### The volume page → the measurement page

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 15 | Builds newest first, serving one marked | carried | Same. |
| 16 | Capture time, revision, short stamp per build | carried | Same, plus whether the build was written by the enrich lane and under which policy. |
| 17 | Annotation / cartography / structure / icons axes | carried | Same four, plus the conventions axis: five, as §5.3 asks. |
| 18 | Merge table per build | carried | Per world: offered (by kind), matched, median px, enriched, added, adopted, held, rejected, alignment. Origin accounts are marked as such. |
| 19 | Held pins with their reasons | carried | Same, and the rest of the ledger with it: rejected, adopted, collections that took an attribute, corrections. "Whole merge-ledger reporting" is the whole ledger. |
| 20 | Compare form choosing two builds | carried | Same. |
| 21 | 404 for an unknown volume | carried | Same. |
| — | *(new)* Score headline, per-world breakdown, allowance and comparability | — | §5.3. |

### The diff page

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 22 | Axis table with A, B, Δ and signed styling | carried | Same shape, over the new axes. |
| 23 | Share deltas in whole points | carried | Same arithmetic. |
| 24 | Byte deltas in MB | carried | Same. |
| 25 | Pins added and removed, by map | carried | Features added and removed, by world, unpacked from both builds on demand. |
| 26 | Matched-pair stability (kept / gained / lost) | carried | Same rule: a pair is the same pair only when donor and winner both agree. |
| 27 | "pick two builds" → 400 | carried | Same. |
| — | *(new)* Score-delta headline, verdict, per-world deltas | — | §5.6. |

### The sources page

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 28 | One card per source, name and slug | carried | Same, from the lane's registry rather than a table in the dashboard. |
| 29 | Licence and attribution | carried | The registry entry's own fields — this is what §5.6 asks the cards to carry. Cartograph only had them inside its prose. |
| 30 | Component badges (raster / icons / locations / metadata) | dropped | The component set was dashboard-local editorial data with no home in the clean-room registry, and it never varied usefully. What a source actually contributed to a build is visible per build, in the ledger. |
| 31 | A prose description per source | dropped | `doc.Provenance` carries identity and terms, not prose. A source's prose lives in its package and in [generate](generate.md); restating it here is the second copy this design refuses. |
| 32 | Per-source fetch form with a target hint | carried | On the operations page, one form per crawlable source, each hinted by its own crawler's `Usage()`. |
| — | *(new)* Id-space badge, crawlable badge | — | Facts the registry entry already carried. |

### The pipeline panel → the operations page

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 33 | Repo / archive / bundles line | carried | Binary / archive / tile set / registry. |
| 34 | Fetch (`tools/crawl`) | carried | `crawl`. |
| 35 | Rebuild pyramids (`tools/tiles`) | carried | `tiles`. |
| 36 | Recompose bundles (`tools/generate`) | carried | `compose`, and `enrich` beside it — the lane split since. |
| 37 | Console pane | carried | `#op-log`, rows appended as they arrive. |
| 38 | `go run ./tools/…` from a repository checkout | replaced | Operations invoke the `atlas` binary — this process's own executable. The lanes are subcommands now, and no checkout is required. |
| 39 | `-repo` flag and `findRepo()` walking up for `go.mod` | dropped | Nothing to find: see #38. |
| 40 | Refusal when `-repo` or `-archive` is unset | carried | Each operation names the configured targets it lacks, on the card and in the refusal. |
| 41 | `fmg-archive` directory-name check before generate | dropped | `atlas compose -archive` takes the archive root directly; the check guarded a positional convention that no longer exists. |
| 42 | Unknown operation, unknown source → 400 | carried | Same. |
| — | *(new)* `enrich` and `measure` operations, optional volume narrowing | — | The lanes that exist now. |

### Mechanics

| # | Cartograph | Verdict | Here |
|---|---|---|---|
| 43 | Origin-checked POST | carried | `oprunner.CheckOrigin`, tested on its own. |
| 44 | One-operation mutex answering 409 | carried | `Runner.Acquire`; the refusal names what is running. |
| 45 | Target validation (safe characters, no leading dash, pair slash rule) | carried | `oprunner.ValidTarget`, rule for rule, plus the volume slug held to the same rule. |
| 46 | Streamed subprocess output, flushed as it arrives | carried | Streamed **HTML rows** carrying the parsed slog stream. |
| 47 | Subprocess dies with the request | carried | Same, via the request's context. |
| 48 | Both streams interleaved in arrival order | carried | Same, with each row saying which stream it came from. |
| 49 | Strict CSP on every page | carried | Plus `base-uri 'none'` and `frame-ancestors 'none'`. |
| 50 | Page rendered into a buffer first | carried | Same. |
| 51 | Hand-written `app.js` streaming the response into a `<pre>` | replaced | An htmx streaming swap; the workbench ships no bespoke JavaScript. |
| 52 | `/static/style.css`, `/static/app.js` | replaced | `/assets/workbench.css` and the vendored runtime at `/assets/htmx.js`. |
| 53 | Plain-text operation output | replaced | HTML rows with level, message and attributes as separate cells, because the stream is structured now. |
| 54 | `-bundles`, `-addr` flags; default library | carried | Same flags, same default. |
| 55 | Prints the listening URL | carried | On stdout, as product output. |
| 56 | Own `cartograph` binary | replaced | A subcommand of the one binary, `atlas workbench`. |
| 57 | Dashboard-local `Source` interface with `FetchArgs` | dropped | Sources are the generate lane's; the workbench receives their entries as data and builds argv from the operations table. |
| 58 | `internal/measure` axis vocabulary (`Pins`, `Zones`, `Categories`) | dropped | The format speaks features, collections and shapes now; the axes are spelled in the format's own words. |

### What the workbench gained

Not parity, but worth naming as the reason the exit criterion is a floor and not
a ceiling: the score and its movement on every page, the build gate's own
comparison rendered rather than reimplemented, the whole ledger instead of its
counts, the enrich lane's operations, structured rows instead of a text pane,
and a runner whose safety properties are a tested library rather than four
paragraphs of handler.
