package workbench

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/semconv"
	"github.com/FelineStateMachine/atlas/format/vnext"
	"github.com/FelineStateMachine/atlas/internal/authoring"
)

// artifactPage is a semantic projection of a validated native Atlas file. It
// is intentionally built from vnext.Reader.Volume rather than the authoring
// manifest, so this page describes what was actually packed and reopened.
type artifactPage struct {
	File         string
	Descriptor   vnext.Descriptor
	TableCount   int
	BlobCount    int
	RuntimeTypes int
	Tables       []artifactTable
	BlobNames    []string
	Schema       []artifactSchemaType
	Worlds       []artifactWorld
	Assets       []artifactAsset
	Receipt      *artifactReceipt
	ReceiptErr   string
}

type artifactTable struct {
	Name    string
	Rows    int
	Columns int
}

type artifactSchemaType struct {
	ID     string
	Name   string
	Fields []artifactField
}

type artifactWorld struct {
	ID              string
	Title           string
	CoordinateSpace vnext.CoordinateSpace
	Claims          []artifactProperty
	FeatureSets     []artifactFeatureSet
	Rasters         []artifactRaster
	Presentation    artifactPresentation
	Features        int
}

type artifactFeatureSet struct {
	ID           string
	Title        string
	SemanticType string
	Geometry     string
	Properties   []artifactField
	Claims       []artifactProperty
	Features     []artifactFeature
	FeatureCount int
	Offset       int
	Previous     int
	Next         int
	HasPrevious  bool
	HasNext      bool
	Selected     *artifactFeature
}

type artifactField struct {
	ID       string
	Name     string
	Kind     string
	Optional bool
}

type artifactFeature struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	Geometry      artifactGeometry
	Properties    []artifactProperty
	Relationships []artifactRelationship
	Provenance    []vnext.Provenance
}

type artifactGeometry struct {
	Kind     string
	Parts    int
	Rings    int
	Vertices int
	Bounds   [4]float64
	HasData  bool
}

type artifactProperty struct {
	ID    string
	Name  string
	Kind  string
	Value string
}

type artifactRelationship struct {
	Predicate string
	Target    string
	Title     string
}

type artifactRaster struct {
	ID          string
	Name        string
	Codec       string
	TileSize    int64
	MinZoom     int64
	MaxZoom     int64
	FullZoom    int64
	Formats     []string
	Coverage    int
	TileBlobs   int
	Template    string
	Interpolate bool
	Background  string
}

type artifactPresentation struct {
	ID     string
	Title  string
	Layers []vnext.Layer
	Styles []vnext.Style
	Legend []vnext.LegendEntry
}

type artifactAsset struct {
	ID         string
	MediaType  string
	Bytes      int
	Provenance string
}

type artifactReceipt struct {
	Project       string
	ProjectDigest string
	Captures      []authoring.SelectedCapture
}

func (w *Workbench) handleProjectInspect(rw http.ResponseWriter, request *http.Request) {
	artifact, err := w.openCompletedArtifact()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	defer artifact.Close()
	offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	page, err := inspectArtifact(artifact, request.URL.Query().Get("set"), request.URL.Query().Get("feature"), offset)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	w.render(rw, "inspector", page)
}

func inspectArtifact(artifact *completedArtifact, selectedSet, selectedFeature string, selectedOffset int) (artifactPage, error) {
	volume, err := artifact.reader.Volume()
	if err != nil {
		return artifactPage{}, fmt.Errorf("restore native artifact: %w", err)
	}
	page := artifactPage{
		File: artifact.name, Descriptor: artifact.descriptor,
		TableCount: len(artifact.reader.TableNames()), BlobCount: len(artifact.reader.BlobNames()),
		RuntimeTypes: len(artifact.reader.RuntimeSchema.Types),
	}
	for _, name := range artifact.reader.TableNames() {
		table, err := artifact.reader.Table(name)
		if err != nil {
			return artifactPage{}, fmt.Errorf("inspect table %s: %w", name, err)
		}
		page.Tables = append(page.Tables, artifactTable{Name: name, Rows: table.Rows, Columns: len(table.Columns)})
	}
	for index, name := range artifact.reader.BlobNames() {
		if index == 50 {
			break
		}
		page.BlobNames = append(page.BlobNames, name)
	}
	for _, typ := range artifact.reader.FileSchema.Types {
		held := artifactSchemaType{ID: typ.ID.String(), Name: typ.Name}
		for _, field := range typ.Fields {
			held.Fields = append(held.Fields, artifactField{
				ID: field.ID.String(), Name: field.Name, Kind: kindName(field.Kind), Optional: field.Optional,
			})
		}
		page.Schema = append(page.Schema, held)
	}
	titles := featureTitles(volume)
	for _, world := range volume.Worlds {
		view := artifactWorld{
			ID: world.ID, Title: world.Title, CoordinateSpace: world.CoordinateSpace,
			Claims: inspectProperties(world.Claims),
			Presentation: artifactPresentation{
				ID: world.Presentation.ID, Title: world.Presentation.Title,
				Layers: world.Presentation.Layers, Styles: world.Presentation.Styles, Legend: world.Presentation.Legend,
			},
		}
		for _, set := range world.FeatureSets {
			contract := artifactFeatureSet{
				ID: set.ID, Title: set.Title, SemanticType: set.SemanticType,
				Geometry: geometryContract(set), Claims: inspectProperties(set.Claims), FeatureCount: len(set.Features),
			}
			for _, field := range set.Properties {
				contract.Properties = append(contract.Properties, artifactField{
					ID: field.ID.String(), Name: field.Name, Kind: kindName(field.Kind), Optional: field.Optional,
				})
			}
			offset := 0
			if set.ID == selectedSet {
				offset = selectedOffset
			}
			if offset >= len(set.Features) && len(set.Features) > 0 {
				offset = (len(set.Features) - 1) / 25 * 25
			}
			contract.Offset = offset
			contract.HasPrevious = offset > 0
			contract.Previous = max(0, offset-25)
			contract.HasNext = offset+25 < len(set.Features)
			contract.Next = offset + 25
			for index, feature := range set.Features {
				item := inspectFeature(feature, titles)
				if set.ID == selectedSet && feature.ID == selectedFeature {
					selected := item
					contract.Selected = &selected
				}
				if index >= offset && index < offset+25 {
					contract.Features = append(contract.Features, item)
				}
			}
			view.Features += len(set.Features)
			view.FeatureSets = append(view.FeatureSets, contract)
		}
		for _, raster := range world.RasterPyramids {
			view.Rasters = append(view.Rasters, artifactRaster{
				ID: raster.ID, Name: raster.Name, Codec: raster.Codec, TileSize: raster.TileSize,
				MinZoom: raster.MinZoom, MaxZoom: raster.MaxZoom, FullZoom: raster.FullZoom,
				Formats: raster.Formats, Coverage: len(raster.Coverage), TileBlobs: countBlobPrefix(artifact.reader.BlobNames(), raster.Template),
				Template: raster.Template, Interpolate: raster.Interpolate, Background: raster.Background,
			})
		}
		page.Worlds = append(page.Worlds, view)
	}
	for _, asset := range volume.Assets {
		page.Assets = append(page.Assets, artifactAsset{
			ID: asset.ID, MediaType: asset.MediaType, Bytes: len(asset.Data), Provenance: asset.Provenance,
		})
		if asset.ID == "build-receipt" {
			selection, err := inspectEvidenceSelection(asset.Data)
			if err != nil {
				page.ReceiptErr = err.Error()
			} else {
				page.Receipt = &artifactReceipt{
					Project: selection.Project, ProjectDigest: selection.ProjectDigest, Captures: selection.Captures,
				}
			}
		}
	}
	return page, nil
}

func countBlobPrefix(names []string, template string) int {
	prefix := template
	if marker := strings.IndexByte(prefix, '{'); marker >= 0 {
		prefix = prefix[:marker]
	}
	if prefix == "" {
		return 0
	}
	count := 0
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count
}

func inspectEvidenceSelection(data []byte) (authoring.EvidenceSelection, error) {
	selection, err := authoring.ParseEvidenceSelection(data)
	if err != nil {
		return selection, fmt.Errorf("receipt metadata is invalid: %w", err)
	}
	return selection, nil
}

func isLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func inspectFeature(feature vnext.Feature, titles map[string]string) artifactFeature {
	item := artifactFeature{
		ID: feature.ID, Title: feature.Title, Subtitle: feature.Subtitle, Description: feature.Description,
		Geometry: inspectGeometry(feature.Geometry), Properties: inspectProperties(feature.Properties),
		Provenance: feature.Provenance,
	}
	for _, relationship := range feature.Relationships {
		item.Relationships = append(item.Relationships, artifactRelationship{
			Predicate: relationship.Predicate, Target: relationship.Target, Title: titles[relationship.Target],
		})
	}
	return item
}

func featureTitles(volume vnext.Volume) map[string]string {
	titles := make(map[string]string)
	for _, world := range volume.Worlds {
		for _, set := range world.FeatureSets {
			for _, feature := range set.Features {
				titles[feature.ID] = feature.Title
			}
		}
	}
	return titles
}

func geometryContract(set vnext.FeatureSet) string {
	for _, claim := range set.Claims {
		if claim.Field.Name == semconv.KeyGeometryKind && claim.Value.Kind == vnext.KindString {
			return claim.Value.String
		}
	}
	if len(set.Features) > 0 {
		return geometryKindName(set.Features[0].Geometry.Kind)
	}
	return "undeclared"
}

func inspectGeometry(geometry vnext.Geometry) artifactGeometry {
	view := artifactGeometry{Kind: geometryKindName(geometry.Kind), Parts: len(geometry.Parts)}
	for _, part := range geometry.Parts {
		view.Rings += len(part.Rings)
		for _, ring := range part.Rings {
			for _, position := range ring {
				view.Vertices++
				if !view.HasData {
					view.Bounds = [4]float64{position[0], position[1], position[0], position[1]}
					view.HasData = true
					continue
				}
				view.Bounds[0] = min(view.Bounds[0], position[0])
				view.Bounds[1] = min(view.Bounds[1], position[1])
				view.Bounds[2] = max(view.Bounds[2], position[0])
				view.Bounds[3] = max(view.Bounds[3], position[1])
			}
		}
	}
	return view
}

func inspectProperties(values []vnext.Property) []artifactProperty {
	out := make([]artifactProperty, 0, len(values))
	for _, property := range values {
		out = append(out, artifactProperty{
			ID: property.FieldID.String(), Name: property.Field.Name,
			Kind: kindName(property.Value.Kind), Value: valueString(property.Value),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func geometryKindName(kind vnext.GeometryKind) string {
	switch kind {
	case vnext.GeometryPoint:
		return "point"
	case vnext.GeometryLineString:
		return "path"
	case vnext.GeometryPolygon:
		return "area"
	default:
		return "unknown"
	}
}

func kindName(kind vnext.Kind) string {
	switch kind {
	case vnext.KindBool:
		return "bool"
	case vnext.KindInt64:
		return "int64"
	case vnext.KindFloat64:
		return "float64"
	case vnext.KindString:
		return "string"
	case vnext.KindBytes:
		return "bytes"
	case vnext.KindID:
		return "id"
	default:
		return "null"
	}
}

func valueString(value vnext.Value) string {
	switch value.Kind {
	case vnext.KindBool:
		return fmt.Sprintf("%t", value.Bool)
	case vnext.KindInt64:
		return fmt.Sprintf("%d", value.Int64)
	case vnext.KindFloat64:
		return fmt.Sprintf("%g", value.Float64)
	case vnext.KindString:
		return value.String
	case vnext.KindBytes:
		digest := sha256.Sum256(value.Bytes)
		prefix := value.Bytes
		truncated := ""
		if len(prefix) > 16 {
			prefix = prefix[:16]
			truncated = "…"
		}
		return fmt.Sprintf("%d bytes · sha256 %x · %s%s", len(value.Bytes), digest[:6], base64.StdEncoding.EncodeToString(prefix), truncated)
	case vnext.KindID:
		return value.ID.String()
	default:
		return ""
	}
}
