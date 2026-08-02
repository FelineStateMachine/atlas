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
  "lenses": [ { "name": "Default", "tileSet": "tunic/world/default-v2" } ],
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

### 1.2 The decisions, and how they differ from what came before

The reference implementation's interchange document was **MapGenie's API
response**, transcribed as Go structs, that every other translator forged
itself into. Everything below is a departure from that.

| | Reference tree | Clean room |
| --- | --- | --- |
| whose schema | MapGenie's wire format, `snake_case` | Atlas's own, `camelCase` |
| structure | `groups → categories → locations`, with `regions` standing apart | one ordered `collections` array; points, paths and areas are the same kind of citizen |
| a group | a container | a heading string on a collection |
| extensions | `atlas_attrs`, `atlas_collections`, `atlas_collection` — fields squatted on somebody else's namespace | `attrs`, and collections declared like anything else |
| render policy | `display_type`, a publisher's legacy field carried the whole way and read at composition | the source speaks `atlas.render.as` once, and the field never travels |
| coordinates | numbers *or* quoted strings, tolerated everywhere | `{lat, lng}` floats; a source's spelling tolerance is the source's business |
| absence | `*int64` for `region_id` and `parent_region_id` | `0`, everywhere, matching the wire's own reading of zero |
| icons | a key implying a file probe into an archive directory at composition time | the artwork travels in the document |
| lens detail | the publisher's claimed zoom range and bounds | name and tile set only; what a bundle promises about a raster is what was actually derived |
| provenance | recovered from the archive's directory names | `source` and a per-world `capture` on the document itself |
| links | resolved at composition, with a MapGenie URL pattern compiled into it | resolved by the source, because a link syntax is a publisher's |
| the world window | half from a hardcoded constant, half from the tile index | curation names the shared window; a pyramid names its own |

Two things were **kept** because they were never MapGenie's:

- The collection/feature model itself — a collection has a key, a title, a
  legend heading, a kind, visibility, attributes and features; a feature has an
  identity, a place or a geometry, nesting, prose, links and attributes. This
  was already the source-neutral schema hiding inside the old normalizer.
- Translation at **read time**, over an archive of source-native bytes. An
  editorial change replays over years of captures instead of re-crawling them.

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
- **Gates are named.** The reference tree's source gates carry over: IGN
  refuses embedded-MapGenie maps, Piggyback refuses unverified transforms,
  ArcGIS refuses an uncurated city. The MapGenie reader refuses a capture whose
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

### 2.4 Adding a sixth source

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
9. **Prove it.** A translator fixture test in `golden/pipeline` comparing what
   your document *means* against the reference material, and — if the archive
   holds a volume only your source reads — a row in `singleSource` so the whole
   bundle is reproduced end to end.

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
<root>/archive.json                                the volume register
<root>/<volume>/game.json                          the world register
<root>/<volume>/icons/<key>.svg|.png               collection artwork
<root>/<volume>/<world>/snapshots/index.json       the capture index
<root>/<volume>/<world>/snapshots/map/<hash>.json  the capture bodies
<root>/<volume>/<world>/tiles/index.json           per-tile records
<root>/<volume>/<world>/tiles/set-<id>/<z>/<x>/<y>.<ext>
```

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
the next wave's question. Nothing new inherits this naming.)*

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

### 4.2 What the deriver must promise

*(Derivation itself is the next wave. The contract it must fill in, carried
from the reference implementation:)*

- **Frame discovery.** A world is a 32-tile square. Local zoom 0 is the source
  zoom whose window collapses to one tile; local coordinates are source
  coordinates less the window's first tile.
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
- **Incrementality.** A pyramid whose stamp matches the register's previous
  entry, whose asset path is unchanged and whose directory still exists, is
  carried over untouched.

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

These tables were extracted from tables in the reference tree's program text,
and their content is golden-identical to it. Two were re-keyed: outsets and
shard declarations were keyed by upstream numeric map ids, which made a
curation entry unreadable without the publisher's database in front of you and
unmovable to a second source describing the same ground.

---

## 7. The CLI

```
atlas compose   -archive DIR -tiles INDEX [-bundles DIR] [-n] [volume…]
atlas translate -archive DIR [-volume SLUG] [-artwork] [-list]
```

`compose` builds every volume the archive holds a registered source for, or
only the ones named. `-n` composes and stamps without writing. With no
`-bundles`, it installs into the application's own library.

`translate` prints an interchange document to stdout — the debugging window
into the first half of the lane, answering the question composition cannot:
what did the source actually make of these bytes?

Both write their event stream to stderr, so piped stdout stays clean. See
[logging.md](logging.md).

---

## 8. What is proven

`golden/pipeline` composes the plain-MapGenie bundle fixture from archived
captures and holds the result against every extraction the reference build was
captured into: part hashes, the canonicalized manifest, the world payload, the
unpacked locations, the deferred prose, the icon set, the tile inventory and the
archive's entry order. Canonical-content equality is mandatory; stamp identity
is tracked as an aspiration.

Today the fixture reproduces **byte for byte**: the same stamp, the same file
name, the same 8,047,414 bytes, the same SHA-256.

The tests are gated on the two inputs that are deliberately not in git — the
capture archive and the derived tile set — and skip with an explanation when
neither `ATLAS_ARCHIVE_DIR`/`ATLAS_TILES_INDEX` nor the repository's own
gitignored copies are present.

The harness's `generate-enrich` gate stays **skipped**: its contract is
`generate ⊕ enrich` over every bundle fixture, and enrichment does not exist
yet, so the merged, split-sheet, lens-sharded and city fixtures cannot be
reproduced by anything. The single-source half runs as an ordinary test in the
meantime.
