// Package bundle reads and writes the .atlas file: one volume, complete, in
// one zip archive.
//
// A volume is the subject of an atlas -- a game, a planet, a city -- and its
// bundle carries everything a reader needs to draw it: world payloads, tile
// pyramids, icons, behind a manifest naming what is inside. A reader can list
// a volume without reading past the manifest and can serve it without
// unpacking anything to disk.
//
// The vocabulary is the format's own. A volume holds worlds -- the distinct
// grounds a reader can stand on, which for a game are its maps and for a
// captured city are its dated revisions -- and each world is pictured by one
// or more lenses. Collections are the ordered groups of features within a
// world; every feature is exactly one of point, path, or area. Nothing in the
// format records where a volume's data was captured from: a producer
// flattens whatever its source looks like into this shape, and a reader only
// ever sees this shape.
//
// The container's layout, byte formats, and invariants are specified in
// docs/format.md, which is written to be sufficient on its own. Three
// invariants govern the package:
//
//   - Determinism. Same archive in, same [Stamp] out, same file name, on any
//     machine. See [Stamp] and [VersionedFileName].
//   - Offline purity. No runtime URL anywhere in a payload; a bundle serves
//     with zero network, forever. See [Reader.Validate].
//   - Asymmetric conventions. Producers are strict and readers are lenient
//     about attribute vocabulary; readers refuse unknown *format versions*
//     outright. See [Manifest.Validate] and format/semconv.
//
// The package depends on the Go standard library alone. A third party who
// knows nothing of Atlas the application can import it and read a bundle.
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
	// versions it does not know rather than guessing at them.
	//
	// Version 2 renamed the format's concepts -- game to volume, map to world,
	// variant to lens -- through the manifest and payload keys alike. Version
	// 3 unified what a world holds: the payload's groups, zones, and pins
	// became one collections array of point, path, and area features, and the
	// manifest counts each kind instead of counting only pins.
	FormatVersion = 3

	// ManifestName is the entry the whole bundle is read through. A writer
	// places it first in the archive so a listing shows it first, though a
	// reader finds it by name wherever it sits.
	ManifestName = "atlas.json"

	// WorldsPrefix, TilesPrefix, and IconsPrefix are the three trees a bundle
	// holds beside its manifest.
	WorldsPrefix = "worlds/"
	TilesPrefix  = "tiles/"
	IconsPrefix  = "icons/"
)

// The three entries one world's payload splits into. Each world's parts are
// named for its slug: worlds/<slug>.json, .bin, and .text.
const (
	// WorldSuffix names the world payload proper: lenses, collections,
	// inline shape geometry, attributes, merge ledger. Read when the world
	// opens.
	WorldSuffix = ".json"
	// PackedSuffix names the ATLASLOC packed point locations. Read when the
	// world opens; see [PackLocations].
	PackedSuffix = ".bin"
	// TextSuffix names the per-feature prose, links, and attributes, keyed by
	// feature id. Read one feature at a time, when a card opens.
	TextSuffix = ".text"
)

// WorldEntryName is the archive entry holding one part of a world's payload:
// WorldEntryName("overworld", WorldSuffix) is "worlds/overworld.json".
func WorldEntryName(slug, suffix string) string {
	return WorldsPrefix + slug + suffix
}

// Manifest is what a bundle says about itself: which volume it is, which
// build of that volume's data it carries, and which worlds are inside.
//
// The JSON encoding of this struct is normative. Its field order, its tags,
// and which fields are omitted when empty all feed the stamp, so a change
// here changes every stamp -- which is why the shape is frozen with the
// format version.
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
// two bundles naming the same slug are two builds of the same volume, however
// their files are named.
type Volume struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Version distinguishes builds of the same volume. The stamp is a content
// fingerprint -- two bundles with equal stamps hold the same data -- and
// CreatedAt orders them, so a newer capture dropped beside an older one wins.
// Revision orders builds of the same capture: the data has not moved, but the
// policy that turns a capture into a bundle has, and the build under the newer
// policy is the one that should serve. A bundle from before the field existed
// reads as revision zero and loses to any revised build.
//
// CreatedAt is the newest capture time across the volume's worlds, never the
// build time. That is what lets the same archive built anywhere, any time,
// carry the same version and the same file name.
type Version struct {
	Stamp     string `json:"stamp"`
	CreatedAt string `json:"createdAt"`
	Revision  int    `json:"revision,omitempty"`
}

// TileGrid is the window this volume's worlds are cut from: the zoom the
// source was captured at, where the first tile sits, and how big a tile and
// the world square are. A world cut from a window of its own overrides part of
// this in its own payload.
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
// entries and is the world's identity everywhere.
//
// Points, Paths, and Areas count the world's features by kind -- every
// feature is exactly one of the three -- so a listing can say how much a
// world holds without opening its payload, and validation can hold the
// payload to the promise.
type WorldEntry struct {
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	Parent     string     `json:"parent,omitempty"`
	IconOutset string     `json:"iconOutset,omitempty"`
	Center     Coordinate `json:"center"`
	Points     int        `json:"points"`
	Paths      int        `json:"paths"`
	Areas      int        `json:"areas"`
	UpdatedAt  string     `json:"updatedAt"`
}

// Features counts everything the world holds, across the three kinds.
func (e WorldEntry) Features() int { return e.Points + e.Paths + e.Areas }

// Validate reports the first thing structurally wrong with the manifest. It
// checks the manifest alone; whether the entries it promises exist is
// [Reader.Validate], which has the archive in hand.
//
// An unknown format version is refused outright: a reader that guesses at a
// layout it does not know is worse than one that says so.
func (m Manifest) Validate() error {
	if m.Format != Format {
		return fmt.Errorf("format is %q, not %q", m.Format, Format)
	}
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("format version is %d, and this reads %d", m.FormatVersion, FormatVersion)
	}
	if err := ValidSlug(m.Volume.Slug); err != nil {
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
		if err := ValidSlug(entry.Slug); err != nil {
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

// ValidSlug admits the names the format uses as identifiers: lowercase
// letters, digits, hyphen, and underscore, not leading with a separator.
//
// The rule exists because a slug is used unescaped as both a path segment and
// a URL segment. There is no reason for an identifier to need more, and
// everything a hostile name could smuggle -- separators, dots that climb --
// needs more.
func ValidSlug(slug string) error {
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
