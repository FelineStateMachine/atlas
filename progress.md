Original prompt: Implement a toggleable geohash telescoping layer with keyboard and click navigation.

- In progress: standard base32 hierarchy over the Atlas pixel extent.
- Controls: G toggles; click or type descends; Escape ascends.
- Selected cells should render all enabled annotations without decluttering.
- Implemented the OpenLayers grid source/layer, base32 extents, cell promotion,
  dedicated geohash input, click navigation, G/Escape controls, and diagnostics.
- Added per-cell annotation counts to diagnostics and refresh promotion when
  switching raster variants.
- Verified root and click descent, typed input, editable-focus shortcut
  isolation, Escape ascent, and the pixel-map fourth level in Chromium.
- Added recursively shaded ancestor sibling cells above ordinary annotations
  so all space outside the current cell is dimmed and directly traversable.
- Verified the shaded context visually at `s4`; clicking the precision-minus-one
  `s5` sibling traversed laterally and changed promoted annotations from 17 to 3.
- `go generate .`, frontend build, and `go test ./...` pass after the migration.
- `go vet ./...` and `go build -tags dev .` pass.
- Remaining: repository diff review only; migration changes intentionally remain
  uncommitted after the requested initial baseline commit.

Follow-up: focus the map viewport by default so wheel zoom works before a click.

- Added semantic autofocus and a post-initialization focus call that runs only
  once; later search, geohash, and selector focus remains user-controlled.
- Verified a fresh page starts with `activeElement === "map"` and wheel zoom
  changes zoom from 1.94 to 2.94 without a preceding click.
- Verified search retains focus and its entered value afterward; no new browser
  console errors.

Follow-up: add a zone table of contents with direct navigation and per-zone
highlighting.

- Added a hierarchical, alphabetized zone index above the category legend.
- Left-click fits the OpenLayers view to the selected zone.
- Right-click toggles a persistent high-contrast zone highlight; the global
  zone control still hides or shows all boundary and title layers.
- Visually verified navigation and highlight toggling on Clair Obscur's Ancient
  Sanctuary and on the nested Pokémon Celadon City / Celadon Gym hierarchy.
- Verified right-clicking a zone restores the global zone layer when hidden,
  and the browser console remains free of errors.
- Verified a map with no zones removes the index without stale entries.
- `npm run build`, `go test ./...`, `go vet ./...`, and `git diff --check` pass.

Follow-up: make map wheel zoom independent of sidebar focus and dim zone
context around active highlights.

- The map target now takes focus during wheel capture, so the same wheel event
  reaches OpenLayers immediately after a filter or zone-list interaction.
- Highlighting one or more zones darkens unrelated zone geometry and labels;
  ancestors remain undimmed so a highlighted child is not shaded by its parent.
- Pins outside the highlighted zone union are culled, while pins inside it are
  rendered in an independent declutter group. This gives the focused zones more
  annotation capacity without stacking every colliding pin. Selected and
  searched pins remain exempt from zone culling.
- Explicitly enabled OpenLayers wheel interactions without its focus-only
  default. Browser verification confirmed a filter can retain focus while a
  wheel gesture over the map changes zoom from 1.646 to 2.646; the same gesture
  over the sidebar leaves map zoom unchanged.
- Verified a two-zone focus reports 56 enabled annotations and renders a
  decluttered 40-annotation sample after fitting Crimson Forest. No browser
  console errors were reported.
- Final `go generate .`, `go test ./...`, `go vet ./...`,
  `go build -tags dev .`, and `git diff --check` all pass.

Follow-up: cap geohash precision at three characters and make terminal cells
inspectable.

- Geohash input and click descent now stop at three characters for every map.
- All raster types permit two display-only overzoom levels. The source tile
  grid remains bounded at native detail, so overzoom reuses the final native
  tiles with each map's configured nearest-neighbor or smooth interpolation.
