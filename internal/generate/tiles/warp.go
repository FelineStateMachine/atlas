package tiles

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/generate/doc"
)

// Warping keeps the lesser picture.
//
// When two sources picture one ground, one raster is usually the finer -- a
// publisher's own map beside a wiki's rasterized in-game rendering -- and a
// registry that simply served the finer one would throw the other away. Instead
// the lesser raster is resampled into the finer one's world and offered as one
// more picture of the same ground: nothing is discarded, both rasters answer to
// one grid, and every feature lands on either.
//
// # What this file does not decide
//
// It does not fit the alignment. Deciding that two readings picture one ground,
// and how, stands on the names they share, and that is alignment work the enrich
// lane owns (issue #5 §5.3) -- the same machinery cross-source merge stands on,
// written once. What arrives here is the fitted transformation as six numbers,
// an input to the derivation exactly as the donor's tiles are. That is why the
// stamp carries it: the same donor through a different transformation is a
// different picture, and a pyramid that quietly kept serving the old alignment
// would be wrong in a way nothing could see.
//
// The seam that fits an affine and hands it here is cmd/atlas, which is the one
// place both lanes are in scope. Neither lane imports the other.

// PlanWarp decides the pyramid a donor picture becomes once it is resampled
// into a base picture's world.
//
// Two of its judgements are worth naming. The variant renders at the base-frame
// zoom nearest the donor's real resolution after the alignment's own scaling --
// deep enough to keep everything the donor drew, never so deep as to invent
// detail that was never there. And the plan lists exactly one captured level,
// the donor's deepest complete one, because that is what the warp samples: a
// stamp that named the donor's other levels would rebuild for a change that
// could not reach these tiles.
func PlanWarp(base, donor Plan, affine Affine, lensName string) Plan {
	effective := float64(WorldDepth(donor)) * affine.Scale()
	targetZoom := int(math.Round(math.Log2(effective / TileSize)))
	targetZoom = max(0, min(targetZoom, base.MaxFullZoom-base.Frame.BaseZoom))

	return Plan{
		TileSet: donor.TileSet + "=>" + base.TileSet,
		Name:    base.Name + "__aligned-" + doc.Slugify(donor.TileSet),
		// The variant is cut from the base picture's window, which is what
		// lets a reader switch between the two without the ground moving.
		Frame:         base.Frame,
		Levels:        map[int][]Tile{donor.MaxFullZoom: donor.Levels[donor.MaxFullZoom]},
		MaxFullZoom:   targetZoom + base.Frame.BaseZoom,
		MaxSourceZoom: targetZoom + base.Frame.BaseZoom,
		// A resample is a new picture rather than a copy of an old one, so it
		// is written afresh as a photograph: smoothed when magnified, and
		// encoded the way every level this deriver writes is encoded.
		Format:      "jpg",
		Interpolate: true,
		LensName:    lensName,
		AlignedWith: base.TileSet,
		Warp: &Warp{
			Donor:      &donor,
			Base:       &base,
			Affine:     affine,
			TargetZoom: targetZoom,
		},
	}
}

// WorldDepth is the finest resolution a plan draws its world at, in pixels
// across the world square. It is what decides which of two readings is the
// picture worth aligning the other onto.
func WorldDepth(p Plan) int {
	return TileSize * (1 << (p.MaxFullZoom - p.Frame.BaseZoom))
}

// Settle takes the collisions out of a set of planned names.
//
// Two sources capturing one volume name the same ground the same thing, so
// their pyramids would land in one directory. Every colliding plan -- all of
// them, never just the later one -- takes its publisher's own path into its
// name, so which pyramid is called what does not depend on the order an archive
// happened to list its captures in.
//
// It runs before anything is stamped, because a pyramid's name is part of what
// it is derived as, and before any warp is planned, because a warped variant is
// named after the picture it aligns onto.
func Settle(plans []Plan) {
	byName := make(map[string][]int, len(plans))
	for index := range plans {
		byName[plans[index].Name] = append(byName[plans[index].Name], index)
	}
	for _, colliders := range byName {
		if len(colliders) < 2 {
			continue
		}
		for _, at := range colliders {
			plans[at].Name += "__" + doc.Slugify(plans[at].TileSet)
		}
	}
}

// deriveWarp renders the donor through the alignment into the base frame at the
// target zoom, then folds the shallower levels down exactly as every other
// pyramid is folded.
func deriveWarp(root string, plan Plan) (Pyramid, error) {
	warp := plan.Warp
	donor := warp.Donor
	captured := donor.Levels[donor.MaxFullZoom]

	// Wherever the donor never drew, the donor's own background stands in: a
	// warped tile is opaque, and painting it black would put a hole in a
	// picture that simply ends there.
	backgroundHex, err := placeholderColor(captured, placeholder(captured))
	if err != nil {
		return Pyramid{}, err
	}
	background := parseHex(backgroundHex)

	sampler, err := newSampler(donor, background)
	if err != nil {
		return Pyramid{}, err
	}
	inverse, ok := warp.Affine.Invert()
	if !ok {
		return Pyramid{}, fmt.Errorf("pyramid %s: the alignment is singular", plan.Name)
	}

	// Where the donor's content lands in the base world, and the tiles that
	// covers at the zoom the variant renders at.
	bounds := warpedBounds(donor.Bounds, warp.Affine)
	first, last := warpedWindow(bounds, warp.TargetZoom)

	targetZoom := warp.TargetZoom
	levelPixels := TileSize * (1 << targetZoom)
	span := float64(WorldSize) / float64(levelPixels)
	formats := make([]string, targetZoom+1)
	coverage := make(map[string]*Coverage)
	mask := &coverageMask{total: (1 << targetZoom) * (1 << targetZoom)}

	for tileY := first[1]; tileY <= last[1]; tileY++ {
		for tileX := first[0]; tileX <= last[0]; tileX++ {
			raster := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
			drew := false
			for y := range TileSize {
				for x := range TileSize {
					baseX := (float64(tileX*TileSize+x) + 0.5) * span
					baseY := (float64(tileY*TileSize+y) + 0.5) * span
					donorX, donorY := inverse.Apply(baseX, baseY)
					sampled, drawn := sampler.at(donorX, donorY)
					if drawn {
						drew = true
					}
					raster.SetNRGBA(x, y, sampled)
				}
			}
			// A tile the donor never reached is not written at all: coverage
			// says nothing about it, and a reader falls back to the parent.
			if !drew {
				continue
			}
			if err := encode(tilePath(root, plan.Name, targetZoom, tileX, tileY, "jpg"),
				raster, "jpg"); err != nil {
				return Pyramid{}, err
			}
			mask.mark(tileX, tileY)
		}
	}
	formats[targetZoom] = "jpg"
	if built := mask.build(); built != nil {
		coverage[strconv.Itoa(targetZoom)] = built
	}

	for localZoom := targetZoom - 1; localZoom >= 0; localZoom-- {
		built, err := deriveLevel(root, plan.Name, localZoom, formats[localZoom+1], "jpg", false, background)
		if err != nil {
			return Pyramid{}, err
		}
		formats[localZoom] = "jpg"
		if built != nil {
			coverage[strconv.Itoa(localZoom)] = built
		}
	}
	if len(coverage) == 0 {
		coverage = nil
	}

	sourceZoom, firstTile := plan.Frame.Window()
	return Pyramid{
		TileSet:    plan.TileSet,
		Name:       plan.Name,
		MinZoom:    0,
		MaxZoom:    targetZoom,
		FullZoom:   targetZoom,
		SourceZoom: targetZoom + plan.Frame.BaseZoom,
		Window:     Window{SourceZoom: sourceZoom, FirstTile: firstTile},
		Formats:    formats,
		// The bounds are the warped content's, as measured, whether or not
		// that comes to the whole square: what the alignment reached is a fact
		// about this picture rather than a default that can be left out.
		Bounds:      bounds,
		Interpolate: true,
		Background:  backgroundHex,
		Coverage:    coverage,
		LensName:    plan.LensName,
		AlignedWith: plan.AlignedWith,
	}, nil
}

// warpedBounds sends the donor's content box through the alignment and clips it
// to the base world.
func warpedBounds(box *Box, affine Affine) *Box {
	if box == nil {
		box = &Box{Width: WorldSize, Height: WorldSize}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [4][2]float64{
		{float64(box.X), float64(box.Y)},
		{float64(box.X + box.Width), float64(box.Y)},
		{float64(box.X), float64(box.Y + box.Height)},
		{float64(box.X + box.Width), float64(box.Y + box.Height)},
	} {
		x, y := affine.Apply(corner[0], corner[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	minX, minY = math.Max(minX, 0), math.Max(minY, 0)
	maxX, maxY = math.Min(maxX, WorldSize), math.Min(maxY, WorldSize)
	if maxX <= minX || maxY <= minY {
		return &Box{}
	}
	return &Box{
		X:      int(minX),
		Y:      int(minY),
		Width:  int(math.Ceil(maxX)) - int(minX),
		Height: int(math.Ceil(maxY)) - int(minY),
	}
}

// warpedWindow is the tile window one level covers a box with, clipped to the
// level.
func warpedWindow(box *Box, zoom int) (first, last [2]int) {
	levelPixels := TileSize * (1 << zoom)
	span := float64(WorldSize) / float64(levelPixels)
	edge := (1 << zoom) - 1
	return [2]int{
			max(0, int(float64(box.X)/span/TileSize)),
			max(0, int(float64(box.Y)/span/TileSize)),
		}, [2]int{
			min(edge, int(float64(box.X+box.Width)/span/TileSize)),
			min(edge, int(float64(box.Y+box.Height)/span/TileSize)),
		}
}

// sampler reads a donor's deepest complete level as one continuous picture,
// decoding tiles as the warp first touches them and holding a bounded number of
// them decoded. A whole level is far larger than the warp of it.
type sampler struct {
	files      map[[2]int]string
	decoded    map[[2]int]*image.NRGBA
	order      [][2]int
	origin     int
	pixelScale float64 // level pixels per world pixel
	levelSpan  int     // level pixels across the level
	background color.NRGBA
}

// sampled is how many decoded tiles the sampler holds at once.
const sampled = 256

func newSampler(donor *Plan, background color.Color) (*sampler, error) {
	files := make(map[[2]int]string)
	for _, tile := range donor.Levels[donor.MaxFullZoom] {
		files[[2]int{tile.Ref.X, tile.Ref.Y}] = tile.Path
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("donor %s holds no complete level to resample", donor.TileSet)
	}
	r, g, b, _ := background.RGBA()
	return &sampler{
		files:      files,
		decoded:    make(map[[2]int]*image.NRGBA),
		origin:     donor.Frame.Origin(donor.MaxFullZoom),
		pixelScale: math.Pow(2, float64(donor.MaxFullZoom-donor.Frame.BaseZoom-worldZoomLevels)),
		levelSpan:  TileSize * (1 << (donor.MaxFullZoom - donor.Frame.BaseZoom)),
		background: color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff},
	}, nil
}

// at reads the donor at a world coordinate, bilinearly, clamped within the tile
// that holds it. The second result says whether the donor drew here at all:
// outside the level, or over a tile the capture never held, it did not, and the
// background stands in.
func (s *sampler) at(worldX, worldY float64) (color.NRGBA, bool) {
	pixelX, pixelY := worldX*s.pixelScale, worldY*s.pixelScale
	if pixelX < 0 || pixelY < 0 ||
		pixelX >= float64(s.levelSpan) || pixelY >= float64(s.levelSpan) {
		return s.background, false
	}
	tileX, tileY := int(pixelX)/TileSize, int(pixelY)/TileSize
	raster, held := s.tile(tileX, tileY)
	if !held {
		return s.background, false
	}
	// Sampling is clamped inside the tile rather than reaching into its
	// neighbour: a seam a pixel wide is invisible, and a decoder held open for
	// every neighbour of every sample is not.
	localX := pixelX - float64(tileX*TileSize) - 0.5
	localY := pixelY - float64(tileY*TileSize) - 0.5
	x0 := clamp(int(math.Floor(localX)), 0, TileSize-1)
	y0 := clamp(int(math.Floor(localY)), 0, TileSize-1)
	x1, y1 := clamp(x0+1, 0, TileSize-1), clamp(y0+1, 0, TileSize-1)
	fx := math.Max(0, math.Min(1, localX-float64(x0)))
	fy := math.Max(0, math.Min(1, localY-float64(y0)))

	blend := func(c00, c10, c01, c11 uint8) uint8 {
		top := float64(c00)*(1-fx) + float64(c10)*fx
		bottom := float64(c01)*(1-fx) + float64(c11)*fx
		return uint8(top*(1-fy) + bottom*fy + 0.5)
	}
	p00, p10 := raster.NRGBAAt(x0, y0), raster.NRGBAAt(x1, y0)
	p01, p11 := raster.NRGBAAt(x0, y1), raster.NRGBAAt(x1, y1)
	return color.NRGBA{
		R: blend(p00.R, p10.R, p01.R, p11.R),
		G: blend(p00.G, p10.G, p01.G, p11.G),
		B: blend(p00.B, p10.B, p01.B, p11.B),
		A: 0xff,
	}, true
}

func (s *sampler) tile(x, y int) (*image.NRGBA, bool) {
	key := [2]int{x + s.origin, y + s.origin}
	if raster, seen := s.decoded[key]; seen {
		return raster, raster != nil
	}
	path, exists := s.files[key]
	if !exists {
		s.hold(key, nil)
		return nil, false
	}
	value, err := decode(path)
	if err != nil {
		// A tile that will not decode is a tile the donor did not draw. The
		// warp is a picture, and one unreadable capture must not stop it.
		s.hold(key, nil)
		return nil, false
	}
	raster := nrgba(value)
	s.hold(key, raster)
	return raster, true
}

func (s *sampler) hold(key [2]int, raster *image.NRGBA) {
	if len(s.order) >= sampled {
		delete(s.decoded, s.order[0])
		s.order = s.order[1:]
	}
	s.decoded[key] = raster
	s.order = append(s.order, key)
}

// nrgba is an image as straight-alpha bytes, copied only where it is not one
// already.
func nrgba(value image.Image) *image.NRGBA {
	if typed, ok := value.(*image.NRGBA); ok {
		return typed
	}
	bounds := value.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := value.At(x, y).RGBA()
			out.SetNRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.NRGBA{
				R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff,
			})
		}
	}
	return out
}

func clamp(value, low, high int) int { return max(low, min(high, value)) }
