package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// ShortStampLength is how much of a stamp URLs, file names, and logs carry:
// long enough that two builds of a volume will not collide, short enough to
// read past.
const ShortStampLength = 12

// Stamp accumulates a content fingerprint for a bundle without requiring the
// bundle's bytes. A producer feeds it a hash per named part -- payloads it
// just built, pyramid stamps its tile pipeline already computed -- and the sum
// is stable however the parts arrived. Equal stamps mean a rebuild would write
// the same bundle, which is what lets an unchanged volume be skipped.
//
// The sum is order-independent by construction: parts are sorted before they
// are hashed, so two producers that visit a volume's worlds in different
// orders still agree. The zero Stamp is ready to use.
type Stamp struct {
	parts []string
}

// Add records one named part. Order does not matter; the sum sorts.
//
// The name matters as much as the hash: it is what distinguishes two parts
// that happen to hold the same bytes, and what makes a renamed part a
// different bundle.
//
// A part name must hold no space and no newline. The two are the record
// separators of the summed form, so a name carrying either makes the sum
// ambiguous -- "a b" with hash "c" sums as "a" with hash "b c" does. Archive
// entry names have never held one, and the constraint is kept rather than
// coded around because widening the separator would restamp every bundle in
// every library.
func (s *Stamp) Add(name, hash string) {
	s.parts = append(s.parts, name+" "+hash)
}

// Sum returns the fingerprint of everything added: the sorted "name hash"
// lines joined by newlines, hashed with SHA-256, in lowercase hex. Sum does
// not consume the parts, so a producer may add more and sum again.
func (s *Stamp) Sum() string {
	sorted := make([]string, len(s.parts))
	copy(sorted, s.parts)
	sort.Strings(sorted)
	digest := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(digest[:])
}

// Parts lists the named parts as they will be hashed, sorted. It exists so a
// producer can report what a stamp was computed over when two builds
// unexpectedly differ.
func (s *Stamp) Parts() []string {
	sorted := make([]string, len(s.parts))
	copy(sorted, s.parts)
	sort.Strings(sorted)
	return sorted
}

// HashBytes fingerprints one part for [Stamp.Add]: SHA-256, lowercase hex.
func HashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// ShortStamp is the stamp as URLs, file names, and logs carry it.
func ShortStamp(stamp string) string {
	if len(stamp) <= ShortStampLength {
		return stamp
	}
	return stamp[:ShortStampLength]
}

// CaptureDay is the day part of a timestamp, as the file name spells it: the
// first eight digits, which for an RFC 3339 time is YYYYMMDD.
//
// The rule is digit-counting rather than time parsing so that a name can be
// derived from a manifest that has not been validated yet. A timestamp
// carrying fewer than eight digits yields the digits it has, and one with none
// yields the empty string, in which case the file name simply goes without.
func CaptureDay(createdAt string) string {
	var day strings.Builder
	for _, r := range createdAt {
		if r >= '0' && r <= '9' {
			day.WriteRune(r)
			if day.Len() == 8 {
				break
			}
		}
	}
	return day.String()
}

// VersionedFileName is the name a bundle carries in a registry directory:
// <slug>-<YYYYMMDD>-<stamp12>.atlas -- the volume, the day its data was
// captured, and enough of the stamp to tell two builds of the same capture
// apart.
//
// Builds of a volume sit side by side under these names, and the fold is what
// serves the right one. The name is for people and for cheap existence
// checks, never for ordering.
func VersionedFileName(m Manifest) string {
	name := m.Volume.Slug
	if day := CaptureDay(m.Version.CreatedAt); day != "" {
		name += "-" + day
	}
	return name + "-" + ShortStamp(m.Version.Stamp) + ".atlas"
}
