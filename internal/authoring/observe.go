package authoring

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

type observation struct {
	SourceOrder int
	Source      string
	NativeID    string
	FeatureSet  string
	Title       string
	Geometry    vnext.Geometry
	Values      map[string]vnext.Value
	Relations   []observedRelation
	Evidence    Capture
}

type observedRelation struct {
	Contract   string
	Predicate  string
	FeatureSet string
	NativeID   string
}

type geoJSON struct {
	Type                  string           `json:"type"`
	Features              []geoJSONFeature `json:"features"`
	Links                 []geoJSONLink    `json:"links,omitempty"`
	NumberMatched         *int64           `json:"numberMatched,omitempty"`
	NumberReturned        *int64           `json:"numberReturned,omitempty"`
	ExceededTransferLimit bool             `json:"exceededTransferLimit,omitempty"`
}

type geoJSONLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

type geoJSONFeature struct {
	Type       string          `json:"type"`
	ID         any             `json:"id"`
	Properties map[string]any  `json:"properties"`
	Geometry   geoJSONGeometry `json:"geometry"`
}

type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func observe(project Project, sourceOrder int, source Source, capture Capture, body []byte) ([]observation, error) {
	set, exists := featureSetContract(project.FeatureSets, source.Mapping.FeatureSet)
	if !exists {
		return nil, fmt.Errorf("source %s targets unknown feature set %s", source.ID, source.Mapping.FeatureSet)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var collection geoJSON
	if err := decoder.Decode(&collection); err != nil {
		return nil, fmt.Errorf("source %s decode GeoJSON: %w", source.ID, err)
	}
	if collection.Type != "FeatureCollection" {
		return nil, fmt.Errorf("source %s is %q, want FeatureCollection", source.ID, collection.Type)
	}
	if err := validateAdapterCompleteness(source, collection); err != nil {
		return nil, err
	}
	out := make([]observation, 0, len(collection.Features))
	for index, feature := range collection.Features {
		if feature.Type != "" && feature.Type != "Feature" {
			return nil, fmt.Errorf("source %s feature %d has type %q", source.ID, index, feature.Type)
		}
		nativeValue, held := featureValue(feature, source.Mapping.Identity)
		if !held {
			return nil, fmt.Errorf("source %s feature %d has no identity at %s", source.ID, index, source.Mapping.Identity)
		}
		nativeID := scalarString(nativeValue)
		if nativeID == "" {
			return nil, fmt.Errorf("source %s feature %d has an empty identity", source.ID, index)
		}
		titleValue, held := featureValue(feature, source.Mapping.FeatureTitle)
		if !held || scalarString(titleValue) == "" {
			return nil, fmt.Errorf("source %s feature %s has no title at %s", source.ID, nativeID, source.Mapping.FeatureTitle)
		}
		geometry, err := geometryOf(feature.Geometry)
		if err != nil {
			return nil, fmt.Errorf("source %s feature %s: %w", source.ID, nativeID, err)
		}
		geometryMap := source.Mapping.Geometry
		geometryMap.Family = set.Geometry
		geometry, err = transformGeometry(geometry, geometryMap, project.Target.CoordinateSpace)
		if err != nil {
			return nil, fmt.Errorf("source %s feature %s: %w", source.ID, nativeID, err)
		}
		item := observation{
			SourceOrder: sourceOrder, Source: source.ID, NativeID: nativeID,
			FeatureSet: source.Mapping.FeatureSet, Title: scalarString(titleValue),
			Geometry: geometry, Values: make(map[string]vnext.Value),
			Evidence: capture,
		}
		for _, mapping := range source.Mapping.Properties {
			raw, held := featureValue(feature, mapping.Source)
			if !held || raw == nil {
				continue
			}
			contract, exists := propertyContract(set, mapping.Field)
			if !exists {
				return nil, fmt.Errorf("source %s maps unknown property %s", source.ID, mapping.Field)
			}
			kind, _ := kindOf(contract.Type)
			value, err := valueOf(project.SchemaNamespace, kind, raw)
			if err != nil {
				return nil, fmt.Errorf("source %s feature %s property %s: %w", source.ID, nativeID, mapping.Field, err)
			}
			item.Values[mapping.Field] = value
		}
		for _, mapping := range source.Mapping.Relations {
			raw, held := featureValue(feature, mapping.Target)
			if !held || scalarString(raw) == "" {
				continue
			}
			contract, exists := relationshipContract(set, mapping.Relationship)
			if !exists {
				return nil, fmt.Errorf("source %s maps unknown relationship %s", source.ID, mapping.Relationship)
			}
			item.Relations = append(item.Relations, observedRelation{
				Contract: mapping.Relationship, Predicate: contract.Predicate,
				FeatureSet: contract.FeatureSet, NativeID: scalarString(raw),
			})
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NativeID < out[j].NativeID })
	return out, nil
}

func validateAdapterCompleteness(source Source, collection geoJSON) error {
	switch source.Adapter {
	case "arcgis-feature-service":
		if collection.ExceededTransferLimit {
			return fmt.Errorf("source %s ArcGIS response exceeded its transfer limit; narrow or partition the configured query", source.ID)
		}
	case "ogc-api-features":
		if collection.NumberReturned != nil && *collection.NumberReturned != int64(len(collection.Features)) {
			return fmt.Errorf("source %s OGC response count differs from its feature payload", source.ID)
		}
		for _, link := range collection.Links {
			if strings.EqualFold(link.Rel, "next") && strings.TrimSpace(link.Href) != "" {
				return fmt.Errorf("source %s OGC response has a next page; narrow or partition the configured query", source.ID)
			}
		}
	}
	return nil
}

func featureValue(feature geoJSONFeature, path string) (any, bool) {
	switch path {
	case "id":
		return feature.ID, feature.ID != nil
	}
	const prefix = "properties."
	if !strings.HasPrefix(path, prefix) {
		return nil, false
	}
	value, held := feature.Properties[strings.TrimPrefix(path, prefix)]
	return value, held
}

func scalarString(value any) string {
	switch held := value.(type) {
	case string:
		return strings.TrimSpace(held)
	case json.Number:
		return held.String()
	case float64:
		return strconv.FormatFloat(held, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(held)
	default:
		return ""
	}
}

func valueOf(namespace string, kind vnext.Kind, raw any) (vnext.Value, error) {
	switch kind {
	case vnext.KindString:
		value := scalarString(raw)
		if value == "" {
			return vnext.Value{}, fmt.Errorf("value is not a scalar string")
		}
		return vnext.StringValue(value), nil
	case vnext.KindBool:
		if value, ok := raw.(bool); ok {
			return vnext.BoolValue(value), nil
		}
		value, err := strconv.ParseBool(scalarString(raw))
		if err != nil {
			return vnext.Value{}, err
		}
		return vnext.BoolValue(value), nil
	case vnext.KindInt64:
		value, err := strconv.ParseInt(scalarString(raw), 10, 64)
		if err != nil {
			return vnext.Value{}, err
		}
		return vnext.Int64Value(value), nil
	case vnext.KindFloat64:
		value, err := strconv.ParseFloat(scalarString(raw), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return vnext.Value{}, fmt.Errorf("value is not a finite float64")
		}
		return vnext.Float64Value(value), nil
	case vnext.KindBytes:
		value, err := base64.StdEncoding.DecodeString(scalarString(raw))
		if err != nil {
			return vnext.Value{}, err
		}
		return vnext.BytesValue(value), nil
	case vnext.KindID:
		value := scalarString(raw)
		if value == "" {
			return vnext.Value{}, fmt.Errorf("value is not an identity")
		}
		return vnext.IDValue(vnext.IDFromName(namespace, value)), nil
	default:
		return vnext.Value{}, fmt.Errorf("unsupported value type")
	}
}

func geometryOf(source geoJSONGeometry) (vnext.Geometry, error) {
	switch source.Type {
	case "Point":
		var point vnext.Position
		if err := json.Unmarshal(source.Coordinates, &point); err != nil {
			return vnext.Geometry{}, err
		}
		return vnext.Geometry{Kind: vnext.GeometryPoint, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{{point}}}}}, nil
	case "LineString":
		var line []vnext.Position
		if err := json.Unmarshal(source.Coordinates, &line); err != nil {
			return vnext.Geometry{}, err
		}
		return vnext.Geometry{Kind: vnext.GeometryLineString, Parts: []vnext.GeometryPart{{Rings: [][]vnext.Position{line}}}}, nil
	case "MultiLineString":
		var lines [][]vnext.Position
		if err := json.Unmarshal(source.Coordinates, &lines); err != nil {
			return vnext.Geometry{}, err
		}
		geometry := vnext.Geometry{Kind: vnext.GeometryLineString}
		for _, line := range lines {
			geometry.Parts = append(geometry.Parts, vnext.GeometryPart{Rings: [][]vnext.Position{line}})
		}
		return geometry, nil
	case "Polygon":
		var rings [][]vnext.Position
		if err := json.Unmarshal(source.Coordinates, &rings); err != nil {
			return vnext.Geometry{}, err
		}
		return vnext.Geometry{Kind: vnext.GeometryPolygon, Parts: []vnext.GeometryPart{{Rings: rings}}}, nil
	case "MultiPolygon":
		var polygons [][][]vnext.Position
		if err := json.Unmarshal(source.Coordinates, &polygons); err != nil {
			return vnext.Geometry{}, err
		}
		geometry := vnext.Geometry{Kind: vnext.GeometryPolygon}
		for _, rings := range polygons {
			geometry.Parts = append(geometry.Parts, vnext.GeometryPart{Rings: rings})
		}
		return geometry, nil
	default:
		return vnext.Geometry{}, fmt.Errorf("unsupported GeoJSON geometry %q", source.Type)
	}
}
