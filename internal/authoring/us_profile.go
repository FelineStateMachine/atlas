package authoring

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	usTopoService  = "https://basemap.nationalmap.gov/arcgis/rest/services/USGSTopo/MapServer/export"
	tigerRoads     = "https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/Transportation/MapServer"
	tigerHydro     = "https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/Hydro/MapServer"
	tigerCounties  = "https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/State_County/MapServer/1"
	webMercatorR   = 20037508.342789244
	webMercatorLat = 85.05112878
	maxExportSize  = int64(4096)
)

// AreaProfile combines an arbitrary geographic selection with the built-in
// official U.S. source pack. Bounds are WGS84 west, south, east, north
// coordinates; they are not restricted to the pack's coverage.
type AreaProfile struct {
	ID              string
	Title           string
	Bounds          [4]float64
	DetailZoom      int
	IncludeTopo     bool
	IncludeRoads    bool
	IncludeHydro    bool
	IncludeCounties bool
	Presentation    AreaPresentation
}

// AreaPresentation is the creator-owned layer, label, ordering, and color
// choice that is compiled into the Atlas presentation and legend.
type AreaPresentation struct {
	RoadLabel, HydroLabel, CountyLabel            string
	RoadColor, HydroColor, HydroFill, CountyColor string
	RoadVisible, HydroVisible, CountyVisible      bool
	RoadOrder, HydroOrder, CountyOrder            int64
}

// AreaSummary is the cost preview shown before a manifest is installed.
type AreaSummary struct {
	OriginX         int64
	OriginY         int64
	SourceZoom      int
	PixelSize       int64
	RasterTiles     int64
	CaptureRequests int
	EstimatedBytes  int64
}

// Deprecated compatibility aliases. New code should name the geographic
// selection separately from the U.S.-coverage source pack.
type USAreaProfile = AreaProfile
type USAreaPresentation = AreaPresentation
type USAreaSummary = AreaSummary

// DefaultAreaProfile selects the built-in official U.S.-coverage source pack.
func DefaultAreaProfile() AreaProfile {
	return AreaProfile{
		ID: "sample-region", Title: "Sample Region",
		Bounds:     [4]float64{-105.1, 39.6, -104.9, 39.8},
		DetailZoom: 12, IncludeTopo: true, IncludeRoads: true,
		IncludeHydro: true, IncludeCounties: true,
		Presentation: AreaPresentation{
			RoadLabel: "Roads", HydroLabel: "Water", CountyLabel: "Counties",
			RoadColor: "#b45f3c", HydroColor: "#4b8db8", HydroFill: "#b9d9ea", CountyColor: "#766b5b",
			RoadVisible: true, HydroVisible: true, CountyVisible: true,
			RoadOrder: 40, HydroOrder: 20, CountyOrder: 10,
		},
	}
}

// DefaultUSAreaProfile is kept for callers that name the bundled source pack.
// Deprecated: use DefaultAreaProfile.
func DefaultUSAreaProfile() USAreaProfile { return DefaultAreaProfile() }

// NewAreaProject turns one arbitrary Web Mercator region selection into a
// complete portable project using the selected source families. Captures and
// artifacts remain outside the manifest.
func NewAreaProject(profile AreaProfile) (Project, AreaSummary, error) {
	window, err := usTileWindow(profile.Bounds, profile.DetailZoom)
	if err != nil {
		return Project{}, AreaSummary{}, err
	}
	if !profile.IncludeTopo && !profile.IncludeRoads && !profile.IncludeHydro && !profile.IncludeCounties {
		return Project{}, AreaSummary{}, fmt.Errorf("select at least one source")
	}
	if err := validateUSPresentation(profile); err != nil {
		return Project{}, AreaSummary{}, err
	}
	project := usProjectBase(window, profile.DetailZoom, profile.ID, profile.Title)
	if profile.IncludeTopo {
		project.Rasters = append(project.Rasters, usTopoRaster(window, profile.DetailZoom))
	}
	if profile.IncludeRoads {
		addRoads(&project, window.transform, profile.Presentation)
	}
	if profile.IncludeHydro {
		addHydro(&project, window.transform, profile.Presentation)
	}
	if profile.IncludeCounties {
		addCounties(&project, window.transform, profile.Presentation)
	}
	if err := project.Validate(); err != nil {
		return Project{}, AreaSummary{}, fmt.Errorf("validate area project: %w", err)
	}
	return project, summarizeUSProject(project, window), nil
}

// NewUSAreaProject is kept for callers that name the bundled source pack.
// Deprecated: use NewAreaProject.
func NewUSAreaProject(profile USAreaProfile) (Project, USAreaSummary, error) {
	return NewAreaProject(profile)
}

func validateUSPresentation(profile AreaProfile) error {
	choices := []struct {
		enabled bool
		label   string
		colors  []string
		order   int64
	}{
		{profile.IncludeRoads, profile.Presentation.RoadLabel, []string{profile.Presentation.RoadColor}, profile.Presentation.RoadOrder},
		{profile.IncludeHydro, profile.Presentation.HydroLabel, []string{profile.Presentation.HydroColor, profile.Presentation.HydroFill}, profile.Presentation.HydroOrder},
		{profile.IncludeCounties, profile.Presentation.CountyLabel, []string{profile.Presentation.CountyColor}, profile.Presentation.CountyOrder},
	}
	for _, choice := range choices {
		if !choice.enabled {
			continue
		}
		if strings.TrimSpace(choice.label) == "" || choice.order < 0 {
			return fmt.Errorf("enabled presentation layers require labels and non-negative order")
		}
		for _, color := range choice.colors {
			if !validHexColor(color) {
				return fmt.Errorf("presentation color %q must be #RRGGBB", color)
			}
		}
	}
	return nil
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

// MarshalProjectYAML emits the single portable manifest after validating it.
func MarshalProjectYAML(project Project) ([]byte, error) {
	if err := project.Validate(); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(project)
	if err != nil {
		return nil, fmt.Errorf("encode project manifest: %w", err)
	}
	return data, nil
}

type usWindow struct {
	originX, originY int64
	side, pixels     int64
	maxZoom          int64
	bbox             [4]float64
	transform        CoordinateTransform
}

func usTileWindow(bounds [4]float64, zoom int) (usWindow, error) {
	for _, coordinate := range bounds {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return usWindow{}, fmt.Errorf("area coordinates must be finite")
		}
	}
	if bounds[0] >= bounds[2] || bounds[1] >= bounds[3] {
		return usWindow{}, fmt.Errorf("area requires west < east and south < north")
	}
	if bounds[0] < -180 || bounds[2] > 180 || bounds[1] < -webMercatorLat || bounds[3] > webMercatorLat {
		return usWindow{}, fmt.Errorf("area must stay within the Web Mercator world")
	}
	if zoom < 6 || zoom > 16 {
		return usWindow{}, fmt.Errorf("detail zoom must be between 6 and 16")
	}
	n := int64(1) << zoom
	minX, maxY := lonTile(bounds[0], n), latTile(bounds[1], n)
	maxX, minY := lonTileUpper(bounds[2], n), latTileUpper(bounds[3], n)
	originX, originY, side := alignedSquare(minX, minY, maxX, maxY)
	pixels := side * 256
	if pixels > maxExportSize {
		return usWindow{}, fmt.Errorf("selection needs a %d-pixel source image; maximum is %d (lower detail or narrow the area)", pixels, maxExportSize)
	}
	bbox := mercatorTileBounds(originX, originY, side, n)
	scale := float64(256*n) / (2 * webMercatorR)
	transform := CoordinateTransform{Kind: "affine", Matrix: [6]float64{
		scale, 0, webMercatorR*scale - float64(256*originX),
		0, -scale, webMercatorR*scale - float64(256*originY),
	}}
	return usWindow{originX: originX, originY: originY, side: side, pixels: pixels, maxZoom: int64(math.Log2(float64(side))), bbox: bbox, transform: transform}, nil
}

func lonTile(longitude float64, count int64) int64 {
	return int64(math.Floor((longitude + 180) / 360 * float64(count)))
}

func lonTileUpper(longitude float64, count int64) int64 {
	return int64(math.Ceil((longitude+180)/360*float64(count))) - 1
}

func latTile(latitude float64, count int64) int64 {
	value := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * float64(count)
	return int64(math.Floor(value))
}

func latTileUpper(latitude float64, count int64) int64 {
	value := (1 - math.Asinh(math.Tan(latitude*math.Pi/180))/math.Pi) / 2 * float64(count)
	return int64(math.Floor(value))
}

func alignedSquare(minX, minY, maxX, maxY int64) (int64, int64, int64) {
	for side := int64(1); ; side *= 2 {
		originX, originY := minX-(minX%side), minY-(minY%side)
		if originX+side-1 >= maxX && originY+side-1 >= maxY {
			return originX, originY, side
		}
	}
}

func mercatorTileBounds(originX, originY, side, count int64) [4]float64 {
	unit := 2 * webMercatorR / float64(count)
	return [4]float64{
		-webMercatorR + float64(originX)*unit,
		webMercatorR - float64(originY+side)*unit,
		-webMercatorR + float64(originX+side)*unit,
		webMercatorR - float64(originY)*unit,
	}
}

func usProjectBase(window usWindow, sourceZoom int, id, title string) Project {
	if id == "" {
		id = "sample-region"
	}
	if title == "" {
		title = "Sample Region"
	}
	return Project{
		Schema: ProjectSchema, SchemaNamespace: "atlas.local/" + id + "/v1",
		ID: id, Title: title,
		Target: Target{World: id, Title: title, CoordinateSpace: CoordinateSpace{
			ID: id + "-grid", Kind: "projected", Unit: "pixel", Definition: "atlas:tile-plane",
			Extent:     [4]float64{0, 0, float64(window.pixels), float64(window.pixels)},
			SourceZoom: int64(sourceZoom), OriginX: window.originX, OriginY: window.originY,
			TileSize: 256, Size: window.pixels,
		}},
		Presentation: Presentation{ID: "default", Title: title},
		Release:      Release{Revision: 1},
		Budgets:      BuildBudgets{RequestBytes: 512 << 20, TotalBytes: 4 << 30, Requests: 10_000, RasterTiles: 10_000, RasterPixels: 1_000_000_000},
	}
}

func usTopoRaster(window usWindow, sourceZoom int) Raster {
	query := url.Values{
		"bbox": {floatList(window.bbox)}, "bboxSR": {"3857"}, "imageSR": {"3857"},
		"size":   {strconv.FormatInt(window.pixels, 10) + "," + strconv.FormatInt(window.pixels, 10)},
		"format": {"png32"}, "transparent": {"false"}, "f": {"image"},
	}
	return Raster{
		ID: "usgs-topo", Name: "USGS Topo", Adapter: "raster-file", Locator: usTopoService + "?" + query.Encode(),
		MediaType: "image/png", License: "U.S. government work; public domain", Attribution: "Map services and data available from U.S. Geological Survey, National Geospatial Program.",
		EstimateBytes: window.pixels * window.pixels, TileSize: 256, SourceZoom: int64(sourceZoom), MaxZoom: window.maxZoom,
		FullZoom: window.maxZoom, Interpolate: true, Background: "#e9e4d7",
		Bounds: &RasterRect{Width: float64(window.pixels), Height: float64(window.pixels)},
	}
}

func floatList(values [4]float64) string {
	return strconv.FormatFloat(values[0], 'f', 6, 64) + "," + strconv.FormatFloat(values[1], 'f', 6, 64) + "," +
		strconv.FormatFloat(values[2], 'f', 6, 64) + "," + strconv.FormatFloat(values[3], 'f', 6, 64)
}

func addRoads(project *Project, transform CoordinateTransform, presentation AreaPresentation) {
	project.FeatureSets = append(project.FeatureSets, FeatureSetContract{
		ID: "roads", Title: "Roads", SemanticType: "transportation", Geometry: "path",
		Properties: []PropertyContract{{ID: "name", Name: "Name", Type: "string", Optional: true}, {ID: "class", Name: "Class", Type: "string", Optional: true}, {ID: "route-type", Name: "Route type", Type: "string", Optional: true}},
	})
	for _, layer := range []struct{ id, number string }{{"primary-roads", "2"}, {"secondary-roads", "6"}, {"local-roads", "8"}} {
		source := tigerSource(layer.id, tigerRoads+"/"+layer.number, "roads", "path", transform)
		source.Mapping.Identity = "properties.OID"
		source.Mapping.FeatureTitle = "coalesce(properties.NAME, properties.OID)"
		source.Mapping.Properties = append(source.Mapping.Properties, PropertyMap{Field: "route-type", Source: "properties.RTTYP"})
		project.Sources = append(project.Sources, source)
	}
	project.Presentation.Styles = append(project.Presentation.Styles, Style{ID: "roads", Stroke: presentation.RoadColor})
	project.Presentation.Layers = append(project.Presentation.Layers, Layer{ID: "roads", FeatureSet: "roads", Style: "roads", Label: presentation.RoadLabel, Group: "Transportation", LabelPolicy: "quiet", Visible: presentation.RoadVisible, Order: presentation.RoadOrder, MaxZoom: 32})
}

func addHydro(project *Project, transform CoordinateTransform, presentation AreaPresentation) {
	project.FeatureSets = append(project.FeatureSets,
		FeatureSetContract{ID: "waterways", Title: "Waterways", SemanticType: "hydrography", Geometry: "path", Properties: commonTigerProperties()},
		FeatureSetContract{ID: "water-bodies", Title: "Water bodies", SemanticType: "hydrography", Geometry: "area", Properties: append(commonTigerProperties(), PropertyContract{ID: "water-area", Name: "Water area", Type: "int64", Optional: true})},
	)
	waterways := tigerSource("waterways", tigerHydro+"/0", "waterways", "path", transform)
	waterBodies := tigerSource("water-bodies", tigerHydro+"/1", "water-bodies", "area", transform)
	waterBodies.Mapping.Properties = append(waterBodies.Mapping.Properties, PropertyMap{Field: "water-area", Source: "properties.AREAWATER"})
	project.Sources = append(project.Sources, waterways, waterBodies)
	project.Presentation.Styles = append(project.Presentation.Styles, Style{ID: "waterways", Stroke: presentation.HydroColor}, Style{ID: "water-bodies", Stroke: presentation.HydroColor, Fill: presentation.HydroFill})
	project.Presentation.Layers = append(project.Presentation.Layers,
		Layer{ID: "water-bodies", FeatureSet: "water-bodies", Style: "water-bodies", Label: presentation.HydroLabel + " bodies", Group: presentation.HydroLabel, LabelPolicy: "quiet", Visible: presentation.HydroVisible, Order: presentation.HydroOrder, MaxZoom: 32},
		Layer{ID: "waterways", FeatureSet: "waterways", Style: "waterways", Label: presentation.HydroLabel + "ways", Group: presentation.HydroLabel, LabelPolicy: "quiet", Visible: presentation.HydroVisible, Order: presentation.HydroOrder + 1, MaxZoom: 32},
	)
}

func addCounties(project *Project, transform CoordinateTransform, presentation AreaPresentation) {
	project.FeatureSets = append(project.FeatureSets, FeatureSetContract{
		ID: "jurisdictions", Title: "Counties", SemanticType: "jurisdiction", Geometry: "area",
		Properties: []PropertyContract{{ID: "name", Name: "Name", Type: "string"}, {ID: "land-area", Name: "Land area", Type: "int64", Optional: true}, {ID: "water-area", Name: "Water area", Type: "int64", Optional: true}},
	})
	source := tigerSource("counties", tigerCounties, "jurisdictions", "area", transform)
	source.Mapping.Identity, source.Mapping.FeatureTitle = "properties.GEOID", "properties.NAME"
	source.Mapping.Properties = []PropertyMap{{Field: "name", Source: "properties.NAME"}, {Field: "land-area", Source: "properties.AREALAND"}, {Field: "water-area", Source: "properties.AREAWATER"}}
	project.Sources = append(project.Sources, source)
	project.Presentation.Styles = append(project.Presentation.Styles, Style{ID: "counties", Stroke: presentation.CountyColor})
	project.Presentation.Layers = append(project.Presentation.Layers, Layer{ID: "counties", FeatureSet: "jurisdictions", Style: "counties", Label: presentation.CountyLabel, Group: "Context", LabelPolicy: "quiet", Visible: presentation.CountyVisible, Order: presentation.CountyOrder, MaxZoom: 32})
}

func commonTigerProperties() []PropertyContract {
	return []PropertyContract{{ID: "name", Name: "Name", Type: "string", Optional: true}, {ID: "class", Name: "Class", Type: "string", Optional: true}}
}

func tigerSource(id, locator, set, family string, transform CoordinateTransform) Source {
	return Source{
		ID: id, Adapter: "arcgis-feature-service", Locator: locator, MediaType: "application/geo+json",
		License: "U.S. government work; public domain", Attribution: "Source: U.S. Census Bureau TIGERweb. This product does not imply Census Bureau endorsement.",
		EstimateBytes: 16 << 20, Query: Query{Where: "1=1", Limit: 1000, ObjectID: "OBJECTID", SpatialReference: 3857},
		Mapping: Mapping{
			FeatureSet: set, Identity: "properties.OBJECTID", FeatureTitle: "coalesce(properties.NAME, properties.OBJECTID)",
			Geometry:   GeometryMap{Family: family, SourceSpace: "EPSG:3857", Transform: transform},
			Properties: []PropertyMap{{Field: "name", Source: "properties.NAME"}, {Field: "class", Source: "properties.MTFCC"}},
		},
	}
}

func summarizeUSProject(project Project, window usWindow) AreaSummary {
	tiles := (int64(1)<<(2*(window.maxZoom+1)) - 1) / 3
	estimated := int64(0)
	for _, source := range project.Sources {
		estimated += source.EstimateBytes
	}
	for _, raster := range project.Rasters {
		estimated += raster.EstimateBytes
	}
	return AreaSummary{
		OriginX: window.originX, OriginY: window.originY, SourceZoom: int(project.Target.CoordinateSpace.SourceZoom),
		PixelSize: window.pixels, RasterTiles: tiles, CaptureRequests: len(project.Sources) + len(project.Rasters), EstimatedBytes: estimated,
	}
}
