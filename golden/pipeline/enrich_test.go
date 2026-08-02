package pipeline

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/golden/capture/canon"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	enrichcuration "github.com/FelineStateMachine/atlas/internal/enrich/curation"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/merge"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/national"
	"github.com/FelineStateMachine/atlas/internal/enrich/enrichers/stdicons"
	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/sources"
)

// The enrich lane, measured against the composed bundle fixtures.
//
// Two of these tests need nothing but what is in git, which is deliberate: the
// two judgements this lane makes that could be wrong in a way nobody notices --
// which places two readings agree about, and which ground lies in which
// subwatershed -- are held against the exact numbers the reference tree
// recorded, from committed fixtures, on every run and every machine.
//
//   - the merge is re-run from the two translator fixtures the merged volume
//     was built from, and has to reproduce that volume's recorded ledger;
//   - the membership join is re-run over the city volume's own payload, and has
//     to reproduce exactly the features the reference claimed.
//
// The third, the whole generate ⊕ enrich reproduction of the merged bundle,
// waits on the sources that read those two captures. It says so and skips.

const (
	mergedFixtureDir = "../fixtures/bundles/cyberpunk-2077"
	cityFixtureDir   = "../fixtures/bundles/bend-or"
	mergedWorld      = "night-city"
)

// The city fixture's world is its capture day, so its slug moves whenever the
// volume is re-captured -- as do its stamp, its file name and, if the survey
// beneath it moved, the number of claims the join can make. Nothing here is
// keyed on any of those: the world is read out of the fixture's own manifest,
// and what is checked is that the join reproduces exactly the claims the
// fixture carries, whatever they turn out to be.
//
// The counts recorded at capture are noted where they are logged, as an
// observation rather than an assertion. Transcribing a golden into a constant
// beside it only creates a second copy to disagree with.
func cityWorld(t *testing.T) string {
	t.Helper()
	var manifest struct {
		Worlds []struct {
			Slug string `json:"slug"`
		} `json:"worlds"`
	}
	readJSON(t, filepath.Join(cityFixtureDir, "manifest.json"), &manifest)
	if len(manifest.Worlds) != 1 {
		t.Fatalf("the city fixture holds %d worlds; this test reads the one it was captured with",
			len(manifest.Worlds))
	}
	return manifest.Worlds[0].Slug
}

// TestMergeReproducesFixtureLedger re-runs the merge over the two readings the
// merged fixture was composed from -- the Piggyback capture that serves and the
// IGN capture folded into it -- and holds the result to the ledger the
// reference tree wrote into that bundle: the alignment it stood on, and what
// became of every one of the donor's 368 features.
//
// The inputs are the committed translator fixtures, which are the reference
// tree's own translator output for those captures. They are a different shape
// from the clean room's interchange document, so this test adapts them; what is
// being checked is not a shape but a judgement, feature by feature.
func TestMergeReproducesFixtureLedger(t *testing.T) {
	tables, err := enrichcuration.Load()
	if err != nil {
		t.Fatalf("enrich curation: %v", err)
	}
	generateTables, err := curation.Load()
	if err != nil {
		t.Fatalf("generate curation: %v", err)
	}

	fixture := readMergedAccount(t)
	grid := gridOfFixture(t, mergedFixtureDir, mergedWorld)

	serving := readReferenceDocument(t, "../fixtures/translators/piggyback.doc.json",
		enrich.Source{Name: "piggyback", Label: "Piggyback"}, grid, generateTables)
	donor := readReferenceDocument(t, "../fixtures/translators/ign.doc.json",
		enrich.Source{Name: "ign-wiki", Label: "IGN Wiki"}, grid, generateTables)

	// The fixture's own ledger says which reading served and what each one
	// offered; a test that quietly merged them the other way round would still
	// produce numbers, so the premise is checked first.
	if got := enrich.Serving([]*enrich.Volume{serving, donor}); got != 0 {
		t.Fatalf("the newest capture is reading %d, not the one the fixture served", got)
	}
	if got := enrich.Tally(&serving.Worlds[0]).Point; got != 823 {
		t.Fatalf("the serving reading holds %d points, fixture origin account 823", got)
	}
	if got := enrich.Tally(&donor.Worlds[0]).Point; got != fixture.DonorFeatures.Point {
		t.Fatalf("the donor offers %d points, fixture %d", got, fixture.DonorFeatures.Point)
	}

	enrich.OpenOrigin(serving)
	contribution, err := merge.New().Enrich(serving, enrich.Context{
		Donors:   []*enrich.Volume{donor},
		Curation: tables,
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := enrich.Apply(serving, contribution); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := enrich.GateWorld(&serving.Worlds[0]); err != nil {
		t.Fatalf("gate: %v", err)
	}

	var account enrich.Account
	for _, held := range serving.Worlds[0].Ledger {
		if !held.Origin {
			account = held
		}
	}

	for _, check := range []struct {
		what      string
		got, want int
	}{
		{"matched", account.MatchedN(), fixture.MatchedN()},
		{"added", account.Added, fixture.Added},
		{"adopted", account.AdoptedN(), fixture.AdoptedN()},
		{"held", account.HeldN(), fixture.HeldN()},
		{"rejected", account.RejectedN(), fixture.RejectedN()},
		{"median match px", account.MedianMatchPx(), fixture.MedianMatchPx()},
	} {
		if check.got != check.want {
			t.Errorf("%s: %d, fixture %d", check.what, check.got, check.want)
		}
	}
	if account.Alignment != fixture.Alignment {
		t.Errorf("alignment %q, fixture %q", account.Alignment, fixture.Alignment)
	}
	if account.Slug != fixture.Slug || account.Source != fixture.Source {
		t.Errorf("account names %s/%s, fixture %s/%s",
			account.Source, account.Slug, fixture.Source, fixture.Slug)
	}

	// Pair by pair, not merely count by count: the same donor feature resolved
	// to the same serving feature, at the same distance.
	want := make(map[int64]enrich.MatchedPair, len(fixture.Matched))
	for _, pair := range fixture.Matched {
		want[pair.Donor] = pair
	}
	mismatched := 0
	for _, pair := range account.Matched {
		expected, held := want[pair.Donor]
		if !held {
			t.Errorf("donor %d matched %d, which the fixture did not match at all", pair.Donor, pair.Winner)
			continue
		}
		if pair.Winner != expected.Winner || pair.DistancePx != expected.DistancePx {
			mismatched++
			if mismatched < 4 {
				t.Errorf("donor %d matched %d at %dpx, fixture %d at %dpx",
					pair.Donor, pair.Winner, pair.DistancePx, expected.Winner, expected.DistancePx)
			}
		}
	}
	if mismatched > 0 {
		t.Errorf("%d of %d pairs resolved differently", mismatched, len(account.Matched))
	}

	// Adoption and holding are judgements too, and the fixture recorded both.
	adopted := map[int64]string{}
	for _, item := range account.Adopted {
		adopted[item.Donor] = item.Into
	}
	for _, item := range fixture.Adopted {
		if into, held := adopted[item.Donor]; !held || into != item.Into {
			t.Errorf("donor %d joined %q, fixture %q", item.Donor, into, item.Into)
		}
	}
	heldFor := map[int64]string{}
	for _, item := range account.Held {
		heldFor[item.Donor] = item.Reason
	}
	for _, item := range fixture.Held {
		reason, was := heldFor[item.Donor]
		if !was {
			t.Errorf("donor %q was carried; the fixture held it: %s", item.Title, item.Reason)
			continue
		}
		if reason != item.Reason {
			t.Errorf("donor %q held for %q, fixture %q", item.Title, reason, item.Reason)
		}
	}

	t.Logf("merge reproduced: %s · %d matched (median %dpx) · %d added (%d adopted) · %d held",
		account.Alignment, account.MatchedN(), account.MedianMatchPx(),
		account.Added, account.AdoptedN(), account.HeldN())
}

// TestNationalJoinReproducesCityClaims re-runs the hydrologic membership join
// over the city fixture's own payload and holds it to exactly the features the
// reference tree claimed: the same features, the same twelve-digit codes, the
// same sentence.
//
// The evidence base is the surveyed subwatersheds, which travel in the capture
// and, having been composed, travel in the volume as well -- so this test can
// state its evidence from the fixture and re-run the judgement with no archive
// present. That is the property the lane's contract asks for: the join re-runs
// from the evidence without refetching anything.
func TestNationalJoinReproducesCityClaims(t *testing.T) {
	tables, err := enrichcuration.Load()
	if err != nil {
		t.Fatalf("enrich curation: %v", err)
	}
	world := cityWorld(t)
	payload := readPayload(t, cityFixtureDir, world)
	evidence := evidenceOf(t, payload, "Subwatersheds")
	if len(evidence.Units) == 0 {
		t.Fatal("the fixture carries no surveyed units, so there is nothing to re-run the join against")
	}

	// What the reference claimed, read straight off the fixture.
	type claim struct {
		code     string
		sentence string
	}
	text := readText(t, cityFixtureDir, world)
	want := map[int64]claim{}
	for _, collection := range payload.Collections {
		for _, feature := range collection.Features {
			code := feature.Attrs[semconv.KeyHydroHUC12]
			if code == "" {
				continue
			}
			want[feature.ID] = claim{code: code, sentence: text[strconv.FormatInt(feature.ID, 10)].Description}
		}
	}
	if len(want) == 0 {
		t.Fatal("the fixture claims no memberships, so this test would prove nothing")
	}

	// The join is re-run over the volume as it stood before anybody joined it:
	// the claims and the sentences they earned are taken back out, and the
	// evidence has to put exactly those back.
	volume := volumeOfPayload(t, "bend-or", world, payload)
	for _, held := range volume.Worlds {
		for _, collection := range held.Collections {
			for index := range collection.Features {
				feature := &collection.Features[index]
				if feature.Attrs[semconv.KeyHydroHUC12] == "" {
					continue
				}
				delete(feature.Attrs, semconv.KeyHydroHUC12)
				feature.Description = ""
			}
		}
	}

	contribution, err := national.New().Enrich(volume, enrich.Context{
		Evidence: staticEvidence{national.EvidenceName: marshal(t, evidence)},
		Curation: tables,
	})
	if err != nil {
		t.Fatalf("national: %v", err)
	}

	got := map[int64]claim{}
	for _, op := range contribution.Ops {
		held := got[op.Feature]
		switch op.Kind {
		case enrich.OpSetAttr:
			if op.Key != semconv.KeyHydroHUC12 {
				t.Errorf("the join wrote %q, which is not a membership", op.Key)
			}
			held.code = op.Value
		case enrich.OpSetProse:
			held.sentence = op.Value
		default:
			t.Errorf("the join made a %s operation; it claims memberships and says so, nothing else", op.Kind)
		}
		got[op.Feature] = held
	}

	if len(got) != len(want) {
		t.Errorf("the join claims %d features, the fixture %d", len(got), len(want))
	}
	for id, expected := range want {
		claimed, made := got[id]
		if !made {
			t.Errorf("feature %d lost its claim of %s", id, expected.code)
			continue
		}
		if claimed.code != expected.code {
			t.Errorf("feature %d claims %s, fixture %s", id, claimed.code, expected.code)
		}
		if claimed.sentence != expected.sentence {
			t.Errorf("feature %d says %q, fixture %q", id, claimed.sentence, expected.sentence)
		}
	}
	for id := range got {
		if _, expected := want[id]; !expected {
			t.Errorf("feature %d gained a claim the fixture does not make", id)
		}
	}

	if err := enrich.Apply(volume, contribution); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Logf("membership join reproduced: %d features claimed from %d surveyed units "+
		"(88 from 12 when this fixture was captured)", len(got), len(evidence.Units))
}

// TestStandardIconResolvesFixtureBytes holds the vendored library to the bytes
// the city fixture carries. A standard icon is an asset, and an asset that is
// almost the same asset is a different asset.
func TestStandardIconResolvesFixtureBytes(t *testing.T) {
	var icons struct {
		Icons []struct {
			Name   string `json:"name"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"icons"`
	}
	readJSON(t, filepath.Join(cityFixtureDir, "icons.json"), &icons)
	if len(icons.Icons) != 1 {
		t.Fatalf("the city fixture carries %d icons, expected the one standard glyph", len(icons.Icons))
	}
	fixture := icons.Icons[0]

	data, asset, err := stdicons.Standard("maki/monument")
	if err != nil {
		t.Fatal(err)
	}
	if "icons/"+asset != fixture.Name {
		t.Errorf("asset name %q, fixture %q", "icons/"+asset, fixture.Name)
	}
	if len(data) != fixture.Bytes || bundle.HashBytes(data) != fixture.SHA256 {
		t.Errorf("asset is %d bytes %s, fixture %d bytes %s",
			len(data), bundle.HashBytes(data), fixture.Bytes, fixture.SHA256)
	}

	// And the enricher reaches the same conclusion from the volume: the
	// collection that declares the glyph is the one that gets it.
	world := cityWorld(t)
	payload := readPayload(t, cityFixtureDir, world)
	volume := volumeOfPayload(t, "bend-or", world, payload)
	for index := range volume.Worlds[0].Collections {
		// The fixture is composed, so its artwork is already resolved; the
		// enricher's subject is a volume on its way to composition.
		volume.Worlds[0].Collections[index].IconAsset = ""
		volume.Worlds[0].Collections[index].Icon = ""
	}
	contribution, err := stdicons.New().Enrich(volume, enrich.Context{})
	if err != nil {
		t.Fatalf("stdicons: %v", err)
	}
	assets, resolved := 0, 0
	for _, op := range contribution.Ops {
		switch op.Kind {
		case enrich.OpAddAsset:
			assets++
			if op.Asset.File != asset || bundle.HashBytes(op.Asset.Data) != fixture.SHA256 {
				t.Errorf("carried asset %s does not match the fixture", op.Asset.File)
			}
		case enrich.OpSetIcon:
			resolved++
		}
	}
	if assets != 1 || resolved != 1 {
		t.Errorf("the enricher carried %d assets for %d collections; the fixture has one of each",
			assets, resolved)
	}
}

// TestMaturityScoresEveryBundleFixture scores every committed bundle fixture
// and reports it. The numbers are not fixed here -- a point table is allowed to
// be re-weighted, and pinning its output would make every re-weighting a
// fixture edit -- but three properties are: a score is positive, it is the sum
// of its worlds, and it is reproducible.
func TestMaturityScoresEveryBundleFixture(t *testing.T) {
	table, err := maturity.Points()
	if err != nil {
		t.Fatalf("point table: %v", err)
	}
	entries, err := os.ReadDir("../fixtures/bundles")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			score := scoreFixture(t, filepath.Join("../fixtures/bundles", entry.Name()), table)
			if score.Total <= 0 {
				t.Errorf("scored %d points", score.Total)
			}
			sum := 0
			for _, world := range score.Worlds {
				if world.Total != world.Features+world.Collections+world.World {
					t.Errorf("world %s: %d is not %d+%d+%d", world.Slug,
						world.Total, world.Features, world.Collections, world.World)
				}
				sum += world.Total
			}
			if sum != score.Total {
				t.Errorf("volume scores %d, its worlds %d", score.Total, sum)
			}
			again := scoreFixture(t, filepath.Join("../fixtures/bundles", entry.Name()), table)
			if again.Total != score.Total {
				t.Errorf("two scorings of one build disagree: %d then %d", score.Total, again.Total)
			}
			t.Logf("%s: %d points (table v%d) · %d point features, %d described · "+
				"%d shapes, %d vertices · %d memberships · %d corroborations",
				score.Volume, score.Total, score.TableVersion, score.Axes.Points,
				score.Axes.Described, score.Axes.Shapes, score.Axes.Vertices,
				score.Axes.Memberships, corroborations(score))
		})
	}
}

// TestEnrichmentRaisesTheScore is the monotonicity property, exercised on real
// data: the city fixture stripped of its claims scores lower than the city
// fixture with them, and the gate refuses the reverse.
func TestEnrichmentRaisesTheScore(t *testing.T) {
	table, err := maturity.Points()
	if err != nil {
		t.Fatal(err)
	}
	enriched := scoreFixture(t, cityFixtureDir, table)
	plain := scoreFixtureWithout(t, cityFixtureDir, table)

	if plain.Total >= enriched.Total {
		t.Fatalf("the joined build scores %d and the unjoined one %d; enrichment has to be worth something",
			enriched.Total, plain.Total)
	}
	if err := maturity.Gate(plain, enriched); err != nil {
		t.Errorf("the gate refused an enrichment that added %d points: %v",
			enriched.Total-plain.Total, err)
	}
	if err := maturity.Gate(enriched, plain); err == nil {
		t.Error("the gate accepted a build that lost every membership claim")
	}
	t.Logf("membership claims and their prose are worth %d points on this build (%d → %d)",
		enriched.Total-plain.Total, plain.Total, enriched.Total)
}

// TestGenerateEnrichReproducesMergedBundle is the whole gate: the merged volume
// rebuilt from its archived captures by generate ⊕ enrich, held against the
// composed fixture.
//
// It runs the shipped command rather than a reassembly of it. The ⊕ is
// `atlas enrich` -- translate every reading of the volume, adapt each into the
// enrich lane's model, run the curated queue over the one that serves, adapt the
// result back and compose it -- and a gate that re-implemented that sequence in
// its own words would be measuring a second implementation nobody ships. So this
// starts the binary, points it at the archive and the derived tile set, and
// holds what lands in an empty registry against every extraction the reference
// build was captured into.
//
// # What must agree, and what cannot
//
// Canonical content is mandatory and unwaived: the world payload, the packed
// locations, the deferred prose, the icon set, the tile inventory of both
// pyramids, and the archive's entry order. Those are what the volume *is*.
//
// The manifest agrees on everything except `version`, and that difference is
// the `enriched-build-revision` waiver rather than a content difference. The
// reference merged inside composition and wrote the plain policy revision; the
// clean room's enrich write bumps the revision past the serving build's so the
// registry fold deterministically serves the enriched build (issue #5 §5.3), and
// a revision rides the manifest, the manifest rides the stamp, and the stamp
// rides the file name. Each of the three is reported here by name, so the cost
// stays legible instead of being implied by a waiver id.
func TestGenerateEnrichReproducesMergedBundle(t *testing.T) {
	missing := missingSources("piggyback", "ign")
	if len(missing) > 0 {
		t.Skipf("awaiting the %s source%s: the merged fixture is composed from their captures, "+
			"and no document can be produced without them. This test activates when "+
			"internal/generate/sources answers to them and the archive is present "+
			"(ATLAS_ARCHIVE_DIR, ATLAS_TILES_INDEX); the merge's own judgement is "+
			"already held to this fixture's ledger by TestMergeReproducesFixtureLedger",
			strings.Join(missing, " and "), plural(len(missing)))
	}
	built := enrichFixture(t, "cyberpunk-2077")
	fixture := readVolumeFixture(t, "cyberpunk-2077")
	dir := mergedFixtureDir

	t.Run("part hashes", func(t *testing.T) {
		for _, name := range sortedKeys(fixture.PartHashes) {
			data, err := built.reader.ReadEntry(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			got := bundle.HashBytes(data)
			if name == bundle.ManifestName {
				// The one stamped part the revision reaches. Its content is
				// compared field by field below.
				if got != fixture.PartHashes[name] {
					t.Logf("%s: hash %s, fixture %s — the waived revision (%s)",
						name, got, fixture.PartHashes[name], revisionWaiver)
				}
				continue
			}
			if got != fixture.PartHashes[name] {
				t.Errorf("%s: hash %s, fixture %s", name, got, fixture.PartHashes[name])
			}
		}
	})

	t.Run("manifest", func(t *testing.T) { compareMergedManifest(t, built, dir) })

	t.Run("world payloads", func(t *testing.T) {
		for _, entry := range built.reader.Manifest.Worlds {
			compareCanon(t, built,
				bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix),
				filepath.Join(dir, "worlds", entry.Slug+".payload.json"))
			compareCanon(t, built,
				bundle.WorldEntryName(entry.Slug, bundle.TextSuffix),
				filepath.Join(dir, "worlds", entry.Slug+".text.json"))
			compareLocations(t, built, dir, entry.Slug)
		}
	})

	t.Run("icons", func(t *testing.T) { compareIcons(t, built, dir) })

	t.Run("tile inventory", func(t *testing.T) { compareTiles(t, built, dir, fixture) })

	t.Run("entry order", func(t *testing.T) {
		if got := hashOf(built.reader.Names()); got != fixture.EntryOrder.SHA256 {
			t.Errorf("entry order %s, fixture %s", got, fixture.EntryOrder.SHA256)
		}
	})

	// The stamp, reported honestly. Identity is unreachable for an enriched
	// build by construction, so what is asserted is that the divergence is
	// exactly the one the waiver describes and nothing more: the capture time
	// is unmoved, the revision is the enrich lane's bump of the fixture's own,
	// and the file is named after the stamp that follows from them.
	t.Run("stamp", func(t *testing.T) {
		got := built.reader.Manifest.Version
		if got.CreatedAt != fixture.CreatedAt {
			t.Errorf("createdAt %q, fixture %q — a build's creation time is "+
				"capture-derived and enrichment must not move it", got.CreatedAt, fixture.CreatedAt)
		}
		want, err := enrich.BuildRevision(fixture.Revision)
		if err != nil {
			t.Fatalf("build revision: %v", err)
		}
		if got.Revision != want {
			t.Errorf("revision %d; the fixture's %d enriched is %d",
				got.Revision, fixture.Revision, want)
		}
		if got.Stamp == fixture.Stamp {
			t.Errorf("an enriched build stamped identically to the plain build "+
				"beside it (%s), so nothing would win the registry fold", got.Stamp)
		}
		if built.file == fixture.File {
			t.Errorf("the enriched build took the plain build's file name %s", built.file)
		}
		t.Logf("WAIVED %s: stamp %s (fixture %s), revision %d (fixture %d), file %s",
			revisionWaiver, got.Stamp[:12], fixture.Stamp[:12],
			got.Revision, fixture.Revision, built.file)
	})
}

// revisionWaiver names the entry in golden/waivers.json this test reports
// against, so a reader of a failure can find the reason without grepping.
const revisionWaiver = "enriched-build-revision"

// enrichFixture runs `atlas enrich` over the archive for one volume and opens
// what it installed.
//
// The subprocess is the point: `runEnrich` is the ⊕, and the gate's claim is
// about the pipeline a person runs, not about a sequence of library calls a
// test arranged in the same order.
func enrichFixture(t *testing.T, volume string) composedBundle {
	t.Helper()
	registry := t.TempDir()
	command := exec.Command("go", "run", "github.com/FelineStateMachine/atlas/cmd/atlas",
		"enrich",
		"-archive", archiveDir(t),
		"-tiles", tileIndex(t),
		"-bundles", registry,
		"-log-level", "warn",
		volume)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("atlas enrich %s: %v\n%s", volume, err, output)
	}

	entries, err := filepath.Glob(filepath.Join(registry, "*.atlas"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("atlas enrich installed %d bundles into an empty registry\n%s",
			len(entries), output)
	}
	reader, err := bundle.Open(entries[0])
	if err != nil {
		t.Fatalf("open %s: %v", entries[0], err)
	}
	t.Cleanup(func() { reader.Close() })
	if err := reader.Validate(); err != nil {
		t.Fatalf("validate %s: %v", entries[0], err)
	}
	info, err := os.Stat(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	return composedBundle{
		reader: reader,
		file:   filepath.Base(entries[0]),
		sha256: hashFile(t, entries[0]),
		bytes:  info.Size(),
	}
}

// compareMergedManifest holds the built manifest to the fixture's, field by
// field, with `version` set aside: the whole of the enriched build's difference
// lives in that one object, and the stamp subtest above accounts for it. Setting
// it aside rather than skipping the manifest is what keeps every other field --
// the tile grid, the volume identity, each world's counts and capture time --
// checked byte for byte.
func compareMergedManifest(t *testing.T, built composedBundle, dir string) {
	t.Helper()
	raw, err := built.reader.ReadEntry(bundle.ManifestName)
	if err != nil {
		t.Fatalf("read %s: %v", bundle.ManifestName, err)
	}
	got := withoutVersion(t, raw)
	want := withoutVersion(t, readFile(t, filepath.Join(dir, "manifest.json")))
	if got != want {
		t.Errorf("manifest differs from %s/manifest.json outside version\n%s",
			dir, firstDifference(want, got))
	}
}

func withoutVersion(t *testing.T, raw []byte) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	delete(value, "version")
	out, err := canon.Value(value)
	if err != nil {
		t.Fatalf("canonicalize manifest: %v", err)
	}
	return string(out)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// missingSources names which of the given sources the generate lane does not
// answer to yet.
func missingSources(names ...string) []string {
	var missing []string
	for _, name := range names {
		if !knownSource(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// TestCuratedQueueMatchesTheEnrichersOffered holds the two halves of the
// ordered queue to each other: every name curation declares is answered, and
// every enricher this binary offers is queued.
func TestCuratedQueueMatchesTheEnrichersOffered(t *testing.T) {
	tables, err := enrichcuration.Load()
	if err != nil {
		t.Fatal(err)
	}
	queue, err := enrich.Queue(tables.Queue(), enrichers.All())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, enricher := range queue {
		names = append(names, enricher.Name())
	}
	t.Logf("the queue runs %s", strings.Join(names, " → "))
}

// --- fixture reading ---------------------------------------------------------

// fixturePayload is the composed world payload, as these tests read it.
type fixturePayload struct {
	Attrs map[string]string `json:"attrs"`
	Grid  *struct {
		SourceZoom int `json:"sourceZoom"`
		FirstTile  int `json:"firstTile"`
	} `json:"grid"`
	Lenses []struct {
		Name    string `json:"name"`
		Tiles   string `json:"tiles"`
		MinZoom int    `json:"minZoom"`
		MaxZoom int    `json:"maxZoom"`
	} `json:"lenses"`
	Collections []struct {
		ID          int64             `json:"id"`
		Title       string            `json:"title"`
		Group       string            `json:"group"`
		Kind        string            `json:"kind"`
		Icon        string            `json:"icon"`
		IconAsset   string            `json:"iconAsset"`
		IconPicture bool              `json:"iconPicture"`
		Visible     bool              `json:"visible"`
		Attrs       map[string]string `json:"attrs"`
		Features    []struct {
			ID       int64             `json:"id"`
			Title    string            `json:"title"`
			Subtitle string            `json:"subtitle"`
			HasText  bool              `json:"hasText"`
			Attrs    map[string]string `json:"attrs"`
			Geometry []enrich.Geometry `json:"geometry"`
		} `json:"features"`
	} `json:"collections"`
	Merged []enrich.Account `json:"merged"`
}

type fixtureText map[string]struct {
	Description string            `json:"d"`
	Attrs       map[string]string `json:"a"`
}

func readPayload(t *testing.T, dir, world string) fixturePayload {
	t.Helper()
	var payload fixturePayload
	readJSON(t, filepath.Join(dir, "worlds", world+".payload.json"), &payload)
	return payload
}

func readText(t *testing.T, dir, world string) fixtureText {
	t.Helper()
	var text fixtureText
	readJSON(t, filepath.Join(dir, "worlds", world+".text.json"), &text)
	return text
}

func readMergedAccount(t *testing.T) enrich.Account {
	t.Helper()
	payload := readPayload(t, mergedFixtureDir, mergedWorld)
	for _, account := range payload.Merged {
		if !account.Origin {
			return account
		}
	}
	t.Fatal("the merged fixture carries no donor account")
	return enrich.Account{}
}

// gridOfFixture is the window the fixture's world was cut from.
func gridOfFixture(t *testing.T, dir, world string) enrich.Grid {
	t.Helper()
	var manifest struct {
		TileGrid struct {
			SourceZoom int `json:"sourceZoom"`
			FirstTile  int `json:"firstTile"`
			TileSize   int `json:"tileSize"`
			Size       int `json:"size"`
		} `json:"tileGrid"`
	}
	readJSON(t, filepath.Join(dir, "manifest.json"), &manifest)
	grid := enrich.Grid{
		SourceZoom: manifest.TileGrid.SourceZoom,
		FirstTile:  manifest.TileGrid.FirstTile,
		TileSize:   manifest.TileGrid.TileSize,
		Size:       manifest.TileGrid.Size,
	}
	payload := readPayload(t, dir, world)
	if payload.Grid != nil {
		grid.SourceZoom = payload.Grid.SourceZoom
		grid.FirstTile = payload.Grid.FirstTile
	}
	return grid
}

// volumeOfPayload adapts a composed fixture payload into a volume, for the
// enrichers whose subject is a volume that already exists.
func volumeOfPayload(t *testing.T, slug, world string, payload fixturePayload) *enrich.Volume {
	t.Helper()
	out := &enrich.Volume{Slug: slug, Title: slug}
	adapted := enrich.World{Slug: world, Title: world, Attrs: payload.Attrs}
	for _, collection := range payload.Collections {
		adaptedCollection := enrich.Collection{
			ID:          collection.ID,
			Title:       collection.Title,
			Group:       collection.Group,
			Kind:        collection.Kind,
			Icon:        collection.Icon,
			IconAsset:   collection.IconAsset,
			IconPicture: collection.IconPicture,
			Visible:     collection.Visible,
			Attrs:       collection.Attrs,
		}
		for _, feature := range collection.Features {
			adaptedCollection.Features = append(adaptedCollection.Features, enrich.Feature{
				ID:       feature.ID,
				Title:    feature.Title,
				Subtitle: feature.Subtitle,
				Attrs:    feature.Attrs,
				Geometry: feature.Geometry,
			})
		}
		adapted.Collections = append(adapted.Collections, adaptedCollection)
	}
	out.Worlds = []enrich.World{adapted}
	return out
}

// evidenceOf states the surveyed units from the fixture's own hydrography
// collection: the code and name a subwatershed carries in its subtitle, and the
// ground it draws.
//
// This is the test's adapter, not the lane's. In the pipeline the evidence
// document is written by whoever captured the survey; here it is recovered from
// the volume the reference tree composed, which is the same evidence by another
// road and is what lets this judgement be re-run with nothing but git.
func evidenceOf(t *testing.T, payload fixturePayload, collection string) national.Evidence {
	t.Helper()
	out := national.Evidence{
		Evidence: national.EvidenceDoc,
		Version:  national.EvidenceVersion,
		Kind:     national.EvidenceKind,
		Space:    national.SpaceWorld,
	}
	for _, held := range payload.Collections {
		if held.Title != collection {
			continue
		}
		for _, feature := range held.Features {
			_, code, found := strings.Cut(feature.Subtitle, "HUC ")
			if !found {
				t.Fatalf("subwatershed %q carries no code", feature.Title)
			}
			var rings [][][][2]float64
			for _, geometry := range feature.Geometry {
				rings = append(rings, geometry.Rings()...)
			}
			out.Units = append(out.Units, national.Unit{
				Code: strings.TrimSpace(code), Name: feature.Title, Rings: rings,
			})
		}
	}
	return out
}

// staticEvidence is an evidence base held in memory.
type staticEvidence map[string][]byte

func (s staticEvidence) Open(name string) ([]byte, bool, error) {
	data, held := s[name]
	return data, held, nil
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mergeReferenceDocument is the reference tree's translator output, which is the
// shape the committed translator fixtures are in.
type mergeReferenceDocument struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	ID    int64  `json:"id"`
	Game  struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"game"`
	Config struct {
		TileSets []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"tile_sets"`
	} `json:"config"`
	Groups []struct {
		Title      string `json:"title"`
		Categories []struct {
			ID        int64             `json:"id"`
			Title     string            `json:"title"`
			Icon      string            `json:"icon"`
			Visible   bool              `json:"visible"`
			Attrs     map[string]string `json:"atlas_attrs"`
			Locations []struct {
				ID          json.Number       `json:"id"`
				Title       string            `json:"title"`
				Latitude    json.Number       `json:"latitude"`
				Longitude   json.Number       `json:"longitude"`
				Description string            `json:"description"`
				Attrs       map[string]string `json:"atlas_attrs"`
			} `json:"locations"`
		} `json:"categories"`
	} `json:"groups"`
}

// readReferenceDocument adapts one committed translator fixture into a volume.
//
// The one judgement it carries over from composition is the merge identity: the
// generate lane writes a curated shared name onto a collection as
// atlas.collection.key, and a merge reads identity off that attribute. Stating
// it here is what makes this a test of the merge rather than a test of the
// curation table.
func readReferenceDocument(
	t *testing.T,
	path string,
	source enrich.Source,
	grid enrich.Grid,
	tables curation.Tables,
) *enrich.Volume {
	t.Helper()
	var reference mergeReferenceDocument
	readJSON(t, path, &reference)

	out := &enrich.Volume{Slug: reference.Game.Slug, Title: reference.Game.Title, Source: source}
	world := enrich.World{
		ID:         reference.ID,
		Slug:       reference.Slug,
		Title:      reference.Title,
		Grid:       grid,
		CapturedAt: capturedAt(t, path),
	}
	for _, set := range reference.Config.TileSets {
		world.Lenses = append(world.Lenses, enrich.Lens{Name: set.Name, TileSet: set.Path})
	}
	for _, group := range reference.Groups {
		for _, category := range group.Categories {
			attrs := map[string]string{}
			for key, value := range category.Attrs {
				attrs[key] = value
			}
			if shared := tables.CollectionEquivalent(out.Slug, category.Icon); shared != "" {
				attrs[semconv.KeyCollectionKey] = shared
			}
			collection := enrich.Collection{
				ID:      category.ID,
				Title:   category.Title,
				Group:   group.Title,
				Kind:    enrich.KindPoint,
				Icon:    category.Icon,
				Visible: category.Visible,
				Attrs:   attrs,
			}
			for _, location := range category.Locations {
				collection.Features = append(collection.Features, enrich.Feature{
					ID:          number(t, location.ID),
					Title:       location.Title,
					Description: location.Description,
					At: &enrich.Position{
						Lat: decimal(t, location.Latitude),
						Lng: decimal(t, location.Longitude),
					},
					Attrs: location.Attrs,
				})
			}
			world.Collections = append(world.Collections, collection)
		}
	}
	out.Worlds = []enrich.World{world}
	return out
}

// capturedAt reads the capture time out of the translator fixture's own
// metadata, which is what decides which reading serves.
func capturedAt(t *testing.T, docPath string) string {
	t.Helper()
	var fixture struct {
		Capture struct {
			CapturedAt string `json:"capturedAt"`
		} `json:"capture"`
	}
	readJSON(t, strings.Replace(docPath, ".doc.json", ".fixture.json", 1), &fixture)
	if fixture.Capture.CapturedAt == "" {
		t.Fatalf("%s records no capture time", docPath)
	}
	return fixture.Capture.CapturedAt
}

func number(t *testing.T, value json.Number) int64 {
	t.Helper()
	parsed, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func decimal(t *testing.T, value json.Number) float64 {
	t.Helper()
	parsed, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// --- scoring fixtures --------------------------------------------------------

// scoreFixture scores a committed bundle fixture from its extractions. The
// fixtures are canonicalized extractions rather than archives, which is exactly
// why the score's own reading is a pure function of payload bytes.
func scoreFixture(t *testing.T, dir string, table maturity.Table) *maturity.Score {
	t.Helper()
	return scoreParts(t, dir, table, nil)
}

// scoreFixtureWithout scores the same fixture with every membership claim and
// the sentence it earned taken back out, which is what the build before the
// join looked like.
func scoreFixtureWithout(t *testing.T, dir string, table maturity.Table) *maturity.Score {
	t.Helper()
	return scoreParts(t, dir, table, func(payload, text []byte) ([]byte, []byte) {
		var world map[string]any
		if err := json.Unmarshal(payload, &world); err != nil {
			t.Fatal(err)
		}
		claimed := map[string]bool{}
		collections, _ := world["collections"].([]any)
		for _, entry := range collections {
			collection, _ := entry.(map[string]any)
			features, _ := collection["features"].([]any)
			for _, held := range features {
				feature, _ := held.(map[string]any)
				attrs, _ := feature["attrs"].(map[string]any)
				if attrs == nil || attrs[semconv.KeyHydroHUC12] == nil {
					continue
				}
				delete(attrs, semconv.KeyHydroHUC12)
				if id, ok := feature["id"].(float64); ok {
					claimed[strconv.FormatInt(int64(id), 10)] = true
				}
			}
		}
		var prose map[string]any
		if err := json.Unmarshal(text, &prose); err != nil {
			t.Fatal(err)
		}
		for id := range claimed {
			delete(prose, id)
		}
		return marshal(t, world), marshal(t, prose)
	})
}

func scoreParts(
	t *testing.T,
	dir string,
	table maturity.Table,
	rewrite func(payload, text []byte) ([]byte, []byte),
) *maturity.Score {
	t.Helper()
	var manifest bundle.Manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &manifest)

	parts := make([]maturity.WorldParts, 0, len(manifest.Worlds))
	for _, entry := range manifest.Worlds {
		payload := readFile(t, filepath.Join(dir, "worlds", entry.Slug+".payload.json"))
		text := readFile(t, filepath.Join(dir, "worlds", entry.Slug+".text.json"))
		if rewrite != nil {
			payload, text = rewrite(payload, text)
		}
		parts = append(parts, maturity.WorldParts{
			Slug:      entry.Slug,
			Payload:   payload,
			Text:      text,
			Locations: locationsOf(t, dir, entry.Slug),
		})
	}
	score, err := maturity.ScoreParts(manifest, parts, table)
	if err != nil {
		t.Fatalf("score %s: %v", dir, err)
	}
	return score
}

func locationsOf(t *testing.T, dir, world string) []bundle.Location {
	t.Helper()
	var fixture struct {
		Locations []struct {
			ID     int64   `json:"id"`
			Owner  uint16  `json:"owner"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Member int64   `json:"member"`
			Shard  int64   `json:"shard"`
			Title  string  `json:"title"`
		} `json:"locations"`
	}
	readJSON(t, filepath.Join(dir, "worlds", world+".locations.json"), &fixture)
	out := make([]bundle.Location, 0, len(fixture.Locations))
	for _, location := range fixture.Locations {
		out = append(out, bundle.Location{
			ID: location.ID, Owner: location.Owner, Lat: location.Lat, Lng: location.Lng,
			Member: location.Member, Shard: location.Shard, Title: location.Title,
		})
	}
	return out
}

func corroborations(score *maturity.Score) int {
	count := 0
	for _, line := range score.Ledger {
		count += line.Account.MatchedN()
	}
	return count
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// knownSource reports whether the generate lane answers to a source name.
func knownSource(name string) bool {
	_, err := sources.For(name)
	return err == nil
}
