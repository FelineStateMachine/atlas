package vnext

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const Extension = ".atlas"

// Release is the small, schema-independent identity used while scanning a
// library. CreatedAt is source capture time, not build time.
type Release struct {
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
	Revision  int    `json:"revision,omitempty"`
	Stamp     string `json:"stamp"`
	Worlds    int    `json:"worlds"`
}

func (release Release) Validate() error { return release.validate(true) }

func (release Release) validate(requireStamp bool) error {
	if release.Title == "" {
		return fmt.Errorf("title is empty")
	}
	if _, err := time.Parse(time.RFC3339, release.CreatedAt); err != nil {
		return fmt.Errorf("createdAt is not RFC 3339: %w", err)
	}
	if release.Revision < 0 {
		return fmt.Errorf("revision is negative")
	}
	if release.Worlds < 1 {
		return fmt.Errorf("world count is less than one")
	}
	if requireStamp {
		decoded, err := hex.DecodeString(release.Stamp)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("stamp is not a SHA-256 digest")
		}
	}
	return nil
}

// ValidSlug keeps file and URL identities deliberately smaller than paths.
func ValidSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("empty")
	}
	for _, character := range slug {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return fmt.Errorf("%q contains %q", slug, character)
	}
	if strings.HasPrefix(slug, "-") || strings.HasPrefix(slug, "_") {
		return fmt.Errorf("%q starts with a separator", slug)
	}
	return nil
}

type Descriptor struct {
	Locator   string
	Slug      string
	Title     string
	Stamp     string
	CreatedAt string
	Revision  int
	Size      int64
	Worlds    int
}

func DescriptorOf(locator, slug string, release Release, size int64) Descriptor {
	return Descriptor{
		Locator: locator, Slug: slug, Title: release.Title, Stamp: release.Stamp,
		CreatedAt: release.CreatedAt, Revision: release.Revision, Size: size, Worlds: release.Worlds,
	}
}

func Newer(left, right Descriptor) bool {
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt > right.CreatedAt
	}
	if left.Revision != right.Revision {
		return left.Revision > right.Revision
	}
	if left.Stamp != right.Stamp {
		return left.Stamp > right.Stamp
	}
	return left.Locator > right.Locator
}

func Fold(candidates []Descriptor) map[string]Descriptor {
	winners := make(map[string]Descriptor, len(candidates))
	for _, candidate := range candidates {
		standing, held := winners[candidate.Slug]
		if !held || Newer(candidate, standing) {
			winners[candidate.Slug] = candidate
		}
	}
	return winners
}

func Shadowed(candidates []Descriptor) []Descriptor {
	winners := Fold(candidates)
	shadowed := make([]Descriptor, 0)
	for _, candidate := range candidates {
		if winner := winners[candidate.Slug]; winner.Locator != candidate.Locator {
			shadowed = append(shadowed, candidate)
		}
	}
	sort.Slice(shadowed, func(i, j int) bool {
		if shadowed[i].Slug != shadowed[j].Slug {
			return shadowed[i].Slug < shadowed[j].Slug
		}
		return Newer(shadowed[i], shadowed[j])
	})
	return shadowed
}

func Changed(before, after map[string]Descriptor) []string {
	changed := make([]string, 0)
	for slug, winner := range after {
		if standing, held := before[slug]; !held || standing.Stamp != winner.Stamp {
			changed = append(changed, slug)
		}
	}
	for slug := range before {
		if _, held := after[slug]; !held {
			changed = append(changed, slug)
		}
	}
	sort.Strings(changed)
	return changed
}

func ShortStamp(stamp string) string {
	if len(stamp) > 12 {
		return stamp[:12]
	}
	return stamp
}

func CaptureDay(createdAt string) string {
	var day strings.Builder
	for _, character := range createdAt {
		if character >= '0' && character <= '9' {
			day.WriteRune(character)
			if day.Len() == 8 {
				break
			}
		}
	}
	return day.String()
}

func VersionedFileName(slug string, release Release) string {
	name := slug
	if day := CaptureDay(release.CreatedAt); day != "" {
		name += "-" + day
	}
	return name + "-" + ShortStamp(release.Stamp) + Extension
}
