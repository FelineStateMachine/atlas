// Warping keeps the lesser picture. When two sources capture the same map,
// one raster is usually finer -- an official scan beside a wiki's rasterized
// in-game rendering -- and the registry would otherwise simply hide the
// lesser one. Instead, the lesser raster is resampled into the finer layer's
// world, using the transformation the two sources' shared named places
// determine, and offered as an additional variant of the same map: nothing
// is discarded, both rasters answer to one grid, and every pin lands on
// either.
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/FelineStateMachine/atlas/internal/blend"
)

// planWarps finds every game captured by more than one source and plans the
// lesser layers' resampling into the finest one's world. Sources do not have
// to agree on how the world divides into maps: whether two captures picture
// the same ground is decided by the fit itself, which refuses pairs that
// share too few named places to align. Plans arrive after asset paths are
// settled, so the warped names stay stable and unique.
func planWarps(pending []pendingPlan) []pendingPlan {
	// One representative layer per map directory: its deepest layer is the
	// picture worth aligning; further layers of the same map are alternate
	// art of the same world, already aligned by their shared window. A
	// capture is the game directory above the maps -- one source's whole
	// account of the game -- and only maps of different captures are ever
	// paired: a single source already divided its world deliberately.
	deepestByDir := make(map[string]int)
	capturesByGame := make(map[string]map[string]bool)
	for index, entry := range pending {
		held, ok := deepestByDir[entry.plan.MapDir]
		if !ok || worldDepth(entry.plan) > worldDepth(pending[held].plan) {
			deepestByDir[entry.plan.MapDir] = index
		}
		slug := entry.plan.GameSlug
		if capturesByGame[slug] == nil {
			capturesByGame[slug] = make(map[string]bool)
		}
		capturesByGame[slug][captureDir(entry.plan)] = true
	}
	byGame := make(map[string][]int)
	var order []string
	for _, index := range deepestByDir {
		slug := pending[index].plan.GameSlug
		if len(capturesByGame[slug]) < 2 {
			continue
		}
		if _, seen := byGame[slug]; !seen {
			order = append(order, slug)
		}
		byGame[slug] = append(byGame[slug], index)
	}
	sort.Strings(order)

	var warps []pendingPlan
	for _, slug := range order {
		group := byGame[slug]
		sort.Slice(group, func(a, b int) bool {
			return pending[group[a]].plan.MapDir < pending[group[b]].plan.MapDir
		})
		base := group[0]
		for _, candidate := range group[1:] {
			if worldDepth(pending[candidate].plan) > worldDepth(pending[base].plan) {
				base = candidate
			}
		}
		for _, donor := range group {
			if donor == base || captureDir(pending[donor].plan) == captureDir(pending[base].plan) {
				continue
			}
			warp, ok := planWarp(&pending[base].plan, &pending[donor].plan, pending[donor].gameTitle)
			if ok {
				warps = append(warps, warp)
			}
		}
	}
	return warps
}

// captureDir names one source's whole account of a game: the directory the
// map directories sit inside.
func captureDir(plan tilePlan) string {
	return filepath.Dir(filepath.Dir(plan.MapDir))
}

// worldDepth is the finest resolution a layer draws its world at, in pixels
// across its own square.
func worldDepth(plan tilePlan) int {
	return tileSize * (1 << (plan.MaxFullZoom - plan.Frame.BaseZoom))
}

func planWarp(base, donor *tilePlan, donorGameTitle string) (pendingPlan, bool) {
	affine, report, err := blend.Fit(donor.Anchors, base.Anchors)
	if err != nil {
		fmt.Printf("skip aligning %s onto %s: %v\n", donor.SourcePath, base.SourcePath, err)
		return pendingPlan{}, false
	}

	// The warp renders at the base-frame zoom nearest the donor's real
	// resolution: deep enough to keep what the donor drew, never so deep as
	// to invent detail that was never there.
	effective := float64(worldDepth(*donor)) * affine.Scale()
	targetZoom := int(math.Round(math.Log2(effective / tileSize)))
	targetZoom = max(0, min(targetZoom, base.MaxFullZoom-base.Frame.BaseZoom))

	fmt.Printf("align %s onto %s: %s, rendered at z%d\n",
		donor.SourcePath, base.SourcePath, report, targetZoom)

	warped := tilePlan{
		AssetPath:       base.AssetPath + "__aligned-" + slugify(donor.SourcePath),
		SourcePath:      donor.SourcePath + "=>" + base.SourcePath,
		Frame:           base.Frame,
		Interpolate:     true,
		MapDir:          donor.MapDir,
		MaxFullZoom:     targetZoom + base.Frame.BaseZoom,
		MaxSourceZoom:   targetZoom + base.Frame.BaseZoom,
		PreferredFormat: "jpg",
		GameSlug:        base.GameSlug,
		MapSlug:         base.MapSlug,
		SetName:         donor.SetName,
		// The donor's deepest complete level is what the warp samples, and
		// listing it here is what lets the stamp notice when those tiles
		// change.
		Levels: map[int][]tileFile{donor.MaxFullZoom: donor.Levels[donor.MaxFullZoom]},
		Warp: &warpSpec{
			Donor:      donor,
			Base:       base,
			Affine:     affine,
			TargetZoom: targetZoom,
		},
	}
	return pendingPlan{gameTitle: donorGameTitle, title: base.MapSlug, plan: warped}, true
}

// buildWarpedPyramid renders the donor through the affine into the base
// frame at the target zoom, then folds the shallower levels down exactly as
// every other pyramid is folded.
func buildWarpedPyramid(root string, plan tilePlan) (lensManifest, error) {
	warp := plan.Warp
	donor := warp.Donor
	donorFiles := donor.Levels[donor.MaxFullZoom]

	// The donor's own background paints everywhere the donor never drew.
	placeholder := placeholderHash(donorFiles)
	backgroundHex, err := placeholderColor(donorFiles, placeholder)
	if err != nil {
		return lensManifest{}, err
	}
	background := parseHexColor(backgroundHex)

	sampler, err := newDonorSampler(donor, background)
	if err != nil {
		return lensManifest{}, err
	}

	// Where the donor's content lands in the base world.
	bounds := warpedBounds(donor, warp.Affine)
	window := tileWindowFor(bounds, warp.TargetZoom)

	targetZoom := warp.TargetZoom
	formats := make([]string, targetZoom+1)
	coverage := make(map[string]*levelCoverage)
	builder := &coverageBuilder{total: (1 << targetZoom) * (1 << targetZoom)}
	inverse, ok := warp.Affine.Invert()
	if !ok {
		return lensManifest{}, fmt.Errorf("alignment is singular")
	}
	// Base world pixels per rendered pixel at the target zoom.
	levelPixels := tileSize * (1 << targetZoom)
	span := float64(worldSize) / float64(levelPixels)

	for tileY := window[1]; tileY <= window[3]; tileY++ {
		for tileX := window[0]; tileX <= window[2]; tileX++ {
			img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
			drewAny := false
			for v := 0; v < tileSize; v++ {
				for u := 0; u < tileSize; u++ {
					baseX := (float64(tileX*tileSize+u) + 0.5) * span
					baseY := (float64(tileY*tileSize+v) + 0.5) * span
					donorX, donorY := inverse.Apply(baseX, baseY)
					sampled, drew := sampler.sample(donorX, donorY)
					if drew {
						drewAny = true
					}
					img.SetNRGBA(u, v, sampled)
				}
			}
			if !drewAny {
				continue
			}
			path := tilePath(root, plan.AssetPath, targetZoom, tileX, tileY, "jpg")
			if err := encodeImage(path, img, "jpg"); err != nil {
				return lensManifest{}, err
			}
			builder.mark(tileX, tileY)
		}
	}
	formats[targetZoom] = "jpg"
	if mask := builder.build(); mask != nil {
		coverage[itoa(targetZoom)] = mask
	}

	for localZoom := targetZoom - 1; localZoom >= 0; localZoom-- {
		mask, err := deriveLevel(root, plan.AssetPath, localZoom, formats[localZoom+1], "jpg", false, background)
		if err != nil {
			return lensManifest{}, err
		}
		formats[localZoom] = "jpg"
		if mask != nil {
			coverage[itoa(localZoom)] = mask
		}
	}
	if len(coverage) == 0 {
		coverage = nil
	}

	return lensManifest{
		SourcePath:  plan.SourcePath,
		AssetPath:   plan.AssetPath,
		MinZoom:     0,
		MaxZoom:     targetZoom,
		FullZoom:    targetZoom,
		SourceZoom:  targetZoom + warp.Base.Frame.BaseZoom,
		Grid:        warp.Base.Frame.grid(),
		Formats:     formats,
		Bounds:      bounds,
		Interpolate: true,
		Background:  backgroundHex,
		Coverage:    coverage,
		Name:        plan.SetName,
		AlignedWith: warp.Base.SourcePath,
	}, nil
}

// warpedBounds sends the donor's content box through the alignment and clips
// it to the base world.
func warpedBounds(donor *tilePlan, affine blend.Affine) *contentBounds {
	box := donor.Bounds
	if box == nil {
		box = &contentBounds{X: 0, Y: 0, Width: worldSize, Height: worldSize}
	}
	corners := [4][2]float64{
		{float64(box.X), float64(box.Y)},
		{float64(box.X + box.Width), float64(box.Y)},
		{float64(box.X), float64(box.Y + box.Height)},
		{float64(box.X + box.Width), float64(box.Y + box.Height)},
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range corners {
		x, y := affine.Apply(corner[0], corner[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	minX, minY = math.Max(minX, 0), math.Max(minY, 0)
	maxX, maxY = math.Min(maxX, worldSize), math.Min(maxY, worldSize)
	if maxX <= minX || maxY <= minY {
		return &contentBounds{}
	}
	return &contentBounds{
		X:      int(minX),
		Y:      int(minY),
		Width:  int(math.Ceil(maxX)) - int(minX),
		Height: int(math.Ceil(maxY)) - int(minY),
	}
}

func tileWindowFor(bounds *contentBounds, zoom int) [4]int {
	levelPixels := tileSize * (1 << zoom)
	span := float64(worldSize) / float64(levelPixels)
	last := (1 << zoom) - 1
	return [4]int{
		max(0, int(float64(bounds.X)/span/tileSize)),
		max(0, int(float64(bounds.Y)/span/tileSize)),
		min(last, int(float64(bounds.X+bounds.Width)/span/tileSize)),
		min(last, int(float64(bounds.Y+bounds.Height)/span/tileSize)),
	}
}

// donorSampler reads the donor's deepest complete level as one continuous
// picture, decoding tiles as the warp first touches them and holding a
// bounded number decoded.
type donorSampler struct {
	files      map[[2]int]string
	decoded    map[[2]int]*image.NRGBA
	order      [][2]int
	origin     int
	pixelScale float64 // donor level pixels per donor world pixel
	levelSpan  int     // donor level pixels across the level
	background color.NRGBA
}

const donorCacheTiles = 256

func newDonorSampler(donor *tilePlan, background color.Color) (*donorSampler, error) {
	files := make(map[[2]int]string)
	for _, file := range donor.Levels[donor.MaxFullZoom] {
		files[[2]int{file.Record.X, file.Record.Y}] = file.Path
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("donor %s has no deepest level", donor.SourcePath)
	}
	r, g, b, _ := background.RGBA()
	return &donorSampler{
		files:      files,
		decoded:    make(map[[2]int]*image.NRGBA),
		origin:     donor.Frame.origin(donor.MaxFullZoom),
		pixelScale: math.Pow(2, float64(donor.MaxFullZoom-donor.Frame.BaseZoom-worldZoomLevels)),
		levelSpan:  tileSize * (1 << (donor.MaxFullZoom - donor.Frame.BaseZoom)),
		background: color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff},
	}, nil
}

// sample reads the donor at a world coordinate, bilinearly, clamped within
// the containing tile. The second result says whether the donor drew here at
// all -- outside the level, or over a tile the capture never held, it did
// not, and the background stands in.
func (s *donorSampler) sample(worldX, worldY float64) (color.NRGBA, bool) {
	pixelX := worldX * s.pixelScale
	pixelY := worldY * s.pixelScale
	if pixelX < 0 || pixelY < 0 || pixelX >= float64(s.levelSpan) || pixelY >= float64(s.levelSpan) {
		return s.background, false
	}
	tileX := int(pixelX) / tileSize
	tileY := int(pixelY) / tileSize
	img, ok := s.tile(tileX, tileY)
	if !ok {
		return s.background, false
	}
	localX := pixelX - float64(tileX*tileSize) - 0.5
	localY := pixelY - float64(tileY*tileSize) - 0.5
	x0 := clampInt(int(math.Floor(localX)), 0, tileSize-1)
	y0 := clampInt(int(math.Floor(localY)), 0, tileSize-1)
	x1 := clampInt(x0+1, 0, tileSize-1)
	y1 := clampInt(y0+1, 0, tileSize-1)
	fx := clampFloat(localX-float64(x0), 0, 1)
	fy := clampFloat(localY-float64(y0), 0, 1)

	blendChannel := func(c00, c10, c01, c11 uint8) uint8 {
		top := float64(c00)*(1-fx) + float64(c10)*fx
		bottom := float64(c01)*(1-fx) + float64(c11)*fx
		return uint8(top*(1-fy) + bottom*fy + 0.5)
	}
	p00 := img.NRGBAAt(x0, y0)
	p10 := img.NRGBAAt(x1, y0)
	p01 := img.NRGBAAt(x0, y1)
	p11 := img.NRGBAAt(x1, y1)
	return color.NRGBA{
		R: blendChannel(p00.R, p10.R, p01.R, p11.R),
		G: blendChannel(p00.G, p10.G, p01.G, p11.G),
		B: blendChannel(p00.B, p10.B, p01.B, p11.B),
		A: 0xff,
	}, true
}

func (s *donorSampler) tile(x, y int) (*image.NRGBA, bool) {
	key := [2]int{x + s.origin, y + s.origin}
	if img, held := s.decoded[key]; held {
		return img, img != nil
	}
	path, exists := s.files[key]
	if !exists {
		s.remember(key, nil)
		return nil, false
	}
	decoded, err := decodeImage(path)
	if err != nil {
		s.remember(key, nil)
		return nil, false
	}
	img := asNRGBA(decoded)
	s.remember(key, img)
	return img, true
}

func (s *donorSampler) remember(key [2]int, img *image.NRGBA) {
	if len(s.order) >= donorCacheTiles {
		delete(s.decoded, s.order[0])
		s.order = s.order[1:]
	}
	s.decoded[key] = img
	s.order = append(s.order, key)
}

func asNRGBA(img image.Image) *image.NRGBA {
	if typed, ok := img.(*image.NRGBA); ok {
		return typed
	}
	bounds := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			out.SetNRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.NRGBA{
				R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff,
			})
		}
	}
	return out
}

func clampInt(value, low, high int) int {
	return max(low, min(high, value))
}

func clampFloat(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
