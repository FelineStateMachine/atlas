package maturity

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/bundle"
	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
)

func points(t *testing.T) Table {
	t.Helper()
	table, err := Points()
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// build states one world's payloads outright, so a case can say exactly what a
// feature has and ask what that is worth.
type build struct {
	attrs       map[string]string
	lenses      []map[string]any
	collections []map[string]any
	text        map[string]any
	locations   []bundle.Location
	merged      []enrich.Account
	revision    int
}

func (b build) score(t *testing.T, table Table) *Score {
	t.Helper()
	payload := map[string]any{
		"attrs":       b.attrs,
		"lenses":      b.lenses,
		"collections": b.collections,
		"merged":      b.merged,
	}
	manifest := bundle.Manifest{
		Format:        bundle.Format,
		FormatVersion: bundle.FormatVersion,
		Conventions:   semconv.Version,
		Volume:        bundle.Volume{Slug: "sample", Title: "Sample"},
		Version: bundle.Version{
			Stamp:     strings.Repeat("a", 64),
			CreatedAt: "2026-08-01T00:00:00Z",
			Revision:  b.revision,
		},
		TileGrid: bundle.TileGrid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192},
		Worlds: []bundle.WorldEntry{{
			Slug: "world", Title: "World", UpdatedAt: "2026-08-01T00:00:00Z",
			Points: len(b.locations),
		}},
	}
	text := b.text
	if text == nil {
		text = map[string]any{}
	}
	score, err := ScoreParts(manifest, []maturityParts{{
		Slug:      "world",
		Payload:   marshal(t, payload),
		Text:      marshal(t, text),
		Locations: b.locations,
	}}[0].asParts(), table)
	if err != nil {
		t.Fatal(err)
	}
	return score
}

// maturityParts is a spelling helper so the table above reads as data.
type maturityParts WorldParts

func (p maturityParts) asParts() []WorldParts { return []WorldParts{WorldParts(p)} }

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func pointCollection(attrs map[string]string) map[string]any {
	return map[string]any{"id": 1, "title": "Chests", "kind": "point", "attrs": attrs}
}

func TestAFeatureEarnsForEachQualityItHas(t *testing.T) {
	table := points(t)
	long := strings.Repeat("prose ", 40)

	cases := []struct {
		what  string
		text  map[string]any
		extra int
	}{
		{what: "a name alone", text: map[string]any{}},
		{
			what:  "and a short description",
			text:  map[string]any{"1": map[string]any{"d": "A chest."}},
			extra: table.Feature.Prose,
		},
		{
			what:  "and prose past the threshold",
			text:  map[string]any{"1": map[string]any{"d": long}},
			extra: table.Feature.Prose + table.Feature.ProseSubstance,
		},
		{
			what: "and true coordinates",
			text: map[string]any{"1": map[string]any{
				"a": map[string]string{semconv.KeyGeoLat: "1", semconv.KeyGeoLon: "2"}}},
			extra: table.Feature.Geo,
		},
		{
			what: "one axis alone is not coordinates",
			text: map[string]any{"1": map[string]any{
				"a": map[string]string{semconv.KeyGeoLat: "1"}}},
		},
		{
			what: "and a membership",
			text: map[string]any{"1": map[string]any{
				"a": map[string]string{semconv.KeyHydroHUC12: "170703010801"}}},
			extra: table.Feature.Membership,
		},
	}

	base := build{
		collections: []map[string]any{pointCollection(nil)},
		locations:   []bundle.Location{{ID: 1, Title: "A chest"}},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			with := base
			with.text = c.text
			want := table.Feature.Name + c.extra
			if got := with.score(t, table).Worlds[0].Features; got != want {
				t.Errorf("the feature earned %d, want %d", got, want)
			}
		})
	}

	t.Run("a nameless feature earns nothing for a name", func(t *testing.T) {
		nameless := base
		nameless.locations = []bundle.Location{{ID: 1, Title: "  "}}
		if got := nameless.score(t, table).Worlds[0].Features; got != 0 {
			t.Errorf("a nameless feature earned %d", got)
		}
	})
}

func TestACollectionThatResolvesArtworkPaysItsFeatures(t *testing.T) {
	table := points(t)
	plain := build{
		collections: []map[string]any{pointCollection(nil)},
		locations:   []bundle.Location{{ID: 1, Title: "A chest"}, {ID: 2, Title: "Another"}},
	}
	arted := plain
	arted.collections = []map[string]any{{
		"id": 1, "title": "Chests", "kind": "point", "iconAsset": "chest.svg",
	}}
	before := plain.score(t, table).Worlds[0].Features
	after := arted.score(t, table).Worlds[0].Features
	if after-before != 2*table.Feature.Icon {
		t.Errorf("artwork was worth %d over two features", after-before)
	}
}

func TestGeometryIsWorthWhatIsActuallyDrawn(t *testing.T) {
	table := points(t)
	shape := func(vertices int) build {
		var coordinates []any
		for index := range vertices {
			coordinates = append(coordinates, []any{float64(index), 0.0})
		}
		return build{collections: []map[string]any{{
			"id": 2, "title": "Zones", "kind": "area",
			"features": []any{map[string]any{
				"id": 10, "title": "A zone",
				"geometry": []any{map[string]any{"coordinates": []any{coordinates}}},
			}},
		}}}
	}
	rectangle := shape(4).score(t, table).Worlds[0].Features
	detailed := shape(1024).score(t, table).Worlds[0].Features
	if detailed <= rectangle {
		t.Errorf("a 1024-vertex boundary (%d) is not worth more than a rectangle (%d)",
			detailed, rectangle)
	}
	// Log scaling: a thousandfold more vertices is worth a handful of points,
	// not a thousandfold more score.
	if detailed-rectangle > 10 {
		t.Errorf("geometry outweighs everything else: %d points for 1020 more vertices",
			detailed-rectangle)
	}
	// And it is monotone.
	previous := 0
	for _, vertices := range []int{2, 4, 8, 64, 512, 4096} {
		got := shape(vertices).score(t, table).Worlds[0].Features
		if got < previous {
			t.Errorf("%d vertices scored %d, less than the shape before it (%d)",
				vertices, got, previous)
		}
		previous = got
	}
}

func TestCorroborationIsReadOffTheLedger(t *testing.T) {
	table := points(t)
	plain := build{
		collections: []map[string]any{pointCollection(nil)},
		locations:   []bundle.Location{{ID: 1, Title: "A chest"}},
	}
	corroborated := plain
	corroborated.merged = []enrich.Account{
		{Source: "MapGenie", Origin: true, DonorFeatures: enrich.Counts{Point: 1}},
		{Source: "IGN Wiki", DonorFeatures: enrich.Counts{Point: 1},
			Matched: []enrich.MatchedPair{{Donor: 99, Winner: 1, DistancePx: 20}}},
	}
	before := plain.score(t, table).Worlds[0].Features
	after := corroborated.score(t, table).Worlds[0].Features
	if after-before != table.Feature.Corroboration {
		t.Errorf("a second reading agreeing was worth %d", after-before)
	}
}

func TestWorldsEarnForWhatTheyDeclareAndHowDeepTheyGo(t *testing.T) {
	table := points(t)
	bare := build{collections: []map[string]any{pointCollection(nil)}}
	declared := bare
	declared.attrs = map[string]string{
		semconv.KeyGeometrySurface: semconv.SurfaceSphere,
		semconv.KeyGeometryBody:    "mars",
	}
	pictured := bare
	pictured.lenses = []map[string]any{{"name": "Default", "minZoom": 0, "maxZoom": 5}}

	if got := declared.score(t, table).Worlds[0].World - bare.score(t, table).Worlds[0].World; got != 2*table.World.Convention {
		t.Errorf("two declarations were worth %d", got)
	}
	if got := pictured.score(t, table).Worlds[0].World; got != table.World.Lens+6*table.World.LensZoom {
		t.Errorf("a six-level lens was worth %d", got)
	}
}

func TestCollectionsEarnForTheConventionsTheyDeclare(t *testing.T) {
	table := points(t)
	bare := build{collections: []map[string]any{pointCollection(nil)}}
	speaking := build{collections: []map[string]any{pointCollection(map[string]string{
		semconv.KeyGeometryKind: semconv.GeometryPoint,
		semconv.KeyRenderAs:     semconv.RenderAsPin,
		"private.key":           "ignored",
	})}}
	got := speaking.score(t, table).Worlds[0].Collections - bare.score(t, table).Worlds[0].Collections
	if got != 2*table.Collection.Convention {
		t.Errorf("two declared conventions were worth %d; a private key must be worth nothing", got)
	}
}

func TestTheScoreIsUnboundedAndAdditive(t *testing.T) {
	table := points(t)
	measure := func(features int) int {
		locations := make([]bundle.Location, 0, features)
		text := map[string]any{}
		for index := range features {
			id := int64(index + 1)
			locations = append(locations, bundle.Location{ID: id, Title: fmt.Sprintf("Chest %d", index)})
			text[fmt.Sprint(id)] = map[string]any{"d": "A chest."}
		}
		return build{
			collections: []map[string]any{pointCollection(nil)},
			locations:   locations,
			text:        text,
		}.score(t, table).Total
	}
	one, ten, hundred := measure(1), measure(10), measure(100)
	if ten != 10*one || hundred != 100*one {
		t.Errorf("features do not add up: %d, %d, %d", one, ten, hundred)
	}
	if hundred <= ten {
		t.Error("the score has a ceiling")
	}
}

func TestSharesAreDiagnosticsAndCannotExceedTheirWhole(t *testing.T) {
	// The reference tooling divided every described feature by the point
	// features alone, so a volume whose shapes were described read 235%.
	table := points(t)
	score := build{
		collections: []map[string]any{
			pointCollection(nil),
			{"id": 2, "title": "Zones", "kind": "area", "features": []any{
				map[string]any{"id": 10, "title": "A zone", "hasText": true},
				map[string]any{"id": 11, "title": "Another zone", "hasText": true},
			}},
		},
		locations: []bundle.Location{{ID: 1, Title: "A chest"}},
		text: map[string]any{
			"1":  map[string]any{"d": "A chest."},
			"10": map[string]any{"d": "A zone."},
			"11": map[string]any{"d": "Another zone."},
		},
	}.score(t, table)

	if score.Axes.Described != 3 || score.Axes.Features != 3 || score.Axes.Points != 1 {
		t.Fatalf("described %d of %d features (%d of them points)",
			score.Axes.Described, score.Axes.Features, score.Axes.Points)
	}
	if got := score.Axes.DescribedShare(); got != "100%" {
		t.Errorf("the described share reads %q, want 100%%", got)
	}
	if got := (Axes{}).DescribedShare(); got != "—" {
		t.Errorf("a share of nothing reads %q", got)
	}
}

func TestTheGateRewardsGoodDataAndPermitsCorrections(t *testing.T) {
	cases := []struct {
		what   string
		before Score
		after  Score
		refuse bool
	}{
		{
			what:   "more is more",
			before: Score{TableVersion: 1, Total: 100},
			after:  Score{TableVersion: 1, Total: 140},
		},
		{
			what:   "standing still passes",
			before: Score{TableVersion: 1, Total: 100},
			after:  Score{TableVersion: 1, Total: 100},
		},
		{
			what:   "losing quality fails",
			before: Score{TableVersion: 1, Total: 100},
			after:  Score{TableVersion: 1, Total: 90},
			refuse: true,
		},
		{
			what:   "a re-weighting is not a mass failure",
			before: Score{TableVersion: 1, Total: 100},
			after:  Score{TableVersion: 2, Total: 40},
		},
		{
			what:   "a ledgered correction is permitted",
			before: Score{TableVersion: 1, Total: 100},
			after: Score{TableVersion: 1, Total: 90, Ledger: []LedgerLine{{
				World: "world",
				Account: enrich.Account{Corrections: []enrich.Correction{{
					Reason: "twelve zones claimed a subwatershed they only touched", Points: 12,
				}}},
			}}},
		},
		{
			what:   "a correction does not excuse everything",
			before: Score{TableVersion: 1, Total: 100},
			after: Score{TableVersion: 1, Total: 50, Ledger: []LedgerLine{{
				Account: enrich.Account{Corrections: []enrich.Correction{{Reason: "a few", Points: 5}}},
			}}},
			refuse: true,
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			err := Gate(&c.before, &c.after)
			if c.refuse && err == nil {
				t.Fatal("accepted")
			}
			if !c.refuse && err != nil {
				t.Fatalf("refused: %v", err)
			}
			comparison := Compare(&c.before, &c.after)
			if comparison.Delta != c.after.Total-c.before.Total {
				t.Errorf("delta reads %d", comparison.Delta)
			}
		})
	}
}

func TestServingIsTheRegistrysOwnOrder(t *testing.T) {
	plain := &Score{Volume: "v", CreatedAt: "2026-08-01T00:00:00Z", Revision: 9, Stamp: "aaa", Path: "a"}
	enriched := &Score{Volume: "v", CreatedAt: "2026-08-01T00:00:00Z", Revision: 109, Stamp: "bbb", Path: "b"}
	newer := &Score{Volume: "v", CreatedAt: "2026-08-02T00:00:00Z", Revision: 9, Stamp: "ccc", Path: "c"}

	if got := Serving([]*Score{plain, enriched}); got != enriched {
		t.Error("the plain build outranked the enriched build of the same capture")
	}
	if got := Serving([]*Score{enriched, newer}); got != newer {
		t.Error("an older capture outranked a newer one")
	}
}

func TestAnEnrichedBuildSaysSo(t *testing.T) {
	table := points(t)
	enrichedRevision, err := enrich.BuildRevision(9)
	if err != nil {
		t.Fatal(err)
	}
	plain := build{collections: []map[string]any{pointCollection(nil)}, revision: 9}.score(t, table)
	if plain.Enriched {
		t.Error("a plain build reads as enriched")
	}
	enriched := build{collections: []map[string]any{pointCollection(nil)}, revision: enrichedRevision}.score(t, table)
	if !enriched.Enriched || enriched.EnrichPolicy != enrich.PolicyRevision {
		t.Errorf("an enriched build reads enriched=%t policy %d", enriched.Enriched, enriched.EnrichPolicy)
	}
}

func TestThePointTableIsVersionedData(t *testing.T) {
	table := points(t)
	if table.Version != 1 || table.Schema != TableSchema {
		t.Errorf("the embedded table is schema %d version %d", table.Schema, table.Version)
	}
	if table.Feature.SubstanceChars <= 0 {
		t.Error("the table declares no substance threshold")
	}
	for _, bad := range []string{
		`{"schema": 99, "version": 1}`,
		`{"schema": 1, "version": 0}`,
		`{"schema": 1, "version": 1, "feature": {"substanceChars": 0}}`,
	} {
		if _, err := ReadTable([]byte(bad)); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	// A score carries the version that produced it, which is what makes two
	// scores comparable or not.
	sample := build{collections: []map[string]any{pointCollection(nil)}}
	if got := sample.score(t, table); got.TableVersion != table.Version {
		t.Errorf("the score says table v%d", got.TableVersion)
	}
}
