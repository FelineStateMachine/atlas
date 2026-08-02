package tiles

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/FelineStateMachine/atlas/internal/generate/archive"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// Deriving a pyramid begins by deciding what is there, which is what a plan is.
// Nothing is read out of a raster to make one: the archive says which tiles
// answered and what their bytes hash to, the source says which tiles a level was
// supposed to hold, and between the two the whole shape of the pyramid is
// settled -- how deep it goes, which level is the one to trust, what encoding
// each level carries, and the ground it actually draws.
//
// That is also why a plan can be stamped without touching a tile. See stamp.go.

// worldZoomLevels is how far the world square sits above the level that holds a
// picture in one tile: 8192 units of world across, 256 to a tile, so five
// halvings. It is what makes one unit of world one pixel of the reference level,
// whichever level that turns out to be.
const worldZoomLevels = 5

// Frame is where a picture sits in its publisher's tile grid.
//
// Publishers do not agree on it. The maps captured most recently are cut from a
// 32-tile square at zoom 13; one older game is cut from a square at the origin
// whose deepest level is 43 tiles across and whose shallowest is 3. Both are the
// same kind of pyramid seen from a different height, so no one height is
// assumed: each picture is measured, and the zoom whose window collapses into a
// single tile is the floor its local zooms are counted from.
type Frame struct {
	// BaseZoom is the publisher's zoom that holds the whole window in one tile,
	// and BaseTile is that tile. Local zoom 0 is this level, so a picture's
	// local zooms are its publisher zooms less BaseZoom.
	BaseZoom int
	BaseTile int
}

// Origin is the first tile of a publisher's level: the base tile, doubled back
// up the pyramid the same way the window was halved down it.
func (f Frame) Origin(zoom int) int { return f.BaseTile << (zoom - f.BaseZoom) }

// Window is where this picture's world space sits in the publisher's grid, in
// the terms a reader needs to place a feature: the zoom whose pixels are the
// world's units, and the first tile of the window at that zoom. A picture in the
// corpus's shared window lands back on zoom 13 and tile 4064.
func (f Frame) Window() (sourceZoom, firstTile int) {
	reference := f.BaseZoom + worldZoomLevels
	return reference, f.Origin(reference)
}

// Tile is one captured raster in a plan: where the publisher put it, what its
// bytes hash to, where they sit, and how they are encoded.
type Tile struct {
	Ref    archive.TileRef
	Path   string
	Format string
}

// Plan is one pyramid, decided but not yet built.
type Plan struct {
	// TileSet is the publisher's path for the picture, and Name is the
	// directory the derived pyramid lands in.
	TileSet string
	Name    string
	// Frame is where the picture sits in the publisher's grid.
	Frame Frame
	// Levels is every usable captured level, by the publisher's zoom. Levels at
	// or below MaxFullZoom cover the picture completely; deeper ones may be
	// partial and are carried as extra detail on top of the complete pyramid.
	Levels map[int][]Tile
	// MaxFullZoom is the deepest level whose expected tiles are all present --
	// the one level the pyramid is folded down from. MaxSourceZoom is the
	// deepest usable level of any kind.
	MaxFullZoom   int
	MaxSourceZoom int
	// Format is the encoding the publisher serves, and Interpolate says whether
	// the raster is smoothed when magnified: false is pixel art, which is
	// reduced nearest-neighbour and normalized to lossless PNG.
	Format      string
	Interpolate bool
	// Bounds is the part of the world square the picture actually draws, or nil
	// where it draws all of it.
	Bounds *Box
	// LensName and AlignedWith mark a warped variant. An ordinary plan leaves
	// both empty.
	LensName    string
	AlignedWith string
	// Warp, when set, makes this a derived plan: a donor picture resampled into
	// another picture's world so both rasters answer to one grid.
	Warp *Warp
}

// Warp says how a donor picture lands in a base picture's world.
//
// Two sources picturing one ground publish two rasters in two spaces. A warp is
// the second one resampled into the first's, so a reader can switch between them
// without the ground moving: the alignment is fitted from the names the two
// sources share, and the fit is as much an input to the derivation as the tiles
// are -- the same donor through a different transformation is a different
// picture, which is why the stamp carries it.
type Warp struct {
	Donor *Plan
	Base  *Plan
	// Affine sends donor world pixels onto base world pixels.
	Affine Affine
	// TargetZoom is the local zoom in the base frame the warp renders at.
	TargetZoom int
}

// Affine is the transformation a warp resamples through: x' = AX*x + BX*y + CX,
// y' = AY*x + BY*y + CY.
//
// Fitting one is not this lane's work -- it stands on the names two readings
// share, which is the enrich lane's alignment (issue #5 §5.3), and a warp
// receives the fitted six numbers as an input. What lives here is only the
// arithmetic of using one, which a resampler cannot do without and which is not
// alignment: applying it, inverting it, and asking how far it stretches.
type Affine struct {
	AX, BX, CX float64
	AY, BY, CY float64
}

// Apply sends a donor-space point into the base picture's space.
func (a Affine) Apply(x, y float64) (float64, float64) {
	return a.AX*x + a.BX*y + a.CX, a.AY*x + a.BY*y + a.CY
}

// Invert solves the transformation the other way, which is the direction a
// resampler reads in: every base pixel asks where in the donor it came from.
// The false result is a degenerate transformation, through which nothing may be
// resampled.
func (a Affine) Invert() (Affine, bool) {
	determinant := a.AX*a.BY - a.BX*a.AY
	if math.Abs(determinant) < 1e-12 {
		return Affine{}, false
	}
	out := Affine{
		AX: a.BY / determinant, BX: -a.BX / determinant,
		AY: -a.AY / determinant, BY: a.AX / determinant,
	}
	out.CX = -(out.AX*a.CX + out.BX*a.CY)
	out.CY = -(out.AY*a.CX + out.BY*a.CY)
	return out, true
}

// Scale reports how many base pixels one donor pixel spans, averaged over the
// axes. It is what decides how deep a warp is worth rendering.
func (a Affine) Scale() float64 {
	return (math.Hypot(a.AX, a.AY) + math.Hypot(a.BX, a.BY)) / 2
}

// ErrNoFrame marks a lens the deriver cannot plan: one whose source declared no
// frame, or whose frame has no square window. It is a refusal rather than a
// guess, because a picture measured against the wrong window derives a pyramid
// that draws a map in one place and its features in another.
var ErrNoFrame = errors.New("this picture declares no frame the deriver can measure")

// PlanLens decides what a lens's pyramid will be, from what the source declared
// and what the archive actually holds.
//
// The complete-level rule is the whole judgement. The deepest level whose
// expected tiles are all present is the one the pyramid is folded down from;
// every shallower level is derived from it rather than taken from whatever
// intermediate levels the capture happens to hold, because those are separately
// encoded pictures of varying completeness and a reduction of the level we
// already trust is both more consistent and never leaves a level empty. Partial
// levels above it are carried while they stay contiguous: a reader falls back to
// the parent tile wherever coverage says nothing, but it cannot jump across a
// missing resolution.
func PlanLens(
	a *archive.Archive,
	world archive.WorldRef,
	name string,
	lens doc.Lens,
	captured map[int][]archive.TileRef,
	interpolate bool,
) (Plan, error) {
	if lens.Frame == nil {
		return Plan{}, fmt.Errorf("%w: %s", ErrNoFrame, lens.TileSet)
	}
	frame, ok := frameOf(*lens.Frame)
	if !ok {
		return Plan{}, fmt.Errorf("%w: %s has no square window", ErrNoFrame, lens.TileSet)
	}

	levels := make(map[int][]Tile)
	maxFullZoom, maxSourceZoom := -1, -1
	for zoom, refs := range captured {
		if zoom < frame.BaseZoom || zoom > lens.Frame.MaxZoom {
			continue
		}
		tiles, full, err := readLevel(a, world, *lens.Frame, zoom, refs)
		if err != nil {
			return Plan{}, err
		}
		if len(tiles) == 0 {
			continue
		}
		levels[zoom] = tiles
		maxSourceZoom = max(maxSourceZoom, zoom)
		if full {
			maxFullZoom = max(maxFullZoom, zoom)
		}
	}
	if maxFullZoom < frame.BaseZoom {
		return Plan{}, fmt.Errorf("%w: %s has no complete captured level", ErrNoFrame, lens.TileSet)
	}
	// A partial level is only usable if every level beneath it is there;
	// otherwise a reader would jump across a missing resolution.
	for zoom := maxFullZoom + 1; zoom <= maxSourceZoom; zoom++ {
		if len(levels[zoom]) == 0 {
			maxSourceZoom = zoom - 1
			break
		}
	}

	return Plan{
		TileSet:       lens.TileSet,
		Name:          name,
		Frame:         frame,
		Levels:        levels,
		MaxFullZoom:   maxFullZoom,
		MaxSourceZoom: maxSourceZoom,
		Format:        archive.NormalizeFormat(lens.Frame.Format),
		Interpolate:   interpolate,
		Bounds:        boundsOf(levels[maxFullZoom], maxFullZoom, frame),
	}, nil
}

// frameOf halves a declared window until it is a single tile.
func frameOf(declared doc.Frame) (Frame, bool) {
	window := expected(declared, declared.MaxZoom)
	for shift := 0; shift <= declared.MaxZoom; shift++ {
		x, y := window.MinX>>shift, window.MinY>>shift
		if x != window.MaxX>>shift || y != window.MaxY>>shift {
			continue
		}
		// A reader places a feature from one origin for both axes, so a window
		// whose axes come to rest on different tiles would draw the picture in
		// one place and its features in another. Nothing in the archive is
		// shaped that way; a picture that is says so rather than being silently
		// a tile out.
		if x != y {
			return Frame{}, false
		}
		return Frame{BaseZoom: declared.MaxZoom - shift, BaseTile: x}, true
	}
	return Frame{}, false
}

// expected is the tile window one level was supposed to hold. A level the source
// says nothing about is measured against the corpus's shared square, which is
// where every picture cut from a 32-tile square at zoom 13 lands.
func expected(declared doc.Frame, zoom int) doc.TileWindow {
	if window, ok := declared.Windows[decimal(zoom)]; ok {
		return window
	}
	minimum, maximum := sharedWindow(zoom)
	return doc.TileWindow{MinX: minimum, MinY: minimum, MaxX: maximum, MaxY: maximum}
}

// The corpus's shared window, and the reference zoom it is measured at. They
// live here rather than in curation because they are what an *undeclared* window
// falls back to during derivation -- the same numbers curation names, read at a
// different moment.
const (
	sharedZoom      = 13
	sharedFirstTile = 4064
	sharedTiles     = WorldSize / TileSize
)

// The corpus's grid: 256-pixel tiles and an 8192-pixel world square.
const (
	TileSize  = 256
	WorldSize = 8192
)

func sharedWindow(zoom int) (first, last int) {
	if zoom <= sharedZoom {
		shift := sharedZoom - zoom
		return sharedFirstTile >> shift, ((sharedFirstTile + sharedTiles) >> shift) - 1
	}
	shift := zoom - sharedZoom
	return sharedFirstTile << shift, ((sharedFirstTile + sharedTiles) << shift) - 1
}

// readLevel collects every captured tile of one publisher level that is inside
// the window the level was supposed to fill, and reports whether the level fills
// it completely. Partial levels are still returned so they can be carried as
// extra detail above the complete pyramid.
func readLevel(
	a *archive.Archive,
	world archive.WorldRef,
	declared doc.Frame,
	zoom int,
	refs []archive.TileRef,
) ([]Tile, bool, error) {
	window := expected(declared, zoom)
	want := (window.MaxX - window.MinX + 1) * (window.MaxY - window.MinY + 1)

	seen := make(map[[2]int]bool, len(refs))
	out := make([]Tile, 0, len(refs))
	for _, ref := range refs {
		key := [2]int{ref.X, ref.Y}
		if seen[key] || ref.X < window.MinX || ref.X > window.MaxX ||
			ref.Y < window.MinY || ref.Y > window.MaxY {
			continue
		}
		path, format, err := a.Raster(world, ref)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		seen[key] = true
		out = append(out, Tile{Ref: ref, Path: path, Format: format})
	}
	return out, want > 0 && len(out) == want, nil
}

// boundsOf is the part of the world square a level actually draws, measured from
// the tiles that are not the level's own background filler.
//
// The window a raster is cut from is not the ground a picture is about. Several
// sheets are drawn inside a printed border or a wide margin of nothing, and
// dividing that whole window up spends most of it on blank paper. Only an
// unambiguous filler tile -- one that is more than half the level -- is treated
// as empty space, so a picture whose real content happens to repeat is never
// punched full of holes.
func boundsOf(tiles []Tile, zoom int, frame Frame) *Box {
	filler := placeholder(tiles)
	minX, minY := math.MaxInt, math.MaxInt
	maxX, maxY := -1, -1
	for _, tile := range tiles {
		if filler != "" && tile.Ref.ContentHash == filler {
			continue
		}
		minX, minY = min(minX, tile.Ref.X), min(minY, tile.Ref.Y)
		maxX, maxY = max(maxX, tile.Ref.X), max(maxY, tile.Ref.Y)
	}
	if maxX < minX || maxY < minY {
		return nil
	}
	// Bounds are reported in world space, which is sized at this picture's own
	// reference zoom -- the level whose pixels are its units. A shallower level
	// covers more world per tile and a deeper one covers less, so the tile pitch
	// scales both ways.
	reference := frame.BaseZoom + worldZoomLevels
	origin := frame.Origin(zoom)
	pitch, divisor := TileSize, 1
	if zoom < reference {
		pitch = TileSize << (reference - zoom)
	} else if zoom > reference {
		divisor = 1 << (zoom - reference)
	}
	box := &Box{
		X:      (minX - origin) * pitch / divisor,
		Y:      (minY - origin) * pitch / divisor,
		Width:  (maxX - minX + 1) * pitch / divisor,
		Height: (maxY - minY + 1) * pitch / divisor,
	}
	if box.X == 0 && box.Y == 0 && box.Width == WorldSize && box.Height == WorldSize {
		return nil
	}
	return box
}

// placeholder is the content hash of a level's background tile, or "" when no
// single tile holds a majority. A level with no clear filler is drawn whole.
func placeholder(tiles []Tile) string {
	counts := make(map[string]int, len(tiles))
	var dominant string
	for _, tile := range tiles {
		counts[tile.Ref.ContentHash]++
		if counts[tile.Ref.ContentHash] > counts[dominant] {
			dominant = tile.Ref.ContentHash
		}
	}
	if dominant == "" || counts[dominant] <= len(tiles)/2 {
		return ""
	}
	return dominant
}

func decimal(zoom int) string {
	if zoom < 0 {
		return fmt.Sprint(zoom)
	}
	// Small, hot, and always a plain decimal: the zooms in play are 0..25.
	const digits = "0123456789"
	if zoom < 10 {
		return digits[zoom : zoom+1]
	}
	return fmt.Sprint(zoom)
}
