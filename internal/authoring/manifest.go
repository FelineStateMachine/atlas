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
	Schema          string       `yaml:"schema"`
	SchemaNamespace string       `yaml:"schema-namespace"`
	ID              string       `yaml:"id"`
	Title           string       `yaml:"title"`
	Target          Target       `yaml:"target"`
	Sources         []Source     `yaml:"sources,omitempty"`
	Rasters         []Raster     `yaml:"rasters,omitempty"`
	Assets          []Asset      `yaml:"assets,omitempty"`
	Presentation    Presentation `yaml:"presentation"`
	Release         Release      `yaml:"release,omitempty"`

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
	FirstTile  int64      `yaml:"first-tile,omitempty"`
	TileSize   int64      `yaml:"tile-size,omitempty"`
	Size       int64      `yaml:"size,omitempty"`
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

// Query carries protocol-neutral selection knobs interpreted only by the
// selected adapter.
type Query struct {
	Layer      int    `yaml:"layer,omitempty"`
	Collection string `yaml:"collection,omitempty"`
	Where      string `yaml:"where,omitempty"`
	Limit      int    `yaml:"limit,omitempty"`
}

// Mapping turns source observations into one semantic feature set. Sources
// that name the same feature-set and canonical identity meet during assembly.
type Mapping struct {
	FeatureSet   string        `yaml:"feature-set"`
	Title        string        `yaml:"title"`
	SemanticType string        `yaml:"semantic-type"`
	Identity     string        `yaml:"identity"`
	FeatureTitle string        `yaml:"feature-title"`
	Geometry     GeometryMap   `yaml:"geometry"`
	Fields       []Field       `yaml:"fields,omitempty"`
	Relations    []RelationMap `yaml:"relationships,omitempty"`
}

// GeometryMap is the source-to-world geometry contract for one FeatureSet.
// Atlas intentionally executes only explicit identity and affine transforms;
// a CRS label alone never implies executable projection behavior.
type GeometryMap struct {
	Family      string              `yaml:"family"`
	SourceSpace string              `yaml:"source-space"`
	Transform   CoordinateTransform `yaml:"transform"`
}

type CoordinateTransform struct {
	Kind   string     `yaml:"kind"`
	Matrix [6]float64 `yaml:"matrix,omitempty"`
}

type Field struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name,omitempty"`
	Source   string `yaml:"source"`
	Type     string `yaml:"type"`
	Optional bool   `yaml:"optional,omitempty"`
}

type RelationMap struct {
	Predicate  string `yaml:"predicate"`
	FeatureSet string `yaml:"feature-set,omitempty"`
	Target     string `yaml:"target"`
	Optional   bool   `yaml:"optional,omitempty"`
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
	if len(project.Sources) == 0 && len(project.Rasters) == 0 {
		return fmt.Errorf("project %s configures no sources or rasters", project.ID)
	}
	sets := make(map[string]Mapping)
	sourceIDs := make(map[string]bool)
	for _, source := range project.Sources {
		if err := validateSource(source, project.Target.CoordinateSpace); err != nil {
			return err
		}
		if sourceIDs[source.ID] {
			return fmt.Errorf("source %s is configured twice", source.ID)
		}
		sourceIDs[source.ID] = true
		if held, ok := sets[source.Mapping.FeatureSet]; ok {
			if err := compatibleMapping(held, source.Mapping); err != nil {
				return fmt.Errorf("feature set %s has conflicting declarations", source.Mapping.FeatureSet)
			}
		} else {
			sets[source.Mapping.FeatureSet] = source.Mapping
		}
	}
	for _, source := range project.Sources {
		for _, relation := range source.Mapping.Relations {
			target := relation.FeatureSet
			if target == "" {
				target = source.Mapping.FeatureSet
			}
			if _, ok := sets[target]; !ok {
				return fmt.Errorf("source %s relationship %s targets unknown feature set %s", source.ID, relation.Predicate, target)
			}
		}
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
	if space.SourceZoom < 0 || space.FirstTile < 0 || space.TileSize < 0 || space.Size < 0 {
		return fmt.Errorf("target coordinate grid metadata is invalid")
	}
	return nil
}

func validateSource(source Source, target CoordinateSpace) error {
	if err := vnext.ValidSlug(source.ID); err != nil {
		return fmt.Errorf("source identity: %w", err)
	}
	if source.Adapter == "" || source.Locator == "" {
		return fmt.Errorf("source %s requires an adapter and locator", source.ID)
	}
	if source.EstimateBytes < 0 {
		return fmt.Errorf("source %s has a negative size estimate", source.ID)
	}
	mapping := source.Mapping
	if err := vnext.ValidSlug(mapping.FeatureSet); err != nil {
		return fmt.Errorf("source %s feature set: %w", source.ID, err)
	}
	if mapping.Title == "" || mapping.SemanticType == "" || mapping.Identity == "" || mapping.FeatureTitle == "" {
		return fmt.Errorf("source %s has an incomplete mapping", source.ID)
	}
	if err := validateGeometryMap(mapping.Geometry, target); err != nil {
		return fmt.Errorf("source %s geometry: %w", source.ID, err)
	}
	fields := make(map[string]string)
	for _, field := range mapping.Fields {
		if field.ID == "" || field.Source == "" || field.Type == "" {
			return fmt.Errorf("source %s has an incomplete field mapping", source.ID)
		}
		if _, err := kindOf(field.Type); err != nil {
			return fmt.Errorf("source %s field %s: %w", source.ID, field.ID, err)
		}
		if held := fields[field.ID]; held != "" {
			return fmt.Errorf("source %s maps field %s twice", source.ID, field.ID)
		}
		fields[field.ID] = field.Type
	}
	for _, relation := range mapping.Relations {
		if relation.Predicate == "" || relation.Target == "" {
			return fmt.Errorf("source %s has an incomplete relationship mapping", source.ID)
		}
	}
	return nil
}

func validateGeometryMap(mapping GeometryMap, target CoordinateSpace) error {
	switch mapping.Family {
	case "point", "path", "area":
	default:
		return fmt.Errorf("unknown geometry family %q", mapping.Family)
	}
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

func compatibleMapping(left, right Mapping) error {
	if left.Title != right.Title || left.SemanticType != right.SemanticType || left.Geometry.Family != right.Geometry.Family {
		return fmt.Errorf("feature set metadata differs")
	}
	fields := make(map[string]Field, len(left.Fields))
	for _, field := range left.Fields {
		fields[field.ID] = field
	}
	for _, field := range right.Fields {
		if held, ok := fields[field.ID]; ok && (held.Name != field.Name || held.Type != field.Type || held.Optional != field.Optional) {
			return fmt.Errorf("field %s contract differs", field.ID)
		}
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
		previous := int64(-1)
		for _, level := range raster.Levels {
			if level.Zoom != previous+1 || level.Zoom < 0 || level.Zoom > 30 {
				return fmt.Errorf("xyz raster %s has an invalid or unordered level", raster.ID)
			}
			maximum := int64(1)<<level.Zoom - 1
			if level.MinX < 0 || level.MinY < 0 || level.MaxX < level.MinX || level.MaxY < level.MinY || level.MaxX > maximum || level.MaxY > maximum {
				return fmt.Errorf("xyz raster %s has an invalid or unordered level", raster.ID)
			}
			previous = level.Zoom
		}
	default:
		return fmt.Errorf("raster %s uses unknown adapter %q", raster.ID, raster.Adapter)
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
	return nil
}

func validatePresentation(presentation Presentation, sets map[string]Mapping, assets map[string]bool) error {
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
		if layer.ID == "" || layers[layer.ID] || sets[layer.FeatureSet].FeatureSet == "" || !styles[layer.Style] || layer.Label == "" || layer.MaxZoom < layer.MinZoom {
			return fmt.Errorf("presentation layer %s is invalid", layer.ID)
		}
		layers[layer.ID] = true
	}
	return nil
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
