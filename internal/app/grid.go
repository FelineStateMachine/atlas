package app

import "github.com/FelineStateMachine/atlas/internal/app/cells"

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
