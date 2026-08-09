# Arbitrary Earth area + U.S. source-pack review gate

This gate proves that `Sample Region` is a parameter, not a fixture. A creator
draws any bounded Web Mercator area on Earth. The geographic selection is not
coupled to source coverage. For the full-data acceptance demo, the creator
chooses an area inside the built-in USGS/TIGER pack's stated U.S. coverage and
receives one native `.atlas` that renders its raster and semantic features with
the network disabled. A selection outside that coverage is still a valid
project; sources may truthfully return empty results.

## Default source profile

| Role | Nationwide source | Selection | Semantic output |
|---|---|---|---|
| Basemap | [USGS Topo MapServer](https://basemap.nationalmap.gov/arcgis/rest/services/USGSTopo/MapServer) | one aligned Web Mercator `/export` image, bounded to 4096×4096 | locally derived raster pyramid |
| Roads | [Census TIGERweb Transportation](https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/Transportation/MapServer) | layers 2, 6, and 8; area envelope; all intersecting features | one `roads` path FeatureSet |
| Water | [Census TIGERweb Hydro](https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/Hydro/MapServer) | layers 0 and 1; area envelope; all intersecting features | `waterways` path and `water-bodies` area FeatureSets |
| Context | [Census TIGERweb State/County](https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/State_County/MapServer/1) | current Counties layer 1; area envelope | `jurisdictions` area FeatureSet |

USGS describes Topo as a 256-pixel, EPSG:3857 cache made from current National
Map and other public-domain data. Its service metadata carries the complete
contributor credit. The National Map states that its services and downloads
are public domain and requests the acknowledgment: “Map services and data
available from U.S. Geological Survey, National Geospatial Program.”

TIGERweb describes the selected road, hydrography, and county layers as
January 1, 2025 vintage, EPSG:3857 feature layers. Their service metadata names
the U.S. Census Bureau as source, advertises GeoJSON, and reports pagination
support. The generated project records the service URL, layer, public-domain
status, and Census source credit in its legal/evidence receipt. It must not
imply Census endorsement.

Authoritative protocol references:

- [ArcGIS export map](https://developers.arcgis.com/rest/services-reference/enterprise/export-map/)
  defines the projected bounding box and exact output image dimensions.
- [ArcGIS layer query](https://developers.arcgis.com/rest/services-reference/enterprise/query-map-service-layer/)
  defines geometry envelopes, spatial references, ordering, object-ID
  selection, and pagination.
- [The National Map terms](https://www.usgs.gov/faqs/what-are-terms-uselicensing-map-services-and-data-national-map)
  state the public-domain status and requested acknowledgment.
- [Census citation guidance](https://www.census.gov/about/policies/citation.html)
  is the source-credit policy; the Atlas receipt preserves the exact service
  copyright text in addition to the authored attribution.
- [TIGER/Line technical documentation](https://www2.census.gov/geo/pdfs/maps-data/data/tiger/tgrshp2024/TGRSHP2024_TechDoc.pdf)
  states the federal-work copyright rule, requests Census source credit, and
  supplies the positional-accuracy and non-jurisdictional-boundary disclaimers
  that the compiled source record must retain.

## Spatial contract

The picker retains the creator's WGS84 rectangle as intent. Compilation maps
it to one aligned Web Mercator tile block:

```text
creator rectangle (EPSG:4326)
              │ project
              ▼
covered global tiles at source zoom Z
              │ grow to smallest aligned power-of-two square
              ▼
origin tile [column, row] + N × N tiles
              │ request one exact N·256 square and derive local levels
              ▼
local pixel plane [0,0,N·256,N·256]
```

This keeps local pyramid derivation deterministic, makes the captured source
image independently verifiable, and makes the EPSG:3857-to-local feature
transform exactly affine.
For radius `R`, source zoom `Z`, world pixel width `W = 256 * 2^Z`, and tile
origin `(ox, oy)`, the transform from EPSG:3857 metres is:

```text
s  = W / (2πR)
x' =  s*x + πR*s - 256*ox
y' = -s*y + πR*s - 256*oy
```

The native coordinate contract therefore needs distinct tile-column and
tile-row origins. The existing singular `firstTile` cannot locate an arbitrary
Earth window and must not be reused for both axes. The generated profile asks
the export service for PNG; the native raster index still carries a codec per
tile so other profiles may use mixed PNG/JPEG caches.

Feature queries use the same projected envelope and request EPSG:3857 output.
The adapter verifies the returned CRS before applying the affine transform.
Intersecting roads and polygons can extend outside the requested envelope, so
the transform lane must deterministically clip paths and areas to the aligned
window before extent validation and partitioning.

## Capture-set contract

Each ArcGIS feature source is one atomic acquisition, not one response:

1. Query the projected envelope with explicit `inSR=3857`, `outSR=3857`,
   `orderByFields=OBJECTID ASC`, page size, and offset.
2. Follow `exceededTransferLimit` with the next exact offset. Require object
   IDs to remain strictly increasing and refuse an empty promised page,
   duplicate ID, out-of-order ID, or false termination.
3. Roads use `OID` as stable feature identity and the declared
   `coalesce(NAME, OID)` title mapping; `OBJECTID` is capture pagination state,
   not semantic identity. The fallback preserves unnamed features instead of
   silently changing “all roads” into “named roads.”
4. Commit the capture-set receipt only after every page and its termination
   proof validates. A partial acquisition never becomes selectable.
5. Replay the recorded pages in receipt order. Offline and exact replay make no
   network request and compile byte-identically from the selected bodies.

The request shapes are generated rather than copied into the manifest:

```text
GET {layer}/query?f=geojson&returnGeometry=true&outFields=*
    &geometry={minX,minY,maxX,maxY}&geometryType=esriGeometryEnvelope
    &inSR=3857&outSR=3857&spatialRel=esriSpatialRelIntersects
    &where=1%3D1&orderByFields=OBJECTID%20ASC
    &resultOffset={offset}&resultRecordCount={page size}
```

The source-neutral contracts use only data meaning:

| FeatureSet | Geometry | Stable identity | Typed properties |
|---|---|---|---|
| `roads` | path | `OID` | `name?: string`, `class: string` (`MTFCC`), `route-type?: string` (`RTTYP`) |
| `waterways` | path | `OID` | `name?: string`, `class: string` (`MTFCC`) |
| `water-bodies` | area | `OID` | `name?: string`, `class: string`, `land-area?: int64`, `water-area?: int64` |
| `jurisdictions` | area | `GEOID` | `name: string`, `land-area: int64`, `water-area: int64` |

Title fallback is a small declarative mapping expression, not adapter-owned
semantics or an arbitrary script.

Raster acquisition records the exact export request, media type, byte length,
hash, capture time, licence, and attribution. Local pyramid tiles are derived
only after that source image is captured as one complete selectable set.

## Workbench path

```text
Choose area ──► Sources ──► Contracts ──► Build ──► Compiled preview
   drag box       defaults      mappings      live       styles + legend
   or bbox        + budget      + clipping    stages     on .atlas truth
                                                              │
                                                Inspect / Open / Drag
```

The first map is only a source-area selector. After the first compile, every
transform, feature, raster, layer, style, and legend preview is read from the
validated `.atlas`; the browser keeps no parallel presentation model.

The generated single manifest uses neutral labels (`sample-region`, `Sample
Region`) and contains the chosen WGS84 intent, resolved local tile-window
contract, explicit sources, mappings, legal text, presentation, and budgets.
The default presentation is:

1. USGS Topo raster;
2. pale blue water areas and blue waterway paths;
3. quiet county outlines;
4. local roads, secondary roads, then primary roads with increasing weight;
5. labels at source-appropriate minimum zooms and a generated legend.

Area size is governed by evidence, tile, feature, and output budgets rather
than a hard-coded named place. The UI estimates the aligned tile block before
capture and offers detail profiles: local roads for bounded local areas,
primary/secondary roads for regional areas. It refuses an over-budget plan; it
never silently drops pages or tiles.

## Acceptance evidence

The release gate uses a creator-selected area and fresh temporary library:

- CUA draws the area, keeps the nationwide defaults, inspects the generated
  neutral manifest, builds, and verifies compiled raster/road/water/context
  semantics plus legal receipts.
- The live feature request uses a deliberately small page size so the receipt
  proves at least two pages and explicit termination.
- The artifact opens through native intake and by drag/drop, then renders with
  the network disabled.
- An offline build into another clean library and an exact-replay build produce
  the same bytes as the online selection while an HTTP counter remains zero.
- Reader telemetry proves bootstrap/page zero, visible feature partitions, and
  visible raster shards were range-read; total bytes stay below the full Atlas
  size and no whole `Volume` is materialized.
- Adversarial fixtures cover repeated/out-of-order IDs, missing continuations,
  false termination, crossing geometry, mixed raster codecs, corrupt shard
  ranges, and a cancelled partial capture.
- Packaged macOS, Windows, and Linux acceptance repeats open, association,
  drag/drop where the desktop supports it, and offline first render.

Live government services are an opt-in review smoke, not a hermetic CI
dependency. CI uses byte-pinned protocol fixtures that reproduce the same
multi-page, clipping, mixed-codec, and termination contracts.
