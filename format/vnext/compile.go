package vnext

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	volumeTableName             = "data/volumes.pack"
	worldTableName              = "data/worlds.pack"
	coordinateTableName         = "data/coordinate-spaces.pack"
	featureSetTableName         = "data/feature-sets.pack"
	propertyDefinitionTableName = "data/property-definitions.pack"
	featureTableName            = "data/features.pack"
	relationshipTableName       = "data/relationships.pack"
	provenanceTableName         = "data/provenance.pack"
	rasterTableName             = "data/raster-pyramids.pack"
	rasterFormatTableName       = "data/raster-formats.pack"
	rasterCoverageTableName     = "data/raster-coverage.pack"
	presentationTableName       = "data/presentations.pack"
	styleTableName              = "data/styles.pack"
	layerTableName              = "data/layers.pack"
	legendTableName             = "data/legend.pack"
	assetTableName              = "data/assets.pack"
)

// NamedTable assigns a stable container entry to a typed table.
type NamedTable struct {
	Name  string
	Table Table
}

// Blob is an opaque, independently range-readable payload.
type Blob struct {
	Name      string
	Data      []byte
	Path      string
	temporary bool
}

// Bundle is the complete compiled form accepted by Write.
type Bundle struct {
	VolumeID string
	Release  Release
	Schema   Schema
	Tables   []NamedTable
	Blobs    []Blob
}

type tableBuilder struct {
	typ     Type
	columns map[ID][]Value
	rows    int
}

func newTableBuilder(schema Schema, typeName string) (*tableBuilder, error) {
	typ, ok := schema.Type(CoreID("type." + typeName))
	if !ok {
		return nil, fmt.Errorf("schema has no %s type", typeName)
	}
	columns := make(map[ID][]Value, len(typ.Fields))
	for _, field := range typ.Fields {
		columns[field.ID] = nil
	}
	return &tableBuilder{typ: typ, columns: columns}, nil
}

func (builder *tableBuilder) add(values map[ID]Value) error {
	for id := range values {
		if _, held := builder.columns[id]; !held {
			return fmt.Errorf("type %s has no field %s", builder.typ.Name, id)
		}
	}
	for _, field := range builder.typ.Fields {
		value, held := values[field.ID]
		if !held {
			value = NullValue()
		}
		builder.columns[field.ID] = append(builder.columns[field.ID], value)
	}
	builder.rows++
	return nil
}

func (builder *tableBuilder) table() Table {
	table := Table{TypeID: builder.typ.ID, Rows: builder.rows, Columns: make([]Column, 0, len(builder.typ.Fields))}
	for _, field := range builder.typ.Fields {
		table.Columns = append(table.Columns, Column{Field: field, Values: builder.columns[field.ID]})
	}
	return canonicalTable(table)
}

// Compile validates a complete semantic Volume and plans it into typed tables
// plus content-addressed blobs.
func Compile(volume Volume) (Bundle, error) {
	if err := validateVolume(volume); err != nil {
		return Bundle{}, err
	}
	schema, err := schemaFor(volume)
	if err != nil {
		return Bundle{}, err
	}
	builders, err := makeBuilders(schema)
	if err != nil {
		return Bundle{}, err
	}
	blobs, err := compileRows(volume, builders)
	if err != nil {
		return Bundle{}, err
	}
	names := tableNames()
	tables := make([]NamedTable, 0, len(names))
	physical := make(map[string]Table, len(names))
	for _, name := range names {
		table := builders[name].table()
		if err := table.validate(); err != nil {
			return Bundle{}, fmt.Errorf("compile %s: %w", name, err)
		}
		physical[name] = table
		partitioned := physical[featureTableName].Rows > 0 &&
			(name == featureTableName || name == relationshipTableName || name == provenanceTableName)
		if !partitioned {
			tables = append(tables, NamedTable{Name: name, Table: table})
		}
	}
	partitions, index, err := partitionFeatureTables(volume, physical)
	if err != nil {
		return Bundle{}, err
	}
	tables = append(tables, partitions...)
	blobs = append(blobs, index)
	slices.SortFunc(tables, func(left, right NamedTable) int { return strings.Compare(left.Name, right.Name) })
	return Bundle{
		VolumeID: volume.ID,
		Release:  Release{Title: volume.Title, Worlds: len(volume.Worlds)},
		Schema:   schema,
		Tables:   tables,
		Blobs:    blobs,
	}, nil
}

func schemaFor(volume Volume) (Schema, error) {
	standard := StandardSchema()
	extensions := make(map[ID]map[ID]Field)
	for _, world := range volume.Worlds {
		if err := collectClaims(extensions, standard, "world", world.Claims); err != nil {
			return Schema{}, err
		}
		for _, set := range world.FeatureSets {
			if err := collectClaims(extensions, standard, "featureSet", set.Claims); err != nil {
				return Schema{}, err
			}
			for _, field := range set.Properties {
				if err := collectField(extensions, standard, "feature", field); err != nil {
					return Schema{}, fmt.Errorf("feature set %s: %w", set.ID, err)
				}
			}
		}
	}
	fragment := Schema{}
	for typeID, fields := range extensions {
		typ, _ := standard.Type(typeID)
		extension := Type{ID: typeID, Name: typ.Name, Fields: make([]Field, 0, len(fields))}
		for _, field := range fields {
			extension.Fields = append(extension.Fields, field)
		}
		fragment.Types = append(fragment.Types, extension)
	}
	return UnionSchemas(standard, fragment)
}

func collectClaims(extensions map[ID]map[ID]Field, standard Schema, typeName string, claims []Property) error {
	for _, claim := range claims {
		field := claim.Field
		if field.ID.isZero() {
			return fmt.Errorf("%s claim %s has no field definition", typeName, claim.fieldID())
		}
		if err := collectField(extensions, standard, typeName, field); err != nil {
			return err
		}
	}
	return nil
}

func collectField(extensions map[ID]map[ID]Field, standard Schema, typeName string, field Field) error {
	typeID := CoreID("type." + typeName)
	core, _ := standard.Type(typeID)
	if _, held := core.Field(field.ID); held {
		return fmt.Errorf("extension reuses core field %s", field.ID)
	}
	field.Optional = true
	if extensions[typeID] == nil {
		extensions[typeID] = make(map[ID]Field)
	}
	current, held := extensions[typeID][field.ID]
	if held && current.Kind != field.Kind {
		return fmt.Errorf("field %s has conflicting kinds", field.ID)
	}
	if held && current.Name < field.Name {
		field.Name = current.Name
	}
	extensions[typeID][field.ID] = field
	return nil
}

func makeBuilders(schema Schema) (map[string]*tableBuilder, error) {
	typeNames := []string{"volume", "world", "coordinate", "featureSet", "propertyDefinition", "feature", "relationship", "provenance", "rasterPyramid", "rasterFormat", "rasterCoverage", "presentation", "style", "layer", "legend", "asset"}
	names := tableNames()
	builders := make(map[string]*tableBuilder, len(names))
	for index, name := range names {
		builder, err := newTableBuilder(schema, typeNames[index])
		if err != nil {
			return nil, err
		}
		builders[name] = builder
	}
	return builders, nil
}

func tableNames() []string {
	return []string{
		volumeTableName, worldTableName, coordinateTableName, featureSetTableName,
		propertyDefinitionTableName, featureTableName, relationshipTableName,
		provenanceTableName, rasterTableName, rasterFormatTableName, rasterCoverageTableName,
		presentationTableName, styleTableName,
		layerTableName, legendTableName, assetTableName,
	}
}

func compileRows(volume Volume, builders map[string]*tableBuilder) ([]Blob, error) {
	if err := builders[volumeTableName].add(valuesOf("volume", map[string]Value{
		"id": StringValue(volume.ID), "title": StringValue(volume.Title),
	})); err != nil {
		return nil, err
	}
	for _, world := range volume.Worlds {
		if err := compileWorld(world, builders); err != nil {
			return nil, fmt.Errorf("world %s: %w", world.ID, err)
		}
	}
	return compileAssets(volume.Assets, builders[assetTableName])
}

func compileWorld(world World, builders map[string]*tableBuilder) error {
	worldValues := valuesOf("world", map[string]Value{
		"id": StringValue(world.ID), "title": StringValue(world.Title),
		"coordinateSpace": StringValue(world.CoordinateSpace.ID),
	})
	addClaims(worldValues, world.Claims)
	if err := builders[worldTableName].add(worldValues); err != nil {
		return err
	}
	if err := compileCoordinate(world, builders[coordinateTableName]); err != nil {
		return err
	}
	for _, set := range world.FeatureSets {
		if err := compileFeatureSet(world.ID, set, builders); err != nil {
			return err
		}
	}
	for _, raster := range world.RasterPyramids {
		if err := compileRaster(world.ID, raster, builders); err != nil {
			return err
		}
	}
	return compilePresentation(world.ID, world.Presentation, builders)
}

func compileCoordinate(world World, builder *tableBuilder) error {
	space := world.CoordinateSpace
	return builder.add(valuesOf("coordinate", map[string]Value{
		"id": StringValue(space.ID), "world": StringValue(world.ID),
		"kind": StringValue(space.Kind), "unit": StringValue(space.Unit),
		"definition": StringValue(space.Definition), "extent": BytesValue(encodeExtent(space.Extent)),
		"sourceZoom": Int64Value(space.SourceZoom), "originX": Int64Value(space.OriginX), "originY": Int64Value(space.OriginY),
		"tileSize": Int64Value(space.TileSize), "size": Int64Value(space.Size),
	}))
}

func compileFeatureSet(worldID string, set FeatureSet, builders map[string]*tableBuilder) error {
	setValues := valuesOf("featureSet", map[string]Value{
		"id": StringValue(set.ID), "world": StringValue(worldID), "title": StringValue(set.Title),
		"semanticType": StringValue(set.SemanticType),
	})
	addClaims(setValues, set.Claims)
	if err := builders[featureSetTableName].add(setValues); err != nil {
		return err
	}
	contract := make(map[ID]Field, len(set.Properties))
	for _, field := range set.Properties {
		contract[field.ID] = field
		if err := builders[propertyDefinitionTableName].add(valuesOf("propertyDefinition", map[string]Value{
			"featureSet": StringValue(set.ID), "field": IDValue(field.ID), "name": StringValue(field.Name),
			"kind": Int64Value(int64(field.Kind)), "optional": BoolValue(field.Optional),
		})); err != nil {
			return err
		}
	}
	for _, feature := range set.Features {
		if err := compileFeature(set.ID, feature, contract, builders); err != nil {
			return err
		}
	}
	return nil
}

func compileFeature(setID string, feature Feature, contract map[ID]Field, builders map[string]*tableBuilder) error {
	geometry, err := encodeGeometry(feature.Geometry)
	if err != nil {
		return fmt.Errorf("feature %s geometry: %w", feature.ID, err)
	}
	values := valuesOf("feature", map[string]Value{
		"id": StringValue(feature.ID), "featureSet": StringValue(setID),
		"title": StringValue(feature.Title), "subtitle": optionalString(feature.Subtitle),
		"description": optionalString(feature.Description), "center": optionalPosition(feature.Center),
		"shard": Int64Value(feature.Shard), "geometry": BytesValue(geometry),
	})
	seen := make(map[ID]bool, len(feature.Properties))
	for _, property := range feature.Properties {
		propertyID := property.fieldID()
		field, held := contract[propertyID]
		if !held || seen[propertyID] || property.Value.Kind != field.Kind {
			return fmt.Errorf("feature %s has an undeclared, duplicate, or mistyped property %s", feature.ID, propertyID)
		}
		seen[propertyID] = true
		values[propertyID] = property.Value
	}
	for id, field := range contract {
		if !field.Optional && !seen[id] {
			return fmt.Errorf("feature %s omits required property %s", feature.ID, id)
		}
	}
	if err := builders[featureTableName].add(values); err != nil {
		return err
	}
	return compileEvidence(feature, builders)
}

func compileEvidence(feature Feature, builders map[string]*tableBuilder) error {
	for _, relationship := range feature.Relationships {
		if err := builders[relationshipTableName].add(valuesOf("relationship", map[string]Value{
			"feature": StringValue(feature.ID), "predicate": StringValue(relationship.Predicate), "target": StringValue(relationship.Target),
		})); err != nil {
			return err
		}
	}
	for _, provenance := range feature.Provenance {
		if err := builders[provenanceTableName].add(valuesOf("provenance", map[string]Value{
			"feature": StringValue(feature.ID), "source": StringValue(provenance.Source),
			"nativeID": StringValue(provenance.NativeID), "capturedAt": StringValue(provenance.CapturedAt),
		})); err != nil {
			return err
		}
	}
	return nil
}

func compileRaster(worldID string, raster RasterPyramid, builders map[string]*tableBuilder) error {
	if err := builders[rasterTableName].add(valuesOf("rasterPyramid", map[string]Value{
		"id": StringValue(raster.ID), "world": StringValue(worldID), "name": StringValue(raster.Name), "codec": StringValue(raster.Codec),
		"tileSize": Int64Value(raster.TileSize), "minZoom": Int64Value(raster.MinZoom),
		"maxZoom": Int64Value(raster.MaxZoom), "fullZoom": Int64Value(raster.FullZoom),
		"sourceZoom": Int64Value(raster.SourceZoom), "template": StringValue(raster.Template),
		"bounds": optionalRect(raster.Bounds), "surface": optionalRect(raster.Surface),
		"interpolate": BoolValue(raster.Interpolate), "background": optionalString(raster.Background), "shard": Int64Value(raster.Shard),
	})); err != nil {
		return err
	}
	for index, format := range raster.Formats {
		if err := builders[rasterFormatTableName].add(valuesOf("rasterFormat", map[string]Value{
			"raster": StringValue(raster.ID), "zoom": Int64Value(raster.MinZoom + int64(index)), "format": StringValue(format),
		})); err != nil {
			return err
		}
	}
	for _, coverage := range raster.Coverage {
		if err := builders[rasterCoverageTableName].add(valuesOf("rasterCoverage", map[string]Value{
			"raster": StringValue(raster.ID), "zoom": Int64Value(coverage.Zoom),
			"x": Int64Value(coverage.X), "y": Int64Value(coverage.Y), "w": Int64Value(coverage.W), "h": Int64Value(coverage.H),
			"bits": BytesValue(coverage.Bits),
		})); err != nil {
			return err
		}
	}
	return nil
}

func compilePresentation(worldID string, presentation Presentation, builders map[string]*tableBuilder) error {
	if err := builders[presentationTableName].add(valuesOf("presentation", map[string]Value{
		"id": StringValue(presentation.ID), "world": StringValue(worldID), "title": StringValue(presentation.Title),
	})); err != nil {
		return err
	}
	for _, style := range presentation.Styles {
		if err := builders[styleTableName].add(valuesOf("style", map[string]Value{
			"id": StringValue(style.ID), "presentation": StringValue(presentation.ID),
			"symbol": optionalString(style.Symbol), "icon": optionalString(style.Icon), "iconAsset": optionalString(style.IconAsset),
			"iconPicture": BoolValue(style.IconPicture), "renderAs": optionalString(style.RenderAs),
			"stroke": optionalString(style.Stroke), "fill": optionalString(style.Fill),
		})); err != nil {
			return err
		}
	}
	for _, layer := range presentation.Layers {
		if err := builders[layerTableName].add(valuesOf("layer", map[string]Value{
			"id": StringValue(layer.ID), "presentation": StringValue(presentation.ID),
			"featureSet": StringValue(layer.FeatureSet), "style": StringValue(layer.Style),
			"group": optionalString(layer.Group), "labelPolicy": optionalString(layer.LabelPolicy), "visible": BoolValue(layer.Visible),
			"order": Int64Value(layer.Order), "minZoom": Int64Value(layer.MinZoom), "maxZoom": Int64Value(layer.MaxZoom),
		})); err != nil {
			return err
		}
	}
	for _, legend := range presentation.Legend {
		if err := builders[legendTableName].add(valuesOf("legend", map[string]Value{
			"presentation": StringValue(presentation.ID), "layer": StringValue(legend.Layer),
			"label": StringValue(legend.Label), "order": Int64Value(legend.Order),
		})); err != nil {
			return err
		}
	}
	return nil
}

func compileAssets(assets []Asset, builder *tableBuilder) ([]Blob, error) {
	blobs := make([]Blob, 0, len(assets))
	held := make(map[string]bool, len(assets))
	for _, asset := range assets {
		digest := sha256.Sum256(asset.Data)
		path := fmt.Sprintf("assets/%x", digest)
		if err := builder.add(valuesOf("asset", map[string]Value{
			"id": StringValue(asset.ID), "mediaType": StringValue(asset.MediaType),
			"path": StringValue(path), "provenance": optionalString(asset.Provenance),
		})); err != nil {
			return nil, err
		}
		if !held[path] {
			held[path] = true
			blobs = append(blobs, Blob{Name: path, Data: append([]byte(nil), asset.Data...)})
		}
	}
	slices.SortFunc(blobs, func(left, right Blob) int {
		if left.Name < right.Name {
			return -1
		}
		if left.Name > right.Name {
			return 1
		}
		return 0
	})
	return blobs, nil
}

func valuesOf(typeName string, values map[string]Value) map[ID]Value {
	out := make(map[ID]Value, len(values))
	for name, value := range values {
		out[CoreID(typeName+"."+name)] = value
	}
	return out
}

func optionalString(value string) Value {
	if value == "" {
		return NullValue()
	}
	return StringValue(value)
}

func addClaims(values map[ID]Value, claims []Property) {
	for _, claim := range claims {
		values[claim.fieldID()] = claim.Value
	}
}

func optionalRect(rect *RasterRect) Value {
	if rect == nil {
		return NullValue()
	}
	return BytesValue(encodeRect(*rect))
}

func optionalPosition(position *Position) Value {
	if position == nil {
		return NullValue()
	}
	out := make([]byte, 16)
	binary.LittleEndian.PutUint64(out, math.Float64bits(position[0]))
	binary.LittleEndian.PutUint64(out[8:], math.Float64bits(position[1]))
	return BytesValue(out)
}

func encodeExtent(extent [4]float64) []byte {
	out := make([]byte, 32)
	for index, coordinate := range extent {
		binary.LittleEndian.PutUint64(out[index*8:], math.Float64bits(coordinate))
	}
	return out
}

func encodeGeometry(geometry Geometry) ([]byte, error) {
	if err := validateGeometry(geometry); err != nil {
		return nil, err
	}
	out := []byte{byte(geometry.Kind)}
	out = binary.LittleEndian.AppendUint32(out, uint32(len(geometry.Parts)))
	for _, part := range geometry.Parts {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(part.Rings)))
		for _, ring := range part.Rings {
			out = binary.LittleEndian.AppendUint32(out, uint32(len(ring)))
			for _, position := range ring {
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(position[0]))
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(position[1]))
			}
		}
	}
	return out, nil
}

func encodeRect(rect RasterRect) []byte {
	return encodeExtent([4]float64{rect.X, rect.Y, rect.Width, rect.Height})
}
