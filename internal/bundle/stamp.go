package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Stamp accumulates a content fingerprint for a bundle without requiring the
// bundle's bytes: a producer feeds it a hash per part -- payloads it just
// built, pyramid stamps its tile pipeline already computed -- and the sum is
// stable however the parts arrived. Equal stamps mean a rebuild would write
// the same bundle, which is what lets an unchanged volume be skipped.
type Stamp struct {
	parts []string
}

// Add records one named part. Order does not matter; the sum sorts.
func (s *Stamp) Add(name, hash string) {
	s.parts = append(s.parts, name+" "+hash)
}

// Sum returns the fingerprint of everything added.
func (s *Stamp) Sum() string {
	sorted := make([]string, len(s.parts))
	copy(sorted, s.parts)
	sort.Strings(sorted)
	digest := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(digest[:])
}

// HashBytes fingerprints one part for Add.
func HashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// MoreRecent reports whether a should shadow b when both claim the same volume.
// Creation time decides; among builds of the same capture the newer policy
// revision decides; the stamp and then the path break the remaining ties, so
// the choice is total and two scans of the same directory always agree.
func MoreRecent(a, b *Bundle) bool {
	if a.Manifest.Version.CreatedAt != b.Manifest.Version.CreatedAt {
		return a.Manifest.Version.CreatedAt > b.Manifest.Version.CreatedAt
	}
	if a.Manifest.Version.Revision != b.Manifest.Version.Revision {
		return a.Manifest.Version.Revision > b.Manifest.Version.Revision
	}
	if a.Manifest.Version.Stamp != b.Manifest.Version.Stamp {
		return a.Manifest.Version.Stamp > b.Manifest.Version.Stamp
	}
	return a.Path > b.Path
}

// ShortStamp is the stamp as URLs and logs carry it: long enough that two
// builds of a volume will not collide, short enough to read past.
func ShortStamp(stamp string) string {
	if len(stamp) <= 12 {
		return stamp
	}
	return stamp[:12]
}

// VersionedFileName is the name a bundle carries in a registry directory:
// the volume, the day its data was captured, and enough of the stamp to tell
// two builds of the same capture apart. Versions of a volume sit side by side
// under these names, and the newest-wins fold is what serves the right one --
// the name is for people and for cheap existence checks, never for ordering.
func VersionedFileName(m Manifest) string {
	var day strings.Builder
	for _, r := range m.Version.CreatedAt {
		if r >= '0' && r <= '9' {
			day.WriteRune(r)
			if day.Len() == 8 {
				break
			}
		}
	}
	name := m.Volume.Slug
	if day.Len() > 0 {
		name += "-" + day.String()
	}
	return name + "-" + ShortStamp(m.Version.Stamp) + ".atlas"
}
