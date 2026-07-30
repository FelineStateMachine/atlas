# Atlas

Atlas is an offline game-map explorer built with
[Allons](../allons). It is generated from the neighboring `gamemap` FMG
archive and includes every complete, z13-compatible map plus its pins,
categories, zones, and tile-set variants.

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
selection, category toggles, group-level show/hide controls, floating
`display_type: "text"` labels, optional region polygons and names, and pin
details. Pins and legend entries use the archive's category SVG and resolved
MapGenie color when available, with a white exterior treatment for contrast.
Dense maps use stable, priority-based annotation sampling while selected and
searched locations always remain visible.

[SCRAPER_PROMPT.md](SCRAPER_PROMPT.md) contains a ready-to-use prompt for
preserving MapGenie's text-display and zone fields in the upstream archive.

## Refreshing the embedded data

With `../gamemap` beside this repository:

```sh
go generate .
```

`main.go` embeds the entire `assets` tree with `//go:embed all:assets`.
The resulting binary has no runtime dependency on `../gamemap` or the network.
Generation copies referenced game icons into `assets/icons`, preserving their
source colors; categories without an SVG receive a compact initials fallback.
Generation skips maps with missing snapshots, incomplete layers, or no z13
tiles instead of exposing a broken map in the picker.
Repeated placeholder tiles around smaller source maps are detected during
generation, then clipped and excluded from the viewer's fit bounds.
