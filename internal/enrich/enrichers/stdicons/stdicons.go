// Package stdicons resolves the standard icon library a collection may name
// instead of shipping artwork of its own.
//
// A translator that reads a source with no icons can still say what a
// collection is a collection of, by naming a glyph from a standard set:
// atlas.icon.std = "maki/monument". This package turns that promise into bytes.
// The named glyph lands in the volume's icon set under a name that spells its
// provenance -- std--maki-monument.svg -- so a bundle listing reads honestly and
// a source's own artwork can never be shadowed by it. The application never
// learns this package exists: a resolved standard icon is one more asset in a
// bundle's icons tree, tinted and haloed like any glyph a source shipped.
//
// It runs after a merge, which is the whole reason it is an enricher rather
// than a step inside a translator: a collection folded in from another reading
// resolves its declaration too.
//
// # Where silence is not the rule
//
// Every other enricher in this lane prefers silence to a plausible claim. This
// one refuses instead: a declaration the library cannot answer fails the build.
// The difference is who made the promise. A membership join guesses about the
// world; a standard-icon declaration was written by a translator author, in
// this repository, and an unanswerable one is a typo that should be heard about
// while it is one table edit old rather than quietly dropped.
//
// Only the names some translator actually speaks are vendored. The embedded set
// is the vocabulary.
package stdicons

import (
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/enrich"
	"github.com/FelineStateMachine/atlas/internal/logging"
)

//go:embed standard/maki/*.svg standard/maki/LICENSE.txt
var standard embed.FS

// Name is what curation queues this enricher as.
const Name = "stdicons"

// Enricher resolves standard icon declarations.
type Enricher struct{}

// New builds the enricher.
func New() *Enricher { return &Enricher{} }

func (*Enricher) Name() string { return Name }

// Declares is empty: this enricher writes artwork, not attributes. The kind of
// artwork a collection ends up carrying is declared by composition, which is
// the step that knows whether the asset actually travelled.
func (*Enricher) Declares() []string { return nil }

// Enrich resolves every declaration a collection makes and does not already
// answer with artwork of its own.
func (e *Enricher) Enrich(v *enrich.Volume, ctx enrich.Context) (enrich.Contribution, error) {
	out := enrich.Contribution{Enricher: Name, Volume: v.Slug}
	log := ctx.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With(logging.Op(Name), logging.Volume(v.Slug))

	carried := make(map[string]bool)
	for _, world := range v.Worlds {
		for _, collection := range world.Collections {
			if collection.Kind != enrich.KindPoint {
				continue
			}
			ref := collection.Attrs[semconv.KeyIconStd]
			// A source's own artwork wins the slot: the declaration is what a
			// collection wears when it has nothing.
			if ref == "" || collection.Icon != "" || collection.IconAsset != "" {
				continue
			}
			data, asset, err := Standard(ref)
			if err != nil {
				return enrich.Contribution{}, fmt.Errorf(
					"world %s collection %q: %w", world.Slug, collection.Title, err)
			}
			key := strings.TrimSuffix(asset, ".svg")
			if !carried[key] {
				carried[key] = true
				out.Ops = append(out.Ops, enrich.Op{
					Kind:  enrich.OpAddAsset,
					Asset: &enrich.Icon{Key: key, File: asset, Data: data},
				})
			}
			out.Ops = append(out.Ops, enrich.Op{
				Kind:       enrich.OpSetIcon,
				World:      world.Slug,
				Collection: collection.ID,
				Key:        key,
				Value:      asset,
			})
			log.Debug("standard icon resolved", logging.World(world.Slug),
				"collection", collection.Title, "icon", ref)
		}
	}
	if len(carried) > 0 {
		log.Info("standard icons resolved", "assets", len(carried))
	}
	return out, nil
}

// Standard resolves a set/name reference to the icon's bytes and the asset name
// it ships under.
func Standard(ref string) ([]byte, string, error) {
	set, name, found := strings.Cut(ref, "/")
	if !found || set != "maki" {
		return nil, "", fmt.Errorf("standard icon %q is not in a vendored set", ref)
	}
	data, err := standard.ReadFile("standard/maki/" + name + ".svg")
	if err != nil {
		return nil, "", fmt.Errorf("standard icon %q is not vendored", ref)
	}
	return data, "std--maki-" + name + ".svg", nil
}

// Vocabulary lists the references the vendored set answers, in sorted order. It
// is what a translator author checks a new declaration against, and what the
// test that holds the embed to the documented set reads.
func Vocabulary() []string {
	entries, err := standard.ReadDir("standard/maki")
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), ".svg")
		if !found {
			continue
		}
		out = append(out, "maki/"+name)
	}
	// The directory hands its entries back in file-name order, which is not the
	// order these read in: a hyphen sorts before a dot, so circle-stroked.svg
	// comes before circle.svg and the references come out shuffled.
	sort.Strings(out)
	return out
}
