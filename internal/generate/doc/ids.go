package doc

import (
	"fmt"
	"hash/fnv"
)

// IDSpace mints the numeric identities a source without its own has to publish.
//
// The document's identifiers are signed 32-bit on the wire and zero reads as
// absence (see the package comment), so a source whose captures name things by
// string has to turn those names into numbers. The rule from §1.4 of
// docs/generate.md, made mechanical here:
//
//   - The number is a pure function of the name. The same capture numbers
//     itself the same way on any machine, in any run, forever, which is what
//     lets a reader's hide set and a merge ledger survive a rebuild.
//   - The name is the source's own stable spelling of the thing, qualified by
//     what it is -- a world, a collection, a feature -- so two kinds of thing
//     with one name do not meet.
//   - A collision is fatal. Two names landing on one number would silently
//     merge two features and lose one, and at this scale a collision means
//     something is wrong rather than that the space is full.
//
// An IDSpace is one document's worth of claims. It is not safe for concurrent
// use, and it is not meant to be: a translation is one pass over one volume.
type IDSpace struct {
	claimed map[int64]string
}

// NewIDSpace opens an empty space.
func NewIDSpace() *IDSpace {
	return &IDSpace{claimed: make(map[int64]string)}
}

// Claim mints the identity of one named thing, or reports the collision.
func (s *IDSpace) Claim(name string) (int64, error) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	id := int64(hash.Sum32() & 0x7fffffff)
	// Zero is absence on the wire, so nothing may be numbered with it.
	if id == 0 {
		id = 1
	}
	// Even the same name twice is refused: a source claiming one thing twice
	// has lost track of what it is describing, and the second claim would have
	// overwritten the first wherever the number is used as a key.
	if holder, taken := s.claimed[id]; taken {
		return 0, fmt.Errorf("%q and %q both number %d", holder, name, id)
	}
	s.claimed[id] = name
	return id, nil
}
