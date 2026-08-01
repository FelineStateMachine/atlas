# Scheduled city snapshots

A city's `.atlas` can build itself on a schedule: crawl the city's ArcGIS
hub, exit quietly when nothing changed, and publish a new versioned bundle
when something did.
The reusable workflow lives in this repository
(`.github/workflows/snapshot.yml`); this repository's own scheduler runs it
weekly over the public proof cities. A city IT team runs the same workflow
in a repository of their own, so every employee works off the same enriched
`.atlas` at whatever cadence the team chooses.

## What a run does

1. Crawls the city into the `fmg-archive` committed on the state branch.
   Captures are content-addressed: an unchanged city registers nothing.
   Beside the city's own hub, every crawl queries the USGS national
   hydrography services on `hydro.nationalmap.gov` for the city's window —
   watersheds, subwatersheds, named streams, waterbodies — so a runner
   with an egress allowlist needs that host beside the hub's.
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
