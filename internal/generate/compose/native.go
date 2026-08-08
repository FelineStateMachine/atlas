package compose

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/generate/doc"
	"github.com/FelineStateMachine/atlas/internal/generate/tiles"
)

func nativeVolume(options Options, worlds []composedWorld, icons map[string][]byte) (vnext.Volume, []vnext.Blob, int, string, error) {
	volume := vnext.Volume{ID: options.Document.Volume.Slug, Title: options.Document.Volume.Title}
	pyramids := make(map[string]tiles.Pyramid)
	capturedAt := ""
	for _, source := range worlds {
		world, err := nativeWorld(options, source, pyramids)
		if err != nil {
			return vnext.Volume{}, nil, 0, "", fmt.Errorf("world %s: %w", source.Slug, err)
		}
		volume.Worlds = append(volume.Worlds, world)
		if source.CapturedAt > capturedAt {
			capturedAt = source.CapturedAt
		}
	}
	if capturedAt == "" {
		return vnext.Volume{}, nil, 0, "", fmt.Errorf("no world carries a capture time to version the bundle by")
	}
	iconNames := make([]string, 0, len(icons))
	for name := range icons {
		iconNames = append(iconNames, name)
	}
	sort.Strings(iconNames)
	for _, name := range iconNames {
		mediaType := mime.TypeByExtension(filepath.Ext(name))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		volume.Assets = append(volume.Assets, vnext.Asset{ID: name, MediaType: mediaType, Data: icons[name], Provenance: options.Document.Volume.Slug})
	}
	blobs, tileCount, err := rasterBlobList(options.Tiles, pyramids)
	return volume, blobs, tileCount, capturedAt, err
}

func nativeWorld(options Options, source composedWorld, pyramids map[string]tiles.Pyramid) (vnext.World, error) {
	grid := source.Grid
	if grid == nil {
		grid = &worldGrid{SourceZoom: options.Curation.Window.SourceZoom, FirstTile: options.Curation.Window.FirstTile}
	}
	world := vnext.World{
		ID: source.Slug, Title: source.Title,
		CoordinateSpace: vnext.CoordinateSpace{
			ID: source.Slug + "/space", Kind: "projected", Unit: "pixel", Definition: "atlas:tile-plane",
			Extent:     [4]float64{0, 0, float64(options.Tiles.Size), float64(options.Tiles.Size)},
			SourceZoom: int64(grid.SourceZoom), FirstTile: int64(grid.FirstTile),
			TileSize: int64(options.Tiles.TileSize), Size: int64(options.Tiles.Size),
		},
		Claims:       stringProperties("world", source.Attrs, true),
		Presentation: vnext.Presentation{ID: source.Slug + "/presentation", Title: source.Title},
	}
	for index, collection := range source.Collections {
		set, style, layer, err := nativeCollection(options.Document.Source.Name, source, collection, index, surfaceGrid{
			SourceZoom: grid.SourceZoom, FirstTile: grid.FirstTile,
			TileSize: options.Tiles.TileSize, Size: options.Tiles.Size,
		})
		if err != nil {
			return vnext.World{}, err
		}
		world.FeatureSets = append(world.FeatureSets, set)
		world.Presentation.Styles = append(world.Presentation.Styles, style)
		world.Presentation.Layers = append(world.Presentation.Layers, layer)
		world.Presentation.Legend = append(world.Presentation.Legend, vnext.LegendEntry{Layer: layer.ID, Label: collection.Title, Order: int64(index)})
	}
	for index, lens := range source.Lenses {
		pyramid := source.Pyramids[index]
		pyramids[lens.Tiles] = pyramid
		raster, err := nativeRaster(source.Slug, options.Tiles.TileSize, index, lens)
		if err != nil {
			return vnext.World{}, err
		}
		world.RasterPyramids = append(world.RasterPyramids, raster)
	}
	return world, nil
}

func nativeCollection(provenanceSource string, world composedWorld, source composedCollection, order int, grid surfaceGrid) (vnext.FeatureSet, vnext.Style, vnext.Layer, error) {
	key := source.Key
	if key == "" {
		key = strconv.FormatInt(source.ID, 10)
	}
	setID := world.Slug + "/set/" + key
	set := vnext.FeatureSet{ID: setID, Title: source.Title, SemanticType: "geometry." + source.Kind, Claims: stringProperties("featureSet", source.Attrs, true)}
	contract := make(map[vnext.ID]vnext.Field)
	for _, item := range source.Features {
		feature, err := nativeFeature(provenanceSource, world, item, grid)
		if err != nil {
			return vnext.FeatureSet{}, vnext.Style{}, vnext.Layer{}, fmt.Errorf("feature %d: %w", item.ID, err)
		}
		for _, property := range feature.Properties {
			contract[property.Field.ID] = property.Field
		}
		set.Features = append(set.Features, feature)
	}
	for _, field := range contract {
		set.Properties = append(set.Properties, field)
	}
	sort.Slice(set.Properties, func(i, j int) bool { return set.Properties[i].ID.String() < set.Properties[j].ID.String() })
	styleID := world.Slug + "/style/" + key
	style := vnext.Style{
		ID: styleID, Icon: source.Icon, IconAsset: source.IconAsset, IconPicture: source.IconPicture,
		RenderAs: semconv.RenderAs(source.Attrs, ""), Stroke: source.IconColor, Fill: source.Color,
	}
	layer := vnext.Layer{
		ID: world.Slug + "/layer/" + key, FeatureSet: setID, Style: styleID,
		Group: source.Group, LabelPolicy: semconv.LabelPolicy(source.Kind, source.Attrs),
		Visible: source.Visible, Order: int64(order), MaxZoom: 32,
	}
	return set, style, layer, nil
}

func nativeFeature(provenanceSource string, world composedWorld, source composedFeature, grid surfaceGrid) (vnext.Feature, error) {
	id := world.Slug + "/feature/" + strconv.FormatInt(source.ID, 10)
	feature := vnext.Feature{
		ID: id, Title: source.Title, Subtitle: source.Subtitle, Description: source.Description,
		Shard: source.Shard, Properties: stringProperties("feature", source.Attrs, false),
		Provenance: []vnext.Provenance{{Source: provenanceSource, NativeID: strconv.FormatInt(source.ID, 10), CapturedAt: world.CapturedAt}},
	}
	if source.At != nil {
		feature.Geometry = vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{projectPosition(source.At.Lng, source.At.Lat, grid)}}}}}
	} else {
		geometry, err := nativeGeometry(source.Geometry, grid)
		if err != nil {
			return vnext.Feature{}, err
		}
		feature.Geometry = geometry
	}
	if source.Center != nil {
		center := projectPosition(source.Center.Lng, source.Center.Lat, grid)
		feature.Center = &center
	}
	if source.Member != 0 {
		feature.Relationships = append(feature.Relationships, vnext.Relationship{Predicate: "within", Target: world.Slug + "/feature/" + strconv.FormatInt(source.Member, 10)})
	}
	if source.Parent != 0 {
		feature.Relationships = append(feature.Relationships, vnext.Relationship{Predicate: "within", Target: world.Slug + "/feature/" + strconv.FormatInt(source.Parent, 10)})
	}
	for _, link := range source.Links {
		feature.Relationships = append(feature.Relationships, vnext.Relationship{Predicate: "references", Target: world.Slug + "/feature/" + strconv.FormatInt(link.Feature, 10)})
	}
	return feature, nil
}

func nativeGeometry(items []doc.Geometry, grid surfaceGrid) (vnext.Geometry, error) {
	var geometry vnext.Geometry
	for _, item := range items {
		switch item.Type {
		case "Point":
			var point vnext.Position
			if err := json.Unmarshal(item.Coordinates, &point); err != nil {
				return vnext.Geometry{}, err
			}
			geometry.Kind, geometry.Parts = vnext.GeometryPoint, append(geometry.Parts, vnext.GeometryPart{Rings: [][]vnext.Position{{projectPosition(point[0], point[1], grid)}}})
		case "LineString":
			var line []vnext.Position
			if err := json.Unmarshal(item.Coordinates, &line); err != nil {
				return vnext.Geometry{}, err
			}
			geometry.Kind, geometry.Parts = vnext.GeometryLineString, append(geometry.Parts, vnext.GeometryPart{Rings: [][]vnext.Position{projectPositions(line, grid)}})
		case "MultiLineString":
			var lines [][]vnext.Position
			if err := json.Unmarshal(item.Coordinates, &lines); err != nil {
				return vnext.Geometry{}, err
			}
			geometry.Kind = vnext.GeometryLineString
			for _, line := range lines {
				geometry.Parts = append(geometry.Parts, vnext.GeometryPart{Rings: [][]vnext.Position{projectPositions(line, grid)}})
			}
		case "Polygon":
			var rings [][]vnext.Position
			if err := json.Unmarshal(item.Coordinates, &rings); err != nil {
				return vnext.Geometry{}, err
			}
			geometry.Kind, geometry.Parts = vnext.GeometryPolygon, append(geometry.Parts, vnext.GeometryPart{Rings: projectRings(rings, grid)})
		case "MultiPolygon":
			var polygons [][][]vnext.Position
			if err := json.Unmarshal(item.Coordinates, &polygons); err != nil {
				return vnext.Geometry{}, err
			}
			geometry.Kind = vnext.GeometryPolygon
			for _, rings := range polygons {
				geometry.Parts = append(geometry.Parts, vnext.GeometryPart{Rings: projectRings(rings, grid)})
			}
		default:
			return vnext.Geometry{}, fmt.Errorf("unsupported geometry %q", item.Type)
		}
	}
	return geometry, nil
}

func projectPosition(lng, lat float64, grid surfaceGrid) vnext.Position {
	return vnext.Position{projectX(lng, grid), projectY(lat, grid)}
}

func projectPositions(positions []vnext.Position, grid surfaceGrid) []vnext.Position {
	out := make([]vnext.Position, len(positions))
	for index, position := range positions {
		out[index] = projectPosition(position[0], position[1], grid)
	}
	return out
}

func projectRings(rings [][]vnext.Position, grid surfaceGrid) [][]vnext.Position {
	out := make([][]vnext.Position, len(rings))
	for index, ring := range rings {
		out[index] = projectPositions(ring, grid)
	}
	return out
}

func nativeRaster(world string, tileSize, index int, source lens) (vnext.RasterPyramid, error) {
	raster := vnext.RasterPyramid{
		ID: world + "/raster/" + strconv.Itoa(index), Name: source.Name, Codec: rasterCodec(source.Formats),
		TileSize: int64(tileSize), MinZoom: int64(source.MinZoom), MaxZoom: int64(source.MaxZoom), FullZoom: int64(source.FullZoom), SourceZoom: int64(source.SourceZoom),
		Template: "tiles/" + source.Tiles + "/{z}/{x}/{y}.{format}", Formats: source.Formats,
		Bounds: nativeBox(source.Bounds), Surface: nativeBox(source.Surface), Interpolate: source.Interpolate, Background: source.Background, Shard: source.Shard,
	}
	levels := make([]string, 0, len(source.Coverage))
	for level := range source.Coverage {
		levels = append(levels, level)
	}
	sort.Strings(levels)
	for _, level := range levels {
		zoom, err := strconv.ParseInt(level, 10, 64)
		if err != nil {
			return vnext.RasterPyramid{}, err
		}
		coverage := source.Coverage[level]
		bits, err := base64.StdEncoding.DecodeString(coverage.Bits)
		if err != nil {
			return vnext.RasterPyramid{}, err
		}
		raster.Coverage = append(raster.Coverage, vnext.RasterCoverage{Zoom: zoom, X: int64(coverage.X), Y: int64(coverage.Y), W: int64(coverage.W), H: int64(coverage.H), Bits: bits})
	}
	return raster, nil
}

func nativeBox(box *tiles.Box) *vnext.RasterRect {
	if box == nil {
		return nil
	}
	return &vnext.RasterRect{X: float64(box.X), Y: float64(box.Y), Width: float64(box.Width), Height: float64(box.Height)}
}

func rasterCodec(formats []string) string {
	if len(formats) == 0 {
		return "application/octet-stream"
	}
	extension := strings.TrimPrefix(formats[len(formats)-1], ".")
	if extension == "jpg" {
		extension = "jpeg"
	}
	return "image/" + extension
}

func stringProperties(owner string, attrs map[string]string, optional bool) []vnext.Property {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	properties := make([]vnext.Property, 0, len(keys))
	for _, key := range keys {
		field := vnext.Field{ID: vnext.IDFromName("dev.atlas.attribute."+owner, key), Name: key, Kind: vnext.KindString, Optional: optional}
		properties = append(properties, vnext.Property{FieldID: field.ID, Field: field, Value: vnext.StringValue(attrs[key])})
	}
	return properties
}
