package stdicons

import (
	"slices"
	"strings"
	"testing"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
)

func volume(collections ...enrich.Collection) *enrich.Volume {
	return &enrich.Volume{
		Slug:   "bend-or",
		Worlds: []enrich.World{{Slug: "today", Collections: collections}},
	}
}

func declaring(id int64, ref string) enrich.Collection {
	return enrich.Collection{
		ID: id, Title: "Historic Resources", Kind: enrich.KindPoint,
		Attrs: map[string]string{semconv.KeyIconStd: ref},
	}
}

func TestResolutionLandsWhereThereIsNoArtwork(t *testing.T) {
	own := declaring(2, "maki/mountain")
	own.Icon = "the-source-shipped-this"
	composed := declaring(3, "maki/park")
	composed.IconAsset = "already-resolved.svg"
	shape := declaring(4, "maki/water")
	shape.Kind = enrich.KindArea

	cases := []struct {
		what       string
		collection enrich.Collection
		resolves   bool
	}{
		{"a declaration with nothing in the slot", declaring(1, "maki/monument"), true},
		{"a collection whose source shipped artwork", own, false},
		{"a collection whose artwork is already resolved", composed, false},
		{"a shape collection, which wears no marker", shape, false},
		{"a collection that declares nothing",
			enrich.Collection{ID: 5, Kind: enrich.KindPoint}, false},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			contribution, err := New().Enrich(volume(c.collection), enrich.Context{})
			if err != nil {
				t.Fatal(err)
			}
			if contribution.Empty() == c.resolves {
				t.Fatalf("resolved=%t, want %t", !contribution.Empty(), c.resolves)
			}
		})
	}
}

func TestOneAssetTravelsOnceHoweverManyCollectionsNameIt(t *testing.T) {
	contribution, err := New().Enrich(volume(
		declaring(1, "maki/monument"),
		declaring(2, "maki/monument"),
		declaring(3, "maki/mountain"),
	), enrich.Context{})
	if err != nil {
		t.Fatal(err)
	}
	assets, resolutions := 0, 0
	for _, op := range contribution.Ops {
		switch op.Kind {
		case enrich.OpAddAsset:
			assets++
		case enrich.OpSetIcon:
			resolutions++
		}
	}
	if assets != 2 || resolutions != 3 {
		t.Errorf("%d assets for %d collections, want 2 for 3", assets, resolutions)
	}
}

func TestADeclarationTheLibraryCannotAnswerFailsTheBuild(t *testing.T) {
	// The one place in this lane where refusal beats silence: the promise was
	// made by a translator author, in this repository, and an unanswerable one
	// is a typo that should be heard about.
	_, err := New().Enrich(volume(declaring(1, "maki/skyscraper")), enrich.Context{})
	if err == nil {
		t.Fatal("a declaration nobody vendored was quietly dropped")
	}
	if !strings.Contains(err.Error(), "not vendored") {
		t.Errorf("refused with %v", err)
	}
}

func TestStandardNamesItsProvenance(t *testing.T) {
	cases := []struct {
		ref    string
		asset  string
		refuse bool
	}{
		{ref: "maki/monument", asset: "std--maki-monument.svg"},
		{ref: "maki/mountain", asset: "std--maki-mountain.svg"},
		{ref: "maki/skyscraper", refuse: true},
		{ref: "lucide/mountain", refuse: true},
		{ref: "mountain", refuse: true},
		{ref: "", refuse: true},
		{ref: "maki/../../etc/passwd", refuse: true},
	}
	for _, c := range cases {
		t.Run(c.ref, func(t *testing.T) {
			data, asset, err := Standard(c.ref)
			if c.refuse {
				if err == nil {
					t.Fatalf("%q resolved to %s", c.ref, asset)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if asset != c.asset {
				t.Errorf("asset %q, want %q", asset, c.asset)
			}
			if !strings.Contains(string(data), "<svg") {
				t.Error("the resolved bytes are not an svg")
			}
		})
	}
}

func TestTheVendoredSetIsTheVocabulary(t *testing.T) {
	vocabulary := Vocabulary()
	if len(vocabulary) < 10 {
		t.Fatalf("the vendored set holds %d names", len(vocabulary))
	}
	if !slices.IsSorted(vocabulary) {
		t.Error("the vocabulary is not in a stable order")
	}
	for _, ref := range vocabulary {
		if _, _, err := Standard(ref); err != nil {
			t.Errorf("%q is vendored but does not resolve: %v", ref, err)
		}
	}
	if slices.Contains(vocabulary, "maki/LICENSE") {
		t.Error("the licence is listed as a glyph")
	}
}

func TestTheEnricherDeclaresNoAttributes(t *testing.T) {
	// Artwork is not a claim about the world, and the kind of artwork a
	// collection carries is declared by composition, which is the step that
	// knows whether the asset actually travelled.
	if declares := New().Declares(); len(declares) != 0 {
		t.Errorf("stdicons declares %v", declares)
	}
}
