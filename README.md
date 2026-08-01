# Atlas

Atlas is a standalone, offline game-map explorer built with
[Allons](../allons), Wails, and OpenLayers. The executable carries only the
application shell; each game travels as a self-contained `.atlas` bundle --
its maps, packed locations, raster tile pyramids, and category icons in one
zip archive. Drop a bundle into the application's `bundles` directory and the
game appears; drop in a newer build of the same game and it updates. There
are no sidecars, CDNs, or runtime network dependencies.

Keep the three repositories beside one another:

```text
~/Developer/
├── allons/
├── atlas/
└── gamemap/
```

Run Atlas from this repository:

```sh
go generate .   # once, to build the bundles into the library
go run -tags dev .
```

Generation installs bundles straight into the central library -- `bundles/`
under the application's data directory (on macOS,
`~/Library/Application Support/dev.felinestatemachine.atlas/bundles`) -- and
the application reads the same place, so a dev run serves whatever has been
built with no pointing at anything. `ATLAS_BUNDLES_DIR` overrides the library
for either side when isolation is wanted, and `tools/generate -bundles`
writes a registry elsewhere (for an export, say) without touching the
library.

Wails requires a desktop build tag even when it is invoked through `go run`.
Atlas supplies the additional macOS `UniformTypeIdentifiers` linker flag
itself, so no shell environment setup is needed.

The app supports scroll/pinch zoom, drag panning, pin search, map and tile-set
selection, compact category toggles, group-level show/hide controls, floating
`display_type: "text"` labels, optional region polygons and names, and pin
details. Search and filter results list in a right-docked panel — a capped,
alphabetical shortlist of whatever survives the current filters — and opening
a location pushes the list aside for its detail card, with the way back kept
on screen. The dock folds to a thin rail and the choice is remembered with
the game. A game with one map keeps that map visible in a disabled selector.
Curated map ordering places a game's primary map first; the small ordering
table lives in `tools/generate/main.go`.

Pins and legend entries use the archive's category SVG and resolved MapGenie
color when available, with a white exterior treatment for contrast. Normal
zoom levels use stable, priority-based decluttering. Selected and searched
locations bypass decluttering, and hovering a pin reveals its title. `Z` opts
into all visible pin labels at native-detail zoom or closer; the default
remains uncluttered. Area and region titles retain their independent toggles
and full-detail pass. Every map permits two display-only overzoom levels so
three-character geohash cells and dense annotations can be inspected without
requesting synthetic tiles. Pixel-art maps remain nearest-neighbor sharp;
photographic maps retain smooth interpolation.

[SCRAPER_PROMPT.md](SCRAPER_PROMPT.md) contains a ready-to-use prompt for
preserving MapGenie's text-display and zone fields in the upstream archive.

## Building the game bundles

With `../gamemap` beside this repository:

```sh
go generate .
```

`main.go` embeds only the application shell (`assets/index.html`,
`assets/app.css`, `assets/app.js`). `go generate` performs three operations:

1. `tools/tiles` finds the highest complete source level for every configured
   layer, keeps it unstitched, and derives every missing lower level into
   `build/tiles`. Photographic source tiles retain JPEG/PNG encoding;
   pixel-art levels are normalized to lossless PNG and use nearest-neighbor
   reduction. Photo pyramids use smooth box reduction. Repeated placeholder
   borders are excluded from fit bounds.

   Each pyramid records a stamp over the tiles it was derived from and the
   tool that derived them, so a layer nothing has changed under is left where
   it is: a run that captures one new game costs seconds rather than the half
   minute of re-deriving the rest. `-force` derives everything again.
2. `tools/generate` writes one `.atlas` bundle per game into the central
   library: the game's manifest, one payload per map, its tile pyramids, and
   its archive SVG icons, validated as each bundle is written. Maps with
   missing snapshots or incomplete configured layers are omitted. Bundles are
   named `<game>-<capture-day>-<stamp>.atlas`, versioned by the newest
   snapshot capture across the game's maps -- building the same archive
   anywhere yields the same file -- and a build already present is left
   untouched. Older versions are never pruned: the library is a registry,
   and `index.json` beside the bundles lists every game's builds, newest
   first.
3. `npm ci` and the local esbuild/OpenLayers installation produce the
   self-contained `assets/app.js` and `assets/app.css` bundles.

Neither the binary nor a bundle has any runtime dependency on `../gamemap`,
npm, or the network.

## The .atlas format

A `.atlas` file is a zip archive holding one game: `atlas.json` (the
manifest: game identity, version stamp, tile grid, map list), `maps/<slug>`
payloads in three parts, `tiles/<pyramid>/z/x/y` rasters stored uncompressed
for byte-range serving, and `icons/`. The format is Atlas's own and carries
nothing specific to any capture source; `internal/bundle` owns reading,
writing, and validation. Two bundles naming the same game slug are two
versions of that game -- the newest by capture time wins, so updating a game
is dropping in a newer file, and an older file dropped beside a newer one is
simply shadowed. File names carry the version for people and for cheap
existence checks; ordering always comes from the manifest inside.

## Renderer architecture

Atlas uses a bounded `ATLAS:PIXELS` projection over an 8192×8192 logical
extent. Each map layer is an XYZ source with a 64-tile cache, no wrapping, no
tile transitions, and interpolation selected by map type. Only visible tiles
are decoded and drawn.

OpenLayers vector layers are ordered as raster, geohash grid, zones, zone
names, floating titles, pins, full-detail labels, and selected or searched
annotations. The grid uses a standard base32 hierarchy over the active map's
pixel bounds, capped at three characters so its terminal cells remain useful
at the available overzoom. Press `G` to toggle it, click a cell to descend, or
enter a hash in the dedicated field to jump one character at a time. `Escape`
and the back button ascend one level; escaping from the root closes the grid.
Selecting a cell promotes every enabled annotation inside it above normal
decluttering.
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

The corpus-dependent tests read the central library and skip themselves when
no bundles have been built, so `go test ./...` passes on a checkout without
`../gamemap`.
