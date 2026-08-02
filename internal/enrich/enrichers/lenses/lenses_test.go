package lenses

import (
	"testing"

	"github.com/FelineStateMachine/atlas/internal/enrich"
)

// offers is a lens offer surface a test states outright.
type offers map[string][]enrich.Lens

func (o offers) Offers(world string) []enrich.Lens { return o[world] }

func volume(attached ...enrich.Lens) *enrich.Volume {
	return &enrich.Volume{
		Slug:   "cyberpunk-2077",
		Worlds: []enrich.World{{Slug: "night-city", Lenses: attached}},
	}
}

func run(t *testing.T, v *enrich.Volume, offered offers) enrich.Contribution {
	t.Helper()
	contribution, err := New().Enrich(v, enrich.Context{Lenses: offered})
	if err != nil {
		t.Fatal(err)
	}
	return contribution
}

func TestAPictureAttachesToTheGroundItPictures(t *testing.T) {
	native := enrich.Lens{Name: "Default", TileSet: "cbp", Stamp: "aaa"}
	warped := enrich.Lens{Name: "IGN Wiki", TileSet: "aligned-night-city",
		Stamp: "bbb", AlignedWith: "cbp"}

	cases := []struct {
		what     string
		attached []enrich.Lens
		offered  []enrich.Lens
		attaches int
	}{
		{
			what:     "a picture the ground does not have",
			attached: []enrich.Lens{native},
			offered:  []enrich.Lens{warped},
			attaches: 1,
		},
		{
			what:     "the same picture, offered again",
			attached: []enrich.Lens{native, warped},
			offered:  []enrich.Lens{warped},
		},
		{
			what:     "the same picture, re-derived",
			attached: []enrich.Lens{native, warped},
			offered:  []enrich.Lens{{Name: "IGN Wiki", TileSet: "aligned-night-city", Stamp: "ccc", AlignedWith: "cbp"}},
			attaches: 1,
		},
		{
			what:     "an offer with nothing behind it",
			attached: []enrich.Lens{native},
			offered:  []enrich.Lens{{Name: "Nothing"}},
		},
		{
			what:     "two pictures at once",
			attached: []enrich.Lens{native},
			offered: []enrich.Lens{warped,
				{Name: "Night", TileSet: "cbp-night", Stamp: "ddd"}},
			attaches: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			v := volume(c.attached...)
			contribution := run(t, v, offers{"night-city": c.offered})
			if len(contribution.Ops) != c.attaches {
				t.Fatalf("attached %d pictures, want %d", len(contribution.Ops), c.attaches)
			}
			if err := enrich.Apply(v, contribution); err != nil {
				t.Fatalf("apply: %v", err)
			}
			// Whatever happened, no picture was taken away.
			for _, standing := range c.attached {
				found := false
				for _, lens := range v.Worlds[0].Lenses {
					if lens.Name == standing.Name {
						found = true
					}
				}
				if !found {
					t.Errorf("lens %q was removed", standing.Name)
				}
			}
		})
	}
}

func TestTheStampTravelsWithThePicture(t *testing.T) {
	v := volume(enrich.Lens{Name: "Default", TileSet: "cbp"})
	contribution := run(t, v, offers{"night-city": {
		{Name: "IGN Wiki", TileSet: "aligned", Stamp: "derivation-stamp", AlignedWith: "cbp"},
	}})
	if err := enrich.Apply(v, contribution); err != nil {
		t.Fatal(err)
	}
	for _, lens := range v.Worlds[0].Lenses {
		if lens.Name != "IGN Wiki" {
			continue
		}
		if lens.Stamp != "derivation-stamp" || lens.AlignedWith != "cbp" {
			t.Errorf("the attached picture says %+v", lens)
		}
		return
	}
	t.Error("the picture did not attach")
}

func TestAnOfferMayNotRepointASourcesLens(t *testing.T) {
	v := volume(enrich.Lens{Name: "Default", TileSet: "cbp"})
	contribution := run(t, v, offers{"night-city": {{Name: "Default", TileSet: "somewhere-else"}}})
	if len(contribution.Ops) != 1 {
		t.Fatalf("the offer produced %d operations", len(contribution.Ops))
	}
	if err := enrich.Apply(v, contribution); err == nil {
		t.Error("a source's picture was repointed at another pyramid")
	}
}

func TestNothingOfferedIsNothingSaid(t *testing.T) {
	v := volume(enrich.Lens{Name: "Default", TileSet: "cbp"})
	if contribution := run(t, v, offers{}); !contribution.Empty() {
		t.Error("an empty offer surface produced operations")
	}
	contribution, err := New().Enrich(v, enrich.Context{})
	if err != nil || !contribution.Empty() {
		t.Errorf("no offer surface at all: %v, %d operations", err, len(contribution.Ops))
	}
}
