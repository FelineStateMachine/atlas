# The capture programs are archived

The programs that recorded `golden/fixtures` used to live here. They are gone
from this branch and they are not coming back, because there is nothing left
for them to read: they opened `.atlas` files through the reference tree's
`internal/bundle`, translated archived captures through `internal/ignmap`,
`internal/pbmap`, `internal/trekmap` and `internal/arcgismap`, and measured
with `internal/measure` — every one of which is now on the tag and not on the
branch (issue #5 §7, M7).

**They are checkout-able, entire and working, at the tag `golden-reference`**
(commit `b8d28888`). That is the deal the fixtures were captured under and it
has not changed: a golden is never edited to match a candidate, and a corrected
golden comes from a re-capture *against the reference tree*, never against the
rewrite.

## Re-capturing

```sh
git worktree add ../atlas-golden-reference golden-reference
cd ../atlas-golden-reference
golden/capture/capture.sh /path/to/somewhere            # the whole set
golden/capture/capture.sh                               # in place, to commit
```

The individual programs, their flags, and what each one reads are documented
where they always were — in the tag's own `golden/fixtures/README.md`, which is
the same document as this branch's minus the archival notes. The inputs it
names (the installed `.atlas` library, the crawl archive beside the repository,
the city's own archive) are outside the repository and are unaffected by the
archival.

The same is true of `golden/parity/capture.mjs`, which recorded the parity
baselines by driving the reference tree's frontend: the frontend is on the tag,
so the recorder runs there.

## What stayed

One package moved rather than left: the fixture canonicalizer, now
`golden/canon`. It is stdlib-only, it decides the one JSON shape every fixture
is committed in, and the enforcement gates (`golden/format`, `golden/pipeline`)
compare against fixtures through it. It was never a capture program — it was
the agreement between the capture programs and the gates about what a byte
comparison is comparing, and the gates still need their half of it.
