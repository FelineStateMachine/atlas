package compose

import (
	"encoding/json"
	"math"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// surfaceGrid is the world square a lens is cut from: where it sits in the
// source tile grid, and how big it is in pixels.
type surfaceGrid struct {
	SourceZoom int
	FirstTile  int
	TileSize   int
	Size       int
}

// surfaceMargin is how much ground is kept around a world's outermost feature,
// so the last pin sits inside a cell rather than along its edge and a map drawn
// right up to its own border keeps a little of what surrounds it. It is a floor
// of 32 world pixels and otherwise a fiftieth of the longer span.
const (
	surfaceMarginFloor = 32.0
	surfaceMarginShare = 0.02
)

// markSurfaces measures the ground a world actually covers, as against the
// window its raster fills.
//
// Many sheets are drawn inside a printed border or a wide margin of nothing, and
// dividing that whole window into named cells spends most of them on blank
// paper. So what a grid divides is measured from the world's own contents:
// where its features stand, and the ground its shapes outline. That is the
// archive answering for each world rather than a list of worlds to treat
// specially, and it says the same thing about a border whether the border is
// solid or patterned.
func markSurfaces(w *composedWorld, grid surfaceGrid) {
	for index := range w.Lenses {
		lens := &w.Lenses[index]
		window := tiles.Box{Width: grid.Size, Height: grid.Size}
		if lens.Bounds != nil {
			window = *lens.Bounds
		}
		box, ok := contentExtent(w.Collections, lens.Shard, window, grid)
		if !ok {
			continue
		}
		if lens.Surface != nil {
			box = unionBox(*lens.Surface, box)
		}
		box = intersectBox(box, window)
		lens.Surface = &box
	}
}

// contentExtent is where a world's own contents lie, in the world pixel space
// the tiles are cut in and measured down from the top of it, as the raster clip
// is. Anything drawn outside the window is left out of the reckoning: a few
// features sit off the sheet entirely, and one of those would stretch the ground
// back over everything it had just left out.
func contentExtent(collections []composedCollection, shard int64, window tiles.Box, grid surfaceGrid) (tiles.Box, bool) {
	left, top := math.Inf(1), math.Inf(1)
	right, bottom := math.Inf(-1), math.Inf(-1)
	found := false
	grow := func(x, y float64) {
		if x < float64(window.X) || y < float64(window.Y) ||
			x > float64(window.X+window.Width) || y > float64(window.Y+window.Height) {
			return
		}
		left, top = math.Min(left, x), math.Min(top, y)
		right, bottom = math.Max(right, x), math.Max(bottom, y)
		found = true
	}
	for _, collection := range collections {
		for _, feature := range collection.Features {
			if shard != 0 && feature.Shard != shard {
				continue
			}
			if collection.Kind == doc.KindPoint {
				if feature.At == nil {
					continue
				}
				grow(projectX(feature.At.Lng, grid), projectY(feature.At.Lat, grid))
				continue
			}
			for _, part := range feature.Geometry {
				for _, point := range flattenCoordinates(part.Coordinates) {
					grow(projectX(point[0], grid), projectY(point[1], grid))
				}
			}
		}
	}
	if !found {
		return tiles.Box{}, false
	}
	margin := math.Max(surfaceMarginFloor,
		surfaceMarginShare*math.Max(right-left, bottom-top))
	return tiles.Box{
		X:      int(left - margin),
		Y:      int(top - margin),
		Width:  int((right - left) + margin*2),
		Height: int((bottom - top) + margin*2),
	}, true
}

// projectX and projectY mirror the reader's own projection, so bounds land in
// the same world pixel space the tile pyramid and the raster clip already use.
// The volume's coordinates are spherical Mercator against its tile window,
// whether that window pictures a real planet or a game's artwork.
func projectX(lng float64, grid surfaceGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	xTile := ((lng + 180) / 360) * worldTiles
	return (xTile - float64(grid.FirstTile)) * float64(grid.TileSize)
}

func projectY(lat float64, grid surfaceGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	yTile := (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2 * worldTiles
	return (yTile - float64(grid.FirstTile)) * float64(grid.TileSize)
}

// flattenCoordinates walks a GeoJSON coordinate array of any nesting and yields
// every position in it, as [longitude, latitude]. The geometry itself is carried
// opaquely through composition, so this is the one place its shape is looked
// into, and it looks only for pairs of numbers.
func flattenCoordinates(raw json.RawMessage) [][2]float64 {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var out [][2]float64
	var walk func(node any)
	walk = func(node any) {
		list, ok := node.([]any)
		if !ok {
			return
		}
		if len(list) == 2 {
			x, xOK := list[0].(float64)
			y, yOK := list[1].(float64)
			if xOK && yOK {
				out = append(out, [2]float64{x, y})
				return
			}
		}
		for _, child := range list {
			walk(child)
		}
	}
	walk(value)
	return out
}

func unionBox(a, b tiles.Box) tiles.Box {
	left, top := min(a.X, b.X), min(a.Y, b.Y)
	right, bottom := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return tiles.Box{X: left, Y: top, Width: right - left, Height: bottom - top}
}

// intersectBox is the overlap of two rectangles, or the second of them where
// they do not meet at all.
func intersectBox(a, b tiles.Box) tiles.Box {
	left, top := max(a.X, b.X), max(a.Y, b.Y)
	right, bottom := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if right <= left || bottom <= top {
		return b
	}
	return tiles.Box{X: left, Y: top, Width: right - left, Height: bottom - top}
}
