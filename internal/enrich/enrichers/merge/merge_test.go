package merge

import (
	"fmt"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/curation"
)

func tables(t *testing.T) enrich.Curation {
	t.Helper()
	loaded, err := curation.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

var grid = enrich.Grid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192}

// place puts a feature at a world position given in world pixels, which is what
// every radius in this lane is measured in.
func place(id int64, title string, x, y float64) enrich.Feature {
	return enrich.Feature{
		ID:    id,
		Title: title,
		At:    &enrich.Position{Lat: grid.UnprojectLat(y), Lng: grid.UnprojectLng(x)},
	}
}

// anchorsFor is the calibration both readings share: enough unambiguous names,
// in the same places, that the fit closes as the identity.
func anchorsFor(base int64) []enrich.Feature {
	var out []enrich.Feature
	for index := range 20 {
		out = append(out, place(base+int64(index),
			fmt.Sprintf("anchor %d", index),
			float64(500+index*180), float64(400+(index*137)%2200)))
	}
	return out
}

// reading builds one source's reading of one volume: the anchors, plus whatever
// else the case wants to say.
func reading(source enrich.Source, captured string, idBase int64, collections ...enrich.Collection) *enrich.Volume {
	world := enrich.World{
		Slug:       "night-city",
		Title:      "Night City",
		Grid:       grid,
		CapturedAt: captured,
		Lenses:     []enrich.Lens{{Name: "Default", TileSet: source.Name + "/night-city"}},
		Collections: []enrich.Collection{{
			ID: idBase, Title: "Landmarks", Kind: enrich.KindPoint, Icon: "landmark",
			Features: anchorsFor(idBase * 1000),
		}},
	}
	world.Collections = append(world.Collections, collections...)
	return &enrich.Volume{
		Slug: "cyberpunk-2077", Title: "Cyberpunk 2077", Source: source,
		Worlds: []enrich.World{world},
	}
}

var (
	piggyback = enrich.Source{Name: "piggyback", Label: "Piggyback"}
	ign       = enrich.Source{Name: "ign-wiki", Label: "IGN Wiki"}
)

// fold runs the merge and hands back the donor's account and the volume it left.
func fold(t *testing.T, serving, donor *enrich.Volume) (*enrich.Volume, enrich.Account, enrich.Contribution) {
	t.Helper()
	enrich.OpenOrigin(serving)
	contribution, err := New().Enrich(serving, enrich.Context{
		Donors:   []*enrich.Volume{donor},
		Curation: tables(t),
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
	for _, account := range serving.Worlds[0].Ledger {
		if !account.Origin {
			return serving, account, contribution
		}
	}
	return serving, enrich.Account{}, contribution
}

func TestResolutionDecidesByNameAndDistance(t *testing.T) {
	cases := []struct {
		what     string
		serving  enrich.Collection
		donor    enrich.Collection
		matched  int
		added    int
		held     int
		rejected int
		reason   string
	}{
		{
			what: "the same name where the alignment predicts is the same place",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9101, "Jinguji", 3100, 3000)}},
			matched: 1,
		},
		{
			what: "the same name far beyond the radius is another place bearing it",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9101, "Jinguji", 4000, 3000)}},
			added: 1,
		},
		{
			what: "the stretch between is left undecided",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9101, "Jinguji", 3250, 3000)}},
			held:   1,
			reason: "too far to merge, too near to double",
		},
		{
			what: "one name written inside the other, in a shared collection",
			serving: enrich.Collection{ID: 90, Title: "Apartments", Kind: enrich.KindPoint, Icon: "apartment",
				Features: []enrich.Feature{place(9001, "Northside, Watson Apartment", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Apartments", Kind: enrich.KindPoint, Icon: "apartment",
				Features: []enrich.Feature{place(9101, "Northside Apartment", 3080, 3000)}},
			matched: 1,
		},
		{
			what: "a single word inside a longer name says nothing",
			serving: enrich.Collection{ID: 90, Title: "Apartments", Kind: enrich.KindPoint, Icon: "apartment",
				Features: []enrich.Feature{place(9001, "Northside Watson Apartment", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Apartments", Kind: enrich.KindPoint, Icon: "apartment",
				Features: []enrich.Feature{place(9101, "Apartment", 3030, 3000)}},
			held:   1,
			reason: "names disagree",
		},
		{
			what: "a nameless neighbour in a shared collection is held, never guessed",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9101, "Kabuki Market", 3020, 3000)}},
			held:   1,
			reason: "in the same category; names disagree",
		},
		{
			what: "something the serving reading has no collection for is simply added",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Graffiti", Kind: enrich.KindPoint, Icon: "graffiti",
				Features: []enrich.Feature{place(9101, "A tarot card", 3020, 3000)}},
			added: 1,
		},
		{
			what: "a feature the alignment puts off the world is refused",
			serving: enrich.Collection{ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)}},
			donor: enrich.Collection{ID: 91, Title: "Graffiti", Kind: enrich.KindPoint, Icon: "graffiti",
				Features: []enrich.Feature{place(9101, "Somewhere else", -4000, 3000)}},
			rejected: 1,
			reason:   outsideReason,
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			serving := reading(piggyback, "2026-08-01T05:00:00Z", 1, c.serving)
			donor := reading(ign, "2026-08-01T04:00:00Z", 2, c.donor)
			_, account, _ := fold(t, serving, donor)

			// The anchors match each other, so the case's own features are what
			// the counts beyond them are about.
			const anchors = 20
			if got := account.MatchedN() - anchors; got != c.matched {
				t.Errorf("matched %d, want %d", got, c.matched)
			}
			if account.Added != c.added {
				t.Errorf("added %d, want %d", account.Added, c.added)
			}
			if account.HeldN() != c.held {
				t.Errorf("held %d, want %d", account.HeldN(), c.held)
			}
			if account.RejectedN() != c.rejected {
				t.Errorf("rejected %d, want %d", account.RejectedN(), c.rejected)
			}
			if c.reason != "" {
				reasons := append(account.Held, account.Rejected...)
				found := false
				for _, item := range reasons {
					if strings.Contains(item.Reason, c.reason) {
						found = true
					}
				}
				if !found {
					t.Errorf("no held or rejected feature says %q; the ledger says %+v", c.reason, reasons)
				}
			}
		})
	}
}

func TestAPlaceIsOnePlace(t *testing.T) {
	// Two donor features bearing one name, both near one serving feature: the
	// first is the place, and the second has to say why it is not.
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1, enrich.Collection{
		ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
		Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)},
	})
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
		Features: []enrich.Feature{
			place(9101, "Jinguji", 3050, 3000),
			place(9102, "Jinguji", 3060, 3000),
		},
	})
	_, account, _ := fold(t, serving, donor)
	if got := account.MatchedN() - 20; got != 1 {
		t.Errorf("matched %d of the two, want 1", got)
	}
	if account.HeldN() != 1 || !strings.Contains(account.Held[0].Reason, "already matched") {
		t.Errorf("the second was not held for the right reason: %+v", account.Held)
	}
}

func TestAttributePolicyFillsWhatIsEmpty(t *testing.T) {
	served := place(9001, "Jinguji", 3000, 3000)
	served.Attrs = map[string]string{semconv.KeyGeoLat: "10"}
	offered := place(9101, "Jinguji", 3020, 3000)
	offered.Description = "A clothing shop in Japantown."
	offered.Attrs = map[string]string{semconv.KeyGeoLat: "20", semconv.KeyGeoLon: "30"}

	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1, enrich.Collection{
		ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
		Features: []enrich.Feature{served},
	})
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 91, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
		Features: []enrich.Feature{offered},
	})
	merged, account, _ := fold(t, serving, donor)

	_, feature := merged.Worlds[0].Feature(9001)
	if feature.Description != offered.Description {
		t.Errorf("the serving feature kept its silence: %q", feature.Description)
	}
	if feature.Attrs[semconv.KeyGeoLat] != "10" {
		t.Errorf("serving-wins was not honoured: latitude is %q", feature.Attrs[semconv.KeyGeoLat])
	}
	if feature.Attrs[semconv.KeyGeoLon] != "30" {
		t.Errorf("donor-fills-empty was not honoured: longitude is %q", feature.Attrs[semconv.KeyGeoLon])
	}

	var pair enrich.MatchedPair
	for _, held := range account.Matched {
		if held.Donor == 9101 {
			pair = held
		}
	}
	if !pair.Enriched {
		t.Error("the pair does not record that prose was taken")
	}
	took := strings.Join(pair.Took, ",")
	if !strings.Contains(took, semconv.KeyNoteText) || !strings.Contains(took, semconv.KeyGeoLon) {
		t.Errorf("takes recorded as %q", took)
	}
	if strings.Contains(took, semconv.KeyGeoLat) {
		t.Errorf("a take was recorded for an attribute the serving side kept: %q", took)
	}
}

func TestCollectionsMeetUnderTheirDeclaredIdentity(t *testing.T) {
	// Two readings spell one concept two ways and say so through the merge
	// identity their payloads carry.
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1, enrich.Collection{
		ID: 90, Title: "Ripperdoc", Kind: enrich.KindPoint, Icon: "ripperdoc",
		Attrs:    map[string]string{semconv.KeyCollectionKey: "ripperdoc"},
		Features: []enrich.Feature{place(9001, "Vik", 3000, 3000)},
	})
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 91, Title: "Ripper Doc", Kind: enrich.KindPoint, Icon: "ripper-doc",
		Attrs:    map[string]string{semconv.KeyCollectionKey: "ripperdoc"},
		Features: []enrich.Feature{place(9101, "Cassius Ryder", 5000, 5000)},
	})
	merged, account, _ := fold(t, serving, donor)

	if account.Added != 1 || account.AdoptedN() != 1 {
		t.Fatalf("added %d, adopted %d", account.Added, account.AdoptedN())
	}
	if account.Adopted[0].Into != "ripperdoc" {
		t.Errorf("adopted into %q", account.Adopted[0].Into)
	}
	if collection, _ := merged.Worlds[0].Feature(9101); collection.ID != 90 {
		t.Errorf("the adopted feature joined collection %d, not the serving one", collection.ID)
	}
	for _, collection := range merged.Worlds[0].Collections {
		if collection.ID == 91 {
			t.Error("a collection was contributed for a concept the serving reading already had")
		}
	}
}

func TestCollectionsWithNoCounterpartKeepTheirOwn(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1)
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 91, Title: "Tarot Graffiti", Kind: enrich.KindPoint, Icon: "tarot",
		Features: []enrich.Feature{place(9101, "The Fool", 3000, 3000)},
	})
	donor.Icons = []enrich.Icon{{Key: "tarot", File: "tarot.svg", Data: []byte("<svg/>")}}
	merged, account, _ := fold(t, serving, donor)

	if account.Added != 1 || account.AdoptedN() != 0 {
		t.Fatalf("added %d, adopted %d", account.Added, account.AdoptedN())
	}
	var kept *enrich.Collection
	for index := range merged.Worlds[0].Collections {
		if merged.Worlds[0].Collections[index].ID == 91 {
			kept = &merged.Worlds[0].Collections[index]
		}
	}
	if kept == nil {
		t.Fatal("the donor's own collection was not contributed")
	}
	if kept.Group != "IGN Wiki" {
		t.Errorf("the contributed collection files under %q; the legend should say where it came from", kept.Group)
	}
	if kept.Icon != "ign-wiki--tarot" {
		t.Errorf("artwork came across as %q; it must not be able to displace the volume's own", kept.Icon)
	}
	carried := false
	for _, icon := range merged.Icons {
		if icon.Key == "ign-wiki--tarot" && icon.File == "ign-wiki--tarot.svg" {
			carried = true
		}
	}
	if !carried {
		t.Errorf("the artwork itself did not travel: %+v", merged.Icons)
	}
}

func TestAGroundWithNoCounterpartJoinsWhole(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1)
	donor := reading(ign, "2026-08-01T04:00:00Z", 2)
	// A second ground the serving reading does not picture, with places that
	// resemble nothing it holds.
	donor.Worlds = append(donor.Worlds, enrich.World{
		Slug: "dogtown", Title: "Dogtown", Grid: grid, CapturedAt: "2026-08-01T04:00:00Z",
		Lenses: []enrich.Lens{{Name: "IGN Wiki", TileSet: "ign/dogtown"}},
		Collections: []enrich.Collection{{
			ID: 95, Title: "Stashes", Kind: enrich.KindPoint, Icon: "stash",
			Features: []enrich.Feature{place(9501, "A stash", 1000, 1000)},
		}},
	})
	merged, _, _ := fold(t, serving, donor)

	dogtown := merged.World("dogtown")
	if dogtown == nil {
		t.Fatal("the ground did not join")
	}
	if len(dogtown.Ledger) != 1 || !dogtown.Ledger[0].Origin || dogtown.Ledger[0].Slug != "ign-wiki" {
		t.Errorf("a ground that joined whole says nothing about where it came from: %+v", dogtown.Ledger)
	}
	if err := enrich.GateWorld(dogtown); err != nil {
		t.Errorf("the contributed ground does not add up: %v", err)
	}
}

func TestShapesAreHeldOnTheRecord(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1)
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 92, Title: "Districts", Kind: enrich.KindArea,
		Features: []enrich.Feature{{ID: 9201, Title: "Watson"}, {ID: 9202, Title: "Westbrook"}},
	})
	_, account, _ := fold(t, serving, donor)

	if account.DonorFeatures.Area != 2 {
		t.Errorf("the offering counts %d areas", account.DonorFeatures.Area)
	}
	shapes := 0
	for _, held := range account.Held {
		if held.Reason == enrich.HeldShapeReason {
			shapes++
		}
	}
	if shapes != 2 {
		t.Errorf("%d shape features are on the record, want 2", shapes)
	}
}

func TestARefusedAlignmentIsARefusedMerge(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1)
	// A reading that shares no name with the serving one cannot be aligned, so
	// nothing may be concluded from it.
	donor := &enrich.Volume{
		Slug: "cyberpunk-2077", Source: ign,
		Worlds: []enrich.World{{
			Slug: "night-city", Grid: grid, CapturedAt: "2026-08-01T04:00:00Z",
			Collections: []enrich.Collection{{
				ID: 93, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
				Features: []enrich.Feature{place(9301, "Somewhere", 3000, 3000)},
			}},
		}},
	}
	enrich.OpenOrigin(serving)
	contribution, err := New().Enrich(serving, enrich.Context{
		Donors: []*enrich.Volume{donor}, Curation: tables(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contribution.Empty() {
		t.Errorf("a merge that could not be aligned contributed %d operations", len(contribution.Ops))
	}
}

func TestTheNewestCaptureServes(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T04:00:00Z", 1)
	donor := reading(ign, "2026-08-01T05:00:00Z", 2)
	enrich.OpenOrigin(serving)
	_, err := New().Enrich(serving, enrich.Context{
		Donors: []*enrich.Volume{donor}, Curation: tables(t),
	})
	if err == nil || !strings.Contains(err.Error(), "the newest capture serves") {
		t.Fatalf("error %v", err)
	}
}

func TestMergeLeavesTheVolumeAloneUntilItIsApplied(t *testing.T) {
	serving := reading(piggyback, "2026-08-01T05:00:00Z", 1, enrich.Collection{
		ID: 90, Title: "Shops", Kind: enrich.KindPoint, Icon: "shop",
		Features: []enrich.Feature{place(9001, "Jinguji", 3000, 3000)},
	})
	donor := reading(ign, "2026-08-01T04:00:00Z", 2, enrich.Collection{
		ID: 91, Title: "Graffiti", Kind: enrich.KindPoint, Icon: "graffiti",
		Features: []enrich.Feature{place(9101, "A tarot card", 6000, 6000)},
	})
	enrich.OpenOrigin(serving)
	before := len(serving.Worlds[0].Collections)
	contribution, err := New().Enrich(serving, enrich.Context{
		Donors: []*enrich.Volume{donor}, Curation: tables(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(serving.Worlds[0].Collections) != before || len(serving.Worlds[0].Ledger) != 1 {
		t.Error("the merge changed the volume before its contribution was applied")
	}
	if contribution.Empty() {
		t.Fatal("nothing was contributed")
	}
	// And applying the contribution twice is refused rather than doubling
	// anything: identifiers are claimed once.
	if err := enrich.Apply(serving, contribution); err != nil {
		t.Fatal(err)
	}
	if err := enrich.Apply(serving, contribution); err == nil {
		t.Error("a contribution applied twice was accepted")
	}
}

func TestSlugifyFoldsALabelIntoAName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"IGN Wiki", "ign-wiki"},
		{"MapGenie", "mapgenie"},
		{"ArcGIS Open Data", "arcgis-open-data"},
		{"  Trailing  ", "trailing"},
		{"", ""},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("%q folded to %q, want %q", c.in, got, c.want)
		}
	}
}
