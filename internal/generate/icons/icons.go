// Package icons carries the standard icon library a source may name instead of
// shipping artwork of its own.
//
// Some publishers have no artwork at all. The IAU Gazetteer names two thousand
// places on Mars and draws none of them; a city's open data publishes geometry
// and attributes and nothing to put on a pin. What those sources do have is a
// vocabulary -- a crater, a mountain, a monument -- so instead of inventing
// pictures they name one, through the registered atlas.icon.std attribute, and
// composition makes good on the name here.
//
// The library is a curated subset of Mapbox's Maki set, public-domain map
// glyphs vendored so that resolution is as offline as everything else. Only the
// names some source actually speaks are vendored: the embed is the vocabulary,
// and a name outside it fails the build one table edit before it would have
// shipped a bundle with a hole in its legend.
//
// A resolved standard icon is one more asset in a bundle's icons tree, tinted
// and haloed like any glyph a source shipped itself. The application never
// learns this package exists.
//
// # Naming a glyph is not choosing one
//
// This package resolves what a source already named. Deciding that a collection
// which named nothing should carry a standard glyph -- and which -- is a
// judgement made after two sources have been folded together, and belongs to the
// enrich lane's stdicons enricher (issue #5 §5.3). The two meet at the
// attribute: an enricher writes the name, and packing it is the same work as
// packing any other artwork.
package icons

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed standard/maki/*.svg standard/maki/LICENSE.txt
var standard embed.FS

// Standard resolves a set/name reference from the atlas.icon.std attribute to
// the glyph's bytes and the asset name it ships under.
//
// The asset name spells its provenance -- std--maki-mountain.svg -- so a
// bundle's icon listing reads honestly, and a source's own artwork can never be
// shadowed by a standard glyph that happened to share its key.
func Standard(ref string) (data []byte, asset string, err error) {
	set, name, found := strings.Cut(ref, "/")
	if !found || set != "maki" {
		return nil, "", fmt.Errorf("standard icon %q names no vendored set", ref)
	}
	data, err = standard.ReadFile("standard/maki/" + name + ".svg")
	if err != nil {
		return nil, "", fmt.Errorf("standard icon %q is not vendored", ref)
	}
	return data, "std--maki-" + name + ".svg", nil
}
