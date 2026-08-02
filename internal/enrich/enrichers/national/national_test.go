package national

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/curation"
)

func tables(t *testing.T) enrich.Curation {
	t.Helper()
	loaded, err := curation.LoadFrom([]byte(`{
		"schema": 1,
		"queue": {"order": ["national"]},
		"merge": {"matchRadiusPx": 160, "separateRadiusPx": 320, "nearbyFloorPx": 48,
			"attributes": {"donorFillsEmpty": []}},
		"national": {"evidenceCollections": {"city": ["Subwatersheds"]}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// box is a square of ground, as a polygon geometry.
func box(west, south, east, north float64) enrich.Geometry {
	coordinates := fmt.Sprintf(`[[[[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]]]]`,
		west, south, east, south, east, north, west, north, west, south)
	return enrich.Geometry{Type: "MultiPolygon", Coordinates: json.RawMessage(coordinates)}
}

func line(positions string) enrich.Geometry {
	return enrich.Geometry{Type: "MultiLineString", Coordinates: json.RawMessage(positions)}
}

// twoUnits is a survey of two subwatersheds side by side: west of zero and east
// of it.
func twoUnits() Evidence {
	return Evidence{
		Evidence: EvidenceDoc, Version: EvidenceVersion, Kind: EvidenceKind, Space: SpaceWorld,
		Units: []Unit{
			{Code: "170703010801", Name: "West Fork", Rings: ringsOf(box(-10, -10, 0, 10))},
			{Code: "170703010802", Name: "East Fork", Rings: ringsOf(box(0, -10, 10, 10))},
		},
	}
}

func ringsOf(g enrich.Geometry) [][][][2]float64 { return g.Rings() }

func city(features ...enrich.Feature) *enrich.Volume {
	return &enrich.Volume{
		Slug: "city",
		Worlds: []enrich.World{{
			Slug: "today",
			Collections: []enrich.Collection{
				{ID: 1, Title: "Zoning", Kind: enrich.KindArea, Features: features},
				{ID: 2, Title: "Subwatersheds", Kind: enrich.KindArea, Features: []enrich.Feature{
					{ID: 900, Title: "West Fork", Geometry: []enrich.Geometry{box(-10, -10, 0, 10)}},
				}},
			},
		}},
	}
}

func run(t *testing.T, volume *enrich.Volume, evidence Evidence) enrich.Contribution {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := New().Enrich(volume, enrich.Context{
		Evidence: staticEvidence{EvidenceName: data},
		Curation: tables(t),
	})
	if err != nil {
		t.Fatalf("national: %v", err)
	}
	return contribution
}

type staticEvidence map[string][]byte

func (s staticEvidence) Open(name string) ([]byte, bool, error) {
	data, held := s[name]
	return data, held, nil
}

func TestTheJoinClaimsOnlyWhatItIsSureOf(t *testing.T) {
	cases := []struct {
		what    string
		feature enrich.Feature
		code    string
	}{
		{
			what:    "ground wholly inside one unit",
			feature: enrich.Feature{ID: 1, Title: "Downtown", Geometry: []enrich.Geometry{box(-8, -8, -2, -2)}},
			code:    "170703010801",
		},
		{
			what:    "ground wholly inside the other",
			feature: enrich.Feature{ID: 1, Title: "Eastside", Geometry: []enrich.Geometry{box(2, 2, 8, 8)}},
			code:    "170703010802",
		},
		{
			what:    "ground that straddles the divide says nothing",
			feature: enrich.Feature{ID: 1, Title: "Riverside", Geometry: []enrich.Geometry{box(-4, -4, 4, 4)}},
		},
		{
			what:    "ground that leaves the survey says nothing",
			feature: enrich.Feature{ID: 1, Title: "Outskirts", Geometry: []enrich.Geometry{box(-20, -4, -2, 4)}},
		},
		{
			what:    "a trail wholly inside one unit",
			feature: enrich.Feature{ID: 1, Title: "River Trail", Geometry: []enrich.Geometry{line(`[[[-8,-8],[-7,-6],[-6,-4]]]`)}},
			code:    "170703010801",
		},
		{
			what:    "a trail that crosses the divide says nothing",
			feature: enrich.Feature{ID: 1, Title: "Crossing Trail", Geometry: []enrich.Geometry{line(`[[[-8,0],[0.5,0],[8,0]]]`)}},
		},
		{
			what: "a feature made of two pieces, both in one unit",
			feature: enrich.Feature{ID: 1, Title: "Two parks",
				Geometry: []enrich.Geometry{box(-8, -8, -6, -6), box(-5, -5, -3, -3)}},
			code: "170703010801",
		},
		{
			what: "a feature made of two pieces in different units",
			feature: enrich.Feature{ID: 1, Title: "Two parks",
				Geometry: []enrich.Geometry{box(-8, -8, -6, -6), box(3, 3, 5, 5)}},
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			contribution := run(t, city(c.feature), twoUnits())
			claimed := ""
			sentence := ""
			for _, op := range contribution.Ops {
				if op.Feature != 1 {
					continue
				}
				switch op.Kind {
				case enrich.OpSetAttr:
					claimed = op.Value
				case enrich.OpSetProse:
					sentence = op.Value
				}
			}
			if claimed != c.code {
				t.Errorf("claimed %q, want %q", claimed, c.code)
			}
			if c.code == "" {
				if sentence != "" {
					t.Errorf("silence was written down as %q", sentence)
				}
				return
			}
			if !strings.Contains(sentence, c.code) || !strings.HasPrefix(sentence, "Lies in the ") {
				t.Errorf("the sentence reads %q", sentence)
			}
		})
	}
}

func TestTheSurveyMakesNoClaimsAboutItself(t *testing.T) {
	// The subwatershed collection is the evidence; a subwatershed does not need
	// a sentence saying which subwatershed it lies in.
	contribution := run(t, city(enrich.Feature{
		ID: 1, Title: "Downtown", Geometry: []enrich.Geometry{box(-8, -8, -2, -2)},
	}), twoUnits())
	for _, op := range contribution.Ops {
		if op.Feature == 900 {
			t.Errorf("the survey made a claim about itself: %+v", op)
		}
	}
}

func TestPointFeaturesAreNotJoined(t *testing.T) {
	volume := city()
	volume.Worlds[0].Collections = append(volume.Worlds[0].Collections, enrich.Collection{
		ID: 3, Title: "Historic Resources", Kind: enrich.KindPoint,
		Features: []enrich.Feature{{ID: 5, Title: "A house", At: &enrich.Position{Lat: -5, Lng: -5}}},
	})
	contribution := run(t, volume, twoUnits())
	for _, op := range contribution.Ops {
		if op.Feature == 5 {
			t.Errorf("a point feature was joined: %+v", op)
		}
	}
}

func TestAClaimAlreadyMadeIsLeftAlone(t *testing.T) {
	feature := enrich.Feature{
		ID: 1, Title: "Downtown", Geometry: []enrich.Geometry{box(-8, -8, -2, -2)},
		Attrs: map[string]string{semconv.KeyHydroHUC12: "170703010801"},
	}
	if contribution := run(t, city(feature), twoUnits()); !contribution.Empty() {
		t.Errorf("a claim that was already made was made again: %+v", contribution.Ops)
	}
}

func TestProseThatIsAlreadyThereIsNotRewritten(t *testing.T) {
	feature := enrich.Feature{
		ID: 1, Title: "Downtown", Geometry: []enrich.Geometry{box(-8, -8, -2, -2)},
		Description: "The city's oldest zoning district.",
	}
	volume := city(feature)
	contribution := run(t, volume, twoUnits())
	for _, op := range contribution.Ops {
		if op.Kind == enrich.OpSetProse {
			t.Errorf("prose a source wrote was rewritten as %q", op.Value)
		}
	}
	// The membership claim still lands: what the join knows is the key, and the
	// sentence is a courtesy it only offers where there is room for it.
	if err := enrich.Apply(volume, contribution); err != nil {
		t.Fatal(err)
	}
	if _, held := volume.Worlds[0].Feature(1); held.Attrs[semconv.KeyHydroHUC12] == "" {
		t.Error("the claim itself was dropped along with the sentence")
	}
}

func TestAnEmptyEvidenceBaseIsSilence(t *testing.T) {
	volume := city(enrich.Feature{ID: 1, Title: "Downtown", Geometry: []enrich.Geometry{box(-8, -8, -2, -2)}})
	contribution, err := New().Enrich(volume, enrich.Context{Curation: tables(t)})
	if err != nil {
		t.Fatalf("a volume nobody surveyed is not an error: %v", err)
	}
	if !contribution.Empty() {
		t.Error("something was claimed with no evidence at all")
	}

	contribution, err = New().Enrich(volume, enrich.Context{
		Evidence: staticEvidence{}, Curation: tables(t),
	})
	if err != nil || !contribution.Empty() {
		t.Errorf("an evidence base without this survey: %v, %d operations", err, len(contribution.Ops))
	}
}

func TestEvidenceIsHeldToItsSchema(t *testing.T) {
	good := twoUnits()
	cases := []struct {
		what   string
		change func(*Evidence)
		refuse string
	}{
		{"a document of another kind", func(e *Evidence) { e.Kind = "hydro.huc8" }, "is of"},
		{"another schema version", func(e *Evidence) { e.Version = 99 }, "version"},
		{"another document entirely", func(e *Evidence) { e.Evidence = "something-else" }, "says"},
		{"coordinates in another space", func(e *Evidence) { e.Space = "geo" }, "published in"},
		{"a code that is not a code", func(e *Evidence) { e.Units[0].Code = "17" }, "twelve digits"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			evidence := good
			evidence.Units = append([]Unit(nil), good.Units...)
			c.change(&evidence)
			data, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ReadEvidence(data); err == nil ||
				!strings.Contains(err.Error(), c.refuse) {
				t.Fatalf("error %v, expected something about %q", err, c.refuse)
			}
		})
	}
}

func TestPositionsAreAnAnchorAndASampleOfTheBoundary(t *testing.T) {
	// A ring with far more vertices than the sample budget still costs the
	// budget, and the anchor is one of them.
	var ring strings.Builder
	ring.WriteString(`[[[`)
	for index := range 200 {
		angle := 2 * math.Pi * float64(index) / 200
		fmt.Fprintf(&ring, "[%f,%f],", 5*math.Cos(angle), 5*math.Sin(angle))
	}
	ring.WriteString(`[5,0]]]]`)
	positions := Positions(enrich.Geometry{Type: "MultiPolygon", Coordinates: json.RawMessage(ring.String())})
	if len(positions) > ClaimSamples+1 {
		t.Errorf("a 200-vertex ring volunteered %d positions; the budget is %d plus an anchor",
			len(positions), ClaimSamples)
	}
	if len(positions) < 2 {
		t.Errorf("a ring volunteered %d positions", len(positions))
	}
	// The anchor of a square is its middle.
	square := Positions(box(-10, -10, 10, 10))
	if len(square) == 0 {
		t.Fatal("a square volunteered nothing")
	}
	if x, y := square[0][0], square[0][1]; x > 1e-9 || x < -1e-9 || y > 1e-9 || y < -1e-9 {
		t.Errorf("the anchor of a centred square is %v", square[0])
	}
}

func TestTheLargestRingIsTheAnchor(t *testing.T) {
	// Two pieces, one much larger: the anchor comes from the larger.
	small := `[[[-9,-9],[-8,-9],[-8,-8],[-9,-8],[-9,-9]]]`
	large := `[[[1,1],[9,1],[9,9],[1,9],[1,1]]]`
	geometry := enrich.Geometry{Type: "MultiPolygon",
		Coordinates: json.RawMessage("[" + small + "," + large + "]")}
	positions := Positions(geometry)
	if len(positions) == 0 {
		t.Fatal("nothing was volunteered")
	}
	if positions[0][0] < 1 || positions[0][1] < 1 {
		t.Errorf("the anchor is %v, which is not in the larger piece", positions[0])
	}
}
