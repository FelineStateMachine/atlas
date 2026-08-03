package enrich

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
)

// unregistered is a key in the conventions namespace that the registry does not
// know. It is assembled rather than written out because a literal atlas.* key
// outside the registry is itself a lint failure -- which is the rule this
// constant exists to test the other side of.
var unregistered = semconv.Namespace + "invented.key"

// A volume to try things on: one world, one point collection, one feature that
// says nothing about itself.
func sample() *Volume {
	return &Volume{
		Slug:   "tunic",
		Title:  "TUNIC",
		Source: Source{Name: "mapgenie", Label: "MapGenie"},
		Worlds: []World{{
			Slug:       "overworld",
			Title:      "Overworld",
			CapturedAt: "2026-07-30T03:57:41.529Z",
			Grid:       Grid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192},
			Lenses:     []Lens{{Name: "Default", TileSet: "tunic/overworld"}},
			Collections: []Collection{{
				ID:      7,
				Title:   "Chests",
				Kind:    KindPoint,
				Icon:    "chest",
				Visible: true,
				Features: []Feature{{
					ID:    1,
					Title: "A chest",
					At:    &Position{Lat: 1, Lng: 2},
				}},
			}},
		}},
	}
}

func TestApplyFillsWhatIsEmptyAndRefusesTheRest(t *testing.T) {
	described := sample()
	described.Worlds[0].Collections[0].Features[0].Description = "already said"
	positioned := sample()
	positioned.Worlds[0].Collections[0].Features[0].Attrs = map[string]string{semconv.KeyGeoLat: "1.5"}
	arted := sample()
	arted.Worlds[0].Collections[0].Icon = "own-artwork"

	cases := []struct {
		what   string
		volume *Volume
		op     Op
		refuse string
	}{
		{
			what:   "prose lands on a feature that has none",
			volume: sample(),
			op:     Op{Kind: OpSetProse, World: "overworld", Feature: 1, Value: "a chest"},
		},
		{
			what:   "prose does not overwrite a source",
			volume: described,
			op:     Op{Kind: OpSetProse, World: "overworld", Feature: 1, Value: "something else"},
			refuse: "rewrites nothing",
		},
		{
			what:   "an attribute lands where the key is free",
			volume: sample(),
			op: Op{Kind: OpSetAttr, World: "overworld", Feature: 1,
				Entity: semconv.EntityFeature, Key: semconv.KeyGeoLat, Value: "1.5"},
		},
		{
			what:   "an attribute does not overwrite a different value",
			volume: positioned,
			op: Op{Kind: OpSetAttr, World: "overworld", Feature: 1,
				Entity: semconv.EntityFeature, Key: semconv.KeyGeoLat, Value: "9.9"},
			refuse: "already says",
		},
		{
			what:   "an attribute that repeats what is there is not a rewrite",
			volume: positioned,
			op: Op{Kind: OpSetAttr, World: "overworld", Feature: 1,
				Entity: semconv.EntityFeature, Key: semconv.KeyGeoLat, Value: "1.5"},
		},
		{
			what:   "an unregistered attribute is refused",
			volume: sample(),
			op: Op{Kind: OpSetAttr, World: "overworld", Feature: 1,
				Entity: semconv.EntityFeature, Key: unregistered, Value: "x"},
			refuse: "not registered",
		},
		{
			what:   "an attribute on the wrong entity is refused",
			volume: sample(),
			op: Op{Kind: OpSetAttr, World: "overworld", Collection: 7,
				Entity: semconv.EntityCollection, Key: semconv.KeyGeoLat, Value: "1.5"},
			refuse: "attaches to a feature",
		},
		{
			what:   "a value outside its vocabulary is refused",
			volume: sample(),
			op: Op{Kind: OpSetAttr, World: "overworld", Collection: 7,
				Entity: semconv.EntityCollection, Key: semconv.KeyRenderAs, Value: "hologram"},
			refuse: "is not one of",
		},
		{
			what:   "a feature joins a collection that exists",
			volume: sample(),
			op: Op{Kind: OpAddFeature, World: "overworld", Collection: 7,
				NewFeature: &Feature{ID: 2, Title: "Another chest", At: &Position{}}},
		},
		{
			what:   "a feature may not reuse an identifier",
			volume: sample(),
			op: Op{Kind: OpAddFeature, World: "overworld", Collection: 7,
				NewFeature: &Feature{ID: 1, Title: "A different chest", At: &Position{}}},
			refuse: "already spoken for",
		},
		{
			what:   "a feature needs a collection that exists",
			volume: sample(),
			op: Op{Kind: OpAddFeature, World: "overworld", Collection: 404,
				NewFeature: &Feature{ID: 2, At: &Position{}}},
			refuse: "no collection",
		},
		{
			what:   "a world may not be added twice",
			volume: sample(),
			op:     Op{Kind: OpAddWorld, NewWorld: &World{Slug: "overworld"}},
			refuse: "already pictures",
		},
		{
			what:   "a lens attaches",
			volume: sample(),
			op:     Op{Kind: OpSetLens, World: "overworld", Lens: &Lens{Name: "Aligned", TileSet: "other"}},
		},
		{
			what:   "a lens updates in place when it is the same picture",
			volume: sample(),
			op: Op{Kind: OpSetLens, World: "overworld",
				Lens: &Lens{Name: "Default", TileSet: "tunic/overworld", Stamp: "abc"}},
		},
		{
			what:   "a lens may not be repointed at another tile set",
			volume: sample(),
			op: Op{Kind: OpSetLens, World: "overworld",
				Lens: &Lens{Name: "Default", TileSet: "somewhere/else"}},
			refuse: "does not repoint",
		},
		{
			what:   "artwork lands on a collection that has none",
			volume: sample(),
			op:     Op{Kind: OpSetIcon, World: "overworld", Collection: 7, Key: "std--maki-monument"},
			refuse: "already names artwork",
		},
		{
			what:   "artwork does not displace a source's own",
			volume: arted,
			op:     Op{Kind: OpSetIcon, World: "overworld", Collection: 7, Key: "std--maki-monument"},
			refuse: "already names artwork",
		},
		{
			what:   "an operation about a world that is not there is refused",
			volume: sample(),
			op:     Op{Kind: OpSetProse, World: "elsewhere", Feature: 1, Value: "x"},
			refuse: "pictures no world",
		},
		{
			what:   "an operation nobody defined is refused",
			volume: sample(),
			op:     Op{Kind: "delete-everything", World: "overworld"},
			refuse: "no such operation",
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			err := Apply(c.volume, Contribution{Enricher: "test", Volume: "tunic", Ops: []Op{c.op}})
			switch {
			case c.refuse == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.refuse != "" && err == nil:
				t.Fatal("accepted")
			case c.refuse != "" && !strings.Contains(err.Error(), c.refuse):
				t.Fatalf("refused with %q, expected something about %q", err, c.refuse)
			}
		})
	}
}

// The one case above that looks like a contradiction: a collection whose
// artwork key is set by its source refuses a standard icon, and one whose key
// is free takes it. The sample's collection names "chest", so both rows refuse;
// this is the accepting half.
func TestArtworkLandsWhereThereIsNone(t *testing.T) {
	volume := sample()
	volume.Worlds[0].Collections[0].Icon = ""
	err := Apply(volume, Contribution{Volume: "tunic", Ops: []Op{{
		Kind: OpSetIcon, World: "overworld", Collection: 7,
		Key: "std--maki-monument", Value: "std--maki-monument.svg",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := volume.Worlds[0].Collections[0]
	if collection.Icon != "std--maki-monument" || collection.IconAsset != "std--maki-monument.svg" {
		t.Errorf("collection carries %q/%q", collection.Icon, collection.IconAsset)
	}
}

func TestContributionRoundTripsThroughItsCanonicalForm(t *testing.T) {
	contribution := Contribution{
		Enricher: "merge",
		Volume:   "tunic",
		Ops: []Op{
			{Kind: OpSetProse, World: "overworld", Feature: 1, Value: "a chest, & a key"},
			{Kind: OpSetAttr, World: "overworld", Feature: 1,
				Entity: semconv.EntityFeature, Key: semconv.KeyGeoLat, Value: "1.5"},
			{Kind: OpLedger, World: "overworld", Account: &Account{
				Source: "IGN Wiki", Slug: "ign-wiki",
				DonorFeatures: Counts{Point: 2},
				Matched:       []MatchedPair{{Donor: 9, Winner: 1, DistancePx: 12}},
				Added:         1,
			}},
		},
	}

	data, err := contribution.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// The canonical form carries what it says it carries: an ampersand in
	// somebody's prose is an ampersand.
	if !strings.Contains(string(data), "a chest, & a key") {
		t.Errorf("the canonical form escaped its own content:\n%s", data)
	}
	read, err := UnmarshalContribution(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := read.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Errorf("a round trip changed the bytes:\n%s\n%s", data, again)
	}

	// Applying the read-back form does what applying the value does. The
	// in-memory interface is a convenience over the serialized contract, so the
	// two have to agree.
	fromValue, fromBytes := sample(), sample()
	if err := Apply(fromValue, contribution); err != nil {
		t.Fatal(err)
	}
	if err := Apply(fromBytes, read); err != nil {
		t.Fatal(err)
	}
	if a, b := marshalVolume(t, fromValue), marshalVolume(t, fromBytes); a != b {
		t.Errorf("the value and its serialization applied differently:\n%s\n%s", a, b)
	}

	digest, err := contribution.Digest()
	if err != nil {
		t.Fatal(err)
	}
	twice, err := read.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digest != twice || len(digest) != 64 {
		t.Errorf("digest %q then %q", digest, twice)
	}
}

func marshalVolume(t *testing.T, v *Volume) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// stub is an enricher a test can point in any direction.
type stub struct {
	name     string
	declares []string
	ops      []Op
	err      error
}

func (s stub) Name() string       { return s.name }
func (s stub) Declares() []string { return s.declares }
func (s stub) Enrich(v *Volume, ctx Context) (Contribution, error) {
	if s.err != nil {
		return Contribution{}, s.err
	}
	return Contribution{Ops: s.ops}, nil
}

func TestQueueHoldsCurationAndTheBinaryToEachOther(t *testing.T) {
	offered := []Enricher{stub{name: "merge"}, stub{name: "stdicons"}}
	cases := []struct {
		what   string
		order  []string
		refuse string
	}{
		{what: "the declared order runs", order: []string{"stdicons", "merge"}},
		{what: "a name nobody answers", order: []string{"merge", "invented"}, refuse: "no enricher answers"},
		{what: "a name twice", order: []string{"merge", "merge", "stdicons"}, refuse: "twice"},
		{what: "an enricher nobody queued", order: []string{"merge"}, refuse: "runs in no declared order"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			queue, err := Queue(c.order, offered)
			if c.refuse == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				for at, name := range c.order {
					if queue[at].Name() != name {
						t.Errorf("position %d runs %s, curation says %s", at, queue[at].Name(), name)
					}
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.refuse) {
				t.Fatalf("error %v, expected something about %q", err, c.refuse)
			}
		})
	}
}

func TestRunHoldsAnEnricherToWhatItDeclares(t *testing.T) {
	writesGeo := []Op{{Kind: OpSetAttr, World: "overworld", Feature: 1,
		Entity: semconv.EntityFeature, Key: semconv.KeyGeoLat, Value: "1.5"}}

	if _, err := Run(sample(), []Enricher{stub{name: "sneaky", ops: writesGeo}}, Context{}); err == nil {
		t.Error("an enricher wrote an attribute it never declared")
	}
	if _, err := Run(sample(), []Enricher{
		stub{name: "honest", declares: []string{semconv.KeyGeoLat}, ops: writesGeo},
	}, Context{}); err != nil {
		t.Errorf("an enricher was refused what it declared: %v", err)
	}
	if _, err := Run(sample(), []Enricher{
		stub{name: "confused", declares: []string{unregistered}},
	}, Context{}); err == nil {
		t.Error("an enricher declared a key the registry does not know")
	}
}

func TestRunReportsNoChangeAsNoChange(t *testing.T) {
	// The driver opens every world's origin account before the queue runs, so
	// the volume a quiet run is compared against has its own account open too.
	opened := sample()
	OpenOrigin(opened)
	before := marshalVolume(t, opened)
	volume := sample()
	result, err := Run(volume, []Enricher{stub{name: "quiet"}}, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Error("an enricher that said nothing reported a change")
	}
	if len(result.Contributions) != 1 || !result.Contributions[0].Empty() {
		t.Error("a run does not report what each enricher had to say")
	}
	if after := marshalVolume(t, volume); after != before {
		t.Errorf("the volume moved:\n%s\n%s", before, after)
	}
}

func TestRunLetsLaterEnrichersSeeEarlierOnes(t *testing.T) {
	adds := stub{name: "adds", ops: []Op{{Kind: OpAddCollection, World: "overworld",
		NewCollection: &Collection{ID: 8, Title: "Doors", Kind: KindPoint}}}}
	// The second enricher can only succeed if the first one's collection is
	// already there, which is the whole meaning of an ordered queue. It ledgers
	// what it adds, because the world gate counts what every account claims.
	fills := stub{name: "fills", ops: []Op{
		{Kind: OpAddFeature, World: "overworld", Collection: 8,
			NewFeature: &Feature{ID: 2, Title: "A door", At: &Position{}}},
		{Kind: OpLedger, World: "overworld", Account: &Account{
			Source: "Doors", DonorFeatures: Counts{Point: 1}, Added: 1}},
	}}

	volume := sample()
	if _, err := Run(volume, []Enricher{adds, fills}, Context{}); err != nil {
		t.Fatalf("in order: %v", err)
	}
	if _, err := Run(sample(), []Enricher{fills, adds}, Context{}); err == nil {
		t.Error("out of order, the second enricher filled a collection that did not exist yet")
	}
}

func TestGateAuditsAnAccount(t *testing.T) {
	cases := []struct {
		what    string
		account Account
		refuse  string
	}{
		{
			what: "everything offered is accounted for",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 3},
				Matched: []MatchedPair{{Donor: 1, Winner: 10}}, Added: 1,
				Held: []HeldItem{{Donor: 3, Reason: "unsure"}}},
		},
		{
			what: "a feature nobody accounted for",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 3},
				Matched: []MatchedPair{{Donor: 1, Winner: 10}}},
			refuse: "accounts for 1 of 3",
		},
		{
			what: "shapes are held, every one",
			account: Account{Source: "IGN", DonorFeatures: Counts{Area: 2},
				Held: []HeldItem{{Donor: 1, Reason: HeldShapeReason}}},
			refuse: "holds 1 shape features of the 2",
		},
		{
			what: "one place matched twice",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 2},
				Matched: []MatchedPair{{Donor: 1, Winner: 10}, {Donor: 2, Winner: 10}}},
			refuse: "a place is one place",
		},
		{
			what: "a take of a key no feature may carry",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 1},
				Matched: []MatchedPair{{Donor: 1, Winner: 10, Took: []string{semconv.KeyRenderAs}}}},
			refuse: "which no feature may carry",
		},
		{
			what: "an enriched flag its takes do not support",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 1},
				Matched: []MatchedPair{{Donor: 1, Winner: 10, Enriched: true}}},
			refuse: "says enriched=true",
		},
		{
			what:    "added shapes nobody can produce",
			account: Account{Source: "IGN", DonorFeatures: Counts{Point: 1}, Added: 1, AddedShapes: 2},
			refuse:  "no shape feature merges yet",
		},
		{
			what:    "an origin account that claims a contribution",
			account: Account{Source: "MapGenie", Origin: true, Added: 3},
			refuse:  "only says what the world arrived with",
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			err := GateAccount(c.account)
			switch {
			case c.refuse == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.refuse != "" && (err == nil || !strings.Contains(err.Error(), c.refuse)):
				t.Fatalf("error %v, expected something about %q", err, c.refuse)
			}
		})
	}
}

func TestGateWorldHoldsTheTallyToTheLedger(t *testing.T) {
	volume := sample()
	OpenOrigin(volume)
	if err := GateWorld(&volume.Worlds[0]); err != nil {
		t.Fatalf("a world with only its origin account: %v", err)
	}

	// A feature added without a ledger line to answer for it.
	volume.Worlds[0].Collections[0].Features = append(volume.Worlds[0].Collections[0].Features,
		Feature{ID: 2, Title: "Unaccounted", At: &Position{}})
	if err := GateWorld(&volume.Worlds[0]); err == nil {
		t.Error("a world held a feature no account claims")
	}

	// Two features under one identifier.
	doubled := sample()
	OpenOrigin(doubled)
	doubled.Worlds[0].Collections = append(doubled.Worlds[0].Collections, Collection{
		ID: 8, Kind: KindArea, Title: "Zones",
		Features: []Feature{{ID: 1, Title: "A zone"}},
	})
	if err := GateWorld(&doubled.Worlds[0]); err == nil {
		t.Error("a point and an area shared an identifier")
	}
}

func TestOpenOriginSaysWhatTheWorldArrivedWith(t *testing.T) {
	volume := sample()
	OpenOrigin(volume)
	account := volume.Worlds[0].Ledger[0]
	if !account.Origin || account.Slug != "mapgenie" || account.DonorFeatures.Point != 1 {
		t.Errorf("origin account is %+v", account)
	}
	// Opening a ledger twice does not rewrite it.
	volume.Worlds[0].Collections[0].Features = append(volume.Worlds[0].Collections[0].Features,
		Feature{ID: 2, At: &Position{}})
	OpenOrigin(volume)
	if volume.Worlds[0].Ledger[0].DonorFeatures.Point != 1 {
		t.Error("opening an account that was already open rewrote it")
	}
}

func TestServingIsTheNewestCapture(t *testing.T) {
	older := &Volume{Worlds: []World{{CapturedAt: "2026-08-01T04:03:49Z"}}}
	newer := &Volume{Worlds: []World{{CapturedAt: "2026-08-01T05:09:07Z"}}}
	if got := Serving([]*Volume{older, newer}); got != 1 {
		t.Errorf("served reading %d, expected the newer one", got)
	}
	if got := Serving([]*Volume{newer, older}); got != 0 {
		t.Errorf("served reading %d, expected the newer one", got)
	}
	if got := Serving(nil); got != -1 {
		t.Errorf("served %d readings of nothing", got)
	}
}

func TestBuildRevisionOutranksThePlainBuild(t *testing.T) {
	for _, generate := range []int{1, 9, 99} {
		enriched, err := BuildRevision(generate)
		if err != nil {
			t.Fatalf("generate revision %d: %v", generate, err)
		}
		if enriched <= generate {
			t.Errorf("an enriched build of generate revision %d carries %d, which does not outrank it",
				generate, enriched)
		}
		policy, ok := Enriched(enriched)
		if !ok || policy != PolicyRevision {
			t.Errorf("revision %d reads as enriched=%t policy %d", enriched, ok, policy)
		}
		if _, ok := Enriched(generate); ok && generate < RevisionSpan {
			t.Errorf("a plain build at revision %d reads as enriched", generate)
		}
	}
	if _, err := BuildRevision(RevisionSpan); err == nil {
		t.Error("a generate revision that does not fit the span was packed anyway")
	}
	// Two enrich policies of one generate revision order by the enrich policy,
	// which is the whole point of the packing.
	first, _ := BuildRevision(9)
	if first != PolicyRevision*RevisionSpan+9 {
		t.Errorf("revision packing is %d", first)
	}
}

func TestGeometryReadsRingsAndLines(t *testing.T) {
	rings := Geometry{Type: "MultiPolygon",
		Coordinates: json.RawMessage(`[[[[0,0],[2,0],[2,2],[0,2],[0,0]]]]`)}
	if got := rings.Rings(); len(got) != 1 || len(got[0]) != 1 || len(got[0][0]) != 5 {
		t.Errorf("rings read as %v", got)
	}
	if got := rings.Lines(); got != nil {
		t.Errorf("a polygon read as lines: %v", got)
	}
	lines := Geometry{Type: "MultiLineString", Coordinates: json.RawMessage(`[[[0,0],[1,1]]]`)}
	if got := lines.Lines(); len(got) != 1 || len(got[0]) != 2 {
		t.Errorf("lines read as %v", got)
	}
	if got := lines.Positions(); len(got) != 2 || got[1] != [2]float64{1, 1} {
		t.Errorf("positions read as %v", got)
	}
}

func TestGridProjectionRoundTrips(t *testing.T) {
	grid := Grid{SourceZoom: 5, FirstTile: 0, TileSize: 256, Size: 8192}
	for _, position := range []Position{{Lat: 0, Lng: 0}, {Lat: 27.05, Lng: -22.5}, {Lat: -60, Lng: 179}} {
		x, y := grid.ProjectX(position.Lng), grid.ProjectY(position.Lat)
		lat, lng := grid.UnprojectLat(y), grid.UnprojectLng(x)
		if diff := lat - position.Lat; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("latitude %v came back as %v", position.Lat, lat)
		}
		if diff := lng - position.Lng; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("longitude %v came back as %v", position.Lng, lng)
		}
	}
	if (Grid{}).Ready() {
		t.Error("a world cut from no window reports that it can measure")
	}
}

func TestMergeIdentityPrefersTheDeclaredName(t *testing.T) {
	cases := []struct {
		what       string
		collection Collection
		want       string
	}{
		{"the declared merge identity wins",
			Collection{Attrs: map[string]string{semconv.KeyCollectionKey: "ripperdoc"},
				Key: "clinic", Icon: "ripper-doc"}, "ripperdoc"},
		{"then the source's own key", Collection{Key: "zoning", Icon: "polygon"}, "zoning"},
		{"then the artwork key", Collection{Icon: "chest"}, "chest"},
		{"and otherwise nothing", Collection{}, ""},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if got := MergeIdentity(c.collection); got != c.want {
				t.Errorf("identity %q, want %q", got, c.want)
			}
		})
	}
}
