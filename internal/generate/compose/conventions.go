package compose

import (
	"fmt"
	"hash/fnv"
	"maps"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// speakConventions makes every world answer in the shared vocabulary, and then
// holds the whole volume to the registry.
//
// Some of the vocabulary is a source's to speak -- how a collection draws, what
// its true coordinates are -- and arrives already spoken in the document. The
// rest is composition's, because it is composition that knows it: a collection's
// kind is mirrored so a reader may trust the attribute or the field beside it,
// the artwork's form is declared once it has actually been resolved, and the
// rim a world's markers wear comes from curation. Nothing here overwrites what a
// source said.
//
// Validation is producer-strict and it is the gate: an unregistered atlas key,
// a value outside its vocabulary, or a key on the wrong entity fails the build
// here, one step before a bundle, rather than riding into somebody's library.
func speakConventions(tables curation.Tables, volume string, worlds []composedWorld) error {
	for worldIndex := range worlds {
		world := &worlds[worldIndex]
		if world.IconOutset != "" {
			world.Attrs = withAttr(world.Attrs, semconv.KeyIconOutset, world.IconOutset)
		}
		if err := semconv.Validate(semconv.EntityWorld, world.Attrs); err != nil {
			return fmt.Errorf("world %s: %w", world.Slug, err)
		}
		for index := range world.Collections {
			collection := &world.Collections[index]
			if _, declared := collection.Attrs[semconv.KeyGeometryKind]; !declared {
				collection.Attrs = withAttr(collection.Attrs, semconv.KeyGeometryKind, collection.Kind)
			}
			if collection.Kind == doc.KindPoint {
				// Where two sources are known to spell one concept two ways,
				// the shared name rides the collection itself, so a later merge
				// reads identity off the payload rather than a table of its own.
				if shared := tables.CollectionEquivalent(volume, collection.Icon); shared != "" {
					if _, declared := collection.Attrs[semconv.KeyCollectionKey]; !declared {
						collection.Attrs = withAttr(collection.Attrs, semconv.KeyCollectionKey, shared)
					}
				}
				if collection.IconAsset != "" {
					kind := semconv.IconKindGlyph
					if collection.IconPicture {
						kind = semconv.IconKindPicture
					}
					collection.Attrs = withAttr(collection.Attrs, semconv.KeyIconKind, kind)
				}
			}
			if err := semconv.Validate(semconv.EntityCollection, collection.Attrs); err != nil {
				return fmt.Errorf("world %s collection %q: %w", world.Slug, collection.Title, err)
			}
			for _, feature := range collection.Features {
				if err := semconv.Validate(semconv.EntityFeature, feature.Attrs); err != nil {
					return fmt.Errorf("world %s feature %q: %w", world.Slug, feature.Title, err)
				}
			}
		}
	}
	return nil
}

// withAttr sets one attribute on a copy of the map, never on the map itself:
// a split sheet's pieces share their source's attributes by reference, and
// speaking for one piece must not put words in another's mouth.
func withAttr(attrs map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	maps.Copy(out, attrs)
	out[key] = value
	return out
}

// claimID derives a collection's number from the world it sits in and the key
// it declared. See numberCollections for why, and for the collision rule.
func claimID(world int64, key string, used map[int64]string) (int64, error) {
	hash := fnv.New32a()
	fmt.Fprintf(hash, "%d:collection:%s", world, key)
	id := int64(hash.Sum32() & 0x7fffffff)
	if id == 0 {
		id = 1
	}
	if holder, taken := used[id]; taken {
		return 0, fmt.Errorf("collection %q collides with %q on id %d", key, holder, id)
	}
	used[id] = key
	return id, nil
}
