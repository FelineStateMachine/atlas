package main

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// The pairing policy, stated on readings a test can hold in its head.
//
// What the deriver does with a warp is exercised whole by the pipeline test in
// this package. What is measured here is the decision before it: which
// readings are paired at all, which of them is the frame the other is brought
// into, and what happens when two readings of one ground share too few names to
// align.

// placed is a reading of one ground: a plan deep enough to say how finely it
// draws, and a world holding named places at known coordinates.
func placed(volume, source, world string, maxFullZoom int, offsetLng float64, names int) reading {
	plan := &tiles.Plan{
		TileSet:     source + "/" + world,
		Name:        volume + "__" + world,
		MaxFullZoom: maxFullZoom,
	}
	held := doc.World{Slug: world, Title: world,
		Lenses: []doc.Lens{{Name: source + " lens", TileSet: plan.TileSet}}}
	collection := doc.Collection{Title: "Places", Kind: doc.KindPoint}
	for index := range names {
		collection.Features = append(collection.Features, doc.Feature{
			Title: fmt.Sprintf("Place %d", index),
			// Laid out as a grid rather than a line: anchors all on one line
			// determine no transformation, and the fit says so.
			At: &doc.Position{
				Lat: float64(index%5) * 0.01,
				Lng: offsetLng + float64(index/5)*0.01,
			},
		})
	}
	held.Collections = append(held.Collections, collection)
	return reading{volume: volume, source: source, world: held, plan: plan}
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestTheFinerPictureIsTheFrame is the policy's whole point: the reading that
// draws its world at the most pixels is the one everything else is resampled
// into, whichever order the archive listed the captures in.
func TestTheFinerPictureIsTheFrame(t *testing.T) {
	coarse := placed("night", "wiki", "city", 5, 0, 20)
	fine := placed("night", "publisher", "city", 7, 0, 20)

	for _, order := range [][]reading{{coarse, fine}, {fine, coarse}} {
		plans := planWarps(order, quiet())
		if len(plans) != 1 {
			t.Fatalf("planned %d warps, want one", len(plans))
		}
		if plans[0].Warp.Base.TileSet != fine.plan.TileSet {
			t.Errorf("the warp is rendered into %s; the finer picture is %s",
				plans[0].Warp.Base.TileSet, fine.plan.TileSet)
		}
		if plans[0].Warp.Donor.TileSet != coarse.plan.TileSet {
			t.Errorf("the warp resamples %s, not the coarser reading", plans[0].Warp.Donor.TileSet)
		}
		if plans[0].LensName != "wiki lens" {
			t.Errorf("the variant is called %q; a reader is being offered the donor's own picture",
				plans[0].LensName)
		}
	}
}

// TestOneSourceIsNeverWarpedOntoItself holds the rule that a source which
// divided its own world into several grounds did so deliberately.
func TestOneSourceIsNeverWarpedOntoItself(t *testing.T) {
	readings := []reading{
		placed("night", "publisher", "city", 7, 0, 20),
		placed("night", "publisher", "badlands", 5, 0, 20),
	}
	if plans := planWarps(readings, quiet()); len(plans) != 0 {
		t.Errorf("planned %d warps between two grounds of one source", len(plans))
	}
}

// TestReadingsThatDoNotAlignStayApart is the refusal: a fit that will not close
// produces no variant rather than a picture drawn confidently in the wrong
// place.
func TestReadingsThatDoNotAlignStayApart(t *testing.T) {
	// Two readings of "one" ground that share no name at all.
	base := placed("night", "publisher", "city", 7, 0, 20)
	donor := placed("night", "wiki", "city", 5, 0, 20)
	donor.world.Collections[0].Features = nil
	if plans := planWarps([]reading{base, donor}, quiet()); len(plans) != 0 {
		t.Errorf("planned %d warps from readings that name nothing in common", len(plans))
	}
}

// TestNamesAreSettledBeforeAnythingIsDerived is the collision rule: two sources
// naming one ground the same thing both take their publisher's path into the
// pyramid name, so neither depends on the order the archive listed them in.
func TestNamesAreSettledBeforeAnythingIsDerived(t *testing.T) {
	plans := []tiles.Plan{
		{TileSet: "cbp", Name: "cyberpunk-2077__night-city"},
		{TileSet: "cyberpunk-2077/night-city", Name: "cyberpunk-2077__night-city"},
		{TileSet: "tunic/world", Name: "tunic__world"},
	}
	tiles.Settle(plans)
	want := []string{
		"cyberpunk-2077__night-city__cbp",
		"cyberpunk-2077__night-city__cyberpunk-2077-night-city",
		"tunic__world",
	}
	for index, name := range want {
		if plans[index].Name != name {
			t.Errorf("plan %d settled as %q, want %q", index, plans[index].Name, name)
		}
	}
}
