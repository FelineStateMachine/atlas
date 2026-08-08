package vnext

import (
	"encoding/binary"
	"fmt"
	"math"
)

type setLocation struct {
	world int
	set   int
}

// Volume reconstructs the semantic graph from typed tables. No presentation
// policy is applied here.
func (reader *Reader) Volume() (Volume, error) {
	tables := make(map[string]Table, len(reader.tables))
	for _, name := range tableNames() {
		table, err := reader.Table(name)
		if err != nil {
			return Volume{}, err
		}
		tables[name] = table
	}
	return decodeVolume(reader, tables)
}

func decodeVolume(reader *Reader, tables map[string]Table) (Volume, error) {
	root := tables[volumeTableName]
	if root.Rows != 1 {
		return Volume{}, fmt.Errorf("volume table has %d rows", root.Rows)
	}
	volume := Volume{
		ID:    requiredString(root, 0, "volume.id"),
		Title: requiredString(root, 0, "volume.title"),
	}
	worlds, worldIndex, err := decodeWorlds(tables)
	if err != nil {
		return Volume{}, err
	}
	volume.Worlds = worlds
	sets, err := decodeFeatureSets(&volume, tables, worldIndex)
	if err != nil {
		return Volume{}, err
	}
	features, err := decodeFeatures(&volume, tables, sets)
	if err != nil {
		return Volume{}, err
	}
	if err := decodeEvidence(&volume, tables, features); err != nil {
		return Volume{}, err
	}
	if err := decodeWorldDetails(&volume, tables, worldIndex); err != nil {
		return Volume{}, err
	}
	assets, err := decodeAssets(reader, tables[assetTableName])
	if err != nil {
		return Volume{}, err
	}
	volume.Assets = assets
	if err := validateVolume(volume); err != nil {
		return Volume{}, fmt.Errorf("decoded volume: %w", err)
	}
	return volume, nil
}

func decodeWorlds(tables map[string]Table) ([]World, map[string]int, error) {
	table := tables[worldTableName]
	worlds := make([]World, table.Rows)
	index := make(map[string]int, table.Rows)
	for row := 0; row < table.Rows; row++ {
		worlds[row] = World{
			ID:              requiredString(table, row, "world.id"),
			Title:           requiredString(table, row, "world.title"),
			CoordinateSpace: CoordinateSpace{ID: requiredString(table, row, "world.coordinateSpace")},
			Claims:          extensionProperties(table, row, "world"),
		}
		index[worlds[row].ID] = row
	}
	coordinates := tables[coordinateTableName]
	for row := 0; row < coordinates.Rows; row++ {
		worldID := requiredString(coordinates, row, "coordinate.world")
		at, held := index[worldID]
		if !held {
			return nil, nil, fmt.Errorf("coordinate space refers to unknown world %s", worldID)
		}
		extent, err := decodeExtent(requiredBytes(coordinates, row, "coordinate.extent"))
		if err != nil {
			return nil, nil, err
		}
		worlds[at].CoordinateSpace = CoordinateSpace{
			ID:         requiredString(coordinates, row, "coordinate.id"),
			Kind:       requiredString(coordinates, row, "coordinate.kind"),
			Unit:       requiredString(coordinates, row, "coordinate.unit"),
			Definition: requiredString(coordinates, row, "coordinate.definition"),
			Extent:     extent,
			SourceZoom: requiredInt64(coordinates, row, "coordinate.sourceZoom"),
			FirstTile:  requiredInt64(coordinates, row, "coordinate.firstTile"),
			TileSize:   requiredInt64(coordinates, row, "coordinate.tileSize"),
			Size:       requiredInt64(coordinates, row, "coordinate.size"),
		}
	}
	return worlds, index, nil
}

func decodeFeatureSets(volume *Volume, tables map[string]Table, worlds map[string]int) (map[string]setLocation, error) {
	table := tables[featureSetTableName]
	sets := make(map[string]setLocation, table.Rows)
	for row := 0; row < table.Rows; row++ {
		worldID := requiredString(table, row, "featureSet.world")
		world, held := worlds[worldID]
		if !held {
			return nil, fmt.Errorf("feature set refers to unknown world %s", worldID)
		}
		set := FeatureSet{
			ID:           requiredString(table, row, "featureSet.id"),
			Title:        requiredString(table, row, "featureSet.title"),
			SemanticType: requiredString(table, row, "featureSet.semanticType"),
			Claims:       extensionProperties(table, row, "featureSet"),
		}
		volume.Worlds[world].FeatureSets = append(volume.Worlds[world].FeatureSets, set)
		sets[set.ID] = setLocation{world: world, set: len(volume.Worlds[world].FeatureSets) - 1}
	}
	definitions := tables[propertyDefinitionTableName]
	for row := 0; row < definitions.Rows; row++ {
		setID := requiredString(definitions, row, "propertyDefinition.featureSet")
		location, held := sets[setID]
		if !held {
			return nil, fmt.Errorf("property definition refers to unknown feature set %s", setID)
		}
		field := Field{
			ID:       requiredID(definitions, row, "propertyDefinition.field"),
			Name:     requiredString(definitions, row, "propertyDefinition.name"),
			Kind:     Kind(requiredInt64(definitions, row, "propertyDefinition.kind")),
			Optional: requiredBool(definitions, row, "propertyDefinition.optional"),
		}
		set := &volume.Worlds[location.world].FeatureSets[location.set]
		set.Properties = append(set.Properties, field)
	}
	return sets, nil
}

func decodeFeatures(volume *Volume, tables map[string]Table, sets map[string]setLocation) (map[string]setLocation, error) {
	table := tables[featureTableName]
	features := make(map[string]setLocation, table.Rows)
	core, _ := StandardSchema().Type(CoreID("type.feature"))
	for row := 0; row < table.Rows; row++ {
		setID := requiredString(table, row, "feature.featureSet")
		location, held := sets[setID]
		if !held {
			return nil, fmt.Errorf("feature refers to unknown feature set %s", setID)
		}
		geometry, err := decodeGeometry(requiredBytes(table, row, "feature.geometry"))
		if err != nil {
			return nil, err
		}
		feature := Feature{
			ID: requiredString(table, row, "feature.id"), Title: requiredString(table, row, "feature.title"),
			Subtitle: optionalStringAt(table, row, "feature.subtitle"), Description: optionalStringAt(table, row, "feature.description"),
			Shard: requiredInt64(table, row, "feature.shard"), Geometry: geometry,
		}
		if center := optionalBytesAt(table, row, "feature.center"); center != nil {
			position, err := decodePosition(center)
			if err != nil {
				return nil, err
			}
			feature.Center = &position
		}
		for _, column := range table.Columns {
			if _, standard := core.Field(column.Field.ID); standard || column.Values[row].Kind == KindInvalid {
				continue
			}
			feature.Properties = append(feature.Properties, Property{FieldID: column.Field.ID, Field: column.Field, Value: column.Values[row]})
		}
		set := &volume.Worlds[location.world].FeatureSets[location.set]
		set.Features = append(set.Features, feature)
		features[feature.ID] = setLocation{world: location.world, set: location.set}
	}
	return features, nil
}

func decodeEvidence(volume *Volume, tables map[string]Table, features map[string]setLocation) error {
	for _, spec := range []struct {
		name   string
		decode func(*Feature, Table, int)
	}{{relationshipTableName, decodeRelationship}, {provenanceTableName, decodeProvenance}} {
		table := tables[spec.name]
		for row := 0; row < table.Rows; row++ {
			featureID := requiredString(table, row, tablePrefix(spec.name)+".feature")
			location, held := features[featureID]
			if !held {
				return fmt.Errorf("evidence refers to unknown feature %s", featureID)
			}
			feature := findFeature(volume, location, featureID)
			spec.decode(feature, table, row)
		}
	}
	return nil
}

func findFeature(volume *Volume, location setLocation, id string) *Feature {
	features := volume.Worlds[location.world].FeatureSets[location.set].Features
	for index := range features {
		if features[index].ID == id {
			return &features[index]
		}
	}
	return nil
}

func decodeRelationship(feature *Feature, table Table, row int) {
	feature.Relationships = append(feature.Relationships, Relationship{
		Predicate: requiredString(table, row, "relationship.predicate"),
		Target:    requiredString(table, row, "relationship.target"),
	})
}

func decodeProvenance(feature *Feature, table Table, row int) {
	feature.Provenance = append(feature.Provenance, Provenance{
		Source:     requiredString(table, row, "provenance.source"),
		NativeID:   requiredString(table, row, "provenance.nativeID"),
		CapturedAt: requiredString(table, row, "provenance.capturedAt"),
	})
}

func tablePrefix(name string) string {
	if name == relationshipTableName {
		return "relationship"
	}
	return "provenance"
}

func decodeWorldDetails(volume *Volume, tables map[string]Table, worlds map[string]int) error {
	if err := decodeRasters(volume, tables, worlds); err != nil {
		return err
	}
	presentations, err := decodePresentations(volume, tables[presentationTableName], worlds)
	if err != nil {
		return err
	}
	if err := decodeStyles(volume, tables[styleTableName], presentations); err != nil {
		return err
	}
	if err := decodeLayers(volume, tables[layerTableName], presentations); err != nil {
		return err
	}
	return decodeLegend(volume, tables[legendTableName], presentations)
}

func decodeRasters(volume *Volume, tables map[string]Table, worlds map[string]int) error {
	table := tables[rasterTableName]
	rasterLocations := make(map[string][2]int, table.Rows)
	for row := 0; row < table.Rows; row++ {
		worldID := requiredString(table, row, "rasterPyramid.world")
		world, held := worlds[worldID]
		if !held {
			return fmt.Errorf("raster pyramid refers to unknown world %s", worldID)
		}
		raster := RasterPyramid{
			ID: requiredString(table, row, "rasterPyramid.id"), Name: requiredString(table, row, "rasterPyramid.name"), Codec: requiredString(table, row, "rasterPyramid.codec"),
			TileSize: requiredInt64(table, row, "rasterPyramid.tileSize"), MinZoom: requiredInt64(table, row, "rasterPyramid.minZoom"),
			MaxZoom: requiredInt64(table, row, "rasterPyramid.maxZoom"), FullZoom: requiredInt64(table, row, "rasterPyramid.fullZoom"),
			SourceZoom: requiredInt64(table, row, "rasterPyramid.sourceZoom"), Template: requiredString(table, row, "rasterPyramid.template"),
			Interpolate: requiredBool(table, row, "rasterPyramid.interpolate"), Background: optionalStringAt(table, row, "rasterPyramid.background"),
			Shard: requiredInt64(table, row, "rasterPyramid.shard"),
		}
		var err error
		if data := optionalBytesAt(table, row, "rasterPyramid.bounds"); data != nil {
			raster.Bounds, err = decodeRect(data)
			if err != nil {
				return err
			}
		}
		if data := optionalBytesAt(table, row, "rasterPyramid.surface"); data != nil {
			raster.Surface, err = decodeRect(data)
			if err != nil {
				return err
			}
		}
		volume.Worlds[world].RasterPyramids = append(volume.Worlds[world].RasterPyramids, raster)
		rasterLocations[raster.ID] = [2]int{world, len(volume.Worlds[world].RasterPyramids) - 1}
	}
	formats := tables[rasterFormatTableName]
	for row := 0; row < formats.Rows; row++ {
		location, held := rasterLocations[requiredString(formats, row, "rasterFormat.raster")]
		if !held {
			return fmt.Errorf("raster format refers to unknown raster")
		}
		raster := &volume.Worlds[location[0]].RasterPyramids[location[1]]
		raster.Formats = append(raster.Formats, requiredString(formats, row, "rasterFormat.format"))
	}
	coverage := tables[rasterCoverageTableName]
	for row := 0; row < coverage.Rows; row++ {
		location, held := rasterLocations[requiredString(coverage, row, "rasterCoverage.raster")]
		if !held {
			return fmt.Errorf("raster coverage refers to unknown raster")
		}
		raster := &volume.Worlds[location[0]].RasterPyramids[location[1]]
		raster.Coverage = append(raster.Coverage, RasterCoverage{
			Zoom: requiredInt64(coverage, row, "rasterCoverage.zoom"), X: requiredInt64(coverage, row, "rasterCoverage.x"),
			Y: requiredInt64(coverage, row, "rasterCoverage.y"), W: requiredInt64(coverage, row, "rasterCoverage.w"),
			H: requiredInt64(coverage, row, "rasterCoverage.h"), Bits: requiredBytes(coverage, row, "rasterCoverage.bits"),
		})
	}
	return nil
}

func decodePresentations(volume *Volume, table Table, worlds map[string]int) (map[string]int, error) {
	presentations := make(map[string]int, table.Rows)
	for row := 0; row < table.Rows; row++ {
		worldID := requiredString(table, row, "presentation.world")
		world, held := worlds[worldID]
		if !held {
			return nil, fmt.Errorf("presentation refers to unknown world %s", worldID)
		}
		presentation := Presentation{ID: requiredString(table, row, "presentation.id"), Title: requiredString(table, row, "presentation.title")}
		volume.Worlds[world].Presentation = presentation
		presentations[presentation.ID] = world
	}
	return presentations, nil
}

func decodeStyles(volume *Volume, table Table, presentations map[string]int) error {
	for row := 0; row < table.Rows; row++ {
		presentationID := requiredString(table, row, "style.presentation")
		world, held := presentations[presentationID]
		if !held {
			return fmt.Errorf("style refers to unknown presentation %s", presentationID)
		}
		volume.Worlds[world].Presentation.Styles = append(volume.Worlds[world].Presentation.Styles, Style{
			ID: requiredString(table, row, "style.id"), Symbol: optionalStringAt(table, row, "style.symbol"),
			Icon: optionalStringAt(table, row, "style.icon"), IconAsset: optionalStringAt(table, row, "style.iconAsset"),
			IconPicture: requiredBool(table, row, "style.iconPicture"), RenderAs: optionalStringAt(table, row, "style.renderAs"),
			Stroke: optionalStringAt(table, row, "style.stroke"), Fill: optionalStringAt(table, row, "style.fill"),
		})
	}
	return nil
}

func decodeLayers(volume *Volume, table Table, presentations map[string]int) error {
	for row := 0; row < table.Rows; row++ {
		presentationID := requiredString(table, row, "layer.presentation")
		world, held := presentations[presentationID]
		if !held {
			return fmt.Errorf("layer refers to unknown presentation %s", presentationID)
		}
		volume.Worlds[world].Presentation.Layers = append(volume.Worlds[world].Presentation.Layers, Layer{
			ID: requiredString(table, row, "layer.id"), FeatureSet: requiredString(table, row, "layer.featureSet"),
			Style: requiredString(table, row, "layer.style"), Order: requiredInt64(table, row, "layer.order"),
			Group: optionalStringAt(table, row, "layer.group"), LabelPolicy: optionalStringAt(table, row, "layer.labelPolicy"),
			Visible: requiredBool(table, row, "layer.visible"),
			MinZoom: requiredInt64(table, row, "layer.minZoom"), MaxZoom: requiredInt64(table, row, "layer.maxZoom"),
		})
	}
	return nil
}

func decodeLegend(volume *Volume, table Table, presentations map[string]int) error {
	for row := 0; row < table.Rows; row++ {
		presentationID := requiredString(table, row, "legend.presentation")
		world, held := presentations[presentationID]
		if !held {
			return fmt.Errorf("legend refers to unknown presentation %s", presentationID)
		}
		volume.Worlds[world].Presentation.Legend = append(volume.Worlds[world].Presentation.Legend, LegendEntry{
			Layer: requiredString(table, row, "legend.layer"), Label: requiredString(table, row, "legend.label"),
			Order: requiredInt64(table, row, "legend.order"),
		})
	}
	return nil
}

func decodeAssets(reader *Reader, table Table) ([]Asset, error) {
	assets := make([]Asset, table.Rows)
	for row := 0; row < table.Rows; row++ {
		path := requiredString(table, row, "asset.path")
		data, err := reader.Blob(path)
		if err != nil {
			return nil, err
		}
		assets[row] = Asset{
			ID: requiredString(table, row, "asset.id"), MediaType: requiredString(table, row, "asset.mediaType"),
			Data: data, Provenance: optionalStringAt(table, row, "asset.provenance"),
		}
	}
	return assets, nil
}

func requiredValue(table Table, row int, name string) Value {
	value, held := table.Value(CoreID(name), row)
	if !held || value.Kind == KindInvalid {
		return Value{}
	}
	return value
}

func requiredString(table Table, row int, name string) string {
	return requiredValue(table, row, name).String
}
func requiredInt64(table Table, row int, name string) int64 {
	return requiredValue(table, row, name).Int64
}
func requiredBool(table Table, row int, name string) bool {
	return requiredValue(table, row, name).Bool
}
func requiredID(table Table, row int, name string) ID { return requiredValue(table, row, name).ID }
func requiredBytes(table Table, row int, name string) []byte {
	return requiredValue(table, row, name).Bytes
}

func optionalStringAt(table Table, row int, name string) string {
	value, _ := table.Value(CoreID(name), row)
	return value.String
}

func optionalBytesAt(table Table, row int, name string) []byte {
	value, held := table.Value(CoreID(name), row)
	if !held || value.Kind == KindInvalid {
		return nil
	}
	return value.Bytes
}

func extensionProperties(table Table, row int, typeName string) []Property {
	core, _ := StandardSchema().Type(CoreID("type." + typeName))
	var properties []Property
	for _, column := range table.Columns {
		if _, standard := core.Field(column.Field.ID); standard || column.Values[row].Kind == KindInvalid {
			continue
		}
		properties = append(properties, Property{FieldID: column.Field.ID, Field: column.Field, Value: column.Values[row]})
	}
	return properties
}

func decodeExtent(data []byte) ([4]float64, error) {
	var extent [4]float64
	if len(data) != len(extent)*8 {
		return extent, fmt.Errorf("coordinate extent has %d bytes", len(data))
	}
	for index := range extent {
		extent[index] = math.Float64frombits(binary.LittleEndian.Uint64(data[index*8:]))
	}
	return extent, nil
}

func decodeGeometry(data []byte) (Geometry, error) {
	if len(data) < 5 {
		return Geometry{}, fmt.Errorf("geometry is truncated")
	}
	cursor := 1
	readCount := func() (int, error) {
		if cursor+4 > len(data) {
			return 0, fmt.Errorf("geometry is truncated")
		}
		count := int(binary.LittleEndian.Uint32(data[cursor : cursor+4]))
		cursor += 4
		return count, nil
	}
	parts, err := readCount()
	if err != nil {
		return Geometry{}, err
	}
	geometry := Geometry{Kind: GeometryKind(data[0]), Parts: make([]GeometryPart, parts)}
	for part := range geometry.Parts {
		rings, err := readCount()
		if err != nil {
			return Geometry{}, err
		}
		geometry.Parts[part].Rings = make([][]Position, rings)
		for ring := range geometry.Parts[part].Rings {
			positions, err := readCount()
			if err != nil {
				return Geometry{}, err
			}
			if positions > (len(data)-cursor)/16 {
				return Geometry{}, fmt.Errorf("geometry position payload is invalid")
			}
			geometry.Parts[part].Rings[ring] = make([]Position, positions)
			for position := range geometry.Parts[part].Rings[ring] {
				geometry.Parts[part].Rings[ring][position][0] = math.Float64frombits(binary.LittleEndian.Uint64(data[cursor:]))
				cursor += 8
				geometry.Parts[part].Rings[ring][position][1] = math.Float64frombits(binary.LittleEndian.Uint64(data[cursor:]))
				cursor += 8
			}
		}
	}
	if cursor != len(data) {
		return Geometry{}, fmt.Errorf("geometry has trailing bytes")
	}
	if _, err := encodeGeometry(geometry); err != nil {
		return Geometry{}, err
	}
	return geometry, nil
}

func decodePosition(data []byte) (Position, error) {
	if len(data) != 16 {
		return Position{}, fmt.Errorf("position has %d bytes", len(data))
	}
	return Position{
		math.Float64frombits(binary.LittleEndian.Uint64(data)),
		math.Float64frombits(binary.LittleEndian.Uint64(data[8:])),
	}, nil
}

func decodeRect(data []byte) (*RasterRect, error) {
	extent, err := decodeExtent(data)
	if err != nil {
		return nil, err
	}
	return &RasterRect{X: extent[0], Y: extent[1], Width: extent[2], Height: extent[3]}, nil
}
