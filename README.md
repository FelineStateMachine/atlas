# Atlas

Atlas is a standalone, offline game-map explorer built with
[Allons](../allons), Wails, and OpenLayers. Generation reads the neighboring
`gamemap` FMG archive; the shipped Go binary embeds the catalog, category
SVGs, frontend, and every raster tile it serves. There are no sidecars, CDNs,
or runtime network dependencies.

Keep the three repositories beside one another:

```text
~/Developer/
├── allons/
├── atlas/
└── gamemap/
```

Run Atlas from this repository:

```sh
go run -tags dev .
```

Wails requires a desktop build tag even when it is invoked through `go run`.
Atlas supplies the additional macOS `UniformTypeIdentifiers` linker flag
itself, so no shell environment setup is needed.

The app supports scroll/pinch zoom, drag panning, pin search, map and tile-set
selection, compact category toggles, group-level show/hide controls, floating
`display_type: "text"` labels, optional region polygons and names, and pin
details. A game with one map keeps that map visible in a disabled selector.
Curated map ordering places a game's primary map first; the small ordering
table lives in `tools/generate/main.go`.

Pins and legend entries use the archive's category SVG and resolved MapGenie
color when available, with a white exterior treatment for contrast. Normal
zoom levels use stable, priority-based decluttering. Selected and searched
locations bypass decluttering, and hovering a pin reveals its title. `Z` opts
into all visible pin labels at native-detail zoom or closer; the default
remains uncluttered. Area and region titles retain their independent toggles
and full-detail pass. Pixel-art maps additionally permit two nearest-neighbor
overzoom levels so labels can spread out without blurring the raster or
requesting synthetic tiles. Photographic maps stop at their highest native
tile level.

[SCRAPER_PROMPT.md](SCRAPER_PROMPT.md) contains a ready-to-use prompt for
preserving MapGenie's text-display and zone fields in the upstream archive.

## Refreshing the embedded data

With `../gamemap` beside this repository:

```sh
go generate .
```

`main.go` embeds the entire `assets` tree with `//go:embed all:assets`.
`go generate` performs three operations:

1. `tools/tiles` finds the highest complete source level for every configured
   layer, keeps it unstitched, and derives every missing lower level.
   Photographic source tiles retain JPEG/PNG encoding; pixel-art levels are
   normalized to lossless PNG and use nearest-neighbor reduction. Photo
   pyramids use smooth box reduction. Repeated placeholder borders are
   excluded from fit bounds.
2. `tools/generate` builds `assets/catalog.json` and copies referenced archive
   SVGs into `assets/icons`. Maps with missing snapshots or incomplete
   configured layers are omitted.
3. `npm ci` and the local esbuild/OpenLayers installation produce the
   self-contained `assets/app.js` and `assets/app.css` bundles.

The resulting binary has no runtime dependency on `../gamemap`, npm, or the
network.

## Renderer architecture

Atlas uses a bounded `ATLAS:PIXELS` projection over an 8192×8192 logical
extent. Each map layer is an XYZ source with a 64-tile cache, no wrapping, no
tile transitions, and interpolation selected by map type. Only visible tiles
are decoded and drawn.

OpenLayers vector layers are ordered as raster, geohash grid, zones, zone
names, floating titles, pins, full-detail labels, and selected or searched
annotations. The grid uses a standard base32 hierarchy over the active map's
pixel bounds. Press `G` to toggle it, click a cell to descend, or enter a hash
in the dedicated field to jump one character at a time. `Escape` and the back
button ascend one level; escaping from the root closes the grid. Selecting a
cell promotes every enabled annotation inside it above normal decluttering.
Ancestor sibling cells remain visible as shaded, clickable context. Together
they dim everything outside the selected cell and permit direct lateral
traversal without backing all the way out.
Map shortcuts are ignored while an input, select, textarea, or editable
control has focus.

Named zones appear in a hierarchical table of contents above the category
legend. Click a zone to fit it in the viewport. Right-click a zone to toggle
its high-contrast outline and fill; highlighting a zone also restores the
global zone layer if it was hidden. While any zone is highlighted, unrelated
zones are shaded as context in the same visual language as geohash neighbors.
Pins outside the highlighted zone union are culled, while every enabled pin
inside that union moves to a dedicated declutter group. This shows a denser,
useful sample without reintroducing every overlapping annotation. Selected and
searched locations remain visible even when they fall outside the zone focus.

Wheel input over the map always zooms, including immediately after using a
sidebar filter. Wheel input over the sidebar remains normal sidebar scrolling.

All annotations and grid cells are canvas-rendered and viewport-culled; the
sidebar does not create a DOM element per location.

Switching layers within one map replaces and clears the raster source while
retaining the current center, resolution, and overlay state.

## Verification

The complete local verification sequence is:

```sh
go generate .
go test ./...
go vet ./...
go build -tags dev .
```
