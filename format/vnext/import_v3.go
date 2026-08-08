package vnext

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/bundle"
)

type v3World struct {
	Attrs       map[string]string `json:"attrs"`
	Grid        *v3Grid           `json:"grid"`
	Lenses      []v3Lens          `json:"lenses"`
	Collections []v3Collection    `json:"collections"`
	Merged      []v3Merge         `json:"merged"`
}

type v3Grid struct {
	SourceZoom *int `json:"sourceZoom"`
	FirstTile  *int `json:"firstTile"`
	TileSize   *int `json:"tileSize"`
	Size       *int `json:"size"`
}

type v3Lens struct {
	Name        string                `json:"name"`
	Tiles       string                `json:"tiles"`
	MinZoom     int64                 `json:"minZoom"`
	MaxZoom     int64                 `json:"maxZoom"`
	FullZoom    int64                 `json:"fullZoom"`
	SourceZoom  int64                 `json:"sourceZoom"`
	Formats     []string              `json:"formats"`
	Bounds      *RasterRect           `json:"bounds"`
	Surface     *RasterRect           `json:"surface"`
	Interpolate bool                  `json:"interpolate"`
	Background  string                `json:"background"`
	Shard       int64                 `json:"shard"`
	Coverage    map[string]v3Coverage `json:"coverage"`
}

type v3Coverage struct {
	X    int64  `json:"x"`
	Y    int64  `json:"y"`
	W    int64  `json:"w"`
	H    int64  `json:"h"`
	Bits string `json:"bits"`
}

type v3Collection struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Kind        string            `json:"kind"`
	Group       string            `json:"group"`
	Icon        string            `json:"icon"`
	IconAsset   string            `json:"iconAsset"`
	IconPicture bool              `json:"iconPicture"`
	Color       string            `json:"color"`
	IconColor   string            `json:"iconColor"`
	Visible     *bool             `json:"visible"`
	Attrs       map[string]string `json:"attrs"`
	Features    []v3Feature       `json:"features"`
}

type v3Feature struct {
	ID       int64              `json:"id"`
	Title    string             `json:"title"`
	Subtitle string             `json:"subtitle"`
	Parent   *int64             `json:"parent"`
	Center   *bundle.Coordinate `json:"center"`
	Shard    int64              `json:"shard"`
	Attrs    map[string]string  `json:"attrs"`
	Geometry []v3Geometry       `json:"geometry"`
}

type v3Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type v3Merge struct {
	Slug   string `json:"slug"`
	Source string `json:"source"`
	Origin bool   `json:"origin"`
}

type v3Text struct {
	Description string            `json:"d"`
	Links       []v3Link          `json:"l"`
	Attrs       map[string]string `json:"a"`
}

type v3Link struct {
	Title      string `json:"title"`
	LocationID int64  `json:"locationId"`
}

// ImportV3 turns the frozen document-shaped format into the vNext semantic
// graph. The old format remains a read-only ingress boundary after cutover.
func ImportV3(reader *bundle.Reader) (Bundle, error) {
	if err := reader.Validate(); err != nil {
		return Bundle{}, fmt.Errorf("validate v3 bundle: %w", err)
	}
	volume, err := ImportV3Volume(reader.Manifest, reader.ReadEntry)
	if err != nil {
		return Bundle{}, err
	}
	assets, err := importV3Assets(reader)
	if err != nil {
		return Bundle{}, err
	}
	volume.Assets = assets
	compiled, err := Compile(volume)
	if err != nil {
		return Bundle{}, fmt.Errorf("compile imported v3 bundle: %w", err)
	}
	for _, name := range reader.Names() {
		if !strings.HasPrefix(name, bundle.TilesPrefix) {
			continue
		}
		data, err := reader.ReadEntry(name)
		if err != nil {
			return Bundle{}, err
		}
		compiled.Blobs = append(compiled.Blobs, Blob{Name: name, Data: data})
	}
	sort.Slice(compiled.Blobs, func(i, j int) bool { return compiled.Blobs[i].Name < compiled.Blobs[j].Name })
	return compiled, nil
}

// ImportV3Volume crosses the migration boundary without choosing a physical
// vNext layout. Runtime adapters use this form to make the semantic graph
// authoritative while opaque assets continue through their existing door.
func ImportV3Volume(manifest bundle.Manifest, read func(string) ([]byte, error)) (Volume, error) {
	volume := Volume{ID: manifest.Volume.Slug, Title: manifest.Volume.Title}
	for _, entry := range manifest.Worlds {
		world, err := importV3World(read, manifest, entry)
		if err != nil {
			return Volume{}, err
		}
		volume.Worlds = append(volume.Worlds, world)
	}
	return volume, nil
}

func importV3World(read func(string) ([]byte, error), manifest bundle.Manifest, entry bundle.WorldEntry) (World, error) {
	detail, err := read(bundle.WorldEntryName(entry.Slug, bundle.WorldSuffix))
	if err != nil {
		return World{}, err
	}
	var source v3World
	if err := json.Unmarshal(detail, &source); err != nil {
		return World{}, fmt.Errorf("decode world %s: %w", entry.Slug, err)
	}
	textData, err := read(bundle.WorldEntryName(entry.Slug, bundle.TextSuffix))
	if err != nil {
		return World{}, err
	}
	var texts map[string]v3Text
	if err := json.Unmarshal(textData, &texts); err != nil {
		return World{}, fmt.Errorf("decode world %s text: %w", entry.Slug, err)
	}
	packed, err := read(bundle.WorldEntryName(entry.Slug, bundle.PackedSuffix))
	if err != nil {
		return World{}, err
	}
	locations, err := bundle.UnpackLocations(packed)
	if err != nil {
		return World{}, err
	}

	grid := manifest.TileGrid
	if source.Grid != nil {
		assignInt(&grid.SourceZoom, source.Grid.SourceZoom)
		assignInt(&grid.FirstTile, source.Grid.FirstTile)
		assignInt(&grid.TileSize, source.Grid.TileSize)
		assignInt(&grid.Size, source.Grid.Size)
	}
	world := World{
		ID: entry.Slug, Title: entry.Title,
		CoordinateSpace: CoordinateSpace{
			ID: entry.Slug + "/coordinates", Kind: "projected", Unit: "world-pixel", Definition: "atlas:v3/tile-grid",
			Extent: [4]float64{0, 0, float64(grid.Size), float64(grid.Size)}, SourceZoom: int64(grid.SourceZoom),
			FirstTile: int64(grid.FirstTile), TileSize: int64(grid.TileSize), Size: int64(grid.Size),
		},
		Claims:       claimsFromV3("world", source.Attrs),
		Presentation: Presentation{ID: entry.Slug + "/presentation/default", Title: "Default"},
	}
	origin := v3Origin(source.Merged)
	for index, collection := range source.Collections {
		set, err := importV3Collection(entry, collection, texts, origin)
		if err != nil {
			return World{}, fmt.Errorf("world %s collection %s: %w", entry.Slug, collection.Title, err)
		}
		world.FeatureSets = append(world.FeatureSets, set)
		styleID := entry.Slug + "/style/" + strconv.FormatInt(collection.ID, 10)
		layerID := entry.Slug + "/layer/" + strconv.FormatInt(collection.ID, 10)
		color := collection.Color
		if color == "" {
			color = collection.IconColor
		}
		world.Presentation.Styles = append(world.Presentation.Styles, Style{
			ID: styleID, Icon: collection.Icon, IconAsset: collection.IconAsset, IconPicture: collection.IconPicture,
			RenderAs: collection.Attrs["atlas.render.as"], Fill: color, Stroke: color,
		})
		visible := collection.Visible == nil || *collection.Visible
		world.Presentation.Layers = append(world.Presentation.Layers, Layer{
			ID: layerID, FeatureSet: set.ID, Style: styleID, Group: collection.Group,
			LabelPolicy: collection.Attrs["atlas.label.policy"], Visible: visible,
			Order: int64(index), MinZoom: 0, MaxZoom: maxV3Zoom(source.Lenses),
		})
		world.Presentation.Legend = append(world.Presentation.Legend, LegendEntry{Layer: layerID, Label: collection.Title, Order: int64(index)})
	}
	for _, location := range locations {
		owner := int(location.Owner)
		if owner >= len(world.FeatureSets) {
			return World{}, fmt.Errorf("world %s point owner %d is out of range", entry.Slug, owner)
		}
		text := texts[strconv.FormatInt(location.ID, 10)]
		feature := Feature{
			ID: v3FeatureID(entry.Slug, location.ID), Title: location.Title, Shard: location.Shard,
			Geometry:   Geometry{Kind: GeometryPoint, Parts: []GeometryPart{{Rings: [][]Position{{{location.Lng, location.Lat}}}}}},
			Properties: propertiesFromV3(text.Attrs), Provenance: v3Provenance(origin, location.ID, entry.UpdatedAt),
			Description: text.Description,
		}
		ensureV3Contract(&world.FeatureSets[owner], feature.Properties)
		if location.Member != 0 {
			feature.Relationships = append(feature.Relationships, Relationship{Predicate: "within", Target: v3FeatureID(entry.Slug, location.Member)})
		}
		feature.Relationships = append(feature.Relationships, relationshipsFromV3(entry.Slug, text.Links)...)
		world.FeatureSets[owner].Features = append(world.FeatureSets[owner].Features, feature)
	}
	for index := range world.FeatureSets {
		sort.Slice(world.FeatureSets[index].Properties, func(i, j int) bool {
			return world.FeatureSets[index].Properties[i].ID.String() < world.FeatureSets[index].Properties[j].ID.String()
		})
	}
	for index, lens := range source.Lenses {
		raster, err := importV3Raster(entry.Slug, grid.TileSize, index, lens)
		if err != nil {
			return World{}, err
		}
		world.RasterPyramids = append(world.RasterPyramids, raster)
	}
	return world, nil
}

func importV3Collection(entry bundle.WorldEntry, source v3Collection, texts map[string]v3Text, origin string) (FeatureSet, error) {
	set := FeatureSet{
		ID: entry.Slug + "/set/" + strconv.FormatInt(source.ID, 10), Title: source.Title,
		SemanticType: "geometry." + source.Kind, Claims: claimsFromV3("collection", source.Attrs),
	}
	contract := make(map[string]Field)
	for _, feature := range source.Features {
		for key := range feature.Attrs {
			contract[key] = v3AttributeField("feature", key)
		}
		for key := range texts[strconv.FormatInt(feature.ID, 10)].Attrs {
			contract[key] = v3AttributeField("feature", key)
		}
	}
	for _, field := range contract {
		set.Properties = append(set.Properties, field)
	}
	sort.Slice(set.Properties, func(i, j int) bool { return set.Properties[i].ID.String() < set.Properties[j].ID.String() })
	for _, sourceFeature := range source.Features {
		text := texts[strconv.FormatInt(sourceFeature.ID, 10)]
		geometry, err := geometryFromV3(sourceFeature.Geometry)
		if err != nil {
			return FeatureSet{}, fmt.Errorf("feature %d geometry: %w", sourceFeature.ID, err)
		}
		feature := Feature{
			ID: v3FeatureID(entry.Slug, sourceFeature.ID), Title: sourceFeature.Title, Subtitle: sourceFeature.Subtitle,
			Description: text.Description, Shard: sourceFeature.Shard, Geometry: geometry,
			Properties:    propertiesFromV3(mergeV3Attrs(sourceFeature.Attrs, text.Attrs)),
			Relationships: relationshipsFromV3(entry.Slug, text.Links), Provenance: v3Provenance(origin, sourceFeature.ID, entry.UpdatedAt),
		}
		if sourceFeature.Center != nil {
			center := Position{sourceFeature.Center.Lng, sourceFeature.Center.Lat}
			feature.Center = &center
		}
		if sourceFeature.Parent != nil {
			feature.Relationships = append(feature.Relationships, Relationship{Predicate: "within", Target: v3FeatureID(entry.Slug, *sourceFeature.Parent)})
		}
		set.Features = append(set.Features, feature)
	}
	return set, nil
}

func importV3Raster(worldID string, tileSize, index int, lens v3Lens) (RasterPyramid, error) {
	raster := RasterPyramid{
		ID: worldID + "/raster/" + strconv.Itoa(index), Name: lens.Name, Codec: "raster-tile", TileSize: int64(tileSize),
		MinZoom: lens.MinZoom, MaxZoom: lens.MaxZoom, FullZoom: lens.FullZoom, SourceZoom: lens.SourceZoom,
		Template: bundle.TilesPrefix + lens.Tiles + "/{z}/{x}/{y}.{format}", Formats: lens.Formats,
		Bounds: lens.Bounds, Surface: lens.Surface, Interpolate: lens.Interpolate, Background: lens.Background, Shard: lens.Shard,
	}
	levels := make([]int, 0, len(lens.Coverage))
	for level := range lens.Coverage {
		zoom, err := strconv.Atoi(level)
		if err != nil {
			return RasterPyramid{}, fmt.Errorf("raster %s has invalid coverage zoom %q", lens.Name, level)
		}
		levels = append(levels, zoom)
	}
	sort.Ints(levels)
	for _, zoom := range levels {
		coverage := lens.Coverage[strconv.Itoa(zoom)]
		bits, err := base64.StdEncoding.DecodeString(coverage.Bits)
		if err != nil {
			return RasterPyramid{}, fmt.Errorf("decode raster %s coverage: %w", lens.Name, err)
		}
		raster.Coverage = append(raster.Coverage, RasterCoverage{Zoom: int64(zoom), X: coverage.X, Y: coverage.Y, W: coverage.W, H: coverage.H, Bits: bits})
	}
	return raster, nil
}

func geometryFromV3(source []v3Geometry) (Geometry, error) {
	var out Geometry
	for _, item := range source {
		switch item.Type {
		case "Point":
			var point Position
			if err := json.Unmarshal(item.Coordinates, &point); err != nil {
				return Geometry{}, err
			}
			out.Kind, out.Parts = GeometryPoint, append(out.Parts, GeometryPart{Rings: [][]Position{{point}}})
		case "LineString":
			var line []Position
			if err := json.Unmarshal(item.Coordinates, &line); err != nil {
				return Geometry{}, err
			}
			out.Kind, out.Parts = GeometryLineString, append(out.Parts, GeometryPart{Rings: [][]Position{line}})
		case "MultiLineString":
			var lines [][]Position
			if err := json.Unmarshal(item.Coordinates, &lines); err != nil {
				return Geometry{}, err
			}
			out.Kind = GeometryLineString
			for _, line := range lines {
				out.Parts = append(out.Parts, GeometryPart{Rings: [][]Position{line}})
			}
		case "Polygon":
			var rings [][]Position
			if err := json.Unmarshal(item.Coordinates, &rings); err != nil {
				return Geometry{}, err
			}
			out.Kind, out.Parts = GeometryPolygon, append(out.Parts, GeometryPart{Rings: rings})
		case "MultiPolygon":
			var polygons [][][]Position
			if err := json.Unmarshal(item.Coordinates, &polygons); err != nil {
				return Geometry{}, err
			}
			out.Kind = GeometryPolygon
			for _, rings := range polygons {
				out.Parts = append(out.Parts, GeometryPart{Rings: rings})
			}
		default:
			return Geometry{}, fmt.Errorf("unsupported v3 geometry %q", item.Type)
		}
	}
	return out, validateGeometry(out)
}

func importV3Assets(reader *bundle.Reader) ([]Asset, error) {
	var assets []Asset
	for _, name := range reader.Names() {
		if !strings.HasPrefix(name, bundle.IconsPrefix) {
			continue
		}
		data, err := reader.ReadEntry(name)
		if err != nil {
			return nil, err
		}
		assets = append(assets, Asset{ID: strings.TrimPrefix(name, bundle.IconsPrefix), MediaType: mediaTypeForV3(name), Data: data, Provenance: "atlas-bundle/v3"})
	}
	return assets, nil
}

func claimsFromV3(owner string, attrs map[string]string) []Property {
	properties := propertiesFromV3Namespace(owner, attrs)
	for index := range properties {
		properties[index].Field.Optional = true
	}
	return properties
}

func propertiesFromV3(attrs map[string]string) []Property {
	return propertiesFromV3Namespace("feature", attrs)
}

func propertiesFromV3Namespace(owner string, attrs map[string]string) []Property {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Property, 0, len(keys))
	for _, key := range keys {
		field := v3AttributeField(owner, key)
		out = append(out, Property{FieldID: field.ID, Field: field, Value: StringValue(attrs[key])})
	}
	return out
}

func ensureV3Contract(set *FeatureSet, properties []Property) {
	for _, property := range properties {
		id := property.fieldID()
		found := false
		for _, field := range set.Properties {
			if field.ID == id {
				found = true
				break
			}
		}
		if !found {
			set.Properties = append(set.Properties, property.Field)
		}
	}
}

func v3AttributeField(owner, key string) Field {
	return Field{ID: IDFromName("dev.atlas.v3.attribute."+owner, key), Name: key, Kind: KindString, Optional: true}
}

func v3FeatureID(world string, id int64) string {
	return world + "/feature/" + strconv.FormatInt(id, 10)
}

func v3Origin(merged []v3Merge) string {
	for _, account := range merged {
		if account.Origin && account.Source != "" {
			return account.Source
		}
	}
	for _, account := range merged {
		if account.Source != "" {
			return account.Source
		}
	}
	return "atlas-bundle/v3"
}

func v3Provenance(source string, id int64, capturedAt string) []Provenance {
	return []Provenance{{Source: source, NativeID: strconv.FormatInt(id, 10), CapturedAt: capturedAt}}
}

func relationshipsFromV3(world string, links []v3Link) []Relationship {
	out := make([]Relationship, 0, len(links))
	for _, link := range links {
		out = append(out, Relationship{Predicate: "references", Target: v3FeatureID(world, link.LocationID)})
	}
	return out
}

func mergeV3Attrs(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func maxV3Zoom(lenses []v3Lens) int64 {
	var maximum int64
	for _, lens := range lenses {
		if lens.MaxZoom > maximum {
			maximum = lens.MaxZoom
		}
	}
	return maximum
}

func assignInt(target *int, source *int) {
	if source != nil {
		*target = *source
	}
}

func mediaTypeForV3(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
