package main

import (
	"fmt"
	"sort"
	"strconv"
)

// A diff reads two builds of one game side by side and says what moved: the
// axis figures, the pins themselves, and the stability of the merge ledgers'
// agreements. It works entirely over measurements and packed payloads already
// on disk -- comparing builds is how a policy change is validated, so the
// comparison must never depend on anything but the builds.

// buildDiff is everything that moved between build A and build B, read in
// A-to-B direction: an addition is something B has that A did not.
type buildDiff struct {
	A, B    *build
	Axes    []axisRow
	Added   []pinRef
	Removed []pinRef
	Pairs   []pairStability
}

// axisRow is one measurement in both builds with its movement, formatted for
// the page; Sign carries the direction for styling without re-parsing.
type axisRow struct {
	Name  string
	A, B  string
	Delta string
	Sign  int
}

// pinRef names one pin as the packed payload carries it.
type pinRef struct {
	Map   string
	ID    int64
	Title string
}

// pairStability is one (map, source) ledger's agreement compared across the
// two builds: pairs standing in both, pairs only the newer build made, and
// pairs the newer build gave up. A pair is the same pair only when both its
// donor and its winner agree -- a match that moved to another winner reads
// as one lost and one gained, which is exactly the event a policy diff is
// looking for.
type pairStability struct {
	Map    string
	Source string
	Kept   int
	Gained int
	Lost   int
}

// diffBuilds compares two measured builds and their packed pins.
func diffBuilds(a, b *build, pinsA, pinsB map[string]map[int64]string) *buildDiff {
	d := &buildDiff{
		A: a,
		B: b,
		Axes: []axisRow{
			axisCount("pins", a.Pins, b.Pins),
			axisCount("described", a.Described, b.Described),
			axisShare("described share", a.Described, a.Pins, b.Described, b.Pins),
			axisCount("median note chars", a.MedianLength, b.MedianLength),
			axisCount("tiles", a.TileCount, b.TileCount),
			axisBytes("unique raster", a.RasterBytes, b.RasterBytes),
			axisCount("depth", a.Depth, b.Depth),
			axisCount("tiles at depth", a.DepthTiles, b.DepthTiles),
			axisCount("categories", a.Categories, b.Categories),
			axisCount("groups", a.Groups, b.Groups),
			axisCount("text label sets", a.TextSets, b.TextSets),
			axisCount("zones", a.Zones, b.Zones),
			axisCount("zone vertices", a.Vertices, b.Vertices),
			axisCount("layers", a.Variants, b.Variants),
			axisCount("icons carried", a.IconsCarried, b.IconsCarried),
			axisShare("icon coverage", a.IconsCarried, a.IconsWanted, b.IconsCarried, b.IconsWanted),
		},
	}

	d.Added = pinsOnly(pinsB, pinsA)
	d.Removed = pinsOnly(pinsA, pinsB)
	d.Pairs = pairDiff(a, b)
	return d
}

// pinsOnly lists the pins in have that missing lacks, by map and id, in a
// stable order.
func pinsOnly(have, missing map[string]map[int64]string) []pinRef {
	var only []pinRef
	for mapSlug, pins := range have {
		for id, title := range pins {
			if _, held := missing[mapSlug][id]; !held {
				only = append(only, pinRef{Map: mapSlug, ID: id, Title: title})
			}
		}
	}
	sort.Slice(only, func(i, j int) bool {
		if only[i].Map != only[j].Map {
			return only[i].Map < only[j].Map
		}
		if only[i].Title != only[j].Title {
			return only[i].Title < only[j].Title
		}
		return only[i].ID < only[j].ID
	})
	return only
}

func pairDiff(a, b *build) []pairStability {
	type key struct{ mapSlug, source string }
	type pair struct{ donor, winner int64 }
	collect := func(from *build) map[key]map[pair]bool {
		held := make(map[key]map[pair]bool)
		for _, account := range from.Merges {
			at := key{account.Map, account.Source}
			if held[at] == nil {
				held[at] = make(map[pair]bool)
			}
			for _, matched := range account.Matched {
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
		if ordered[i].mapSlug != ordered[j].mapSlug {
			return ordered[i].mapSlug < ordered[j].mapSlug
		}
		return ordered[i].source < ordered[j].source
	})

	var stability []pairStability
	for _, at := range ordered {
		line := pairStability{Map: at.mapSlug, Source: at.source}
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

func axisCount(name string, a, b int) axisRow {
	return axisRow{
		Name:  name,
		A:     strconv.Itoa(a),
		B:     strconv.Itoa(b),
		Delta: signedInt(b - a),
		Sign:  signOf(b - a),
	}
}

func axisBytes(name string, a, b int64) axisRow {
	delta := float64(b-a) / 1e6
	row := axisRow{
		Name: name,
		A:    fmt.Sprintf("%.1f MB", float64(a)/1e6),
		B:    fmt.Sprintf("%.1f MB", float64(b)/1e6),
		Sign: signOf(int(b - a)),
	}
	if row.Sign == 0 {
		row.Delta = "±0.0 MB"
	} else {
		row.Delta = fmt.Sprintf("%+.1f MB", delta)
	}
	return row
}

// axisShare compares two ratios in whole points, so coverage that must not
// decline across builds reads as a signed number of points.
func axisShare(name string, aPart, aWhole, bPart, bWhole int) axisRow {
	points := func(part, whole int) int {
		if whole == 0 {
			return 0
		}
		return part * 100 / whole
	}
	delta := points(bPart, bWhole) - points(aPart, aWhole)
	return axisRow{
		Name:  name,
		A:     percent(aPart, aWhole),
		B:     percent(bPart, bWhole),
		Delta: signedInt(delta) + " pt",
		Sign:  signOf(delta),
	}
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
