package main

import (
	"fmt"
	"strings"
	"testing"
)

// fixtureMergeGrid is one world-sized tile: zoom 0 puts the whole longitude
// range across a single 8192-pixel square, so fixture coordinates project
// somewhere sensible without a tile pyramid behind them.
func fixtureMergeGrid() tileGrid {
	return tileGrid{SourceZoom: 0, FirstTile: 0, TileSize: 8192, Size: 8192}
}

// fixtureLandmarks builds one point collection of twelve named pins spread
// wide enough for the alignment to stand on, with ids counted from base.
// Both sides of a merge use the same names and places, so the fit is exact
// and every donor pin resolves as matched.
func fixtureLandmarks(base int64) worldCollection {
	collection := worldCollection{
		ID: 1, Title: "Landmarks", Group: "Markers",
		Kind: kindPoint, Icon: "landmark", Visible: true,
	}
	lats := []float64{40, 10, -20}
	lngs := []float64{-120, -60, 20, 90}
	for atLat, lat := range lats {
		for atLng, lng := range lngs {
			n := int64(atLat*len(lngs) + atLng)
			collection.Features = append(collection.Features, feature{
				ID:    base + n,
				Title: fmt.Sprintf("Landmark %d", n),
				Lat:   lat,
				Lng:   lng,
			})
		}
	}
	return collection
}

// TestMergeWorldHoldsDonorShapeFeatures is the ledger's promise for a merge
// no source pair exercises yet: a donor carrying areas and paths has every
// one of them held on the record, counted in donorFeatures, and none of them
// carried into the winner.
func TestMergeWorldHoldsDonorShapeFeatures(t *testing.T) {
	winnerGame := catalogVolume{
		Slug: "hollowmere", Title: "Hollowmere",
		Worlds: []catalogWorld{{
			ID: 1, Slug: "overworld", UpdatedAt: "2026-01-02",
			Collections: []worldCollection{
				fixtureLandmarks(1),
				{ID: 90, Key: "regions", Title: "Regions", Kind: kindArea,
					Features: []feature{{ID: 100, Title: "Old Town"}}},
			},
			Merged: []mergedSource{{
				Source: "MapGenie", Slug: "mapgenie", Origin: true,
				DonorFeatures: featureCounts{Point: 12, Area: 1},
			}},
		}},
	}
	donorGame := catalogVolume{
		Slug: "hollowmere", Title: "Hollowmere",
		Worlds: []catalogWorld{{
			ID: 2, Slug: "overworld", UpdatedAt: "2026-01-01",
			Collections: []worldCollection{
				fixtureLandmarks(201),
				{ID: 91, Key: "basins", Title: "Basins", Kind: kindArea,
					Features: []feature{
						{ID: 301, Title: "North Basin"},
						{ID: 302, Title: "South Basin"},
					}},
				{ID: 92, Key: "races", Title: "Mill Races", Kind: kindPath,
					Features: []feature{{ID: 303, Title: "Mill Race"}}},
			},
			Merged: []mergedSource{{
				Source: "IGN Wiki", Slug: "ign-wiki", Origin: true,
				DonorFeatures: featureCounts{Point: 12, Path: 1, Area: 2},
			}},
		}},
	}

	winner := &winnerGame.Worlds[0]
	err := mergeWorld(&winnerGame, winner, &donorGame, &donorGame.Worlds[0], fixtureMergeGrid())
	if err != nil {
		t.Fatal(err)
	}
	if len(winner.Merged) != 2 {
		t.Fatalf("winner carries %d accounts, want origin plus the merge", len(winner.Merged))
	}
	account := winner.Merged[1]
	if account.DonorFeatures != (featureCounts{Point: 12, Path: 1, Area: 2}) {
		t.Errorf("donor features counted as %+v", account.DonorFeatures)
	}
	if len(account.Matched) != 12 || account.Added != 0 || account.AddedShapes != 0 {
		t.Errorf("points resolved as %d matched, %d added, %d added shapes; want 12, 0, 0",
			len(account.Matched), account.Added, account.AddedShapes)
	}
	if len(account.Held) != 3 {
		t.Fatalf("held ledger carries %d entries, want one per donor shape: %+v",
			len(account.Held), account.Held)
	}
	wantHeld := []heldPin{
		{Donor: 301, Title: "North Basin", Reason: heldShapeReason},
		{Donor: 302, Title: "South Basin", Reason: heldShapeReason},
		{Donor: 303, Title: "Mill Race", Reason: heldShapeReason},
	}
	for at, want := range wantHeld {
		if account.Held[at] != want {
			t.Errorf("held[%d] = %+v, want %+v", at, account.Held[at], want)
		}
	}
	// The shapes are held, not carried: the winner keeps exactly the
	// collections it opened with.
	if len(winner.Collections) != 2 {
		t.Errorf("winner grew to %d collections; donor shapes must not ride in", len(winner.Collections))
	}
}

// gateWorld builds the smallest winner a gate case needs: the given
// collections and one origin account claiming the given counts.
func gateWorld(counts featureCounts, collections ...worldCollection) *catalogWorld {
	return &catalogWorld{
		Slug:        "overworld",
		Collections: collections,
		Merged: []mergedSource{{
			Source: "MapGenie", Origin: true, DonorFeatures: counts,
		}},
	}
}

func TestMergeGateAccountsPerKind(t *testing.T) {
	cases := []struct {
		name   string
		merge  *mergedSource
		winner *catalogWorld
		want   string
	}{
		{
			name:   "every donor shape must be held",
			merge:  &mergedSource{DonorFeatures: featureCounts{Area: 1}},
			winner: gateWorld(featureCounts{}),
			want:   "holds 0 shape features of the 1",
		},
		{
			name: "a held point never settles the shape account",
			merge: &mergedSource{DonorFeatures: featureCounts{Point: 1, Area: 1},
				Held: []heldPin{{Donor: 5, Title: "Well", Reason: "named like another"}}},
			winner: gateWorld(featureCounts{}),
			want:   "holds 0 shape features of the 1",
		},
		{
			name:   "added shapes are reserved at zero",
			merge:  &mergedSource{AddedShapes: 1},
			winner: gateWorld(featureCounts{}),
			want:   "no shape feature merges yet",
		},
		{
			name:  "one id space across kinds",
			merge: &mergedSource{},
			winner: gateWorld(featureCounts{Point: 1, Area: 1},
				worldCollection{Kind: kindPoint, Features: []feature{{ID: 7, Title: "Gate"}}},
				worldCollection{Kind: kindArea, Features: []feature{{ID: 7, Title: "Gate District"}}},
			),
			want: "feature id 7 held by both",
		},
		{
			name:  "the ledger answers for the points the world holds",
			merge: &mergedSource{},
			winner: gateWorld(featureCounts{},
				worldCollection{Kind: kindPoint, Features: []feature{{ID: 1, Title: "Gate"}}},
			),
			want: "holds 1 points but its ledger claims 0",
		},
		{
			name:  "the ledger answers for the shapes the world holds",
			merge: &mergedSource{},
			winner: gateWorld(featureCounts{},
				worldCollection{Kind: kindArea, Features: []feature{{ID: 9, Title: "Old Town"}}},
			),
			want: "holds 0 paths and 1 areas but its ledger claims 0 and 0",
		},
		{
			name: "a consistent account of every kind passes",
			merge: &mergedSource{DonorFeatures: featureCounts{Point: 1, Path: 1, Area: 1},
				Held: []heldPin{
					{Donor: 5, Title: "Well", Reason: "named like another"},
					{Donor: 6, Title: "Race", Reason: heldShapeReason},
					{Donor: 7, Title: "Basin", Reason: heldShapeReason},
				}},
			winner: gateWorld(featureCounts{Point: 1, Path: 1, Area: 1},
				worldCollection{Kind: kindPoint, Features: []feature{{ID: 1, Title: "Gate"}}},
				worldCollection{Kind: kindPath, Features: []feature{{ID: 2, Title: "Canal"}}},
				worldCollection{Kind: kindArea, Features: []feature{{ID: 3, Title: "Old Town"}}},
			),
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mergeGate(c.merge, c.winner)
			if c.want == "" {
				if err != nil {
					t.Fatalf("gate refused a consistent account: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("gate said %v, want it to say %q", err, c.want)
			}
		})
	}
}
