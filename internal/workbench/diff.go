package workbench

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/enrich/maturity"
)

// Two builds of one volume, read side by side.
//
// A diff says what moved, and it is headlined by the one number that decides
// anything: the score (issue #5 §5.3). Everything under the headline is
// diagnostic -- the absolute axes, the features themselves, the stability of
// the merge ledgers' agreements -- and everything is computed from two builds
// already on disk, because comparing builds is how a policy change is judged
// and a judgement must not depend on anything but its subjects.

// buildDiff is everything that moved between build A and build B, read in the
// A-to-B direction: an addition is something B has that A did not.
type buildDiff struct {
	A, B *maturity.Score
	// Movement is the score delta, with what the ledger accounts for. It is
	// the headline; the gate of docs/enrich.md reads the same comparison.
	Movement maturity.Comparison
	Worlds   []deltaRow
	Axes     []deltaRow
	Added    []featureRef
	Removed  []featureRef
	Pairs    []pairStability
}

// Verdict is the headline in one word: how the score moved, and whether the two
// builds are comparable at all.
func (d *buildDiff) Verdict() string {
	switch {
	case !d.Movement.Comparable:
		return "not comparable"
	case d.Movement.Delta > 0:
		return "richer"
	case d.Movement.Delta < 0 && -d.Movement.Delta <= d.Movement.Allowance:
		return "corrected"
	case d.Movement.Delta < 0:
		return "poorer"
	}
	return "unmoved"
}

// Delta is the score movement, signed.
func (d *buildDiff) Delta() string { return signedInt(d.Movement.Delta) }

// deltaRow is one measurement in both builds with its movement, formatted for
// the page; Sign carries the direction for styling without re-parsing.
type deltaRow struct {
	Name  string
	A, B  string
	Delta string
	Sign  int
}

// featureRef names one point feature as the packed payload carries it.
type featureRef struct {
	World string
	ID    int64
	Title string
}

// pairStability is one (world, source) ledger's agreement compared across the
// two builds: pairs standing in both, pairs only the newer build made, and
// pairs the newer build gave up. A pair is the same pair only when both its
// donor and its winner agree -- a match that moved to another winner reads as
// one lost and one gained, which is exactly the event a policy diff is looking
// for.
type pairStability struct {
	World  string
	Source string
	Kept   int
	Gained int
	Lost   int
}

// diffBuilds compares two scored builds and their unpacked features.
func diffBuilds(a, b *maturity.Score, featuresA, featuresB map[string]map[int64]string) *buildDiff {
	d := &buildDiff{A: a, B: b, Movement: maturity.Compare(a, b)}
	d.Worlds = worldDeltas(a, b)
	d.Axes = axisDeltas(a.Axes, b.Axes)
	d.Added = featuresOnly(featuresB, featuresA)
	d.Removed = featuresOnly(featuresA, featuresB)
	d.Pairs = pairDiff(a, b)
	return d
}

// worldDeltas is the score itself, broken down the way it was summed: every
// world either build carries, with what it is worth in each.
func worldDeltas(a, b *maturity.Score) []deltaRow {
	totals := map[string][2]int{}
	var order []string
	take := func(score *maturity.Score, side int) {
		for _, world := range score.Worlds {
			held, seen := totals[world.Slug]
			if !seen {
				order = append(order, world.Slug)
			}
			held[side] = world.Total
			totals[world.Slug] = held
		}
	}
	take(a, 0)
	take(b, 1)
	sort.Strings(order)

	rows := make([]deltaRow, 0, len(order))
	for _, slug := range order {
		held := totals[slug]
		rows = append(rows, count(slug, held[0], held[1]))
	}
	return rows
}

// axisDeltas is the five absolute axes, carried from the reference workbench as
// diagnostics. Nothing here gates anything; it is what a person reads to
// understand a score that moved.
func axisDeltas(a, b maturity.Axes) []deltaRow {
	return []deltaRow{
		count("point features", a.Points, b.Points),
		count("features of every kind", a.Features, b.Features),
		count("described", a.Described, b.Described),
		share("described share", a.Described, a.Features, b.Described, b.Features),
		count("median prose chars", a.MedianLength, b.MedianLength),
		count("tiles", a.TileCount, b.TileCount),
		megabytes("unique raster", a.RasterBytes, b.RasterBytes),
		count("depth", a.Depth, b.Depth),
		count("tiles at depth", a.DepthTiles, b.DepthTiles),
		count("collections", a.Collections, b.Collections),
		count("groups", a.Groups, b.Groups),
		count("text label sets", a.TextSets, b.TextSets),
		count("shapes", a.Shapes, b.Shapes),
		count("shape vertices", a.Vertices, b.Vertices),
		count("lenses", a.Lenses, b.Lenses),
		count("icons carried", a.IconsCarried, b.IconsCarried),
		share("icon coverage", a.IconsCarried, a.IconsWanted, b.IconsCarried, b.IconsWanted),
		count("conventions", a.Conventions, b.Conventions),
		share("render declared", a.RenderDeclared, a.Collections, b.RenderDeclared, b.Collections),
		count("standard icons", a.StdIcons, b.StdIcons),
		count("features with coordinates", a.GeoFeatures, b.GeoFeatures),
		count("memberships joined", a.Memberships, b.Memberships),
		count("unknown attributes", a.UnknownAttrs, b.UnknownAttrs),
	}
}

// featuresOnly lists the features in have that missing lacks, by world and id,
// in a stable order.
func featuresOnly(have, missing map[string]map[int64]string) []featureRef {
	var only []featureRef
	for world, features := range have {
		for id, title := range features {
			if _, held := missing[world][id]; !held {
				only = append(only, featureRef{World: world, ID: id, Title: title})
			}
		}
	}
	sort.Slice(only, func(i, j int) bool {
		if only[i].World != only[j].World {
			return only[i].World < only[j].World
		}
		if only[i].Title != only[j].Title {
			return only[i].Title < only[j].Title
		}
		return only[i].ID < only[j].ID
	})
	return only
}

func pairDiff(a, b *maturity.Score) []pairStability {
	type key struct{ world, source string }
	type pair struct{ donor, winner int64 }
	collect := func(from *maturity.Score) map[key]map[pair]bool {
		held := make(map[key]map[pair]bool)
		for _, line := range from.Ledger {
			at := key{line.World, line.Account.Source}
			if held[at] == nil {
				held[at] = make(map[pair]bool)
			}
			for _, matched := range line.Account.Matched {
				held[at][pair{matched.Donor, matched.Winner}] = true
			}
		}
		return held
	}
	inA, inB := collect(a), collect(b)

	keys := make(map[key]bool)
	for at := range inA {
		keys[at] = true
	}
	for at := range inB {
		keys[at] = true
	}
	ordered := make([]key, 0, len(keys))
	for at := range keys {
		ordered = append(ordered, at)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].world != ordered[j].world {
			return ordered[i].world < ordered[j].world
		}
		return ordered[i].source < ordered[j].source
	})

	var stability []pairStability
	for _, at := range ordered {
		line := pairStability{World: at.world, Source: at.source}
		for held := range inA[at] {
			if inB[at][held] {
				line.Kept++
			} else {
				line.Lost++
			}
		}
		for held := range inB[at] {
			if !inA[at][held] {
				line.Gained++
			}
		}
		stability = append(stability, line)
	}
	return stability
}

func count(name string, a, b int) deltaRow {
	return deltaRow{
		Name:  name,
		A:     strconv.Itoa(a),
		B:     strconv.Itoa(b),
		Delta: signedInt(b - a),
		Sign:  signOf(b - a),
	}
}

func megabytes(name string, a, b int64) deltaRow {
	row := deltaRow{
		Name: name,
		A:    fmt.Sprintf("%.1f MB", float64(a)/1e6),
		B:    fmt.Sprintf("%.1f MB", float64(b)/1e6),
		Sign: signOf(int(b - a)),
	}
	if row.Sign == 0 {
		row.Delta = "±0.0 MB"
	} else {
		row.Delta = fmt.Sprintf("%+.1f MB", float64(b-a)/1e6)
	}
	return row
}

// share compares two ratios in whole points, so coverage reads as a signed
// number of points rather than as two percentages a reader has to subtract.
func share(name string, aPart, aWhole, bPart, bWhole int) deltaRow {
	points := func(part, whole int) int {
		if whole == 0 {
			return 0
		}
		return part * 100 / whole
	}
	delta := points(bPart, bWhole) - points(aPart, aWhole)
	return deltaRow{
		Name:  name,
		A:     percent(aPart, aWhole),
		B:     percent(bPart, bWhole),
		Delta: signedInt(delta) + " pt",
		Sign:  signOf(delta),
	}
}

func percent(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return strconv.Itoa(part*100/whole) + "%"
}

func signedInt(delta int) string {
	if delta > 0 {
		return "+" + strconv.Itoa(delta)
	}
	if delta < 0 {
		return strconv.Itoa(delta)
	}
	return "±0"
}

func signOf(delta int) int {
	switch {
	case delta > 0:
		return 1
	case delta < 0:
		return -1
	}
	return 0
}
