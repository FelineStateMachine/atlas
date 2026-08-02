// Package merge folds other readings of one volume into the reading that
// serves.
//
// When two sources capture one game, the newest capture is the one a registry
// serves, and the other reading would otherwise ride along unseen in a shadowed
// bundle. Here the serving volume absorbs it: each other reading's ground is
// aligned into the serving world by the transformation their shared named
// places determine -- the same alignment a resampled raster stands on -- and
// every feature is resolved against what the serving world already holds.
//
// A place both readings pin is recorded as one place and never drawn twice. A
// place only the other reading knows is added, into the serving world's own
// collection where the two spell the same concept, and under a collection named
// for its source where they do not. A place the resolution cannot be sure about
// is held back, with the reason written down. The ledger carries the whole
// account, so a merge is a documented derivation over immutable captures rather
// than an edit.
package merge

import (
	"fmt"
	"log/slog"
	"math"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/enrich/align"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

// Name is what curation queues this enricher as.
const Name = "merge"

// Enricher folds donor readings into the serving volume.
type Enricher struct{}

// New builds the enricher. It carries no state: everything it consults arrives
// in the context, which is what lets one instance serve every volume in a run.
func New() *Enricher { return &Enricher{} }

func (*Enricher) Name() string { return Name }

// Declares says what a merge may write onto the serving volume. Prose is not
// here: a description travels through the contribution's own prose operation,
// under a name the conventions registry deliberately does not know, because no
// payload carries it as an attribute.
func (*Enricher) Declares() []string {
	return []string{semconv.KeyGeoLat, semconv.KeyGeoLon, semconv.KeyIconStd}
}

// Enrich folds every donor reading into the serving volume, in the order the
// donors were given.
//
// Each donor is resolved against the volume as the donors before it left it,
// which is the only order that can be right: a place two donors both know is
// one place, and the second donor has to be able to see that the first already
// contributed it.
func (e *Enricher) Enrich(v *enrich.Volume, ctx enrich.Context) (enrich.Contribution, error) {
	out := enrich.Contribution{Enricher: Name, Volume: v.Slug}
	if len(ctx.Donors) == 0 {
		return out, nil
	}
	log := ctx.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op("merge"), logging.Volume(v.Slug))

	// The donors are folded into a copy, so that each donor sees what the ones
	// before it contributed while the caller's volume stays untouched until the
	// whole contribution is applied.
	working := v.Clone()
	for _, donor := range ctx.Donors {
		if donor.NewestCapture() > working.NewestCapture() {
			return enrich.Contribution{}, fmt.Errorf(
				"%s captured %s is newer than the serving reading captured %s; "+
					"the newest capture serves and the others are folded into it",
				donor.Source.Name, donor.NewestCapture(), working.NewestCapture())
		}
		ops, err := foldVolume(working, donor, ctx, log)
		if err != nil {
			return enrich.Contribution{}, err
		}
		if len(ops) == 0 {
			continue
		}
		step := enrich.Contribution{Enricher: Name, Volume: v.Slug, Ops: ops}
		if err := enrich.Apply(working, step); err != nil {
			return enrich.Contribution{}, err
		}
		out.Ops = append(out.Ops, ops...)
	}
	return out, nil
}

// foldVolume folds one donor reading into the working volume.
func foldVolume(working, donor *enrich.Volume, ctx enrich.Context, log *slog.Logger) ([]enrich.Op, error) {
	var ops []enrich.Op
	for index := range donor.Worlds {
		donorWorld := &donor.Worlds[index]
		target := working.World(donorWorld.Slug)
		if target == nil {
			// Sources do not divide the world the same way: one reading's
			// separate ground may be ground another draws inside a larger
			// sheet, in its own projection. A shared slug is only the cheapest
			// evidence of shared ground -- the real test is whether the places
			// both name determine a transformation -- so before a world is
			// taken as new, it is tried against every world the volume already
			// draws.
			target = overlapping(working, donorWorld, log)
		}
		if target == nil {
			contributed, err := wholeWorld(working, donor, donorWorld)
			if err != nil {
				return nil, err
			}
			log.Info("world joins whole", logging.World(donorWorld.Slug),
				logging.Source(donor.Source.Name))
			ops = append(ops, contributed...)
			continue
		}
		folded, account, merged, err := foldWorld(working, target, donor, donorWorld, ctx, log)
		if err != nil {
			return nil, err
		}
		if !merged {
			continue
		}
		if err := enrich.GateAccount(account); err != nil {
			return nil, err
		}
		log.Info("world merged", logging.World(target.Slug), logging.Source(donor.Source.Name),
			"offered", account.DonorFeatures.Total(), "matched", account.MatchedN(),
			"enriched", account.EnrichedN(), "added", account.Added,
			"adopted", account.AdoptedN(), "held", account.HeldN(),
			"rejected", account.RejectedN(), "alignment", account.Alignment)
		ops = append(ops, folded...)
		ops = append(ops, enrich.Op{Kind: enrich.OpLedger, World: target.Slug, Account: &account})
	}
	return ops, nil
}

// overlapping finds the serving world that pictures the same ground as a donor
// world under another slug, if any does: the one whose named places fit the
// donor's through an affine that closes. The fit is the compatibility test the
// differing projections make necessary -- coordinates cannot be compared, but
// the places can.
func overlapping(working *enrich.Volume, donorWorld *enrich.World, log *slog.Logger) *enrich.World {
	donorAnchors := anchorsOf(donorWorld)
	var best *enrich.World
	bestAnchors := 0
	for index := range working.Worlds {
		candidate := &working.Worlds[index]
		_, report, err := align.Fit(donorAnchors, anchorsOf(candidate))
		if err != nil {
			continue
		}
		if report.Anchors > bestAnchors {
			best, bestAnchors = candidate, report.Anchors
		}
	}
	if best != nil {
		log.Info("world pictures ground already drawn", logging.World(donorWorld.Slug),
			"drawn_as", best.Slug, "anchors", bestAnchors)
	}
	return best
}

// wholeWorld carries one reading's ground into the volume untouched: its
// collections, features and prose as captured, and its artwork brought across
// -- under its own key where that key is free or already holds the same bytes,
// and under a source-prefixed key where the volume spells something else with
// it.
func wholeWorld(working, donor *enrich.Volume, donorWorld *enrich.World) ([]enrich.Op, error) {
	contributed := donorWorld.Clone()
	if len(contributed.Ledger) == 0 {
		// A ground that joins whole opens its own account, exactly as it would
		// have if it had been composed on its own: it came from this reading,
		// and it arrived holding this.
		contributed.Ledger = []enrich.Account{{
			Source:        donor.Source.Label,
			Slug:          donor.Source.Name,
			Origin:        true,
			DonorFeatures: enrich.Tally(&contributed),
		}}
	}
	tag := slugify(donor.Source.Label)
	carried := newArtwork(working, donor, tag, false)
	for index := range contributed.Collections {
		collection := &contributed.Collections[index]
		key, err := carried.carry(collection.Icon)
		if err != nil {
			return nil, err
		}
		collection.Icon = key
	}
	ops := carried.ops
	return append(ops, enrich.Op{Kind: enrich.OpAddWorld, NewWorld: &contributed}), nil
}

// foldWorld resolves one donor world into one serving world.
func foldWorld(
	working *enrich.Volume,
	serving *enrich.World,
	donor *enrich.Volume,
	donorWorld *enrich.World,
	ctx enrich.Context,
	log *slog.Logger,
) ([]enrich.Op, enrich.Account, bool, error) {
	if !serving.Grid.Ready() || !donorWorld.Grid.Ready() {
		return nil, enrich.Account{}, false, fmt.Errorf(
			"world %s: a merge measures distance, and one of the two readings is cut from no window",
			serving.Slug)
	}
	affine, report, err := align.Fit(anchorsOf(donorWorld), anchorsOf(serving))
	if err != nil {
		// A fit that will not close is a merge that does not happen. The two
		// readings stay apart, whole, in separate builds, which is exactly what
		// they were before anybody tried -- and a person should eventually hear
		// that two readings of one ground could not be brought together.
		log.Warn("readings stay apart", logging.World(serving.Slug),
			logging.Source(donor.Source.Name), "reason", err.Error())
		return nil, enrich.Account{}, false, nil
	}

	account := enrich.Account{
		Source:    donor.Source.Label,
		Slug:      donor.Source.Name,
		Alignment: report.String(),
	}
	// A nameless neighbour matters out to where the alignment's own noise
	// reaches: two readings place the same shop apart by their residuals, so
	// the radius listens to the fit rather than assuming precision.
	nearbyRadius := math.Max(ctx.Curation.NearbyFloorPx(), 2*report.P90Px)
	index := indexServing(serving)
	// Artwork carried into a world that already draws its own always lands
	// under a source-tagged file, so a contributed picture can never be
	// written over one the serving reading ships under the same name.
	carried := newArtwork(working, donor, slugify(donor.Source.Label), true)

	var ops []enrich.Op
	var kept []enrich.Collection
	for _, donorCollection := range donorWorld.Collections {
		if donorCollection.Kind != enrich.KindPoint {
			// Matching is point-only, but the ledger is not: every donor path
			// and area feature is held on the record, one line each with its
			// title, rather than dropped without a word.
			for _, shape := range donorCollection.Features {
				if donorCollection.Kind == enrich.KindPath {
					account.DonorFeatures.Path++
				} else {
					account.DonorFeatures.Area++
				}
				account.Held = append(account.Held, enrich.HeldItem{
					Donor: shape.ID, Title: shape.Title, Reason: enrich.HeldShapeReason,
				})
			}
			continue
		}
		identity := enrich.MergeIdentity(donorCollection)
		adoptive := index.collections[identity]

		// Attribute-level resolution at the collection: a serving collection
		// with no artwork of any kind takes the donor's standard-icon
		// declaration, and the ledger says so.
		if adoptive != nil && ctx.Curation.DonorFillsEmpty(semconv.KeyIconStd) &&
			adoptive.IconAsset == "" && adoptive.Icon == "" &&
			adoptive.Attrs[semconv.KeyIconStd] == "" &&
			donorCollection.Attrs[semconv.KeyIconStd] != "" {
			ops = append(ops, enrich.Op{
				Kind:       enrich.OpSetAttr,
				World:      serving.Slug,
				Collection: adoptive.ID,
				Entity:     semconv.EntityCollection,
				Key:        semconv.KeyIconStd,
				Value:      donorCollection.Attrs[semconv.KeyIconStd],
			})
			account.Enriched = append(account.Enriched, enrich.CollectionTake{
				Collection: identity, Key: semconv.KeyIconStd,
			})
		}

		keeping := donorCollection
		keeping.Features = nil
		for _, feature := range donorCollection.Features {
			account.DonorFeatures.Point++
			if feature.At == nil {
				account.Rejected = append(account.Rejected, enrich.HeldItem{
					Donor: feature.ID, Title: feature.Title, Reason: placelessReason,
				})
				continue
			}
			x, y := affine.Apply(
				donorWorld.Grid.ProjectX(feature.At.Lng),
				donorWorld.Grid.ProjectY(feature.At.Lat),
			)
			outcome := resolve(feature, x, y, index, identity, nearbyRadius, ctx.Curation)
			switch outcome.kind {
			case matched:
				index.claimed[outcome.match.ID] = feature.ID
				pair := enrich.MatchedPair{
					Donor:      feature.ID,
					Winner:     outcome.match.ID,
					DistancePx: int(outcome.distance + 0.5),
				}
				// Attribute-level: the policy table says, key by key, what a
				// serving feature takes from its counterpart, and every take is
				// ledgered. The description travels under its policy name, and
				// true coordinates fill in the same way words do: only where
				// the serving side has none of its own.
				if ctx.Curation.DonorFillsEmpty(semconv.KeyNoteText) &&
					outcome.match.Description == "" && feature.Description != "" {
					ops = append(ops, enrich.Op{
						Kind:    enrich.OpSetProse,
						World:   serving.Slug,
						Feature: outcome.match.ID,
						Value:   feature.Description,
					})
					pair.Enriched = true
					pair.Took = append(pair.Took, semconv.KeyNoteText)
				}
				for _, key := range []string{semconv.KeyGeoLat, semconv.KeyGeoLon} {
					if !ctx.Curation.DonorFillsEmpty(key) {
						continue
					}
					if feature.Attrs[key] == "" || outcome.match.Attrs[key] != "" {
						continue
					}
					ops = append(ops, enrich.Op{
						Kind:    enrich.OpSetAttr,
						World:   serving.Slug,
						Feature: outcome.match.ID,
						Entity:  semconv.EntityFeature,
						Key:     key,
						Value:   feature.Attrs[key],
					})
					pair.Took = append(pair.Took, key)
				}
				account.Matched = append(account.Matched, pair)
			case held:
				account.Held = append(account.Held, enrich.HeldItem{
					Donor: feature.ID, Title: feature.Title, Reason: outcome.reason,
				})
			case distinct:
				if x < 0 || y < 0 || x > float64(serving.Grid.Size) || y > float64(serving.Grid.Size) {
					account.Rejected = append(account.Rejected, enrich.HeldItem{
						Donor: feature.ID, Title: feature.Title,
						Reason: outsideReason,
					})
					continue
				}
				if holder, taken := index.ids[feature.ID]; taken {
					return nil, enrich.Account{}, false, fmt.Errorf(
						"feature id %d (%s) collides with serving feature %q",
						feature.ID, feature.Title, holder)
				}
				index.ids[feature.ID] = feature.Title
				moved := feature.Clone()
				moved.At = &enrich.Position{
					Lat: serving.Grid.UnprojectLat(y),
					Lng: serving.Grid.UnprojectLng(x),
				}
				// A contributed feature stands on the serving ground, not on
				// whatever ground it stood on in the reading it came from.
				moved.Member = 0
				account.Added++
				if adoptive != nil {
					ops = append(ops, enrich.Op{
						Kind:       enrich.OpAddFeature,
						World:      serving.Slug,
						Collection: adoptive.ID,
						NewFeature: &moved,
					})
					account.Adopted = append(account.Adopted, enrich.AdoptedItem{
						Donor: feature.ID, Into: identity,
					})
					continue
				}
				keeping.Features = append(keeping.Features, moved)
			}
		}
		if len(keeping.Features) == 0 {
			continue
		}
		kept = append(kept, keeping)
	}

	for index := range kept {
		collection := &kept[index]
		// The kept collections file under a group named for their source, so
		// the legend says where they came from without machinery of its own.
		collection.Group = donor.Source.Label
		key, err := carried.carry(collection.Icon)
		if err != nil {
			return nil, enrich.Account{}, false, err
		}
		collection.Icon = key
		collection.IconAsset = ""
		collection.IconPicture = false
		ops = append(ops, enrich.Op{
			Kind: enrich.OpAddCollection, World: serving.Slug, NewCollection: collection,
		})
	}
	return append(carried.ops, ops...), account, true, nil
}

// servingIndex is everything the serving world holds, arranged for resolution:
// each point feature with its place, name and name tokens; the collections by
// merge identity; and the identifiers already spoken for.
type servingIndex struct {
	features    []placed
	byName      map[string][]int
	collections map[string]*enrich.Collection
	ids         map[int64]string
	// claimed maps each serving feature already matched to the donor that
	// matched it. A place is one place: the next donor bearing the same name
	// must find its own, or say why it cannot.
	claimed map[int64]int64
}

type placed struct {
	feature  *enrich.Feature
	identity string
	x, y     float64
	tokens   map[string]bool
}

func indexServing(serving *enrich.World) *servingIndex {
	index := &servingIndex{
		byName:      make(map[string][]int),
		collections: make(map[string]*enrich.Collection),
		ids:         make(map[int64]string),
		claimed:     make(map[int64]int64),
	}
	for collectionIndex := range serving.Collections {
		collection := &serving.Collections[collectionIndex]
		if collection.Kind != enrich.KindPoint {
			for _, feature := range collection.Features {
				index.ids[feature.ID] = feature.Title
			}
			continue
		}
		identity := enrich.MergeIdentity(*collection)
		if _, held := index.collections[identity]; !held && identity != "" {
			index.collections[identity] = collection
		}
		for featureIndex := range collection.Features {
			feature := &collection.Features[featureIndex]
			name := align.NormalizeTitle(feature.Title)
			index.byName[name] = append(index.byName[name], len(index.features))
			var x, y float64
			if feature.At != nil {
				x = serving.Grid.ProjectX(feature.At.Lng)
				y = serving.Grid.ProjectY(feature.At.Lat)
			}
			index.features = append(index.features, placed{
				feature:  feature,
				identity: identity,
				x:        x,
				y:        y,
				tokens:   align.Tokens(name),
			})
			index.ids[feature.ID] = feature.Title
		}
	}
	return index
}

// The reasons a feature is held back or refused.
//
// These are the reference implementation's wording, kept letter for letter.
// They are not messages about the code: they are data, written into the ledger
// of every merged bundle already published, and a reader comparing two builds'
// accounts compares these strings. Their vocabulary is one rewrite behind the
// rest of this lane -- a feature is a "pin", a collection a "category" -- and
// that is the price of not rewriting what every existing bundle says about
// itself.
const (
	namedLikeReason      = "named like %q %.0fpx away; too far to merge, too near to double"
	besideReason         = "beside %q in the same category; names disagree"
	alreadyMatchedReason = "every nearby pin of this name is already matched"
	outsideReason        = "outside the shared world"
	placelessReason      = "stands nowhere"
)

// The outcomes one donor feature may have.
const (
	distinct = iota
	matched
	held
)

type outcome struct {
	kind     int
	match    *enrich.Feature
	distance float64
	reason   string
}

// resolve decides what one donor feature is, against everything the serving
// world holds.
//
// The same name near where the alignment predicts is the same place, and so is
// a name one reading spells inside the other's -- "Northside Apartment" inside
// "Northside, Watson Apartment" -- when the two share a collection. The same
// name far beyond the radius is a different place bearing it; the stretch
// between is left undecided. A nameless neighbour inside the same collection is
// likewise held rather than guessed -- proximity alone never merges -- and only
// a feature resembling nothing at all is added.
func resolve(
	donor enrich.Feature,
	x, y float64,
	index *servingIndex,
	identity string,
	nearbyRadius float64,
	tables enrich.Curation,
) outcome {
	name := align.NormalizeTitle(donor.Title)
	donorTokens := align.Tokens(name)

	var nearest *placed
	nearestDistance := math.Inf(1)
	alreadyClaimed := false
	consider := func(candidate *placed, distance float64) {
		// A serving feature another donor already resolved to is one place, not
		// two: it cannot be matched again, but its nearness is remembered so
		// the refusal can say what stood in the way.
		if _, taken := index.claimed[candidate.feature.ID]; taken {
			if distance <= tables.MatchRadiusPx() {
				alreadyClaimed = true
			}
			return
		}
		if distance < nearestDistance {
			nearest, nearestDistance = candidate, distance
		}
	}
	if name != "" {
		for _, at := range index.byName[name] {
			candidate := &index.features[at]
			consider(candidate, math.Hypot(candidate.x-x, candidate.y-y))
		}
		// One reading's name written inside the other's counts only with the
		// collection agreeing: a bare "Apartment" must not roam the world for a
		// long-named cousin.
		if nearest == nil && len(donorTokens) >= 2 {
			for at := range index.features {
				candidate := &index.features[at]
				if candidate.identity != identity || !tokenSubset(donorTokens, candidate.tokens) {
					continue
				}
				consider(candidate, math.Hypot(candidate.x-x, candidate.y-y))
			}
		}
	}
	if nearest != nil {
		switch {
		case nearestDistance <= tables.MatchRadiusPx():
			return outcome{kind: matched, match: nearest.feature, distance: nearestDistance}
		case nearestDistance <= tables.SeparateRadiusPx():
			return outcome{kind: held, reason: fmt.Sprintf(namedLikeReason,
				nearest.feature.Title, nearestDistance)}
		}
	}
	if alreadyClaimed {
		return outcome{kind: held, reason: alreadyMatchedReason}
	}
	if _, shared := index.collections[identity]; shared {
		for at := range index.features {
			candidate := &index.features[at]
			if candidate.identity != identity {
				continue
			}
			if math.Hypot(candidate.x-x, candidate.y-y) <= nearbyRadius {
				return outcome{kind: held, reason: fmt.Sprintf(besideReason, candidate.feature.Title)}
			}
		}
	}
	return outcome{kind: distinct}
}

// tokenSubset reports whether one name is spelled entirely inside the other,
// either way round. The shorter side must carry at least two words: a single
// word inside a longer name -- every "Apartment" inside every apartment -- says
// nothing.
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

func anchorsOf(w *enrich.World) []align.Anchor {
	var anchors []align.Anchor
	for _, collection := range w.Collections {
		if collection.Kind != enrich.KindPoint {
			continue
		}
		for _, feature := range collection.Features {
			if feature.At == nil {
				continue
			}
			anchors = append(anchors, align.Anchor{
				Title: feature.Title,
				X:     w.Grid.ProjectX(feature.At.Lng),
				Y:     w.Grid.ProjectY(feature.At.Lat),
			})
		}
	}
	return anchors
}

// artwork carries a donor's icons across, so that nothing a donor ships can
// displace what the serving reading already drew.
//
// Two names travel with one picture, and they are tagged on different rules
// because they answer different questions. The **file** is where the bytes land
// in the volume's icons directory, and a contribution's bytes may never land on
// the serving volume's: when a donor's collections are folded into a ground the
// serving reading already draws, the file always takes the source's tag. The
// **key** is what a collection names its artwork by, and it stays the donor's
// own -- there is nothing to displace unless the serving volume already names
// different artwork with it, and only then is the key tagged too.
type artwork struct {
	into  *enrich.Volume
	donor *enrich.Volume
	tag   string
	// tagFiles marks a contribution into a volume that is already drawing:
	// every file it carries is tagged, whether or not the name was free.
	tagFiles bool
	byKey    map[string]enrich.Icon
	renamed  map[string]string
	ops      []enrich.Op
}

func newArtwork(into, donor *enrich.Volume, tag string, tagFiles bool) *artwork {
	byKey := make(map[string]enrich.Icon, len(donor.Icons))
	for _, icon := range donor.Icons {
		byKey[icon.Key] = icon
	}
	return &artwork{
		into: into, donor: donor, tag: tag, tagFiles: tagFiles,
		byKey: byKey, renamed: map[string]string{},
	}
}

// carry brings one icon across and answers with the key the contributed
// collection should name. An icon the donor does not actually hold answers with
// no key: a collection with artwork nobody shipped draws without it, which is
// what it did in the reading it came from.
func (a *artwork) carry(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	if replacement, seen := a.renamed[key]; seen {
		return replacement, nil
	}
	icon, held := a.byKey[key]
	if !held {
		a.renamed[key] = ""
		return "", nil
	}
	name, file := key, icon.File
	if a.tagFiles {
		file = a.tag + "--" + icon.File
	}
	for _, standing := range a.into.Icons {
		if standing.Key != key {
			continue
		}
		if string(standing.Data) == string(icon.Data) {
			// The same artwork under the same name is the same artwork.
			a.renamed[key] = key
			return key, nil
		}
		// The name is spoken for by something else, so the contribution
		// answers to a tagged one instead.
		name = a.tag + "--" + key
		file = a.tag + "--" + icon.File
		break
	}
	carried := enrich.Icon{Key: name, File: file, Data: icon.Data}
	a.ops = append(a.ops, enrich.Op{Kind: enrich.OpAddAsset, Asset: &carried})
	a.renamed[key] = name
	return name, nil
}

// slugify folds a source's label into the shape a file name may carry.
func slugify(label string) string {
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
