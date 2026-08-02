package app

import (
	"strings"

	"github.com/FelineStateMachine/atlas/internal/app/cells"
)

// heldCell is the question the session's grid asks of every point: is it
// inside the cell being held. It is nil when no grid is open, when no cell is
// held inside it, or when the session's system does not divide this world.
//
// A grid narrows what stands the same way a highlight does
// (docs/render-seam.md §4.2, rule 4), and the count above the panel beside the
// map has to be the count of what the map is drawing -- so the application has
// to be able to answer "is this feature inside the held cell" without asking
// the seam, and it has to be able to answer it for whichever system the
// session holds. Answering for one system and shrugging at the other is the
// shape of the disagreement: the seam narrows the drawn set, the server
// narrows nothing, and the two counts part company while both are sure. The
// arithmetic itself, both systems of it, and why there is a Go copy at all, is
// internal/app/cells.
//
// The last case -- a system this world does not offer -- is a session that has
// outlived its world: a reader holding an S2 cell on a sphere and then opening
// a plane in the same volume. Nothing narrows, because the address names no
// ground here. applyGrid refuses to *enter* that state; this is what the
// filter does about a record already in it.
func heldCell(session Session, model *worldModel, lens *payloadLens) cells.Held {
	if session.Grid.System == "" || session.Grid.Cell == "" {
		return nil
	}
	var attrs map[string]string
	size := 0.0
	if model != nil {
		attrs, size = model.Attrs, float64(model.Grid.Size)
	}
	if !cells.Applicable(attrs, session.Grid.System) {
		return nil
	}
	switch session.Grid.System {
	case cells.SystemGeohash:
		var surface, bounds *cells.Rect
		if lens != nil {
			surface, bounds = lens.Surface, lens.Bounds
		}
		return cells.GeohashHeld(cells.SurfaceExtent(surface, bounds, size), session.Grid.Cell)
	case cells.SystemS2:
		// The ground S2 divides is the body itself rather than the lens's
		// window on it, which is why nothing here reads the surface: the
		// address is a place on a sphere, and the flattening the world
		// declares is the whole of what puts it back on the raster. That
		// there *is* one was settled above -- this reads it.
		mapping, mapped := cells.MappingOf(attrs)
		if !mapped {
			return nil
		}
		return cells.S2Held(mapping, session.Grid.Cell)
	}
	return nil
}

// THE NAVIGATOR'S VOCABULARY, per system.
//
// Everything below answers the same question the seam's `CellSystem` answers
// about itself -- what the field keeps of a keystroke, when the draft has
// become a place, what is one level out, how the control names it -- for the
// two systems a session can hold. It is the server's half of
// analysis/cellsystems, and it is here rather than in internal/app/cells for
// one reason: a normalization is a *field* concern, not arithmetic, and the
// port says so in as many words (S2ParseCell's doc: "minus the keystroke
// normalization, which belongs to the field and not to the server").
//
// WHY THE SERVER NORMALIZES AT ALL. In the reference the field normalized as
// the reader typed and the session only ever saw addresses. Here the field is
// an <input> posting its raw value, and the record is the only place an
// address can be made canonical -- so the server does what the field used to,
// and the field is left to be an input. The alternative is a session holding
// "M6X!", a seam that would have held "m6x", and two halves of one page
// dividing different ground.

// normalizeCell is what the navigator's field keeps of a keystroke: the
// characters this system spells addresses with, lowercased, cut at the depth
// its telescope stops at. Nothing here refuses -- a reader mid-word is not
// making a mistake, and what they have typed so far is either a place or a
// draft, which is [parseCell]'s question rather than this one's.
func normalizeCell(system, text string) string {
	keep, depth := "", 0
	switch system {
	case cells.SystemGeohash:
		keep, depth = cells.GeohashAlphabet, cells.GeohashMaxDepth
	case cells.SystemS2:
		keep, depth = "0123456789abcdef", cells.S2InputLength
	default:
		return ""
	}
	out := make([]rune, 0, depth)
	for _, character := range strings.ToLower(text) {
		if !strings.ContainsRune(keep, character) {
			continue
		}
		if out = append(out, character); len(out) == depth {
			break
		}
	}
	return string(out)
}

// parseCell asks whether a normalized draft has become a place, and answers
// the canonical spelling of it.
//
// The two systems answer differently, and the difference is the contract's
// "whole-or-nothing" rule. A geohash address refines by appending a character,
// so every prefix of one is itself an address and parsing never refuses: the
// reader travels on every keystroke. An S2 token is a Hilbert index, and half
// of one names nothing at all -- so a partial token is a DRAFT the field holds
// while the session stays exactly where it was, and only a whole token moves
// the reader. A token past the floor clamps to it and comes back respelled.
func parseCell(system, text string) (string, bool) {
	switch system {
	case cells.SystemGeohash:
		return text, true
	case cells.SystemS2:
		return cells.S2ParseCell(text)
	}
	return "", false
}

// parentCell is one level out of an address, per the system holding it.
//
// This is the line the telescope used to get wrong, and it is worth saying why
// out loud: an address is opaque, and only its own system knows how to shorten
// it. Geohash refines by appending a character, so its parent is the address
// with the last character taken off. S2 does not -- the parent of `47a1cb` is
// `47a1`, three characters shorter and spelled differently -- so taking a
// character off an S2 token produces either a different cell somewhere else on
// the body or no cell at all. Both were wrong in the same quiet way: the
// reader pressed Escape and arrived somewhere they had never been.
func parentCell(system, cell string) string {
	switch system {
	case cells.SystemGeohash:
		runes := []rune(cell)
		if len(runes) == 0 {
			return ""
		}
		return string(runes[:len(runes)-1])
	case cells.SystemS2:
		return cells.S2Parent(cell)
	}
	return ""
}

// systemName is how the navigator names a system in prose, and systemMark is
// the mark its cycle button wears. Both are the registry's own spellings
// (analysis/cellsystems: `name` and `short`), because the button and the field
// label are read beside a seam that draws the same words.
func systemName(system string) string {
	switch system {
	case cells.SystemGeohash:
		return "Geohash"
	case cells.SystemS2:
		return "S2"
	}
	return ""
}

func systemMark(system string) string {
	switch system {
	case cells.SystemGeohash:
		return "G"
	case cells.SystemS2:
		return "S2"
	}
	return ""
}

// inputLength is how many characters the navigator's field accepts, which is
// the depth this system's telescope stops at. It is the same number
// [normalizeCell] cuts at, and it is spelled on the input as well so that the
// field and the record agree in front of the reader rather than behind them.
func inputLength(system string) int {
	switch system {
	case cells.SystemGeohash:
		return cells.GeohashMaxDepth
	case cells.SystemS2:
		return cells.S2InputLength
	}
	return 0
}

// nextSystem is the one the cycle button offers: the next this world divides
// by, wrapping. A system the world does not offer -- or a list of one -- has no
// next, which is the empty answer and the hidden button.
func nextSystem(offered []string, system string) string {
	if len(offered) < 2 {
		return ""
	}
	for at, candidate := range offered {
		if candidate == system {
			return offered[(at+1)%len(offered)]
		}
	}
	return ""
}

// currentLens is the lens the session has open, or the world's first, which is
// the one a world opens on when nobody has chosen. It is the same choice
// [App.view] makes when it assembles the page; a grid dividing a different
// lens's ground from the one being drawn would divide the wrong rectangle on a
// split sheet.
func currentLens(model *worldModel, session Session) *payloadLens {
	if model == nil {
		return nil
	}
	for at := range model.Lenses {
		entry := &model.Lenses[at]
		if entry.Name == session.Lens || (session.Lens == "" && at == 0) {
			return entry
		}
	}
	return nil
}

// gridGround is the rectangle the systems divide: the lens's declared surface
// where it has one, its raster window otherwise, and the whole world square
// where it declares neither (internal/app/cells, SurfaceExtent).
func gridGround(c *concernContext) cells.Extent {
	var surface, bounds *cells.Rect
	size := 0.0
	if c.world != nil {
		size = float64(c.world.Grid.Size)
		if lens := currentLens(c.world, *c.session); lens != nil {
			surface, bounds = lens.Surface, lens.Bounds
		}
	}
	return cells.SurfaceExtent(surface, bounds, size)
}
