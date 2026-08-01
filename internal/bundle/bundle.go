// Package bundle defines the .atlas file: one volume, complete, in one zip
// archive. A volume is the subject of an atlas -- a game, a planet, a city --
// and its bundle carries everything the viewer needs to draw it: world
// payloads, tile pyramids, category icons, behind a manifest naming what is
// inside, so the application can list a volume without reading past the
// manifest and can serve it without unpacking anything to disk.
//
// The format is Atlas's own, and so are its words. A volume holds worlds --
// the distinct grounds a reader can stand on, which for a game are its maps
// and for a captured city are its dated revisions -- and each world is seen
// through one or more lenses, the rasters that picture it. The format
// carries no trace of where a volume's data was captured from; a producer
// flattens whatever its source looks like into this shape, and the
// application only ever sees this shape.
package bundle

import (
	"fmt"
	"strings"
)

const (
	// Format names the container in the manifest, so a stray zip renamed to
	// .atlas is refused by name rather than by whatever fails first.
	Format = "atlas-bundle"
	// FormatVersion is bumped when the layout changes shape. A reader refuses
	// versions it does not know rather than guessing at them. Version 2
	// renamed the format's concepts -- game to volume, map to world, variant
	// to lens -- through the manifest and payload keys alike.
	FormatVersion = 2

	// ManifestName is the entry the whole bundle is read through. The writer
	// places it first in the archive so a listing shows it first, though the
	// reader finds it by name wherever it sits.
	ManifestName = "atlas.json"
)

// Manifest is what a bundle says about itself: which volume it is, which
// build of that volume's data it carries, and which worlds are inside.
//
// Conventions names the semantic-convention vocabulary the payloads were
// written against; zero is a bundle from before the conventions existed.
// It is a declaration, never a gate -- a reader that knows a newer or older
// vocabulary still opens the bundle and ignores what it cannot speak --
// but validation is stricter about a bundle that declares than one that
// does not.
type Manifest struct {
	Format        string       `json:"format"`
	FormatVersion int          `json:"formatVersion"`
	Conventions   int          `json:"conventions,omitempty"`
	Volume        Volume       `json:"volume"`
	Version       Version      `json:"version"`
	TileGrid      TileGrid     `json:"tileGrid"`
	Worlds        []WorldEntry `json:"worlds"`
}

// Volume is the identity a bundle claims. The slug is the identity proper:
// two bundles naming the same slug are two versions of the same volume,
// however their files are named.
type Volume struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Version distinguishes builds of the same volume. The stamp is a content
// fingerprint -- two bundles with equal stamps hold the same data -- and
// CreatedAt orders them, so a newer bundle dropped beside an older one wins.
// Revision orders builds of the same capture: the data has not moved, but
// the policy that turns a capture into a bundle has, and the build under the
// newer policy is the one that should serve. Bundles from before the field
// existed read as revision zero and lose to any revised build.
type Version struct {
	Stamp     string `json:"stamp"`
	CreatedAt string `json:"createdAt"`
	Revision  int    `json:"revision,omitempty"`
}

// TileGrid is the window this volume's worlds are cut from: the zoom the
// source was captured at, where the first tile sits, and how big a tile and
// the world square are. A world cut from a window of its own overrides part
// of this in its own payload.
type TileGrid struct {
	SourceZoom int `json:"sourceZoom"`
	FirstTile  int `json:"firstTile"`
	TileSize   int `json:"tileSize"`
	Size       int `json:"size"`
}

// Coordinate is a point in world space, in the latitude and longitude of the
// volume's own projection.
type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// WorldEntry lists one world: enough to offer it and to open it, and nothing
// that only matters once it is open. The slug names the world's payload
// entries -- worlds/<slug>.json, .bin, and .text -- and is the world's
// identity everywhere.
type WorldEntry struct {
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	Parent     string     `json:"parent,omitempty"`
	IconOutset string     `json:"iconOutset,omitempty"`
	Center     Coordinate `json:"center"`
	PinCount   int        `json:"pinCount"`
	UpdatedAt  string     `json:"updatedAt"`
}

// Validate reports the first thing structurally wrong with the manifest. It
// checks the manifest alone; whether the entries it promises exist is the
// reader's Validate, which has the archive in hand.
func (m Manifest) Validate() error {
	if m.Format != Format {
		return fmt.Errorf("format is %q, not %q", m.Format, Format)
	}
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("format version is %d, and this reads %d", m.FormatVersion, FormatVersion)
	}
	if err := validSlug(m.Volume.Slug); err != nil {
		return fmt.Errorf("volume slug: %w", err)
	}
	if m.Volume.Title == "" {
		return fmt.Errorf("volume %s has no title", m.Volume.Slug)
	}
	if m.Version.Stamp == "" || m.Version.CreatedAt == "" {
		return fmt.Errorf("volume %s carries no version", m.Volume.Slug)
	}
	if len(m.Worlds) == 0 {
		return fmt.Errorf("volume %s lists no worlds", m.Volume.Slug)
	}
	seen := make(map[string]bool, len(m.Worlds))
	for _, entry := range m.Worlds {
		if err := validSlug(entry.Slug); err != nil {
			return fmt.Errorf("world slug: %w", err)
		}
		if entry.Title == "" {
			return fmt.Errorf("world %s has no title", entry.Slug)
		}
		if seen[entry.Slug] {
			return fmt.Errorf("world slug %s is listed twice", entry.Slug)
		}
		seen[entry.Slug] = true
	}
	return nil
}

// validSlug admits names that are safe as path segments and as URL segments
// without escaping: there is no reason for an identifier to need more, and
// everything a hostile name could smuggle -- separators, dots that climb --
// needs more.
func validSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("empty")
	}
	for _, r := range slug {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("%q contains %q", slug, r)
	}
	if strings.HasPrefix(slug, "-") || strings.HasPrefix(slug, "_") {
		return fmt.Errorf("%q starts with a separator", slug)
	}
	return nil
}
