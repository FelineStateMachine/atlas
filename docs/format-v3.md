# The `.atlas` format — version 3

**Status: normative.** This document specifies the `.atlas` container as it is
frozen at format version 3, semantic conventions version 2. It is written to
be sufficient on its own: an implementer who has never seen Atlas the
application should be able to write a conforming reader from this document and
nothing else. Where this document and any implementation disagree, take it as
a defect in one of them and say so.

The reference implementation is `format/bundle` and `format/semconv`, which
depend on the Go standard library alone. The attribute vocabulary has its own
normative document at [`semconv/REGISTRY.md`](semconv/REGISTRY.md).

---

## 1. Vocabulary

Five words carry the format. They are used precisely throughout.

| Word | Meaning |
|---|---|
| **Volume** | The subject of one `.atlas` file: a game, a planet, a city. One file, one volume. |
| **World** | A distinct ground within a volume that a reader can stand on. For a game these are its maps; for a captured city, its dated revisions. |
| **Lens** | One raster tile pyramid picturing a world. Worlds are grounds; lenses are pictures of them. A world may have several lenses — map styles, dated captures, warped variants. |
| **Collection** | An ordered group of features within a world. Every collection declares one **kind**: `point`, `path`, or `area`. |
| **Feature** | One point, path, or area, with a title and attributes. Every feature is exactly one kind. |

A **build** is one packing of a volume's data. Builds of one volume share a
slug and are told apart by their **stamp**. Nothing else in the format is a
noun.

The format carries no trace of where a volume's data came from. A producer
flattens whatever its sources look like into this shape; a reader only ever
sees this shape. The one exception is the optional merge ledger (§6.6), which
is provenance a reader may show and must never depend on.

---

## 2. The three invariants

These hold for every conforming producer and reader. Everything else in this
document serves them.

### 2.1 Determinism

The same input archive, built anywhere, at any time, on any machine, produces
the same stamp and therefore the same file name.

Concretely: the stamp is an order-independent SHA-256 over named part hashes
(§8.1); the file name is `<slug>-<YYYYMMDD>-<stamp12>.atlas` (§8.2);
`createdAt` is the **newest capture time** across the volume's worlds, never
the build time; and the build ordering is `createdAt`, then policy `revision`,
then `stamp` (§8.3).

### 2.2 Offline purity

**No runtime URL appears anywhere in a payload.** A bundle serves with zero
network access, forever. A producer must reject a build whose
`worlds/<slug>.json` or `worlds/<slug>.text` contains the substring `http://`
or `https://` (§9.2). Cross-references that a source published as URLs are
resolved at build time to in-volume identifiers, or dropped.

### 2.3 Asymmetric conventions

Producers are strict; readers are lenient — with exactly one exception.

- **Producers**: an `atlas.*` attribute the registry does not know, or one
  attached to the wrong entity, or one carrying a value outside its
  vocabulary, **fails the build**.
- **Readers**: unknown attribute keys are **ignored, never refused**. A bundle
  written by a newer pipeline still opens in an older reader.
- **The exception**: an unknown `formatVersion` is **refused outright**. A
  reader that guesses at a layout it does not know is worse than one that says
  so.

---

## 3. The container

A `.atlas` file is a **zip archive**. It holds:

```
atlas.json                          the manifest — deflated
worlds/<world-slug>.json            world payload — deflated
worlds/<world-slug>.bin             packed point locations — STORED
worlds/<world-slug>.text            per-feature prose — deflated
tiles/<pyramid>/<z>/<x>/<y>.<ext>   raster tiles — STORED
icons/<name>                        icon assets — deflated
```

### 3.1 Compression policy

**Tiles and packed locations are stored uncompressed.** They are already dense
— JPEG, PNG, WebP, packed binary — and a stored zip entry can be served as a
straight byte range out of the archive, where a deflated one must be inflated
on every read. This is the one performance property the container is designed
around, and a conforming producer must honour it.

JSON payloads and icons are deflated: text shrinks severalfold and is read
whole.

### 3.2 Entry ordering

`atlas.json` is written first, so a listing of the archive leads with what the
file is. A reader must find entries by name and must not depend on order for
anything else.

### 3.3 Entry names

Entry names are slash-separated relative paths of plain segments. A name must
not be empty, must not begin or end with `/`, and no segment may be empty,
`.`, or `..`. A reader that unpacks a bundle to disk is entitled to assume
this held, and a producer must enforce it.

### 3.4 Slugs

Volume and world slugs are identifiers used unescaped as both path segments
and URL segments. A slug matches `[a-z0-9_-]+` and must not begin with `-` or
`_`. Nothing else is admitted: everything a hostile name could smuggle needs
more.

---

## 4. `atlas.json` — the manifest

The manifest is what a bundle says about itself. It is read through to list a
volume without touching any payload.

**A conforming reader must refuse an archive with no `atlas.json`.** The
reference implementation also refuses one whose uncompressed size exceeds 1
MiB, on the grounds that a manifest lists worlds and nothing per feature.

### 4.1 Schema

```json
{
  "format": "atlas-bundle",
  "formatVersion": 3,
  "conventions": 2,
  "volume": { "slug": "bend-or", "title": "Bend, Oregon" },
  "version": {
    "stamp": "ec3fe8c21cfec928d43a616eca97ebda08dc0ad7a970f531c677241bb5c70788",
    "createdAt": "2026-08-01T20:13:08.883648Z",
    "revision": 9
  },
  "tileGrid": { "sourceZoom": 13, "firstTile": 4064, "tileSize": 256, "size": 8192 },
  "worlds": [
    {
      "slug": "2026-08-01",
      "title": "2026-08-01",
      "center": { "lat": 0, "lng": 0 },
      "points": 45, "paths": 5, "areas": 16,
      "updatedAt": "2026-08-01T20:13:08.883648Z"
    }
  ]
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `format` | string | yes | Always `"atlas-bundle"`. A reader refuses any other value, so a stray zip renamed to `.atlas` fails by name rather than by whatever breaks first. |
| `formatVersion` | integer | yes | `3`. A reader refuses versions it does not know (§2.3). |
| `conventions` | integer | omitted when 0 | The semantic-conventions vocabulary the payloads were written against. `2` is current; absent means a bundle from before the conventions existed. A **declaration**, never a gate: a reader that knows a different vocabulary still opens the bundle. Validation is stricter about a bundle that declares (§9.4). |
| `volume.slug` | slug | yes | The volume's identity. Two bundles naming the same slug are two builds of the same volume, however their files are named. |
| `volume.title` | string | yes, non-empty | Display name. |
| `version.stamp` | hex string | yes, non-empty | Content fingerprint (§8.1). Two bundles with equal stamps hold the same data. |
| `version.createdAt` | RFC 3339 | yes, non-empty | The newest capture time across the volume's worlds. **Never the build time.** |
| `version.revision` | integer | omitted when 0 | Policy revision: which generation of build rules produced this file. Orders builds of one capture (§8.3). |
| `tileGrid` | object | yes | The window this volume's worlds are cut from (§4.2). |
| `worlds` | array | yes, non-empty | One entry per world (§4.3). Order is the offering order. |

### 4.2 `tileGrid`

| Field | Type | Meaning |
|---|---|---|
| `sourceZoom` | integer | The zoom level the source raster was captured at. |
| `firstTile` | integer | The tile column/row the captured window begins at, in the source grid. |
| `tileSize` | integer | Pixels per tile edge. `256` throughout. |
| `size` | integer | The world square's edge in world pixels. `8192` throughout. |

A world cut from a window of its own overrides `sourceZoom` and `firstTile` in
its own payload (§6.2).

### 4.3 `worlds[]`

| Field | Type | Required | Meaning |
|---|---|---|---|
| `slug` | slug | yes | Names the world's three payload entries and is the world's identity everywhere. Must be unique within the manifest. |
| `title` | string | yes, non-empty | Display name. |
| `parent` | slug | omitted when empty | The world this one was split out of, so an inset sorts with the sheet it came from. |
| `iconOutset` | string | omitted when empty | `light` or `dark`: the rim markers wear to stay legible against this world's art. Duplicated as `atlas.icon.outset` in the payload. |
| `center` | `{lat, lng}` | yes | Opening view, in the volume's own projection. |
| `points`, `paths`, `areas` | integer | yes | Feature counts by kind. Every feature is exactly one of the three, so a listing can say how much a world holds without opening it — and validation holds the payload to the promise (§9.3). |
| `updatedAt` | RFC 3339 | yes | This world's capture time. The manifest's `createdAt` is the newest of these. |

### 4.4 Manifest encoding is normative

The manifest's **encoded bytes** feed the stamp (§8.1), so its encoding is part
of the format, not an implementation detail:

- Field order is exactly as tabled above.
- `conventions` and `version.revision` are omitted when zero. **Every other
  field is always present**, including zero-valued ones (`"points":0`,
  `"center":{"lat":0,"lng":0}`, `"updatedAt":""`).
- Compact — no indentation, no trailing newline.
- HTML-sensitive characters in strings are escaped to their `\u` forms:
  `<` becomes `\u003c`, `>` becomes `\u003e`, `&` becomes `\u0026`. This is
  Go's `encoding/json` default and it is load-bearing — a volume titled
  `Hospitals & Care` is encoded with `\u0026` in every published bundle, and
  an encoder that emits a bare `&` computes a different stamp.

An implementation that re-encodes a manifest and gets different bytes will
compute different stamps and produce differently named files.

---

## 5. World payload entries

Each world splits three ways, by when its parts are needed:

| Entry | Read when | Holds |
|---|---|---|
| `worlds/<slug>.json` | the world opens | lenses, collections, inline shape geometry, world attributes, merge ledger |
| `worlds/<slug>.bin` | the world opens | every point feature, packed |
| `worlds/<slug>.text` | a feature's card opens | descriptions, links, per-feature attributes |

The split exists because descriptions were half the payload by weight and are
read one feature at a time, while point features are overwhelmingly numbers,
and numbers written as text cost several times what they measure. Shape
features are mostly geometry either way, so they ride the JSON inline.

---

## 6. `worlds/<slug>.json` — the world payload

```json
{
  "grid": { "sourceZoom": 5, "firstTile": 0 },
  "lenses": [ … ],
  "collections": [ … ],
  "attrs": { "atlas.geometry.surface": "sphere", … },
  "merged": [ … ]
}
```

| Field | Required | Meaning |
|---|---|---|
| `grid` | optional | Window override (§6.2). Present only on a world cut from a window that is not the manifest's. |
| `lenses` | yes, non-empty | The pictures of this world (§6.3). |
| `collections` | yes | **Order-significant** (§6.4). May be empty. |
| `attrs` | optional | The world speaking the shared conventions, entity `world`. |
| `merged` | optional | Provenance ledger (§6.6). A reader may show it and must never depend on it. |

### 6.1 Attributes generally

`attrs` objects appear on the world, on each collection, and on each feature.
They are flat `string → string` maps. Keys in the `atlas.` namespace are
governed by [`semconv/REGISTRY.md`](semconv/REGISTRY.md); keys outside it are
not the registry's business and pass through. **A reader ignores any key it
does not know** (§2.3).

### 6.2 `grid`

```json
{ "sourceZoom": 5, "firstTile": 0 }
```

Overrides the manifest's `tileGrid.sourceZoom` and `tileGrid.firstTile` for
this world. `tileSize` and `size` are never overridden.

### 6.3 `lenses[]`

One raster pyramid picturing this world.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | yes | Display name of the picture, e.g. `"Viking MDIM 2.1"`, `"Basemap"`. |
| `tiles` | string | yes | The pyramid's directory under `tiles/`. Several lenses may name one pyramid. |
| `minZoom` | integer | yes | Lowest zoom the pyramid holds. **`0` in every published bundle.** |
| `maxZoom` | integer | yes | Highest zoom the pyramid holds. |
| `fullZoom` | integer | yes | The highest zoom at which the pyramid is *complete*. Above it, tiles exist only where the capture reached, and `coverage` says where. |
| `sourceZoom` | integer | yes | The zoom this pyramid's source raster was captured at. |
| `formats` | array of string | yes | One file extension per level, indexed by `z − minZoom`: `formats[z − minZoom]` is the extension of every tile at zoom `z`. Length must equal `maxZoom − minZoom + 1`. |
| `bounds` | `{x,y,width,height}` | optional | The window of world pixels the pyramid's tiles cover. |
| `surface` | `{x,y,width,height}` | optional | The ground the world actually covers, where `bounds` is the window cut to draw it. On a piece of a split sheet the window is grown to take in a title drawn beside it, so anything dividing the world up measures `surface` and leaves no cell on margin. |
| `interpolate` | boolean | yes | `true` to resample smoothly (photographic rasters); `false` for nearest-neighbour (pixel art). |
| `background` | string | optional | CSS colour behind the raster, e.g. `"#14181d"`. |
| `shard` | integer | omitted when 0 | Which layer of a split world this lens draws. Features carry a matching `shard`. |
| `coverage` | object | optional | Sparse-level bitsets (§6.3.1). Absent for a level means that level is fully covered. |

#### 6.3.1 `coverage`

```json
"coverage": { "3": { "x": 1, "y": 0, "w": 7, "h": 8, "bits": "YDDc//v5cA==" } }
```

Keyed by **zoom level as a decimal string**. Each value describes which tiles
of that level were actually written:

- `x`, `y`, `w`, `h` — the bounding tile window at that level.
- `bits` — **standard base64** of a row-major bitset over that window, **least
  significant bit first within each byte**.

To test whether tile `(z, x, y)` exists:

```
c = x − coverage.x;  r = y − coverage.y
if c < 0 or r < 0 or c >= coverage.w or r >= coverage.h:  absent
i = r * coverage.w + c
present  ⟺  (bits[i >> 3] & (1 << (i & 7))) != 0
```

A level with no `coverage` entry is fully covered. A reader must consult the
bitset before requesting a tile: asking for one that was never written is a
wasted request, and the correct fallback is to draw the parent tile.

### 6.4 `collections[]` — order is significant

The collections array is the world's whole structure in one flat list.
**Its order is load-bearing**: the packed payload's `owner` column indexes it
(§7).

Point collections are written first and shape collections after, so a packed
location's `owner` is simply the ordinal of its collection among the points.
The array order is also the legend order.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | integer | yes | Stable identifier, unique within the world. |
| `title` | string | yes | Display name. |
| `group` | string | optional | A heading several collections sit under. |
| `kind` | string | yes | `point`, `path`, or `area`. |
| `icon` | string | optional | The collection's icon key, before resolution. |
| `iconAsset` | string | optional | The resolved icon's name under `icons/`. Must exist in the archive. |
| `iconPicture` | boolean | omitted when false | The asset is a picture drawn as-is rather than a glyph the reader tints. |
| `color`, `iconColor` | string | optional | CSS colours. |
| `visible` | boolean | yes | Whether the collection is drawn when the world opens. |
| `attrs` | object | optional | Conventions, entity `collection`. |
| `features` | array | optional | Inline features. **A point collection carries none** — its members ride the packed payload. Path and area collections carry theirs here. |

### 6.5 `features[]` — inline shape features

Present only on `path` and `area` collections.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | integer | yes | Unique within the world, across *all* kinds — one world never keys two things alike. |
| `title` | string | yes | Display name. |
| `subtitle` | string | optional | Secondary line. |
| `hasText` | boolean | omitted when false | This feature has an entry in `worlds/<slug>.text`. |
| `parent` | integer | optional | The id of a containing feature. |
| `center` | `{lat,lng}` | optional | A representative point, for framing and labelling. |
| `shard` | integer | omitted when 0 | Which layer of a split world this feature belongs to. |
| `geometry` | array | yes | GeoJSON-shaped geometry objects: `{"type": …, "coordinates": …}`. |
| `attrs` | object | optional | Conventions, entity `feature`. |

Geometry types must match the collection's kind:

| Kind | Admitted geometry types |
|---|---|
| `area` | `Polygon`, `MultiPolygon` |
| `path` | `LineString`, `MultiLineString` |
| `point` | *none* — points are never inlined |

Coordinates are in the volume's own world space (the same `lat`/`lng`
convention as `center`), not WGS 84. A world that declares a spherical surface
also declares the mapping from world space to true coordinates (§6.7).

### 6.6 `merged[]` — the provenance ledger

Optional. Present when a build folded more than one source together, or to
record where a single-source world came from.

```json
[{ "source": "ArcGIS Open Data", "slug": "arcgis-hub", "origin": true,
   "donorFeatures": {"point": 45, "path": 5, "area": 16}, "added": 0 }]
```

| Field | Meaning |
|---|---|
| `source` | Display name of the contributing source. |
| `slug` | The source's canonical name, so ledgers and tooling agree without translation. |
| `origin` | This entry accounts for the source the world itself came from. Its `donorFeatures` is the world's own tally at composition. |
| `donorFeatures` | `{point, path, area}` — the donor's whole offering, counted per kind. |
| `matched`, `added`, `addedShapes`, `adopted`, `held`, `rejected`, `enrichedCategories`, `alignment` | The merge's own account: what met an existing feature, what was new, what was held back and why. Producer-defined; a reader displays what it recognises. |

**A reader must never depend on the ledger.** It is provenance a curious
person reads, not data the picture needs.

### 6.7 Spherical worlds

A world whose raster pictures a sphere declares so in `attrs`:

```json
"attrs": {
  "atlas.geometry.surface": "sphere",
  "atlas.geometry.projection": "equirect",
  "atlas.geometry.equirect.px": "0,0,8192,4096",
  "atlas.geometry.equirect.deg": "-180,90,180,-90",
  "atlas.geometry.body": "mars",
  "atlas.geometry.radius_km": "3389.5"
}
```

`equirect.px` is `x,y,w,h` in world pixels; `equirect.deg` is
`west,north,east,south` in degrees. The mapping is linear in both axes:

```
x = X + (lon − West) / (East − West) * W
y = Y + (North − lat) / (North − South) * H

lon = West + (x − X) / W * (East − West)
lat = North − (y − Y) / H * (North − South)
```

West may exceed east numerically (a world centred on the antimeridian); the
span still reads west to east. `w` and `h` must be positive, and north must
differ from south and west from east.

A world that says nothing about its surface is a **plane**. A world declaring
`sphere` **must** also declare `projection` and both `equirect.*` keys (§9.4).

The `atlas.geometry.mercator.*` keys declare a Web-Mercator cut instead: `y`
is linear in the projected latitude, `asinh(tan lat)`, rather than in degrees.
They are experimental.

---

## 7. `worlds/<slug>.bin` — `ATLASLOC` version 3

Every point feature of a world, packed as parallel little-endian typed arrays.
The layout exists so a reader can build typed-array views directly over the
downloaded buffer, with no parsing and no per-feature allocation.

**All integers and floats are little-endian.** Let `N` be the location count.

| Offset | Size | Type | Column |
|---|---|---|---|
| `0` | 8 | bytes | Magic: ASCII `ATLASLOC` |
| `8` | 2 | `uint16` | Version. `3`. |
| `10` | 4 | `uint32` | `N`, the location count |
| `14` | 2 | — | Reserved, zero. Present so every column that follows is four-byte aligned. |
| `16` | `4N` | `int32` | **id** |
| `16 + 4N` | `4N` | `float32` | **lat** |
| `16 + 8N` | `4N` | `float32` | **lng** |
| `16 + 12N` | `4N` | `int32` | **member** |
| `16 + 16N` | `4N` | `int32` | **shard** |
| `16 + 20N` | `4(N+1)` | `uint32` | **title offsets** |
| `20 + 24N` | `2N` | `uint16` | **owner** |
| `20 + 26N` | variable | bytes | **title bytes** |

Total size is `20 + 26N + Σ len(title_i)`.

### 7.1 Columns

| Column | Meaning |
|---|---|
| **id** | The feature's identifier, unique within the world across all kinds. Signed 32-bit on the wire. |
| **lat**, **lng** | Position in the volume's world space. Single precision, which resolves far finer than a world is drawn. A conforming reader must expect coordinates to be float32-rounded. |
| **member** | The id of the **area feature containing this point**, or `0` for none. |
| **shard** | Which layer of a split world the point belongs to, or `0` when the world is whole. |
| **owner** | **The index of this feature's collection in the world payload's `collections` array.** This is the only collection identity a reader needs, and it is why that array's order is significant. `owner` must index an existing collection whose `kind` is `point`. |
| **title** | See below. |

### 7.2 Titles

The title offsets column holds `N+1` byte offsets into the title region,
relative to its start. Location `i`'s title is
`titles[offsets[i] : offsets[i+1]]`, as **UTF-8, not NUL-terminated**. Offsets
are non-decreasing; `offsets[0]` is `0` and `offsets[N]` is the length of the
title region. An empty title is a zero-length run, which is legal.

A conforming reader should verify that the offsets are non-decreasing and that
`offsets[N]` does not exceed the remaining bytes, because these are the only
values in the payload that can point outside it.

### 7.3 Version history

- **1** — the original packing.
- **2** — added the `shard` column. A world split into layers offers one at a
  time; without it every layer's locations drew over every other.
- **3** — moved meaning without moving a byte. `owner` now indexes the world
  payload's `collections` array rather than a flattened category order, and
  the fifth column reads as `member` — the containing area feature — where it
  previously named a region.

A reader refuses a version it does not know.

---

## 8. Stamps, names, and ordering

### 8.1 The stamp

A build's stamp is an **order-independent SHA-256 over named part hashes**.

1. For each part, the producer records a `name` and a `hash`. A part's hash is
   the lowercase hex SHA-256 of its bytes, except for tile pyramids, whose
   hash is the pyramid's own derivation stamp (which already names its source
   tiles and the tool that derived them, so tiles weigh into the stamp without
   being read).
2. Each part becomes the line `name + " " + hash`.
3. The lines are **sorted lexically** and joined with `"\n"`.
4. The stamp is the lowercase hex SHA-256 of that string.

The parts a producer records are:

| Part name | Hash of |
|---|---|
| `worlds/<slug>.json` | the encoded world payload |
| `worlds/<slug>.bin` | the packed locations |
| `worlds/<slug>.text` | the encoded text payload |
| `tiles/<pyramid>` | the pyramid's derivation stamp |
| `icons/<name>` | the icon's bytes |
| `atlas.json` | the encoded manifest **with `version.stamp` and `version.createdAt` both empty** |

That last row is the one non-obvious rule: the manifest cannot hash itself, so
the stamp is computed over the manifest as it stands *before* the stamp and
the capture time are filled in. `version.revision` **is** included, which is
what makes a policy bump a new stamp.

> **Constraint.** A part name must contain no space and no newline. Those are
> the record separators of the summed form, so a name carrying either makes
> the sum ambiguous — `"a b"` with hash `"c"` sums identically to `"a"` with
> hash `"b c"`. Archive entry names have never held one. Widening the
> separator would restamp every bundle in existence, so the constraint is kept
> rather than coded around.

### 8.2 The file name

A bundle installed in a registry directory is named:

```
<volume-slug>-<YYYYMMDD>-<stamp12>.atlas
```

- `YYYYMMDD` is the **first eight digits of `version.createdAt`**, taken by
  counting digits rather than parsing a time, so a name can be derived from a
  manifest that has not been validated. A `createdAt` with no digits yields no
  day segment.
- `stamp12` is the first 12 hex characters of the stamp, or the whole stamp if
  it is shorter.

The name is for people and for cheap existence checks. **It is never used for
ordering** — that is §8.3.

### 8.3 Build ordering

When two builds claim the same volume slug, `a` shadows `b` if:

1. `a.createdAt > b.createdAt` (string comparison — RFC 3339 sorts lexically);
   else
2. `a.revision > b.revision`; else
3. `a.stamp > b.stamp`; else
4. `a`'s locator (file path) sorts after `b`'s.

The comparison is **total**, so two scans of one library always agree. Step 2
is what lets a rebuild under new policy supersede the builds already in every
library: the data has not moved, but what a producer makes of it has. A build
from before `revision` existed reads as `0` and loses to any revised build.

---

## 9. Validation — what a producer must guarantee

A conforming producer runs all of these before publishing. A reader may run
them on import; it must not run them merely to open a bundle, since they cost
a pass over every payload.

### 9.1 Manifest

`format` is `atlas-bundle`; `formatVersion` is `3`; volume slug is valid and
titled; `stamp` and `createdAt` are non-empty; at least one world is listed;
every world slug is valid, titled, and listed once.

### 9.2 Offline purity

Neither `worlds/<slug>.json` nor `worlds/<slug>.text` contains `http://` or
`https://`, anywhere, as a raw substring.

### 9.3 Structure

- Every listed world has all three payload entries.
- The packed payload's location count equals `worlds[i].points`.
- Inline path features across all path collections total `worlds[i].paths`;
  inline area features total `worlds[i].areas`.
- Collection ids are unique within a world.
- Every collection's `kind` is one of `point`, `path`, `area`.
- A point collection carries no inline features.
- Every inline geometry type fits its collection's kind (§6.5).
- Every path collection declares `atlas.stroke.width_px`.
- `atlas.label.policy` appears only on area collections — only areas curate
  their labels.
- Every `iconAsset` names an entry that exists under `icons/`.
- Every location's `owner` indexes an existing collection whose kind is
  `point`.
- The world has at least one lens; each lens's `formats` has one entry per
  level; each level the lens claims holds at least one tile.

### 9.4 Conventions — only when declared

If `conventions ≥ 1`, additionally:

- Every `atlas.*` key on the world, on every collection, on every inline
  feature, and in every text entry is registered, attached to that entity, and
  carries a value its vocabulary admits.
- A world declaring `atlas.geometry.surface: sphere` also declares
  `atlas.geometry.projection`, and its `equirect.*` pair parses (§6.7).
- A collection declaring `atlas.geometry.kind` declares the same kind its
  `kind` field does.
- A collection declaring `atlas.icon.std` has a non-empty `iconAsset` — a
  standard icon must be resolved by the time the bundle is written.

A bundle that declares no conventions is held to none. **The strictness
belongs to the claim.**

---

## 10. The registry directory

A registry is a directory of immutable `.atlas` files. New builds land beside
old ones; **nothing ever mutates a published bundle**.

- **Scan** reads every `*.atlas` in the directory and takes a descriptor —
  slug, title, stamp, createdAt, revision, size, world count — from each
  manifest. A file that cannot be read is skipped, not fatal: one bad download
  should not take a library down with it.
- **Fold** picks the winning build per slug under §8.3. It is a pure function
  of the descriptors: order-independent, filesystem-free, and always the same
  answer for the same library.
- **Install** validates a candidate, copies it to a temporary name in the same
  directory, renames it into place under §8.2, then reopens and revalidates
  the installed copy — the copy is what will serve. A build already present
  under its own name is a no-op. Installing an old file can never overwrite a
  newer build; it just arrives already shadowed.
- The directory is scanned at launch and rescanned on import. It is not
  watched: a file dropped in externally appears at next launch.

### 10.1 `index.json`

A registry directory may carry an `index.json` listing every volume and every
build of it, newest first. **It is always derived from the files it sits with,
never the other way around**, so a hand-copied or hand-deleted bundle is
reflected by the next scan rather than contradicted by a stale listing. It is
always safe to delete.

```json
{"games":[{"slug":"mars","title":"Mars","versions":[
  {"file":"mars-20260801-68e141f26b1a.atlas",
   "stamp":"68e141f2…","createdAt":"2026-08-01T14:48:10.486177Z",
   "revision":9,"size":214003201,"maps":1}]}]}
```

Volumes sort by title; builds within a volume sort by §8.3; a volume answers
to its newest build's title.

> The wire keys `games` and `maps` are history showing through. The listing
> predates the volume/world vocabulary and was never renamed, because nothing
> reads it that would benefit and every tool that writes one would have to
> agree at once. They are kept for compatibility and mean *volumes* and
> *worlds*.

---

## 11. Format version history

| Version | What changed |
|---|---|
| **1** | The original container. Manifest spoke of `game`, `maps`, `pinCount`; payloads lived under `maps/`. |
| **2** | Renamed the format's concepts throughout — game→volume, map→world, variant→lens — in the manifest and the payload keys alike. Payloads moved to `worlds/`. |
| **3** | Unified what a world holds. The payload's separate `groups`, `zones`, and `pins` became one order-significant `collections` array of `point`, `path`, and `area` features; the manifest counts each kind instead of counting only pins; `ATLASLOC` moved to version 3 with `owner` indexing that array. |

Conventions versions move independently: `1` introduced the `atlas.*`
vocabulary; `2` collapsed the entities `zone`, `category`, and `location` into
`collection` and `feature` to match format 3, and renamed
`atlas.category.key` to `atlas.collection.key`.

**A reader refuses a `formatVersion` it does not know.** A library accumulates
history, so a registry scan is expected to pass over older builds rather than
fail on them.

---

## 12. Checklist for a new reader

In the order you would implement them:

1. Open the zip. Find `atlas.json`; refuse the archive without one.
2. Parse the manifest. **Refuse any `formatVersion` but 3.** Ignore fields you
   do not recognise.
3. List worlds from `manifest.worlds`. You now have a working volume listing
   without touching a payload.
4. To open a world: read `worlds/<slug>.json`. Keep the `collections` array
   **in order**.
5. Read `worlds/<slug>.bin`. Check the magic and version, then build views
   over the columns at the offsets in §7. Resolve each location's collection
   by `collections[owner]`.
6. Draw from `tiles/<lens.tiles>/<z>/<x>/<y>.<formats[z − minZoom]>`. Consult
   `coverage` before requesting anything; fall back to the parent tile.
7. Fetch `worlds/<slug>.text` lazily and index it by feature id as a string.
8. Read `attrs` through the conventions registry. **Ignore every key you do
   not know.**
9. Assume no network. If you find yourself constructing an outbound URL from
   payload data, the bundle is malformed or you have misread this document.
