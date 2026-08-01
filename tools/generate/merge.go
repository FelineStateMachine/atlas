package main

// Merging keeps every source's pins. When two sources capture one game, the
// newest capture's bundle is the one the registry serves, and until now the
// other source's pins simply rode along unseen in a shadowed bundle. Now the
// serving game absorbs them: each other source's map is aligned into the
// serving map's world by the transformation their shared named places
// determine -- the same alignment the raster variants stand on -- and every
// pin is resolved against what the serving map already holds. A pin both
// sources place is recorded as the same place, never drawn twice; a pin only
// the other source knows is added under its own source-named group, icons
// and all; a pin the resolution cannot be sure about is held back and said
// so. The payload carries the whole account -- what matched, what was added,
// what was held -- so the merge is a documented derivation over immutable
// captures, not an edit.

import (
	"fmt"
	"math"
	"sort"

	"github.com/FelineStateMachine/atlas/internal/blend"
)

// Resolution distances, in world pixels of an 8192-pixel square. A confirmed
// match needs the same name near where the alignment predicts; farther than
// twice the radius the same name is treated as a different place bearing it.
// Between the two, and for nameless proximity inside a shared category, the
// resolution declines to decide: a pin held back with its reason recorded
// costs a curiosity, where a wrong merge costs a place.
const (
	matchRadiusPx    = 160
	separateRadiusPx = 320
	nearbyRadiusPx   = 48
)

// categoryEquivalents maps one source's category slugs onto another's where
// the concepts are known to be the same thing. Slugs equal after
// normalization pair automatically; everything else stays source-specific
// rather than being guessed together.
var categoryEquivalents = map[string]map[string]string{
	"cyberpunk-2077": {
		"ripper-doc":     "ripperdoc",
		"tarot-card":     "tarot-graffiti",
		"gun-shop":       "weapon-shop",
		"clothes-shop":   "clothing-vendor",
		"medicine-shop":  "medpoint",
		"melee-shop":     "melee-vendor",
		"netrunner-shop": "netrunner",
	},
}

// mergedSource is one source's account of its merge, carried in the map's
// payload: the alignment it stood on, what became of every donor pin, and
// the ledger a later pass -- or a curious reader -- can audit the decisions
// by.
type mergedSource struct {
	Source    string       `json:"source"`
	DonorPins int          `json:"donorPins"`
	Matched   []mergedPair `json:"matched,omitempty"`
	Added     int          `json:"added"`
	Held      []heldPin    `json:"held,omitempty"`
	Rejected  []heldPin    `json:"rejected,omitempty"`
	Alignment string       `json:"alignment"`
}

// mergedPair records one place both sources pin: the donor pin, the serving
// pin it resolved to, and how far apart the alignment put them.
type mergedPair struct {
	Donor      int64 `json:"d"`
	Winner     int64 `json:"w"`
	DistancePx int   `json:"px"`
	// Enriched marks a pair whose serving pin had nothing to say and took
	// the donor's description.
	Enriched bool `json:"e,omitempty"`
}

// heldPin is a donor pin the merge did not carry, with the reason.
type heldPin struct {
	Donor  int64  `json:"d"`
	Title  string `json:"t"`
	Reason string `json:"why"`
}

// mergeAcrossSources folds every game slug's catalog entries into one served
// account: the newest capture absorbs the others' pins, and every source
// still emits its own bundle untouched, so nothing is lost however the merge
// policy evolves.
func mergeAcrossSources(games []catalogGame, shared tileGrid) ([]catalogGame, error) {
	bySlug := make(map[string][]int)
	var order []string
	for index, game := range games {
		if _, seen := bySlug[game.Slug]; !seen {
			order = append(order, game.Slug)
		}
		bySlug[game.Slug] = append(bySlug[game.Slug], index)
	}
	sort.Strings(order)

	for _, slug := range order {
		group := bySlug[slug]
		if len(group) < 2 {
			continue
		}
		winner := group[0]
		for _, candidate := range group[1:] {
			if newestCapture(games[candidate]) > newestCapture(games[winner]) {
				winner = candidate
			}
		}
		for _, donor := range group {
			if donor == winner {
				continue
			}
			if err := mergeGame(&games[winner], &games[donor], shared); err != nil {
				return nil, fmt.Errorf("merge %s: %w", slug, err)
			}
		}
	}
	return games, nil
}

func newestCapture(game catalogGame) string {
	newest := ""
	for _, m := range game.Maps {
		if m.UpdatedAt > newest {
			newest = m.UpdatedAt
		}
	}
	return newest
}

func mergeGame(winner, donor *catalogGame, shared tileGrid) error {
	winnerMaps := make(map[string]*catalogMap, len(winner.Maps))
	for index := range winner.Maps {
		winnerMaps[winner.Maps[index].Slug] = &winner.Maps[index]
	}
	for index := range donor.Maps {
		donorMap := &donor.Maps[index]
		target := winnerMaps[donorMap.Slug]
		if target == nil {
			fmt.Printf("merge %s: %s has no counterpart in the serving capture; its pins stay in their own bundle\n",
				winner.Slug, donorMap.Slug)
			continue
		}
		if target.Parent != "" || donorMap.Parent != "" {
			fmt.Printf("merge %s: %s is a split sheet; merging across shards is not attempted\n",
				winner.Slug, donorMap.Slug)
			continue
		}
		if err := mergeMap(winner, target, donor, donorMap, shared); err != nil {
			return err
		}
	}
	return nil
}

func mergeMap(
	winnerGame *catalogGame,
	winner *catalogMap,
	donorGame *catalogGame,
	donor *catalogMap,
	shared tileGrid,
) error {
	winnerGrid := gridOf(winner, shared)
	donorGrid := gridOf(donor, shared)

	affine, report, err := blend.Fit(anchorsOfMap(donor, donorGrid), anchorsOfMap(winner, winnerGrid))
	if err != nil {
		fmt.Printf("merge %s/%s: pins stay apart: %v\n", winnerGame.Slug, winner.Slug, err)
		return nil
	}

	sourceLabel := sourceLabelOf(donor, donorGame)
	merge := &mergedSource{Source: sourceLabel, Alignment: report.String()}

	// What the serving map already holds, by name and by place.
	winnerByName := make(map[string][]*catalogLocation)
	var winnerPlaced []placedPin
	winnerIDs := make(map[int64]string)
	winnerCategoryByKey := make(map[string]bool)
	for groupIndex := range winner.Groups {
		for categoryIndex := range winner.Groups[groupIndex].Categories {
			category := &winner.Groups[groupIndex].Categories[categoryIndex]
			winnerCategoryByKey[category.Icon] = true
			for locationIndex := range category.Locations {
				location := &category.Locations[locationIndex]
				name := blend.NormalizeTitle(location.Title)
				winnerByName[name] = append(winnerByName[name], location)
				winnerPlaced = append(winnerPlaced, placedPin{
					location: location,
					category: category.Icon,
					x:        projectX(location.Longitude, winnerGrid),
					y:        projectY(location.Latitude, winnerGrid),
				})
				winnerIDs[location.ID] = location.Title
			}
		}
	}

	equivalents := categoryEquivalents[winnerGame.Slug]

	// Donor categories survive with the pins that stay distinct; everything
	// else lands in the ledger.
	var keptCategories []catalogCategory
	for _, group := range donor.Groups {
		for _, category := range group.Categories {
			kept := category
			kept.Locations = nil
			donorKey := category.Icon
			mappedKey := donorKey
			if equivalent, ok := equivalents[donorKey]; ok {
				mappedKey = equivalent
			}
			for _, location := range category.Locations {
				merge.DonorPins++
				x, y := affine.Apply(
					projectX(location.Longitude, donorGrid),
					projectY(location.Latitude, donorGrid),
				)
				outcome := resolvePin(location, x, y, winnerByName, winnerPlaced, mappedKey, winnerCategoryByKey[mappedKey])
				switch outcome.kind {
				case pinMatched:
					pair := mergedPair{
						Donor:      location.ID,
						Winner:     outcome.match.ID,
						DistancePx: int(outcome.distance + 0.5),
					}
					// Field-level: a serving pin with nothing to say takes
					// the donor's words rather than silencing them.
					if outcome.match.Description == "" && location.Description != "" {
						outcome.match.Description = location.Description
						pair.Enriched = true
					}
					merge.Matched = append(merge.Matched, pair)
				case pinHeld:
					merge.Held = append(merge.Held, heldPin{
						Donor: location.ID, Title: location.Title, Reason: outcome.reason,
					})
				case pinDistinct:
					if x < 0 || y < 0 || x > float64(shared.Size) || y > float64(shared.Size) {
						merge.Rejected = append(merge.Rejected, heldPin{
							Donor: location.ID, Title: location.Title,
							Reason: "outside the shared world",
						})
						continue
					}
					if holder, taken := winnerIDs[location.ID]; taken {
						return fmt.Errorf("pin id %d (%s) collides with serving pin %q",
							location.ID, location.Title, holder)
					}
					winnerIDs[location.ID] = location.Title
					moved := location
					moved.Longitude = unprojectLongitude(x, winnerGrid)
					moved.Latitude = unprojectLatitude(y, winnerGrid)
					moved.RegionID = nil
					moved.Shard = 0
					kept.Locations = append(kept.Locations, moved)
					merge.Added++
				}
			}
			if len(kept.Locations) == 0 {
				continue
			}
			if err := carryIcon(winnerGame, donorGame, &kept, sourceLabel); err != nil {
				return err
			}
			keptCategories = append(keptCategories, kept)
		}
	}

	if len(keptCategories) > 0 {
		winner.Groups = append(winner.Groups, catalogGroup{
			ID:         mergedGroupID(winner),
			Title:      sourceLabel,
			Categories: keptCategories,
		})
	}
	winner.Merged = append(winner.Merged, *merge)
	winner.PinCount += merge.Added
	// The merged account is as fresh as its freshest ingredient, so a newer
	// donor capture re-versions the merged bundle even when the serving
	// capture stood still.
	if donor.UpdatedAt > winner.UpdatedAt {
		winner.UpdatedAt = donor.UpdatedAt
	}

	if err := mergeGate(merge, winner); err != nil {
		return err
	}
	fmt.Printf("merge %s/%s: %s: %d donor pins → %d matched (%d enriched) · %d added · %d held · %d outside\n",
		winnerGame.Slug, winner.Slug, sourceLabel, merge.DonorPins, len(merge.Matched),
		enrichedCount(merge), merge.Added, len(merge.Held), len(merge.Rejected))
	return nil
}

type placedPin struct {
	location *catalogLocation
	category string
	x, y     float64
}

type pinOutcome struct {
	kind     int
	match    *catalogLocation
	distance float64
	reason   string
}

const (
	pinDistinct = iota
	pinMatched
	pinHeld
)

// resolvePin decides what one donor pin is, against everything the serving
// map holds. The same name near where the alignment predicts is the same
// place; the same name far beyond it is a different place bearing it; the
// stretch between is left undecided. A nameless neighbour inside the same
// mapped category is likewise held rather than guessed -- proximity alone
// never merges -- and only a pin resembling nothing at all is added.
func resolvePin(
	donor catalogLocation,
	x, y float64,
	byName map[string][]*catalogLocation,
	placed []placedPin,
	mappedKey string,
	categoryShared bool,
) pinOutcome {
	name := blend.NormalizeTitle(donor.Title)
	if name != "" {
		var nearest *catalogLocation
		nearestDistance := 0.0
		for _, candidate := range byName[name] {
			distance := pinDistance(x, y, candidate, placed)
			if nearest == nil || distance < nearestDistance {
				nearest, nearestDistance = candidate, distance
			}
		}
		if nearest != nil {
			switch {
			case nearestDistance <= matchRadiusPx:
				return pinOutcome{kind: pinMatched, match: nearest, distance: nearestDistance}
			case nearestDistance <= separateRadiusPx:
				return pinOutcome{kind: pinHeld, reason: fmt.Sprintf(
					"same name %.0fpx away; too far to merge, too near to double", nearestDistance)}
			}
		}
	}
	if categoryShared {
		for _, candidate := range placed {
			if candidate.category != mappedKey {
				continue
			}
			dx, dy := candidate.x-x, candidate.y-y
			if dx*dx+dy*dy <= nearbyRadiusPx*nearbyRadiusPx {
				return pinOutcome{kind: pinHeld, reason: fmt.Sprintf(
					"beside %q in the same category; names disagree", candidate.location.Title)}
			}
		}
	}
	return pinOutcome{kind: pinDistinct}
}

// pinDistance finds how far a point sits from a serving pin. The placed list
// is small enough to scan; identity is by pointer.
func pinDistance(x, y float64, location *catalogLocation, placed []placedPin) float64 {
	for _, candidate := range placed {
		if candidate.location == location {
			return math.Hypot(candidate.x-x, candidate.y-y)
		}
	}
	return math.Inf(1)
}

// carryIcon brings a merged category's icon across from the donor's archive
// under a source-prefixed name, so it cannot displace a serving icon that
// shares its key.
func carryIcon(winnerGame, donorGame *catalogGame, category *catalogCategory, sourceLabel string) error {
	if category.IconAsset == "" {
		return nil
	}
	data, held := donorGame.Icons[category.IconAsset]
	if !held {
		category.IconAsset = ""
		category.IconPicture = false
		return nil
	}
	carried := slugifyLabel(sourceLabel) + "--" + category.IconAsset
	if winnerGame.Icons == nil {
		winnerGame.Icons = make(map[string][]byte)
	}
	winnerGame.Icons[carried] = data
	category.IconAsset = carried
	return nil
}

// mergedGroupID numbers the source's group away from every id the map
// already uses.
func mergedGroupID(winner *catalogMap) int64 {
	used := make(map[int64]bool)
	for _, group := range winner.Groups {
		used[group.ID] = true
	}
	id := int64(1)
	for used[id] {
		id++
	}
	return id
}

func sourceLabelOf(donor *catalogMap, donorGame *catalogGame) string {
	for _, v := range donor.Variants {
		if v.Name != "" && v.Name != "Default" {
			return v.Name
		}
	}
	return donorGame.Title
}

func slugifyLabel(label string) string {
	out := make([]rune, 0, len(label))
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+'a'-'A')
		default:
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

func gridOf(m *catalogMap, shared tileGrid) tileGrid {
	if m.Grid == nil {
		return shared
	}
	return tileGrid{
		SourceZoom: m.Grid.SourceZoom,
		FirstTile:  m.Grid.FirstTile,
		TileSize:   shared.TileSize,
		Size:       shared.Size,
	}
}

func anchorsOfMap(m *catalogMap, grid tileGrid) []blend.Anchor {
	var anchors []blend.Anchor
	for _, group := range m.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				anchors = append(anchors, blend.Anchor{
					Title: location.Title,
					X:     projectX(location.Longitude, grid),
					Y:     projectY(location.Latitude, grid),
				})
			}
		}
	}
	return anchors
}

func enrichedCount(merge *mergedSource) int {
	count := 0
	for _, pair := range merge.Matched {
		if pair.Enriched {
			count++
		}
	}
	return count
}

// mergeGate is the merge's own audit: every donor pin accounted for, no
// identifier doubled, nothing the serving map held made worse. It fails the
// build rather than writing a bundle that quietly lost something.
func mergeGate(merge *mergedSource, winner *catalogMap) error {
	accounted := len(merge.Matched) + merge.Added + len(merge.Held) + len(merge.Rejected)
	if accounted != merge.DonorPins {
		return fmt.Errorf("merge accounts for %d of %d donor pins", accounted, merge.DonorPins)
	}
	seen := make(map[int64]string)
	counted := 0
	for _, group := range winner.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				if holder, taken := seen[location.ID]; taken {
					return fmt.Errorf("pin id %d held by both %q and %q", location.ID, holder, location.Title)
				}
				seen[location.ID] = location.Title
				counted++
			}
		}
	}
	if counted != winner.PinCount {
		return fmt.Errorf("map holds %d pins but claims %d", counted, winner.PinCount)
	}
	return nil
}
