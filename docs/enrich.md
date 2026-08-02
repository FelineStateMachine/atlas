# The enrich lane

Enrich makes volumes richer. It takes a volume the [generate lane](generate.md)
composed — or the several readings of one volume that several sources produced —
and writes a **new build of the same volume**: same slug, new stamp, more in it.

```
readings of one volume ──▶ [ merge → national → stdicons → lenses ] ──▶ a richer build
   generate, one per source        the curated queue                   beside the old one
```

Five laws hold everywhere in this lane.

1. **A volume in, a new build of the same volume out.** Same slug, new stamp.
2. **Nothing published is ever mutated.** The registry is a directory of
   immutable files and a fold over them, so publishing is a new file landing
   beside the old one.
3. **Nothing a source said is removed or rewritten.** An enricher fills what is
   empty. It has no way to express anything else — see §3.
4. **Every contribution is ledgered**, and a gate audits the ledger before a
   build is written.
5. **No change, no build.** An enrichment that finds nothing to say leaves the
   library exactly as it was.

This document is normative for the enricher interface, the contribution format,
the ledger vocabulary, the curation data, and the scoring table. It is the prose
twin of `internal/enrich`.

---

## 1. The seam: how `generate ⊕ enrich` is joined

The two pipeline lanes never import each other (issue #5 §3.2, enforced by
`golden/depcheck`). Generate owns the interchange document; enrich owns its own
model of a volume under enrichment. Both carry the same information, because
both are shaped by what the format needs.

The ⊕ is performed by the one binary that holds both:

```
atlas enrich
  │
  ├─ generate:  archive ──▶ doc.Document, one per source reading
  ├─ cmd/atlas: doc.Document ──▶ enrich.Volume            (adapt.go, a copy)
  ├─ enrich:    the curated queue runs, contributions are applied
  ├─ cmd/atlas: enrich.Volume ──▶ doc.Document + ledger   (adapt.go, a copy)
  └─ generate:  compose.Compose(document, ledger, revision) ──▶ a .atlas file
```

**Why this design and not the alternatives.** Three shapes were possible:

- *Enrich composes.* Rejected: enrich would import `generate/compose`, which the
  lane matrix forbids, and the forbidding is not bureaucratic — it is what keeps
  either lane replaceable without the other.
- *Enrich emits doc-level additions the CLI feeds back.* This is what happens,
  but the additions are expressed in **enrich's own vocabulary**, not the
  document's, so an enricher never learns the document schema.
- *A shared model in `format/`.* Rejected: the volume-under-enrichment is not a
  format concept. The format describes a written bundle; this model describes a
  build in progress, including a grid to measure distances in and a ledger that
  is not final yet.

The adaptation is a mechanical copy in both directions and is held to identity
by `TestAdaptationRoundTrips`: a document adapted and adapted back is the same
document, byte for byte. That test is also the mechanical form of law 5 — an
empty queue cannot produce a different build, because it cannot produce a
different document.

The ledger does not travel through the document. A document is what **one
source** has to say, and a ledger is provenance about a composition, so the
accounts reach the payload through `compose.Options.Ledger` — already
serialized, spliced in verbatim. Composition never learns what a matched pair or
a held feature is, which is what lets the ledger vocabulary below belong wholly
to this lane.

---

## 2. Winning the registry fold

An enriched build has to be the one a library serves, deterministically, without
mutating anything.

The registry's ordering is **creation time, then policy revision, then stamp,
then locator** (see [format.md](format.md)). Creation time is the newest capture
the build was made from and never the build clock — a format invariant this lane
does not touch. An enrichment of the same captures therefore ties on creation
time with the plain build beside it, and the revision has to decide.

**The mechanism.** The revision is one integer carrying two policy numbers:

```
revision = enrichPolicy × RevisionSpan + generatePolicy      RevisionSpan = 100
```

A plain single-source build writes its generate policy revision alone, which is
the same number with an enrich policy of zero. So `9` is generate policy 9
unenriched, and `109` is generate policy 9 enriched under enrich policy 1.

Three properties made this the choice:

- **Deterministic.** The number is a pure function of two compiled-in
  constants. Nothing scans the library, nothing reads a clock, and the stamp —
  which covers the manifest, revision included — stays a pure function of the
  inputs. A mechanism that read the serving build's revision off disk and added
  one would have made the stamp depend on which machine built it, which breaks
  the format's first invariant.
- **Total.** Every enriched build of one capture outranks every plain build of
  that capture, with no tie for the stamp to break arbitrarily.
- **Honest about which axis wins.** Packing two axes into one integer makes one
  dominant, and this picks the enrich axis: within one set of captures an
  enriched build is a *superset* of the plain build, so serving the plain build
  over it would lose data the library already holds. A generate policy change
  takes effect for an enriched volume when the pipeline is re-run — which is how
  a policy change reaches a merged volume anyway, because enrichment is a
  pipeline stage and not a separate ritual.

`BuildRevision` refuses a generate revision that does not fit the span rather
than wrapping into the next enrich band. Widening the span is a deliberate
restamp of every enriched build, not an accident.

---

## 3. The `Enricher` interface

```go
type Enricher interface {
    Name() string
    Declares() []string
    Enrich(v *Volume, ctx Context) (Contribution, error)
}
```

- **`Name`** is how curation orders it and how a log line names it.
- **`Declares`** lists the semantic-conventions keys this enricher may write.
  An attribute it writes that is not declared here fails the build, so what an
  enricher can say about a volume is visible without reading it. A declared key
  the registry does not know fails the build too.
- **`Enrich`** reads the volume and returns additions. **It must not modify the
  volume**: the driver applies contributions, so every change goes through the
  ledger and the gate. The merge enricher, which needs to see the effect of one
  donor before folding the next, works on a clone of its own.

`Context` is everything else an enricher may consult, and every field is data
the caller supplies: the donor readings, the evidence base, the lens offers, the
curation tables, the run's logger. An enricher never opens a file, never reaches
the network, and never learns which source anything came from except through a
ledger it is handed.

### 3.1 The contribution, and its canonical form

A contribution is a flat, ordered list of operations, and it has a **canonical
serialized form** — that form is the contract, and the Go value is a convenience
over it. It can be written to a file, diffed between two runs, replayed against
the same volume years later, or read by a reviewer who wants to see what an
enricher would do before it does it. `Digest()` is its SHA-256, which is what a
log line names a contribution by.

```jsonc
{
  "contribution": "atlas-enrich-contribution",
  "version": 1,
  "enricher": "merge",
  "volume": "cyberpunk-2077",
  "ops": [
    { "op": "set-prose", "world": "night-city", "feature": 123, "value": "…" },
    { "op": "set-attr",  "world": "night-city", "feature": 123,
      "entity": "feature", "key": "atlas.geo.lon", "value": "-1.5" },
    { "op": "add-feature", "world": "night-city", "collection": 90,
      "newFeature": { … } },
    { "op": "add-collection", "world": "night-city", "newCollection": { … } },
    { "op": "add-world",   "newWorld": { … } },
    { "op": "set-icon",    "world": "night-city", "collection": 90,
      "key": "std--maki-monument", "value": "std--maki-monument.svg" },
    { "op": "add-asset",   "asset": { "key": "…", "file": "…", "data": "<base64>" } },
    { "op": "set-lens",    "world": "night-city", "lens": { … } },
    { "op": "ledger",      "world": "night-city", "account": { … } }
  ]
}
```

**The vocabulary is deliberately additive.** There is no operation that removes
a feature, empties a collection, or deletes a world, so law 3 is not a rule an
enricher has to remember — it is a property of what an enricher is able to
express. The two operations that touch existing data refuse to overwrite:

| operation | what it refuses |
| --- | --- |
| `set-attr` | a key already set to a different value; an unregistered key; a key on the wrong entity; a value outside its vocabulary |
| `set-prose` | a feature that already has prose |
| `add-feature`, `add-collection` | an identifier already spoken for in that world |
| `add-world` | a slug the volume already pictures |
| `set-icon` | a collection that already names artwork |
| `set-lens` | repointing an existing lens at another tile set (a re-derivation of the same tile set updates in place, which is how a new derivation stamp lands) |
| `add-asset` | a file name already carried with different bytes |

A refused operation fails the whole application: half an account is not an
account.

### 3.2 The ordered queue

Enrichers run in an order declared in `internal/enrich/curation/curation.json`,
never in an order that emerges from a map iteration or a registration side
effect. `enrich.Queue` is strict both ways — a queued name nothing implements
fails, and an implemented enricher nothing queues fails — because an enricher
running in an order nobody wrote down is folklore.

The order today, and why:

| # | enricher | why here |
| --- | --- | --- |
| 1 | `merge` | first, so everything after sees the whole volume: a collection folded in from another reading wants an icon and a score like any other |
| 2 | `national` | after merge, so a feature another reading contributed can also learn which subwatershed its ground lies in |
| 3 | `stdicons` | after merge — the whole reason it is post-composition: a merged-in collection that declares a standard icon resolves it too |
| 4 | `lenses` | last, because a picture attaches to a ground and the grounds are not all known until everything that could contribute one has run |

Each enricher sees the volume as the ones before it left it.

### 3.3 The evidence ethic

**Silence over plausibility.** An enricher that cannot be sure says nothing and,
where the doubt is about a particular thing, writes down what it was unsure
about. A held feature with its reason recorded costs a curiosity; a wrong claim
costs a place, and a reader cannot tell a plausible claim from a true one.

There is exactly one deliberate exception, `stdicons`: a standard-icon
declaration the vendored library cannot answer **fails the build**. The
difference is who made the promise. A membership join guesses about the world; a
standard-icon declaration was written by a translator author in this repository,
and an unanswerable one is a typo that should be heard about while it is one
table edit old.

---

## 4. The ledger

Every contribution to a world is written down in one vocabulary, whatever made
it. The vocabulary is five words, on purpose: a ledger nobody can hold in their
head is a log, and a log is not an account.

| word | meaning |
| --- | --- |
| **matched** | the world already held this, and the two are one thing |
| **added** | the world did not hold this, and now does |
| **adopted** | an added thing joined a collection the world already had |
| **held** | the contribution could not decide, and says why |
| **rejected** | the contribution refused this outright, and says why |

Each world carries one **origin account** — where its ground came from and what
it held when it got there — plus one account per contribution folded in since.
The origin account is opened before the queue runs, because after an enricher
has run there is no way to find out what the world arrived with.

```jsonc
{
  "source": "IGN Wiki", "slug": "ign-wiki",
  "donorFeatures": { "point": 368, "path": 0, "area": 0 },
  "added": 37,
  "matched": [ { "d": 2123771580, "w": 1452480306, "px": 20,
                 "e": true, "took": ["atlas.note.text"] } ],
  "adopted": [ { "d": 2041587856, "into": "clothing-vendor" } ],
  "held":    [ { "d": 1775561692, "t": "Arroyo Clothes Shop",
                 "why": "beside \"Clothing Vendor\" in the same category; names disagree" } ],
  "rejected": [],
  "alignment": "99 anchors, median 26.0px, p90 52.0px, worst 67.4px"
}
```

The held and rejected reasons are **data, not messages about code**: they are
written into every merged bundle already published, and a reader comparing two
builds' accounts compares these strings. Their vocabulary is one rewrite behind
the rest of the lane — a feature is a "pin", a collection a "category" — and
that is the price of not rewriting what every existing bundle says about itself.

### 4.1 The gate

`GateAccount` and `GateWorld` fail the build rather than let a bundle be written
that quietly lost something — or quietly agreed too much:

- every offered feature of every kind is accounted for: matched + added + held
  points + rejected must equal the offered points, and every offered shape must
  appear in the held ledger (shape features do not merge yet);
- every match is one-to-one — a serving feature matched by two donors is a place
  counted twice;
- every attribute a take claims answers to the conventions registry, and the
  older `enriched` flag says exactly what the takes say;
- one identifier space per world across every kind;
- the world holds exactly as many features of each kind as its accounts claim:
  what it opened with, plus what every account says it added.

---

## 5. The enrichers

### 5.1 `merge` — two readings of one volume

The newest capture serves; the others are folded into it.

Each donor world is aligned into the serving world by the transformation their
shared named places determine (`internal/enrich/align`): pairs formed only from
names that are unambiguous in **both** readings, at least 12 of them, solved by
least squares per axis, trimmed twice to nine tenths to shed the places one side
simply pinned sloppily, and **refused outright** when the median residual still
exceeds 96px. A fit that will not close is a merge that does not happen; the two
readings stay apart in separate builds, which is exactly where they were before
anybody tried.

A donor world with no counterpart by slug is tried against every world the
volume already draws — sources do not divide the world the same way, and a
shared slug is only the cheapest evidence of shared ground. A world nothing
pictures joins whole, artwork and all, opening its own origin account.

Resolution, per donor feature, in world pixels of the volume's own square:

| distance to the nearest same-named serving feature | outcome |
| --- | --- |
| ≤ 160px (`matchRadiusPx`) | **matched** — the same place |
| ≤ 320px (`separateRadiusPx`) | **held** — too far to merge, too near to double |
| beyond, or no such name | fall through |

A name one reading spells inside the other's — "Northside Apartment" inside
"Northside, Watson Apartment" — counts as the same name when the two share a
collection **and** the shorter name carries at least two words: a bare
"Apartment" must not roam the world for a long-named cousin. A serving feature
another donor already matched cannot be matched again. Falling through, a
donor feature within `max(48px, 2 × the fit's p90)` of a serving feature in the
same collection is **held** — proximity alone never merges. A feature the
alignment puts outside the world square is **rejected**. Everything left is
**added**: into the serving collection where the two spell the same concept
(**adopted**), and otherwise under a collection of its own, filed under a group
named for its source, with its artwork carried across under a source-prefixed
key so it cannot displace the volume's own.

Collections meet under their **merge identity**: the `atlas.collection.key` the
payload carries, then the key the source gave the collection, then its artwork
key. The curated equivalents that produce that attribute live in the generate
lane's curation, so the merge reads identity off the payload rather than a table
of its own.

**Per-key attribute policy.** Serving-wins is the default: what the serving
reading says stands. The keys listed under `merge.attributes.donorFillsEmpty` in
curation are *donor-fills-empty* — the serving side keeps what it has and takes
only what it lacks — and every take is ledgered by key:

| key | why |
| --- | --- |
| `atlas.note.text` | a description; the rule this policy was invented for |
| `atlas.geo.lat`, `atlas.geo.lon` | true coordinates fill in the same way words do |
| `atlas.icon.std` | a serving collection with no artwork of any kind takes the donor's declaration |

### 5.2 `national` — the hydrologic membership join

A city's zones and trails are curated by the city; the subwatersheds under them
are surveyed by the USGS. Where a capture carries both, each piece of the city's
ground can be told which subwatershed it lies in — one sentence for the card and
the twelve-digit code beside it as `atlas.hydro.huc12`.

Fetching stays in the generate lane (fetching is crawling). **The evidence base
travels in the capture**, so the join re-runs against the archive without
refetching, which is what lets the sentence be re-curated. The evidence reaches
the enricher through `Context.Evidence` under the name `hydro/huc12.json`:

```jsonc
{
  "evidence": "atlas-enrich-evidence",
  "version": 1,
  "kind": "hydro.huc12",
  "space": "world",          // the volume's own projection — the only space v1 reads
  "units": [ { "code": "170703010801", "name": "Deschutes Junction",
               "rings": [ [ [ [lng, lat], … ] ] ] } ]
}
```

`space` is `world` because a claim is decided in the space the volume's features
are in, and whoever writes the evidence has already projected the survey into
the window it was clipped to. An evidence base that carries nothing about a
volume is not an error: it is a volume nobody surveyed, and the enricher says
nothing about it.

**The judgement.** Each piece of a feature's geometry volunteers an anchor — the
centroid of its largest outer ring, or the middle vertex of its longest line —
plus an even sample of up to 16 boundary vertices. The claim is made **only when
every volunteered position lands in the same unit**. A trail that crosses a
divide says nothing; a zone whose boundary leaves the surveyed extent says
nothing. The anchor alone would let a citywide polygon claim whichever
subwatershed its centroid happened to sit in; sampling the boundary catches the
spread, and the cost stays flat however detailed the ring.

The survey makes no claims about itself: `national.evidenceCollections` in
curation names the collections a national capture contributed to a volume, and
the join never writes a membership onto one of them. Point features are not
joined. A feature that already carries the key is left alone, and a feature that
already has prose keeps it — the claim still lands as the key, because the
sentence is a courtesy and the code is the knowledge.

### 5.3 `stdicons` — the standard glyph library

A collection that ships no artwork may name one from a standard set
(`atlas.icon.std = "maki/monument"`). This resolves the promise to bytes: the
glyph lands in the volume's icon set as `std--maki-monument.svg`, a name that
spells its provenance so a bundle listing reads honestly and a source's own
artwork can never be shadowed by it. The application never learns the library
exists — a resolved standard icon is one more asset in a bundle's icons tree.

It runs after merge so merged-in collections resolve too. Only point
collections, only where the slot is empty, one asset however many collections
name it. Only the names some translator actually speaks are vendored; the
embedded set is the vocabulary, and `Vocabulary()` lists it.

### 5.4 `lenses` — a picture attaches to a ground

A world is a ground; a lens is a picture of it. The two are separable, and that
separation is what makes this an enricher: a ground published years ago can be
given another picture — a second map style, a later capture, somebody else's
raster warped into this world's space — without the ground changing and without
anything published being touched.

This enricher does not derive pyramids. Deriving rasters is the generate lane's
work, and the derivation stamp a pyramid carries is written there. What arrives
here through `Context.Lenses` is the **offer**: these pictures were derived for
this ground, each with the stamp that says what it was made from. The enricher
attaches what the world does not already carry, updates in place what was
re-derived under a new stamp, and says nothing about a picture that has not
moved. `atlas enrich -lenses FILE` supplies the offers:

```jsonc
{ "offers": { "night-city": [ { "name": "IGN Wiki", "tileSet": "aligned-night-city",
                                "stamp": "…", "alignedWith": "cbp" } ] } }
```

Aligned pyramids that the tile set already files under a world's own tile set
are attached by composition itself; the offer file is for pictures nothing has
claimed.

---

## 6. The feature-maturity score

The score replaces bundle maturity. It is **unbounded, additive, and monotone**:
every feature earns points for each quality it verifiably has, and those sums
roll up feature → collection → world → volume.

**No denominators, no ceilings.** A share moves when its denominator moves, so a
build that added five hundred features and described half of them could read as
a regression. Percentages survive only as diagnostics. That also retires the
known `DescribedPct`-above-100% defect by construction — and where the
diagnostic still prints a share, it now divides by every feature rather than by
the point features alone, which is what produced the 235% the reference tooling
reported for the city fixture.

The table is versioned data (`internal/enrich/maturity/points.json`, v1). A
re-weighting is a **new version**, not a mass failure.

### 6.1 Point table v1

| earner | quality | points |
| --- | --- | --- |
| feature | a name | 1 |
| feature | prose at all | 1 |
| feature | prose longer than 140 characters | +1 |
| feature | true coordinates, both axes | 1 |
| feature | each membership attribute (`atlas.hydro.huc12`, and the joins after it) | 1 |
| feature | its collection resolves artwork | 1 |
| feature | geometry: `floor(log2(1 + vertices))` | ×1 |
| feature | each other reading that pinned this place, from the merge ledger | 1 |
| collection | each registered semantic-conventions key it declares | 1 |
| world | each registered world-level key it declares | 1 |
| world | each lens | 1 |
| world | each zoom level a lens holds | 1 |

Geometry is log-scaled because ground that is actually drawn is worth more than
ground that is merely claimed, while one over-noded polygon must not outweigh a
city. Corroboration is scored because two sources agreeing is evidence, and
evidence is quality. A private attribute earns nothing: the score measures
adoption of the *shared* vocabulary.

### 6.2 The monotonicity gate

An enrichment build whose score declines fails.

- Two builds are compared **only under the same table version**. Across versions
  nothing is concluded.
- A decline is permitted up to what the later build's ledger accounts for in
  `corrections`. That is the one shape a decrease may take, and it has to be
  written down. The gate exists to reward good data and never to punish the
  removal of data that was wrong.

### 6.3 The five axes, as diagnostics

Annotation, cartography, structure, icons and conventions carry over from the
reference tooling as the score's breakdown. They are absolute measurements of
one build, never ranks within a library, and nothing gates on them. `atlas
measure` prints the score first and the axes beneath it, with every merge ledger
the payloads carry.

---

## 7. Curation

`internal/enrich/curation/curation.json`, embedded, so a build carries its own
curation and an enriched volume cannot depend on what was on the operator's
disk. Every section carries its own prose as a sibling key.

```jsonc
{
  "schema": 1,
  "queue":  { "order": ["merge", "national", "stdicons", "lenses"] },
  "merge":  {
    "matchRadiusPx": 160, "separateRadiusPx": 320, "nearbyFloorPx": 48,
    "attributes": { "donorFillsEmpty": [
      "atlas.note.text", "atlas.geo.lat", "atlas.geo.lon", "atlas.icon.std" ] }
  },
  "national": { "evidenceCollections": { "bend-or": ["Watersheds", "Subwatersheds",
                                                     "Streams", "Waterbodies"] } }
}
```

The reader refuses a separate radius that is not larger than the match radius —
nothing would ever be held back — and an attribute policy for a key the
conventions registry does not know. The one deliberate exception is
`atlas.note.text`, which is registered nowhere because no payload carries it: it
is the name a description travels under so that "which description wins" is
decided and recorded in the same vocabulary as everything else.

---

## 8. The CLI

```
atlas enrich  -archive DIR -tiles INDEX [-bundles DIR] [-evidence DIR]
              [-lenses FILE] [-n] [volume...]
atlas measure [-bundles DIR] [-json] [volume...]
```

`enrich` groups every reading in the archive by the volume it is a reading of,
picks the newest capture to serve, folds the rest in, and composes. A volume the
queue had nothing to say about is not rebuilt and says so at `info` — which is
what makes running this over a whole archive cheap. `-evidence DIR` resolves as
`<dir>/<volume>/<name>`.

`measure` scores every build in a registry, newest-per-volume marked as serving.

---

## 9. What is proven

| claim | held by |
| --- | --- |
| the merge reproduces the reference tree's judgement, pair by pair | `golden/pipeline`: re-runs the merge from the two committed translator fixtures and holds it to the merged fixture's recorded ledger — 99 anchors, median 26.0px, 270 matched, 37 added (7 adopted), 61 held, every pair to the same serving feature at the same distance, every hold for the same reason |
| the membership join reproduces the reference tree's claims | `golden/pipeline`: re-runs the join over the city fixture's own payload and reproduces exactly its 88 claimed features, codes and sentences |
| a standard glyph is byte-identical to what the reference shipped | `golden/pipeline`: the vendored `maki/monument` against the city fixture's icon hash |
| enrichment raises the score, and the gate refuses the reverse | `golden/pipeline`: the city fixture with and without its membership claims |
| every fixture volume scores, reproducibly, and a score is the sum of its worlds | `golden/pipeline` |
| the seam is a copy, not a decision | `cmd/atlas`: a document adapted and adapted back is byte-identical |
| the queue curation declares and the enrichers the binary offers are the same set | `golden/pipeline` |

The whole `generate ⊕ enrich` reproduction of the merged bundle waits on the
sources that read its two captures; the test is written, names its activation
condition, and skips until then.

One divergence from the goldens is declared in `golden/waivers.json`: an
enriched build's manifest revision, and therefore its stamp and file name,
cannot equal the reference tree's, because the reference merged inside
composition at the plain revision and §2 above requires the enrich write to bump
past it. Canonical content is unaffected.
