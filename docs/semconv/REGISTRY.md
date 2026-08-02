# Atlas semantic conventions — registry v2

The shared language of the format: attribute keys that carry display and
geometry meaning from a producer, through composition, into the bundle, and
out to every reader. `format/semconv` is the executable twin of this
document, and `TestRegistryAgreesWithItsDocument` holds the two to the same
list of keys, entities, and stability tiers.

A bundle declares the vocabulary it was written against in its manifest:
`"conventions": 2`. The number moves only when an existing key's meaning
breaks; new keys arrive as `experimental` and earn `stable`. v2 is such a
break: it collapsed the v1 entities `zone`, `category`, and `location` into
`collection` and `feature` when the format unified them — every kind of thing
on a world is a feature, every named set of them a collection — and renamed
`atlas.category.key` to `atlas.collection.key` to match.

## Entities

| Entity | What it is | Where its attributes ride |
|---|---|---|
| `bundle` | one `.atlas` file: one volume | reserved; no key attaches here yet |
| `world` | one ground within the volume | `worlds/<slug>.json` → `attrs` |
| `collection` | one ordered group of features | `worlds/<slug>.json` → `collections[].attrs` |
| `feature` | one point, path, or area | inline `features[].attrs`, or `worlds/<slug>.text` → `<id>.a` |

## Attributes

| Key | Entity | Values | Stability | Meaning |
|---|---|---|---|---|
| `atlas.geometry.kind` | collection | `point` \| `path` \| `area` | stable | What shape of thing the collection holds. Declared once per collection; every feature in it is that kind, and readers pick rendering and UX by this key instead of sniffing geometry types. |
| `atlas.label.policy` | collection | `always` \| `quiet` | experimental | Whether an area collection's features wear their names on the map always, or quietly — only on highlight, selection, or an explicit reveal. Area collections only; absent means `always` for areas, and paths are always quiet. |
| `atlas.render.as` | collection | `pin` \| `text` | stable | How a point collection's features draw: markers, or floating text labels. Absent means `pin`; no payload carries a display-type field, and only ingestion still reads one to speak this key for old captures. |
| `atlas.icon.std` | collection | `set/name`, e.g. `maki/mountain` | stable | Standard-library icon for a collection without artwork. Resolved to embedded bytes at build time; a reader only sees the resolved asset. |
| `atlas.icon.kind` | collection | `glyph` \| `picture` | stable | Whether the icon asset is a monochrome glyph the reader tints, or a picture drawn as-is. Names what a file suffix used to imply. |
| `atlas.icon.outset` | world | `light` \| `dark` | stable | The rim a world's markers wear to stay legible against its art. |
| `atlas.geometry.surface` | world | `plane` \| `sphere` | stable | What the raster pictures. Absent means `plane`. |
| `atlas.geometry.projection` | world | `equirect` | stable | How a sphere was flattened. Required when surface is `sphere`. |
| `atlas.geometry.equirect.px` | world | `x,y,w,h` (world px) | stable | The raster window the projection fills. |
| `atlas.geometry.equirect.deg` | world | `west,north,east,south` (degrees) | stable | The ground that window pictures. |
| `atlas.geometry.mercator.px` | world | `x,y,w,h` (world px) | experimental | The raster window a Web-Mercator cut fills. Where `equirect` declares y linear in degrees, `mercator` declares y linear in projected latitude — `asinh(tan lat)` — the flattening a real-world tile window actually is. |
| `atlas.geometry.mercator.deg` | world | `west,north,east,south` (degrees) | experimental | The ground the Mercator window pictures, in degrees at its edges. |
| `atlas.geometry.body` | world | slug, e.g. `mars` | experimental | The body pictured. |
| `atlas.geometry.radius_km` | world | decimal string | experimental | The body's mean radius. |
| `atlas.geo.lat` | feature | decimal degrees | experimental | True planetary latitude as published (planetocentric). Provenance and card material; rendering derives from the world-level mapping. |
| `atlas.geo.lon` | feature | decimal degrees | experimental | True planetary longitude as published (east-positive). |
| `atlas.hydro.huc12` | feature | twelve digits | experimental | The USGS twelve-digit hydrologic unit — the subwatershed — the feature's ground lies wholly within, from the national Watershed Boundary Dataset. A feature spanning subwatersheds carries no key rather than a misleading one. |
| `atlas.stroke.width_px` | collection | positive decimal (world px) | experimental | The ground width of a path collection's features: a trail is a line and a weight, and the weight lets a reader draw the path as one continuous stroke. |
| `atlas.collection.key` | collection | slug | experimental | A collection's merge identity across sources. Absent, the icon key stands in. |

## Policy names (never written to a payload)

| Name | Meaning |
|---|---|
| `atlas.note.text` | A feature's description, as a merge policy table and its ledger name it, so "which description wins" is decided and recorded in the same vocabulary as everything else. It is deliberately unregistered: `Validate` refuses it on any entity. |

## Rules

- **Producer-side (strict):** any `atlas.*` key not in this registry, attached
  to the wrong entity, or carrying a value outside its vocabulary **fails the
  build**. `semconv.Validate` and `semconv.Check` are that gate.
- **Reader-side (lenient):** unknown keys are ignored. A bundle is never
  refused over conventions. `semconv.EntityOf` reporting `false` is a cue to
  skip an attribute, not to fail.
- Keys outside the `atlas.` namespace are not this registry's business and
  pass through untouched.
- `sphere` requires `projection` and both `equirect.*` mappings; the declared
  mapping must invert — a packed position run backward through it recovers the
  source's published coordinates.
- `atlas.icon.std`, once declared, must be resolved to a real icon asset by
  the time the bundle is written.
- A collection's `atlas.geometry.kind`, when declared, must agree with the
  `kind` field on the wire.
- `atlas.label.policy` attaches only to area collections; a declaration on any
  other kind fails validation.
