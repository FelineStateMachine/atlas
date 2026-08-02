// Package lenses attaches pictures to grounds.
//
// A world is a ground you can stand on; a lens is a picture of it. The two are
// separable, and that separation is what makes this an enricher rather than a
// step in composition: a ground published years ago can be given another
// picture -- a second map style, a later capture, somebody else's raster warped
// into this world's space -- without the ground itself changing, and without
// anything already published being touched. A new build lands beside the old
// one carrying one more picture.
//
// # What this enricher does and does not decide
//
// It does not derive pyramids. Deriving rasters is the generate lane's work
// (fetching is crawling, cutting is tiling), and the derivation stamp a pyramid
// carries is written there. What arrives here is the offer: these pictures were
// derived for this ground, each with the stamp that says what it was made from.
// This enricher decides which of them the world does not already have, attaches
// those, and carries the stamps through so the picture in the bundle can always
// be traced back to the derivation that produced it.
//
// An offer whose stamp differs from the lens already attached under that name is
// a re-derivation, and it updates in place: the ground has not changed and the
// picture is of the same tile set, so what moves is the stamp. An offer that
// would repoint a source's lens at another tile set is refused by the
// contribution's own rules -- that would be a rewrite of what a source said its
// picture was.
package lenses

import (
	"log/slog"

	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// Name is what curation queues this enricher as.
const Name = "lenses"

// Enricher attaches offered pictures to the worlds they picture.
type Enricher struct{}

// New builds the enricher.
func New() *Enricher { return &Enricher{} }

func (*Enricher) Name() string { return Name }

// Declares is empty: a lens is a picture, not a claim about the ground.
func (*Enricher) Declares() []string { return nil }

// Enrich attaches every offered picture a world does not already carry.
func (e *Enricher) Enrich(v *enrich.Volume, ctx enrich.Context) (enrich.Contribution, error) {
	out := enrich.Contribution{Enricher: Name, Volume: v.Slug}
	if ctx.Lenses == nil {
		return out, nil
	}
	log := ctx.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op(Name), logging.Volume(v.Slug))

	for _, world := range v.Worlds {
		for _, offer := range ctx.Lenses.Offers(world.Slug) {
			if offer.Name == "" || offer.TileSet == "" {
				continue
			}
			if standing, held := attached(world, offer.Name); held {
				// Same picture, same derivation: there is nothing to say, and
				// saying it would be a build with no change in it.
				if standing.TileSet == offer.TileSet && standing.Stamp == offer.Stamp &&
					standing.AlignedWith == offer.AlignedWith {
					continue
				}
			}
			attach := offer
			out.Ops = append(out.Ops, enrich.Op{
				Kind: enrich.OpSetLens, World: world.Slug, Lens: &attach,
			})
			log.Info("lens attached", logging.World(world.Slug), logging.Lens(offer.Name),
				"tileSet", offer.TileSet, "aligned", offer.AlignedWith != "")
		}
	}
	return out, nil
}

func attached(w enrich.World, name string) (enrich.Lens, bool) {
	for _, lens := range w.Lenses {
		if lens.Name == name {
			return lens, true
		}
	}
	return enrich.Lens{}, false
}
