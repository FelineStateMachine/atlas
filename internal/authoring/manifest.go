// Package authoring builds native Atlas volumes from portable project manifests.
//
// A project is configuration only. Captured evidence lives in a shared cache and
// completed .atlas files live in the library; neither is copied beside the
// manifest. This keeps one small file as the reproducible statement of intent.
package authoring

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"gopkg.in/yaml.v3"
)

const (
	ProjectSchema    = "atlas-project/v2"
	ProjectExtension = ".atlas-project"
)

// Project is the complete durable input to one Atlas build.
type Project struct {
	Schema          string               `yaml:"schema"`
	SchemaNamespace string               `yaml:"schema-namespace"`
	ID              string               `yaml:"id"`
	Title           string               `yaml:"title"`
	Target          Target               `yaml:"target"`
	FeatureSets     []FeatureSetContract `yaml:"feature-sets,omitempty"`
	Sources         []Source             `yaml:"sources,omitempty"`
	Rasters         []Raster             `yaml:"rasters,omitempty"`
	Assets          []Asset              `yaml:"assets,omitempty"`
	Presentation    Presentation         `yaml:"presentation"`
	Release         Release              `yaml:"release,omitempty"`
	Budgets         BuildBudgets         `yaml:"budgets,omitempty"`

	baseDir string
}

// Target states the one world and coordinate space every configured source
// must speak before semantic assembly begins.
type Target struct {
	World           string          `yaml:"world"`
	Title           string          `yaml:"title"`
	CoordinateSpace CoordinateSpace `yaml:"coordinate-space"`
}

type CoordinateSpace struct {
	ID         string     `yaml:"id"`
	Kind       string     `yaml:"kind"`
	Unit       string     `yaml:"unit"`
	Definition string     `yaml:"definition"`
	Extent     [4]float64 `yaml:"extent"`
	SourceZoom int64      `yaml:"source-zoom,omitempty"`
	OriginX    int64      `yaml:"origin-x,omitempty"`
	OriginY    int64      `yaml:"origin-y,omitempty"`
	// FirstTile is accepted only for legacy local projects while their
	// producers migrate to independent column and row origins.
	FirstTile int64 `yaml:"first-tile,omitempty"`
	TileSize  int64 `yaml:"tile-size,omitempty"`
	Size      int64 `yaml:"size,omitempty"`
}

// Source is one configured instance of a reusable feature adapter.
type Source struct {
	ID            string  `yaml:"id"`
	Adapter       string  `yaml:"adapter"`
	Locator       string  `yaml:"locator"`
	MediaType     string  `yaml:"media-type,omitempty"`
	License       string  `yaml:"license,omitempty"`
	Attribution   string  `yaml:"attribution,omitempty"`
	EstimateBytes int64   `yaml:"estimate-bytes,omitempty"`
	Query         Query   `yaml:"query,omitempty"`
	Mapping       Mapping `yaml:"mapping"`
}

// Query carries source-selection knobs interpreted only by the selected
// adapter. Adapter-specific transport details never enter Atlas semantics.
type Query struct {
	Layer            int    `yaml:"layer,omitempty"`
	Collection       string `yaml:"collection,omitempty"`
	Where            string `yaml:"where,omitempty"`
	Limit            int    `yaml:"limit,omitempty"`
	ObjectID         string `yaml:"object-id,omitempty"`
	SpatialReference int    `yaml:"spatial-reference,omitempty"`
}

// FeatureSetContract is the source-independent semantic and geometry contract
// populated by every mapping that targets its ID.
type FeatureSetContract struct {
	ID            string                 `yaml:"id"`
	Title         string                 `yaml:"title"`
	SemanticType  string                 `yaml:"semantic-type"`
	Geometry      string                 `yaml:"geometry"`
	Properties    []PropertyContract     `yaml:"properties,omitempty"`
	Relationships []RelationshipContract `yaml:"relationships,omitempty"`
}

type PropertyContract struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Optional bool   `yaml:"optional,omitempty"`
}

type RelationshipContract struct {
	ID         string `yaml:"id"`
	Predicate  string `yaml:"predicate"`
	FeatureSet string `yaml:"feature-set"`
	Optional   bool   `yaml:"optional,omitempty"`
}

// Mapping binds source paths to one declared FeatureSet contract. Sources that
// name the same feature-set and canonical identity meet during assembly.
type Mapping struct {
	FeatureSet   string        `yaml:"feature-set"`
	Identity     string        `yaml:"identity"`
	FeatureTitle string        `yaml:"feature-title"`
	Geometry     GeometryMap   `yaml:"geometry"`
	Properties   []PropertyMap `yaml:"properties,omitempty"`
	Relations    []RelationMap `yaml:"relationships,omitempty"`
}

// GeometryMap is the source-to-world geometry contract for one FeatureSet.
// Atlas intentionally executes only explicit identity and affine transforms;
// a CRS label alone never implies executable projection behavior.
type GeometryMap struct {
	Family      string              `yaml:"-" json:"-"`
	SourceSpace string              `yaml:"source-space"`
	Transform   CoordinateTransform `yaml:"transform"`
}

type CoordinateTransform struct {
	Kind   string     `yaml:"kind"`
	Matrix [6]float64 `yaml:"matrix,omitempty"`
}

type PropertyMap struct {
	Field  string `yaml:"field"`
	Source string `yaml:"source"`
}

type RelationMap struct {
	Relationship string `yaml:"relationship"`
	Target       string `yaml:"target"`
}

// Raster is a configured raster adapter. A file is deterministically expanded
// through MaxZoom; an xyz source names explicit tile windows so planning never
// probes the network.
type Raster struct {
	ID            string        `yaml:"id"`
	Name          string        `yaml:"name"`
	Adapter       string        `yaml:"adapter"`
	Locator       string        `yaml:"locator"`
	MediaType     string        `yaml:"media-type,omitempty"`
	License       string        `yaml:"license,omitempty"`
	Attribution   string        `yaml:"attribution,omitempty"`
	EstimateBytes int64         `yaml:"estimate-bytes,omitempty"`
	TileSize      int64         `yaml:"tile-size"`
	SourceZoom    int64         `yaml:"source-zoom,omitempty"`
	MaxZoom       int64         `yaml:"max-zoom,omitempty"`
	FullZoom      int64         `yaml:"full-zoom,omitempty"`
	Interpolate   bool          `yaml:"interpolate,omitempty"`
	Background    string        `yaml:"background,omitempty"`
	Bounds        *RasterRect   `yaml:"bounds,omitempty"`
	Surface       *RasterRect   `yaml:"surface,omitempty"`
	Shard         int64         `yaml:"shard,omitempty"`
	Levels        []RasterLevel `yaml:"levels,omitempty"`
}

type RasterRect struct {
	X      float64 `yaml:"x"`
	Y      float64 `yaml:"y"`
	Width  float64 `yaml:"width"`
	Height float64 `yaml:"height"`
}

type RasterLevel struct {
	Zoom int64 `yaml:"zoom"`
	MinX int64 `yaml:"min-x"`
	MinY int64 `yaml:"min-y"`
	MaxX int64 `yaml:"max-x"`
	MaxY int64 `yaml:"max-y"`
}

type Presentation struct {
	ID     string  `yaml:"id"`
	Title  string  `yaml:"title"`
	Styles []Style `yaml:"styles,omitempty"`
	Layers []Layer `yaml:"layers,omitempty"`
}

// Asset is authored supporting content packed into the immutable Atlas file.
type Asset struct {
	ID            string `yaml:"id"`
	Locator       string `yaml:"locator"`
	MediaType     string `yaml:"media-type"`
	Provenance    string `yaml:"provenance,omitempty"`
	License       string `yaml:"license,omitempty"`
	Attribution   string `yaml:"attribution,omitempty"`
	EstimateBytes int64  `yaml:"estimate-bytes,omitempty"`
}

type Style struct {
	ID          string `yaml:"id"`
	Symbol      string `yaml:"symbol,omitempty"`
	Icon        string `yaml:"icon,omitempty"`
	IconAsset   string `yaml:"icon-asset,omitempty"`
	IconPicture bool   `yaml:"icon-picture,omitempty"`
	RenderAs    string `yaml:"render-as,omitempty"`
	Stroke      string `yaml:"stroke,omitempty"`
	Fill        string `yaml:"fill,omitempty"`
}

type Layer struct {
	ID          string `yaml:"id"`
	FeatureSet  string `yaml:"feature-set"`
	Style       string `yaml:"style"`
	Label       string `yaml:"label"`
	Group       string `yaml:"group,omitempty"`
	LabelPolicy string `yaml:"label-policy,omitempty"`
	Visible     bool   `yaml:"visible"`
	Order       int64  `yaml:"order"`
	MinZoom     int64  `yaml:"min-zoom,omitempty"`
	MaxZoom     int64  `yaml:"max-zoom,omitempty"`
}

type Release struct {
	Revision int `yaml:"revision,omitempty"`
}

// BuildBudgets are reproducible refusal limits. Zero selects the stable
// compiler default; a manifest can choose a smaller or deliberately larger
// positive value, but evidence is never silently truncated.
type BuildBudgets struct {
	RequestBytes int64 `yaml:"request-bytes,omitempty" json:"requestBytes"`
	TotalBytes   int64 `yaml:"total-bytes,omitempty" json:"totalBytes"`
	Requests     int   `yaml:"requests,omitempty" json:"requests"`
	RasterTiles  int64 `yaml:"raster-tiles,omitempty" json:"rasterTiles"`
	RasterPixels int64 `yaml:"raster-pixels,omitempty" json:"rasterPixels"`
}

var defaultBuildBudgets = BuildBudgets{
	RequestBytes: 512 << 20,
	TotalBytes:   8 << 30,
	Requests:     100_000,
	RasterTiles:  90_000,
	RasterPixels: 16_000_000_000,
}

func (budgets BuildBudgets) resolved() (BuildBudgets, error) {
	if budgets.RequestBytes < 0 || budgets.TotalBytes < 0 || budgets.Requests < 0 || budgets.RasterTiles < 0 || budgets.RasterPixels < 0 {
		return BuildBudgets{}, fmt.Errorf("build budgets must be positive")
	}
	resolved := budgets
	if resolved.RequestBytes == 0 {
		resolved.RequestBytes = defaultBuildBudgets.RequestBytes
	}
	if resolved.TotalBytes == 0 {
		resolved.TotalBytes = defaultBuildBudgets.TotalBytes
	}
	if resolved.Requests == 0 {
		resolved.Requests = defaultBuildBudgets.Requests
	}
	if resolved.RasterTiles == 0 {
		resolved.RasterTiles = defaultBuildBudgets.RasterTiles
	}
	if resolved.RasterPixels == 0 {
		resolved.RasterPixels = defaultBuildBudgets.RasterPixels
	}
	if resolved.RequestBytes > resolved.TotalBytes {
		return BuildBudgets{}, fmt.Errorf("request byte budget %d exceeds total byte budget %d", resolved.RequestBytes, resolved.TotalBytes)
	}
	return resolved, nil
}

// LoadProject strictly decodes one manifest. Unknown fields fail so a typo
// cannot silently remove a source, mapping, or layer from a build.
func LoadProject(path string) (Project, error) {
	if filepath.Ext(path) != ProjectExtension {
		return Project{}, fmt.Errorf("project %s must use %s", filepath.Base(path), ProjectExtension)
	}
	file, err := os.Open(path)
	if err != nil {
		return Project{}, fmt.Errorf("open project: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var project Project
	if err := decoder.Decode(&project); err != nil {
		return Project{}, fmt.Errorf("decode project: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Project{}, fmt.Errorf("project carries more than one YAML document")
		}
		return Project{}, fmt.Errorf("decode project trailer: %w", err)
	}
	project.baseDir = filepath.Dir(path)
	if err := project.Validate(); err != nil {
		return Project{}, err
	}
	return project, nil
}

// ResolveLocator resolves only local locators. Remote locators remain exactly
// as authored so their request identity is portable across machines.
func (project Project) ResolveLocator(locator string) string {
	if isRemote(locator) || filepath.IsAbs(locator) {
		return locator
	}
	return filepath.Clean(filepath.Join(project.baseDir, filepath.FromSlash(locator)))
}

func (project Project) Validate() error {
	if project.Schema != ProjectSchema {
		return fmt.Errorf("project schema %q, want %q", project.Schema, ProjectSchema)
	}
	if err := vnext.ValidSlug(project.ID); err != nil {
		return fmt.Errorf("project identity: %w", err)
	}
	if project.Title == "" {
		return fmt.Errorf("project %s has no title", project.ID)
	}
	if strings.TrimSpace(project.SchemaNamespace) == "" {
		return fmt.Errorf("project %s has no schema namespace", project.ID)
	}
	if err := validateTarget(project.Target); err != nil {
		return err
	}
	if _, err := project.Budgets.resolved(); err != nil {
		return fmt.Errorf("project %s: %w", project.ID, err)
	}
	if len(project.Sources) == 0 && len(project.Rasters) == 0 {
		return fmt.Errorf("project %s configures no sources or rasters", project.ID)
	}
	sets, err := validateFeatureSetContracts(project.FeatureSets)
	if err != nil {
		return err
	}
	sourceIDs := make(map[string]bool)
	for _, source := range project.Sources {
		if err := validateSource(source, project.Target.CoordinateSpace, sets); err != nil {
			return err
		}
		if sourceIDs[source.ID] {
			return fmt.Errorf("source %s is configured twice", source.ID)
		}
		sourceIDs[source.ID] = true
	}
	rasterIDs := make(map[string]bool)
	for _, raster := range project.Rasters {
		if err := validateRaster(raster); err != nil {
			return err
		}
		if rasterIDs[raster.ID] {
			return fmt.Errorf("raster %s is configured twice", raster.ID)
		}
		rasterIDs[raster.ID] = true
	}
	assetIDs := make(map[string]bool)
	for _, asset := range project.Assets {
		if err := validateAsset(asset); err != nil {
			return err
		}
		if assetIDs[asset.ID] {
			return fmt.Errorf("asset %s is configured twice", asset.ID)
		}
		assetIDs[asset.ID] = true
	}
	return validatePresentation(project.Presentation, sets, assetIDs)
}

func validateTarget(target Target) error {
	if err := vnext.ValidSlug(target.World); err != nil {
		return fmt.Errorf("target world: %w", err)
	}
	space := target.CoordinateSpace
	if target.Title == "" || space.ID == "" || space.Kind == "" || space.Unit == "" || space.Definition == "" {
		return fmt.Errorf("target requires a world title and complete coordinate space")
	}
	if space.Extent[2] <= space.Extent[0] || space.Extent[3] <= space.Extent[1] {
		return fmt.Errorf("target coordinate extent is empty")
	}
	if space.SourceZoom < 0 || space.OriginX < 0 || space.OriginY < 0 || space.FirstTile < 0 || space.TileSize < 0 || space.Size < 0 {
		return fmt.Errorf("target coordinate grid metadata is invalid")
	}
	return nil
}

func validateFeatureSetContracts(values []FeatureSetContract) (map[string]FeatureSetContract, error) {
	sets := make(map[string]FeatureSetContract, len(values))
	for _, set := range values {
		if err := validateFeatureSetContract(set); err != nil {
			return nil, err
		}
		if _, exists := sets[set.ID]; exists {
			return nil, fmt.Errorf("feature set %s is configured twice", set.ID)
		}
		sets[set.ID] = set
	}
	for _, set := range values {
		for _, relation := range set.Relationships {
			if _, exists := sets[relation.FeatureSet]; !exists {
				return nil, fmt.Errorf("feature set %s relationship %s targets unknown feature set %s", set.ID, relation.ID, relation.FeatureSet)
			}
		}
	}
	return sets, nil
}

func validateFeatureSetContract(set FeatureSetContract) error {
	if err := vnext.ValidSlug(set.ID); err != nil {
		return fmt.Errorf("feature set identity: %w", err)
	}
	if set.Title == "" || set.SemanticType == "" {
		return fmt.Errorf("feature set %s requires a title and semantic type", set.ID)
	}
	if err := validateGeometryFamily(set.Geometry); err != nil {
		return fmt.Errorf("feature set %s: %w", set.ID, err)
	}
	properties := make(map[string]bool, len(set.Properties))
	for _, property := range set.Properties {
		if property.ID == "" || property.Name == "" || property.Type == "" {
			return fmt.Errorf("feature set %s has an incomplete property contract", set.ID)
		}
		if _, err := kindOf(property.Type); err != nil {
			return fmt.Errorf("feature set %s property %s: %w", set.ID, property.ID, err)
		}
		if properties[property.ID] {
			return fmt.Errorf("feature set %s declares property %s twice", set.ID, property.ID)
		}
		properties[property.ID] = true
	}
	return validateRelationshipContracts(set)
}

func validateRelationshipContracts(set FeatureSetContract) error {
	identities := make(map[string]bool, len(set.Relationships))
	semantics := make(map[string]bool, len(set.Relationships))
	for _, relation := range set.Relationships {
		if err := vnext.ValidSlug(relation.ID); err != nil {
			return fmt.Errorf("feature set %s relationship identity: %w", set.ID, err)
		}
		if relation.Predicate == "" || relation.FeatureSet == "" {
			return fmt.Errorf("feature set %s has an incomplete relationship contract", set.ID)
		}
		semantic := relation.Predicate + "\x00" + relation.FeatureSet
		if identities[relation.ID] || semantics[semantic] {
			return fmt.Errorf("feature set %s declares relationship %s twice", set.ID, relation.ID)
		}
		identities[relation.ID], semantics[semantic] = true, true
	}
	return nil
}

func validateGeometryFamily(family string) error {
	switch family {
	case "point", "path", "area":
		return nil
	default:
		return fmt.Errorf("unknown geometry family %q", family)
	}
}

func validateSource(source Source, target CoordinateSpace, sets map[string]FeatureSetContract) error {
	if err := vnext.ValidSlug(source.ID); err != nil {
		return fmt.Errorf("source identity: %w", err)
	}
	if source.Adapter == "" || source.Locator == "" {
		return fmt.Errorf("source %s requires an adapter and locator", source.ID)
	}
	if strings.TrimSpace(source.License) == "" || strings.TrimSpace(source.Attribution) == "" {
		return fmt.Errorf("source %s must state a license and attribution", source.ID)
	}
	if source.EstimateBytes < 0 {
		return fmt.Errorf("source %s has a negative size estimate", source.ID)
	}
	if source.Query.Layer < 0 || source.Query.Limit < 0 || source.Query.SpatialReference < 0 {
		return fmt.Errorf("source %s has invalid query settings", source.ID)
	}
	mapping := source.Mapping
	set, exists := sets[mapping.FeatureSet]
	if !exists {
		return fmt.Errorf("source %s targets unknown feature set %s", source.ID, mapping.FeatureSet)
	}
	if mapping.Identity == "" || mapping.FeatureTitle == "" {
		return fmt.Errorf("source %s has an incomplete mapping", source.ID)
	}
	if err := validateGeometryMap(mapping.Geometry, target); err != nil {
		return fmt.Errorf("source %s geometry: %w", source.ID, err)
	}
	properties := make(map[string]bool, len(mapping.Properties))
	for _, property := range mapping.Properties {
		if property.Field == "" || property.Source == "" {
			return fmt.Errorf("source %s has an incomplete property mapping", source.ID)
		}
		if _, exists := propertyContract(set, property.Field); !exists {
			return fmt.Errorf("source %s maps unknown property %s", source.ID, property.Field)
		}
		if properties[property.Field] {
			return fmt.Errorf("source %s maps property %s twice", source.ID, property.Field)
		}
		properties[property.Field] = true
	}
	relations := make(map[string]bool, len(mapping.Relations))
	for _, relation := range mapping.Relations {
		if relation.Relationship == "" || relation.Target == "" {
			return fmt.Errorf("source %s has an incomplete relationship mapping", source.ID)
		}
		if _, exists := relationshipContract(set, relation.Relationship); !exists {
			return fmt.Errorf("source %s maps unknown relationship %s", source.ID, relation.Relationship)
		}
		if relations[relation.Relationship] {
			return fmt.Errorf("source %s maps relationship %s twice", source.ID, relation.Relationship)
		}
		relations[relation.Relationship] = true
	}
	return nil
}

func validateGeometryMap(mapping GeometryMap, target CoordinateSpace) error {
	if strings.TrimSpace(mapping.SourceSpace) == "" {
		return fmt.Errorf("geometry has no source space")
	}
	switch mapping.Transform.Kind {
	case "identity":
		if mapping.SourceSpace != target.ID && mapping.SourceSpace != target.Definition {
			return fmt.Errorf("identity transform source space %q is not target %q", mapping.SourceSpace, target.ID)
		}
	case "affine":
		for _, value := range mapping.Transform.Matrix {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("affine transform has a non-finite coefficient")
			}
		}
		matrix := mapping.Transform.Matrix
		if matrix[0]*matrix[4]-matrix[1]*matrix[3] == 0 {
			return fmt.Errorf("affine transform is singular")
		}
	default:
		return fmt.Errorf("unknown transform %q", mapping.Transform.Kind)
	}
	return nil
}

func validateRaster(raster Raster) error {
	if err := vnext.ValidSlug(raster.ID); err != nil {
		return fmt.Errorf("raster identity: %w", err)
	}
	if raster.Name == "" || raster.Adapter == "" || raster.Locator == "" || raster.TileSize <= 0 || raster.EstimateBytes < 0 || raster.MaxZoom < 0 || raster.FullZoom < 0 || raster.Shard < 0 {
		return fmt.Errorf("raster %s is incomplete", raster.ID)
	}
	if strings.TrimSpace(raster.License) == "" || strings.TrimSpace(raster.Attribution) == "" {
		return fmt.Errorf("raster %s must state a license and attribution", raster.ID)
	}
	if raster.Bounds != nil && !validRasterRect(*raster.Bounds) {
		return fmt.Errorf("raster %s has invalid bounds", raster.ID)
	}
	if raster.Surface != nil && !validRasterRect(*raster.Surface) {
		return fmt.Errorf("raster %s has invalid surface", raster.ID)
	}
	switch raster.Adapter {
	case "raster-file":
		if len(raster.Levels) != 0 || raster.MaxZoom > 6 || (raster.FullZoom != 0 && raster.FullZoom > raster.MaxZoom) {
			return fmt.Errorf("raster-file %s must not declare xyz levels", raster.ID)
		}
	case "xyz":
		if len(raster.Levels) == 0 || !strings.Contains(raster.Locator, "{z}") || !strings.Contains(raster.Locator, "{x}") || !strings.Contains(raster.Locator, "{y}") {
			return fmt.Errorf("xyz raster %s requires a template and explicit levels", raster.ID)
		}
		if err := validateRasterLevels(raster); err != nil {
			return err
		}
	case "wmts":
		if len(raster.Levels) == 0 || !strings.Contains(raster.Locator, "{TileMatrix}") || !strings.Contains(raster.Locator, "{TileCol}") || !strings.Contains(raster.Locator, "{TileRow}") {
			return fmt.Errorf("wmts raster %s requires a REST template and explicit levels", raster.ID)
		}
		if err := validateRasterLevels(raster); err != nil {
			return err
		}
	default:
		return fmt.Errorf("raster %s uses unknown adapter %q", raster.ID, raster.Adapter)
	}
	return nil
}

func validateRasterLevels(raster Raster) error {
	previous := int64(-1)
	for _, level := range raster.Levels {
		if level.Zoom != previous+1 || level.Zoom < 0 || level.Zoom > 30 {
			return fmt.Errorf("%s raster %s has an invalid or unordered level", raster.Adapter, raster.ID)
		}
		maximum := int64(1)<<level.Zoom - 1
		if level.MinX < 0 || level.MinY < 0 || level.MaxX < level.MinX || level.MaxY < level.MinY || level.MaxX > maximum || level.MaxY > maximum {
			return fmt.Errorf("%s raster %s has an invalid or unordered level", raster.Adapter, raster.ID)
		}
		previous = level.Zoom
	}
	return nil
}

func validRasterRect(rect RasterRect) bool {
	return finite(rect.X) && finite(rect.Y) && finite(rect.Width) && finite(rect.Height) && rect.Width > 0 && rect.Height > 0
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateAsset(asset Asset) error {
	if err := vnext.ValidSlug(asset.ID); err != nil {
		return fmt.Errorf("asset identity: %w", err)
	}
	if asset.ID == "build-receipt" {
		return fmt.Errorf("asset build-receipt is reserved")
	}
	if asset.Locator == "" || asset.MediaType == "" || asset.EstimateBytes < 0 {
		return fmt.Errorf("asset %s is incomplete", asset.ID)
	}
	if strings.TrimSpace(asset.License) == "" || strings.TrimSpace(asset.Attribution) == "" {
		return fmt.Errorf("asset %s must state a license and attribution", asset.ID)
	}
	return nil
}

func validatePresentation(presentation Presentation, sets map[string]FeatureSetContract, assets map[string]bool) error {
	if presentation.ID == "" || presentation.Title == "" {
		return fmt.Errorf("presentation requires an ID and title")
	}
	styles := make(map[string]bool)
	for _, style := range presentation.Styles {
		if style.ID == "" || styles[style.ID] {
			return fmt.Errorf("presentation has an invalid or duplicate style")
		}
		if style.IconAsset != "" && !assets[style.IconAsset] {
			return fmt.Errorf("presentation style %s refers to unknown asset %s", style.ID, style.IconAsset)
		}
		styles[style.ID] = true
	}
	layers := make(map[string]bool)
	for _, layer := range presentation.Layers {
		_, knownSet := sets[layer.FeatureSet]
		if layer.ID == "" || layers[layer.ID] || !knownSet || !styles[layer.Style] || layer.Label == "" || layer.MaxZoom < layer.MinZoom {
			return fmt.Errorf("presentation layer %s is invalid", layer.ID)
		}
		layers[layer.ID] = true
	}
	return nil
}

func propertyContract(set FeatureSetContract, id string) (PropertyContract, bool) {
	for _, property := range set.Properties {
		if property.ID == id {
			return property, true
		}
	}
	return PropertyContract{}, false
}

func featureSetContract(sets []FeatureSetContract, id string) (FeatureSetContract, bool) {
	for _, set := range sets {
		if set.ID == id {
			return set, true
		}
	}
	return FeatureSetContract{}, false
}

func relationshipContract(set FeatureSetContract, id string) (RelationshipContract, bool) {
	for _, relation := range set.Relationships {
		if relation.ID == id {
			return relation, true
		}
	}
	return RelationshipContract{}, false
}

func kindOf(name string) (vnext.Kind, error) {
	switch name {
	case "bool":
		return vnext.KindBool, nil
	case "int64":
		return vnext.KindInt64, nil
	case "float64":
		return vnext.KindFloat64, nil
	case "string":
		return vnext.KindString, nil
	case "bytes":
		return vnext.KindBytes, nil
	case "id":
		return vnext.KindID, nil
	default:
		return vnext.KindInvalid, fmt.Errorf("unknown type %q", name)
	}
}

func isRemote(locator string) bool {
	return strings.HasPrefix(locator, "https://") || strings.HasPrefix(locator, "http://")
}
