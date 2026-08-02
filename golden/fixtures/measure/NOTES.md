# What the measurement fixtures say, and what is wrong with it

`maturity.txt` and the `*.build.json` beside it are the current tooling's
output, unedited. Everything below is an annotation on that output. A golden
is never corrected to match what it ought to have said; it is corrected only
by a deliberate, reviewed change to the thing being measured, and then
recaptured.

## Defects carried verbatim

**`DescribedPct` can exceed 100%.** `Build.Described` counts every entry in a
world's `.text` payload that carries prose, and `Build.Pins` counts only the
world's *point* features. Path and area features defer their prose into the
same `.text` file, so a world whose shapes are described divides a whole by a
part:

    annotation      45 pins · 48 described (106%) · median 46 chars

That line is from the withheld city fixture (see `FIXTURES.json`), the only
volume in reach whose shapes carry descriptions. **No committed fixture
exhibits the defect**, which is exactly why it is written down here: a
rewrite that quietly fixes the arithmetic would pass every committed golden
and still be a behavior change. Either carry the defect or waive it in
`golden/waivers.json` with a reason.

`Percent` also truncates rather than rounds — `part*100/whole` in integer
arithmetic — so 2592 of 2635 prints as 98%, not 98.4% and not 99%.

**`Depth` and `DepthTiles` conflate a volume's pyramids.** `Depth` is the
deepest `maxZoom` of any lens of any world, and `DepthTiles` then counts
every tile at that zoom across every pyramid in the archive. For a
split-sheet volume this mixes thirteen worlds' pyramids into one number, and
a world whose own pyramid stops shallower contributes nothing to it:

    cartography   2651 tiles · 21.5 MB unique raster · depth z5 holds 1812 tiles

**`Groups` counts a group once per world.** The `groups` set is rebuilt for
each world, so a group title shared by thirteen worlds counts thirteen times.
`fallout-new-vegas` reads `258 categories in 72 groups` for that reason.

**The merge lines do not say which world they account for.** `MergeAccount`
carries `Map`, and the report drops it, so a split-sheet volume prints
thirteen indistinguishable `merge MapGenie:` lines. The structured fixtures
keep the field; only the printed report loses it.

## Not defects, though they read like one

**`icons` counts fewer categories than `structure` does.** A collection that
renders as text (`atlas.render.as = text`) is counted as a text label set and
is not asked for an icon, so `zelda-tears-of-the-kingdom` reads `161
categories` in one line and `158 of 159 marker categories carry one` in the
next: 161 collections, 2 of them text sets.

**`RasterBytes` is smaller than the sum of the tile sizes.** Tiles are
de-duplicated by (CRC32, uncompressed size) before being weighed, so a border
of repeated filler counts once. It is a measure of information, not of disk.

**`geometry plane-default`** means the world declared no
`atlas.geometry.surface` at all. `plane` means it declared one. Only `mars`
reads `sphere`.

## What the fixture set does not measure

No committed fixture has a path or area collection, an inline geometry, a
label policy, or a standard icon resolved through the conventions — those
live in the withheld city volume. Anything the rewrite changes in that ground
is invisible to these goldens until a publicly curated city is captured.
