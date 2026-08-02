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
	"github.com/FelineStateMachine/atlas/internal/semconv"
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

// categoryEquivalents names the shared concept where two sources spell one
// category differently, per game. It is applied by speakConventions, which
// writes the shared name onto each category as atlas.collection.key -- so the
// payloads themselves carry the merge identity, and the merge below reads
// only the attribute. Slugs equal after normalization pair automatically;
// everything else stays source-specific rather than being guessed together.
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

// categoryKey is a category's merge identity: the declared shared name when
// the payload carries one, its icon key otherwise -- which is today's
// behavior, named.
func categoryKey(icon string, attrs map[string]string) string {
	if key := attrs[semconv.KeyCollectionKey]; key != "" {
		return key
	}
	return icon
}

// attributeMergePolicy says, attribute by attribute, which side of a matched
// pin wins. servingWins is the default for anything unlisted; donorFillsEmpty
// is the enrichment rule the description pioneered -- the serving side keeps
// what it has and takes only what it lacks. Every take is ledgered by key,
// so the payload accounts for its own composition.
type attrRule int

const (
	servingWins attrRule = iota
	donorFillsEmpty
)

var attributeMergePolicy = map[string]attrRule{
	semconv.KeyNoteText: donorFillsEmpty,
	semconv.KeyGeoLat:   donorFillsEmpty,
	semconv.KeyGeoLon:   donorFillsEmpty,
	semconv.KeyIconStd:  donorFillsEmpty,
}

// mergedSource is one source's account of its merge, carried in the map's
// payload: the alignment it stood on, what became of every donor pin, and
// the ledger a later pass -- or a curious reader -- can audit the decisions
// by.
type mergedSource struct {
	Source string `json:"source"`
	// Slug is the source's canonical name, the one the workbench's registry
	// speaks, so ledgers and plugin cards agree without translation.
	Slug string `json:"slug,omitempty"`
	// Origin marks the account of the source the map itself came from, so a
	// single-source map still says where it is from and a composed map's
	// unledgered pins have somewhere to answer to. DonorFeatures on an
	// origin account is simply the map's own tally at composition.
	Origin bool `json:"origin,omitempty"`
	// DonorFeatures is the donor's whole offering counted per kind. The
	// donorPins spelling it stood beside died with the v2 wire.
	DonorFeatures featureCounts `json:"donorFeatures"`
	Matched       []mergedPair  `json:"matched,omitempty"`
	Added         int           `json:"added"`
	// AddedShapes is reserved: no shape feature merges yet, so it is always
	// zero, but the key holds the ledger's place for the day one does.
	AddedShapes int            `json:"addedShapes,omitempty"`
	Adopted     []adoptedPin   `json:"adopted,omitempty"`
	Held        []heldPin      `json:"held,omitempty"`
	Rejected    []heldPin      `json:"rejected,omitempty"`
	Enriched    []categoryTake `json:"enrichedCategories,omitempty"`
	Alignment   string         `json:"alignment,omitempty"`
}

// featureCounts counts features by kind: the three dimensionalities a
// collection may declare, as the ledger speaks of them.
type featureCounts struct {
	Point int `json:"point"`
	Path  int `json:"path"`
	Area  int `json:"area"`
}

// heldShapeReason is the one reason a donor shape feature is held: matching
// is point-only for now, so every donor path and area goes on the record
// instead of vanishing. The gate reads the reason back off the ledger to
// tell shape holds from point holds.
const heldShapeReason = "shape features do not merge yet"

// categoryTake records one attribute a serving category took from the
// donor's counterpart: which category, which key.
type categoryTake struct {
	Category string `json:"cat"`
	Key      string `json:"k"`
}

// canonicalSourceSlug maps what the archive records -- crawler tags, with
// emptiness meaning the founding source -- onto the slugs the workbench
// registry declares, which are the one vocabulary sources are named in.
func canonicalSourceSlug(source string) string {
	switch source {
	case "", "mapgenie":
		return "mapgenie"
	case "ign":
		return "ign-wiki"
	}
	return source
}

// sourceDisplayLabel is how a source's canonical slug reads on a card or a
// ledger.
func sourceDisplayLabel(source string) string {
	switch canonicalSourceSlug(source) {
	case "mapgenie":
		return "MapGenie"
	case "ign-wiki":
		return "IGN Wiki"
	case "piggyback":
		return "Piggyback"
	case "nasa-trek":
		return "NASA Trek"
	case "arcgis-hub":
		return "ArcGIS Open Data"
	}
	return source
}

// mergedPair records one place both sources pin: the donor pin, the serving
// pin it resolved to, how far apart the alignment put them, and every
// attribute the serving pin took from the donor, by key.
type mergedPair struct {
	Donor      int64 `json:"d"`
	Winner     int64 `json:"w"`
	DistancePx int   `json:"px"`
	// Enriched marks a pair whose serving pin had nothing to say and took
	// the donor's description: derived from Took, kept for readers of the
	// older spelling.
	Enriched bool     `json:"e,omitempty"`
	Took     []string `json:"took,omitempty"`
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
func mergeAcrossSources(games []catalogVolume, shared tileGrid) ([]catalogVolume, error) {
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
			if err := mergeVolume(&games[winner], &games[donor], shared); err != nil {
				return nil, fmt.Errorf("merge %s: %w", slug, err)
			}
			absorbed[donor] = true
		}
		sortVolumeWorlds(games[winner].Slug, games[winner].Worlds)
	}

	composed := make([]catalogVolume, 0, len(games))
	for index, game := range games {
		if !absorbed[index] {
			composed = append(composed, game)
		}
	}
	return composed, nil
}

func newestCapture(game catalogVolume) string {
	newest := ""
	for _, m := range game.Worlds {
		if m.UpdatedAt > newest {
			newest = m.UpdatedAt
		}
	}
	return newest
}

func mergeVolume(winner, donor *catalogVolume, shared tileGrid) error {
	winnerMaps := make(map[string]*catalogWorld, len(winner.Worlds))
	for index := range winner.Worlds {
		winnerMaps[winner.Worlds[index].Slug] = &winner.Worlds[index]
	}
	for index := range donor.Worlds {
		donorMap := &donor.Worlds[index]
		target := winnerMaps[donorMap.Slug]
		if target == nil {
			// Sources do not divide the world the same way: one source's
			// separate map may be ground another source draws inside a
			// larger sheet, in its own projection. A shared slug is only
			// the cheapest evidence of shared ground -- the real test is
			// whether the places both name determine a transformation, so
			// before a map is taken as new, it is tried against every map
			// the game already draws.
			target = overlappingWorld(winner, donorMap, shared)
		}
		if target == nil {
			// Nothing the game draws pictures this ground: the map joins
			// whole, as this source captured it, icons and all.
			if err := contributeWorld(winner, donor, donorMap); err != nil {
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
		if err := mergeWorld(winner, target, donor, donorMap, shared); err != nil {
			return err
		}
	}
	return nil
}

// overlappingWorld finds the winner map that pictures the same ground as a
// donor map under another slug, if any does: the one whose named places fit
// the donor's through an affine that closes. The fit is the compatibility
// test the differing projections make necessary -- coordinates cannot be
// compared, but the places can.
func overlappingWorld(winner *catalogVolume, donorMap *catalogWorld, shared tileGrid) *catalogWorld {
	donorAnchors := anchorsOfWorld(donorMap, gridOf(donorMap, shared))
	var best *catalogWorld
	bestAnchors := 0
	for index := range winner.Worlds {
		candidate := &winner.Worlds[index]
		if candidate.Parent != "" {
			continue
		}
		_, report, err := blend.Fit(donorAnchors, anchorsOfWorld(candidate, gridOf(candidate, shared)))
		if err != nil {
			continue
		}
		if report.Anchors > bestAnchors {
			best, bestAnchors = candidate, report.Anchors
		}
	}
	if best != nil {
		fmt.Printf("merge %s: %s pictures ground %s already draws (%d shared names)\n",
			winner.Slug, donorMap.Slug, best.Slug, bestAnchors)
	}
	return best
}

// contributeWorld carries one source's map into the composed game untouched:
// its layers, pins, and zones as captured, and its icons brought across --
// under their own names where those are free or already hold the same bytes,
// under source-prefixed names where the composed game spells something else
// with them.
func contributeWorld(winner, donor *catalogVolume, donorMap *catalogWorld) error {
	contributed := *donorMap
	sourceTag := slugifyLabel(sourceLabelOf(donorMap, donor))
	renamed := make(map[string]string)
	for collectionIndex := range contributed.Collections {
		collection := &contributed.Collections[collectionIndex]
		if collection.IconAsset == "" {
			continue
		}
		data, held := donor.Icons[collection.IconAsset]
		if !held {
			collection.IconAsset = ""
			collection.IconPicture = false
			continue
		}
		name := collection.IconAsset
		if replacement, seen := renamed[name]; seen {
			collection.IconAsset = replacement
			continue
		}
		if winner.Icons == nil {
			winner.Icons = make(map[string][]byte)
		}
		if existing, taken := winner.Icons[name]; taken && !bytes.Equal(existing, data) {
			name = sourceTag + "--" + name
		}
		winner.Icons[name] = data
		renamed[collection.IconAsset] = name
		collection.IconAsset = name
	}
	winner.Worlds = append(winner.Worlds, contributed)
	return nil
}

func mergeWorld(
	winnerGame *catalogVolume,
	winner *catalogWorld,
	donorGame *catalogVolume,
	donor *catalogWorld,
	shared tileGrid,
) error {
	winnerGrid := gridOf(winner, shared)
	donorGrid := gridOf(donor, shared)

	affine, report, err := blend.Fit(anchorsOfWorld(donor, donorGrid), anchorsOfWorld(winner, winnerGrid))
	if err != nil {
		fmt.Printf("merge %s/%s: pins stay apart: %v\n", winnerGame.Slug, winner.Slug, err)
		return nil
	}

	sourceLabel := sourceLabelOf(donor, donorGame)
	merge := &mergedSource{
		Source:    sourceLabel,
		Slug:      sourceSlugOf(donor),
		Alignment: report.String(),
	}

	// A nameless neighbour matters out to where the alignment's own noise
	// reaches: two sources place the same shop apart by their residuals, so
	// the radius listens to the fit rather than assuming precision.
	nearbyRadius := math.Max(nearbyFloorPx, 2*report.P90Px)

	// What the serving map already holds, by name, token, and place.
	index := indexWinner(winner, winnerGrid)

	// A donor pin that stays distinct joins the serving category its own
	// category maps onto, and the ledger records the adoption; only concepts
	// the serving map does not have at all keep their donor categories, so
	// the source-named group holds nothing but what is truly source-specific.
	// Categories meet under their declared merge identity -- the
	// atlas.collection.key their payloads carry, their icon key otherwise --
	// so the equivalence curation lives in the payloads, not here.
	var keptCollections []worldCollection
	for _, donorCollection := range donor.Collections {
		if donorCollection.Kind != kindPoint {
			// Matching is point-only, but the ledger is not: every donor
			// path and area feature is held on the record, one line each
			// with its title, rather than dropped without a word.
			for _, shape := range donorCollection.Features {
				if donorCollection.Kind == kindPath {
					merge.DonorFeatures.Path++
				} else {
					merge.DonorFeatures.Area++
				}
				merge.Held = append(merge.Held, heldPin{
					Donor: shape.ID, Title: shape.Title, Reason: heldShapeReason,
				})
			}
			continue
		}
		kept := donorCollection
		kept.Features = nil
		mappedKey := categoryKey(donorCollection.Icon, donorCollection.Attrs)
		adoptive := index.categories[mappedKey]
		// Attribute-level resolution at the category: a serving category
		// with no artwork of any kind takes the donor's standard-icon
		// declaration, and the ledger says so.
		if adoptive != nil && attributeMergePolicy[semconv.KeyIconStd] == donorFillsEmpty &&
			adoptive.IconAsset == "" && adoptive.Attrs[semconv.KeyIconStd] == "" &&
			donorCollection.Attrs[semconv.KeyIconStd] != "" {
			adoptive.Attrs = withAttr(adoptive.Attrs, semconv.KeyIconStd,
				donorCollection.Attrs[semconv.KeyIconStd])
			merge.Enriched = append(merge.Enriched, categoryTake{
				Category: mappedKey, Key: semconv.KeyIconStd,
			})
		}
		for _, pin := range donorCollection.Features {
			merge.DonorFeatures.Point++
			x, y := affine.Apply(
				projectX(pin.Lng, donorGrid),
				projectY(pin.Lat, donorGrid),
			)
			outcome := resolvePin(pin, x, y, index, mappedKey, nearbyRadius)
			switch outcome.kind {
			case pinMatched:
				index.claimed[outcome.match] = pin.ID
				pair := mergedPair{
					Donor:      pin.ID,
					Winner:     outcome.match.ID,
					DistancePx: int(outcome.distance + 0.5),
				}
				// Attribute-level: the policy table says, key by key,
				// what a serving pin takes from its donor, and every
				// take is ledgered. The description travels under its
				// policy name, and a pin's true coordinates fill in the
				// same way words do: only where the serving side has
				// none of its own.
				if attributeMergePolicy[semconv.KeyNoteText] == donorFillsEmpty &&
					outcome.match.Description == "" && pin.Description != "" {
					outcome.match.Description = pin.Description
					pair.Enriched = true
					pair.Took = append(pair.Took, semconv.KeyNoteText)
				}
				for _, key := range []string{semconv.KeyGeoLat, semconv.KeyGeoLon} {
					if attributeMergePolicy[key] != donorFillsEmpty {
						continue
					}
					if pin.Attrs[key] == "" || outcome.match.Attrs[key] != "" {
						continue
					}
					outcome.match.Attrs = withAttr(outcome.match.Attrs, key, pin.Attrs[key])
					pair.Took = append(pair.Took, key)
				}
				merge.Matched = append(merge.Matched, pair)
			case pinHeld:
				merge.Held = append(merge.Held, heldPin{
					Donor: pin.ID, Title: pin.Title, Reason: outcome.reason,
				})
			case pinDistinct:
				if x < 0 || y < 0 || x > float64(shared.Size) || y > float64(shared.Size) {
					merge.Rejected = append(merge.Rejected, heldPin{
						Donor: pin.ID, Title: pin.Title,
						Reason: "outside the shared world",
					})
					continue
				}
				if holder, taken := index.ids[pin.ID]; taken {
					return fmt.Errorf("pin id %d (%s) collides with serving pin %q",
						pin.ID, pin.Title, holder)
				}
				index.ids[pin.ID] = pin.Title
				moved := pin
				moved.Lng = unprojectLongitude(x, winnerGrid)
				moved.Lat = unprojectLatitude(y, winnerGrid)
				moved.Member = nil
				moved.Shard = 0
				merge.Added++
				if adoptive != nil {
					adoptive.Features = append(adoptive.Features, moved)
					merge.Adopted = append(merge.Adopted, adoptedPin{
						Donor: pin.ID, Into: mappedKey,
					})
				} else {
					kept.Features = append(kept.Features, moved)
				}
			}
		}
		if len(kept.Features) == 0 {
			continue
		}
		if err := carryIcon(winnerGame, donorGame, &kept, sourceLabel); err != nil {
			return err
		}
		keptCollections = append(keptCollections, kept)
	}

	if len(keptCollections) > 0 {
		// The kept collections file under a group named for their source, so
		// the legend says where they came from without a section of its own
		// machinery.
		for index := range keptCollections {
			keptCollections[index].Group = sourceLabel
		}
		winner.Collections = append(winner.Collections, keptCollections...)
	}
	winner.Merged = append(winner.Merged, *merge)
	// The merged account is as fresh as its freshest ingredient, so a newer
	// donor capture re-versions the merged bundle even when the serving
	// capture stood still.
	if donor.UpdatedAt > winner.UpdatedAt {
		winner.UpdatedAt = donor.UpdatedAt
	}

	if err := mergeGate(merge, winner); err != nil {
		return err
	}
	donorTotal := merge.DonorFeatures.Point + merge.DonorFeatures.Path + merge.DonorFeatures.Area
	fmt.Printf("merge %s/%s: %s: %d donor features → %d matched (%d enriched) · %d added · %d held · %d outside\n",
		winnerGame.Slug, winner.Slug, sourceLabel, donorTotal, len(merge.Matched),
		enrichedCount(merge), merge.Added, len(merge.Held), len(merge.Rejected))
	return nil
}

// winnerIndex is everything the serving map holds, arranged for resolution:
// each pin with its place, name, and name tokens; the categories by key; and
// the identifiers already spoken for.
type winnerIndex struct {
	pins       []placedPin
	byName     map[string][]int
	categories map[string]*worldCollection
	ids        map[int64]string
	// claimed maps each serving pin already matched to the donor that
	// matched it. A place is one place: the next donor bearing the same name
	// must find its own, or say why it cannot.
	claimed map[*feature]int64
}

type placedPin struct {
	feature  *feature
	category string
	x, y     float64
	tokens   map[string]bool
}

func indexWinner(winner *catalogWorld, grid tileGrid) *winnerIndex {
	index := &winnerIndex{
		byName:     make(map[string][]int),
		categories: make(map[string]*worldCollection),
		ids:        make(map[int64]string),
		claimed:    make(map[*feature]int64),
	}
	for collectionIndex := range winner.Collections {
		collection := &winner.Collections[collectionIndex]
		if collection.Kind != kindPoint {
			continue
		}
		key := categoryKey(collection.Icon, collection.Attrs)
		if _, held := index.categories[key]; !held {
			index.categories[key] = collection
		}
		for pinIndex := range collection.Features {
			pin := &collection.Features[pinIndex]
			name := blend.NormalizeTitle(pin.Title)
			index.byName[name] = append(index.byName[name], len(index.pins))
			index.pins = append(index.pins, placedPin{
				feature:  pin,
				category: key,
				x:        projectX(pin.Lng, grid),
				y:        projectY(pin.Lat, grid),
				tokens:   tokensOf(name),
			})
			index.ids[pin.ID] = pin.Title
		}
	}
	return index
}

type pinOutcome struct {
	kind     int
	match    *feature
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
	donor feature,
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
		if _, taken := index.claimed[pin.feature]; taken {
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
			return pinOutcome{kind: pinMatched, match: nearest.feature, distance: nearestDistance}
		case nearestDistance <= separateRadiusPx:
			return pinOutcome{kind: pinHeld, reason: fmt.Sprintf(
				"named like %q %.0fpx away; too far to merge, too near to double",
				nearest.feature.Title, nearestDistance)}
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
					"beside %q in the same category; names disagree", pin.feature.Title)}
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
func carryIcon(winnerGame, donorGame *catalogVolume, collection *worldCollection, sourceLabel string) error {
	if collection.IconAsset == "" {
		return nil
	}
	data, held := donorGame.Icons[collection.IconAsset]
	if !held {
		collection.IconAsset = ""
		collection.IconPicture = false
		return nil
	}
	carried := slugifyLabel(sourceLabel) + "--" + collection.IconAsset
	if winnerGame.Icons == nil {
		winnerGame.Icons = make(map[string][]byte)
	}
	winnerGame.Icons[carried] = data
	collection.IconAsset = carried
	return nil
}

func sourceLabelOf(donor *catalogWorld, donorGame *catalogVolume) string {
	for _, account := range donor.Merged {
		if account.Origin {
			return account.Source
		}
	}
	for _, v := range donor.Lenses {
		if v.Name != "" && v.Name != "Default" {
			return v.Name
		}
	}
	return donorGame.Title
}

// sourceSlugOf reads the canonical slug off the donor's origin account. A
// map without one -- there should be none, every map opens its account at
// composition -- simply contributes no slug rather than a guessed one.
func sourceSlugOf(donor *catalogWorld) string {
	for _, account := range donor.Merged {
		if account.Origin {
			return account.Slug
		}
	}
	return ""
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

func gridOf(m *catalogWorld, shared tileGrid) tileGrid {
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

func anchorsOfWorld(m *catalogWorld, grid tileGrid) []blend.Anchor {
	var anchors []blend.Anchor
	for _, collection := range m.Collections {
		if collection.Kind != kindPoint {
			continue
		}
		for _, pin := range collection.Features {
			anchors = append(anchors, blend.Anchor{
				Title: pin.Title,
				X:     projectX(pin.Lng, grid),
				Y:     projectY(pin.Lat, grid),
			})
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

// mergeGate is the merge's own audit: every donor feature of every kind
// accounted for, every match one-to-one, no identifier doubled, nothing the
// serving map held made worse. It fails the build rather than writing a
// bundle that quietly lost something -- or quietly agreed too much.
func mergeGate(merge *mergedSource, winner *catalogWorld) error {
	heldPoints, heldShapes := 0, 0
	for _, held := range merge.Held {
		if held.Reason == heldShapeReason {
			heldShapes++
		} else {
			heldPoints++
		}
	}
	accounted := len(merge.Matched) + merge.Added + heldPoints + len(merge.Rejected)
	if accounted != merge.DonorFeatures.Point {
		return fmt.Errorf("merge accounts for %d of %d donor points", accounted, merge.DonorFeatures.Point)
	}
	if shapes := merge.DonorFeatures.Path + merge.DonorFeatures.Area; heldShapes != shapes {
		return fmt.Errorf("merge holds %d shape features of the %d the donor carries", heldShapes, shapes)
	}
	if merge.AddedShapes != 0 {
		return fmt.Errorf("merge claims %d added shapes; no shape feature merges yet", merge.AddedShapes)
	}
	claimed := make(map[int64]int64)
	for _, pair := range merge.Matched {
		if first, taken := claimed[pair.Winner]; taken {
			return fmt.Errorf("serving pin %d matched by donors %d and %d; a place is one place",
				pair.Winner, first, pair.Donor)
		}
		claimed[pair.Winner] = pair.Donor
		// Every take answers to the policy table and the registry: a key
		// nobody registered has no business in a ledger, and the older
		// enriched flag must say exactly what the takes say.
		tookNote := false
		for _, key := range pair.Took {
			if key == semconv.KeyNoteText {
				tookNote = true
				continue
			}
			if entity, known := semconv.EntityOf(key); !known || entity != semconv.EntityFeature {
				return fmt.Errorf("pair %d took %q, which no pin may carry", pair.Donor, key)
			}
		}
		if pair.Enriched != tookNote {
			return fmt.Errorf("pair %d says enriched=%t but its takes say %t",
				pair.Donor, pair.Enriched, tookNote)
		}
	}
	for _, take := range merge.Enriched {
		if entity, known := semconv.EntityOf(take.Key); !known || entity != semconv.EntityCollection {
			return fmt.Errorf("category %q took %q, which no category may carry", take.Category, take.Key)
		}
	}
	// One identifier space for every kind: a point and an area may not share
	// an id any more than two points may.
	seen := make(map[int64]string)
	var counted featureCounts
	for _, collection := range winner.Collections {
		for _, held := range collection.Features {
			if holder, taken := seen[held.ID]; taken {
				return fmt.Errorf("feature id %d held by both %q and %q", held.ID, holder, held.Title)
			}
			seen[held.ID] = held.Title
			switch collection.Kind {
			case kindPoint:
				counted.Point++
			case kindPath:
				counted.Path++
			default:
				counted.Area++
			}
		}
	}
	// The counts the map claims are no longer fields to drift; they are the
	// sums of its own ledger -- what it opened with, plus what every merge
	// says it added -- and the map must actually hold that many of each kind.
	// Held shapes stay the donor's, so only the origin account speaks for the
	// shapes the map draws.
	var ledgered featureCounts
	for _, account := range winner.Merged {
		if account.Origin {
			ledgered.Point += account.DonorFeatures.Point
			ledgered.Path += account.DonorFeatures.Path
			ledgered.Area += account.DonorFeatures.Area
		} else {
			ledgered.Point += account.Added
			if account.AddedShapes != 0 {
				return fmt.Errorf("account of %s claims %d added shapes; no shape feature merges yet",
					account.Source, account.AddedShapes)
			}
		}
	}
	if counted.Point != ledgered.Point {
		return fmt.Errorf("world holds %d points but its ledger claims %d", counted.Point, ledgered.Point)
	}
	if counted.Path != ledgered.Path || counted.Area != ledgered.Area {
		return fmt.Errorf("world holds %d paths and %d areas but its ledger claims %d and %d",
			counted.Path, counted.Area, ledgered.Path, ledgered.Area)
	}
	return nil
}
