package vnext

// Volume is the durable subject of one Atlas file.
type Volume struct {
	ID     string
	Title  string
	Worlds []World
	Assets []Asset
}

// World is one coherent spatial environment or snapshot.
type World struct {
	ID              string
	Title           string
	CoordinateSpace CoordinateSpace
	Claims          []Property
	FeatureSets     []FeatureSet
	RasterPyramids  []RasterPyramid
	Presentation    Presentation
}

// CoordinateSpace is the authoritative offline frame for a world.
type CoordinateSpace struct {
	ID         string
	Kind       string
	Unit       string
	Definition string
	Extent     [4]float64
	SourceZoom int64
	FirstTile  int64
	TileSize   int64
	Size       int64
}

// FeatureSet carries semantic membership and its typed property contract.
type FeatureSet struct {
	ID           string
	Title        string
	SemanticType string
	Properties   []Field
	Claims       []Property
	Features     []Feature
}

// Feature separates spatial facts, typed claims, relationships, and evidence.
type Feature struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	Center        *Position
	Shard         int64
	Geometry      Geometry
	Properties    []Property
	Relationships []Relationship
	Provenance    []Provenance
}

// GeometryKind identifies the interpretation of a coordinate sequence.
type GeometryKind uint8

const (
	GeometryPoint GeometryKind = iota + 1
	GeometryLineString
	GeometryPolygon
)

// Position is one x,y pair in a world's CoordinateSpace.
type Position [2]float64

// GeometryPart is one point, line, or polygon. Lines carry one ring; polygons
// carry an exterior ring followed by any holes.
type GeometryPart struct {
	Rings [][]Position
}

// Geometry holds every part of a point, multi-line, or multi-polygon without
// flattening away the boundaries between pieces and holes.
type Geometry struct {
	Kind  GeometryKind
	Parts []GeometryPart
}

// Property is one typed feature claim identified independently of its label.
type Property struct {
	FieldID ID
	Field   Field
	Value   Value
}

func (property Property) fieldID() ID {
	if !property.FieldID.isZero() {
		return property.FieldID
	}
	return property.Field.ID
}

// Relationship is a first-class edge to another entity.
type Relationship struct {
	Predicate string
	Target    string
}

// Provenance records where a claim or feature came from.
type Provenance struct {
	Source     string
	NativeID   string
	CapturedAt string
}

// RasterPyramid describes an offline tile matrix whose bytes remain opaque.
type RasterPyramid struct {
	ID          string
	Name        string
	Codec       string
	TileSize    int64
	MinZoom     int64
	MaxZoom     int64
	FullZoom    int64
	SourceZoom  int64
	Template    string
	Formats     []string
	Bounds      *RasterRect
	Surface     *RasterRect
	Interpolate bool
	Background  string
	Shard       int64
	Coverage    []RasterCoverage
}

type RasterRect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

type RasterCoverage struct {
	Zoom int64
	X    int64
	Y    int64
	W    int64
	H    int64
	Bits []byte
}

// Presentation is replaceable render intent over semantic feature sets.
type Presentation struct {
	ID     string
	Title  string
	Layers []Layer
	Styles []Style
	Legend []LegendEntry
}

// Layer selects a FeatureSet and applies one Style over a visibility range.
type Layer struct {
	ID          string
	FeatureSet  string
	Style       string
	Group       string
	LabelPolicy string
	Visible     bool
	Order       int64
	MinZoom     int64
	MaxZoom     int64
}

// Style names the appearance of a layer without changing feature meaning.
type Style struct {
	ID          string
	Symbol      string
	Icon        string
	IconAsset   string
	IconPicture bool
	RenderAs    string
	Stroke      string
	Fill        string
}

// LegendEntry gives a layer its presented label and order.
type LegendEntry struct {
	Layer string
	Label string
	Order int64
}

// Asset is a content-bearing supporting blob.
type Asset struct {
	ID         string
	MediaType  string
	Path       string
	Data       []byte
	Provenance string
}

func coreField(name string, kind Kind, optional bool) Field {
	return Field{ID: CoreID(name), Name: name, Kind: kind, Optional: optional}
}

func coreType(name string, fields ...Field) Type {
	return Type{ID: CoreID("type." + name), Name: name, Fields: fields}
}

// StandardSchema is the Atlas-owned vocabulary. Extensions union into these
// types by ID and require no format release or central allocation.
func StandardSchema() Schema {
	return canonicalSchema(Schema{Types: []Type{
		coreType("volume",
			coreField("volume.id", KindString, false),
			coreField("volume.title", KindString, false)),
		coreType("world",
			coreField("world.id", KindString, false),
			coreField("world.title", KindString, false),
			coreField("world.coordinateSpace", KindString, false)),
		coreType("coordinate",
			coreField("coordinate.id", KindString, false),
			coreField("coordinate.world", KindString, false),
			coreField("coordinate.kind", KindString, false),
			coreField("coordinate.unit", KindString, false),
			coreField("coordinate.definition", KindString, false),
			coreField("coordinate.extent", KindBytes, false),
			coreField("coordinate.sourceZoom", KindInt64, false),
			coreField("coordinate.firstTile", KindInt64, false),
			coreField("coordinate.tileSize", KindInt64, false),
			coreField("coordinate.size", KindInt64, false)),
		coreType("featureSet",
			coreField("featureSet.id", KindString, false),
			coreField("featureSet.world", KindString, false),
			coreField("featureSet.title", KindString, false),
			coreField("featureSet.semanticType", KindString, false)),
		coreType("propertyDefinition",
			coreField("propertyDefinition.featureSet", KindString, false),
			coreField("propertyDefinition.field", KindID, false),
			coreField("propertyDefinition.name", KindString, false),
			coreField("propertyDefinition.kind", KindInt64, false),
			coreField("propertyDefinition.optional", KindBool, false)),
		coreType("feature",
			coreField("feature.id", KindString, false),
			coreField("feature.featureSet", KindString, false),
			coreField("feature.title", KindString, false),
			coreField("feature.subtitle", KindString, true),
			coreField("feature.description", KindString, true),
			coreField("feature.center", KindBytes, true),
			coreField("feature.shard", KindInt64, false),
			coreField("feature.geometry", KindBytes, false)),
		coreType("relationship",
			coreField("relationship.feature", KindString, false),
			coreField("relationship.predicate", KindString, false),
			coreField("relationship.target", KindString, false)),
		coreType("provenance",
			coreField("provenance.feature", KindString, false),
			coreField("provenance.source", KindString, false),
			coreField("provenance.nativeID", KindString, false),
			coreField("provenance.capturedAt", KindString, false)),
		coreType("rasterPyramid",
			coreField("rasterPyramid.id", KindString, false),
			coreField("rasterPyramid.world", KindString, false),
			coreField("rasterPyramid.name", KindString, false),
			coreField("rasterPyramid.codec", KindString, false),
			coreField("rasterPyramid.tileSize", KindInt64, false),
			coreField("rasterPyramid.minZoom", KindInt64, false),
			coreField("rasterPyramid.maxZoom", KindInt64, false),
			coreField("rasterPyramid.fullZoom", KindInt64, false),
			coreField("rasterPyramid.sourceZoom", KindInt64, false),
			coreField("rasterPyramid.template", KindString, false),
			coreField("rasterPyramid.bounds", KindBytes, true),
			coreField("rasterPyramid.surface", KindBytes, true),
			coreField("rasterPyramid.interpolate", KindBool, false),
			coreField("rasterPyramid.background", KindString, true),
			coreField("rasterPyramid.shard", KindInt64, false)),
		coreType("rasterFormat",
			coreField("rasterFormat.raster", KindString, false),
			coreField("rasterFormat.zoom", KindInt64, false),
			coreField("rasterFormat.format", KindString, false)),
		coreType("rasterCoverage",
			coreField("rasterCoverage.raster", KindString, false),
			coreField("rasterCoverage.zoom", KindInt64, false),
			coreField("rasterCoverage.x", KindInt64, false),
			coreField("rasterCoverage.y", KindInt64, false),
			coreField("rasterCoverage.w", KindInt64, false),
			coreField("rasterCoverage.h", KindInt64, false),
			coreField("rasterCoverage.bits", KindBytes, false)),
		coreType("presentation",
			coreField("presentation.id", KindString, false),
			coreField("presentation.world", KindString, false),
			coreField("presentation.title", KindString, false)),
		coreType("style",
			coreField("style.id", KindString, false),
			coreField("style.presentation", KindString, false),
			coreField("style.symbol", KindString, true),
			coreField("style.icon", KindString, true),
			coreField("style.iconAsset", KindString, true),
			coreField("style.iconPicture", KindBool, false),
			coreField("style.renderAs", KindString, true),
			coreField("style.stroke", KindString, true),
			coreField("style.fill", KindString, true)),
		coreType("layer",
			coreField("layer.id", KindString, false),
			coreField("layer.presentation", KindString, false),
			coreField("layer.featureSet", KindString, false),
			coreField("layer.style", KindString, false),
			coreField("layer.group", KindString, true),
			coreField("layer.labelPolicy", KindString, true),
			coreField("layer.visible", KindBool, false),
			coreField("layer.order", KindInt64, false),
			coreField("layer.minZoom", KindInt64, false),
			coreField("layer.maxZoom", KindInt64, false)),
		coreType("legend",
			coreField("legend.presentation", KindString, false),
			coreField("legend.layer", KindString, false),
			coreField("legend.label", KindString, false),
			coreField("legend.order", KindInt64, false)),
		coreType("asset",
			coreField("asset.id", KindString, false),
			coreField("asset.mediaType", KindString, false),
			coreField("asset.path", KindString, false),
			coreField("asset.provenance", KindString, true)),
	}})
}
