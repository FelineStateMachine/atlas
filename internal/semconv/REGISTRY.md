# Atlas semantic conventions — registry v2

The shared language of the pipeline: attribute keys that carry display and
geometry meaning from a source's translator, through composition, into the
bundle, and out to every reader. Producers are held to this vocabulary at
build time; readers ignore what they do not know and never refuse a bundle
over it. `semconv.go` is the executable twin of this document, and a test
holds the two to the same list.

A bundle declares the vocabulary it was written against in its manifest:
`"conventions": 2`. The number moves only when an existing key's meaning
breaks; new keys arrive as `experimental` and earn `stable`. v2 is such a
break: it collapsed the v1 entities `zone`, `category`, and `location` into
`collection` and `feature` when the format unified them — every kind of
thing on a map is a feature, every named set of them a collection — and
renamed `atlas.category.key` to `atlas.collection.key` to match.

## Attributes

| Key | Entity | Values | Stability | Meaning |
|---|---|---|---|---|
| `atlas.geometry.kind` | collection | `point` \| `path` \| `area` | stable | What shape of thing the collection holds. Declared once per collection; every feature in it is that kind, and readers pick rendering and UX by this key instead of sniffing geometry types. |
| `atlas.label.policy` | collection | `always` \| `quiet` | experimental | Whether an area collection's features wear their names on the map always, or quietly — only on highlight, selection, or an explicit reveal. Area collections only; absent means `always` for areas (today's look), and paths are always quiet. |
| `atlas.render.as` | collection | `pin` \| `text` | stable | How a point collection's features draw: markers, or floating text labels. Absent, legacy `displayType` decides; absent both, `pin`. |
| `atlas.icon.std` | collection | `set/name`, e.g. `maki/mountain` | stable | Standard-library icon for a collection without artwork. Resolved to embedded bytes at build time; the app only sees the resolved asset. |
| `atlas.icon.kind` | collection | `glyph` \| `picture` | stable | Whether the icon asset is a monochrome glyph the viewer tints, or a picture drawn as-is. Names what the `.png` suffix used to imply. |
| `atlas.icon.outset` | world | `light` \| `dark` | stable | The rim a map's markers wear to stay legible against its art. |
| `atlas.geometry.surface` | world | `plane` \| `sphere` | stable | What the raster pictures. Absent means `plane` — every map before the planets. |
| `atlas.geometry.projection` | world | `equirect` | stable | How a sphere was flattened. Required when surface is `sphere`. |
| `atlas.geometry.equirect.px` | world | `x,y,w,h` (world px) | stable | The raster window the projection fills. |
| `atlas.geometry.equirect.deg` | world | `west,north,east,south` (degrees) | stable | The ground that window pictures. |
| `atlas.geometry.mercator.px` | world | `x,y,w,h` (world px) | experimental | The raster window a Web-Mercator cut fills. Where `equirect` declares y linear in degrees, `mercator` declares y linear in projected latitude — `asinh(tan lat)` — the flattening a real-world tile window actually is. |
| `atlas.geometry.mercator.deg` | world | `west,north,east,south` (degrees) | experimental | The ground the Mercator window pictures, in degrees at its edges. |
| `atlas.geometry.body` | world | slug, e.g. `mars` | experimental | The body pictured. |
| `atlas.geometry.radius_km` | world | decimal string | experimental | The body's mean radius. |
| `atlas.geo.lat` | feature | decimal degrees | experimental | True planetary latitude as published (planetocentric). Provenance and card material; rendering derives from the map-level mapping. |
| `atlas.geo.lon` | feature | decimal degrees | experimental | True planetary longitude as published (east-positive). |
| `atlas.hydro.huc12` | feature | twelve digits | experimental | The USGS twelve-digit hydrologic unit — the subwatershed — the feature's ground lies wholly within, from the national Watershed Boundary Dataset. A feature spanning subwatersheds carries no key rather than a misleading one. |
| `atlas.stroke.width_px` | collection | positive decimal (world px) | experimental | The ground width of a path collection's features: a trail is a line and a weight, and the weight lets a reader draw the path as one continuous stroke. |
| `atlas.collection.key` | collection | slug | experimental | A collection's merge identity across sources. Absent, the icon key stands in. |

## Policy names (never written to a payload)

| Name | Meaning |
|---|---|
| `atlas.note.text` | A pin's description, as the merge policy table and ledger name it, so "which description wins" is decided and recorded in the same vocabulary as everything else. |

## Rules

- Producer-side: any `atlas.*` key not in this registry, attached to the
  wrong entity, or carrying a value outside its vocabulary **fails the
  build**.
- Reader-side: unknown keys are ignored. Bundles are never refused over
  conventions.
- `sphere` requires `projection` and both `equirect.*` mappings; the
  declared mapping must invert — a pin's packed position run backward
  through it recovers the source's published coordinates.
- `atlas.icon.std`, once declared, must be resolved to a real icon asset by
  the time the bundle is written.
