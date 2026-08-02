package app

import "github.com/FelineStateMachine/atlas/internal/app/cells"

// heldCell is the rectangle the session's grid holds, or nothing when no grid
// is open, no cell is held, or the system is one internal/cells does not
// divide.
//
// A grid narrows what stands the same way a highlight does
// (docs/render-seam.md §4.2, rule 4), and the count above the panel beside the
// map has to be the count of what the map is drawing -- so the application has
// to be able to answer "is this feature inside the held cell" without asking
// the seam. The arithmetic itself, and why there is a Go copy of it at all,
// is internal/cells.
func heldCell(session Session, lens *payloadLens, size float64) *cells.Extent {
	if session.Grid.System != defaultCellSystem || session.Grid.Cell == "" {
		return nil
	}
	var surface, bounds *cells.Rect
	if lens != nil {
		surface, bounds = lens.Surface, lens.Bounds
	}
	held := cells.GeohashExtent(cells.SurfaceExtent(surface, bounds, size), session.Grid.Cell)
	return &held
}
