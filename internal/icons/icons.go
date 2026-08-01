// Package icons carries the standard icon library the ingestion pipeline
// enriches icon-less sources from: a curated subset of Mapbox's Maki set,
// public-domain map glyphs vendored here so the enrichment is as offline as
// everything else. The app never learns this package exists -- a resolved
// standard icon is one more asset in a bundle's icons tree, tinted and
// haloed like any glyph a source shipped itself.
//
// Only the names some translator actually speaks are vendored; the embed is
// the vocabulary, and a name outside it is a build error at resolution
// time, one table edit old.
package icons

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed standard/maki/*.svg standard/maki/LICENSE.txt
var standard embed.FS

// Standard resolves a set/name reference from the atlas.icon.std attribute
// to the icon's bytes and the asset name it ships under. The asset name
// spells its provenance -- std--maki-mountain.svg -- so a bundle listing
// reads honestly and a source's own artwork can never be shadowed by it.
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
