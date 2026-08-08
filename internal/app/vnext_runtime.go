package app

import (
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/app/cells"
	"github.com/FelineStateMachine/atlas/internal/app/hostenv"
)

type semanticCache struct {
	mu    sync.Mutex
	held  map[string]vnext.Volume
	order []string
}

func newSemanticCache() *semanticCache {
	return &semanticCache{held: make(map[string]vnext.Volume)}
}

func (cache *semanticCache) load(key string, importVolume func() (vnext.Volume, error)) (vnext.Volume, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if held, ok := cache.held[key]; ok {
		return held, nil
	}
	volume, err := importVolume()
	if err != nil {
		return vnext.Volume{}, err
	}
	cache.order = append(cache.order, key)
	for len(cache.order) > worldsHeld {
		delete(cache.held, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.held[key] = volume
	return volume, nil
}

func (a *App) semanticVolume(source hostenv.Volume) (vnext.Volume, error) {
	info := source.Info()
	key := info.Slug + "@" + vnext.ShortStamp(info.Stamp)
	return a.semantic.load(key, func() (vnext.Volume, error) {
		return source.Semantic(), nil
	})
}

func semanticWorld(volume vnext.Volume, slug string) (vnext.World, bool) {
	for _, world := range volume.Worlds {
		if world.ID == slug {
			return world, true
		}
	}
	return vnext.World{}, false
}

// buildVNextWorld is the native presentation boundary. Stable schema IDs,
// typed properties, semantic relationships and coordinate-space geometry flow
// straight into the app model; no v3 JSON, ATLASLOC table or GeoJSON-shaped
// compatibility object is created along the way.
func buildVNextWorld(world vnext.World, assets []vnext.Asset) (*worldModel, error) {
	space := world.CoordinateSpace
	grid := nativeGrid(space)
	model := &worldModel{
		Slug: world.ID, Space: space, Lenses: nativeRasters(world.RasterPyramids), Attrs: propertiesToAttrs(world.Claims),
		Origin: worldOrigin(world), Grid: grid,
		ByID: map[string]*collectionModel{}, PointByID: map[string]*pointModel{}, ShapeByID: map[string]*shapeModel{},
	}
	sets := make(map[string]vnext.FeatureSet, len(world.FeatureSets))
	for _, set := range world.FeatureSets {
		sets[set.ID] = set
	}
	styles := make(map[string]vnext.Style, len(world.Presentation.Styles))
	for _, style := range world.Presentation.Styles {
		styles[style.ID] = style
	}
	assetPaths := make(map[string]string, len(assets))
	for _, asset := range assets {
		assetPaths[asset.ID] = asset.Path
	}
	layers := append([]vnext.Layer(nil), world.Presentation.Layers...)
	sort.SliceStable(layers, func(i, j int) bool { return layers[i].Order < layers[j].Order })
	for index, layer := range layers {
		set, ok := sets[layer.FeatureSet]
		if !ok {
			return nil, fmt.Errorf("layer %s refers to missing feature set %s", layer.ID, layer.FeatureSet)
		}
		style, ok := styles[layer.Style]
		if !ok {
			return nil, fmt.Errorf("layer %s refers to missing style %s", layer.ID, layer.Style)
		}
		kind := featureSetGeometryKind(set)
		attrs := propertiesToAttrs(set.Claims)
		collection := &collectionModel{
			ID: layer.ID, Title: legendLabel(world.Presentation.Legend, layer.ID, set.Title), Kind: kind,
			Group: layer.Group, Icon: style.Icon, IconAsset: assetPaths[style.IconAsset],
			Color: nativeColor(kind, style), Attrs: attrs, Index: index,
			Curated: layer.LabelPolicy, RenderAs: style.RenderAs, Hidden: !layer.Visible,
		}
		if collection.Curated == "" {
			collection.Curated = semconv.LabelPolicy(kind, attrs)
		}
		if collection.RenderAs == "" {
			collection.RenderAs = semconv.RenderAs(attrs, "")
		}
		model.Members = append(model.Members, collection)
		model.ByID[collection.ID] = collection
		for featureIndex := range set.Features {
			feature := &set.Features[featureIndex]
			if err := addNativeFeature(model, collection, feature, grid); err != nil {
				return nil, err
			}
		}
	}
	for _, shape := range model.Shapes {
		shape.Depth = shapeDepth(model, shape, 0)
	}
	return model, nil
}

func addNativeFeature(
	model *worldModel,
	collection *collectionModel,
	feature *vnext.Feature,
	grid tileGrid,
) error {
	if feature.Geometry.Kind == vnext.GeometryPoint {
		if len(feature.Geometry.Parts) != 1 || len(feature.Geometry.Parts[0].Rings) != 1 || len(feature.Geometry.Parts[0].Rings[0]) != 1 {
			return fmt.Errorf("point %s has an invalid geometry", feature.ID)
		}
		position := feature.Geometry.Parts[0].Rings[0][0]
		pin := &pointModel{
			ID: feature.ID, Title: feature.Title, Coordinates: coordinateLabel(model.Space, grid, position),
			X: position[0], Y: -position[1],
			Shard: feature.Shard, Feature: feature, Collection: collection,
		}
		model.Points = append(model.Points, pin)
		model.PointByID[pin.ID] = pin
		collection.Count++
		return nil
	}
	shape, err := buildNativeShape(feature, collection)
	if err != nil {
		return err
	}
	collection.Shapes = append(collection.Shapes, shape)
	collection.Count++
	model.Shapes = append(model.Shapes, shape)
	model.ShapeByID[shape.ID] = shape
	return nil
}

// coordinateLabel keeps the detail card in the coordinate space the volume
// declares. Raster-era tile planes retain their familiar latitude/longitude
// reading; a native projected or synthetic space shows its own x/y values and
// unit instead of pretending those values are Web Mercator.
func coordinateLabel(space vnext.CoordinateSpace, grid tileGrid, position vnext.Position) string {
	if tilePlane(space.Definition) && grid.SourceZoom > 0 && grid.TileSize > 0 && grid.Size > 0 {
		lat, lng := grid.unproject(position[0], position[1])
		return strconv.FormatFloat(lat, 'f', 6, 64) + ", " +
			strconv.FormatFloat(lng, 'f', 6, 64)
	}
	label := strconv.FormatFloat(position[0], 'f', 6, 64) + ", " +
		strconv.FormatFloat(position[1], 'f', 6, 64)
	if space.Unit != "" {
		label += " " + space.Unit
	}
	return label
}

func nativeGrid(space vnext.CoordinateSpace) tileGrid {
	grid := tileGrid{SourceZoom: int(space.SourceZoom), FirstTile: int(space.FirstTile), TileSize: int(space.TileSize), Size: int(space.Size)}
	if grid.TileSize == 0 {
		grid.TileSize = 256
	}
	if grid.Size == 0 {
		width := space.Extent[2] - space.Extent[0]
		height := space.Extent[3] - space.Extent[1]
		grid.Size = int(math.Ceil(math.Max(width, height)))
	}
	return grid
}

func tilePlane(definition string) bool {
	return definition == "atlas:tile-plane" || strings.HasPrefix(definition, "atlas:v3/tile-grid")
}

func buildNativeShape(feature *vnext.Feature, collection *collectionModel) (*shapeModel, error) {
	shape := &shapeModel{
		ID: feature.ID, Title: feature.Title, Subtitle: feature.Subtitle, Shard: feature.Shard,
		HasText: hasNativeText(feature), Attrs: propertiesToAttrs(feature.Properties), Feature: feature, Collection: collection,
		MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1),
	}
	shape.Parent = relationshipTarget(feature.Relationships, "within")
	for _, part := range feature.Geometry.Parts {
		switch feature.Geometry.Kind {
		case vnext.GeometryPolygon:
			polygon := make([][]point, 0, len(part.Rings))
			for _, ring := range part.Rings {
				polygon = append(polygon, shape.nativePoints(ring))
			}
			if len(polygon) > 0 {
				shape.Polygons = append(shape.Polygons, polygon)
			}
		case vnext.GeometryLineString:
			if len(part.Rings) != 1 {
				return nil, fmt.Errorf("line %s has an invalid part", feature.ID)
			}
			if line := shape.nativePoints(part.Rings[0]); len(line) > 1 {
				shape.Lines = append(shape.Lines, line)
			}
		default:
			return nil, fmt.Errorf("feature %s has unsupported geometry kind %d", feature.ID, feature.Geometry.Kind)
		}
	}
	shape.Drawn = len(shape.Polygons) > 0 || len(shape.Lines) > 0
	return shape, nil
}

func (shape *shapeModel) nativePoints(positions []vnext.Position) []point {
	out := make([]point, 0, len(positions))
	for _, position := range positions {
		x, y := position[0], -position[1]
		out = append(out, point{X: x, Y: y})
		shape.MinX, shape.MaxX = math.Min(shape.MinX, x), math.Max(shape.MaxX, x)
		shape.MinY, shape.MaxY = math.Min(shape.MinY, y), math.Max(shape.MaxY, y)
	}
	return out
}

func nativeRasters(rasters []vnext.RasterPyramid) []payloadLens {
	out := make([]payloadLens, 0, len(rasters))
	for _, raster := range rasters {
		lens := payloadLens{
			Name: raster.Name, Tiles: raster.Template, MinZoom: int(raster.MinZoom), MaxZoom: int(raster.MaxZoom),
			FullZoom: int(raster.FullZoom), SourceZoom: int(raster.SourceZoom), Formats: append([]string(nil), raster.Formats...),
			Bounds: cellRect(raster.Bounds), Surface: cellRect(raster.Surface), Interpolate: raster.Interpolate,
			Background: raster.Background, Shard: int(raster.Shard),
		}
		if len(raster.Coverage) > 0 {
			lens.Coverage = make(map[string]payloadCoverage, len(raster.Coverage))
			for _, coverage := range raster.Coverage {
				lens.Coverage[strconv.FormatInt(coverage.Zoom, 10)] = payloadCoverage{
					X: int(coverage.X), Y: int(coverage.Y), W: int(coverage.W), H: int(coverage.H),
					Bits: base64.StdEncoding.EncodeToString(coverage.Bits),
				}
			}
		}
		out = append(out, lens)
	}
	return out
}

func propertiesToAttrs(properties []vnext.Property) map[string]string {
	out := make(map[string]string, len(properties))
	for _, property := range properties {
		if property.Field.Name != "" {
			out[property.Field.Name] = nativeValue(property.Value)
		}
	}
	return out
}

func nativeValue(value vnext.Value) string {
	switch value.Kind {
	case vnext.KindBool:
		return strconv.FormatBool(value.Bool)
	case vnext.KindInt64:
		return strconv.FormatInt(value.Int64, 10)
	case vnext.KindFloat64:
		return strconv.FormatFloat(value.Float64, 'g', -1, 64)
	case vnext.KindString:
		return value.String
	case vnext.KindBytes:
		return base64.StdEncoding.EncodeToString(value.Bytes)
	case vnext.KindID:
		return value.ID.String()
	default:
		return ""
	}
}

func featureSetGeometryKind(set vnext.FeatureSet) string {
	for _, claim := range set.Claims {
		if claim.Field.Name == semconv.KeyGeometryKind {
			return claim.Value.String
		}
	}
	for _, feature := range set.Features {
		switch feature.Geometry.Kind {
		case vnext.GeometryPoint:
			return semconv.GeometryPoint
		case vnext.GeometryLineString:
			return semconv.GeometryPath
		case vnext.GeometryPolygon:
			return semconv.GeometryArea
		}
	}
	return semconv.GeometryPoint
}

func relationshipTarget(relationships []vnext.Relationship, predicate string) string {
	for _, relationship := range relationships {
		if relationship.Predicate == predicate {
			return relationship.Target
		}
	}
	return ""
}

func hasNativeText(feature *vnext.Feature) bool {
	return feature.Description != "" || len(feature.Properties) > 0 || len(feature.Relationships) > 0
}

func worldOrigin(world vnext.World) string {
	for _, set := range world.FeatureSets {
		for _, feature := range set.Features {
			if len(feature.Provenance) > 0 {
				return feature.Provenance[0].Source
			}
		}
	}
	return ""
}

func legendLabel(entries []vnext.LegendEntry, layer, fallback string) string {
	for _, entry := range entries {
		if entry.Layer == layer && entry.Label != "" {
			return entry.Label
		}
	}
	return fallback
}

func nativeColor(kind string, style vnext.Style) string {
	if kind == semconv.GeometryPath && style.Stroke != "" {
		return style.Stroke
	}
	return style.Fill
}

func cellRect(rect *vnext.RasterRect) *cells.Rect {
	if rect == nil {
		return nil
	}
	return &cells.Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}
