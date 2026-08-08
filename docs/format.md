# The `.atlas` native schema bundle

**Status: normative.** Atlas now has one accepted physical format. The retired
document-shaped v3 format is preserved for historical reference in
[`format-v3.md`](format-v3.md); current readers do not install it.

## Semantic shape

```text
Volume
 ├── Worlds
 │    ├── CoordinateSpace
 │    ├── FeatureSets
 │    │    └── Features
 │    │         ├── Geometry
 │    │         ├── TypedProperties
 │    │         ├── Relationships
 │    │         └── Provenance
 │    ├── RasterPyramids
 │    └── Presentation
 │         ├── Layers
 │         ├── Styles
 │         └── Legend
 └── Assets
```

Semantics and presentation are separate. Feature membership, geometry,
relationships, typed claims, and evidence remain true when a presentation is
replaced. Layers and styles refer to semantic identities; they do not own or
duplicate features.

## Physical shape

A `.atlas` is a deterministic ZIP container. ZIP is only framing: the data
model is the embedded schema plus typed column blocks, not a collection of JSON
documents.

```text
atlas.json                         bootstrap + release metadata
schema/<sha256>.json               canonical file schema
data/volumes.pack                  typed column blocks
data/worlds.pack
data/coordinate-spaces.pack
data/feature-sets.pack
data/property-definitions.pack
data/features.pack
data/relationships.pack
data/provenance.pack
data/raster-pyramids.pack
data/raster-formats.pack
data/raster-coverage.pack
data/presentations.pack
data/styles.pack
data/layers.pack
data/legend.pack
data/assets.pack
assets/<sha256>                    content-addressed supporting bytes
tiles/<pyramid>/<z>/<x>/<y>.<ext>  opaque raster bytes
```

Typed blocks and opaque blobs are stored without ZIP compression so they can
be read independently. The bootstrap and schema may be deflated. Entry names
are relative, unique, and cannot contain empty, `.` or `..` segments.

## Bootstrap and release identity

`atlas.json` is deliberately small and schema-independent:

```json
{
  "format": "atlas-schema-bundle",
  "framing": 1,
  "volume": "earth",
  "release": {
    "title": "Earth",
    "createdAt": "2026-08-03T16:21:07Z",
    "revision": 10,
    "stamp": "<64 lowercase hex characters>",
    "worlds": 1
  },
  "schema": { "name": "schema/<hash>.json", "hash": "<sha256>" },
  "tables": [],
  "blobs": []
}
```

- `createdAt` is newest source capture time, never build time.
- `revision` orders different producer policies over the same capture.
- `stamp` is SHA-256 over the canonical bootstrap with an empty stamp, after
  all schema, table, and blob hashes and lengths have been filled.
- file names are `<volume>-<YYYYMMDD>-<stamp12>.atlas`.
- registry winners order by `createdAt`, `revision`, `stamp`, then locator.

The directory is the registry. `index.json`, when present, is a derived
convenience listing with no authority.

## Typed block framing

Every `.pack` begins with `ATLASPK\0`, framing version 1, row and column counts,
the canonical schema hash, the root type identity, and a fixed-width column
directory. Each directory entry declares:

- stable 128-bit field identity;
- logical and physical kind;
- optionality;
- presence bitmap range;
- payload range;
- CRC-32 over presence and payload.

Fixed-width values are directly addressable. Strings and bytes use a row
offset table followed by content. Consumers verify bounds and checksums before
exposing column views.

## Compatibility without a committee

Identity, not field position or spelling, governs meaning.

| Change | Result |
|---|---|
| Add a type with a new ID | compatible |
| Add an optional field with a new ID | compatible |
| Rename a type or field while keeping its ID and kind | compatible |
| Independently add different IDs on two branches | union is compatible |
| Reuse an ID with a different kind | refused |
| Remove or reinterpret a required field | new identity required |
| Change container framing | old reader refuses |

A reader merges its application schema with the file schema. Unknown types and
columns remain addressable and round-trippable; a presentation may ignore them.
This gives backward, forward, and sideward compatibility without a central
sequence of schema versions.

## Install and validation

One command serves picker, desktop open, drag/drop, and programmatic install:

```text
source → open framing → merge schema → verify every table/blob
       → restore and validate semantic Volume
       → stage beside library → atomic rename → reopen + verify
       → rescan + deterministic fold → announce changed volume
```

A malformed file never lands under `.atlas`. A legacy v3 file, ordinary ZIP,
unknown framing, inconsistent schema, changed field kind, corrupt column, bad
hash, missing reference, or invalid semantic graph is refused.

## Producer requirements

The producer constructs the semantic graph directly and calls the native
compiler. It must not create v3 JSON payloads as an intermediate format.
Raster files may be streamed from disk; their hashes and lengths still
participate in the release stamp. A completed file is reopened and fully
validated before it is renamed into the registry.

## Reader requirements

A conforming reader:

1. validates bootstrap identity, framing, release metadata, and release stamp;
2. verifies and canonicalizes the embedded schema;
3. unions file and application schemas by stable identity;
4. verifies table roots, column kinds, required fields, bounds, and CRCs;
5. verifies opaque blob length and SHA-256 before use;
6. restores references by semantic ID and validates the complete graph;
7. treats Presentation as a view over semantic data, not as the data itself.
