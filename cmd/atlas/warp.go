package main

import (
	"log/slog"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/align"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// The other half of the seam adapt.go opens.
//
// A warped variant is a generate-lane pyramid -- rasters cut, folded and
// stamped -- whose one extra input is an alignment, and an alignment is fitted
// from the names two readings share, which is enrich-lane work (issue #5 §5.3).
// Neither lane may import the other, so the fit is made here, in the binary that
// holds both, and handed to the deriver as six numbers.
//
// Nothing is duplicated to arrange that. The deriver already declares the
// transformation as an input on `tiles.Warp`, exactly as it declares the donor's
// tiles: what it needs is a fitted affine, not the ability to fit one. The
// least-squares solver, the anchor matching, the trimming and the refusal all
// stay in `enrich/align`, where the merge that stands on the same fit uses them.

// reading is one source's account of one world, planned: the pyramid it will
// derive, and the places it names, which are what another reading is aligned by.
type reading struct {
	volume string
	source string
	world  doc.World
	frame  tiles.Frame
	plan   *tiles.Plan
}

// planWarps pairs the readings of each volume and plans the lesser picture's
// resample into the finer one's world.
//
// Which reading serves is a resolution question, not a recency one: the picture
// that draws its world at the most pixels is the frame everything else is
// brought into, because resampling into a coarser world would throw away detail
// that was captured. Only readings from *different* sources are ever paired --
// one source that divided its world into several grounds did so deliberately --
// and the pairing is decided from the whole set at once, so it never depends on
// the order the archive happened to list its captures in.
//
// A pair whose names do not close is not warped. The refusal comes from the fit
// itself, and it is warned about rather than swallowed: two readings that could
// not be brought together is something a person should eventually hear.
func planWarps(readings []reading, log *slog.Logger) []tiles.Plan {
	byVolume := map[string][]int{}
	var volumes []string
	for index, held := range readings {
		if _, seen := byVolume[held.volume]; !seen {
			volumes = append(volumes, held.volume)
		}
		byVolume[held.volume] = append(byVolume[held.volume], index)
	}
	sort.Strings(volumes)

	var out []tiles.Plan
	for _, volume := range volumes {
		group := deepestPerWorld(readings, byVolume[volume])
		if len(sourcesOf(readings, group)) < 2 {
			continue
		}
		base := group[0]
		for _, candidate := range group[1:] {
			if tiles.WorldDepth(*readings[candidate].plan) > tiles.WorldDepth(*readings[base].plan) {
				base = candidate
			}
		}
		for _, donor := range group {
			if readings[donor].source == readings[base].source {
				continue
			}
			plan, ok := planWarp(readings[base], readings[donor], log)
			if ok {
				out = append(out, plan)
			}
		}
	}
	return out
}

// deepestPerWorld keeps one reading per ground: the picture worth aligning is
// the deepest one, and a world's other lenses are alternate art already sharing
// its window. The result is ordered, so what follows it is deterministic.
func deepestPerWorld(readings []reading, indexes []int) []int {
	held := map[[2]string]int{}
	for _, index := range indexes {
		key := [2]string{readings[index].source, readings[index].world.Slug}
		at, seen := held[key]
		if !seen || tiles.WorldDepth(*readings[index].plan) > tiles.WorldDepth(*readings[at].plan) {
			held[key] = index
		}
	}
	out := make([]int, 0, len(held))
	for _, index := range held {
		out = append(out, index)
	}
	sort.Slice(out, func(a, b int) bool {
		left, right := readings[out[a]], readings[out[b]]
		if left.source != right.source {
			return left.source < right.source
		}
		return left.world.Slug < right.world.Slug
	})
	return out
}

func sourcesOf(readings []reading, indexes []int) map[string]bool {
	out := map[string]bool{}
	for _, index := range indexes {
		out[readings[index].source] = true
	}
	return out
}

func planWarp(base, donor reading, log *slog.Logger) (tiles.Plan, bool) {
	affine, report, err := align.Fit(anchorsOf(donor), anchorsOf(base))
	if err != nil {
		log.Warn("pictures stay apart", logging.Volume(base.volume),
			logging.World(base.world.Slug), logging.Source(donor.source),
			"reason", err.Error())
		return tiles.Plan{}, false
	}
	plan := tiles.PlanWarp(*base.plan, *donor.plan, tiles.Affine{
		AX: affine.AX, BX: affine.BX, CX: affine.CX,
		AY: affine.AY, BY: affine.BY, CY: affine.CY,
	}, lensName(donor))
	log.Info("picture aligned", logging.Volume(base.volume),
		logging.World(base.world.Slug), logging.Source(donor.source),
		logging.Lens(plan.Name), "alignment", report.String(),
		"zoom", plan.Warp.TargetZoom)
	return plan, true
}

// lensName is what the warped picture is called once it hangs on somebody
// else's ground: the donor's own name for its picture, because that is what a
// reader is being offered a second view from.
func lensName(donor reading) string {
	for _, lens := range donor.world.Lenses {
		if lens.TileSet == donor.plan.TileSet {
			return lens.Name
		}
	}
	return donor.source
}

// anchorsOf lands every named place a reading pins in that reading's own world
// pixels. It is the same reading of a ground the merge takes -- one anchor per
// located point feature, titled as the source titled it -- so one fit serves
// both the raster and the features drawn on it.
func anchorsOf(held reading) []align.Anchor {
	zoom, first := held.frame.Window()
	grid := enrich.Grid{
		SourceZoom: zoom,
		FirstTile:  first,
		TileSize:   tiles.TileSize,
		Size:       tiles.WorldSize,
	}
	var out []align.Anchor
	for _, collection := range held.world.Collections {
		if collection.Kind != doc.KindPoint {
			continue
		}
		for _, feature := range collection.Features {
			if feature.At == nil {
				continue
			}
			out = append(out, align.Anchor{
				Title: feature.Title,
				X:     grid.ProjectX(feature.At.Lng),
				Y:     grid.ProjectY(feature.At.Lat),
			})
		}
	}
	return out
}
