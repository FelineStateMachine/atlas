# Scheduled city snapshots

A city's `.atlas` can build itself on a schedule: crawl the city's ArcGIS
hub (and, with a key, the Zoneomics rules behind its zoning), exit quietly
when nothing changed, and publish a new versioned bundle when something did.
The reusable workflow lives in this repository
(`.github/workflows/snapshot.yml`); this repository's own scheduler runs it
weekly over the public proof cities. A city IT team runs the same workflow
in a repository of their own, so every employee works off the same enriched
`.atlas` at whatever cadence the team chooses.

## What a run does

1. Crawls the city into the `fmg-archive` committed on the state branch.
   Captures are content-addressed: an unchanged city registers nothing.
2. Composes the bundle. Its filename embeds the capture day and a content
   stamp over everything inside.
3. Gates: if that exact filename is already a release asset, the run ends —
   "unchanged". The gate catches data changes, tool changes, and policy
   revisions alike, because the stamp covers them all.
4. Otherwise commits the archive delta (durable, diffable history at any
   cadence — weekly, monthly, quarterly) and uploads the bundle to the
   rolling `snapshot-<city>` release. Every published version stays as an
   asset; `<city>-latest.atlas` always names the newest, so one URL serves
   the whole team.

## Setting it up in your own repository

One-time, in a (typically private) repository:

```sh
git switch --orphan snapshots
# your city's curation, if it is not one of the public proof cities:
cp /path/to/cities_local.go .
git add -A && git commit -m "snapshot state"
git push -u origin snapshots
git switch main
```

Then a workflow:

```yaml
name: City snapshot
on:
  schedule:
    - cron: "0 6 * * 1"   # weekly; monthly/quarterly are equally safe
  workflow_dispatch:
permissions:
  contents: write
jobs:
  snapshot:
    uses: FelineStateMachine/atlas/.github/workflows/snapshot.yml@main
    with:
      city: your-city-slug
```

The `cities_local.go` on the state branch is adopted into the build, so a
private city's curation never has to be published. Employees fetch
`https://github.com/<org>/<repo>/releases/download/snapshot-<city>/<city>-latest.atlas`
and drop it into their Atlas bundles directory — or IT mirrors it onto a
share the app watches.

## Zoneomics, plainly

Zoning-rule enrichment comes from **zone reports you export from your own
Zoneomics subscription** — the attribute/value CSVs the platform produces,
one per zoning district. Place them on the state branch under
`zoneomics/<city-slug>/` and every snapshot joins them to the city's zoning
zones by zone code; each district's card then carries its purpose,
permitted and prohibited uses, and dimensional standards, offline.

- No API and no scraping: the API prices single-point reports and the
  public code pages bar automated access, so neither fits correlating a
  whole town. Files you exported under your own subscription are the
  honest channel, and a snapshot run needs no key and no wire.
- Reports are *point* reports: below the district rules they describe the
  queried parcel — its owner's name, address, valuation. The importer
  drops those rows unconditionally; they never enter a capture.
- Enrichment is all-or-nothing per capture: an unreadable report fails the
  run rather than silently minting a version that lost its rules.
- **Before an organization circulates enriched bundles internally, confirm
  that its Zoneomics subscription permits embedding and internal
  redistribution of exported report content.** Without reports, snapshots
  are pure open data with no strings attached.
