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
	"os"
	"path/filepath"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
	"gopkg.in/yaml.v3"
)

const (
	ProjectSchema    = "atlas-project/v1"
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
	Fields       []Field       `yaml:"fields,omitempty"`
	Relations    []RelationMap `yaml:"relationships,omitempty"`
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
}

// Raster is a configured raster adapter. A file is one tile at zoom zero; an
// xyz source names explicit tile windows so planning never probes the network.
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
	Interpolate   bool          `yaml:"interpolate,omitempty"`
	Background    string        `yaml:"background,omitempty"`
	Levels        []RasterLevel `yaml:"levels,omitempty"`
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

type Style struct {
	ID       string `yaml:"id"`
	Symbol   string `yaml:"symbol,omitempty"`
	Icon     string `yaml:"icon,omitempty"`
	RenderAs string `yaml:"render-as,omitempty"`
	Stroke   string `yaml:"stroke,omitempty"`
	Fill     string `yaml:"fill,omitempty"`
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
		if err := validateSource(source); err != nil {
			return err
		}
		if sourceIDs[source.ID] {
			return fmt.Errorf("source %s is configured twice", source.ID)
		}
		sourceIDs[source.ID] = true
		if held, ok := sets[source.Mapping.FeatureSet]; ok {
			if held.Title != source.Mapping.Title || held.SemanticType != source.Mapping.SemanticType {
				return fmt.Errorf("feature set %s has conflicting declarations", source.Mapping.FeatureSet)
			}
		} else {
			sets[source.Mapping.FeatureSet] = source.Mapping
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
	return validatePresentation(project.Presentation, sets)
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

func validateSource(source Source) error {
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
	fields := make(map[string]string)
	for _, field := range mapping.Fields {
		if field.ID == "" || field.Source == "" || field.Type == "" {
			return fmt.Errorf("source %s has an incomplete field mapping", source.ID)
		}
		if _, err := kindOf(field.Type); err != nil {
			return fmt.Errorf("source %s field %s: %w", source.ID, field.ID, err)
		}
		if held := fields[field.ID]; held != "" && held != field.Type {
			return fmt.Errorf("source %s field %s has conflicting types", source.ID, field.ID)
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

func validateRaster(raster Raster) error {
	if err := vnext.ValidSlug(raster.ID); err != nil {
		return fmt.Errorf("raster identity: %w", err)
	}
	if raster.Name == "" || raster.Adapter == "" || raster.Locator == "" || raster.TileSize <= 0 || raster.EstimateBytes < 0 {
		return fmt.Errorf("raster %s is incomplete", raster.ID)
	}
	switch raster.Adapter {
	case "raster-file":
		if len(raster.Levels) != 0 {
			return fmt.Errorf("raster-file %s must not declare xyz levels", raster.ID)
		}
	case "xyz":
		if len(raster.Levels) == 0 || !strings.Contains(raster.Locator, "{z}") || !strings.Contains(raster.Locator, "{x}") || !strings.Contains(raster.Locator, "{y}") {
			return fmt.Errorf("xyz raster %s requires a template and explicit levels", raster.ID)
		}
		previous := int64(-1)
		for _, level := range raster.Levels {
			if level.Zoom <= previous || level.MinX < 0 || level.MinY < 0 || level.MaxX < level.MinX || level.MaxY < level.MinY {
				return fmt.Errorf("xyz raster %s has an invalid or unordered level", raster.ID)
			}
			previous = level.Zoom
		}
	default:
		return fmt.Errorf("raster %s uses unknown adapter %q", raster.ID, raster.Adapter)
	}
	return nil
}

func validatePresentation(presentation Presentation, sets map[string]Mapping) error {
	if presentation.ID == "" || presentation.Title == "" {
		return fmt.Errorf("presentation requires an ID and title")
	}
	styles := make(map[string]bool)
	for _, style := range presentation.Styles {
		if style.ID == "" || styles[style.ID] {
			return fmt.Errorf("presentation has an invalid or duplicate style")
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
