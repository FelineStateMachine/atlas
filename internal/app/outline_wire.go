package app

import (
	"encoding/base64"
	"strconv"

	"github.com/FelineStateMachine/atlas/format/vnext"
)

const outlineWireVersion = 1

type outlineWire struct {
	Version int         `json:"version"`
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Worlds  []worldWire `json:"worlds"`
	Assets  []assetWire `json:"assets"`
}

type worldWire struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	CoordinateSpace coordinateSpaceWire `json:"coordinateSpace"`
	Claims          []propertyWire      `json:"claims"`
	FeatureSets     []featureSetWire    `json:"featureSets"`
	RasterPyramids  []rasterWire        `json:"rasterPyramids"`
	Presentation    presentationWire    `json:"presentation"`
}

type coordinateSpaceWire struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Unit       string     `json:"unit"`
	Definition string     `json:"definition"`
	Extent     [4]float64 `json:"extent"`
	SourceZoom string     `json:"sourceZoom"`
	OriginX    string     `json:"originX"`
	OriginY    string     `json:"originY"`
	TileSize   string     `json:"tileSize"`
	Size       string     `json:"size"`
}

type featureSetWire struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	SemanticType string         `json:"semanticType"`
	Properties   []fieldWire    `json:"properties"`
	Claims       []propertyWire `json:"claims"`
}

type fieldWire struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     vnext.Kind `json:"kind"`
	Optional bool       `json:"optional"`
}

type rasterWire struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Codec       string          `json:"codec"`
	TileSize    string          `json:"tileSize"`
	MinZoom     string          `json:"minZoom"`
	MaxZoom     string          `json:"maxZoom"`
	FullZoom    string          `json:"fullZoom"`
	SourceZoom  string          `json:"sourceZoom"`
	Template    string          `json:"template"`
	Formats     []string        `json:"formats"`
	Bounds      *rasterRectWire `json:"bounds,omitempty"`
	Surface     *rasterRectWire `json:"surface,omitempty"`
	Interpolate bool            `json:"interpolate"`
	Background  string          `json:"background,omitempty"`
	Shard       string          `json:"shard"`
	Coverage    []coverageWire  `json:"coverage"`
}

type rasterRectWire struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type coverageWire struct {
	Zoom string `json:"zoom"`
	X    string `json:"x"`
	Y    string `json:"y"`
	W    string `json:"w"`
	H    string `json:"h"`
	Bits string `json:"bits"`
}

type presentationWire struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Styles []styleWire  `json:"styles"`
	Layers []layerWire  `json:"layers"`
	Legend []legendWire `json:"legend"`
}

type styleWire struct {
	ID          string `json:"id"`
	Symbol      string `json:"symbol,omitempty"`
	Icon        string `json:"icon,omitempty"`
	IconAsset   string `json:"iconAsset,omitempty"`
	IconPicture bool   `json:"iconPicture"`
	RenderAs    string `json:"renderAs,omitempty"`
	Stroke      string `json:"stroke,omitempty"`
	Fill        string `json:"fill,omitempty"`
}

type layerWire struct {
	ID          string `json:"id"`
	FeatureSet  string `json:"featureSet"`
	Style       string `json:"style"`
	Group       string `json:"group,omitempty"`
	LabelPolicy string `json:"labelPolicy,omitempty"`
	Visible     bool   `json:"visible"`
	Order       string `json:"order"`
	MinZoom     string `json:"minZoom"`
	MaxZoom     string `json:"maxZoom"`
}

type legendWire struct {
	Layer string `json:"layer"`
	Label string `json:"label"`
	Order string `json:"order"`
}

type assetWire struct {
	ID         string `json:"id"`
	MediaType  string `json:"mediaType"`
	Path       string `json:"path"`
	Provenance string `json:"provenance,omitempty"`
}

func outlineResponse(volume vnext.Volume) outlineWire {
	out := outlineWire{Version: outlineWireVersion, ID: volume.ID, Title: volume.Title}
	for _, world := range volume.Worlds {
		out.Worlds = append(out.Worlds, worldResponse(world))
	}
	for _, asset := range volume.Assets {
		out.Assets = append(out.Assets, assetWire{
			ID: asset.ID, MediaType: asset.MediaType, Path: asset.Path, Provenance: asset.Provenance,
		})
	}
	return out
}

func worldResponse(world vnext.World) worldWire {
	space := world.CoordinateSpace
	out := worldWire{
		ID: world.ID, Title: world.Title, Claims: propertyResponses(world.Claims),
		CoordinateSpace: coordinateSpaceWire{
			ID: space.ID, Kind: space.Kind, Unit: space.Unit, Definition: space.Definition, Extent: space.Extent,
			SourceZoom: intText(space.SourceZoom), OriginX: intText(space.OriginX), OriginY: intText(space.OriginY),
			TileSize: intText(space.TileSize), Size: intText(space.Size),
		},
		Presentation: presentationResponse(world.Presentation),
	}
	for _, set := range world.FeatureSets {
		out.FeatureSets = append(out.FeatureSets, featureSetResponse(set))
	}
	for _, raster := range world.RasterPyramids {
		out.RasterPyramids = append(out.RasterPyramids, rasterResponse(raster))
	}
	return out
}

func featureSetResponse(set vnext.FeatureSet) featureSetWire {
	out := featureSetWire{
		ID: set.ID, Title: set.Title, SemanticType: set.SemanticType, Claims: propertyResponses(set.Claims),
	}
	for _, field := range set.Properties {
		out.Properties = append(out.Properties, fieldWire{
			ID: field.ID.String(), Name: field.Name, Kind: field.Kind, Optional: field.Optional,
		})
	}
	return out
}

func rasterResponse(raster vnext.RasterPyramid) rasterWire {
	out := rasterWire{
		ID: raster.ID, Name: raster.Name, Codec: raster.Codec, TileSize: intText(raster.TileSize),
		MinZoom: intText(raster.MinZoom), MaxZoom: intText(raster.MaxZoom), FullZoom: intText(raster.FullZoom),
		SourceZoom: intText(raster.SourceZoom), Template: raster.Template, Formats: append([]string(nil), raster.Formats...),
		Interpolate: raster.Interpolate, Background: raster.Background, Shard: intText(raster.Shard),
	}
	if raster.Bounds != nil {
		out.Bounds = rectResponse(*raster.Bounds)
	}
	if raster.Surface != nil {
		out.Surface = rectResponse(*raster.Surface)
	}
	for _, coverage := range raster.Coverage {
		out.Coverage = append(out.Coverage, coverageWire{
			Zoom: intText(coverage.Zoom), X: intText(coverage.X), Y: intText(coverage.Y),
			W: intText(coverage.W), H: intText(coverage.H), Bits: base64.StdEncoding.EncodeToString(coverage.Bits),
		})
	}
	return out
}

func rectResponse(rect vnext.RasterRect) *rasterRectWire {
	return &rasterRectWire{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

func presentationResponse(presentation vnext.Presentation) presentationWire {
	out := presentationWire{ID: presentation.ID, Title: presentation.Title}
	for _, style := range presentation.Styles {
		out.Styles = append(out.Styles, styleWire{
			ID: style.ID, Symbol: style.Symbol, Icon: style.Icon, IconAsset: style.IconAsset, IconPicture: style.IconPicture,
			RenderAs: style.RenderAs, Stroke: style.Stroke, Fill: style.Fill,
		})
	}
	for _, layer := range presentation.Layers {
		out.Layers = append(out.Layers, layerWire{
			ID: layer.ID, FeatureSet: layer.FeatureSet, Style: layer.Style, Group: layer.Group, LabelPolicy: layer.LabelPolicy,
			Visible: layer.Visible, Order: intText(layer.Order), MinZoom: intText(layer.MinZoom), MaxZoom: intText(layer.MaxZoom),
		})
	}
	for _, legend := range presentation.Legend {
		out.Legend = append(out.Legend, legendWire{Layer: legend.Layer, Label: legend.Label, Order: intText(legend.Order)})
	}
	return out
}

func propertyResponses(properties []vnext.Property) []propertyWire {
	out := make([]propertyWire, 0, len(properties))
	for _, property := range properties {
		out = append(out, propertyResponse(property))
	}
	return out
}

func intText(value int64) string { return strconv.FormatInt(value, 10) }
