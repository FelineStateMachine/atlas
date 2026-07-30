# Prompt: preserve MapGenie text labels and zones

Use this prompt in the `gamemap` scraper repository:

> Extend the FMG scraper/archive pipeline so downstream map clients can render
> MapGenie text labels and geographic zones without inferring behavior from
> category names.
>
> First inspect the current full-map response and archive format. The raw
> snapshots may already contain some or all of these fields; do not add a
> second representation when preservation through the existing pipeline is
> sufficient.
>
> Preserve these category and location fields:
>
> - `groups[].categories[].display_type` exactly as returned by MapGenie.
>   Known values are `marker` and `text`; retain unknown future values.
> - `groups[].categories[].icon`, `id`, `title`, `visible`, and `locations`.
> - `locations[].region_id`, including explicit `null`.
> - Existing location `id`, `title`, `description`, `latitude`, and
>   `longitude`.
>
> Preserve the map's complete `regions` collection. For every region and
> nested subregion retain:
>
> - `id`, `map_id`, `title`, `subtitle`, and `order`
> - `parent_region_id`
> - `center_x` and `center_y`, preserving nulls and numeric strings
> - `features[]` as valid GeoJSON Features, including the complete
>   `geometry.type` and `geometry.coordinates`
> - `subregions[]` recursively
>
> Do not flatten away hierarchy, discard polygon holes, reduce coordinate
> precision, calculate centers during scraping, or classify a category as text
> merely because its title is `Area`. Behavior must be driven by
> `display_type`.
>
> Keep the archive deterministic: stable key ordering where the project
> already guarantees it, no timestamps beyond the existing capture metadata,
> and no changes to tile paths or content hashes for an unchanged response.
> Update the JSON schema emitted beside snapshots so these fields are covered.
>
> Add fixtures and tests proving:
>
> 1. Marathon / Cryo Archive / Locations / Area retains
>    `display_type: "text"` and the `PANOPTICON` location with its original
>    latitude and longitude.
> 2. All four Marathon `Area` categories remain text categories. The current
>    fixture set contains 40 such locations in total.
> 3. Forza Horizon 6 / Japan retains its 10 titled regions, polygon geometry,
>    hierarchy, and the `region_id` on all 806 current locations.
> 4. Polygon coordinates round-trip without precision loss.
> 5. Pokémon FireRed/LeafGreen's category titled `Area` remains
>    `display_type: "marker"`; category names must not control rendering.
> 6. Older snapshots lacking these optional fields still decode.
>
> Document the downstream contract: `display_type: "text"` is rendered as a
> floating title at the location coordinate; `marker` is rendered as an
> ordinary pin; `regions[].features[].geometry` supplies optional zone
> boundaries and region titles.
