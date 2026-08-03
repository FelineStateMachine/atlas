# The generate lane

Generate makes volumes. It takes a capture archive — bytes somebody else
published, fetched once and kept — and writes a `.atlas` file: one volume,
complete, offline, deterministically named.

The lane has four steps and one rule.

```
capture ──▶ translate ──▶ [tiles] ──▶ compose ──▶ a volume in a registry
 network      per source    rasters    conventions, packing, stamping
 hand-run     read-time     derived    producer-strict
```

**The rule: no data source's shape, vocabulary, or name appears outside
`internal/generate/sources/<name>/`.** Everything downstream of a translator
speaks Atlas's own vocabulary and cannot tell which source it read. Only a
merge ledger ever names a source, and merging is not this lane's work.

Generate is **single-source**. What one source said travels through
untouched; folding two sources' readings of one volume together belongs to the
[enrich lane](enrich.md), and the composed multi-source result is
`generate ⊕ enrich`.

This document is normative for the interchange document's schema and for the
curation data files. It is the prose twin of `internal/generate/doc` and
`internal/generate/curation`.

---

## 1. The interchange document

`internal/generate/doc` defines the one thing that crosses the middle of the
lane. It is **Atlas's schema, designed backwards from what composition needs**
— worlds, lenses, collections, features, geometry, attributes, provenance —
and no source's shape is privileged in it.

A document is produced and consumed inside one run. It is not an archive
format and nothing is expected to read one back years later; it exists so that
the two halves of the lane can be reasoned about, tested, and printed
separately. `atlas translate` prints one.

### 1.1 The shape

```jsonc
{
  "doc": "atlas-generate-doc",
  "version": 1,
  "volume": { "slug": "tunic", "title": "TUNIC" },
  "source": {
    "name": "mapgenie",              // the slug a ledger names it by
    "label": "MapGenie",             // the same source, spelled for a person
    "license": "",                   // what the volume owes, where it is known
    "attribution": "…",
    "idSpace": "native"              // "native" | "derived"
  },
  "worlds": [ … ],
  "icons":  [ { "key": "fox_shrine", "file": "fox_shrine.svg", "data": "<base64>" } ]
}
```

A **world**:

```jsonc
{
  "id": 427,
  "slug": "world",
  "title": "World",
  "center": { "lat": 0.787…, "lng": -0.771… },
  "capture": {
    "kind": "map",                   // the source's own name for these bytes
    "id": 427,                       // the capture's id in the source's id space
    "locator": "/api/v1/maps/427/full",
    "contentHash": "ff58c59a…",      // SHA-256 of the archived bytes
    "capturedAt": "2026-07-30T03:57:41.529Z"
  },
  "lenses": [ {
    "name": "Default",
    "tileSet": "tunic/world/default-v2",
    "frame": {                         // the deriver reads this; composition never does
      "minZoom": 9, "maxZoom": 15, "format": "jpg",
      "windows": { "15": { "minX": 16256, "minY": 16256, "maxX": 16383, "maxY": 16383 } }
    }
  } ],
  "collections": [ … ],
  "attrs": { }                        // the world speaking the conventions
}
```

A **collection** and its **features**:

```jsonc
{
  "id": 5984,                        // zero asks composition to derive one from key
  "key": "regions",                  // merge identity; absent, the icon key stands in
  "title": "Fox Shrine",
  "group": "Locations",              // a legend heading, and only that
  "kind": "point",                   // point | path | area
  "icon": "fox_shrine",              // names artwork in the document's icon set
  "color": "#6984F2",
  "iconColor": "#6984F2",
  "visible": true,
  "attrs": { "atlas.render.as": "pin" },
  "features": [
    {
      "id": 177474,
      "title": "Fox Shrine",
      "subtitle": "",
      "description": "",
      "at": { "lat": 0.804…, "lng": -0.737… },   // points
      "geometry": [ { "type": "Polygon", "coordinates": … } ],  // paths and areas
      "center": { "lat": …, "lng": … },
      "member": 0,                   // the area feature whose ground this stands on
      "parent": 0,                   // the area feature this area sits inside
      "links":  [ { "title": "the well", "feature": 200 } ],
      "attrs":  { }
    }
  ]
}
```

### 1.2 The decisions

The schema is source-neutral by design
([decision 4](decisions/0004-source-neutral-interchange-document.md) carries
the context), and its load-bearing choices are these:

- **One ordered `collections` array.** Points, paths and areas are the same
  kind of citizen; a group is a heading string on a collection, never a
  container.
- **Extensions are ordinary fields.** `attrs` speaks the conventions, and a
  collection is declared like anything else.
- **Render policy travels once.** A publisher's own render field is spoken as
  `atlas.render.as` by the source, and the field itself never travels.
- **Coordinates are `{lat, lng}` floats.** A source's tolerance for other
  spellings is the source's business.
- **Absence is `0`, everywhere** — matching the wire's own reading of zero
  (§1.4).
- **Artwork travels in the document**, never as a key implying a file probe at
  composition time.
- **Lens detail splits in two.** Name and tile set are for composition; the
  publisher's claimed frame is carried separately, for the deriver alone,
  because only a source can say which tiles a complete level was supposed to
  hold.
- **Provenance rides the document** — `source`, and a per-world `capture` —
  rather than being recovered from directory names.
- **Links are resolved by the source**, because a link syntax is a
  publisher's.
- **The world window is curation's** to name where it is shared; a pyramid
  names its own.

Two structural choices frame all of that: the collection/feature model — a
collection has a key, a title, a legend heading, a kind, visibility,
attributes and features; a feature has an identity, a place or a geometry,
nesting, prose, links and attributes — and translation at **read time**, over
an archive of source-native bytes, so an editorial change replays over years
of captures instead of re-crawling them.

### 1.3 Coordinates

Positions are latitude and longitude **in the volume's own projection** — the
same world space `format/bundle.Coordinate` carries and the same one a lens's
tile pyramid is cut in. For a game map that space is synthetic: an image was
encoded as a slippy map long ago and the numbers are pixels wearing degrees.
For a planet or a city it is the real thing.

A source publishes what its ground means; composition does no reprojection.
Composition projects to world pixels in exactly one place — measuring the
ground a world's contents cover — using spherical Mercator against the world's
tile window.

### 1.4 Identifiers

Feature and collection identifiers are `int64` in the document and **signed
32-bit on the wire, where zero reads as absence**.

- A source with native numeric identities passes them through and declares
  `idSpace: "native"`.
- A source without them derives stable identifiers from stable names and
  declares `idSpace: "derived"`. Derivation must be a pure function of a
  name, and a collision must fail rather than rename something.
- A collection may carry a `key` and no `id`; composition derives one, as
  FNV-1a over `"<world id>:collection:<key>"`, masked to the positive range of
  an `int32`, with zero moved to one, refusing a collision with a number the
  source already claimed.

**One id space per world.** The text payload is keyed by feature id across
every collection and every kind, so two features sharing a number would lose
one of their descriptions. `Document.Validate` refuses it.

### 1.5 Determinism

Every ordering in a document comes from the capture. Maps are the only
unordered thing in it and JSON sorts their keys. A source that sorts is
throwing away an editorial order somebody chose.

---

## 2. Sources

A source is one publisher's reader. The interface is three lines:

```go
type Source interface {
    Describe() doc.Provenance
    Translate(a *archive.Archive, v archive.VolumeRef, log *slog.Logger) (doc.Document, error)
}
```

`internal/generate/sources/registry.go` is the assembly point and **the one
file outside a source's own directory permitted to name a source**. It may name
only a constructor. If adding a source needs a second line there, the source is
leaking.

### 2.1 The rules a source keeps

- **No network.** Capture is a separate, hand-run step. A translator is a pure
  function of bytes already on disk, which `depcheck`'s `netconfine` rule
  enforces: outbound HTTP lives in `internal/generate/crawl` and nowhere else.
- **Refusal over guessing.** A source states its structural preconditions and
  refuses what does not meet them. A volume that is merely *not crawled yet*
  wraps `ErrNotReady` and is skipped; anything else fails the build.
- **Gates are named.** IGN refuses embedded-MapGenie maps, Piggyback refuses
  unverified transforms, ArcGIS refuses an uncurated city
  ([decision 16](decisions/0016-uncurated-captures-are-passed-over.md)). The
  MapGenie reader refuses a capture whose
  kind is not its own, because reading another source's bytes through it would
  produce a document that lies about where it came from.
- **Offline purity starts here.** A publisher's links are resolved or removed
  by its own reader. Nothing downstream should have to know a link syntax.
- **Determinism.** Same archived bytes, same document, on any machine.

### 2.2 The MapGenie reader

`internal/generate/sources/mapgenie` is the worked example and the only source
in the tree today.

- Capture kind `map`: one map's whole API response, archived verbatim.
- Every `group × category` becomes a point collection, in capture order. The
  group survives as a heading string.
- A category's colour resolves over its group's, normalized to `#RRGGBB` upper
  case.
- Coordinates arrive as JSON numbers on some maps and quoted strings on others;
  both are read.
- Regions become an area collection keyed `regions`, titled `Regions`,
  numbered by composition. A region whose geometry all came through empty is
  dropped. A line among the regions is refused: MapGenie's regions are ground,
  and a line would be a path collection somebody meant to declare.
- `display_type` is spoken once as `atlas.render.as` and never travels.
- Markdown links are replaced by their labels; a link carrying
  `locationIds=<n>` that names another pin of the same world survives as a
  cross-reference; every surviving URL is stripped.
- Artwork is read once per key, `.svg` then `.png`, and carried in the
  document.

### 2.3 The NASA Trek reader

`internal/generate/sources/nasatrek` reads a planetary volume. A capture marries
two publications that know nothing of each other: a global equirectangular
mosaic from NASA's Trek tile services, and the IAU Gazetteer of Planetary
Nomenclature's feature list for the same body.

- Capture kind `trek-map`, one per body, carrying every mosaic captured for it.
- The coordinate design is the whole trick. A Trek mosaic is two tiles wide and
  one tall at its own zoom zero, so a Trek level sits one zoom up in the square
  the corpus cuts and the planet fills the top half of the world square. A
  feature's planetary coordinates become a pixel of that image — longitude across
  the full width, latitude down the top half — and the pixel becomes a synthetic
  position. The projection's distortion cancels exactly, because the raster and
  the features ride one mapping.
- The world declares the flattening as conventions (`atlas.geometry.surface`,
  `.projection`, `.equirect.px`, `.equirect.deg`, `.body`), so a reader can run a
  packed position backward and stand on the planet, and every feature carries the
  coordinates the Gazetteer published verbatim as `atlas.geo.lat`/`.lon`.
- One collection per Gazetteer feature type, sorted, all under the heading
  `Nomenclature`. The type descriptor `"Crater, craters"` keeps its singular half
  as the title and lends its slug as the artwork key.
- The Gazetteer has no artwork, so each collection names a library glyph through
  `atlas.icon.std`, chosen from the IAU's own type codes — a *mons* is a mountain,
  a *patera* a volcano, a *palus* literally a marsh — falling back to shape
  language where no glyph says the thing.
- `idSpace: "derived"`: nothing in either publication numbers a mosaic or a
  feature type, so every identity is minted from a stable name.

### 2.4 The Blue Marble reader

`internal/generate/sources/nasabluemarble` reads the included Earth volume's
one capture: NASA Earth Observatory's Blue Marble Next Generation base map
married to Natural Earth's 1:110m cultural vectors — pinned publications
rather than live endpoints.

- Capture kind `blue-marble-map`, one per capture of the pinned files.
- One world (`earth`), one lens (`Blue Marble`), and the lightest cut of
  ground truth: the primary capitals as a point collection per continent,
  each pin a member of the country it stands in, and the country borders as
  one quiet area collection. Continents are Natural Earth's own filing; a
  capital whose microstate the 1:110m borders leave out takes the nearest
  drawn ground's continent, computed from the captured polygons at translate
  time the way the city source computes its watershed join. Everything else
  is later sources' work, folded in by the enrich lane.
- The coordinate design is the shared whole-sphere window NASA Trek also rides
  (`doc.EquirectPx` and friends): a 2:1 global image filling the top half of
  the world square, declared as registered conventions — surface, projection,
  the equirect px/deg pair, the body, and the body's mean radius — so the cell
  systems recognise the ground through the same declarations every spherical
  world makes.
- The capture body records the product's identity — its title, NASA's credit
  line, the source digest and size — and the derivation policy its tiles were
  cut under, so a policy change reads as a new capture rather than the same
  one wearing new bytes.
- The gates: another kind's capture, another source's name, another body, a
  missing digest, a missing size, a capture declaring no pyramid or carrying
  no features, a country with no identity, or a capital off the planet are
  refused by name.
- `idSpace: "derived"`: nothing in the publication numbers a base map.

The crawler half (`internal/generate/crawl/bluemarble.go`) is the one place
the pinned URLs, digests and capture time are spelled. It fetches each pinned
file exactly once into the staged archive, refuses any digest but the pinned
one, cuts the pyramid's deepest level through the deterministic fixed-point
Catmull-Rom resampler beside it, distills the Natural Earth files into the
capture body — names, codes, continents, label points and rings, coordinates
verbatim — and records the capture under the pinned time, so the archive — and
everything stamped downstream of it — reproduces on any machine. `included/README.md` is the recipe and the provenance, and
`make included-earth` is the whole run.

### 2.5 The IGN reader

`internal/generate/sources/ign` reads a community wikimap: a flat image tiled
like a world, markers placed on it in image-relative coordinates, and a flat list
of marker types that names a parent to make two levels of legend.

- Capture kind `ign-map`.
- A wikimap's image is normalized so its taller dimension spans 1. A marker's
  latitude runs down that span and is negative, its longitude across it, so
  `(lng, -lat)` times the world square's edge is the pixel it is drawn on.
- One collection per marker type, types in slug order, gathered under the heading
  of whichever parent each names, headings in the order their first type appears.
  A type no marker uses is left out — an empty collection would only dim the
  legend — but it stays in the capture, so a marker appearing under it later
  revives the collection without a policy change.
- Artwork is a PNG sprite per type slug, read from the archive and carried.
- **The gate: an embedded MapGenie map is not an IGN map.** Some IGN wikimap
  pages are MapGenie maps in an IGN frame — the page declares a MapGenie game id
  and serves MapGenie's tiles. Capturing one here would archive a second, worse
  copy of data Atlas already reads properly, and a merge would then fold a source
  into itself. The refusal lives in `internal/generate/crawl`, where the page's
  own declaration is still in front of the crawler; by the time a capture exists
  the evidence has been thrown away.
- `idSpace: "derived"`: markers are opaque strings and types are slugs.

### 2.6 The Piggyback reader

`internal/generate/sources/piggyback` reads the official guide house's maps,
which carry what a community wikimap does not: prose. Pins arrive with names and
descriptions and both survive into a volume.

- Capture kind `piggyback-map`.
- Piggyback draws in a game's own coordinates on a Leaflet `CRS.Simple` map: a
  linear transformation squeezes them onto the unit tile at zoom zero. A pin
  passes through it and then through the shared inverse Mercator.
- A declared category becomes a heading and its types become collections under
  it, ordered by declared position then key. A type no pin uses is left out.
- District name pins arrive filed under the reader-state `favorites` category,
  which is nothing to build a legend from. They gather under their own heading
  and render as `text`, which is what they are on Piggyback's own map. Any *other*
  undeclared type fails the build: better a loud build than a pin silently
  dropped.
- Piggyback publishes no bounds, so the crawler's own survey of which tiles
  answered is the only account of where the pyramid is drawn, and a capture with
  no observed level is refused.
- **The gate: an unverified transform is refused.** The transformation is read
  off the page's own scripts, it decides where every pin in the volume stands,
  and a wrong one puts a whole map's contents somewhere plausible and wrong —
  exactly the failure a later merge would try to fit an affine to.
  `verifiedTransforms` is the list of games whose numbers have been checked
  against the published map; a capture whose numbers are not in it fails.
- `idSpace: "derived"`: pins, categories and types are opaque string ids.

### 2.7 The ArcGIS Hub reader

`internal/generate/sources/arcgishub` reads a city. It is the only source whose
subject is a real place, and the only one where **a world is a date**: each crawl
day of a city's open-data hub registers its own world, so a volume's world picker
is the city's version history and the difference between two worlds is a
difference in the city.

- Capture kind `arcgis-map`.
- A city sits off the Web-Mercator diagonal, so its curated bounding box is
  padded five percent per side and squared in Mercator's own terms; that square
  is the world. Features project through the Mercator forward onto it and out
  again through the shared synthetic inverse, and the world declares the mapping
  as `atlas.geometry.mercator.px`/`.deg`, so a reader can invert it and stand on
  the ground. Every pin also keeps the coordinates the city published, verbatim.
- Polygon and line datasets become shape collections — one collection per
  dataset, one feature per curated bucket, holding every row that fell into it.
  Point datasets become pins under their curated heading. Pins are emitted first
  and ground second, which is the legend order.
- **The membership join.** A capture that also carries the national
  subwatersheds can say which subwatershed each of the *city's own* zones lies
  in, computed from the captured polygons at translate time and carried as
  `atlas.hydro.huc12` plus one sentence. The claim is made only when every
  sampled part of a zone agrees on one answer: a zone that straddles two says
  nothing rather than something misleading.
- **The gate: an uncurated city is not read.** The curation table is the whole
  authority — an unverified window would hang every pin on the wrong pixel, and
  unverified field names would title every zone with nothing. Unlike the other
  gates it is not a build failure: an uncurated city wraps `ErrNotReady` and the
  volume is passed over, because an archive may legitimately hold a city this
  table may not name (the operator's own), and a hard refusal would take every
  other volume down with it.
- The one lens is a basemap **rendered offline from the city's own vector data**,
  never fetched, so the pyramid is reproducible from the archived capture alone.
  Its frame declares every level complete, which is what lands the derivation on
  the translated world's own grid rather than on the shared window a city does
  not sit in, and the lens carries the drawing the deriver rasterizes it from —
  see §4.4.
- `idSpace: "derived"`: a hub's object ids are load artifacts that churn between
  refreshes, and nothing above a row is numbered at all.

### 2.8 Adding a source

A source is one directory, one constructor, and one line in
`internal/generate/sources/registry.go`. The walkthrough:

1. **Declare what you read.** Put the archived capture's shape in
   `capture.go`, with only the fields the interchange document needs. A capture
   holds a great deal else, and the way to keep it out of Atlas is to not have a
   field for it. These are the only declarations in the tree permitted to carry
   your publisher's field names.
2. **Describe yourself.** `Describe() doc.Provenance` returns the registry slug a
   ledger names you by, the label a person reads, the licence and attribution the
   volume owes, and your id space. Every document you emit carries it verbatim,
   and the workbench's source card reads it.
3. **State your gates.** Before anything is read out of a capture, say what a
   capture has to be: the right kind, a named map, a declared pyramid, features
   that are actually somewhere. Refuse rather than guess. A volume that is merely
   *not crawled yet* wraps `ErrNotReady` and is skipped; anything else fails the
   build.
4. **Number things.** If your captures carry stable numeric identities, pass them
   through and declare `IDSpaceNative`. If they do not, mint them with
   `doc.NewIDSpace()` from stable names, declare `IDSpaceDerived`, and let a
   collision fail rather than rename something.
5. **Place things.** If your ground is a real planet, publish real coordinates.
   If it is a picture, measure in the world square's pixels and hand them to
   `doc.SyntheticPosition`, which inverts the reader's own projection so a
   feature lands on exactly the pixel you measured.
6. **Resolve your links.** A publisher's link syntax is the publisher's business:
   turn a deep link into a `doc.Link` where it names another feature of the same
   world, and strip every URL that survives. A bundle serves offline.
7. **Say it in the conventions.** A publisher's own render field, its declared
   projection, a feature's true coordinates — speak them once through
   `format/semconv`'s registered keys and let the field itself stop there.
8. **Register.** One line in `registry.go`, naming a constructor. If you need a
   second line there, the source is leaking.
9. **Prove it.** A translator test in your own package comparing what your
   document *means* against a committed capture — the NASA Trek reader is the
   worked example, over `testdata/corpus/translators/nasa-trek.doc.json` and
   its fixture — plus your reader's refusals, exercised the way every source
   package exercises its own.

What you may not do: reach the network (`depcheck`'s `netconfine` rule forbids
it outside `internal/generate/crawl`), sort anything the capture ordered, read a
clock, or let your vocabulary out of your directory.

---

## 3. The capture archive

`internal/generate/archive` reads it. The archive is a **source-neutral
concept** — content-addressed bytes, grouped by volume and by world, each
carrying the time it was first seen — but the archive on disk is a specific,
historical layout, kept verbatim as input because years of captured history are
data, not code.

```
<root>/archive.json                                      the volume register
<root>/<vol>/game.json                                   the world register
<root>/<vol>/icons/<key>.svg|.png                        collection artwork
<root>/<vol>/maps/<world>/snapshots/index.json           the capture index
<root>/<vol>/maps/<world>/snapshots/map/<hash>.json      the capture bodies
<root>/<vol>/maps/<world>/tiles/index.json               per-tile records
<root>/<vol>/maps/<world>/tiles/set-<id>/<z>/<x>/<y>.<ext>
```

`<vol>` is the register entry's own `directory` field, not a slug this lane
computes — the register is the only thing that says where a volume sits. In the
staged archives it is `games/<title-slug>-<id>`, which is a habit of the layout
and not a rule: a caller that resolves a volume's directory any other way is
reading the disk instead of the register. The `maps/` level is the layout's word
for "the worlds of this volume"; nothing above `archive.Volume` ever sees it.

What a caller sees is the vocabulary, never the layout: volumes, their worlds,
each world's captures, a capture's body, a volume's artwork. Two habits of the
layout are translated rather than passed on — a register entry naming no source
is MapGenie, and a capture's volume and world are recovered from the path it
sits at rather than from the record.

**Unchanged bytes record nothing.** A capture is deduplicated by content hash
alone: a re-crawl that fetched the same thing leaves the index byte for byte as
it was, so nothing downstream moves. `capturedAt` is first-seen, never
last-verified. Times compare as strings, which is the order that means
something for RFC 3339.

**Only the newest capture of a world is read.** Older ones are history kept on
disk.

*(The layout a new archive would use — and whether one is worth writing — is
still an open question. Nothing new inherits this naming.)*

### 3.1 Writing it

`internal/generate/crawl` is the write path, and it is **the only package in
Atlas permitted to reach the network**. `depcheck`'s `netconfine` rule is what
makes that true rather than customary: no other package in the format or
pipeline lanes may import `net/http` at all, and the outbound half of it is
reported anywhere outside this package. Fetching is crawling wherever it
happens — the enrich lane's national hydrography evidence is captured here too,
and travels in the archive, so its join re-runs against the archive rather than
against a live endpoint.

**Politeness.** One `Fetcher` per run holds a single monotonically advancing
instant under a lock. Every request of the run takes the next slot, so two
requests are never closer together than the interval (150 ms by default,
under seven a second) however many goroutines are waiting. It is not a token
bucket on purpose: a bucket lets a run that idled spend its savings in a burst,
which is the exact shape a rate limiter is watching for. A 429 or a 5xx pushes
*the schedule* forward — the origin's own `Retry-After` where it gives one,
exponential backoff otherwise — so the whole run slows rather than the one
worker that heard the refusal. Four attempts including the first; beyond that,
an origin that is refusing is refusing.

The user-agent is a browser's string, and that is a decision. Several of these
origins answer 403 to anything else, and a 403 is indistinguishable from "never
published" in a tile pyramid, so an honest header would silently punch holes in
an archive. The politeness that matters is in the behaviour.

**Absence is a result.** 404 and 403 both mean *not published*: a pyramid is a
rectangle and its corners are usually empty. It is recorded as `absent` so a
re-crawl does not ask again. 202 means the origin is preparing the answer, and
the caller waits. Anything else non-200 is a disagreement rather than a hiccup
and is not retried.

**Content addressing.** A capture's body is written to a path that is its own
SHA-256, so writing it twice writes the same bytes; the index is appended to
only when that hash has never been seen. `capturedAt` is therefore **first-seen,
never last-verified**, and a re-crawl of unchanged data leaves the working tree
byte for byte as it was. That is the property everything replayable stands on:
the translators replay, the derivation stamps stand still, and a rebuild writes
the file that is already there.

**Nothing is renamed.** A register entry is found by identity, merged in place,
and keeps its position — so a directory named once is named forever, and a field
this package has never heard of survives a run. Directory names are only ever
computed on first sight.

**The same-slug policy.** Two publishers describe Night City. Each registers the
volume under its *plain* title, so their builds answer for one library entry and
the newest capture is the one a reader sees; each names its *directory* from a
title carrying its own prefix (`IGN Cyberpunk 2077`), so they do not fight over
one directory. And each source's archive identities carry a bit of their own —
IGN at 2³², Piggyback at 2³³, NASA Trek at 2³⁴, ArcGIS at 2³⁵ — over an FNV-1a
hash of a stable name in the low 31 bits, all held under 2⁵³ so a JSON round
trip through `float64` is exact.

**A crawl is interruptible.** Every write is idempotent and every fetched thing
is recorded before the next is asked for, so stopping a run is a normal way to
end it. A resumed run skips what is cached and does not re-ask for what was
absent.

### 3.2 The crawlers

```
atlas crawl -archive DIR -source NAME TARGET [-n] [-interval D] [-concurrency N] [-max-zoom Z] [-on DATE]
atlas crawl -source list
```

A crawler is the outward half of a source, and a separate package from its
reader on purpose: the two have opposite properties. A reader is a pure function
of an archive; a crawler is the only thing in Atlas that can fail because
somebody else's server is having a bad afternoon.

| crawler | target | runnable here |
| --- | --- | --- |
| `ign` | `<objectSlug>/<mapSlug>` | code-complete, not run — the captures are archived |
| `nasa-blue-marble` | `earth` | runnable — one pinned image, digest-verified, fetched once (§2.4) |

The game sources' captures are archived and their endpoints are somebody else's
editorial work, so their crawlers are kept complete and are not run against live
endpoints. The ArcGIS/USGS crawler is the one that may run, because its data is
public, and it is the one still outstanding. The renderer it would feed landed
first (§4.4), which leaves the city's archive an input rather than something
this lane can take: the proof city's archive is staged at
`crawl/bend-or/fmg-archive`, and the lane reads, draws and
composes from there. What the crawler adds is the ability to take a *new* day —
a second world in the city's version history — rather than the ability to
rebuild the one that is there.

The IGN crawler carries this lane's most consequential gate. Some IGN wikimap
pages are MapGenie maps in an IGN frame: the page declares a MapGenie game id
and serves MapGenie's tiles. Archiving one would put a second, worse copy of
data Atlas already reads properly into the archive, where a later merge would
fold a source into itself and report a beautiful agreement between a thing and
itself. The evidence is the page's own declaration, and it exists only while the
crawler is looking at the page — which is why the refusal lives there and the
reader says plainly that it cannot check.

---

## 4. Tiles

`internal/generate/tiles` reads derived pyramids and the stamps that say how
they were derived.

A **lens** is one raster pyramid picturing a world. Deriving one — finding the
deepest complete captured level, folding it down, choosing between
nearest-neighbour and box reduction, recording which tiles exist — happens once,
into a tile set on disk. Composition reads the result.

```
<root>/index.json                       the register
<root>/<pyramid>/<z>/<x>/<y>.<ext>      the rasters; z, x and y are local
```

A pyramid's register entry carries its source tile set, its zoom range
(`minZoom`, `maxZoom`, `fullZoom`, `sourceZoom`), the window it was cut from,
a format per level, its content bounds, whether it is smoothed when magnified,
the colour painted behind it, a coverage bitset per partial level, and its
derivation stamp.

### 4.1 The derivation stamp

Every pyramid carries a stamp over everything it was derived from: the plan's
shape, the content hash of every source tile that went into it, and a hash of
the deriving tool's own source. Composition folds it into the bundle's stamp
under the part name

```
tiles/<pyramid>
```

— one part per pyramid, however many tiles it holds — and **never reads a tile
to do it**. That is what makes an unchanged volume cheap to notice: a rebuild
that would write the same bytes computes the same stamp without decoding a
single raster, and the file it would write is already there under a name
carrying that stamp.

The mechanism is one-directional on purpose. Composition trusts a pyramid's
stamp and cannot recompute it: the captured tiles it was folded down from are no
longer in front of it. A pyramid with an empty stamp stamps as empty, which is
honest — the bundle records that nothing was claimed about how those tiles came
to be.

### 4.2 What the deriver promises

`atlas tiles -archive DIR -output DIR` folds captured frames into pyramids.
Its input is the archive — the tiles a crawler wrote, under the paths a tile
server used — plus each lens's **frame**, which is what the source declares
about the tiles it captured: how deep the pyramid goes, in what encoding, and
which tiles each level was supposed to hold. That last question is one no amount
of looking at an archive can settle, because a level missing its last row looks
exactly like a level that never had one, and it is the only reason a frame rides
the interchange document at all. Composition never reads it.

- **Frame discovery.** A world is a 32-tile square. Local zoom 0 is the source
  zoom whose window collapses to one tile; local coordinates are source
  coordinates less the window's first tile. No height is assumed: the frame is
  measured, so a publisher cutting the same ground from zoom 6 and one cutting it
  from zoom 13 both derive correctly. A frame whose axes come to rest on
  different tiles is refused — it would draw a map in one place and its features
  in another.
- **The complete-level rule.** The deepest level whose expected tiles are all
  present is `fullZoom` and is copied byte for byte; every shallower level is
  folded down from it, never taken from the captured intermediates. Partial
  levels above it are copied while they stay contiguous, and are what
  `maxZoom` counts.
- **Pixel art versus photographs.** A pyramid that is not interpolated reduces
  nearest-neighbour and normalizes to lossless PNG; a photographic one reduces
  by box filter and keeps its encoding.
- **Coverage bitsets.** A level that is empty or exactly full records nothing;
  otherwise a bounding box in tiles and a row-major bitset over it, least
  significant bit first, base64. An absent entry means the level is complete.
- **The stamp.** SHA-256 over: the tool's source hash; the source and asset
  paths; `fullZoom` and the deepest zoom; the preferred format and the
  interpolation flag; the content bounds where declared; the affine and target
  zoom where the pyramid is a warp; and then, per level in ascending order, the
  level number, its tile count, and each tile's `x`, `y`, content hash and
  format, sorted by `x` then `y`. Every field is NUL-terminated. The listing
  order on disk is not an input.
- **The background tile.** A level's filler is the content hash more than half
  its tiles share, found once on the level that is trusted and omitted from every
  level. Reusing one hash keeps a sparse deep level from having its handful of
  real tiles voted "background" by its own majority. The colour painted behind
  the raster is the mean of that tile's pixels, and only if the tile is flat
  within twelve levels per channel — otherwise nothing is omitted at all, because
  a hole nobody can paint over is worse than a duplicated tile.
- **Incrementality.** A pyramid whose stamp matches the register's previous
  entry, whose asset path is unchanged and whose directory still exists, is
  carried over untouched.
- **The write.** Each pyramid is derived under a temporary name and arrives by
  one rename, so a reader never sees a half-written one; pyramids the archive no
  longer offers are taken out; and the register is written last, so until the end
  it still names the stamps of what is actually on disk. A run interrupted
  anywhere leaves a register whose stamps disagree with a plan, and the next run
  derives exactly those pyramids again.

### 4.3 What a stamp promises, and what it does not

A derivation stamp is a **rebuild-cost** promise — *nothing that made this has
moved* — and never a content promise. It covers the deriving code's own source,
so two derivers that write byte-identical tiles stamp differently. That is the
point: changing how a level is reduced has to invalidate every pyramid, and a
stamp that watched only the archive would quietly keep serving the old
derivation.

The consequence is that stamp identity across two derivers is impossible by
construction. What is proven instead is the tiles and the plan where they are
observable: the stamp arithmetic and the register's promises in
`internal/generate/tiles`, the drawn level held to the capture's own recorded
hashes on every derivation (§4.4), the whole pipeline run twice over one
archive writing the same bytes (`cmd/atlas/pipeline_test.go`), and the corpus
tile inventories, which carry a content and a decoded-pixel digest for every
tile of every corpus pyramid (`testdata/corpus`). `docs/stamps.md` carries
the reasoning.

### 4.4 The drawn level

Most pictures in the corpus arrive as tiles from somebody's tile server, and a
pyramid's deepest level is those tiles copied through. A city has no tile
server: a municipal open-data hub publishes geometry, and the offline invariant
forbids reaching for anybody else's raster. So a city's deepest level is
**drawn**, from the geometry the city publishes about itself, and every
shallower level folds down from it exactly as any other pyramid's does. Nothing
downstream of `Derive` can tell a drawn level from a copied one, which is the
design.

`internal/generate/tiles/basemap` is the renderer, and it draws one tile from
one small vocabulary.

- **Roles, not datasets.** A shape says what it is to the ground — `parcel`,
  `park`, `water`, `street`, `trail`, `boundary` — and nothing about where it
  came from, so no publisher's vocabulary reaches the renderer (issue #5 §5.1).
  A role the style table does not know draws nothing, rather than being given an
  invented look.
- **The z-order is the drawing's only opinion about layering:** parcel texture
  first, then ground covers, water over parks so a lake punches through one,
  streets over water so a bridge reads as a bridge, trails above streets, and
  the boundary last as the one accent colour. Within a role nothing is ordered:
  a role's shapes accumulate into a single path and the rasterizer saturates, so
  they are unioned rather than painted over one another.
- **Widths are world-true.** The style table's widths are spelled at local zoom
  6 and scale with depth, so a street keeps its width on the ground however deep
  the pyramid goes — which, folded back down, is one width on the screen. A
  shape's emphasis scales its own stroke, so an arterial is a street drawn wider
  rather than a role of its own.
- **Ground is rings; linework is capsules.** Rings clip Sutherland–Hodgman, the
  first ring ground and every ring after it a hole — a positional convention,
  because publishers do not reliably wind holes the way GeoJSON asks. Lines clip
  Liang–Barsky a segment at a time, and each surviving segment joins the path as
  its own closed capsule: the segment widened to the stroke, with a semicircular
  cap at each end. Every capsule winds the same way, so capsules overlapping at a
  vertex union into one continuous line with a round join for free. There is no
  stroker, no join table and no miter limit — it is the cheapest stroker that
  never shows a seam.
- **Eight pixels of bleed.** Both clippers cut against a window bled eight tile
  pixels past the tile's own edge, so every cut lands outside the picture and two
  neighbours drawn independently continue each other exactly. A dashed stroke
  keeps its phase across the seam too: the rhythm advances by the whole of every
  segment whether or not its middle was drawn here, and a segment whose head was
  cut off begins at the phase its lost lead had already carried.
- **Opaque truecolour PNG**, encoded at the slowest and smallest setting. A tile
  has no transparency to carry, so the alpha channel is dropped rather than
  spent on every pixel of every city; the setting is fixed rather than a knob,
  because a knob is a way for two runs of one pipeline to disagree.

**The curation is the source's, and it is the same curation.** A dataset's role
and optional emphasis travel beside the rest of that layer's judgement in
`generate/sources/arcgishub`, unread by translation, so the drawing and the
legend cannot drift into two tables. What a source states is a `doc.Drawing` on
the lens: shapes in world pixels, projected through the very window the pins are
projected through, so the raster and the features cannot disagree about where
the ground is. It is stated *beside* the world's collections rather than derived
from them, because the two do not line up — every road centreline draws and none
of them is a legend row, only named trails are legend rows and every trail
segment draws, and emphasis varies row by row where a collection's features are
buckets of many rows.

**The capture is the witness.** The crawl that first drew a city wrote its
rasters into the archive, and a drawn lens's plan still names their content
hashes, so the deriver holds every tile it draws against what the capture
recorded and refuses the derivation by name if the two disagree. That is a
stronger thing to check than a suite could: not "these bytes look right" but
"these bytes are what a different implementation made of the same vectors",
checked on every derivation rather than in a test that could be skipped. It also
means a drawn level's plan — and therefore its stamp — is the ordinary one, with
the level's tiles listed and hashed like any other level's.

**And the renderer is part of the tool.** A derivation stamp covers the deriving
code because a change to how a level is made has to invalidate that level; a
city's deepest level is now made here, so `basemap`'s sources sit inside the
`ToolStamp` embed beside `plan.go`, `stamp.go`, `derive.go` and `draw.go`.

### 4.5 Warped variants

When two sources picture one ground, one raster is usually the finer — a
publisher's own map beside a wiki's rasterized in-game rendering — and a registry
that simply served the finer one would throw the other away. Instead the lesser
raster is **resampled into the finer one's world** and offered as one more
picture of the same ground. Nothing is discarded, both rasters answer to one
grid, and every feature lands on either.

Three decisions make a warp, and they happen in this order.

**Names are settled first.** Two sources capturing one volume name the same
ground the same thing, so their pyramids would land in one directory.
`tiles.Settle` gives *every* colliding plan its publisher's own path as a
suffix — all of them, never just the later one — so which pyramid is called what
does not depend on the order an archive listed its captures in. Cyberpunk's two
readings settle as `cyberpunk-2077__night-city__cbp` and
`cyberpunk-2077__night-city__cyberpunk-2077-night-city`. A warped variant is
named after the picture it aligns onto, so it can only be planned once the names
have stopped moving.

**The pairing is decided from the whole set.** One reading per ground — the
deepest, since a world's other lenses are alternate art already sharing its
window — and, among the readings of one volume, the one that draws its world at
the most pixels is the base every other is brought into. Resampling into a
coarser world would throw away detail that was captured. Only readings from
*different* sources are ever paired: a source that divided its own world into
several grounds did so deliberately. This policy lives in `cmd/atlas/warp.go`.

**The alignment is an input, not a derivation.** `tiles.PlanWarp` is handed the
fitted affine as six numbers. Fitting one stands on the names two readings share
and belongs to `internal/enrich/align` — the same fit the cross-source merge
folds features by, so a raster and the features drawn on it can never disagree
about where the ground is. Neither lane imports the other; `cmd/atlas` holds
both, and the seam is the one `adapt.go` opens for documents and volumes. Nothing
is duplicated to arrange it: the deriver needs a fitted transformation, not the
ability to fit one, so only the arithmetic of *using* one — apply, invert,
scale — sits beside `tiles.Affine`.

What the deriver then does:

- **The target zoom** is the base-frame zoom nearest the donor's real resolution
  after the alignment's own scaling, clamped to the base's deepest complete
  level: deep enough to keep what the donor drew, never so deep as to invent
  detail that was never there.
- **The deepest level is rendered, not copied.** Every pixel of every tile in the
  window the warped content falls in asks the inverse transformation where in the
  donor it came from, and samples the donor's deepest complete level bilinearly,
  clamped inside the tile that holds it. A tile no donor pixel reached is not
  written at all: coverage says nothing about it, and a reader falls back to the
  parent.
- **Everything shallower folds down** from that level exactly as any other
  pyramid folds, as a photograph.
- **The donor's own background** paints wherever the donor never drew, so a
  warped tile is opaque and a picture that simply ends does not end in black.
- **The bounds are the warped content's**, measured through the alignment and
  clipped to the world square — reported even when they come to the whole square,
  because what the alignment reached is a fact about this picture rather than a
  default that can be left out.
- **The plan lists one captured level**, the donor's deepest complete one, which
  is what the warp samples: a stamp naming the donor's other levels would rebuild
  for a change that could not reach these tiles.
- **The stamp carries the alignment** — the base's tile set, the target zoom, the
  base frame, and the six coefficients to nine decimal places — because the same
  donor through a different transformation is a different picture.

`cmd/atlas/warp_test.go` holds the pairing policy — the finer picture is the
frame, a source is never warped onto itself, readings that do not align stay
apart, and names settle before anything is derived — and the resampling
itself is the deriver's ordinary path, judged the way §4.3 says everything
else is.

---

## 5. Composition

`internal/generate/compose` turns a document into a volume.

1. **Resolve.** Each world's lenses are found in the tile set, its collections
   numbered, and the ground its contents cover measured. Every lens of a world
   must agree on the window it is cut from; one that does not would be a world
   drawn in two places at once. A world in a window that is not the shared one
   carries a `grid` in its payload.
2. **Order.** Worlds sort as families: a piece of a split sheet carries its
   sheet's position and follows it. Curation decides which world leads and
   whether a volume's worlds read forward or backward.
3. **Artwork.** Every point collection's icon key resolves against the
   document's icon set. Artwork nothing names is left behind.
4. **Conventions.** Composition speaks what it knows on the volume's behalf and
   then holds the whole thing to the registry, **producer-strict**: an
   unregistered `atlas.*` key, a value outside its vocabulary, or a key on the
   wrong entity fails the build here, one step before a bundle.
5. **Pack, stamp, write, install.**

### 5.1 What composition speaks, and what a source speaks

| key | written by | when |
| --- | --- | --- |
| `atlas.geometry.kind` | composition | mirrors the collection's kind, unless declared |
| `atlas.icon.kind` | composition | once the artwork has actually been resolved |
| `atlas.icon.outset` | composition | from curation |
| `atlas.collection.key` | composition | from curation, unless declared |
| `atlas.render.as` | the source | it is a reading of a publisher's own field |
| `atlas.geometry.*`, `atlas.geo.*` | the source | only a source knows what its ground is |
| `atlas.icon.std` | the source | naming a glyph is all a source without artwork can do |

Nothing composition writes overwrites what a source said.

**Standard icons are named by a source and resolved by composition.** A source
with no artwork of its own — a gazetteer that names two thousand craters and
draws none of them, a city's open data — names a library glyph through
`atlas.icon.std` instead of inventing one. Composition resolves the name against
the vendored library in `internal/generate/icons` and packs the glyph under a
provenance-spelling asset name (`std--maki-mountain.svg`), so a source's own
drawing of a thing can never be shadowed by a generic one and a bundle's icon
listing reads honestly. A declaration the library cannot answer fails the build.

Resolving a name is not choosing one. Deciding that a collection which named
nothing should carry a standard glyph — and which — is a judgement made after two
sources have been folded together, and it belongs to the enrich lane's
`stdicons` enricher (issue #5 §5.3). The two meet at the attribute.

### 5.2 Splitting

A sheet that holds several separate places is **declared in curation, never
detected**: guessing wrong divides a whole map into pieces nobody asked for.
Two modes: `worlds` gives each piece its own entry in the picker, `lenses`
keeps one world and offers the pieces one at a time.

The splitter reads only the interchange document's own vocabulary, so a second
source describing the same sheet the same way splits the same way. A **piece**
is one top-level area — an area feature nothing is the parent of — together with
its descendants and the point features that name one of them as their member.

1. **Plan.** Every area walks up its parent chain to its root. A piece's extent
   is the ground its areas outline, projected into world pixels; the points
   assigned to it do not stretch it. A point belonging to no area fails the
   build, because it would vanish when the sheet came apart.
2. **Grow.** Each extent widens by up to 256 world pixels into the empty space
   around it, stopping halfway to whatever it would run into, so a title printed
   beside a piece travels with the piece it names. Two pieces that grew into the
   same diagonal corner are eased apart along whichever axis they overlap least,
   and neither is ever pushed back inside the ground it grew from.
3. **Rehome.** A point whose position lies outside the piece that claims it, and
   inside exactly one other, moves. Where it sits is not in doubt; the claim is
   treated as the mistake it is. An ambiguous point stays where it was claimed.
4. **Order.** Lenses read top to bottom, so the sky comes before the ground
   beneath it. Separate worlds lead with the largest, so a sheet's main map heads
   its insets.
5. **Cut.** Into worlds: the first piece keeps the sheet's identity and every
   other becomes `<sheet title> — <area title>` under `<sheet slug>-<slug of the
   area title>`, carrying `parent`. Into lenses: one lens per piece, named after
   its area, and every feature carries the piece it belongs to as its `shard`. In
   both modes the piece's own outline is dropped — once an area has been used to
   cut a world out of a sheet, drawing it again just traces the edge of what the
   reader is already looking at — and a point collection left empty goes with it.

The lenses of every piece draw from the same pyramid; only the window onto it
changes. A piece's `bounds` is the grown window and its `surface` is the ground
it actually covers, clipped to that window, because those are not the same
rectangle and anything measuring a world rather than drawing it wants the
ground.

### 5.3 The payload

Three parts, because they are wanted at three different moments.

- `worlds/<slug>.json` — lenses and the ordered collections array. Point
  collections carry no inline features; shape collections inline theirs whole.
  **Point collections are written first**, so a packed location's owner is the
  ordinal of its collection among the points.
- `worlds/<slug>.bin` — `ATLASLOC` v3, the point features packed.
- `worlds/<slug>.text` — prose, links and per-feature attributes, keyed by
  feature id as a decimal string. A point earns an entry for prose, links or
  attributes; a shape earns one for prose alone and marks `hasText`, keeping
  its attributes inline because a card needs them the moment ground is asked
  about.

Its field names, its field order, and which fields are omitted when empty all
feed the stamp, so the shape is frozen with the format version.

Every world opens a **provenance account** — where its ground came from, and
what it held when it got here — merged with anything or not. A single-source
build carries exactly one entry, marked `origin`. The ledger fields the enrich
lane adds extend that entry in place, after `added`, so an origin account's
bytes never move.

### 5.4 Stamping and writing

The stamp is taken over **named parts**, sorted, one `"<name> <hash>"` line
each, joined by newlines, SHA-256:

```
atlas.json              hash of the manifest as it stands before its own stamp
                        and creation time are filled in — the revision is in
worlds/<slug>.json      hash of the payload bytes
worlds/<slug>.bin
worlds/<slug>.text
tiles/<pyramid>         the pyramid's derivation stamp, not its bytes
icons/<name>            hash of the artwork
```

`createdAt` is the **newest capture time across the volume's worlds**, never
the build clock. The name is `<slug>-<YYYYMMDD>-<stamp12>.atlas`. Building the
same archive anywhere, at any time, yields the same version and the same file
name, which is the whole reason a directory of these files can stand as a
registry — and why a build already present is left untouched.

The write: the archive is built under a temporary name that does not end in the
bundle extension, so a reader scanning the registry mid-write passes over it;
it is renamed into place, so the name appears whole or not at all; and then it
is reopened from disk and validated, because the file that will serve is the
copy and it is the copy's promises that matter.

Entry order inside the zip: the manifest, then each world's three parts in world
order, then the pyramids in sorted local name order (walked lexically, so level
`10` precedes level `2`), then the icons in sorted name order. Tiles and packed
locations are **stored uncompressed** so a reader serves them as byte ranges.

### 5.5 The policy revision

`compose.PolicyRevision` orders builds of the same capture: the data has not
moved between them, but what the lane makes of a capture has. Among equal
captures a registry serves the highest revision. It is part of the stamp, so
bumping it supersedes the builds already in every library. Its changelog is in
the constant's doc comment.

---

## 6. Curation

`internal/generate/curation/curation.json`, embedded so a build carries its own
curation and a composed bundle cannot depend on what was on the operator's
disk. Every key is spelled in Atlas's vocabulary — a volume slug, or
`"<volume>/<world>"` — never in a source's identifiers.

```jsonc
{
  "schema": 1,
  "window":   { "sourceZoom": 13, "firstTile": 4064 },
  "worldOrder": {
    "preferred":   { "<volume>": ["<world>", …] },
    "newestFirst": { "volumes": ["<volume>", …] }
  },
  "iconOutset": {
    "byVolume": { "<volume>": "light" | "dark" },
    "byWorld":  { "<volume>/<world>": "light" | "dark" }
  },
  "shard": {
    "worlds": { "<volume>/<world>": "worlds" | "lenses" }
  },
  "collectionEquivalents": {
    "<volume>": { "<icon key>": "<shared name>" }
  }
}
```

Every section carries its reasoning beside its data, under keys the reader
ignores (`what`, `why`, `byVolumeWhy`). A value that is not the shape a section
expects is commentary, not an entry.

| table | what it decides |
| --- | --- |
| `window` | the source tile window a world is cut from unless it declares one of its own. A property of the corpus, not of the format. |
| `worldOrder.preferred` | a volume's primary world, ahead of its secondary areas. Unlisted worlds follow in title order. |
| `worldOrder.newestFirst` | volumes whose worlds are dated captures of one ground: a version history opens on the present. |
| `iconOutset` | which rim a world's markers wear against its art. The raster decides which outline reads, so it is declared. A world's entry wins over its volume's. |
| `shard` | the sheets that hold several separate places, and what to do with them. |
| `collectionEquivalents` | where two sources spell one concept differently, keyed by artwork key. Composition writes the shared name as `atlas.collection.key`, so the payload carries the merge identity and a later merge reads only the attribute. |

Every table is keyed in Atlas's vocabulary — including outsets and shard
declarations, keyed by volume and world slugs rather than by a publisher's
numeric map ids, so a curation entry is readable without the publisher's
database in front of you and movable to a second source describing the same
ground.

---

## 7. The CLI

```
atlas crawl     -archive DIR -source NAME TARGET [-n]
atlas tiles     -archive DIR -output DIR [-force] [volume…]
atlas compose   -archive DIR -tiles INDEX [-bundles DIR] [-n] [volume…]
atlas translate -archive DIR [-volume SLUG] [-artwork] [-list]
```

`crawl` is the network-touching, hand-run step: §3.2. `tiles` folds captured
frames into the pyramids composition packs, carrying over every pyramid whose
captures have not moved.

`compose` builds every volume the archive holds a registered source for, or
only the ones named. `-n` composes and stamps without writing. With no
`-bundles`, it installs into the application's own library.

`translate` prints an interchange document to stdout — the debugging window
into the first half of the lane, answering the question composition cannot:
what did the source actually make of these bytes? `-list` names every volume
the archive registers, as `source · title · directory`; the slug `-volume`
takes is the one the run logs as `volume=`.

Both write their event stream to stderr, so piped stdout stays clean. See
[logging.md](logging.md).

### 7.1 Where the inputs are

Neither the capture archives nor the derived tile sets are in git — they are
large, they are somebody else's bytes, and they are staged in the working copy
instead. The repository's own copies are gitignored and are what every default
points at:

| input | staged at | override |
| --- | --- | --- |
| the games corpus archive | `crawl/fmg-archive` | `ATLAS_ARCHIVE_DIR` |
| the proof city's archive | `crawl/bend-or/fmg-archive` | `ATLAS_CITY_ARCHIVE_DIR` |
| the included Earth's archive | `crawl/blue-marble/fmg-archive` | none — `make included-earth` names it |
| the derived tile sets | `tiles/`, register at `tiles/index.json` | `ATLAS_TILES_INDEX` |

So the whole lane, over one volume, is three commands:

```sh
atlas translate -archive crawl/fmg-archive -volume tunic         # look at it
atlas compose   -archive crawl/fmg-archive -tiles tiles/index.json \
                -bundles <a scratch registry> tunic              # build it
```

Without `-bundles` the build installs into the application's own library, which
is a thing to do deliberately rather than while reading a document.

`atlas tiles -archive crawl/fmg-archive -output tiles` re-derives the pyramids
into the staged set, and is only needed when the archive has moved: a pyramid
whose captures have not moved is carried over untouched (§4.2).

**Deriving into a *fresh* output directory does not reproduce the staged set's
stamps.** A derivation stamp covers the deriving tool's own source (§4.3), so
re-deriving the same tiles under a different build of the tool stamps
differently — which changes the bundle's stamp, and therefore its file name.
The tiles are identical; the accounting of how they were made is not.
Composing against the staged `tiles/index.json` reproduces the builds already
in circulation name for name; composing against a freshly derived index is a
correct build of the same volume under a different name. `docs/stamps.md`
carries the reasoning.

---

## 8. What is proven

How the lane is judged overall is [testing.md](testing.md); every suite below
runs under `make test`, hermetically, over stated inputs and the committed
corpus.

| claim | held by |
| --- | --- |
| a source reads what its publisher actually serves | `internal/generate/sources/nasatrek`, over the committed capture at `testdata/corpus/translators/`; every other source over stated captures of its own shape, refusals included |
| the city translates exactly — the window, the buckets, the membership join | `internal/generate/sources/arcgishub` (`TestCityTranslatesExactly`) |
| the drawn level is deterministic and honest about its edges | `internal/generate/tiles/basemap`: same shapes, same bytes; holes, bleed, dash phase, opaque truecolour |
| the whole pipeline writes the same bytes twice | `cmd/atlas/pipeline_test.go` (`TestPipelineWritesTheSameBytesTwice`): a synthetic archive — two readings of an invented game and one crawl day of the proof city — run through the shipped stages, twice, byte-identical |
| a drawn tile is held to its witness | `cmd/atlas/pipeline_test.go` (`TestADrawnTileIsHeldToItsWitness`) — the §4.4 refusal, exercised |
| the warp pairing policy (§4.5) | `cmd/atlas/warp_test.go` |
| splitting, ordering, conventions, identifiers | `internal/generate/compose` |
| what a composed bundle holds, tile for tile | the corpus extractions (`testdata/corpus/bundles/`), read by `format/bundle`'s corpus tests and the render lane's pyramid tests |

The staged capture archives and tile sets (§7.1) are maintainer inputs, not
test inputs: no required test reads them, and `make corpus-smoke` is the
maintainer's own deep check over a real installed library
([testing.md](testing.md)).
