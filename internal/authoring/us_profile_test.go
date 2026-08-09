package authoring

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUSAreaProjectBuildsTheNationwideDefaultContract(t *testing.T) {
	profile := DefaultUSAreaProfile()
	profile.Bounds = [4]float64{-105.1, 39.6, -104.9, 39.8}
	profile.DetailZoom = 12

	project, summary, err := NewUSAreaProject(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(); err != nil {
		t.Fatalf("generated project is invalid: %v", err)
	}
	if project.ID != "sample-region" || project.Title != "Sample Region" {
		t.Fatalf("project identity = %q / %q", project.ID, project.Title)
	}
	space := project.Target.CoordinateSpace
	if space.Definition != "atlas:tile-plane" || space.SourceZoom != 12 || space.Size != summary.PixelSize {
		t.Fatalf("coordinate space = %+v, summary = %+v", space, summary)
	}
	if space.Extent != [4]float64{0, 0, float64(space.Size), float64(space.Size)} {
		t.Fatalf("coordinate extent = %v", space.Extent)
	}
	if len(project.Rasters) != 1 || project.Rasters[0].Adapter != "raster-file" || !strings.Contains(project.Rasters[0].Locator, "USGSTopo/MapServer/export") {
		t.Fatalf("topographic raster = %+v", project.Rasters)
	}
	if len(project.Sources) != 6 {
		t.Fatalf("feature sources = %d, want roads, hydro, and counties", len(project.Sources))
	}
	for _, source := range project.Sources {
		mapped := make(map[string]string, len(source.Mapping.Properties))
		for _, property := range source.Mapping.Properties {
			mapped[property.Field] = property.Source
		}
		switch source.ID {
		case "primary-roads", "secondary-roads", "local-roads":
			if source.Mapping.Identity != "properties.OID" || source.Query.ObjectID != "OBJECTID" || mapped["route-type"] != "properties.RTTYP" {
				t.Errorf("road identity or mapping = %+v", source)
			}
		case "water-bodies":
			if mapped["water-area"] != "properties.AREAWATER" {
				t.Errorf("water body mapping = %+v", mapped)
			}
		case "counties":
			if mapped["land-area"] != "properties.AREALAND" || mapped["water-area"] != "properties.AREAWATER" {
				t.Errorf("county mapping = %+v", mapped)
			}
		}
	}
	wantSets := map[string]string{"roads": "path", "waterways": "path", "water-bodies": "area", "jurisdictions": "area"}
	for _, set := range project.FeatureSets {
		if wantSets[set.ID] != set.Geometry {
			t.Errorf("feature set %s geometry = %q", set.ID, set.Geometry)
		}
		delete(wantSets, set.ID)
	}
	if len(wantSets) != 0 {
		t.Fatalf("missing feature sets: %v", wantSets)
	}
	if summary.CaptureRequests != len(project.Sources)+1 || summary.RasterTiles < 1 || summary.EstimatedBytes < 1 {
		t.Fatalf("summary = %+v", summary)
	}
	volume, err := assemble(project, nil)
	if err != nil {
		t.Fatalf("assemble presentation: %v", err)
	}
	presentation := volume.Worlds[0].Presentation
	if len(presentation.Legend) != len(project.Presentation.Layers) || len(presentation.Legend) == 0 {
		t.Fatalf("compiled legend = %+v for layers %+v", presentation.Legend, project.Presentation.Layers)
	}
	plan, err := PlanProject(project, nil, t.TempDir())
	if err != nil {
		t.Fatalf("generated project does not plan: %v", err)
	}
	if plan.RasterTiles != summary.RasterTiles || len(plan.Requests) != summary.CaptureRequests {
		t.Fatalf("plan = %d tiles / %d requests, summary = %+v", plan.RasterTiles, len(plan.Requests), summary)
	}
}

func TestUSAreaProjectCanSelectSourceFamiliesWithoutLosingTypedPresentation(t *testing.T) {
	profile := DefaultUSAreaProfile()
	profile.Bounds = [4]float64{-105.1, 39.6, -104.9, 39.8}
	profile.DetailZoom = 10
	profile.IncludeHydro = false
	profile.IncludeCounties = false
	profile.Presentation.RoadLabel = "Routes"
	profile.Presentation.RoadColor = "#c05030"
	profile.Presentation.RoadVisible = false
	profile.Presentation.RoadOrder = 17

	project, _, err := NewUSAreaProject(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.FeatureSets) != 1 || project.FeatureSets[0].ID != "roads" {
		t.Fatalf("selected feature sets = %+v", project.FeatureSets)
	}
	if len(project.Presentation.Layers) != 1 || project.Presentation.Layers[0].FeatureSet != "roads" {
		t.Fatalf("selected presentation = %+v", project.Presentation.Layers)
	}
	layer, style := project.Presentation.Layers[0], project.Presentation.Styles[0]
	if layer.Label != "Routes" || layer.Visible || layer.Order != 17 || style.Stroke != "#c05030" {
		t.Fatalf("creator presentation choices were lost: layer=%+v style=%+v", layer, style)
	}
	for _, source := range project.Sources {
		if source.Mapping.FeatureSet != "roads" || source.Query.SpatialReference != 3857 || source.Query.ObjectID == "" {
			t.Fatalf("road source = %+v", source)
		}
	}
}

func TestUSAreaProjectSupportsAlaskaAndHawaii(t *testing.T) {
	tests := []struct {
		name   string
		bounds [4]float64
	}{
		{name: "Alaska", bounds: [4]float64{-151.0, 60.9, -150.7, 61.2}},
		{name: "Hawaii", bounds: [4]float64{-157.95, 21.25, -157.7, 21.45}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := DefaultUSAreaProfile()
			profile.Bounds, profile.DetailZoom = tt.bounds, 11
			project, _, err := NewUSAreaProject(profile)
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Validate(); err != nil {
				t.Fatalf("generated project is invalid: %v", err)
			}
		})
	}
}

func TestAreaProjectSelectionIsGeographicRatherThanSourceBounded(t *testing.T) {
	for name, bounds := range map[string][4]float64{
		"Paris":  {2.28, 48.82, 2.42, 48.91},
		"Sydney": {151.14, -33.92, 151.26, -33.82},
	} {
		t.Run(name, func(t *testing.T) {
			profile := DefaultAreaProfile()
			profile.Bounds, profile.DetailZoom = bounds, 11
			if _, _, err := NewAreaProject(profile); err != nil {
				t.Fatalf("an arbitrary geographic selection was coupled to source coverage: %v", err)
			}
		})
	}
}

func TestUSAreaProjectCarriesCreatorOwnedIdentity(t *testing.T) {
	profile := DefaultUSAreaProfile()
	profile.ID = "my-region"
	profile.Title = "My Region"
	project, _, err := NewUSAreaProject(profile)
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "my-region" || project.Title != "My Region" || project.Target.World != "my-region" || project.Target.Title != "My Region" {
		t.Fatalf("creator identity was not carried: %+v", project)
	}
	if project.Target.CoordinateSpace.ID != "my-region-grid" || project.Presentation.Title != "My Region" {
		t.Fatalf("dependent identity was not carried: target=%+v presentation=%+v", project.Target, project.Presentation)
	}
}

func TestUSRoadLayersDoNotFuseSharedPagingObjectIDs(t *testing.T) {
	profile := DefaultUSAreaProfile()
	profile.Bounds = [4]float64{-105.1, 39.6, -104.9, 39.8}
	profile.DetailZoom = 10
	profile.IncludeTopo, profile.IncludeHydro, profile.IncludeCounties = false, false, false
	project, _, err := NewUSAreaProject(profile)
	if err != nil {
		t.Fatal(err)
	}
	var observations []observation
	for index, source := range project.Sources[:2] {
		extent, err := sourceExtent(source.Mapping.Geometry, project.Target.CoordinateSpace)
		if err != nil {
			t.Fatal(err)
		}
		x, y := (extent[0]+extent[2])/2, (extent[1]+extent[3])/2
		body := []byte(fmt.Sprintf(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"OBJECTID":7,"OID":"road-%d","NAME":"Road %d","MTFCC":"S1100","RTTYP":"I"},"geometry":{"type":"LineString","coordinates":[[%f,%f],[%f,%f]]}}]}`,
			index, index, x, y, x+1, y+1))
		seen, err := observe(project, index, source, Capture{}, body)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, seen...)
	}
	volume, err := assemble(project, observations)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(volume.Worlds[0].FeatureSets[0].Features); got != 2 {
		t.Fatalf("two road layers sharing OBJECTID fused into %d feature(s)", got)
	}
}

func TestUSAreaProjectRefusesOutOfBoundsOrExplosiveSelections(t *testing.T) {
	tests := []struct {
		name    string
		bounds  [4]float64
		zoom    int
		message string
	}{
		{name: "outside Web Mercator", bounds: [4]float64{2, 85, 3, 89}, zoom: 10, message: "Web Mercator world"},
		{name: "empty", bounds: [4]float64{-105, 40, -105, 40}, zoom: 10, message: "west < east"},
		{name: "too detailed", bounds: [4]float64{-124, 25, -67, 49}, zoom: 16, message: "4096"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := DefaultUSAreaProfile()
			profile.Bounds, profile.DetailZoom = tt.bounds, tt.zoom
			if _, _, err := NewUSAreaProject(profile); err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("error = %v, want %q", err, tt.message)
			}
		})
	}
}

func TestMarshalProjectYAMLRoundTripsStrictly(t *testing.T) {
	profile := DefaultUSAreaProfile()
	profile.Bounds = [4]float64{-105.1, 39.6, -104.9, 39.8}
	profile.DetailZoom = 11
	project, _, err := NewUSAreaProject(profile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalProjectYAML(project)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var restored Project
	if err := decoder.Decode(&restored); err != nil {
		t.Fatalf("strict decode: %v\n%s", err, data)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("round trip: %v\n%s", err, data)
	}
	if !strings.Contains(string(data), "title: Sample Region") || !strings.Contains(string(data), "origin-x:") || !strings.Contains(string(data), "origin-y:") {
		t.Fatalf("portable manifest misses neutral identity or tile origins:\n%s", data)
	}
}
