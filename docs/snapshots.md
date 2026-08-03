# Scheduled city snapshots

> **Parked.** The workflow's steps are the shipped ones —
> `atlas crawl|tiles|compose|enrich` — but the ArcGIS/USGS crawler is not
> built: `atlas crawl -source list` offers `ign` alone, and
> `docs/generate.md` §3.2 says why. A run therefore fails at its first step,
> so this repository's scheduler is dispatch-only and the weekly cron is
> written down in `snapshots.yml` rather than armed. Everything after the
> crawl is the shipped path, walked over a city archive by the pipeline's own
> tests (`docs/generate.md` §8). One crawler registered as `arcgis-hub`
> unparks the whole thing, and nothing else here changes.

A city's `.atlas` can build itself on a schedule: crawl the city's ArcGIS
hub, exit quietly when nothing changed, and publish a new versioned bundle
when something did.
The reusable workflow lives in this repository
(`.github/workflows/snapshot.yml`); this repository's own scheduler runs it
over the public proof cities. A city IT team runs the same workflow
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
git commit --allow-empty -m "snapshot state"
git push -u origin snapshots
git switch main
```

The branch starts empty and fills up with the capture archive, one commit per
run that found something new.

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

A city that should not be named in a public repository is carried by the
`atlas-ref` input: point it at a branch or fork of Atlas whose curation table
(`internal/generate/sources/arcgishub/cities.go`) names the city. There is no
side door — a table that can be extended by a file nobody reviews is not the
gate it is meant to be — so an uncurated city is refused rather than composed
wrong ([decision 16](decisions/0016-uncurated-captures-are-passed-over.md)).

Employees fetch
`https://github.com/<org>/<repo>/releases/download/snapshot-<city>/<city>-latest.atlas`
and drop it into their Atlas bundles directory — or IT mirrors it onto a
share people copy from. (A bundle dropped in from outside appears at the next
launch: the registry scans at launch and rescans on import, never watches.)
