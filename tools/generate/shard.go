package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Some maps are published as one tile sheet holding several places. Tears of
// the Kingdom stacks three elevations of one Hyrule; the Mojave Wasteland rings
// its main map with eight district insets. In both the split is already in the
// data -- the top-level regions are the pieces, and every location names one --
// so the sheet can be taken apart rather than shown whole.
//
// Which maps those are is declared rather than detected. Breath of the Wild has
// fifteen top-level regions and Palworld forty-two, and those are areas of one
// continuous map: sharding them would shatter it. Nothing in the data separates
// Mojave's nine leaf regions from Hyrule's fifteen, so guessing is not an option.
type shardMode int

const (
	shardNone shardMode = iota
	// shardIntoMaps gives each piece its own entry in the map picker. Use it
	// when the pieces are unrelated places that happen to share a sheet.
	shardIntoMaps
	// shardIntoVariants keeps one map and offers the pieces as layers, so the
	// view survives switching between them. Use it when the pieces are the same
	// ground at different heights.
	shardIntoVariants
)

var shardedMaps = map[int64]shardMode{
	602: shardIntoMaps,     // Mojave Wasteland: eight district insets in the margins
	536: shardIntoVariants, // Hyrule: Sky, Surface and Depths of one world
}

type shard struct {
	Region    rawRegion
	Locations map[int64]bool
	Regions   map[int64]bool
	// Bounds is in the shared world pixel space the tile pyramid uses.
	Bounds contentBounds
	Center coordinate
}

// planShards groups a map's regions and locations under their top-level region.
//
// A piece is sized by the ground its regions cover, not by the locations
// assigned to it. A handful of locations sit outside the region that claims
// them -- forty on the Mojave sheet -- and stretching a piece to reach them
// pulled the main map back over the very insets it was being split away from.
// Locations are not clipped by these bounds, only the raster is, so a stray one
// stays reachable just beyond the edge of its map.
func planShards(raw rawMap, grid tileGrid, mode shardMode) ([]shard, error) {
	regions := make(map[int64]rawRegion, len(raw.Regions))
	for _, region := range raw.Regions {
		regions[region.ID] = region
	}
	rootOf := func(id int64) (int64, bool) {
		seen := make(map[int64]bool)
		for {
			region, ok := regions[id]
			if !ok {
				return 0, false
			}
			if region.ParentRegionID == nil || seen[id] {
				return id, true
			}
			seen[id] = true
			id = *region.ParentRegionID
		}
	}

	byRoot := make(map[int64]*shard)
	take := func(id int64) *shard {
		if existing, ok := byRoot[id]; ok {
			return existing
		}
		created := &shard{
			Region:    regions[id],
			Locations: make(map[int64]bool),
			Regions:   make(map[int64]bool),
		}
		byRoot[id] = created
		return created
	}

	extents := make(map[int64]*[4]float64)
	grow := func(root int64, x, y float64) {
		box, ok := extents[root]
		if !ok {
			box = &[4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			extents[root] = box
		}
		box[0], box[1] = math.Min(box[0], x), math.Min(box[1], y)
		box[2], box[3] = math.Max(box[2], x), math.Max(box[3], y)
	}

	for _, region := range raw.Regions {
		root, ok := rootOf(region.ID)
		if !ok {
			continue
		}
		piece := take(root)
		piece.Regions[region.ID] = true
		for _, feature := range region.Features {
			for _, point := range flattenCoordinates(feature.Geometry.Coordinates) {
				x, y := projectPoint(point[1], point[0], grid)
				grow(root, x, y)
			}
		}
	}

	var orphans int
	placed := make(map[int64][2]float64)
	for _, group := range raw.Groups {
		for _, category := range group.Categories {
			for _, location := range category.Locations {
				if location.RegionID == nil {
					orphans++
					continue
				}
				root, ok := rootOf(*location.RegionID)
				if !ok {
					orphans++
					continue
				}
				latitude, err := number(location.Latitude)
				if err != nil {
					return nil, err
				}
				longitude, err := number(location.Longitude)
				if err != nil {
					return nil, err
				}
				take(root).Locations[location.ID] = true
				placed[location.ID] = [2]float64{projectX(longitude, grid), projectY(latitude, grid)}
				_, _ = latitude, longitude
			}
		}
	}
	if orphans > 0 {
		return nil, fmt.Errorf("%d locations belong to no region, so they would vanish when split", orphans)
	}

	// Barely any air. The insets on the Mojave sheet sit about twenty pixels
	// apart, so a generous margin would show a slice of the neighbouring panel
	// along the edge of each one.
	const padding = 8
	pieces := make([]shard, 0, len(byRoot))
	for root, piece := range byRoot {
		box := extents[root]
		if box == nil || math.IsInf(box[0], 1) {
			continue
		}
		left := math.Max(0, box[0]-padding)
		top := math.Max(0, box[1]-padding)
		right := math.Min(float64(grid.Size), box[2]+padding)
		bottom := math.Min(float64(grid.Size), box[3]+padding)
		// Bounds are measured down from the top of the world, matching how the
		// viewer clips the raster.
		piece.Bounds = contentBounds{
			X:      int(left),
			Y:      int(top),
			Width:  int(right - left),
			Height: int(bottom - top),
		}
		piece.Center = coordinate{
			Latitude:  unprojectLatitude((top+bottom)/2, grid),
			Longitude: unprojectLongitude((left+right)/2, grid),
		}
		pieces = append(pieces, *piece)
	}
	for index := range pieces {
		var strays int
		for id := range pieces[index].Locations {
			at, ok := placed[id]
			b := pieces[index].Bounds
			if !ok {
				continue
			}
			if at[0] < float64(b.X) || at[0] > float64(b.X+b.Width) ||
				at[1] < float64(b.Y) || at[1] > float64(b.Y+b.Height) {
				strays++
			}
		}
		if strays > 0 {
			fmt.Printf("   %s: %d of %d locations sit outside the region that claims them\n",
				pieces[index].Region.Title, strays, len(pieces[index].Locations))
		}
	}
	orderShards(pieces, mode)
	if len(pieces) < 2 {
		return nil, fmt.Errorf("only %d top-level region, nothing to split", len(pieces))
	}
	return pieces, nil
}

// orderShards puts the pieces in the order they are offered. Layers read down
// the way they are stacked, so the sky comes before the ground beneath it.
// Separate places lead with the largest, so a sheet's main map heads its insets.
func orderShards(pieces []shard, mode shardMode) {
	sort.Slice(pieces, func(a, b int) bool {
		if mode == shardIntoVariants {
			if pieces[a].Bounds.Y != pieces[b].Bounds.Y {
				return pieces[a].Bounds.Y < pieces[b].Bounds.Y
			}
			return pieces[a].Region.Title < pieces[b].Region.Title
		}
		areaA := pieces[a].Bounds.Width * pieces[a].Bounds.Height
		areaB := pieces[b].Bounds.Width * pieces[b].Bounds.Height
		if areaA != areaB {
			return areaA > areaB
		}
		return pieces[a].Region.Title < pieces[b].Region.Title
	})
}

func flattenCoordinates(raw json.RawMessage) [][2]float64 {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var out [][2]float64
	var walk func(any)
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

// projectPoint mirrors the viewer's projection so bounds land in the same world
// pixel space the tile pyramid and the raster clip already use.
func projectPoint(latitude, longitude float64, grid tileGrid) (float64, float64) {
	return projectX(longitude, grid), projectY(latitude, grid)
}

func projectX(longitude float64, grid tileGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	xTile := ((longitude + 180) / 360) * worldTiles
	return (xTile - float64(grid.FirstTile)) * float64(grid.TileSize)
}

func projectY(latitude float64, grid tileGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	yTile := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * worldTiles
	return (yTile - float64(grid.FirstTile)) * float64(grid.TileSize)
}

func unprojectLongitude(x float64, grid tileGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	xTile := x/float64(grid.TileSize) + float64(grid.FirstTile)
	return (xTile/worldTiles)*360 - 180
}

func unprojectLatitude(y float64, grid tileGrid) float64 {
	worldTiles := math.Pow(2, float64(grid.SourceZoom))
	yTile := y/float64(grid.TileSize) + float64(grid.FirstTile)
	return math.Atan(math.Sinh(math.Pi*(1-2*yTile/worldTiles))) * 180 / math.Pi
}

// splitMap takes a sheet apart when one is declared for it, and otherwise
// hands back the single map it was given.
func splitMap(m catalogMap, raw rawMap, grid tileGrid) ([]catalogMap, error) {
	mode := shardedMaps[m.ID]
	if mode == shardNone {
		return []catalogMap{m}, nil
	}
	pieces, err := planShards(raw, grid, mode)
	if err != nil {
		return nil, fmt.Errorf("split %s: %w", m.Title, err)
	}
	if mode == shardIntoVariants {
		return []catalogMap{asVariants(m, pieces)}, nil
	}
	return asMaps(m, pieces), nil
}

// asMaps gives each piece its own map. The largest keeps the sheet's name and
// leads; the rest are named after it so they sort together in the picker
// instead of scattering among the game's standalone maps.
func asMaps(m catalogMap, pieces []shard) []catalogMap {
	out := make([]catalogMap, 0, len(pieces))
	for index, piece := range pieces {
		copied := m
		copied.Groups = keepLocations(m.Groups, piece.Locations)
		copied.Zones = keepZones(m.Zones, piece.Regions)
		copied.PinCount = countLocations(copied.Groups)
		copied.Center = piece.Center
		copied.Variants = boundVariants(m.Variants, piece.Bounds, 0)
		if index > 0 {
			copied.ID = piece.Region.ID
			copied.Title = m.Title + " — " + piece.Region.Title
			copied.Slug = m.Slug + "-" + slugify(piece.Region.Title)
			copied.Parent = m.Slug
		}
		out = append(out, copied)
	}
	return out
}

// asVariants keeps one map and offers the pieces as layers. Locations and zones
// carry the piece they belong to so the viewer can show one layer at a time,
// and the view survives switching because the map itself never changes.
func asVariants(m catalogMap, pieces []shard) catalogMap {
	var variants []variant
	for _, piece := range pieces {
		bound := boundVariants(m.Variants, piece.Bounds, piece.Region.ID)
		for index := range bound {
			bound[index].Name = piece.Region.Title
		}
		variants = append(variants, bound...)
		for groupIndex := range m.Groups {
			categories := m.Groups[groupIndex].Categories
			for categoryIndex := range categories {
				locations := categories[categoryIndex].Locations
				for locationIndex := range locations {
					if piece.Locations[locations[locationIndex].ID] {
						locations[locationIndex].Shard = piece.Region.ID
					}
				}
			}
		}
		for zoneIndex := range m.Zones {
			if piece.Regions[m.Zones[zoneIndex].ID] {
				m.Zones[zoneIndex].Shard = piece.Region.ID
			}
		}
	}
	m.Variants = variants
	if len(pieces) > 0 {
		m.Center = pieces[0].Center
	}
	return m
}

// boundVariants clips each raster to the piece. The tiles are the same pyramid
// either way; only the window onto it changes.
func boundVariants(variants []variant, bounds contentBounds, shardID int64) []variant {
	out := make([]variant, 0, len(variants))
	for _, source := range variants {
		clipped := source
		clipped.Bounds = &contentBounds{
			X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height,
		}
		clipped.Shard = shardID
		out = append(out, clipped)
	}
	return out
}

func keepLocations(groups []catalogGroup, wanted map[int64]bool) []catalogGroup {
	out := make([]catalogGroup, 0, len(groups))
	for _, group := range groups {
		kept := make([]catalogCategory, 0, len(group.Categories))
		for _, category := range group.Categories {
			locations := make([]catalogLocation, 0, len(category.Locations))
			for _, location := range category.Locations {
				if wanted[location.ID] {
					locations = append(locations, location)
				}
			}
			if len(locations) == 0 {
				continue
			}
			category.Locations = locations
			kept = append(kept, category)
		}
		if len(kept) == 0 {
			continue
		}
		group.Categories = kept
		out = append(out, group)
	}
	return out
}

func keepZones(zones []zone, wanted map[int64]bool) []zone {
	out := make([]zone, 0, len(zones))
	for _, z := range zones {
		if wanted[z.ID] {
			out = append(out, z)
		}
	}
	return out
}

func countLocations(groups []catalogGroup) int {
	total := 0
	for _, group := range groups {
		for _, category := range group.Categories {
			total += len(category.Locations)
		}
	}
	return total
}

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
