package curation

import (
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

func TestTheEmbeddedCorpusIsReadable(t *testing.T) {
	tables, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(tables.Queue()) == 0 {
		t.Fatal("the corpus declares no queue")
	}
	// The queue is a decision, so it is spelled out rather than derived.
	want := []string{"merge", "national", "stdicons", "lenses"}
	for at, name := range want {
		if tables.Queue()[at] != name {
			t.Errorf("position %d runs %q, want %q", at, tables.Queue()[at], name)
		}
	}
	if tables.MatchRadiusPx() != 160 || tables.SeparateRadiusPx() != 320 || tables.NearbyFloorPx() != 48 {
		t.Errorf("the resolution distances read %g/%g/%g",
			tables.MatchRadiusPx(), tables.SeparateRadiusPx(), tables.NearbyFloorPx())
	}
	for _, key := range []string{
		semconv.KeyNoteText, semconv.KeyGeoLat, semconv.KeyGeoLon, semconv.KeyIconStd,
	} {
		if !tables.DonorFillsEmpty(key) {
			t.Errorf("%s is not a donor-fills-empty attribute", key)
		}
	}
	if tables.DonorFillsEmpty(semconv.KeyRenderAs) {
		t.Error("an unlisted attribute is not serving-wins")
	}
	if len(tables.EvidenceCollections("bend-or")) == 0 {
		t.Error("the city volume names no evidence collections")
	}
	if len(tables.EvidenceCollections("tunic")) != 0 {
		t.Error("a volume nobody surveyed names evidence collections")
	}
}

func TestCurationIsHeldToItsSchema(t *testing.T) {
	cases := []struct {
		what   string
		data   string
		refuse string
	}{
		{
			what:   "another schema",
			data:   `{"schema": 99}`,
			refuse: "schema",
		},
		{
			what:   "no queue at all",
			data:   `{"schema": 1, "merge": {"matchRadiusPx": 1, "separateRadiusPx": 2}}`,
			refuse: "no enricher queue",
		},
		{
			what: "radii that cannot hold anything back",
			data: `{"schema": 1, "queue": {"order": ["merge"]},
				"merge": {"matchRadiusPx": 320, "separateRadiusPx": 160}}`,
			refuse: "the second has to be the larger",
		},
		{
			what: "a policy for an attribute nobody registered",
			data: `{"schema": 1, "queue": {"order": ["merge"]},
				"merge": {"matchRadiusPx": 160, "separateRadiusPx": 320,
					"attributes": {"donorFillsEmpty": ["atlas.invented.key"]}}}`,
			refuse: "the conventions registry does not know it",
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if _, err := LoadFrom([]byte(c.data)); err == nil ||
				!strings.Contains(err.Error(), c.refuse) {
				t.Fatalf("error %v, expected something about %q", err, c.refuse)
			}
		})
	}
}

func TestProseRidesBesideTheData(t *testing.T) {
	// Every section carries its own explanation as a sibling key, the way the
	// generate lane's curation does. A value that is not the shape a section
	// expects is commentary, not data.
	tables, err := LoadFrom([]byte(`{
		"schema": 1,
		"queue": {"what": "why these run in this order", "order": ["merge"]},
		"merge": {"what": "the distances", "matchRadiusPx": 160, "separateRadiusPx": 320},
		"national": {"what": "the evidence", "evidenceCollections": {
			"what": "keyed by volume", "city": ["Subwatersheds"]}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tables.EvidenceCollections("city")) != 1 {
		t.Error("a volume's evidence was lost among the prose")
	}
	if len(tables.EvidenceCollections("what")) != 0 {
		t.Error("commentary was read as a volume")
	}
}
