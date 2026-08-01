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
	"bytes"
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
	nearbyFloorPx    = 48
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
	Adopted   []adoptedPin `json:"adopted,omitempty"`
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

// adoptedPin records a donor-only pin that joined one of the serving map's
// own categories: provenance for a pin the legend does not single out.
type adoptedPin struct {
	Donor int64  `json:"d"`
	Into  string `json:"into"`
}

// heldPin is a donor pin the merge did not carry, with the reason.
type heldPin struct {
	Donor  int64  `json:"d"`
	Title  string `json:"t"`
	Reason string `json:"why"`
}

// mergeAcrossSources composes every game slug's catalog entries into one
// bundle. Each source contributed what it had -- a map the game already
// draws, a map the game has never seen, icons, pins -- and the composition
// takes each contribution on its own terms: pins of a shared map resolve
// into it, a map without a counterpart joins the game whole, and the one
// bundle written per slug is the game as every source together knows it.
// The captures stay untouched in the archive, so recomposing under a better
// policy never needs anything recrawled.
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

	absorbed := make(map[int]bool)
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
			absorbed[donor] = true
		}
		sortGameMaps(games[winner].Slug, games[winner].Maps)
	}

	composed := make([]catalogGame, 0, len(games))
	for index, game := range games {
		if !absorbed[index] {
			composed = append(composed, game)
		}
	}
	return composed, nil
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
			// A map the game has never drawn is a whole contribution: it
			// joins the game as this source captured it, icons and all.
			if err := contributeMap(winner, donor, donorMap); err != nil {
				return err
			}
			fmt.Printf("merge %s: %s joins whole from %s\n",
				winner.Slug, donorMap.Slug, sourceLabelOf(donorMap, donor))
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

// contributeMap carries one source's map into the composed game untouched:
// its layers, pins, and zones as captured, and its icons brought across --
// under their own names where those are free or already hold the same bytes,
// under source-prefixed names where the composed game spells something else
// with them.
func contributeMap(winner, donor *catalogGame, donorMap *catalogMap) error {
	contributed := *donorMap
	sourceTag := slugifyLabel(sourceLabelOf(donorMap, donor))
	renamed := make(map[string]string)
	for groupIndex := range contributed.Groups {
		categories := contributed.Groups[groupIndex].Categories
		for categoryIndex := range categories {
			category := &categories[categoryIndex]
			if category.IconAsset == "" {
				continue
			}
			data, held := donor.Icons[category.IconAsset]
			if !held {
				category.IconAsset = ""
				category.IconPicture = false
				continue
			}
			name := category.IconAsset
			if replacement, seen := renamed[name]; seen {
				category.IconAsset = replacement
				continue
			}
			if winner.Icons == nil {
				winner.Icons = make(map[string][]byte)
			}
			if existing, taken := winner.Icons[name]; taken && !bytes.Equal(existing, data) {
				name = sourceTag + "--" + name
			}
			winner.Icons[name] = data
			renamed[category.IconAsset] = name
			category.IconAsset = name
		}
	}
	winner.Maps = append(winner.Maps, contributed)
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

	// A nameless neighbour matters out to where the alignment's own noise
	// reaches: two sources place the same shop apart by their residuals, so
	// the radius listens to the fit rather than assuming precision.
	nearbyRadius := math.Max(nearbyFloorPx, 2*report.P90Px)

	// What the serving map already holds, by name, token, and place.
	index := indexWinner(winner, winnerGrid)

	equivalents := categoryEquivalents[winnerGame.Slug]

	// A donor pin that stays distinct joins the serving category its own
	// category maps onto, and the ledger records the adoption; only concepts
	// the serving map does not have at all keep their donor categories, so
	// the source-named group holds nothing but what is truly source-specific.
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
			adoptive := index.categories[mappedKey]
			for _, location := range category.Locations {
				merge.DonorPins++
				x, y := affine.Apply(
					projectX(location.Longitude, donorGrid),
					projectY(location.Latitude, donorGrid),
				)
				outcome := resolvePin(location, x, y, index, mappedKey, nearbyRadius)
				switch outcome.kind {
				case pinMatched:
					index.claimed[outcome.match] = location.ID
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
					if holder, taken := index.ids[location.ID]; taken {
						return fmt.Errorf("pin id %d (%s) collides with serving pin %q",
							location.ID, location.Title, holder)
					}
					index.ids[location.ID] = location.Title
					moved := location
					moved.Longitude = unprojectLongitude(x, winnerGrid)
					moved.Latitude = unprojectLatitude(y, winnerGrid)
					moved.RegionID = nil
					moved.Shard = 0
					merge.Added++
					if adoptive != nil {
						adoptive.Locations = append(adoptive.Locations, moved)
						merge.Adopted = append(merge.Adopted, adoptedPin{
							Donor: location.ID, Into: mappedKey,
						})
					} else {
						kept.Locations = append(kept.Locations, moved)
					}
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

// winnerIndex is everything the serving map holds, arranged for resolution:
// each pin with its place, name, and name tokens; the categories by key; and
// the identifiers already spoken for.
type winnerIndex struct {
	pins       []placedPin
	byName     map[string][]int
	categories map[string]*catalogCategory
	ids        map[int64]string
	// claimed maps each serving pin already matched to the donor that
	// matched it. A place is one place: the next donor bearing the same name
	// must find its own, or say why it cannot.
	claimed map[*catalogLocation]int64
}

type placedPin struct {
	location *catalogLocation
	category string
	x, y     float64
	tokens   map[string]bool
}

func indexWinner(winner *catalogMap, grid tileGrid) *winnerIndex {
	index := &winnerIndex{
		byName:     make(map[string][]int),
		categories: make(map[string]*catalogCategory),
		ids:        make(map[int64]string),
		claimed:    make(map[*catalogLocation]int64),
	}
	for groupIndex := range winner.Groups {
		for categoryIndex := range winner.Groups[groupIndex].Categories {
			category := &winner.Groups[groupIndex].Categories[categoryIndex]
			if _, held := index.categories[category.Icon]; !held {
				index.categories[category.Icon] = category
			}
			for locationIndex := range category.Locations {
				location := &category.Locations[locationIndex]
				name := blend.NormalizeTitle(location.Title)
				index.byName[name] = append(index.byName[name], len(index.pins))
				index.pins = append(index.pins, placedPin{
					location: location,
					category: category.Icon,
					x:        projectX(location.Longitude, grid),
					y:        projectY(location.Latitude, grid),
					tokens:   tokensOf(name),
				})
				index.ids[location.ID] = location.Title
			}
		}
	}
	return index
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
// place, and so is a name one source spells inside the other's -- "Northside
// Apartment" inside "Northside, Watson Apartment" -- when the pins share a
// category. The same name far beyond the radius is a different place bearing
// it; the stretch between is left undecided. A nameless neighbour inside the
// same mapped category is likewise held rather than guessed -- proximity
// alone never merges -- and only a pin resembling nothing at all is added.
func resolvePin(
	donor catalogLocation,
	x, y float64,
	index *winnerIndex,
	mappedKey string,
	nearbyRadius float64,
) pinOutcome {
	name := blend.NormalizeTitle(donor.Title)
	donorTokens := tokensOf(name)

	var nearest *placedPin
	nearestDistance := math.Inf(1)
	alreadyClaimed := false
	consider := func(pin *placedPin, distance float64) {
		// A serving pin another donor already resolved to is one place, not
		// two: it cannot be matched again, but its nearness is remembered so
		// the refusal can say what stood in the way.
		if _, taken := index.claimed[pin.location]; taken {
			if distance <= matchRadiusPx {
				alreadyClaimed = true
			}
			return
		}
		if distance < nearestDistance {
			nearest, nearestDistance = pin, distance
		}
	}
	if name != "" {
		for _, at := range index.byName[name] {
			pin := &index.pins[at]
			consider(pin, math.Hypot(pin.x-x, pin.y-y))
		}
		// One source's name written inside the other's counts only with the
		// category agreeing: a bare "Apartment" must not roam the map for a
		// long-named cousin.
		if nearest == nil && len(donorTokens) >= 2 {
			for at := range index.pins {
				pin := &index.pins[at]
				if pin.category != mappedKey || !tokenSubset(donorTokens, pin.tokens) {
					continue
				}
				consider(pin, math.Hypot(pin.x-x, pin.y-y))
			}
		}
	}
	if nearest != nil {
		switch {
		case nearestDistance <= matchRadiusPx:
			return pinOutcome{kind: pinMatched, match: nearest.location, distance: nearestDistance}
		case nearestDistance <= separateRadiusPx:
			return pinOutcome{kind: pinHeld, reason: fmt.Sprintf(
				"named like %q %.0fpx away; too far to merge, too near to double",
				nearest.location.Title, nearestDistance)}
		}
	}
	if alreadyClaimed {
		return pinOutcome{kind: pinHeld, reason: "every nearby pin of this name is already matched"}
	}
	if _, shared := index.categories[mappedKey]; shared {
		for at := range index.pins {
			pin := &index.pins[at]
			if pin.category != mappedKey {
				continue
			}
			if math.Hypot(pin.x-x, pin.y-y) <= nearbyRadius {
				return pinOutcome{kind: pinHeld, reason: fmt.Sprintf(
					"beside %q in the same category; names disagree", pin.location.Title)}
			}
		}
	}
	return pinOutcome{kind: pinDistinct}
}

// tokenSubset reports whether one name is spelled entirely inside the other,
// either way round. The shorter side must carry at least two words: a single
// word inside a longer name -- every "Apartment" inside every apartment --
// says nothing.
func tokenSubset(a, b map[string]bool) bool {
	small, big := a, b
	if len(small) > len(big) {
		small, big = big, small
	}
	if len(small) < 2 {
		return false
	}
	for token := range small {
		if !big[token] {
			return false
		}
	}
	return true
}

func tokensOf(normalized string) map[string]bool {
	tokens := make(map[string]bool)
	start := -1
	for at, r := range normalized {
		if r != ' ' {
			if start < 0 {
				start = at
			}
			continue
		}
		if start >= 0 {
			tokens[normalized[start:at]] = true
			start = -1
		}
	}
	if start >= 0 {
		tokens[normalized[start:]] = true
	}
	return tokens
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

// mergeGate is the merge's own audit: every donor pin accounted for, every
// match one-to-one, no identifier doubled, nothing the serving map held made
// worse. It fails the build rather than writing a bundle that quietly lost
// something -- or quietly agreed too much.
func mergeGate(merge *mergedSource, winner *catalogMap) error {
	accounted := len(merge.Matched) + merge.Added + len(merge.Held) + len(merge.Rejected)
	if accounted != merge.DonorPins {
		return fmt.Errorf("merge accounts for %d of %d donor pins", accounted, merge.DonorPins)
	}
	claimed := make(map[int64]int64)
	for _, pair := range merge.Matched {
		if first, taken := claimed[pair.Winner]; taken {
			return fmt.Errorf("serving pin %d matched by donors %d and %d; a place is one place",
				pair.Winner, first, pair.Donor)
		}
		claimed[pair.Winner] = pair.Donor
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
