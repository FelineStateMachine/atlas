package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/FelineStateMachine/atlas/format/bundle"
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

func (a *App) projectedContent(source hostenv.Volume, name string) ([]byte, string, bool, error) {
	volume, err := a.semanticVolume(source)
	if err != nil {
		return nil, "", true, err
	}
	for _, world := range volume.Worlds {
		data, kind, held, err := projectedVNextEntry(world, name)
		if held {
			return data, kind, true, err
		}
	}
	return nil, "", false, nil
}

type compatibilityIDs struct {
	byName map[string]int64
	byID   map[int64]string
}

func newCompatibilityIDs() *compatibilityIDs {
	return &compatibilityIDs{byName: make(map[string]int64), byID: make(map[int64]string)}
}

func (ids *compatibilityIDs) resolve(name string) (int64, error) {
	if held, ok := ids.byName[name]; ok {
		return held, nil
	}
	candidate := numericTail(name)
	if candidate <= 0 {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(name))
		candidate = int64(hash.Sum64() & math.MaxInt64)
		if candidate == 0 {
			candidate = 1
		}
	}
	if owner, held := ids.byID[candidate]; held && owner != name {
		return 0, fmt.Errorf("compatibility ID %d is shared by %s and %s", candidate, owner, name)
	}
	ids.byName[name], ids.byID[candidate] = candidate, name
	return candidate, nil
}

func presentVNextWorld(world vnext.World) (worldPayload, []bundle.Location, map[string]featureText, error) {
	sets := make(map[string]vnext.FeatureSet, len(world.FeatureSets))
	for _, set := range world.FeatureSets {
		sets[set.ID] = set
	}
	styles := make(map[string]vnext.Style, len(world.Presentation.Styles))
	for _, style := range world.Presentation.Styles {
		styles[style.ID] = style
	}
	layers := append([]vnext.Layer(nil), world.Presentation.Layers...)
	sort.SliceStable(layers, func(i, j int) bool { return layers[i].Order < layers[j].Order })
	projection := worldProjection{
		world: world, sets: sets, styles: styles,
		collectionIDs: newCompatibilityIDs(), featureIDs: newCompatibilityIDs(),
		text: make(map[string]featureText),
	}
	return projection.present(layers)
}

func projectedVNextEntry(world vnext.World, name string) ([]byte, string, bool, error) {
	payload, locations, text, err := presentVNextWorld(world)
	if err != nil {
		return nil, "", true, err
	}
	switch name {
	case bundle.WorldEntryName(world.ID, bundle.WorldSuffix):
		data, err := json.Marshal(payload)
		return data, "application/json", true, err
	case bundle.WorldEntryName(world.ID, bundle.PackedSuffix):
		return bundle.PackLocations(locations), "application/octet-stream", true, nil
	case bundle.WorldEntryName(world.ID, bundle.TextSuffix):
		data, err := json.Marshal(text)
		return data, "application/json", true, err
	default:
		return nil, "", false, nil
	}
}

type worldProjection struct {
	world         vnext.World
	sets          map[string]vnext.FeatureSet
	styles        map[string]vnext.Style
	collectionIDs *compatibilityIDs
	featureIDs    *compatibilityIDs
	text          map[string]featureText
}

func (projection worldProjection) present(layers []vnext.Layer) (worldPayload, []bundle.Location, map[string]featureText, error) {
	payload := worldPayload{
		Grid: &payloadGrid{
			SourceZoom: intPointer(int(projection.world.CoordinateSpace.SourceZoom)),
			FirstTile:  intPointer(int(projection.world.CoordinateSpace.FirstTile)),
			TileSize:   intPointer(int(projection.world.CoordinateSpace.TileSize)),
			Size:       intPointer(int(projection.world.CoordinateSpace.Size)),
		},
		Attrs: claimsToAttrs(projection.world.Claims),
	}
	payload.Lenses = projectRasters(projection.world.RasterPyramids)
	if source := worldOrigin(projection.world); source != "" {
		payload.Merged = []payloadMerge{{Source: source, Origin: true}}
	}
	var locations []bundle.Location
	for owner, layer := range layers {
		collection, points, err := projection.collection(layer, owner)
		if err != nil {
			return worldPayload{}, nil, nil, err
		}
		payload.Collections = append(payload.Collections, collection)
		locations = append(locations, points...)
	}
	return payload, locations, projection.text, nil
}

func (projection worldProjection) collection(layer vnext.Layer, owner int) (payloadCollection, []bundle.Location, error) {
	set, held := projection.sets[layer.FeatureSet]
	if !held {
		return payloadCollection{}, nil, fmt.Errorf("layer %s refers to missing feature set %s", layer.ID, layer.FeatureSet)
	}
	style, held := projection.styles[layer.Style]
	if !held {
		return payloadCollection{}, nil, fmt.Errorf("layer %s refers to missing style %s", layer.ID, layer.Style)
	}
	id, err := projection.collectionIDs.resolve(set.ID)
	if err != nil {
		return payloadCollection{}, nil, err
	}
	kind := strings.TrimPrefix(set.SemanticType, "geometry.")
	collection := payloadCollection{
		ID: id, Title: set.Title, Kind: kind, Group: layer.Group, Icon: style.Icon,
		IconAsset: style.IconAsset, IconPicture: style.IconPicture,
		Color: projectedColor(kind, style), Visible: boolPointer(layer.Visible),
		Attrs: claimsToAttrs(set.Claims),
	}
	collection.Attrs[semconv.KeyGeometryKind] = kind
	putAttr(collection.Attrs, semconv.KeyLabelPolicy, layer.LabelPolicy)
	putAttr(collection.Attrs, semconv.KeyRenderAs, style.RenderAs)
	return projection.features(set, collection, owner)
}

func (projection worldProjection) features(set vnext.FeatureSet, collection payloadCollection, owner int) (payloadCollection, []bundle.Location, error) {
	if owner > math.MaxUint16 {
		return payloadCollection{}, nil, fmt.Errorf("presentation has too many collections")
	}
	var locations []bundle.Location
	for _, feature := range set.Features {
		id, err := projection.featureIDs.resolve(feature.ID)
		if err != nil {
			return payloadCollection{}, nil, err
		}
		text, err := projection.featureText(feature)
		if err != nil {
			return payloadCollection{}, nil, err
		}
		projection.text[strconv.FormatInt(id, 10)] = text
		if feature.Geometry.Kind == vnext.GeometryPoint {
			location, err := projection.point(feature, id, owner)
			if err != nil {
				return payloadCollection{}, nil, err
			}
			locations = append(locations, location)
			continue
		}
		shape, err := projection.shape(feature, id, text)
		if err != nil {
			return payloadCollection{}, nil, err
		}
		collection.Features = append(collection.Features, shape)
	}
	return collection, locations, nil
}

func (projection worldProjection) point(feature vnext.Feature, id int64, owner int) (bundle.Location, error) {
	if len(feature.Geometry.Parts) != 1 || len(feature.Geometry.Parts[0].Rings) != 1 || len(feature.Geometry.Parts[0].Rings[0]) != 1 {
		return bundle.Location{}, fmt.Errorf("point %s has an invalid geometry", feature.ID)
	}
	position := feature.Geometry.Parts[0].Rings[0][0]
	member, err := projection.relationship(feature.Relationships, "within")
	if err != nil {
		return bundle.Location{}, err
	}
	return bundle.Location{
		ID: id, Title: feature.Title, Lat: position[1], Lng: position[0],
		Member: member, Shard: feature.Shard, Owner: uint16(owner),
	}, nil
}

func (projection worldProjection) shape(feature vnext.Feature, id int64, text featureText) (payloadFeature, error) {
	geometry, err := projectGeometry(feature.Geometry)
	if err != nil {
		return payloadFeature{}, err
	}
	shape := payloadFeature{
		ID: id, Title: feature.Title, Subtitle: feature.Subtitle, HasText: hasFeatureText(text),
		Shard: feature.Shard, Attrs: propertiesToAttrs(feature.Properties), Geometry: geometry,
	}
	if feature.Center != nil {
		shape.Center = &bundle.Coordinate{Lat: feature.Center[1], Lng: feature.Center[0]}
	}
	if parent, err := projection.relationship(feature.Relationships, "within"); err != nil {
		return payloadFeature{}, err
	} else if parent != 0 {
		shape.Parent = &parent
	}
	return shape, nil
}

func (projection worldProjection) featureText(feature vnext.Feature) (featureText, error) {
	text := featureText{Description: feature.Description, Attrs: propertiesToAttrs(feature.Properties)}
	for _, relationship := range feature.Relationships {
		if relationship.Predicate != "references" {
			continue
		}
		id, err := projection.featureIDs.resolve(relationship.Target)
		if err != nil {
			return featureText{}, err
		}
		text.Links = append(text.Links, id)
	}
	return text, nil
}

func (projection worldProjection) relationship(relationships []vnext.Relationship, predicate string) (int64, error) {
	for _, relationship := range relationships {
		if relationship.Predicate == predicate {
			return projection.featureIDs.resolve(relationship.Target)
		}
	}
	return 0, nil
}

func projectRasters(rasters []vnext.RasterPyramid) []payloadLens {
	out := make([]payloadLens, 0, len(rasters))
	for _, raster := range rasters {
		lens := payloadLens{
			Name: raster.Name, Tiles: rasterTiles(raster.Template), MinZoom: int(raster.MinZoom), MaxZoom: int(raster.MaxZoom),
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

func projectGeometry(geometry vnext.Geometry) ([]payloadGeometry, error) {
	if geometry.Kind == vnext.GeometryPolygon {
		polygons := make([][][]vnext.Position, 0, len(geometry.Parts))
		for _, part := range geometry.Parts {
			polygons = append(polygons, part.Rings)
		}
		if len(polygons) == 1 {
			return marshalGeometry("Polygon", polygons[0])
		}
		return marshalGeometry("MultiPolygon", polygons)
	}
	if geometry.Kind == vnext.GeometryLineString {
		lines := make([][]vnext.Position, 0, len(geometry.Parts))
		for _, part := range geometry.Parts {
			if len(part.Rings) != 1 {
				return nil, fmt.Errorf("line geometry has an invalid part")
			}
			lines = append(lines, part.Rings[0])
		}
		if len(lines) == 1 {
			return marshalGeometry("LineString", lines[0])
		}
		return marshalGeometry("MultiLineString", lines)
	}
	return nil, fmt.Errorf("geometry kind %d cannot be an inline shape", geometry.Kind)
}

func marshalGeometry(kind string, coordinates any) ([]payloadGeometry, error) {
	data, err := json.Marshal(coordinates)
	if err != nil {
		return nil, fmt.Errorf("marshal %s geometry: %w", kind, err)
	}
	return []payloadGeometry{{Type: kind, Coordinates: data}}, nil
}

func buildVNextWorld(world vnext.World) (*worldModel, error) {
	payload, locations, _, err := presentVNextWorld(world)
	if err != nil {
		return nil, err
	}
	space := world.CoordinateSpace
	grid := tileGrid{SourceZoom: int(space.SourceZoom), FirstTile: int(space.FirstTile), TileSize: int(space.TileSize), Size: int(space.Size)}
	return buildProjectedWorld(world.ID, payload, locations, grid), nil
}

func buildProjectedWorld(slug string, decoded worldPayload, locations []bundle.Location, grid tileGrid) *worldModel {
	model := &worldModel{
		Slug: slug, Lenses: decoded.Lenses, Attrs: decoded.Attrs, Grid: grid,
		ByID: map[string]*collectionModel{}, PointByID: map[string]*pointModel{}, ShapeByID: map[string]*shapeModel{},
	}
	for _, account := range decoded.Merged {
		if account.Origin {
			model.Origin = account.Source
			break
		}
	}
	buildProjectedCollections(model, decoded.Collections, grid)
	buildProjectedPoints(model, locations, grid)
	for _, shape := range model.Shapes {
		shape.Depth = shapeDepth(model, shape, 0)
	}
	return model
}

func buildProjectedCollections(model *worldModel, collections []payloadCollection, grid tileGrid) {
	for at, held := range collections {
		kind := held.Kind
		if kind == "" {
			kind = semconv.GeometryPoint
		}
		collection := &collectionModel{
			ID: strconv.FormatInt(held.ID, 10), Title: held.Title, Kind: kind, Group: held.Group,
			Icon: held.Icon, IconAsset: held.IconAsset, Color: held.Color, IconColor: held.IconColor,
			Attrs: held.Attrs, Curated: semconv.LabelPolicy(kind, held.Attrs), RenderAs: semconv.RenderAs(held.Attrs, ""),
			Hidden: held.Visible != nil && !*held.Visible, Index: at, Count: len(held.Features),
		}
		model.Members = append(model.Members, collection)
		model.ByID[collection.ID] = collection
		for _, feature := range held.Features {
			shape := buildShape(feature, collection, grid)
			collection.Shapes = append(collection.Shapes, shape)
			model.Shapes = append(model.Shapes, shape)
			model.ShapeByID[shape.ID] = shape
		}
	}
}

func buildProjectedPoints(model *worldModel, locations []bundle.Location, grid tileGrid) {
	for _, location := range locations {
		owner := int(location.Owner)
		if owner >= len(model.Members) {
			continue
		}
		collection := model.Members[owner]
		x, y := grid.project(location.Lat, location.Lng)
		pin := &pointModel{
			ID: strconv.FormatInt(location.ID, 10), Title: location.Title, Lat: location.Lat, Lng: location.Lng,
			X: x, Y: y, Shard: location.Shard, Collection: collection,
		}
		model.Points = append(model.Points, pin)
		model.PointByID[pin.ID] = pin
		collection.Count++
	}
}

func claimsToAttrs(claims []vnext.Property) map[string]string { return propertiesToAttrs(claims) }

func propertiesToAttrs(properties []vnext.Property) map[string]string {
	out := make(map[string]string, len(properties))
	for _, property := range properties {
		if property.Field.Name != "" {
			out[property.Field.Name] = projectedValue(property.Value)
		}
	}
	return out
}

func projectedValue(value vnext.Value) string {
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

func projectedColor(kind string, style vnext.Style) string {
	if kind == semconv.GeometryPath && style.Stroke != "" {
		return style.Stroke
	}
	return style.Fill
}

func rasterTiles(template string) string {
	held := strings.TrimPrefix(template, bundle.TilesPrefix)
	if at := strings.IndexByte(held, '/'); at >= 0 {
		return held[:at]
	}
	return held
}

func cellRect(rect *vnext.RasterRect) *cells.Rect {
	if rect == nil {
		return nil
	}
	return &cells.Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

func numericTail(value string) int64 {
	if at := strings.LastIndexByte(value, '/'); at >= 0 {
		value = value[at+1:]
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func hasFeatureText(text featureText) bool {
	return text.Description != "" || len(text.Links) > 0 || len(text.Attrs) > 0
}

func putAttr(attrs map[string]string, key, value string) {
	if value != "" {
		attrs[key] = value
	}
}

func intPointer(value int) *int    { return &value }
func boolPointer(value bool) *bool { return &value }
