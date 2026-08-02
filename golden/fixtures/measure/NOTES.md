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

    annotation      65 pins · 153 described (235%) · median 63 chars

That line is `bend-or`, the city fixture: 65 described historic sites, and 88
zones each carrying the sentence the subwatershed membership join earned it.
It is the only volume in the set whose shapes are described, and since it
joined the set the defect is **committed rather than merely recorded** — a
rewrite that quietly fixes the arithmetic now fails a golden instead of
passing every one of them and still changing behavior. Either carry the
defect or waive it in `golden/waivers.json` with a reason.

The withheld city that stood in this slot before read `45 pins · 48 described
(106%)`. Same defect, milder ratio, and worth keeping in view: the number
does not merely round past 100%, it scales with how much of a volume is
shape rather than point.

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

Path and area collections, inline geometry, label policy and standard icons
resolved through the conventions used to be invisible here; the `bend-or`
city fixture measures all four now, and `zelda-tears-of-the-kingdom` has 23
areas beside it.

Worth not mistaking the city for: the packed `member` column — the id of the
area feature a pin lies in — reads zero for all 65 of its pins. `member` is
filled from a translator's `region_id`, which the ArcGIS translator never
assigns, so the city's pins are unowned however many zones surround them.
The column is exercised elsewhere in the set (`zelda-tears-of-the-kingdom`
fills it for every pin, `fallout-new-vegas` for ten of its thirteen worlds).
The city's own membership claim — which subwatershed a zone drains through —
is not carried there: it rides on the shape features as
`atlas.hydro.huc12` and as a sentence in the `.text` payload.
