package compose

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/FelineStateMachine/atlas/internal/generate/curation"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

// Some publishers print several separate places on one sheet. A game rings its
// main map with district insets; another stacks three elevations of one ground
// side by side. In both the split is already in the data -- the world's
// top-level areas are the pieces, and every point names the area it stands on --
// so a sheet can be taken apart rather than shown whole.
//
// Which sheets those are is declared in curation and never detected. Plenty of
// worlds have fifteen or forty top-level areas that are districts of one
// continuous ground, and splitting one of those would shatter it. Nothing in a
// document separates the two cases, so guessing is not an option and a wrong
// guess divides a whole map into pieces nobody asked for.
//
// Nothing here knows a source. A piece is planned from the interchange
// document's own vocabulary -- area features, their parents, and the areas point
// features declare themselves members of -- so a second source describing the
// same sheet the same way splits the same way.

// splitReach is how far a piece may grow into the empty space around it: far
// enough to reach a title banner printed beside the ground it names, short
// enough that an isolated piece does not swallow the sheet around it.
const splitReach = 256

// piece is one part of a split sheet: the area that defines it, what belongs to
// it, and the window onto the sheet that draws it.
type piece struct {
	// Area is the top-level area feature the piece is cut around.
	Area composedFeature
	// Features is every feature id the piece keeps -- points by membership,
	// areas by ancestry.
	Points map[int64]bool
	Areas  map[int64]bool
	// Bounds is the window onto the sheet, in world pixels, grown into the
	// space around the piece so a title printed beside it comes along.
	Bounds tiles.Box
	// Surface is the same box before it was grown: the ground the piece's areas
	// actually cover, without the margin the raster keeps.
	Surface tiles.Box
	Center  doc.Position
}

// splitWorld takes a curated sheet apart, and otherwise hands back the single
// world it was given.
//
// The two modes answer two different questions about a sheet. Pieces that are
// unrelated places sharing a sheet become worlds of their own, each in the
// picker; pieces that are the same ground at different heights stay one world
// and become lenses, so the view survives switching between them.
func splitWorld(w composedWorld, mode curation.ShardMode, grid surfaceGrid) ([]composedWorld, error) {
	if mode == curation.ShardNone {
		return []composedWorld{w}, nil
	}
	pieces, err := planPieces(w, grid, mode)
	if err != nil {
		return nil, err
	}
	if mode == curation.ShardIntoLenses {
		return []composedWorld{asLenses(w, pieces)}, nil
	}
	return asWorlds(w, pieces), nil
}

// planPieces groups a world's areas and points under their top-level area.
//
// A piece is sized by the ground its areas cover, not by the points assigned to
// it. A handful of points sit outside the area that claims them -- forty on one
// sheet in the corpus -- and stretching a piece to reach them pulls the main map
// back over the very insets it is being split away from. Points are not clipped
// by these bounds, only the raster is, so a stray one stays reachable just
// beyond the edge of its map.
func planPieces(w composedWorld, grid surfaceGrid, mode curation.ShardMode) ([]piece, error) {
	areas := make(map[int64]composedFeature)
	for _, collection := range w.Collections {
		if collection.Kind != doc.KindArea {
			continue
		}
		for _, feature := range collection.Features {
			areas[feature.ID] = feature
		}
	}
	// rootOf walks an area up to the top of its family. A cycle stops where it
	// started rather than spinning: a malformed parent chain is somebody else's
	// data problem and not a reason to hang.
	rootOf := func(id int64) (int64, bool) {
		seen := make(map[int64]bool)
		for {
			area, held := areas[id]
			if !held {
				return 0, false
			}
			if area.Parent == 0 || seen[id] {
				return id, true
			}
			seen[id] = true
			id = area.Parent
		}
	}

	byRoot := make(map[int64]*piece)
	take := func(id int64) *piece {
		if existing, held := byRoot[id]; held {
			return existing
		}
		created := &piece{
			Area:   areas[id],
			Points: make(map[int64]bool),
			Areas:  make(map[int64]bool),
		}
		byRoot[id] = created
		return created
	}

	extents := make(map[int64]*[4]float64)
	grow := func(root int64, x, y float64) {
		box, held := extents[root]
		if !held {
			box = &[4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			extents[root] = box
		}
		box[0], box[1] = math.Min(box[0], x), math.Min(box[1], y)
		box[2], box[3] = math.Max(box[2], x), math.Max(box[3], y)
	}

	for _, collection := range w.Collections {
		if collection.Kind != doc.KindArea {
			continue
		}
		for _, area := range collection.Features {
			root, ok := rootOf(area.ID)
			if !ok {
				continue
			}
			part := take(root)
			part.Areas[area.ID] = true
			for _, geometry := range area.Geometry {
				for _, point := range flattenCoordinates(geometry.Coordinates) {
					grow(root, projectX(point[0], grid), projectY(point[1], grid))
				}
			}
		}
	}

	var orphans int
	placed := make(map[int64][2]float64)
	for _, collection := range w.Collections {
		if collection.Kind != doc.KindPoint {
			continue
		}
		for _, point := range collection.Features {
			if point.Member == 0 || point.At == nil {
				orphans++
				continue
			}
			root, ok := rootOf(point.Member)
			if !ok {
				orphans++
				continue
			}
			take(root).Points[point.ID] = true
			placed[point.ID] = [2]float64{
				projectX(point.At.Lng, grid),
				projectY(point.At.Lat, grid),
			}
		}
	}
	if orphans > 0 {
		return nil, fmt.Errorf(
			"%d point features belong to no area, so they would vanish when the sheet is split", orphans)
	}

	// Only a piece with ground under it can be cut out: an area family that
	// outlines nothing has no window to give.
	boxes := make([][4]float64, 0, len(byRoot))
	order := make([]int64, 0, len(byRoot))
	for _, collection := range w.Collections {
		if collection.Kind != doc.KindArea {
			continue
		}
		for _, area := range collection.Features {
			part, held := byRoot[area.ID]
			if !held || part.Area.ID != area.ID {
				continue
			}
			box := extents[area.ID]
			if box == nil || math.IsInf(box[0], 1) {
				continue
			}
			boxes = append(boxes, *box)
			order = append(order, area.ID)
		}
	}
	grown := growIntoGaps(boxes, float64(grid.Size))

	pieces := make([]piece, 0, len(order))
	for index, root := range order {
		part := byRoot[root]
		left, top, right, bottom := grown[index][0], grown[index][1], grown[index][2], grown[index][3]
		// Bounds are measured down from the top of the world, matching how a
		// reader clips the raster.
		part.Bounds = tiles.Box{
			X:      int(left),
			Y:      int(top),
			Width:  int(right - left),
			Height: int(bottom - top),
		}
		box := boxes[index]
		part.Surface = tiles.Box{
			X:      int(box[0]),
			Y:      int(box[1]),
			Width:  int(box[2] - box[0]),
			Height: int(box[3] - box[1]),
		}
		part.Center = doc.Position{
			Lat: unprojectLat((top+bottom)/2, grid),
			Lng: unprojectLng((left+right)/2, grid),
		}
		pieces = append(pieces, *part)
	}
	rehomeStrays(pieces, placed)
	orderPieces(pieces, mode)
	if len(pieces) < 2 {
		return nil, fmt.Errorf("only %d top-level area, so there is nothing to split", len(pieces))
	}
	return pieces, nil
}

// growIntoGaps widens each piece into the empty space around it, stopping
// halfway to whatever it would otherwise run into.
//
// An area outlines the ground it covers, not the art drawn around it. A sheet
// that titles each of its pieces with a banner sitting just above the outline
// gets the words sliced in half by a crop to the outline alone. Claiming the
// space between pieces takes the banner with the piece it names, while stopping
// at the midpoint keeps panels twenty pixels apart from showing a slice of their
// neighbour.
func growIntoGaps(boxes [][4]float64, world float64) [][4]float64 {
	grown := make([][4]float64, len(boxes))
	for index, box := range boxes {
		room := [4]float64{splitReach, splitReach, splitReach, splitReach}
		for other, rival := range boxes {
			if other == index {
				continue
			}
			// Only pieces sharing a span on the other axis can be run into.
			if spansOverlap(box[1], box[3], rival[1], rival[3]) {
				if rival[2] <= box[0] {
					room[0] = math.Min(room[0], (box[0]-rival[2])/2)
				}
				if rival[0] >= box[2] {
					room[2] = math.Min(room[2], (rival[0]-box[2])/2)
				}
			}
			if spansOverlap(box[0], box[2], rival[0], rival[2]) {
				if rival[3] <= box[1] {
					room[1] = math.Min(room[1], (box[1]-rival[3])/2)
				}
				if rival[1] >= box[3] {
					room[3] = math.Min(room[3], (rival[1]-box[3])/2)
				}
			}
		}
		grown[index] = [4]float64{
			math.Max(0, box[0]-room[0]),
			math.Max(0, box[1]-room[1]),
			math.Min(world, box[2]+room[2]),
			math.Min(world, box[3]+room[3]),
		}
	}
	separate(grown, boxes)
	return grown
}

// separate undoes the growth where two pieces met in a corner. Pieces sharing no
// span on either axis do not hold each other back, so both can expand into the
// same diagonal space. Whichever way they now overlap least is the way they are
// eased apart.
//
// Only the growth is ever given back. Easing two pieces apart used to be free to
// cut into the ground a piece was cut for, which took the bottom border off a
// panel: the crop stopped inside the frame the panel is drawn with. Where two
// pieces genuinely cover the same ground they are left overlapping, because that
// is the sheet saying so, not the crop.
func separate(boxes, floors [][4]float64) {
	for pass := 0; pass < len(boxes); pass++ {
		clear := true
		for a := range boxes {
			for b := a + 1; b < len(boxes); b++ {
				across := math.Min(boxes[a][2], boxes[b][2]) - math.Max(boxes[a][0], boxes[b][0])
				down := math.Min(boxes[a][3], boxes[b][3]) - math.Max(boxes[a][1], boxes[b][1])
				if across <= 0 || down <= 0 {
					continue
				}
				clear = false
				if across <= down {
					give(&boxes[a], &boxes[b], across/2, 0)
				} else {
					give(&boxes[a], &boxes[b], down/2, 1)
				}
				hold(&boxes[a], floors[a])
				hold(&boxes[b], floors[b])
			}
		}
		if clear {
			return
		}
	}
}

// hold keeps a box around the ground it was grown from.
func hold(box *[4]float64, floor [4]float64) {
	box[0] = math.Min(box[0], floor[0])
	box[1] = math.Min(box[1], floor[1])
	box[2] = math.Max(box[2], floor[2])
	box[3] = math.Max(box[3], floor[3])
}

// give pulls two boxes back from each other along one axis, axis 0 across and
// axis 1 down.
func give(a, b *[4]float64, by float64, axis int) {
	low, high := axis, axis+2
	if a[low] < b[low] {
		a[high] -= by
		b[low] += by
		return
	}
	b[high] -= by
	a[low] += by
}

func spansOverlap(lowA, highA, lowB, highB float64) bool {
	return lowA < highB && lowB < highA
}

// rehomeStrays moves a point to the piece it is actually drawn on.
//
// Some points name an area they do not sit in: on one sheet in the corpus forty
// claim the main map while lying inside the panels around it. Trusting the claim
// left them floating in the dark beside the map they were split from, exactly
// where their panel used to be. Where a point sits is not in doubt, so position
// decides and the claim is treated as the mistake it is.
func rehomeStrays(pieces []piece, placed map[int64][2]float64) {
	inside := func(part piece, at [2]float64) bool {
		b := part.Bounds
		return at[0] >= float64(b.X) && at[0] <= float64(b.X+b.Width) &&
			at[1] >= float64(b.Y) && at[1] <= float64(b.Y+b.Height)
	}
	for index := range pieces {
		for id := range pieces[index].Points {
			at, known := placed[id]
			if !known || inside(pieces[index], at) {
				continue
			}
			hosts := make([]int, 0, 2)
			for other := range pieces {
				if other != index && inside(pieces[other], at) {
					hosts = append(hosts, other)
				}
			}
			// Only an unambiguous home is worth moving to: a point inside two
			// overlapping pieces has not told us anything the claim did not.
			if len(hosts) != 1 {
				continue
			}
			delete(pieces[index].Points, id)
			pieces[hosts[0]].Points[id] = true
		}
	}
}

// orderPieces puts the pieces in the order they are offered. Lenses read down
// the way they are stacked, so the sky comes before the ground beneath it.
// Separate places lead with the largest, so a sheet's main map heads its insets.
func orderPieces(pieces []piece, mode curation.ShardMode) {
	sort.Slice(pieces, func(a, b int) bool {
		if mode == curation.ShardIntoLenses {
			if pieces[a].Bounds.Y != pieces[b].Bounds.Y {
				return pieces[a].Bounds.Y < pieces[b].Bounds.Y
			}
			return pieces[a].Area.Title < pieces[b].Area.Title
		}
		areaA := pieces[a].Bounds.Width * pieces[a].Bounds.Height
		areaB := pieces[b].Bounds.Width * pieces[b].Bounds.Height
		if areaA != areaB {
			return areaA > areaB
		}
		return pieces[a].Area.Title < pieces[b].Area.Title
	})
}

// asWorlds gives each piece its own world. The largest keeps the sheet's name
// and leads; the rest are named after it so they sort together in the picker
// instead of scattering among the volume's standalone worlds.
func asWorlds(w composedWorld, pieces []piece) []composedWorld {
	out := make([]composedWorld, 0, len(pieces))
	for index, part := range pieces {
		copied := w
		copied.Collections = keepAreas(
			keepPoints(w.Collections, part.Points),
			part.Areas, part.Area.ID,
		)
		copied.Center = part.Center
		copied.Lenses = boundLenses(w.Lenses, part.Bounds, part.Surface, 0)
		if index > 0 {
			copied.ID = part.Area.ID
			copied.Title = w.Title + " — " + part.Area.Title
			copied.Slug = w.Slug + "-" + slugify(part.Area.Title)
			copied.Parent = w.Slug
		}
		out = append(out, copied)
	}
	return out
}

// asLenses keeps one world and offers the pieces as lenses. Features carry the
// piece they belong to so a reader can show one at a time, and the view survives
// switching because the world itself never changes.
func asLenses(w composedWorld, pieces []piece) composedWorld {
	// The collections are rewritten in place, so they are copied first: a
	// document's features belong to the document, and marking one piece's must
	// not reach back into it.
	collections := make([]composedCollection, len(w.Collections))
	for index, collection := range w.Collections {
		collection.Features = append([]composedFeature(nil), collection.Features...)
		collections[index] = collection
	}

	var lenses []lens
	// One pyramid draws every lens of a sharded world: the pieces are windows
	// onto the same sheet, so each piece repeats the world's whole tile-set
	// entry list and the two slices stay index for index.
	var pyramids []tiles.Pyramid
	var outlines []int64
	for _, part := range pieces {
		bound := boundLenses(w.Lenses, part.Bounds, part.Surface, part.Area.ID)
		for index := range bound {
			bound[index].Name = part.Area.Title
		}
		lenses = append(lenses, bound...)
		pyramids = append(pyramids, w.Pyramids...)
		for collectionIndex := range collections {
			collection := &collections[collectionIndex]
			claims := part.Areas
			if collection.Kind == doc.KindPoint {
				claims = part.Points
			}
			for featureIndex := range collection.Features {
				if claims[collection.Features[featureIndex].ID] {
					collection.Features[featureIndex].Shard = part.Area.ID
				}
			}
		}
		outlines = append(outlines, part.Area.ID)
	}
	w.Lenses = lenses
	w.Pyramids = pyramids
	w.Collections = keepAreas(collections, nil, outlines...)
	if len(pieces) > 0 {
		w.Center = pieces[0].Center
	}
	return w
}

// boundLenses clips each raster to the piece. The tiles are the same pyramid
// either way; only the window onto it changes. The ground the piece covers
// travels alongside the window, because they are not the same rectangle and
// anything measuring the world rather than drawing it wants the ground.
func boundLenses(lenses []lens, bounds, surface tiles.Box, shard int64) []lens {
	// Easing two pieces apart can pull a window in past the ground it grew
	// from, and what a grid divides has to be drawn, so the ground is clipped to
	// the window rather than reaching beyond it.
	ground := intersectBox(surface, bounds)
	out := make([]lens, 0, len(lenses))
	for _, source := range lenses {
		clipped := source
		window := bounds
		clipped.Bounds = &window
		covered := ground
		clipped.Surface = &covered
		clipped.Shard = shard
		out = append(out, clipped)
	}
	return out
}

// keepPoints pares each point collection down to the piece's own features. A
// collection left with none is dropped, which is also what empties a legend
// heading out of a piece's payload: no collection carries it any more.
func keepPoints(collections []composedCollection, wanted map[int64]bool) []composedCollection {
	out := make([]composedCollection, 0, len(collections))
	for _, collection := range collections {
		if collection.Kind != doc.KindPoint {
			out = append(out, collection)
			continue
		}
		kept := make([]composedFeature, 0, len(collection.Features))
		for _, point := range collection.Features {
			if wanted[point.ID] {
				kept = append(kept, point)
			}
		}
		if len(kept) == 0 {
			continue
		}
		collection.Features = kept
		out = append(out, collection)
	}
	return out
}

// keepAreas drops the piece's own outline. Once an area has been used to cut a
// world or a lens out of a sheet, drawing it again just traces the edge of what
// the reader is already looking at.
func keepAreas(collections []composedCollection, wanted map[int64]bool, drop ...int64) []composedCollection {
	discard := make(map[int64]bool, len(drop))
	for _, id := range drop {
		discard[id] = true
	}
	out := make([]composedCollection, 0, len(collections))
	for _, collection := range collections {
		if collection.Kind == doc.KindPoint {
			out = append(out, collection)
			continue
		}
		kept := make([]composedFeature, 0, len(collection.Features))
		for _, shape := range collection.Features {
			if discard[shape.ID] || (wanted != nil && !wanted[shape.ID]) {
				continue
			}
			kept = append(kept, shape)
		}
		if len(kept) == 0 {
			continue
		}
		collection.Features = kept
		out = append(out, collection)
	}
	return out
}

// slugify is how a piece's title becomes part of a world slug: lower case,
// everything that is not a letter or a digit run together as a hyphen.
func slugify(value string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, value), "-")
}

func unprojectLng(x float64, grid surfaceGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	xTile := x/float64(grid.TileSize) + float64(grid.FirstTile)
	return (xTile/worldTiles)*360 - 180
}

func unprojectLat(y float64, grid surfaceGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	yTile := y/float64(grid.TileSize) + float64(grid.FirstTile)
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}
